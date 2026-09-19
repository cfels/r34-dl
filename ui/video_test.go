package ui

import (
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
	"moxiu/r34-dl/safe"

	tea "github.com/charmbracelet/bubbletea"
)

type stubClient struct{}

func (stubClient) SearchPosts(string, int, int) ([]api.Post, error) { return nil, nil }
func (stubClient) CountPosts(string) (int, error)                   { return 0, nil }
func (stubClient) Name() string                                     { return "stub" }
func (stubClient) Autocomplete(string) ([]string, error)            { return nil, nil }

func makeTestVideo(t *testing.T, fps, seconds int) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	path := testMediaFile(t, "clip.mp4")
	cmd := exec.Command(
		"ffmpeg", "-y", "-loglevel", "error",
		"-f", "lavfi", "-i",
		fmt.Sprintf("testsrc2=size=640x360:rate=%d:duration=%d", fps, seconds),
		"-pix_fmt", "yuv420p", path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot generate test video: %v: %s", err, out)
	}
	return path
}

func makeTestGIF(t *testing.T, seconds int) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	path := testMediaFile(t, "loop.gif")
	cmd := exec.Command(
		"ffmpeg", "-y", "-loglevel", "error",
		"-f", "lavfi", "-i",
		fmt.Sprintf("testsrc2=size=160x120:rate=10:duration=%d", seconds),
		"-loop", "0", path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot generate test gif: %v: %s", err, out)
	}
	return path
}

func videoPost(path string) api.Post {
	return api.Post{Image: filepath.Base(path), FileURL_: testMediaURL(path)}
}

var (
	testMediaRoot = sync.OnceValue(func() string {
		dir, err := os.MkdirTemp("", "r34-dl-media")
		if err != nil {
			return ""
		}
		return dir
	})
	testMediaServer = sync.OnceValue(func() *httptest.Server {
		return httptest.NewServer(http.FileServer(http.Dir(testMediaRoot())))
	})
)

func testMediaFile(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(testMediaRoot(), filepath.Base(t.TempDir()))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("media dir: %v", err)
	}
	return filepath.Join(dir, name)
}

