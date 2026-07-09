package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
	"moxiu/r34-dl/dl"

	"github.com/kenshaw/rasterm"
	xdraw "golang.org/x/image/draw"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type state int

const (
	stateAgeGate state = iota
	stateSearch
	stateSearching
	stateList
	stateDownloading
	stateDone
	stateViewerLoading
	stateViewer
	stateVideoPlaying
)

const maxTagsShown = 10
const suffixReserve = 14
const countRefreshInterval = 5 * time.Second
const previewHeightFraction = 0.75

type Model struct {
	cfg        conf.Config
	client     api.Client
	sbClient   api.Client
	r34Client  api.Client
	limit      int
	query      string
	err        string
	notice     string
	totalCount int
	totalKnown bool
	searchGen  int

	posts       []api.Post
	cursor      int
	offset      int
	width       int
	height      int
	page        int
	loadingMore bool
	noMore      bool

	state      state
	downloader *dl.Downloader
	results    <-chan dl.Result
	done       int
	failed     int
	total      int

	viewerPost    api.Post
	viewerErr     string
	viewerImage   string
	videoFrame    string
	video         *videoPlayer
	ageGateCursor int

	history    []string
	historyIdx int
}

// Cfg returns the current config. Used by tests to inspect state changes.
func (m Model) Cfg() conf.Config { return m.cfg }

func NewModel(sbClient, r34Client api.Client, cfg conf.Config, initialTags string, limit int) Model {
	active := sbClient
	if cfg.ActiveAPI == "rule34" && cfg.AgeVerified {
		active = r34Client
	}
	m := Model{
		cfg:        cfg,
		client:     active,
		sbClient:   sbClient,
		r34Client:  r34Client,
		limit:      limit,
		query:      initialTags,
		downloader: func() *dl.Downloader {
			home, err := os.UserHomeDir()
			if err != nil {
				home = "."
			}
			dlPath := filepath.Join(home, "r34-dl-downloads")
			_ = os.MkdirAll(dlPath, 0o755)
			return dl.New(dlPath, 4)
		}(),
		width:      100,
		height:     30,
		history:    cfg.SearchHistory,
		historyIdx: len(cfg.SearchHistory),
	}
	if !cfg.AgeVerified {
		m.state = stateAgeGate
	} else if initialTags != "" {
		m.state = stateSearching
	} else {
		m.state = stateSearch
	}
	return m
}

func (m Model) Init() tea.Cmd {
	if m.state == stateSearching {
		gen := m.searchGen
		return tea.Batch(
			doSearch(m.client, m.query, m.limit, 0),
			doCountTotal(m.client, m.query, gen),
			startCountTicker(gen),
		)
	}
	return nil
}


type searchResultMsg struct {
	posts []api.Post
	err   error
}

type loadMoreMsg struct {
	posts []api.Post
	page  int
	err   error
}

type totalCountMsg struct {
	count int
	err   error
	gen   int
}

type countTickMsg struct {
	gen int
}

type imageFetchedMsg struct {
	data []byte
	err  error
}

type imageRenderedMsg struct{ s string }

type singleDownloadMsg struct {
	path string
	err  error
}

type videoFrameMsg struct {
	frame image.Image
}

type videoDoneMsg struct {
	err error
}

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
}

func startVideoPlayer(rawURL string, termCols, termRows int) (*videoPlayer, error) {
	previewRows := int(float64(termRows)*previewHeightFraction) - 2
	if previewRows < 4 {
		previewRows = 4
	}
	pxW := termCols * 8
	pxH := previewRows * 16

	cmd := exec.Command(
		"ffmpeg",
		"-loglevel", "quiet",
		"-i", rawURL,
		"-vf", fmt.Sprintf("fps=10,scale=%d:%d:force_original_aspect_ratio=decrease", pxW, pxH),
		"-f", "image2pipe",
		"-vcodec", "png",
		"-",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &videoPlayer{cmd: cmd, stdout: stdout}, nil
}

func nextFrame(vp *videoPlayer) tea.Cmd {
	return func() tea.Msg {
		img, _, err := image.Decode(vp.stdout)
		if err != nil {
			_ = vp.cmd.Wait()
			return videoDoneMsg{}
		}
		return videoFrameMsg{frame: img}
	}
}


func doSearch(client api.Client, query string, limit, page int) tea.Cmd {
	return func() tea.Msg {
		posts, err := client.SearchPosts(query, limit, page)
		return searchResultMsg{posts: posts, err: err}
	}
}

func doLoadMore(client api.Client, query string, limit, page int) tea.Cmd {
	return func() tea.Msg {
		posts, err := client.SearchPosts(query, limit, page)
		return loadMoreMsg{posts: posts, page: page, err: err}
	}
}

func doCountTotal(client api.Client, query string, gen int) tea.Cmd {
	return func() tea.Msg {
		count, err := client.CountPosts(query)
		return totalCountMsg{count: count, err: err, gen: gen}
	}
}

func startCountTicker(gen int) tea.Cmd {
	return tea.Tick(countRefreshInterval, func(t time.Time) tea.Msg {
		return countTickMsg{gen: gen}
	})
}

func fetchImage(rawURL string) tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 30 * time.Second}
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("build request: %w", err)}
		}
		req.Header.Set("User-Agent", "r34-dl/viewer")
		resp, err := client.Do(req)
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("fetch: %w", err)}
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("read body: %w", err)}
		}
		return imageFetchedMsg{data: data}
	}
}

