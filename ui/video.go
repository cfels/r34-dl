package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/safe"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	videoUserAgent = "r34-dl/viewer"

	mediaProtocolWhitelist = api.MediaProtocols
	mediaReadTimeout       = "60000000"

	videoProbeTimeout = 4 * time.Second
)

const (
	kittyGraphicsPrefix = "\x1b_Ga=T"
	kittyTransmitPrefix = "\x1b_Ga=T,f=100,m=1;"
)

const (
	kittyImageIDCycle = 1 << 20
	kittyStaleLimit   = 4
)

var videoExts = map[string]bool{
	".mp4":  true,
	".webm": true,
	".gif":  true,
	".mov":  true,
	".avi":  true,
	".mkv":  true,
	".flv":  true,
}

func isVideo(p api.Post) bool {
	if p.Video {
		return true
	}
	ext := strings.ToLower(filepath.Ext(p.Image))
	return videoExts[ext]
}

type videoPlayer struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser

	stderr     bytes.Buffer
	stderrMu   sync.Mutex
	stderrPipe *os.File

	width  int
	height int
	source string
	proxy  string
	relay  *api.MediaRelay

	frames chan image.Image
	done   chan struct{}

	fpsMu sync.Mutex
	fps   float64

	// audioMu guards muted and audio. audioSpawnMu serialises reconciliation of
	// the desired mute state with the running player: without it two keypresses
	// racing the first frame could each start an ffplay and only one would be
	// tracked, leaving the other playing forever with no way to stop it.
	audioMu      sync.Mutex
	audioSpawnMu sync.Mutex
	audio        *audioTrack
	muted        bool
	audioOnce    sync.Once

	clockMu   sync.Mutex
	startedAt time.Time

	idMu     sync.Mutex
	frameSeq int
	pending  []int

	stopOnce sync.Once
	waitOnce sync.Once
	errMu    sync.Mutex
	err      error
}

type videoOptions struct {
	audio bool
}

func videoFitSize(termCols, termRows, srcW, srcH int) (int, int) {
	cols, rows := videoBox(termCols, termRows)
	boxW := cols * cellW
	boxH := rows * cellH
	if srcW <= 0 || srcH <= 0 {
		srcW, srcH = 16, 9
	}
	scale := math.Min(float64(boxW)/float64(srcW), float64(boxH)/float64(srcH))
	w := int(math.Round(float64(srcW) * scale))
	h := int(math.Round(float64(srcH) * scale))
	if w > boxW {
		w = boxW
	}
	if h > boxH {
		h = boxH
	}
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}
	w &^= 1
	h &^= 1
	return w, h
}

func startVideoPlayer(post api.Post, termCols, termRows int, opts videoOptions) (*videoPlayer, error) {
	stream, err := api.Media(post)
	if err != nil {
		return nil, err
	}
	if !safe.PublicMediaURL(stream.URL) {
		return nil, fmt.Errorf("refusing to play non-public media url for post %d (set %s=1 to allow)", post.ID, safe.AllowPrivateHostsEnv)
	}
	return startStreamPlayer(stream, post, termCols, termRows, opts)
}

