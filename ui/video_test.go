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
	vp, err := startVideoPlayer(videoPost(path), 80, 24, false)
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
	vp, err := startVideoPlayer(videoPost(makeTestVideo(t, 30, 3)), 80, 24, false)
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
	vp, err := startVideoPlayer(videoPost(makeTestVideo(t, 30, 2)), 80, 24, false)
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
	vp, err := startVideoPlayer(videoPost(makeTestVideo(t, 30, 2)), 80, 24, true)
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

func TestVideoFrameReusesKittyImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	msg := renderVideoFrame(&videoPlayer{}, img, 80, 24)()
	rendered, ok := msg.(videoRenderedMsg)
	if !ok {
		t.Fatalf("renderVideoFrame returned %T", msg)
	}
	if !strings.Contains(rendered.s, kittyGraphicsPrefix) {
		t.Skip("terminal graphics encoder is not kitty")
	}
	if !strings.Contains(rendered.s, kittyImageIDPrefix) {
		t.Error("kitty frames should reuse a stable image id to avoid flashing")
	}
}

func TestVideoFitHasNoLetterbox(t *testing.T) {
	w, h := videoFitSize(80, 24, 640, 360)
	if w > 640 || h > 256 {
		t.Errorf("fit %dx%d exceeds the 640x256 preview box", w, h)
	}
	if diff := w*360 - h*640; diff > 640 || diff < -640 {
		t.Errorf("fit %dx%d distorts the 640x360 aspect", w, h)
	}
	if h != 256 {
		t.Errorf("wide 640x360 source should fill the box height, got %d", h)
	}

	w, h = videoFitSize(80, 24, 0, 0)
	if w != 640 || h != 256 {
		t.Errorf("unknown source size should fall back to the 640x256 box, got %dx%d", w, h)
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

func TestGIFPlaysOnce(t *testing.T) {
	if !isGIFURL("https://example.org/a/b.gif?x=1") {
		t.Fatal("gif url with query string should be detected")
	}
	frames, elapsed := playClip(t, makeTestGIF(t, 2))
	if frames == 0 {
		t.Fatal("gif produced no frames")
	}
	if elapsed > 6*time.Second {
		t.Errorf("looping gif played for %v, want a single pass", elapsed)
	}
}

func TestVideoViewerRendersEveryFrame(t *testing.T) {
	vp, err := startVideoPlayer(videoPost(makeTestVideo(t, 30, 3)), 80, 24, false)
	if err != nil {
		t.Fatalf("startVideoPlayer: %v", err)
	}
	defer vp.stop()

	m := NewModel(stubClient{}, stubClient{}, conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "", 30)
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
	m := NewModel(stubClient{}, stubClient{}, conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "", 30)
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