func testMediaURL(path string) string {
	rel, err := filepath.Rel(testMediaRoot(), path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return testMediaServer().URL + "/" + filepath.ToSlash(rel)
}

func TestMain(m *testing.M) {
	os.Setenv(safe.AllowPrivateHostsEnv, "1")
	configDir := ""
	if os.Getenv("XDG_CONFIG_HOME") == "" {
		if dir, err := os.MkdirTemp("", "r34-dl-testcfg"); err == nil {
			configDir = dir
			os.Setenv("XDG_CONFIG_HOME", dir)
		}
	}
	code := m.Run()
	if root := testMediaRoot(); root != "" {
		os.RemoveAll(root)
	}
	if configDir != "" {
		os.RemoveAll(configDir)
	}
	os.Exit(code)
}

func playClip(t *testing.T, path string) (int, time.Duration) {
	t.Helper()
	vp, err := startVideoPlayer(videoPost(path), 80, 24, videoOptions{})
	if err != nil {
		t.Fatalf("startVideoPlayer: %v", err)
	}
	defer vp.stop()

	start := time.Now()
	frames := 0
	deadline := time.After(30 * time.Second)
	for {
		select {
		case img, ok := <-vp.frames:
			if !ok {
				if err := vp.takeErr(); err != nil {
					t.Fatalf("playback error: %v", err)
				}
				return frames, time.Since(start)
			}
			if b := img.Bounds(); b.Dx() != vp.width || b.Dy() != vp.height {
				t.Fatalf("frame %d is %dx%d, want %dx%d", frames, b.Dx(), b.Dy(), vp.width, vp.height)
			}
			frames++
		case <-deadline:
			t.Fatal("timed out waiting for frames")
		}
	}
}

func TestVideoPlayerStreamsFrames(t *testing.T) {
	frames, elapsed := playClip(t, makeTestVideo(t, 30, 3))
	if frames < 20 {
		t.Fatalf("only %d frames streamed, expected continuous playback", frames)
	}
	if elapsed < 2*time.Second {
		t.Errorf("3s clip played in %v, playback is not paced in realtime", elapsed)
	}
	if elapsed > 8*time.Second {
		t.Errorf("3s clip took %v, playback is falling behind", elapsed)
	}
}

func TestVideoPlayerMatchesSourceFramerate(t *testing.T) {
	const seconds = 2

	frames, elapsed := playClip(t, makeTestVideo(t, 120, seconds))
	if max := 120*seconds + 40; frames > max {
		t.Errorf("120fps source streamed %d frames, want at most %d", frames, max)
	}
	if min := 100*seconds - 30; frames < min {
		t.Errorf("120fps source streamed %d frames in %v, want at least %d", frames, elapsed, min)
	}
	if elapsed < time.Second || elapsed > 8*time.Second {
		t.Errorf("2s clip played in %v, playback is not paced in realtime", elapsed)
	}

	frames, _ = playClip(t, makeTestVideo(t, 60, seconds))
	if min := 60*seconds - 20; frames < min {
		t.Errorf("60fps source streamed %d frames, want about %d", frames, 60*seconds)
	}
}

func TestVideoPlayerKeepsLatestFrame(t *testing.T) {
	vp, err := startVideoPlayer(videoPost(makeTestVideo(t, 30, 3)), 80, 24, videoOptions{})
	if err != nil {
		t.Fatalf("startVideoPlayer: %v", err)
	}
	defer vp.stop()

	time.Sleep(1500 * time.Millisecond)
	if got := len(vp.frames); got == 0 {
		t.Fatal("no frame held for the renderer to pick up")
	}
}

func TestVideoAudioIsOffByDefault(t *testing.T) {
	vp, err := startVideoPlayer(videoPost(makeTestVideo(t, 30, 2)), 80, 24, videoOptions{})
	if err != nil {
		t.Fatalf("startVideoPlayer: %v", err)
	}
	defer vp.stop()

	time.Sleep(1200 * time.Millisecond)
	if !vp.isMuted() {
		t.Error("audio should be off unless it was requested")
	}
	if vp.audioProcess() != nil {
		t.Error("audio process started although audio is off")
	}
}

func TestVideoAudioToggle(t *testing.T) {
	if _, err := exec.LookPath("ffplay"); err != nil {
		t.Skip("ffplay not installed")
	}
	vp, err := startVideoPlayer(videoPost(makeTestVideo(t, 30, 2)), 80, 24, videoOptions{audio: true})
	if err != nil {
		t.Fatalf("startVideoPlayer: %v", err)
	}
	defer vp.stop()

	deadline := time.Now().Add(3 * time.Second)
	for vp.audioProcess() == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if vp.audioProcess() == nil {
		t.Fatal("audio player did not start")
	}

	vp.toggleMute()
	if !vp.isMuted() || vp.audioProcess() != nil {
		t.Error("toggling audio off should stop the audio player")
	}
	vp.toggleMute()
	if vp.isMuted() {
		t.Error("toggling audio on should enable it again")
	}
	if vp.audioProcess() == nil {
		t.Error("audio player did not restart after unmuting")
	}
	vp.stop()
	if vp.audioProcess() != nil {
		t.Error("audio player survived playback")
	}
}

func TestVideoFrameKeepsDisplayedImageAlive(t *testing.T) {
	vp := &videoPlayer{}
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	first, ok := renderVideoFrame(vp, img, 80, 24)().(videoRenderedMsg)
	if !ok {
		t.Fatal("renderVideoFrame did not return a rendered frame")
	}
	if !strings.Contains(first.s, kittyGraphicsPrefix) {
		t.Skip("terminal graphics encoder is not kitty")
	}
	second, ok := renderVideoFrame(vp, img, 80, 24)().(videoRenderedMsg)
	if !ok {
		t.Fatal("renderVideoFrame did not return a rendered frame")
	}
	firstID, secondID := kittyFrameID(t, first.s), kittyFrameID(t, second.s)
	if firstID == secondID {
		t.Fatalf("consecutive frames reused image id %d, re-transmitting a live id blanks the frame", firstID)
	}
	if strings.Contains(first.s, kittyDeleteImage(firstID)) {
		t.Error("a frame must not drop the image it just placed")
	}
	if !strings.Contains(second.s, kittyDeleteImage(firstID)) {
		t.Errorf("second frame should drop the image it replaced (%d)", firstID)
	}
	if strings.Contains(first.s, "\x1b_Ga=d,i=") {
		t.Error("delete must target the image id with d=I, d defaults to wiping every placement")
	}
}

func kittyFrameID(t *testing.T, frame string) int {
	t.Helper()
	const key = "i="
	start := strings.Index(frame, key)
	if start < 0 {
		t.Fatalf("frame %q has no image id", frame[:40])
	}
	rest := frame[start+len(key):]
	end := strings.IndexByte(rest, ',')
	if semi := strings.IndexByte(rest, ';'); semi >= 0 && (end < 0 || semi < end) {
		end = semi
	}
	if end < 0 {
		t.Fatalf("frame %q has a malformed image id", frame[:40])
	}
	id, err := strconv.Atoi(rest[:end])
	if err != nil {
		t.Fatalf("frame image id: %v", err)
	}
	return id
}

func TestVideoFrameIDsStayUnique(t *testing.T) {
	vp := &videoPlayer{}
	seen := map[int]bool{}
	placed := []int{}
	for i := 0; i < 500; i++ {
		id, stale := vp.kittyFrameIDs()
		if id <= 0 {
			t.Fatalf("frame %d got invalid image id %d", i, id)
		}
		if seen[id] {
			t.Fatalf("frame %d reused image id %d, re-transmitting a live id blanks the frame", i, id)
		}
		seen[id] = true
		placed = append(placed, id)
		for _, old := range stale {
			if old == id {
				t.Fatalf("frame %d drops the image it is about to place", i)
			}
		}
	}
	if len(placed) != 500 {
		t.Fatalf("placed %d frames, want 500", len(placed))
	}
}

func TestVideoFrameCleansUpSkippedFrames(t *testing.T) {
	vp := &videoPlayer{}
	first, _ := vp.kittyFrameIDs()
	second, _ := vp.kittyFrameIDs()
	_, stale := vp.kittyFrameIDs()
	dropped := map[int]bool{}
	for _, id := range stale {
		dropped[id] = true
	}
	if !dropped[first] || !dropped[second] {
		t.Errorf("stale ids %v should drop the skipped frames %d and %d", stale, first, second)
	}
}

func TestVideoFrameViewKeepsFrameLast(t *testing.T) {
	frame, err := encodeVideoFrame(image.NewRGBA(image.Rect(0, 0, 64, 32)), 2, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(frame, kittyGraphicsPrefix) {
		t.Skip("terminal graphics encoder is not kitty")
	}
	view := videoFrameView(frame, "status")
	body := strings.TrimSuffix(frame, "\n")
	if !strings.HasSuffix(view, body) {
		t.Error("view must draw the frame last")
	}
	if !strings.HasPrefix(view, "status\n") {
		t.Error("view must show the status line above the frame")
	}
	if head := strings.TrimSuffix(view, body); strings.Count(head, "\n") != 1 {
		t.Errorf("header %q must be one status line, anything below the frame scrolls it away", head)
	}
}

func TestVideoFrameViewKeepsTextLayoutForOtherEncoders(t *testing.T) {
	if got := videoFrameView("image\n", "status"); got != "image\n\nstatus" {
		t.Errorf("text layout = %q, want %q", got, "image\n\nstatus")
	}
}

func TestVideoFitHasNoLetterbox(t *testing.T) {
	cols, rows := videoBox(80, 24)
	boxW, boxH := cols*cellW, rows*cellH
	w, h := videoFitSize(80, 24, 640, 360)
	if w > boxW || h > boxH {
		t.Errorf("fit %dx%d exceeds the preview box %dx%d", w, h, boxW, boxH)
	}
	if w != boxW && h != boxH {
		t.Errorf("fit %dx%d should fill the preview box %dx%d", w, h, boxW, boxH)
	}
	if ratio := float64(w) / float64(h); ratio < 1.75 || ratio > 1.81 {
		t.Errorf("fit %dx%d distorts the 640x360 aspect", w, h)
	}

	w, h = videoFitSize(80, 24, 0, 0)
	if w > boxW || h > boxH {
		t.Errorf("16:9 fallback %dx%d exceeds the preview box %dx%d", w, h, boxW, boxH)
	}
	if w != boxW && h != boxH {
		t.Errorf("16:9 fallback %dx%d should fill the preview box %dx%d", w, h, boxW, boxH)
	}
	if ratio := float64(w) / float64(h); ratio < 1.75 || ratio > 1.81 {
		t.Errorf("16:9 fallback %dx%d is not 16:9", w, h)
	}
	if w%2 != 0 || h%2 != 0 {
		t.Errorf("fit %dx%d must stay even for the scale/pad chain", w, h)
	}
}

func TestVideoBoxIsBigger(t *testing.T) {
	for _, term := range []struct {
		cols, rows int
	}{{80, 24}, {100, 30}, {200, 60}} {
		cols, rows := videoBox(term.cols, term.rows)
		baseCols, baseRows := previewCols(term.cols), previewRows(term.rows)
		if cols <= baseCols || rows <= baseRows {
			t.Errorf("video box %dx%d is not bigger than the preview box %dx%d", cols, rows, baseCols, baseRows)
		}
		if cols > term.cols-2 || rows > term.rows-2 {
			t.Errorf("video box %dx%d does not fit terminal %dx%d", cols, rows, term.cols, term.rows)
		}
		growth := float64(cols) / float64(baseCols)
		if growth < 1.15 || growth > 1.35 {
			t.Errorf("video width grew %.3fx at %dx%d, want about 1.25x", growth, term.cols, term.rows)
		}
	}
}

func TestImageBoxIsBigger(t *testing.T) {
	for _, term := range []struct {
		cols, rows int
	}{{80, 24}, {100, 30}, {200, 60}} {
		cols, rows := imageBox(term.cols, term.rows)
		baseCols, baseRows := previewCols(term.cols), previewRows(term.rows)
		if cols <= baseCols || rows <= baseRows {
			t.Errorf("image box %dx%d is not bigger than the preview box %dx%d", cols, rows, baseCols, baseRows)
		}
		if cols > term.cols-2 || rows > term.rows-2 {
			t.Errorf("image box %dx%d does not fit terminal %dx%d", cols, rows, term.cols, term.rows)
		}
		growth := float64(cols) / float64(baseCols)
		if growth < 1.1 || growth > 1.3 {
			t.Errorf("image width grew %.3fx at %dx%d, want about 1.2x", growth, term.cols, term.rows)
		}
	}
}

func TestPreviewBoxStaysModest(t *testing.T) {
	if cols := previewCols(100); cols < 70 || cols > 80 {
		t.Errorf("previewCols(100) = %d, want a modest share of the width", cols)
	}
	if rows := previewRows(30); rows < 18 || rows > 24 {
		t.Errorf("previewRows(30) = %d, want a modest share of the height", rows)
	}
	if cols := previewCols(300); cols != previewMaxCols {
		t.Errorf("huge terminals should cap the preview width, got %d", cols)
	}
	if rows := previewRows(80); rows != previewMaxRows {
		t.Errorf("huge terminals should cap the preview height, got %d", rows)
	}
	if cols := previewCols(20); cols < 8 {
		t.Errorf("tiny terminals should still get a preview, got %d", cols)
	}
	if rows := previewRows(6); rows < 4 {
		t.Errorf("tiny terminals should still get a preview, got %d", rows)
	}

	videoCols, videoRows := videoBox(300, 80)
	w, h := videoFitSize(300, 80, 1920, 1080)
	if w > videoCols*cellW || h > videoRows*cellH {
		t.Errorf("fit %dx%d is bigger than the capped preview box", w, h)
	}
	if diff := w*1080 - h*1920; diff > 1920 || diff < -1920 {
		t.Errorf("fit %dx%d distorts the 1920x1080 aspect", w, h)
	}
}

func TestParseSourceInfo(t *testing.T) {
	info := parseSourceInfo("width=1920\nheight=1080\navg_frame_rate=30/1\nr_frame_rate=30000/1001\n")
	if info.width != 1920 || info.height != 1080 {
		t.Errorf("dims = %dx%d, want 1920x1080", info.width, info.height)
	}
	if info.fps < 29.9 || info.fps > 30.1 {
		t.Errorf("fps = %v, want about 30", info.fps)
	}
	if empty := parseSourceInfo(""); empty.width != 0 || empty.fps != 0 {
		t.Errorf("empty probe = %+v, want zeroes", empty)
	}
	avg := parseSourceInfo("avg_frame_rate=60/1\nr_frame_rate=120/1\n")
	if avg.fps != 60 {
		t.Errorf("avg fps = %v, want 60", avg.fps)
	}
	fallback := parseSourceInfo("avg_frame_rate=0/0\nr_frame_rate=120/1\n")
	if fallback.fps != 120 {
		t.Errorf("fallback fps = %v, want 120", fallback.fps)
	}
}

func countVideoFrames(t *testing.T, opts videoOptions, fps int) int {
	t.Helper()
	clip := makeTestVideo(t, fps, 1)
	vp, err := startVideoPlayer(videoPost(clip), 80, 24, opts)
	if err != nil {
		t.Fatalf("startVideoPlayer: %v", err)
	}
	defer vp.stop()

	frames := 0
	deadline := time.After(20 * time.Second)
	for {
		select {
		case _, ok := <-vp.frames:
			if !ok {
				return frames
			}
			frames++
		case <-deadline:
			t.Fatalf("timed out after %d frames", frames)
		}
	}
}

func TestVideoPlaysAtSourceFramerate(t *testing.T) {
	for _, fps := range []int{24, 30, 60} {
		frames := countVideoFrames(t, videoOptions{}, fps)
		low, high := fps*3/4, fps*5/4+2
		if frames < low || frames > high {
			t.Errorf("a %dfps one second clip produced %d frames, want between %d and %d", fps, frames, low, high)
		}
	}
}

func TestVideoPlayerReportsSourceFramerate(t *testing.T) {
	vp, err := startVideoPlayer(videoPost(makeTestVideo(t, 30, 1)), 80, 24, videoOptions{})
	if err != nil {
		t.Fatalf("startVideoPlayer: %v", err)
	}
	defer vp.stop()

	deadline := time.Now().Add(5 * time.Second)
	for vp.fpsLabel() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if label := vp.fpsLabel(); label != "30fps" {
		t.Errorf("fps label = %q, want 30fps", label)
	}
	if label := (&videoPlayer{fps: 29.97}).fpsLabel(); label != "29.97fps" {
		t.Errorf("29.97 fps label = %q, want 29.97fps", label)
	}
	if label := (&videoPlayer{}).fpsLabel(); label != "" {
		t.Errorf("unknown fps label = %q, want empty", label)
	}
}

func withProbeSource(t *testing.T, fn func(string, string) sourceInfo) {
	t.Helper()
	probeSourceMu.Lock()
	original := probeSourceFn
	probeSourceFn = fn
	probeSourceMu.Unlock()
	t.Cleanup(func() {
		probeSourceMu.Lock()
		probeSourceFn = original
		probeSourceMu.Unlock()
	})
}

func TestVideoPlayerStartsBeforeProbeFinishes(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	withProbeSource(t, func(string, string) sourceInfo {
		<-release
		return sourceInfo{fps: 30, width: 640, height: 360}
	})

	start := time.Now()
	vp, err := startVideoPlayer(videoPost(makeTestVideo(t, 30, 2)), 80, 24, videoOptions{})
	if err != nil {
		t.Fatalf("startVideoPlayer: %v", err)
	}
	defer vp.stop()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("startVideoPlayer took %v, playback must not wait for the stream probe", elapsed)
	}
	select {
	case <-vp.frames:
	case <-time.After(3 * time.Second):
		t.Fatal("no frame arrived while the stream probe was still running")
	}
	if label := vp.fpsLabel(); label != "" {
		t.Errorf("fps label = %q before the probe returned, want it empty", label)
	}

	finish()
	deadline := time.Now().Add(3 * time.Second)
	for vp.fpsLabel() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if label := vp.fpsLabel(); label != "30fps" {
		t.Errorf("fps label = %q after the probe returned, want 30fps", label)
	}
}

func TestVideoPlaybackNeverFakesFrames(t *testing.T) {
	vp, err := startVideoPlayer(videoPost(makeTestVideo(t, 30, 1)), 80, 24, videoOptions{})
	if err != nil {
		t.Fatalf("startVideoPlayer: %v", err)
	}
	defer vp.stop()

	args := strings.Join(vp.cmd.Args, " ")
	for _, unwanted := range []string{"minterpolate", "fps=", "-r "} {
		if strings.Contains(args, unwanted) {
			t.Errorf("ffmpeg args %q contain %q, playback must keep the source frames", args, unwanted)
		}
	}
}

func TestAudioFilterArgs(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	args := audioFilterArgs()
	if len(args) != 2 || args[0] != "-af" || args[1] == "" {
		t.Fatalf("audio filter args = %v", args)
	}
}

func TestMuteKeyTogglesAudioPreference(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newListModel()
	m.state = stateVideoPlaying
	m.video = &videoPlayer{done: make(chan struct{}), muted: true}

	updated, _ := m.Update(keyPress('m'))
	m = updated.(Model)
	if m.video.isMuted() || !m.cfg.AudioEnabled {
		t.Error("pressing m should turn audio on for a muted player")
	}

	updated, _ = m.Update(keyPress('m'))
	m = updated.(Model)
	if !m.video.isMuted() || m.cfg.AudioEnabled {
		t.Error("pressing m again should turn audio off")
	}
	if m.state != stateVideoPlaying {
		t.Error("mute key should not close the player")
	}
}

func TestGIFKeepsLooping(t *testing.T) {
	if !isGIFURL("https://example.org/a/b.gif?x=1") {
		t.Fatal("gif url with query string should be detected")
	}
	post := api.Post{Image: "loop.gif", FileURL_: testMediaURL(makeTestGIF(t, 1))}
	vp, err := startVideoPlayer(post, 80, 24, videoOptions{})
	if err != nil {
		t.Fatalf("startVideoPlayer: %v", err)
	}
	defer vp.stop()

	start := time.Now()
	frames := 0
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-vp.frames:
			if !ok {
				t.Fatalf("gif stopped after %d frames in %v, want it to loop", frames, time.Since(start))
			}
			frames++
		case <-deadline:
			if frames < 10 {
				t.Fatalf("only %d frames in %v", frames, time.Since(start))
			}
			return
		}
	}
}