func scaleToFit(img image.Image, termCols, termRows int) image.Image {
	const cellW, cellH = 8, 16
	maxPxW := termCols * cellW
	maxPxH := termRows * cellH
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW == 0 || srcH == 0 {
		return img
	}
	if srcW <= maxPxW && srcH <= maxPxH {
		return img
	}
	scaleW := float64(maxPxW) / float64(srcW)
	scaleH := float64(maxPxH) / float64(srcH)
	scale := scaleW
	if scaleH < scaleW {
		scale = scaleH
	}
	dstW := int(float64(srcW) * scale)
	dstH := int(float64(srcH) * scale)
	if dstW < 1 {
		dstW = 1
	}
	if dstH < 1 {
		dstH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

func encodeImage(img image.Image) (string, error) {
	var buf strings.Builder
	err := rasterm.Encode(&buf, img)
	if err == nil {
		return buf.String(), nil
	}
	return halfBlockEncode(img)
}

func halfBlockEncode(img image.Image) (string, error) {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	var buf strings.Builder
	for y := b.Min.Y; y < b.Min.Y+height; y += 2 {
		for x := b.Min.X; x < b.Min.X+width; x++ {
			tr, tg, tb, _ := img.At(x, y).RGBA()
			var br, bg, bb uint32
			if y+1 < b.Min.Y+height {
				br, bg, bb, _ = img.At(x, y+1).RGBA()
			}
			fmt.Fprintf(&buf, "\033[38;2;%d;%d;%dm\033[48;2;%d;%d;%dm▄",
				br>>8, bg>>8, bb>>8,
				tr>>8, tg>>8, tb>>8,
			)
		}
		buf.WriteString("\033[0m\n")
	}
	return buf.String(), nil
}

func renderImage(data []byte, termCols, termRows int) tea.Cmd {
	return func() tea.Msg {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("decode image: %w", err)}
		}
		previewRows := int(float64(termRows)*previewHeightFraction) - 2
		if previewRows < 4 {
			previewRows = 4
		}
		scaled := scaleToFit(img, termCols, previewRows)
		s, err := encodeImage(scaled)
		if err != nil {
			return imageFetchedMsg{err: fmt.Errorf("render: %w", err)}
		}
		return imageRenderedMsg{s: s}
	}
}

func renderVideoFrame(img image.Image, termCols, termRows int) tea.Cmd {
	return func() tea.Msg {
		previewRows := int(float64(termRows)*previewHeightFraction) - 2
		if previewRows < 4 {
			previewRows = 4
		}
		scaled := scaleToFit(img, termCols, previewRows)
		s, err := encodeImage(scaled)
		if err != nil {
			return videoDoneMsg{err: fmt.Errorf("render frame: %w", err)}
		}
		return imageRenderedMsg{s: s}
	}
}

func downloadOne(d *dl.Downloader, post api.Post) tea.Cmd {
	return func() tea.Msg {
		ch := d.DownloadAll([]api.Post{post})
		r := <-ch
		if r.Err != nil {
			return singleDownloadMsg{err: r.Err}
		}
		return singleDownloadMsg{path: r.Path}
	}
}

type resultMsg dl.Result
type doneMsg struct{}

func waitForResult(ch <-chan dl.Result) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return doneMsg{}
		}
		return resultMsg(r)
	}
}


const reservedLines = 4

