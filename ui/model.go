package ui

import (
	"os"
	"path/filepath"
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
const reservedLines = 4

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

	history       []string
	historyIdx    int
	inputCursor   int
	cursorVisible bool
}

func (m Model) Cfg() conf.Config { return m.cfg }

func NewModel(sbClient, r34Client api.Client, cfg conf.Config, initialTags string, limit int) Model {
	active := sbClient
	if cfg.ActiveAPI == "rule34" && cfg.AgeVerified {
		active = r34Client
	}
	m := Model{
		cfg:       cfg,
		client:    active,
		sbClient:  sbClient,
		r34Client: r34Client,
		limit:     limit,
		query:     initialTags,
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
	if m.state == stateSearch {
		return startBlink()
	}
	return nil
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
