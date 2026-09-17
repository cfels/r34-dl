package ui

import (
	"strings"
	"time"

	"moxiu/r34-dl/api"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	suggestDelay  = 160 * time.Millisecond
	suggestMinLen = 2
	maxSuggests   = 6
	maxShownTags  = 4
)

type suggestionsMsg struct {
	gen  int
	word string
	tags []string
}

type suggestTickMsg struct {
	gen int
}

func suggestTick(gen int) tea.Cmd {
	return tea.Tick(suggestDelay, func(time.Time) tea.Msg {
		return suggestTickMsg{gen: gen}
	})
}

func doAutocomplete(client api.Client, word string, local []string, gen int) tea.Cmd {
	return func() tea.Msg {
		tags, err := client.Autocomplete(word)
		if err != nil {
			tags = nil
		}
		merged := make([]string, 0, len(tags)+len(local))
		seen := map[string]bool{}
		for _, tag := range append(tags, local...) {
			tag = strings.TrimSpace(tag)
			if tag == "" || seen[tag] {
				continue
			}
			if !strings.HasPrefix(strings.ToLower(tag), strings.ToLower(word)) {
				continue
			}
			seen[tag] = true
			merged = append(merged, tag)
			if len(merged) == maxSuggests {
				break
			}
		}
		return suggestionsMsg{gen: gen, word: word, tags: merged}
	}
}

func splitWord(query string, cursor int) (start int, word string, dash bool, ok bool) {
	runes := []rune(query)
	if cursor > len(runes) {
		cursor = len(runes)
	}
	if cursor < 0 {
		cursor = 0
	}
	start = cursor
	for start > 0 && runes[start-1] != ' ' {
		start--
	}
	word = string(runes[start:cursor])
	if strings.HasPrefix(word, "-") {
		dash = true
		word = word[1:]
	}
	return start, word, dash, len(word) >= suggestMinLen
}

func (m *Model) localSuggestions(word string) []string {
	var tags []string
	lower := strings.ToLower(word)
	add := func(tag string) {
		tag = strings.TrimSpace(tag)
		if tag == "" || !strings.HasPrefix(strings.ToLower(tag), lower) {
			return
		}
		tags = append(tags, tag)
	}
	for i := len(m.history) - 1; i >= 0; i-- {
		for _, field := range strings.Fields(m.history[i]) {
			add(strings.TrimPrefix(field, "-"))
		}
	}
	for _, post := range m.posts {
		for _, field := range strings.Fields(post.Tags) {
			add(field)
		}
	}
	return tags
}

func (m *Model) refreshSuggestions() tea.Cmd {
	m.suggestGen++
	m.suggestIdx = 0
	m.suggestDone = ""
	m.suggestPick = false
	start, word, dash, ok := splitWord(m.query, m.inputCursor)
	m.suggestStart, m.suggestWord, m.suggestDash = start, word, dash
	if !ok {
		m.suggestions = nil
		return nil
	}
	m.suggestions = m.localSuggestions(word)
	return suggestTick(m.suggestGen)
}

func (m *Model) suggestLookup(word string) tea.Cmd {
	if word == "" {
		m.suggestions = nil
		return nil
	}
	local := m.localSuggestions(word)
	if len(local) > maxSuggests {
		local = local[:maxSuggests]
	}
	return doAutocomplete(m.client, word, local, m.suggestGen)
}

func (m *Model) clearSuggestions() {
	m.suggestGen++
	m.suggestions = nil
	m.suggestWord = ""
	m.suggestStart = 0
	m.suggestIdx = 0
	m.suggestDone = ""
	m.suggestPick = false
}

func (m *Model) suggestionAt(i int) string {
	if len(m.suggestions) == 0 {
		return ""
	}
	return m.suggestions[i%len(m.suggestions)]
}

func (m *Model) nextSuggestions() []string {
	if m.suggestIdx == 0 {
		return m.suggestions
	}
	rotated := make([]string, 0, len(m.suggestions))
	for i := range m.suggestions {
		rotated = append(rotated, m.suggestionAt(m.suggestIdx+i))
	}
	return rotated
}

func (m *Model) cycleSuggestion(delta int) bool {
	n := len(m.suggestions)
	if n == 0 {
		return false
	}
	m.suggestIdx = ((m.suggestIdx+delta)%n + n) % n
	return true
}

func (m *Model) pickSuggestion(delta int) bool {
	n := len(m.suggestions)
	if n == 0 {
		return false
	}
	if !m.suggestPick {
		m.suggestPick = true
		switch {
		case m.suggestDone != "":
			m.suggestIdx = ((m.suggestIdx+delta)%n + n) % n
		case delta < 0:
			m.suggestIdx = n - 1
		default:
			m.suggestIdx = 0
		}
		return true
	}
	return m.cycleSuggestion(delta)
}

func (m *Model) ghostSuggestion() string {
	if m.suggestDone != "" {
		return ""
	}
	if m.inputCursor != len([]rune(m.query)) {
		return ""
	}
	top := m.suggestionAt(m.suggestIdx)
	topRunes := []rune(top)
	wordRunes := []rune(m.suggestWord)
	if len(topRunes) <= len(wordRunes) {
		return ""
	}
	if !strings.EqualFold(string(topRunes[:len(wordRunes)]), m.suggestWord) {
		return ""
	}
	return string(topRunes[len(wordRunes):])
}

func (m *Model) acceptSuggestion() bool {
	if len(m.suggestions) == 0 || m.suggestWord == "" {
		return false
	}
	runes := []rune(m.query)
	end := m.inputCursor
	if m.suggestDone != "" {
		end = m.suggestStart + len([]rune(m.suggestDone))
	}
	if m.suggestStart > len(runes) || end < m.suggestStart || end > len(runes) {
		return false
	}
	tag := m.suggestionAt(m.suggestIdx)
	dash := ""
	if m.suggestDash {
		dash = "-"
	}
	rest := runes[end:]
	m.query = string(runes[:m.suggestStart]) + dash + tag + string(rest)
	m.inputCursor = m.suggestStart + len([]rune(dash+tag))
	m.suggestDone = dash + tag
	m.suggestPick = false
	return true
}
