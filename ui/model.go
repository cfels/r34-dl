package ui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
	"moxiu/r34-dl/dl"

	tea "github.com/charmbracelet/bubbletea"
)

type state int

const (
	stateAgeGate state = iota
	stateSearch
	stateSearching
	stateList
	stateViewerLoading
	stateViewer
	stateVideoPlaying
)

const maxTagsShown = 10
const suffixReserve = 14
const countRefreshInterval = 5 * time.Second
const reservedLines = 4
const aiTag = "ai_generated"

type Model struct {
	cfg        conf.Config
	client     api.Client
	clients    map[string]api.Client
	sites      []string
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

	viewerPost    api.Post
	viewerErr     string
	viewerImage   string
	videoFrame    string
	video         *videoPlayer
	ageGateCursor int

	history       []string
	historyIdx    int
	inputCursor   int
	cursorVisible bool

	previewKeyAt time.Time

	suggestions  []string
	suggestStart int
	suggestWord  string
	suggestDash  bool
	suggestIdx   int
	suggestDone  string
	suggestPick  bool
	suggestGen   int

	version  versionInfo
	release  versionInfo
	outdated bool
}

func (m Model) Cfg() conf.Config { return m.cfg }

func (m Model) apiTags() string {
	parts := make([]string, 0, 2+len(m.cfg.Blacklist))
	if query := strings.TrimSpace(m.query); query != "" {
		parts = append(parts, query)
	}
	if m.aiFilterOn() {
		parts = append(parts, "-"+aiTag)
	}
	for _, tag := range m.blacklistTags() {
		if tag == aiTag && m.aiFilterOn() {
			continue
		}
		parts = append(parts, "-"+tag)
	}
	return strings.Join(parts, " ")
}

func (m Model) blacklistTags() []string {
	if !api.SupportsNegativeTags(m.cfg.ActiveAPI) {
		return nil
	}
	seen := make(map[string]bool, len(m.cfg.Blacklist))
	tags := make([]string, 0, len(m.cfg.Blacklist))
	for _, raw := range m.cfg.Blacklist {
		tag := conf.NormalizeTag(raw)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags
}

func (m Model) aiFilterOn() bool {
	return m.cfg.FilterAI && m.cfg.ActiveAPI == "rule34"
}

func NewModel(clients map[string]api.Client, cfg conf.Config, initialTags string, limit int) Model {
	active := activeClient(clients, cfg)
	m := Model{
		cfg:     cfg,
		client:  active,
		clients: clients,
		sites:   api.SiteNames(),
		limit:   limit,
		query:   initialTags,
		downloader: func() *dl.Downloader {
			home, err := os.UserHomeDir()
			if err != nil {
				home = "."
			}
			dlPath := filepath.Join(home, "r34-dl_downloads")
			_ = os.MkdirAll(dlPath, 0o755)
			return dl.New(dlPath, 4)
		}(),
		width:         100,
		height:        30,
		history:       cfg.SearchHistory,
		historyIdx:    len(cfg.SearchHistory),
		cursorVisible: true,
		version:       localVersionInfo(),
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

func activeClient(clients map[string]api.Client, cfg conf.Config) api.Client {
	name := cfg.ActiveAPI
	if !cfg.AgeVerified && api.IsAdultSite(name) {
		name = "safebooru"
	}
	if client := clients[name]; client != nil {
		return client
	}
	if client := clients["safebooru"]; client != nil {
		return client
	}
	return api.NewSafebooruClient()
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.ClearScreen}
	switch {
	case m.state == stateSearching:
		gen := m.searchGen
		cmds = append(cmds,
			doSearch(m.client, m.apiTags(), m.limit, 0),
			doCountTotal(m.client, m.apiTags(), gen),
			startCountTicker(gen),
		)
	case m.state == stateSearch:
		cmds = append(cmds, startBlink())
	}
	cmds = append(cmds, fetchReleaseInfo())
	return tea.Batch(cmds...)
}

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
		cmd = doLoadMore(m.client, m.apiTags(), m.limit, nextPage)
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

func (m *Model) allowedSites() []string {
	var names []string
	for _, name := range m.sites {
		if api.IsAdultSite(name) && !m.cfg.AgeVerified {
			continue
		}
		if m.clients[name] == nil {
			continue
		}
		names = append(names, name)
	}
	return names
}

func (m *Model) switchAPI(name string) {
	client := m.clients[name]
	if client == nil {
		return
	}
	m.cfg.ActiveAPI = name
	m.client = client
	_ = conf.Save(m.cfg)
}

func (m *Model) cycleAPI(delta int) {
	names := m.allowedSites()
	if len(names) < 2 {
		return
	}
	index := 0
	for i, name := range names {
		if name == m.cfg.ActiveAPI {
			index = i
			break
		}
	}
	index = ((index+delta)%len(names) + len(names)) % len(names)
	m.switchAPI(names[index])
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
		doSearch(m.client, m.apiTags(), m.limit, 0),
		doCountTotal(m.client, m.apiTags(), gen),
		startCountTicker(gen),
	)
}

func (m *Model) stopVideo() {
	if m.video != nil {
		m.video.stop()
		m.video = nil
	}
	m.videoFrame = ""
}

const previewRepeatGap = 220 * time.Millisecond

func (m *Model) previewKeyRepeated() bool {
	now := time.Now()
	repeated := !m.previewKeyAt.IsZero() && now.Sub(m.previewKeyAt) < previewRepeatGap
	m.previewKeyAt = now
	return repeated
}
