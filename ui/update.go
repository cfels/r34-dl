package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
	"moxiu/r34-dl/safe"

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
			m.outdated = msg.outdated
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
				return m, m.historyBack()
			case tea.KeyDown:
				return m, m.historyForward()
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
				return m, m.historyBack()
			case tea.KeyCtrlN:
				return m, m.historyForward()
			case tea.KeyLeft:
				if m.pickSuggestion(-1) {
					return m, nil
				}
				if m.inputCursor > 0 {
					m.inputCursor--
				}
				return m, m.refreshSuggestions()
			case tea.KeyRight:
				if m.pickSuggestion(1) {
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
			case "up", "j":
				m.cursor--
				return m, m.clampViewport()
			case "down", "k":
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
					return m, fetchPreview(m.viewerPost)
				}
			case "enter":
				if m.bulkMode {
					if !m.bulkActive {
						m.openBulkForm()
					}
					return m, nil
				}
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

		case stateBulk:
			return m.updateBulk(msg)

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
			m.notice = errorStyle.Render("download failed: " + safe.Text(msg.err.Error()))
		} else {
			m.notice = noticeStyle.Render("saved → " + safe.Text(msg.path))
		}
		return m, nil

	case bulkResultMsg:
		if !m.bulkActive {
			return m, nil
		}
		if msg.result.Err != nil {
			m.bulkFailed++
			m.bulkLastErr = safe.Text(msg.result.Err.Error())
			m.logBulk(fmt.Sprintf("failed #%d: %s", msg.result.Post.ID, m.bulkLastErr))
		} else {
			m.bulkDone++
			m.bulkLastPath = safe.Text(msg.result.Path)
			m.logBulk(fmt.Sprintf("saved #%d -> %s", msg.result.Post.ID, m.bulkLastPath))
		}
		m.notice = m.bulkNotice(false)
		return m, nextBulkResult(m.bulkCh)

	case bulkFinishedMsg:
		if !m.bulkActive {
			return m, nil
		}
		m.bulkActive = false
		m.bulkCh = nil
		m.bulkStage = bulkDone
		m.notice = m.bulkNotice(true)
		return m, nil

	case bulkPostsMsg:
		if msg.gen != m.searchGen {
			return m, nil
		}
		if msg.err != nil {
			m.bulkStage = bulkDone
			m.logBulk("search failed: " + safe.Text(msg.err.Error()))
			return m, nil
		}
		if len(msg.posts) == 0 {
			m.bulkStage = bulkDone
			m.logBulk("no results")
			return m, nil
		}
		m.logBulk(fmt.Sprintf("bulk downloading %d posts...", len(msg.posts)))
		return m, m.startBulkDownload(msg.posts)

	case imageFetchedMsg:
		if msg.err != nil {
			m.viewerErr = safe.Text(msg.err.Error())
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
			m.viewerErr = safe.Text(msg.err.Error())
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
			m.err = safe.Text(msg.err.Error())
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
		if m.state != stateList && m.state != stateBulk {
			return m, nil
		}
		cmds := m.countCommands(msg.gen)
		cmds = append(cmds, startCountTicker(msg.gen))
		return m, tea.Batch(cmds...)

	case loadMoreMsg:
		m.loadingMore = false
		if msg.err != nil {
			m.notice = "couldn't load more: " + safe.Text(msg.err.Error())
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

	case blinkMsg:
		if m.state == stateSearch {
			m.cursorVisible = !m.cursorVisible
			return m, startBlink()
		}
	}
	return m, nil
}

func (m *Model) historyBack() tea.Cmd {
	m.suggestPick = false
	if len(m.history) > 0 && m.historyIdx > 0 {
		m.historyIdx--
		m.query = m.history[m.historyIdx]
		m.inputCursor = len([]rune(m.query))
	}
	return m.refreshSuggestions()
}

func (m Model) bulkCount() int {
	count, err := strconv.Atoi(strings.TrimSpace(m.bulkInput))
	if err != nil || count <= 0 {
		count = m.limit
	}
	if count <= 0 {
		count = defaultBulkCount
	}
	if count > maxBulkPosts {
		count = maxBulkPosts
	}
	return count
}

func (m *Model) openBulkForm() {
	m.state = stateBulk
	m.bulkStage = bulkForm
	m.bulkTags = strings.TrimSpace(m.query)
	m.bulkInput = ""
	m.bulkFocus = 0
	m.bulkLog = nil
}

func (m *Model) logBulk(line string) {
	m.bulkLog = append(m.bulkLog, safe.Text(line))
	if len(m.bulkLog) > maxBulkLogLines {
		m.bulkLog = m.bulkLog[len(m.bulkLog)-maxBulkLogLines:]
	}
}