func TestVideoViewerRendersEveryFrame(t *testing.T) {
	vp, err := startVideoPlayer(videoPost(makeTestVideo(t, 30, 3)), 80, 24, videoOptions{})
	if err != nil {
		t.Fatalf("startVideoPlayer: %v", err)
	}
	defer vp.stop()

	m := NewModel(testClients(stubClient{}), conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "", 30)
	m.width, m.height = 80, 24
	m.viewerPost = api.Post{ID: 1, Image: "1.mp4"}
	m.state = stateVideoPlaying
	m.video = vp

	pending := []tea.Cmd{nextFrame(vp)}
	rendered := map[string]bool{}
	done := false
	deadline := time.Now().Add(30 * time.Second)

	for len(pending) > 0 && time.Now().Before(deadline) {
		cmd := pending[0]
		pending = pending[1:]
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			pending = append(pending, batch...)
			continue
		}
		updated, next := m.Update(msg)
		m = updated.(Model)
		if next != nil {
			pending = append(pending, next)
		}
		switch msg := msg.(type) {
		case videoRenderedMsg:
			rendered[msg.s] = true
			if !strings.Contains(m.View(), strings.TrimSuffix(msg.s, "\n")) {
				t.Fatal("rendered frame is not what the viewer shows")
			}
		case videoDoneMsg:
			if msg.err != nil {
				t.Fatalf("playback error: %v", msg.err)
			}
			done = true
		}
	}

	if !done {
		t.Fatal("playback never finished")
	}
	if len(rendered) < 20 {
		t.Fatalf("viewer rendered %d distinct frames, expected continuous playback", len(rendered))
	}
	if m.video != nil {
		t.Error("player was not released after playback")
	}
	if m.state != stateList {
		t.Errorf("state = %d after playback, want list", m.state)
	}
}