func (m Model) visibleRows() int {
	rows := m.height - reservedLines
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m *Model) clampViewport() tea.Cmd {
	if len(m.posts) == 0 {
		m.cursor = 0
		m.offset = 0
		return nil
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.posts)-1 {
		m.cursor = len(m.posts) - 1
	}
	var cmd tea.Cmd
	if m.cursor == len(m.posts)-1 && !m.loadingMore && !m.noMore {
		m.loadingMore = true
		nextPage := m.page + 1
		cmd = doLoadMore(m.client, m.query, m.limit, nextPage)
	}
	rows := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	maxOffset := len(m.posts) - rows
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
	if m.offset < 0 {
		m.offset = 0
	}
	return cmd
}

func (m *Model) switchAPI(name string) {
	m.cfg.ActiveAPI = name
	if name == "rule34" {
		m.client = m.r34Client
	} else {
		m.client = m.sbClient
	}
	_ = conf.Save(m.cfg)
}

func (m *Model) startSearch() tea.Cmd {
	m.searchGen++
	gen := m.searchGen
	m.state = stateSearching
	m.err = ""
	m.posts = nil
	m.cursor, m.offset, m.page = 0, 0, 0
	m.noMore = false
	m.totalKnown = false
	m.totalCount = 0
	return tea.Batch(
		doSearch(m.client, m.query, m.limit, 0),
		doCountTotal(m.client, m.query, gen),
		startCountTicker(gen),
	)
}