func startStreamPlayer(stream api.Stream, post api.Post, termCols, termRows int, opts videoOptions) (*videoPlayer, error) {
	relay, err := api.NewMediaRelay(stream, post.Site)
	if err != nil {
		return nil, err
	}
	rawURL := relay.URL
	srcW, srcH := post.Width, post.Height
	w, h := videoFitSize(termCols, termRows, srcW, srcH)
	scale := fmt.Sprintf(
		"scale=%d:%d:force_original_aspect_ratio=decrease,"+
			"pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black",
		w, h, w, h,
	)
	filter := strings.Join([]string{scale, "setsar=1", "format=rgba"}, ",")
	cmd := exec.Command(
		"ffmpeg",
		"-loglevel", "error",
		"-nostdin",
		"-re",
	)
	cmd.Env = api.MediaEnv()
	if isHTTP(rawURL) {
		cmd.Args = append(cmd.Args, "-user_agent", videoUserAgent, "-http_proxy", relay.Proxy)
	}
	if isGIFURL(rawURL) || strings.EqualFold(filepath.Ext(post.Image), ".gif") {
		cmd.Args = append(cmd.Args, "-stream_loop", "-1")
	}
	cmd.Args = append(cmd.Args,
		"-protocol_whitelist", mediaProtocolWhitelist,
		"-rw_timeout", mediaReadTimeout,
		"-i", rawURL,
		"-an", "-sn",
		"-vf", filter,
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"-",
	)
	vp := &videoPlayer{
		cmd:    cmd,
		width:  w,
		height: h,
		source: rawURL,
		proxy:  relay.Proxy,
		relay:  relay,
		muted:  !opts.audio,
		frames: make(chan image.Image, 1),
		done:   make(chan struct{}),
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		relay.Close()
		return nil, err
	}
	// A real pipe handle keeps os/exec from spawning a copy goroutine that
	// cmd.Wait would block on (it never reaches EOF on Windows).
	cmd.Stderr = stderrWrite
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stderrRead.Close()
		stderrWrite.Close()
		relay.Close()
		return nil, err
	}
	vp.stdout = stdout
	vp.stderrPipe = stderrRead
	if err := cmd.Start(); err != nil {
		stdout.Close()
		stderrRead.Close()
		stderrWrite.Close()
		relay.Close()
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("ffmpeg not found, install it to play videos")
		}
		return nil, err
	}
	stderrWrite.Close()
	go vp.collectStderr()
	go vp.readFrames()
	go vp.probeFramerate(rawURL, relay.Proxy)
	return vp, nil
}

func (vp *videoPlayer) probeFramerate(rawURL, proxy string) {
	info := probeSource(rawURL, proxy)
	if info.fps <= 0 {
		return
	}
	vp.fpsMu.Lock()
	vp.fps = info.fps
	vp.fpsMu.Unlock()
}

func (vp *videoPlayer) fpsLabel() string {
	if vp == nil {
		return ""
	}
	vp.fpsMu.Lock()
	fps := vp.fps
	vp.fpsMu.Unlock()
	if fps <= 0 {
		return ""
	}
	if math.Abs(fps-math.Round(fps)) < 0.01 {
		return fmt.Sprintf("%.0ffps", fps)
	}
	return fmt.Sprintf("%.2ffps", fps)
}

func isHTTP(rawURL string) bool {
	return strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://")
}

type sourceInfo struct {
	fps    float64
	width  int
	height int
}

var (
	probeSourceMu sync.Mutex
	probeSourceFn = probeStream
)

func probeSource(rawURL, proxy string) sourceInfo {
	probeSourceMu.Lock()
	fn := probeSourceFn
	probeSourceMu.Unlock()
	return fn(rawURL, proxy)
}

func probeStream(rawURL, proxy string) sourceInfo {
	ctx, cancel := context.WithTimeout(context.Background(), videoProbeTimeout)
	defer cancel()
	args := []string{
		"-v", "error",
		"-protocol_whitelist", mediaProtocolWhitelist,
		"-rw_timeout", mediaReadTimeout,
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,avg_frame_rate,r_frame_rate",
		"-of", "default=nw=1",
	}
	if isHTTP(rawURL) {
		args = append(args, "-user_agent", videoUserAgent)
		if proxy != "" {
			args = append(args, "-http_proxy", proxy)
		}
	}
	args = append(args, rawURL)
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	cmd.Env = api.MediaEnv()
	out, err := cmd.Output()
	if err != nil {
		return sourceInfo{}
	}
	return parseSourceInfo(string(out))
}

func parseSourceInfo(out string) sourceInfo {
	info := sourceInfo{}
	var avg, nominal float64
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "width":
			if n, err := strconv.Atoi(value); err == nil {
				info.width = n
			}
		case "height":
			if n, err := strconv.Atoi(value); err == nil {
				info.height = n
			}
		case "avg_frame_rate":
			if rate, ok := parseRate(value); ok {
				avg = rate
			}
		case "r_frame_rate":
			if rate, ok := parseRate(value); ok {
				nominal = rate
			}
		}
	}
	if avg > 0 {
		info.fps = avg
	} else {
		info.fps = nominal
	}
	return info
}

func parseRate(s string) (float64, bool) {
	num, den, ok := strings.Cut(strings.TrimSpace(s), "/")
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, false
	}
	d, err := strconv.ParseFloat(den, 64)
	if err != nil || d <= 0 || n <= 0 {
		return 0, false
	}
	return n / d, true
}

