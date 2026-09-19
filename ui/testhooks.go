package ui

import (
	"image"
	"os/exec"

	"moxiu/r34-dl/api"

	tea "github.com/charmbracelet/bubbletea"
)

type State = state

const (
	StateAgeGate       = stateAgeGate
	StateSearch        = stateSearch
	StateSearching     = stateSearching
	StateList          = stateList
	StateDownloading   = stateDownloading
	StateDone          = stateDone
	StateViewerLoading = stateViewerLoading
	StateViewer        = stateViewer
	StateVideoPlaying  = stateVideoPlaying
)

type VideoPlayer = videoPlayer
type VideoOptions = videoOptions
type VideoStartedMsg = videoStartedMsg
type VideoFrameMsg = videoFrameMsg
type VideoRenderedMsg = videoRenderedMsg
type VideoDoneMsg = videoDoneMsg
type SuggestionsMsg = suggestionsMsg

const KittyGraphicsPrefix = kittyGraphicsPrefix

func StartVideoPlayer(post api.Post, termCols, termRows int, opts VideoOptions) (*VideoPlayer, error) {
	return startVideoPlayer(post, termCols, termRows, opts)
}

func RenderVideoFrame(vp *VideoPlayer, img image.Image, termCols, termRows int) tea.Cmd {
	return renderVideoFrame(vp, img, termCols, termRows)
}

func NextFrame(vp *VideoPlayer) tea.Cmd { return nextFrame(vp) }

func VideoFitSize(termCols, termRows, srcW, srcH int) (int, int) {
	return videoFitSize(termCols, termRows, srcW, srcH)
}

func VideoRateFilter(rate float64) string { return videoRateFilter(rate) }

func AudioFilterArgs() []string { return audioFilterArgs() }

func IsGIFURL(rawURL string) bool { return isGIFURL(rawURL) }

func KittyTransmit(id int) string { return kittyTransmit(id) }

func KittyDeleteImage(id int) string { return kittyDeleteImage(id) }

func NewVideoPlayer(done chan struct{}, muted bool) *VideoPlayer {
	return &videoPlayer{done: done, muted: muted}
}

func (vp *videoPlayer) Frames() <-chan image.Image { return vp.frames }
func (vp *videoPlayer) Width() int                 { return vp.width }
func (vp *videoPlayer) Height() int                { return vp.height }
func (vp *videoPlayer) Stop()                      { vp.stop() }
func (vp *videoPlayer) Err() error                 { return vp.takeErr() }
func (vp *videoPlayer) IsMuted() bool              { return vp.isMuted() }
func (vp *videoPlayer) AudioProcess() *exec.Cmd    { return vp.audioProcess() }
func (vp *videoPlayer) ToggleMute()                { vp.toggleMute() }
func (vp *videoPlayer) Stopped() bool              { return vp.stopped() }

func (m Model) State() State                 { return m.state }
func (m *Model) SetState(s State)            { m.state = s }
func (m *Model) SetSize(w, h int)            { m.width, m.height = w, h }
func (m *Model) SwitchAPI(name string)       { m.switchAPI(name) }
func (m Model) ActiveAPI() string            { return m.cfg.ActiveAPI }
func (m Model) AllowedSites() []string       { return m.allowedSites() }
func (m *Model) SetPosts(posts []api.Post)   { m.posts = posts }
func (m *Model) SetViewerPost(post api.Post) { m.viewerPost = post }
func (m *Model) SetVideo(vp *VideoPlayer)    { m.video = vp }
func (m Model) Video() *VideoPlayer          { return m.video }
func (m *Model) StopVideo()                  { m.stopVideo() }
func (m Model) Query() string                { return m.query }
func (m Model) InputCursor() int             { return m.inputCursor }
func (m Model) Suggestions() []string        { return m.suggestions }
func (m Model) SuggestWord() string          { return m.suggestWord }
func (m Model) SuggestDash() bool            { return m.suggestDash }
func (m Model) SuggestPick() bool            { return m.suggestPick }
func (m Model) SuggestIdx() int              { return m.suggestIdx }
func (m Model) SuggestGen() int              { return m.suggestGen }
func (m Model) GhostSuggestion() string      { return m.ghostSuggestion() }
