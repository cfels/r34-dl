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

	case releaseInfoMsg:
		if msg.err == nil {
			m.release = msg.info
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "p" && m.previewKeyRepeated() {
			return m, nil
		}
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
					m.switchAPI("rule34")
				} else {
					m.switchAPI("safebooru")
				}
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
			case tea.KeyCtrlA:
				if m.cfg.ActiveAPI == "rule34" {
					m.cfg.FilterAI = !m.cfg.FilterAI
					_ = conf.Save(m.cfg)
				}
				return m, nil
			case tea.KeyCtrlC, tea.KeyEsc:
				if msg.Type == tea.KeyEsc && m.suggestPick {
					m.suggestPick = false
					m.suggestIdx = 0
					return m, nil
				}
				m.clearSuggestions()
				return m, tea.Quit
			case tea.KeyEnter:
				if m.suggestPick && m.acceptSuggestion() {
					return m, nil
				}
				if m.query != "" {
					if len(m.history) == 0 || m.history[len(m.history)-1] != m.query {
						m.history = append(m.history, m.query)
						m.cfg.SearchHistory = m.history
						_ = conf.Save(m.cfg)
					}
					m.historyIdx = len(m.history)
					m.clearSuggestions()
					return m, m.startSearch()
				}
			case tea.KeyUp:
				if m.suggestPick {
					if m.suggestIdx == 0 {
						m.suggestPick = false
						return m, nil
					}
					m.cycleSuggestion(-1)
					return m, nil
				}
				m.suggestPick = false
				if len(m.history) > 0 && m.historyIdx > 0 {
					m.historyIdx--
					m.query = m.history[m.historyIdx]
					m.inputCursor = len([]rune(m.query))
				}
				return m, m.refreshSuggestions()
			case tea.KeyDown:
				if m.pickSuggestion(1) {
					return m, nil
				}
				m.suggestPick = false
				if m.historyIdx < len(m.history)-1 {
					m.historyIdx++
					m.query = m.history[m.historyIdx]
					m.inputCursor = len([]rune(m.query))
				} else if m.historyIdx == len(m.history)-1 {
					m.historyIdx = len(m.history)
					m.query = ""
					m.inputCursor = 0
				}
				return m, m.refreshSuggestions()
			case tea.KeyShiftUp:
				if m.pickSuggestion(-1) {
					return m, nil
				}
				return m, nil
			case tea.KeyShiftDown:
				if m.pickSuggestion(1) {
					return m, nil
				}
				return m, nil
			case tea.KeyShiftRight:
				if m.pickSuggestion(1) {
					return m, nil
				}
				return m, nil
			case tea.KeyShiftLeft:
				if m.pickSuggestion(-1) {
					return m, nil
				}
				return m, nil
			case tea.KeyCtrlP:
				m.suggestPick = false
				if len(m.history) > 0 && m.historyIdx > 0 {
					m.historyIdx--
					m.query = m.history[m.historyIdx]
					m.inputCursor = len([]rune(m.query))
				}
				return m, m.refreshSuggestions()
			case tea.KeyCtrlN:
				m.suggestPick = false
				if m.historyIdx < len(m.history)-1 {
					m.historyIdx++
					m.query = m.history[m.historyIdx]
					m.inputCursor = len([]rune(m.query))
				} else if m.historyIdx == len(m.history)-1 {
					m.historyIdx = len(m.history)
					m.query = ""
					m.inputCursor = 0
				}
				return m, m.refreshSuggestions()
			case tea.KeyLeft:
				if m.inputCursor > 0 {
					m.inputCursor--
				}
				return m, m.refreshSuggestions()
			case tea.KeyRight:
				if m.inputCursor >= len([]rune(m.query)) && m.acceptGhost() {
					return m, nil
				}
				if m.inputCursor < len([]rune(m.query)) {
					m.inputCursor++
				}
				return m, m.refreshSuggestions()
			case tea.KeyBackspace:
				runes := []rune(m.query)
				if m.inputCursor > 0 {
					runes = append(runes[:m.inputCursor-1], runes[m.inputCursor:]...)
					m.query = string(runes)
					m.inputCursor--
				}
				return m, m.refreshSuggestions()
			case tea.KeySpace:
				runes := []rune(m.query)
				runes = append(runes[:m.inputCursor], append([]rune{' '}, runes[m.inputCursor:]...)...)
				m.query = string(runes)
				m.inputCursor++
				return m, m.refreshSuggestions()
			case tea.KeyTab:
				if m.canAcceptSuggestion() && m.acceptSuggestion() {
					return m, nil
				}
				m.cycleAPI(1)
				return m, m.refreshSuggestions()
			case tea.KeyShiftTab:
				m.cycleAPI(-1)
				return m, m.refreshSuggestions()
			case tea.KeyRunes:
				if len(msg.Runes) == 1 && msg.Runes[0] == 'Y' && m.acceptSuggestion() {
					return m, nil
				}
				runes := []rune(m.query)
				runes = append(runes[:m.inputCursor], append(msg.Runes, runes[m.inputCursor:]...)...)
				m.query = string(runes)
				m.inputCursor += len(msg.Runes)
				return m, m.refreshSuggestions()
			}
			return m, nil

		case stateList:
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "ctrl+a":
				if m.cfg.ActiveAPI == "rule34" {
					m.cfg.FilterAI = !m.cfg.FilterAI
					_ = conf.Save(m.cfg)
					return m, m.startSearch()
				}
				return m, nil
			case "tab":
				m.cycleAPI(1)
				return m, m.startSearch()
			case "shift+tab":
				m.cycleAPI(-1)
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
					m.stopVideo()
					m.viewerPost = m.posts[m.cursor]
					m.viewerErr = ""
					m.viewerImage = ""
					if isVideo(m.viewerPost) {
						m.state = stateViewerLoading
						post := m.viewerPost
						w, h := m.width, m.height
						opts := videoOptions{
							audio: m.cfg.AudioEnabled,
						}
						return m, func() tea.Msg {
							vp, err := startVideoPlayer(post, w, h, opts)
							if err != nil {
								return videoDoneMsg{err: err}
							}
							return videoStartedMsg{vp: vp}
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
			if msg.String() == "m" && m.video != nil {
				m.video.toggleMute()
				m.cfg.AudioEnabled = !m.video.isMuted()
				_ = conf.Save(m.cfg)
				return m, nil
			}
			m.stopVideo()
			m.state = stateList
			return m, tea.Batch(tea.ExitAltScreen, tea.ClearScreen)
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

	case suggestTickMsg:
		if msg.gen != m.suggestGen || m.state != stateSearch {
			return m, nil
		}
		return m, m.suggestLookup(m.suggestWord)

	case suggestionsMsg:
		if msg.gen != m.suggestGen || msg.word != m.suggestWord || m.state != stateSearch {
			return m, nil
		}
		m.suggestions = msg.tags
		if len(msg.tags) == 0 {
			m.suggestIdx = 0
		} else {
			m.suggestIdx %= len(msg.tags)
		}
		return m, nil

	case imageRenderedMsg:
		if m.state == stateVideoPlaying {
			m.videoFrame = msg.s
		} else {
			m.viewerImage = msg.s
			m.state = stateViewer
		}
		return m, nil

	case videoStartedMsg:
		if msg.vp == nil {
			m.viewerErr = "failed to start video player"
			m.state = stateViewer
			return m, nil
		}
		if m.video != nil || m.state != stateViewerLoading {
			msg.vp.stop()
			return m, nil
		}
		m.video = msg.vp
		m.state = stateVideoPlaying
		return m, tea.Batch(tea.EnterAltScreen, nextFrame(m.video))

	case videoFrameMsg:
		if m.state != stateVideoPlaying || msg.vp == nil || msg.vp != m.video {
			return m, nil
		}
		w, h := m.width, m.height
		return m, renderVideoFrame(msg.vp, msg.frame, w, h)

	case videoRenderedMsg:
		if msg.vp != nil && msg.vp == m.video {
			m.videoFrame = msg.s
			return m, nextFrame(msg.vp)
		}
		return m, nil

	case videoDoneMsg:
		if msg.vp != nil && msg.vp != m.video {
			return m, nil
		}
		m.stopVideo()
		if msg.err != nil {
			m.viewerErr = msg.err.Error()
			m.state = stateViewer
		} else {
			m.state = stateList
		}
		return m, tea.Batch(tea.ExitAltScreen, tea.ClearScreen)

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
			doCountTotal(m.client, m.apiTags(), msg.gen),
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