func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.clampViewport()
		return m, nil

	case tea.KeyMsg:
		switch m.state {

		case stateAgeGate:
			switch msg.String() {
			case "left", "h":
				m.ageGateCursor = 0
			case "right", "l":
				m.ageGateCursor = 1
			case "enter", " ":
				m.cfg.AgeVerified = true
				if m.ageGateCursor == 0 {
					m.cfg.ActiveAPI = "rule34"
					m.client = m.r34Client
				} else {
					m.cfg.ActiveAPI = "safebooru"
					m.client = m.sbClient
				}
				_ = conf.Save(m.cfg)
				m.state = stateSearch
			case "ctrl+c", "q":
				return m, tea.Quit
			}
			return m, nil

		case stateSearch:
			switch msg.Type {
			case tea.KeyCtrlC, tea.KeyEsc:
				return m, tea.Quit
			case tea.KeyEnter:
				if m.query != "" {
					if len(m.history) == 0 || m.history[len(m.history)-1] != m.query {
						m.history = append(m.history, m.query)
						m.cfg.SearchHistory = m.history
						_ = conf.Save(m.cfg)
					}
					m.historyIdx = len(m.history)
					return m, m.startSearch()
				}
			case tea.KeyUp:
				if len(m.history) > 0 && m.historyIdx > 0 {
					m.historyIdx--
					m.query = m.history[m.historyIdx]
				}
			case tea.KeyDown:
				if m.historyIdx < len(m.history)-1 {
					m.historyIdx++
					m.query = m.history[m.historyIdx]
				} else if m.historyIdx == len(m.history)-1 {
					m.historyIdx = len(m.history)
					m.query = ""
				}
			case tea.KeyBackspace:
				if len(m.query) > 0 {
					m.query = m.query[:len(m.query)-1]
				}
			case tea.KeySpace:
				m.query += " "
			case tea.KeyTab:
				if m.cfg.ActiveAPI == "rule34" {
					m.switchAPI("safebooru")
				} else {
					if m.cfg.AgeVerified {
						m.switchAPI("rule34")
					}
				}
			case tea.KeyRunes:
				m.query += string(msg.Runes)
			}
			return m, nil

		case stateList:
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "tab":
				if m.cfg.ActiveAPI == "rule34" {
					m.switchAPI("safebooru")
				} else if m.cfg.AgeVerified {
					m.switchAPI("rule34")
				}
				return m, m.startSearch()
			case "up", "k":
				m.cursor--
				return m, m.clampViewport()
			case "down", "j":
				m.cursor++
				return m, m.clampViewport()
			case "pgup":
				m.cursor -= m.visibleRows()
				return m, m.clampViewport()
			case "pgdown":
				m.cursor += m.visibleRows()
				return m, m.clampViewport()
			case "p":
				if len(m.posts) > 0 {
					m.viewerPost = m.posts[m.cursor]
					m.viewerErr = ""
					m.viewerImage = ""
					m.videoFrame = ""
					if isVideo(m.viewerPost) {
						m.state = stateViewerLoading
						fileURL := m.viewerPost.FileURL()
						w, h := m.width, m.height
						return m, func() tea.Msg {
							vp, err := startVideoPlayer(fileURL, w, h)
							if err != nil {
								return videoDoneMsg{err: err}
							}
							return struct{ vp *videoPlayer }{vp}
						}
					}
					m.state = stateViewerLoading
					return m, fetchImage(m.viewerPost.FileURL())
				}
			case "enter":
				if len(m.posts) > 0 {
					post := m.posts[m.cursor]
					m.notice = fmt.Sprintf("downloading #%d...", post.ID)
					return m, downloadOne(m.downloader, post)
				}
			case "/":
				m.state = stateSearch
				m.query = ""
				m.posts = nil
				m.cursor, m.offset, m.page = 0, 0, 0
				m.noMore = false
				m.totalKnown = false
				m.totalCount = 0
				m.historyIdx = len(m.history)
			}
			return m, nil

		case stateViewer:
			m.viewerImage = ""
			m.state = stateList
			return m, tea.ClearScreen
		
		case stateVideoPlaying:
			if m.video != nil {
				_ = m.video.cmd.Process.Kill()
				m.video = nil
			}
			m.videoFrame = ""
			m.state = stateList
			return m, tea.ClearScreen
		}

	case singleDownloadMsg:
		if msg.err != nil {
			m.notice = errorStyle.Render("download failed: " + msg.err.Error())
		} else {
			m.notice = noticeStyle.Render("saved → " + msg.path)
		}
		return m, nil

	case imageFetchedMsg:
		if msg.err != nil {
			m.viewerErr = msg.err.Error()
			m.state = stateViewer
			return m, nil
		}
		return m, renderImage(msg.data, m.width, m.height)

	case imageRenderedMsg:
		if m.state == stateVideoPlaying {
			m.videoFrame = msg.s
		} else {
			m.viewerImage = msg.s
			m.state = stateViewer
		}
		return m, nil

	case struct{ vp *videoPlayer }:
		if msg.vp == nil {
			m.viewerErr = "failed to start video player"
			m.state = stateViewer
			return m, nil
		}
		m.video = msg.vp
		m.state = stateVideoPlaying
		return m, nextFrame(m.video)

	case videoFrameMsg:
		if m.state != stateVideoPlaying || m.video == nil {
			return m, nil
		}
		w, h := m.width, m.height
		vp := m.video
		return m, tea.Batch(
			renderVideoFrame(msg.frame, w, h),
			nextFrame(vp),
		)

	case videoDoneMsg:
		if m.video != nil {
			_ = m.video.cmd.Process.Kill()
			m.video = nil
		}
		if msg.err != nil {
			m.viewerErr = msg.err.Error()
			m.state = stateViewer
		} else {
			m.state = stateList
		}
		return m, tea.ClearScreen

	case searchResultMsg:
		m.state = stateList
		m.cursor, m.offset, m.page = 0, 0, 0
		m.notice = ""
		if msg.err != nil {
			m.err = msg.err.Error()
			m.posts = nil
		} else {
			m.posts = msg.posts
			if len(msg.posts) < m.limit {
				m.noMore = true
			}
		}
		return m, nil

	case totalCountMsg:
		if msg.gen != m.searchGen {
			return m, nil
		}
		if msg.err == nil {
			m.totalCount = msg.count
			m.totalKnown = true
		}
		return m, nil

	case countTickMsg:
		if msg.gen != m.searchGen {
			return m, nil
		}
		if m.state != stateList {
			return m, nil
		}
		return m, tea.Batch(
			doCountTotal(m.client, m.query, msg.gen),
			startCountTicker(msg.gen),
		)

	case loadMoreMsg:
		m.loadingMore = false
		if msg.err != nil {
			m.notice = "couldn't load more: " + msg.err.Error()
			return m, nil
		}
		if len(msg.posts) == 0 {
			m.noMore = true
			m.notice = "no more results"
			return m, nil
		}
		m.posts = append(m.posts, msg.posts...)
		m.page = msg.page
		if len(msg.posts) < m.limit {
			m.noMore = true
		}
		m.notice = ""
		m.cursor++
		return m, m.clampViewport()

	case resultMsg:
		if msg.Err != nil {
			m.failed++
		} else {
			m.done++
		}
		return m, waitForResult(m.results)

	case doneMsg:
		m.state = stateDone
	}
	return m, nil
}


