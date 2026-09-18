package ui

import (
	"fmt"
	"image"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"

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
	path := filepath.Join(t.TempDir(), "clip.mp4")
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
	path := filepath.Join(t.TempDir(), "loop.gif")
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
	return api.Post{Image: filepath.Base(path), FileURL_: path}
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

func TestVideoPlayerCapsFramerateAt60(t *testing.T) {
	const seconds = 2

	frames, elapsed := playClip(t, makeTestVideo(t, 120, seconds))
	if max := 60*seconds + 20; frames > max {
		t.Errorf("120fps source streamed %d frames, want at most %d", frames, max)
	}
	if frames < 30 {
		t.Errorf("only %d frames in %v, playback is stalling", frames, elapsed)
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

func TestVideoFrameSwapsKittyImage(t *testing.T) {
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

	if !strings.Contains(first.s, kittyTransmit(2)) || !strings.Contains(first.s, kittyDeleteImage(1)) {
		t.Errorf("first frame should draw image 2 and drop image 1")
	}
	if !strings.Contains(second.s, kittyTransmit(1)) || !strings.Contains(second.s, kittyDeleteImage(2)) {
		t.Errorf("second frame should draw image 1 and drop image 2")
	}
	if strings.Contains(first.s, "\x1b_Ga=d,i=") {
		t.Error("delete must target the image id with d=I, d defaults to wiping every placement")
	}
}

func TestVideoFitHasNoLetterbox(t *testing.T) {
	w, h := videoFitSize(80, 24, 640, 360)
	if w > previewCols(80)*cellW || h > previewRows(24)*cellH {
		t.Errorf("fit %dx%d exceeds the preview box", w, h)
	}
	if diff := w*360 - h*640; diff > 640 || diff < -640 {
		t.Errorf("fit %dx%d distorts the 640x360 aspect", w, h)
	}
	if h != previewRows(24)*cellH {
		t.Errorf("wide 640x360 source should fill the box height, got %d", h)
	}

	w, h = videoFitSize(80, 24, 0, 0)
	boxW, boxH := previewCols(80)*cellW, previewRows(24)*cellH
	if w > boxW || h > boxH {
		t.Errorf("16:9 fallback %dx%d exceeds the preview box %dx%d", w, h, boxW, boxH)
	}
	if h != boxH {
		t.Errorf("16:9 fallback should fill the box height, got %d want %d", h, boxH)
	}
	if diff := w*9 - h*16; diff > 16 || diff < -16 {
		t.Errorf("16:9 fallback %dx%d is not 16:9", w, h)
	}
	if w%2 != 0 || h%2 != 0 {
		t.Errorf("fit %dx%d must stay even for the scale/pad chain", w, h)
	}
}

func TestPreviewBoxStaysModest(t *testing.T) {
	if cols := previewCols(100); cols < 55 || cols > 75 {
		t.Errorf("previewCols(100) = %d, want a modest share of the width", cols)
	}
	if rows := previewRows(30); rows < 14 || rows > 19 {
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

	w, h := videoFitSize(300, 80, 1920, 1080)
	if w > previewMaxCols*cellW || h > previewMaxRows*cellH {
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
}

func TestVideoRateFilter(t *testing.T) {
	on, off := true, false
	cases := []struct {
		rate   float64
		smooth *bool
		want   string
	}{
		{rate: 120, want: "fps=60"},
		{rate: 60, want: ""},
		{rate: 59.94, want: ""},
		{rate: 30, want: "minterpolate="},
		{rate: 24, want: "minterpolate="},
		{rate: 30, smooth: &on, want: "minterpolate="},
		{rate: 30, smooth: &off, want: ""},
		{rate: 0, want: "fps=60"},
	}
	for _, c := range cases {
		got := videoRateFilter(c.rate, c.smooth)
		if c.want == "" && got != "" {
			t.Errorf("rate %v -> %q, want none", c.rate, got)
			continue
		}
		if c.want != "" && !strings.HasPrefix(got, c.want) {
			t.Errorf("rate %v -> %q, want prefix %q", c.rate, got, c.want)
		}
	}
}

func countVideoFrames(t *testing.T, opts videoOptions) int {
	t.Helper()
	clip := makeTestVideo(t, 30, 1)
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

func TestVideoIsSmoothByDefault(t *testing.T) {
	if frames := countVideoFrames(t, videoOptions{}); frames < 45 {
		t.Errorf("a 30fps clip produced %d frames by default, want it smoothed to about 60", frames)
	}
}

func TestVideoInterpolationCanBeDisabled(t *testing.T) {
	off := false
	if frames := countVideoFrames(t, videoOptions{smooth: &off}); frames > 40 {
		t.Errorf("a 30fps clip produced %d frames with interpolation off, want about 30", frames)
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
	post := api.Post{Image: "loop.gif", FileURL_: makeTestGIF(t, 1)}
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
			if !strings.Contains(m.View(), msg.s) {
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
	m.posts = []api.Post{{ID: 1, Image: "1.mp4", FileURL_: clip}}

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
