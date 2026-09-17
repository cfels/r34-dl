package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"moxiu/r34-dl/api"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	videoMaxFPS          = 60
	interpolateMaxPixels = 130000
	videoUserAgent       = "r34-dl/viewer"
)

const (
	kittyGraphicsPrefix = "\x1b_Ga=T"
	kittyTransmitPrefix = "\x1b_Ga=T,f=100,m=1;"
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
	ext := strings.ToLower(filepath.Ext(p.Image))
	return videoExts[ext]
}

type videoPlayer struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr bytes.Buffer

	width  int
	height int
	source string

	frames chan image.Image
	done   chan struct{}

	audioMu   sync.Mutex
	audio     *exec.Cmd
	muted     bool
	audioOnce sync.Once

	idMu     sync.Mutex
	frameSeq int

	stopOnce sync.Once
	waitOnce sync.Once
	errMu    sync.Mutex
	err      error
}

type videoOptions struct {
	audio  bool
	smooth *bool
}

func videoFitSize(termCols, termRows, srcW, srcH int) (int, int) {
	previewRows := int(float64(termRows)*previewHeightFraction) - 2
	if previewRows < 4 {
		previewRows = 4
	}
	boxW := termCols * 8
	boxH := previewRows * 16
	if srcW <= 0 || srcH <= 0 {
		return boxW, boxH
	}
	scale := math.Min(float64(boxW)/float64(srcW), float64(boxH)/float64(srcH))
	w := int(float64(srcW) * scale)
	h := int(float64(srcH) * scale)
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}
	return w, h
}

func videoRateFilter(rate float64, pixels int, smooth *bool) string {
	switch {
	case rate > videoMaxFPS:
		return fmt.Sprintf("fps=%d", videoMaxFPS)
	case smooth != nil && !*smooth:
		return ""
	case rate == 0:
		return fmt.Sprintf("fps=%d", videoMaxFPS)
	case rate >= videoMaxFPS-1:
		return ""
	case smooth == nil && pixels > interpolateMaxPixels:
		return ""
	default:
		return fmt.Sprintf("minterpolate=fps=%d:mi_mode=mci:mc_mode=aobmc:me_mode=bidir:vsbmc=1", videoMaxFPS)
	}
}

func startVideoPlayer(post api.Post, termCols, termRows int, opts videoOptions) (*videoPlayer, error) {
	rawURL := post.FileURL()
	w, h := videoFitSize(termCols, termRows, post.Width, post.Height)
	scale := fmt.Sprintf("scale=%d:%d", w, h)
	if post.Width <= 0 || post.Height <= 0 {
		scale = fmt.Sprintf(
			"scale=%d:%d:force_original_aspect_ratio=decrease,"+
				"pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black",
			w, h, w, h,
		)
	}
	parts := []string{scale}
	if rate := videoRateFilter(sourceFPS(rawURL), w*h, opts.smooth); rate != "" {
		parts = append(parts, rate)
	}
	parts = append(parts, "setsar=1", "format=rgba")
	filter := strings.Join(parts, ",")
	cmd := exec.Command(
		"ffmpeg",
		"-loglevel", "error",
		"-nostdin",
		"-re",
	)
	if isHTTP(rawURL) {
		cmd.Args = append(cmd.Args, "-user_agent", videoUserAgent)
	}
	if isGIFURL(rawURL) || strings.EqualFold(filepath.Ext(post.Image), ".gif") {
		cmd.Args = append(cmd.Args, "-stream_loop", "-1")
	}
	cmd.Args = append(cmd.Args,
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
		muted:  !opts.audio,
		frames: make(chan image.Image, 1),
		done:   make(chan struct{}),
	}
	cmd.Stderr = &vp.stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	vp.stdout = stdout
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("ffmpeg not found, install it to play videos")
		}
		return nil, err
	}
	go vp.readFrames()
	return vp, nil
}

func isHTTP(rawURL string) bool {
	return strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://")
}

func sourceFPS(rawURL string) float64 {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=avg_frame_rate,r_frame_rate",
		"-of", "default=nw=1:nk=1",
	}
	if isHTTP(rawURL) {
		args = append(args, "-user_agent", videoUserAgent)
	}
	args = append(args, rawURL)
	out, err := exec.CommandContext(ctx, "ffprobe", args...).Output()
	if err != nil {
		return 0
	}
	rates := make([]float64, 0, 2)
	for _, line := range strings.Split(string(out), "\n") {
		if rate, ok := parseRate(line); ok {
			rates = append(rates, rate)
		}
	}
	if len(rates) == 0 {
		return 0
	}
	lowest := rates[0]
	for _, rate := range rates[1:] {
		if rate < lowest {
			lowest = rate
		}
	}
	return lowest
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