func keyPress(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func newListModel() Model {
	m := NewModel(testClients(stubClient{}), conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "", 30)
	m.width, m.height = 80, 24
	m.state = stateList
	m.posts = []api.Post{{ID: 1, Image: "1.mp4", FileURL_: "/does/not/exist.mp4"}}
	return m
}

func TestPreviewKeyRepeatDoesNotRestartPlayback(t *testing.T) {
	m := newListModel()
	updated, cmd := m.Update(keyPress('p'))
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("first preview press should open the viewer")
	}
	for i := 0; i < 20; i++ {
		updated, cmd := m.Update(keyPress('p'))
		m = updated.(Model)
		if cmd != nil {
			t.Fatalf("held key press %d started another preview", i+1)
		}
	}

	m.state = stateVideoPlaying
	m.video = &videoPlayer{done: make(chan struct{})}
	updated, _ = m.Update(keyPress('p'))
	m = updated.(Model)
	if m.state != stateVideoPlaying {
		t.Error("held preview key stopped the running video")
	}
}

func TestOnlyOneVideoPlayerAtATime(t *testing.T) {
	m := newListModel()
	m.state = stateVideoPlaying
	running := &videoPlayer{done: make(chan struct{})}
	m.video = running

	extra := &videoPlayer{done: make(chan struct{})}
	updated, _ := m.Update(videoStartedMsg{vp: extra})
	m = updated.(Model)
	if m.video != running {
		t.Error("a second player replaced the running one")
	}
	if !extra.stopped() {
		t.Error("second player was left running")
	}

	m.state = stateList
	m.video = nil
	late := &videoPlayer{done: make(chan struct{})}
	m.Update(videoStartedMsg{vp: late})
	if !late.stopped() {
		t.Error("player started after the viewer closed was left running")
	}
}

