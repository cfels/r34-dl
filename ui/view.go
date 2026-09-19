package ui

import (
	"fmt"
	"strings"

	"moxiu/r34-dl/api"

	"github.com/charmbracelet/lipgloss"
)

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
	apiPHStyle    = lipgloss.NewStyle().Foreground(mochaPeach).Bold(true)
	apiXVStyle    = lipgloss.NewStyle().Foreground(mochaYellow).Bold(true)
	apiXHStyle    = lipgloss.NewStyle().Foreground(mochaBlue).Bold(true)
)

func renderPostLine(p api.Post, width int, selected bool) string {
	idPart := fmt.Sprintf("#%d", p.ID)
	prefix := "  "
	if selected {
		prefix = "> "
	}
	prefixRendered := prefix
	if selected {
		prefixRendered = cursorStyle.Render(prefix)
	}
	if p.Title != "" {
		return prefixRendered + idStyle.Render(idPart) + "  " + renderTitle(p.Title, width, selected) + durationSuffix(p.Duration)
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
	return prefixRendered + idRendered + "  " + tagsRendered + moreRendered
}

func renderTitle(title string, width int, selected bool) string {
	budget := width - len("  ") - 12 - suffixReserve
	if budget < 10 {
		budget = 10
	}
	runes := []rune(strings.TrimSpace(title))
	if len(runes) > budget {
		title = string(runes[:budget]) + "…"
	} else {
		title = string(runes)
	}
	if selected {
		return selectedStyle.Render(title)
	}
	return tagStyle.Render(title)
}

func durationSuffix(duration string) string {
	if duration == "" {
		return ""
	}
	return moreTagStyle.Render("  " + duration)
}

func (m Model) apiLabel() string {
	switch m.cfg.ActiveAPI {
	case "rule34":
		return apiR34Style.Render("[rule34]")
	case "pornhub":
		return apiPHStyle.Render("[pornhub]")
	case "xvideos":
		return apiXVStyle.Render("[xvideos]")
	case "xhamster":
		return apiXHStyle.Render("[xhamster]")
	}
	return apiSBStyle.Render("[safebooru]")
}

func switchLine(on bool) string {
	knob := "───────●"
	label := "off"
	style := dimStyle
	if on {
		knob = "●───────"
		label = " on"
		style = selectedStyle
	}
	return inputStyle.Render("   Filter AI posts ") +
		style.Render("[ "+knob+" ]") +
		dimStyle.Render(" "+label+"  ctrl+a")
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
			yesBox = activeYesBox.Render("yes (rule34)")
			noBox = inactiveBox.Render("no (safebooru)")
		} else {
			yesBox = inactiveBox.Render("yes (rule34)")
			noBox = activeNoBox.Render("no (safebooru)")
		}

		s := titleStyle.Render("r34-dl") + "\n\n"
		s += inputStyle.Render("are you 18 or older?") + "\n\n"
		s += lipgloss.JoinHorizontal(lipgloss.Center, yesBox, "   ", noBox) + "\n\n"
		s += dimStyle.Render("←/→: select · enter: confirm · q: quit")
		return s

	case stateSearch:
		s := m.bannerText() + "\n\n"
		runes := []rune(m.query)
		before := inputStyle.Render(string(runes[:m.inputCursor]))
		var cursorChar string
		if m.inputCursor < len(runes) {
			cursorChar = string(runes[m.inputCursor])
		} else {
			cursorChar = " "
		}
		var cursorRendered string
		if m.cursorVisible {
			cursorRendered = cursorStyle.Reverse(true).Render(cursorChar)
		} else {
			cursorRendered = inputStyle.Render(cursorChar)
		}
		var after string
		if m.inputCursor+1 < len(runes) {
			after = inputStyle.Render(string(runes[m.inputCursor+1:]))
		}
		ghost := m.ghostSuggestion()
		ghostRendered := ""
		if ghost != "" {
			ghostRendered = dimStyle.Render(ghost)
		}
		s += m.apiLabel() + " " + titleStyle.Render("tags: ") + before + cursorRendered + ghostRendered + after + "\n"
		if tags := m.nextSuggestions(); len(tags) > 0 {
			current := tags[0]
			if len(tags) > maxShownTags {
				tags = tags[:maxShownTags]
			}
			line := selectedStyle.Render(current)
			if m.suggestPick {
				line = selectedStyle.Render("▸ " + current)
			}
			if len(tags) > 1 {
				line += dimStyle.Render(" · " + strings.Join(tags[1:], " · "))
			}
			s += dimStyle.Render("   tab/Y → ") + line + "\n"
		}
		if m.cfg.ActiveAPI == "rule34" {
			s += switchLine(m.cfg.FilterAI) + "\n"
		}
		s += "\n"
		if m.cfg.ActiveAPI == "rule34" && m.cfg.APIKey == "" {
			s += errorStyle.Render("   rule34 now requires an API key") + "\n"
			s += dimStyle.Render(" make sure to get one from thier website, then use: ./r34-dl -apik") + "\n\n"
		}
		if m.err != "" {
			s += errorStyle.Render(m.err) + "\n\n"
		}
		hint := "tab: switch site · shift+tab: back · enter: search · ↑/↓: history"
		if len(m.suggestions) > 0 {
			hint = "↓: pick tag · tab/Y: accept · →: complete · enter: search · ↑: history"
		}
		if m.suggestPick {
			hint = "↑/↓: move · tab/Y/enter: accept · esc: back to typing"
		}
		s += dimStyle.Render(hint)
		return s

	case stateSearching:
		return m.apiLabel() + " searching...\n\n" + dimStyle.Render("ctrl+c: quit")

	case stateList:
		if len(m.posts) == 0 {
			return titleStyle.Render("Search Results:") + "  " + m.apiLabel() +
				"\n\nno results\n\n" + dimStyle.Render("/: new search · tab: switch site · q: quit")
		}
		rows := m.visibleRows()
		end := m.offset + rows
		if end > len(m.posts) {
			end = len(m.posts)
		}
		s := titleStyle.Render("Search Results:") + "  " + m.apiLabel() + "\n\n"
		if m.aiFilterOn() {
			s = titleStyle.Render("Search Results:") + "  " + m.apiLabel() +
				"  " + dimStyle.Render("[no AI]") + "\n\n"
		}
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
		s += "  " + dimStyle.Render("↑/↓·j/k: move · p: preview · enter: download · tab: switch site · /: search · q: quit")
		return s

	case stateViewerLoading:
		return dimStyle.Render(fmt.Sprintf("fetching #%d ...", m.viewerPost.ID))

	case stateVideoPlaying:
		audio := "audio: on"
		if m.video == nil || m.video.isMuted() {
			audio = "audio: off"
		}
		line := fmt.Sprintf("▶ #%d  %s", m.viewerPost.ID, postSize(m.viewerPost))
		if fps := m.video.fpsLabel(); fps != "" {
			line += "  " + fps
		}
		line += fmt.Sprintf("  %s  ·  m: toggle audio  ·  any key: stop", audio)
		status := "\n" + dimStyle.Render(line)
		if m.videoFrame != "" {
			return m.videoFrame + status
		}
		return status

	case stateViewer:
		if m.viewerErr != "" {
			return errorStyle.Render(m.viewerErr) + "\n\n" + dimStyle.Render("any key: back")
		}
		status := "\n" + dimStyle.Render(fmt.Sprintf(
			"#%d  %s  any key / space: back",
			m.viewerPost.ID, postSize(m.viewerPost),
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

func postSize(p api.Post) string {
	if p.Duration != "" {
		return p.Duration
	}
	if p.Video {
		return "video"
	}
	return fmt.Sprintf("%dx%d", p.Width, p.Height)
}