func (vp *videoPlayer) readFrames() {
	defer close(vp.frames)
	stride := vp.width * 4
	buf := make([]byte, stride*vp.height)
	started := false
	for {
		if _, err := io.ReadFull(vp.stdout, buf); err != nil {
			if !vp.stopped() && err != io.EOF && err != io.ErrUnexpectedEOF {
				vp.setErr(fmt.Errorf("video stream: %w", err))
			}
			break
		}
		pix := make([]byte, len(buf))
		copy(pix, buf)
		img := &image.RGBA{
			Pix:    pix,
			Stride: stride,
			Rect:   image.Rect(0, 0, vp.width, vp.height),
		}
		if !vp.publish(img) {
			return
		}
		if !started {
			started = true
			vp.startAudio()
		}
	}
	vp.wait()
	if !vp.stopped() && vp.takeErr() == nil {
		if msg := lastLine(vp.stderr.String()); msg != "" {
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

func spawnAudio(rawURL string) *exec.Cmd {
	path, err := exec.LookPath("ffplay")
	if err != nil {
		return nil
	}
	args := []string{"-loglevel", "error", "-nodisp", "-autoexit", "-vn", "-sn"}
	args = append(args, audioFilterArgs()...)
	if isHTTP(rawURL) {
		args = append(args, "-user_agent", videoUserAgent)
	}
	cmd := exec.Command(path, append(args, rawURL)...)
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil
	}
	return cmd
}

func (vp *videoPlayer) startAudio() {
	vp.audioOnce.Do(func() {
		vp.audioMu.Lock()
		defer vp.audioMu.Unlock()
		if vp.muted || vp.source == "" {
			return
		}
		vp.audio = spawnAudio(vp.source)
	})
}

func (vp *videoPlayer) toggleMute() {
	vp.audioOnce.Do(func() {})
	vp.audioMu.Lock()
	defer vp.audioMu.Unlock()
	if vp.muted {
		vp.muted = false
		if vp.source != "" {
			vp.audio = spawnAudio(vp.source)
		}
		return
	}
	vp.muted = true
	vp.killAudio()
}

func (vp *videoPlayer) killAudio() {
	if vp.audio != nil {
		if vp.audio.Process != nil {
			_ = vp.audio.Process.Kill()
		}
		_ = vp.audio.Wait()
		vp.audio = nil
	}
}

func (vp *videoPlayer) audioProcess() *exec.Cmd {
	vp.audioMu.Lock()
	defer vp.audioMu.Unlock()
	return vp.audio
}

func (vp *videoPlayer) isMuted() bool {
	vp.audioMu.Lock()
	defer vp.audioMu.Unlock()
	return vp.muted
}

func (vp *videoPlayer) publish(img image.Image) bool {
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
		vp.audioMu.Lock()
		vp.killAudio()
		vp.audioMu.Unlock()
		if vp.cmd != nil && vp.cmd.Process != nil {
			_ = vp.cmd.Process.Kill()
		}
	})
	if vp.cmd != nil {
		vp.wait()
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

func renderVideoFrame(vp *videoPlayer, img image.Image, termCols, termRows int) tea.Cmd {
	return func() tea.Msg {
		scaled := scaleToFit(img, termCols, previewRows(termRows))
		s, err := encodeImage(scaled)
		if err != nil {
			return videoDoneMsg{vp: vp, err: fmt.Errorf("render frame: %w", err)}
		}
		return videoRenderedMsg{vp: vp, s: swapKittyImage(vp, s)}
	}
}

func swapKittyImage(vp *videoPlayer, s string) string {
	if !strings.Contains(s, kittyTransmitPrefix) {
		return s
	}
	cur, prev := vp.kittyImageIDs()
	s = strings.Replace(s, kittyTransmitPrefix, kittyTransmit(cur), 1)
	return s + kittyDeleteImage(prev)
}

func kittyTransmit(id int) string {
	return fmt.Sprintf("\x1b_Ga=T,f=100,i=%d,q=2,m=1;", id)
}

func kittyDeleteImage(id int) string {
	return fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2;\x1b\\", id)
}

func (vp *videoPlayer) kittyImageIDs() (int, int) {
	vp.idMu.Lock()
	defer vp.idMu.Unlock()
	vp.frameSeq++
	return 1 + vp.frameSeq%2, 1 + (vp.frameSeq+1)%2
}