var (
	mochaMauve    = lipgloss.Color("#c6a0f6")
	mochaRed      = lipgloss.Color("#ed8796")
	mochaPeach    = lipgloss.Color("#f5a97f")
	mochaYellow   = lipgloss.Color("#eed49f")
	mochaGreen    = lipgloss.Color("#a6da95")
	mochaBlue     = lipgloss.Color("#8aadf4")
	mochaLavender = lipgloss.Color("#b7bdf8")
	mochaText     = lipgloss.Color("#cad3f5")
	mochaSubtext0 = lipgloss.Color("#a5adcb")
	mochaOverlay1 = lipgloss.Color("#8087a2")
	mochaOverlay0 = lipgloss.Color("#6e738d")
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(mochaMauve)
	idStyle       = lipgloss.NewStyle().Foreground(mochaPeach).Bold(true)
	tagStyle      = lipgloss.NewStyle().Foreground(mochaSubtext0)
	moreTagStyle  = lipgloss.NewStyle().Foreground(mochaOverlay1).Italic(true)
	selectedStyle = lipgloss.NewStyle().Foreground(mochaGreen).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(mochaOverlay0)
	errorStyle    = lipgloss.NewStyle().Foreground(mochaRed)
	noticeStyle   = lipgloss.NewStyle().Foreground(mochaYellow)
	inputStyle    = lipgloss.NewStyle().Foreground(mochaText)
	cursorStyle   = lipgloss.NewStyle().Foreground(mochaLavender)
	scrollStyle   = lipgloss.NewStyle().Foreground(mochaBlue)
	apiR34Style   = lipgloss.NewStyle().Foreground(mochaRed).Bold(true)
	apiSBStyle    = lipgloss.NewStyle().Foreground(mochaGreen).Bold(true)
)

func renderPostLine(p api.Post, width int, selected bool) string {
	idPart := fmt.Sprintf("#%d", p.ID)
	prefix := "  "
	if selected {
		prefix = "> "
	}
	tags := strings.Fields(p.Tags)
	budget := width - len(prefix) - len(idPart) - 3 - suffixReserve
	if budget < 10 {
		budget = 10
	}
	var shown []string
	used := 0
	cut := len(tags)
	for i, t := range tags {
		if len(shown) >= maxTagsShown {
			cut = i
			break
		}
		add := len(t) + 1
		if used+add > budget {
			cut = i
			break
		}
		shown = append(shown, t)
		used += add
	}
	tagStr := strings.Join(shown, " ")
	remaining := len(tags) - cut
	more := ""
	if remaining > 0 {
		more = fmt.Sprintf(" (+%d more)", remaining)
	}
	idRendered := idStyle.Render(idPart)
	tagsRendered := tagStyle.Render(tagStr)
	if selected {
		tagsRendered = selectedStyle.Render(tagStr)
	}
	moreRendered := ""
	if more != "" {
		moreRendered = moreTagStyle.Render(more)
	}
	prefixRendered := prefix
	if selected {
		prefixRendered = cursorStyle.Render(prefix)
	}
	return prefixRendered + idRendered + "  " + tagsRendered + moreRendered
}

func (m Model) apiLabel() string {
	if m.cfg.ActiveAPI == "rule34" {
		return apiR34Style.Render("[rule34]")
	}
	return apiSBStyle.Render("[safebooru]")
}