func (vp *videoPlayer) collectStderr() {
	buf := make([]byte, 4096)
	for {
		n, err := vp.stderrPipe.Read(buf)
		if n > 0 {
			vp.stderrMu.Lock()
			if vp.stderr.Len() < 64<<10 {
				vp.stderr.Write(buf[:n])
			}
			vp.stderrMu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (vp *videoPlayer) stderrText() string {
	vp.stderrMu.Lock()
	defer vp.stderrMu.Unlock()
	return vp.stderr.String()
}

func (vp *videoPlayer) readFrames() {
	defer close(vp.frames)
	defer vp.relay.Close()
	stride := vp.width * 4
	started := false
	for {
		pix := make([]byte, stride*vp.height)
		if _, err := io.ReadFull(vp.stdout, pix); err != nil {
			if !vp.stopped() && err != io.EOF && err != io.ErrUnexpectedEOF {
				vp.setErr(fmt.Errorf("video stream: %w", err))
			}
			break
		}
		img := &image.RGBA{
			Pix:    pix,
			Stride: stride,
			Rect:   image.Rect(0, 0, vp.width, vp.height),
		}
		if !started {
			started = true
			vp.startAudio()
		}
		if !vp.publish(img) {
			return
		}
	}
	vp.wait()
	if !vp.stopped() && vp.takeErr() == nil {
		if msg := lastLine(vp.stderrText()); msg != "" {
			vp.setErr(errors.New(msg))
		}
	}
}

func isGIFURL(rawURL string) bool {
	if i := strings.IndexAny(rawURL, "?#"); i >= 0 {
		rawURL = rawURL[:i]
	}
	return strings.EqualFold(filepath.Ext(rawURL), ".gif")
}

var (
	audioQualityOnce sync.Once
	audioQualityArgs []string
)

func audioFilterArgs() []string {
	audioQualityOnce.Do(func() {
		soxr := []string{"-af", "aresample=resampler=soxr:precision=28"}
		if ffmpegAccepts(soxr...) {
			audioQualityArgs = soxr
			return
		}
		audioQualityArgs = []string{"-af", "aresample=filter_size=64"}
	})
	return audioQualityArgs
}

func ffmpegAccepts(args ...string) bool {
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=d=0.01")
	cmd.Args = append(cmd.Args, args...)
	cmd.Args = append(cmd.Args, "-f", "null", "-")
	return cmd.Run() == nil
}

const (
	// audioStartLead is added to the picture position when audio starts: ffplay
	// needs a moment to open the stream and fill its buffer, so asking it for a
	// slightly later point keeps the sound level with the frames instead of
	// trailing them.
	audioStartLead = 0.25
	// audioSeekMin is how far in the clip has to be before unmuting seeks at
	// all. Seeking a fraction of a second into a fresh clip would only cut the
	// start of the sound off.
	audioSeekMin = 1.0
	// audioStopTimeout bounds how long a mute waits for ffplay to die. Killing is
	// immediate, so this only ever fires if the OS refuses to reap it.
	audioStopTimeout = 2 * time.Second
)

// audioTrack is one running ffplay plus a channel closed once it exited and was
// reaped. Owning the process this way means a mute can wait for the sound to
// actually stop, and an ffplay that exits on its own frees its slot.
type audioTrack struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (t *audioTrack) stop() {
	if t == nil {
		return
	}
	killProcessTree(t.cmd)
	select {
	case <-t.done:
	case <-time.After(audioStopTimeout):
	}
}

// killProcessTree terminates cmd together with any process it launched. The
// ffplay that LookPath finds is usually a package-manager shim, a small launcher
// that starts the real player as its child: killing only the shim leaves the
// real ffplay running with no handle left and the audio unkillable.
func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		if err := kill.Run(); err == nil {
			return
		}
	}
	_ = cmd.Process.Kill()
}

func (vp *videoPlayer) trackAudio(t *audioTrack) {
	vp.audioMu.Lock()
	vp.audio = t
	vp.audioMu.Unlock()
	go func() {
		_ = t.cmd.Wait()
		close(t.done)
		vp.audioMu.Lock()
		if vp.audio == t {
			vp.audio = nil
		}
		vp.audioMu.Unlock()
	}()
}

func spawnAudio(rawURL, proxy string, offset float64) *audioTrack {
	path, err := exec.LookPath("ffplay")
	if err != nil {
		return nil
	}
	args := []string{"-loglevel", "error", "-nodisp", "-autoexit", "-vn", "-sn"}
	args = append(args, audioFilterArgs()...)
	args = append(args, "-protocol_whitelist", mediaProtocolWhitelist)
	args = append(args, "-rw_timeout", mediaReadTimeout)
	if isHTTP(rawURL) {
		args = append(args, "-user_agent", videoUserAgent)
		if proxy != "" {
			args = append(args, "-http_proxy", proxy)
		}
	}
	if offset > 0 {
		// Input seek: ffplay jumps instead of decoding everything before it.
		args = append(args, "-ss", strconv.FormatFloat(offset, 'f', 3, 64))
	}
	cmd := exec.Command(path, append(args, rawURL)...)
	cmd.Env = api.MediaEnv()
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return nil
	}
	return &audioTrack{cmd: cmd, done: make(chan struct{})}
}