func (m *Model) startBulkRun() tea.Cmd {
	count := m.bulkCount()
	m.bulkTags = strings.TrimSpace(m.bulkTags)
	m.bulkStage = bulkRunning
	m.bulkActive = false
	m.bulkTotal = 0
	m.bulkLog = nil
	m.bulkDone, m.bulkFailed = 0, 0
	m.bulkLastErr, m.bulkLastPath = "", ""
	m.logBulk(fmt.Sprintf("searching %s for %q (up to %d posts)", m.cfg.ActiveAPI, safe.Text(m.bulkTags), count))
	return fetchBulkPostsCmd(m.client, m.bulkQuery(), count, m.searchGen)
}

func (m Model) updateBulk(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.bulkStage {
	case bulkRunning:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		return m, nil
	case bulkDone:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		m.state = stateList
		m.bulkStage = bulkForm
		return m, nil
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.state = stateList
		m.bulkStage = bulkForm
		return m, nil
	case tea.KeyTab:
		m.cycleAPI(1)
		return m, nil
	case tea.KeyShiftTab:
		m.cycleAPI(-1)
		return m, nil
	case tea.KeyUp, tea.KeyShiftUp:
		m.bulkFocus = 0
		return m, nil
	case tea.KeyDown, tea.KeyShiftDown:
		m.bulkFocus = 1
		return m, nil
	case tea.KeyEnter:
		if m.bulkFocus == 0 {
			m.bulkFocus = 1
			return m, nil
		}
		return m, m.startBulkRun()
	case tea.KeyBackspace:
		m.trimBulkField()
		return m, nil
	case tea.KeyCtrlU:
		if m.bulkFocus == 0 {
			m.bulkTags = ""
		} else {
			m.bulkInput = ""
		}
		return m, nil
	case tea.KeySpace:
		if m.bulkFocus == 0 && len([]rune(m.bulkTags)) < safe.MaxTagRunes {
			m.bulkTags += " "
		}
		return m, nil
	case tea.KeyRunes:
		for _, r := range msg.Runes {
			m.typeBulkRune(r)
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) trimBulkField() {
	if m.bulkFocus == 0 {
		if runes := []rune(m.bulkTags); len(runes) > 0 {
			m.bulkTags = string(runes[:len(runes)-1])
		}
		return
	}
	if runes := []rune(m.bulkInput); len(runes) > 0 {
		m.bulkInput = string(runes[:len(runes)-1])
	}
}

func (m *Model) typeBulkRune(r rune) {
	if m.bulkFocus == 0 {
		if len([]rune(m.bulkTags)) >= safe.MaxTagRunes {
			return
		}
		m.bulkTags += string(r)
		return
	}
	if r < '0' || r > '9' || len(m.bulkInput) >= maxBulkDigits {
		return
	}
	m.bulkInput += string(r)
}

func (m *Model) startBulkDownload(posts []api.Post) tea.Cmd {
	if m.bulkActive || len(posts) == 0 {
		return nil
	}
	queue := make([]api.Post, len(posts))
	copy(queue, posts)
	m.bulkActive = true
	m.bulkTotal = len(queue)
	m.bulkDone, m.bulkFailed = 0, 0
	m.bulkLastErr, m.bulkLastPath = "", ""
	m.bulkCh = m.downloader.DownloadAll(queue)
	m.notice = noticeStyle.Render(fmt.Sprintf("bulk downloading %d posts...", len(queue)))
	return nextBulkResult(m.bulkCh)
}

func (m Model) bulkNotice(finished bool) string {
	processed := m.bulkDone + m.bulkFailed
	label := fmt.Sprintf("bulk %d/%d saved", processed, m.bulkTotal)
	if finished {
		label = fmt.Sprintf("bulk done: %d/%d saved", processed, m.bulkTotal)
	}
	line := noticeStyle.Render(label)
	if m.bulkFailed > 0 {
		line += " " + errorStyle.Render(fmt.Sprintf("· %d failed", m.bulkFailed))
	}
	if m.bulkLastErr != "" {
		return line + dimStyle.Render(" · "+safe.Limit(m.bulkLastErr, bulkNoticeDetail))
	}
	if m.bulkLastPath != "" {
		return line + dimStyle.Render(" · "+safe.Limit(filepath.Base(m.bulkLastPath), bulkNoticeDetail))
	}
	return line
}

func (m *Model) historyForward() tea.Cmd {
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
	return m.refreshSuggestions()
}