func TestHeldPreviewKeyKeepsOnePlayer(t *testing.T) {
	clip := makeTestVideo(t, 30, 3)
	m := newListModel()
	m.posts = []api.Post{{ID: 1, Image: "1.mp4", FileURL_: testMediaURL(clip)}}

	msgs := make(chan tea.Msg, 64)
	exec := func(cmd tea.Cmd) {
		if cmd != nil {
			go func() { msgs <- cmd() }()
		}
	}
	drain := func() {
		for {
			select {
			case msg := <-msgs:
				if batch, ok := msg.(tea.BatchMsg); ok {
					for _, c := range batch {
						exec(c)
					}
					continue
				}
				updated, next := m.Update(msg)
				m = updated.(Model)
				exec(next)
			default:
				return
			}
		}
	}
	players := map[*videoPlayer]bool{}

	for i := 0; i < 20; i++ {
		updated, cmd := m.Update(keyPress('p'))
		m = updated.(Model)
		exec(cmd)
		drain()
		if m.video != nil {
			players[m.video] = true
		}
		time.Sleep(30 * time.Millisecond)
	}
	deadline := time.Now().Add(5 * time.Second)
	for m.video == nil && time.Now().Before(deadline) {
		drain()
		time.Sleep(10 * time.Millisecond)
	}

	if len(players) != 1 {
		t.Fatalf("held preview key ran %d players, want 1", len(players))
	}
	if m.state != stateVideoPlaying {
		t.Errorf("state = %v after holding preview key, want playing", m.state)
	}
	m.stopVideo()
}