// audioOffset reports how far into the clip the picture is, so unmuting can
// start the sound where the video already is instead of at the beginning. The
// stream is decoded in realtime, so wall clock since the first frame is the
// playback position; frame counting would need an accurate source framerate
// before the probe has answered.
func (vp *videoPlayer) audioOffset() float64 {
	vp.clockMu.Lock()
	start := vp.startedAt
	vp.clockMu.Unlock()
	if start.IsZero() {
		return 0
	}
	elapsed := time.Since(start).Seconds()
	if elapsed < audioSeekMin {
		return 0
	}
	return elapsed + audioStartLead
}

// syncAudio reconciles the desired mute state with the running player. It is the
// only place that starts audio and it always stops the tracked player first, so
// at most one ffplay is ever alive for this video.
func (vp *videoPlayer) syncAudio() {
	vp.audioSpawnMu.Lock()
	defer vp.audioSpawnMu.Unlock()

	vp.audioMu.Lock()
	muted, source, proxy, current := vp.muted, vp.source, vp.proxy, vp.audio
	vp.audioMu.Unlock()

	if muted || source == "" || vp.stopped() {
		if current != nil {
			vp.audioMu.Lock()
			if vp.audio == current {
				vp.audio = nil
			}
			vp.audioMu.Unlock()
			current.stop()
		}
		return
	}
	if current != nil {
		return // Already playing.
	}
	track := spawnAudio(source, proxy, vp.audioOffset())
	if track == nil {
		return
	}
	// The state can change while ffplay is opening the stream; drop the player
	// instead of leaving it running untracked.
	vp.audioMu.Lock()
	stale := vp.muted || vp.stopped()
	vp.audioMu.Unlock()
	if stale {
		track.stop()
		return
	}
	vp.trackAudio(track)
}

func (vp *videoPlayer) toggleMute() {
	vp.audioMu.Lock()
	vp.muted = !vp.muted
	vp.audioMu.Unlock()
	vp.syncAudio()
}

func (vp *videoPlayer) killAudio() {
	vp.audioMu.Lock()
	track := vp.audio
	vp.audio = nil
	vp.audioMu.Unlock()
	track.stop()
}

func (vp *videoPlayer) audioProcess() *exec.Cmd {
	vp.audioMu.Lock()
	defer vp.audioMu.Unlock()
	if vp.audio == nil {
		return nil
	}
	return vp.audio.cmd
}

func (vp *videoPlayer) startAudio() {
	vp.audioOnce.Do(vp.syncAudio)
}

func (vp *videoPlayer) isMuted() bool {
	vp.audioMu.Lock()
	defer vp.audioMu.Unlock()
	return vp.muted
}

func (vp *videoPlayer) publish(img image.Image) bool {
	vp.clockMu.Lock()
	if vp.startedAt.IsZero() {
		vp.startedAt = time.Now()
	}
	vp.clockMu.Unlock()
	select {
	case vp.frames <- img:
		return true
	default:
	}
	select {
	case <-vp.frames:
	default:
	}
	select {
	case vp.frames <- img:
		return true
	case <-vp.done:
		return false
	}
}