func (m Model) View() string {
	switch m.state {
	case stateAgeGate:
		activeYesBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(mochaGreen).
			Foreground(mochaGreen).
			Bold(true).
			Padding(0, 2)

		activeNoBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(mochaRed).
			Foreground(mochaRed).
			Bold(true).
			Padding(0, 2)

		inactiveBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(mochaOverlay0).
			Foreground(mochaOverlay0).
			Padding(0, 2)

		var yesBox, noBox string
		if m.ageGateCursor == 0 {
			yesBox = activeYesBox.Render("yes  (rule34)")
			noBox = inactiveBox.Render("no  (safebooru)")
		} else {
			yesBox = inactiveBox.Render("yes  (rule34)")
			noBox = activeNoBox.Render("no  (safebooru)")
		}

		s := titleStyle.Render("r34-dl") + "\n\n"
		s += inputStyle.Render("are you 18 or older?") + "\n\n"
		s += lipgloss.JoinHorizontal(lipgloss.Center, yesBox, "   ", noBox) + "\n\n"
		s += dimStyle.Render("←/→: select · enter: confirm · q: quit")
		return s

	case stateSearch:
		ascii := `
 ░▒▓███████▓▒░░▒▓███████▓▒░░▒▓█▓▒░░▒▓█▓▒░▒▓███████▓▒░░▒▓█▓▒░        
 ░▒▓█▓▒░░▒▓█▓▒░      ░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░        
 ░▒▓█▓▒░░▒▓█▓▒░      ░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░        
 ░▒▓███████▓▒░░▒▓███████▓▒░░▒▓████████▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░        
 ░▒▓█▓▒░░▒▓█▓▒░      ░▒▓█▓▒░      ░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░        
 ░▒▓█▓▒░░▒▓█▓▒░      ░▒▓█▓▒░      ░▒▓█▓▒░▒▓█▓▒░░▒▓█▓▒░▒▓█▓▒░        
 ░▒▓█▓▒░░▒▓█▓▒░▒▓███████▓▒░       ░▒▓█▓▒░▒▓███████▓▒░░▒▓████████▓▒░ v1.1 `
		s := titleStyle.Render(ascii) + "\n\n"
		s += m.apiLabel() + " " + titleStyle.Render("tags: ") + inputStyle.Render(m.query) + cursorStyle.Render("█") + "\n\n"
		if m.cfg.ActiveAPI == "rule34" && m.cfg.APIKey == "" {
			s += errorStyle.Render("   rule34 now requires an API key") + "\n"
			s += dimStyle.Render(" make sure to get one from thier website, then use: ./rule34 -apik") + "\n\n"
		}
		if m.err != "" {
			s += errorStyle.Render(m.err) + "\n\n"
		}
		s += dimStyle.Render("tab: switch api · enter: search · ctrl+c: quit")
		return s

	case stateSearching:
		return m.apiLabel() + " searching...\n\n" + dimStyle.Render("ctrl+c: quit")

	case stateList:
		if len(m.posts) == 0 {
			return "no results\n\n" + dimStyle.Render("/: new search · q: quit")
		}
		rows := m.visibleRows()
		end := m.offset + rows
		if end > len(m.posts) {
			end = len(m.posts)
		}
		s := titleStyle.Render("Search Results:") + "  " + m.apiLabel() + "\n\n"
		for i := m.offset; i < end; i++ {
			s += renderPostLine(m.posts[i], m.width, i == m.cursor) + "\n"
		}
		if m.notice != "" {
			s += "\n" + m.notice
		} else if m.loadingMore {
			s += "\n" + dimStyle.Render("loading more...")
		}
		countLabel := fmt.Sprintf("[%d/%d]", m.cursor+1, len(m.posts))
		if m.totalKnown {
			countLabel = fmt.Sprintf("[%d/%d]", m.cursor+1, m.totalCount)
		}
		if m.noMore {
			countLabel += dimStyle.Render(" (end)")
		}
		s += "\n" + scrollStyle.Render(countLabel)
		s += "  " + dimStyle.Render("↑/↓·j/k: move · p: preview · enter: download · tab: switch api · /: search · q: quit")
		return s

	case stateViewerLoading:
		return dimStyle.Render(fmt.Sprintf("fetching #%d ...", m.viewerPost.ID))

	case stateVideoPlaying:
		status := "\n" + dimStyle.Render(fmt.Sprintf(
			"▶ #%d  %dx%d  any key: stop",
			m.viewerPost.ID, m.viewerPost.Width, m.viewerPost.Height,
		))
		if m.videoFrame != "" {
			return m.videoFrame + status
		}
		return status

	case stateViewer:
		if m.viewerErr != "" {
			return errorStyle.Render(m.viewerErr) + "\n\n" + dimStyle.Render("any key: back")
		}
		status := "\n" + dimStyle.Render(fmt.Sprintf(
			"#%d  %dx%d  any key / space: back",
			m.viewerPost.ID, m.viewerPost.Width, m.viewerPost.Height,
		))
		if m.viewerImage != "" {
			return m.viewerImage + status
		}
		return status

	case stateDownloading:
		return fmt.Sprintf("downloading...\n\n%d/%d done, %d failed\n\n%s",
			m.done+m.failed, m.total, m.failed, dimStyle.Render("ctrl+c: quit"))

	case stateDone:
		return fmt.Sprintf("finished!\n\n%d downloaded, %d failed\nSaved to ./downloads\n\n%s",
			m.done, m.failed, dimStyle.Render("/: new search · q: quit"))
	}
	return ""
}
