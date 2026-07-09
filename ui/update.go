package ui

import (
	"fmt"

	"moxiu/r34-dl/conf"

	tea "github.com/charmbracelet/bubbletea"
)

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
				m.cursorVisible = true
				return m, startBlink()
			case "ctrl+c", "q":
				return m, tea.Quit
			}
			return m, nil

		case stateSearch:
			m.cursorVisible = true
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
					m.inputCursor = len([]rune(m.query))
				}
			case tea.KeyDown:
				if m.historyIdx < len(m.history)-1 {
					m.historyIdx++
					m.query = m.history[m.historyIdx]
					m.inputCursor = len([]rune(m.query))
				} else if m.historyIdx == len(m.history)-1 {
					m.historyIdx = len(m.history)
					m.query = ""
					m.inputCursor = 0
				}
			case tea.KeyLeft:
				if m.inputCursor > 0 {
					m.inputCursor--
				}
			case tea.KeyRight:
				if m.inputCursor < len([]rune(m.query)) {
					m.inputCursor++
				}
			case tea.KeyBackspace:
				runes := []rune(m.query)
				if m.inputCursor > 0 {
					runes = append(runes[:m.inputCursor-1], runes[m.inputCursor:]...)
					m.query = string(runes)
					m.inputCursor--
				}
			case tea.KeySpace:
				runes := []rune(m.query)
				runes = append(runes[:m.inputCursor], append([]rune{' '}, runes[m.inputCursor:]...)...)
				m.query = string(runes)
				m.inputCursor++
			case tea.KeyTab:
				if m.cfg.ActiveAPI == "rule34" {
					m.switchAPI("safebooru")
				} else {
					if m.cfg.AgeVerified {
						m.switchAPI("rule34")
					}
				}
			case tea.KeyRunes:
				runes := []rune(m.query)
				runes = append(runes[:m.inputCursor], append(msg.Runes, runes[m.inputCursor:]...)...)
				m.query = string(runes)
				m.inputCursor += len(msg.Runes)
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
				m.inputCursor = 0
				m.cursorVisible = true
				m.posts = nil
				m.cursor, m.offset, m.page = 0, 0, 0
				m.noMore = false
				m.totalKnown = false
				m.totalCount = 0
				m.historyIdx = len(m.history)
				return m, startBlink()
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

	case blinkMsg:
		if m.state == stateSearch {
			m.cursorVisible = !m.cursorVisible
			return m, startBlink()
		}
	}
	return m, nil
}