func (vp *videoPlayer) stopped() bool {
	select {
	case <-vp.done:
		return true
	default:
		return false
	}
}

func (vp *videoPlayer) stop() {
	vp.stopOnce.Do(func() {
		close(vp.done)
		vp.audioOnce.Do(func() {})
		// killAudio takes audioMu itself; taking it here and then calling it
		// deadlocks the mutex and freezes the whole event loop on stop.
		vp.killAudio()
		if vp.stderrPipe != nil {
			vp.stderrPipe.Close()
		}
		if vp.relay != nil {
			vp.relay.Close()
		}
		killProcessTree(vp.cmd)
	})
	if vp.cmd != nil {
		go vp.wait()
	}
}

func (vp *videoPlayer) wait() {
	vp.waitOnce.Do(func() { _ = vp.cmd.Wait() })
}

func (vp *videoPlayer) setErr(err error) {
	vp.errMu.Lock()
	vp.err = err
	vp.errMu.Unlock()
}

func (vp *videoPlayer) takeErr() error {
	vp.errMu.Lock()
	defer vp.errMu.Unlock()
	return vp.err
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

func nextFrame(vp *videoPlayer) tea.Cmd {
	return func() tea.Msg {
		select {
		case img, ok := <-vp.frames:
			if !ok {
				return videoDoneMsg{vp: vp, err: vp.takeErr()}
			}
			return videoFrameMsg{vp: vp, frame: img}
		case <-vp.done:
			return videoDoneMsg{vp: vp}
		}
	}
}

func previewFrameView(frame, status string) string {
	return status + "\n\n" + strings.TrimSuffix(frame, "\n")
}

// videoFrameOut is where sixel video frames go. Frames are written straight to
// the terminal instead of being rendered through Bubble Tea: the renderer
// repaints a view line whenever it changes, which erased and re-wrapped every
// frame and made playback flicker.
var videoFrameOut io.Writer = os.Stdout

func paintSixelVideoFrame(img image.Image) error {
	s, err := encodeSixelVideoFrame(img)
	if err != nil {
		return err
	}
	_, err = io.WriteString(videoFrameOut, s)
	return err
}

func renderVideoFrame(vp *videoPlayer, img image.Image, termCols, termRows int) tea.Cmd {
	return func() tea.Msg {
		cols, rows := videoBox(termCols, termRows)
		scaled := scaleToFit(img, cols, rows)
		if sixelVideoTerminal() {
			if err := paintSixelVideoFrame(scaled); err != nil {
				return videoDoneMsg{vp: vp, err: fmt.Errorf("render frame: %w", err)}
			}
			return videoRenderedMsg{vp: vp}
		}
		cur, stale := vp.kittyFrameIDs()
		s, err := encodeVideoFrame(scaled, cur, stale)
		if err != nil {
			return videoDoneMsg{vp: vp, err: fmt.Errorf("render frame: %w", err)}
		}
		return videoRenderedMsg{vp: vp, s: s}
	}
}

func encodeVideoFrame(img image.Image, id int, stale []int) (string, error) {
	return encodeTermImage(img, png.NoCompression, kittyTransmit(id), kittyDeleteImages(stale))
}

func kittyTransmit(id int) string {
	return fmt.Sprintf("\x1b_Ga=T,f=100,i=%d,q=2,m=1;", id)
}

func kittyDeleteImage(id int) string {
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2;\x1b\\", id)
}

func kittyDeleteImages(ids []int) string {
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(kittyDeleteImage(id))
	}
	return b.String()
}

func (vp *videoPlayer) kittyFrameIDs() (int, []int) {
	vp.idMu.Lock()
	defer vp.idMu.Unlock()
	vp.frameSeq++
	id := 1 + vp.frameSeq%kittyImageIDCycle
	stale := make([]int, 0, len(vp.pending))
	for _, old := range vp.pending {
		if old != id {
			stale = append(stale, old)
		}
	}
	vp.pending = append(vp.pending, id)
	if len(vp.pending) > kittyStaleLimit {
		vp.pending = append(vp.pending[:0], vp.pending[len(vp.pending)-kittyStaleLimit:]...)
	}
	return id, stale
}
