package ui

import (
	"strings"
	"testing"
	"time"

	"moxiu/r34-dl/conf"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeTagClient struct {
	stubClient
	tags map[string][]string
}

func (f fakeTagClient) Autocomplete(prefix string) ([]string, error) {
	return f.tags[prefix], nil
}

func drainCommands(t *testing.T, m Model, cmds ...tea.Cmd) Model {
	t.Helper()
	for i := 0; i < 24 && len(cmds) > 0; i++ {
		cmd := cmds[0]
		cmds = cmds[1:]
		if cmd == nil {
			continue
		}
		msg, ok := runCmd(cmd, 400*time.Millisecond)
		if !ok {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			cmds = append(cmds, batch...)
			continue
		}
		updated, next := m.Update(msg)
		m = updated.(Model)
		if next != nil {
			cmds = append(cmds, next)
		}
	}
	return m
}

func runCmd(cmd tea.Cmd, timeout time.Duration) (tea.Msg, bool) {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg, true
	case <-time.After(timeout):
		return nil, false
	}
}

func typeKeys(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, key := range keys {
		var msg tea.KeyMsg
		if len([]rune(key)) == 1 {
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		} else {
			msg = keyByName(key)
		}
		updated, cmd := m.Update(msg)
		m = drainCommands(t, updated.(Model), cmd)
	}
	return m
}

func keyByName(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "ctrl+n":
		return tea.KeyMsg{Type: tea.KeyCtrlN}
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+a":
		return tea.KeyMsg{Type: tea.KeyCtrlA}
	case "shift+down":
		return tea.KeyMsg{Type: tea.KeyShiftDown}
	case "shift+up":
		return tea.KeyMsg{Type: tea.KeyShiftUp}
	case "shift+right":
		return tea.KeyMsg{Type: tea.KeyShiftRight}
	case "shift+left":
		return tea.KeyMsg{Type: tea.KeyShiftLeft}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func suggestModel(tags map[string][]string, history []string) Model {
	client := fakeTagClient{tags: tags}
	m := NewModel(testClients(client), conf.Config{
		AgeVerified:   true,
		ActiveAPI:     "safebooru",
		SearchHistory: history,
	}, "", 30)
	m.width, m.height = 100, 30
	m.state = stateSearch
	return m
}

func TestSuggestionsCompleteCurrentWord(t *testing.T) {
	m := suggestModel(map[string][]string{
		"cat_g": {"cat_girl", "cat_gloves", "cat_girls"},
	}, nil)

	m = typeKeys(t, m, "c", "a", "t", "_", "g")
	if m.suggestWord != "cat_g" {
		t.Fatalf("suggestWord = %q, want cat_g", m.suggestWord)
	}
	if len(m.suggestions) != 3 {
		t.Fatalf("got %d suggestions, want 3: %v", len(m.suggestions), m.suggestions)
	}
	if ghost := m.ghostSuggestion(); ghost != "irl" {
		t.Errorf("ghost = %q, want %q", ghost, "irl")
	}
	view := m.View()
	if !strings.Contains(view, "cat_g") || !strings.Contains(view, "cat_girl") {
		t.Error("view should show the typed word and the suggested tag")
	}
	if !strings.Contains(view, "Y") {
		t.Error("view should show the completion key")
	}

	m = typeKeys(t, m, "Y")
	if m.query != "cat_girl" {
		t.Fatalf("after completing, query = %q, want cat_girl", m.query)
	}
	if m.inputCursor != len([]rune("cat_girl")) {
		t.Errorf("cursor = %d, want %d", m.inputCursor, len([]rune("cat_girl")))
	}
	if ghost := m.ghostSuggestion(); ghost != "" {
		t.Errorf("ghost = %q, want none after completing", ghost)
	}

	m = typeKeys(t, m, "shift+down")
	m = typeKeys(t, m, "Y")
	if m.query != "cat_gloves" {
		t.Fatalf("shift+down then Y should accept the next tag, query = %q", m.query)
	}
}

func TestShiftArrowsPickPredictions(t *testing.T) {
	m := suggestModel(map[string][]string{
		"cat_g": {"cat_girl", "cat_gloves", "cat_girls"},
	}, nil)
	m = typeKeys(t, m, "c", "a", "t", "_", "g")
	if ghost := m.ghostSuggestion(); ghost != "irl" {
		t.Fatalf("first ghost = %q, want irl", ghost)
	}

	m = typeKeys(t, m, "shift+down")
	if !m.suggestPick {
		t.Fatal("shift+down should start picking from the bar")
	}
	if ghost := m.ghostSuggestion(); ghost != "irl" {
		t.Errorf("after entering the bar ghost = %q, want irl", ghost)
	}
	if !strings.Contains(m.View(), "▸ cat_girl") {
		t.Error("picked entry should be marked in the bar")
	}

	m = typeKeys(t, m, "shift+right")
	if ghost := m.ghostSuggestion(); ghost != "loves" {
		t.Errorf("shift+right should move forward, ghost = %q", ghost)
	}
	m = typeKeys(t, m, "shift+down")
	if ghost := m.ghostSuggestion(); ghost != "irls" {
		t.Errorf("shift+down should move forward, ghost = %q", ghost)
	}
	m = typeKeys(t, m, "shift+left")
	if ghost := m.ghostSuggestion(); ghost != "loves" {
		t.Errorf("shift+left should move back, ghost = %q", ghost)
	}
	m = typeKeys(t, m, "shift+right")
	if ghost := m.ghostSuggestion(); ghost != "irls" {
		t.Errorf("shift+right should move forward again, ghost = %q", ghost)
	}

	view := m.View()
	if !strings.Contains(view, "cat_girls") {
		t.Error("view should show the highlighted tag")
	}

	m = typeKeys(t, m, "enter")
	if m.query != "cat_girls" {
		t.Fatalf("query = %q, want the picked tag cat_girls", m.query)
	}
	if m.inputCursor != len([]rune("cat_girls")) {
		t.Errorf("cursor = %d, want %d", m.inputCursor, len([]rune("cat_girls")))
	}
	if m.suggestPick {
		t.Error("picking should end once the tag is accepted")
	}

	m = typeKeys(t, m, "enter")
	if m.state != stateList {
		t.Errorf("state = %v, want the search to run on the next enter", m.state)
	}
}

func TestPlainUpWalksBackOutOfPredictionBar(t *testing.T) {
	m := suggestModel(map[string][]string{
		"cat_g": {"cat_girl", "cat_gloves", "cat_girls"},
	}, nil)
	m = typeKeys(t, m, "c", "a", "t", "_", "g")
	m = typeKeys(t, m, "shift+down")
	m = typeKeys(t, m, "shift+down")
	if !m.suggestPick {
		t.Fatal("shift+down should be picking")
	}

	m = typeKeys(t, m, "up")
	if !m.suggestPick {
		t.Error("plain up should step back through the picks first")
	}
	if m.suggestIdx != 0 {
		t.Errorf("suggestIdx = %d, want back to the first suggestion", m.suggestIdx)
	}

	m = typeKeys(t, m, "up")
	if m.suggestPick {
		t.Error("plain up on the first pick should leave the prediction bar")
	}
	if m.query != "cat_g" {
		t.Errorf("typing box changed to %q, want cat_g", m.query)
	}
	if m.suggestIdx != 0 {
		t.Errorf("suggestIdx = %d, want back to the first suggestion", m.suggestIdx)
	}
}

func TestPlainDownPicksTagAndUpRecallsHistory(t *testing.T) {
	m := suggestModel(nil, []string{"cat_girl solo"})
	m = typeKeys(t, m, "c", "a", "t", "_", "g")
	m = typeKeys(t, m, "down")
	if !m.suggestPick {
		t.Fatal("plain down should pick from the bar")
	}
	if got := m.suggestionAt(m.suggestIdx); got != "cat_girl" {
		t.Errorf("picked %q, want cat_girl", got)
	}

	m = suggestModel(nil, []string{"cat_girl solo"})
	m = typeKeys(t, m, "up")
	if m.query != "cat_girl solo" {
		t.Fatalf("query = %q, want the recalled history entry", m.query)
	}
	m = typeKeys(t, m, "ctrl+n")
	if m.query != "" {
		t.Fatalf("ctrl+n should clear back to an empty query, got %q", m.query)
	}
}

func TestTabAcceptsTopPrediction(t *testing.T) {
	m := suggestModel(map[string][]string{
		"cat_g": {"cat_girl", "cat_gloves"},
	}, nil)
	m = typeKeys(t, m, "c", "a", "t", "_", "g")
	m = typeKeys(t, m, "tab")
	if m.query != "cat_girl" {
		t.Fatalf("tab should accept the top prediction, query = %q", m.query)
	}
	if m.cfg.ActiveAPI != "safebooru" {
		t.Errorf("tab picked a tag, it should not have switched to %q", m.cfg.ActiveAPI)
	}
	if m.inputCursor != len([]rune("cat_girl")) {
		t.Errorf("cursor = %d, want %d", m.inputCursor, len([]rune("cat_girl")))
	}
}

func TestTabSwitchesSiteWhenNothingToAccept(t *testing.T) {
	m := suggestModel(nil, nil)
	m = typeKeys(t, m, "tab")
	if m.cfg.ActiveAPI != "rule34" {
		t.Errorf("active site = %q, want the tab to cycle sites", m.cfg.ActiveAPI)
	}
}

func TestRightArrowCompletesPrediction(t *testing.T) {
	m := suggestModel(map[string][]string{
		"cat_g": {"cat_girl"},
	}, nil)
	m = typeKeys(t, m, "c", "a", "t", "_", "g")
	if ghost := m.ghostSuggestion(); ghost != "irl" {
		t.Fatalf("ghost = %q, want irl", ghost)
	}
	m = typeKeys(t, m, "right")
	if m.query != "cat_girl" {
		t.Fatalf("right arrow should take the inline completion, query = %q", m.query)
	}
	if m.inputCursor != len([]rune("cat_girl")) {
		t.Errorf("cursor = %d, want %d", m.inputCursor, len([]rune("cat_girl")))
	}
}

func TestEscLeavesPredictionBar(t *testing.T) {
	m := suggestModel(map[string][]string{
		"cat_g": {"cat_girl", "cat_gloves"},
	}, nil)
	m = typeKeys(t, m, "c", "a", "t", "_", "g")
	m = typeKeys(t, m, "down")
	if !m.suggestPick {
		t.Fatal("plain down should pick from the bar")
	}
	m = typeKeys(t, m, "esc")
	if m.suggestPick {
		t.Error("esc should leave the prediction bar")
	}
	if m.query != "cat_g" {
		t.Errorf("query = %q, want the typed word untouched", m.query)
	}
}

func TestSuggestionAcceptsWithEnterAndDash(t *testing.T) {
	m := suggestModel(map[string][]string{
		"cat_g": {"cat_girl"},
	}, nil)

	m = typeKeys(t, m, "c", "a", "t", "_", "g")
	m = typeKeys(t, m, "shift+down")
	m = typeKeys(t, m, "enter")
	if m.query != "cat_girl" {
		t.Fatalf("enter should accept the picked tag, query = %q", m.query)
	}

	m = suggestModel(map[string][]string{"cat_g": {"cat_girl"}}, nil)
	m = typeKeys(t, m, "-", "c", "a", "t", "_", "g")
	if !m.suggestDash {
		t.Fatal("negative search prefix should be tracked")
	}
	m = typeKeys(t, m, "Y")
	if m.query != "-cat_girl" {
		t.Fatalf("query = %q, want -cat_girl", m.query)
	}
}

func TestSuggestionUsesHistoryWhenOffline(t *testing.T) {
	m := suggestModel(nil, []string{"cat_girl solo", "landscape"})
	m = typeKeys(t, m, "c", "a", "t", "_")
	if len(m.suggestions) == 0 {
		t.Fatal("history should provide suggestions without a network result")
	}
	if m.suggestions[0] != "cat_girl" {
		t.Errorf("suggestions = %v, want cat_girl first", m.suggestions)
	}
	m = typeKeys(t, m, "Y")
	if m.query != "cat_girl" {
		t.Fatalf("query = %q, want cat_girl", m.query)
	}
}

func TestSuggestionsIgnoreStaleResults(t *testing.T) {
	m := suggestModel(map[string][]string{"cat_g": {"cat_girl"}}, nil)
	m = typeKeys(t, m, "c", "a", "t", "_", "g")

	stale := suggestionsMsg{gen: m.suggestGen - 1, word: "cat_g", tags: []string{"wrong_tag"}}
	updated, _ := m.Update(stale)
	m = updated.(Model)
	if len(m.suggestions) != 1 || m.suggestions[0] != "cat_girl" {
		t.Errorf("stale suggestions were applied: %v", m.suggestions)
	}

	mismatched := suggestionsMsg{gen: m.suggestGen, word: "other", tags: []string{"wrong_tag"}}
	updated, _ = m.Update(mismatched)
	m = updated.(Model)
	if len(m.suggestions) != 1 || m.suggestions[0] != "cat_girl" {
		t.Errorf("suggestions for another word were applied: %v", m.suggestions)
	}
}

func TestTypingYStillWorksAfterSuggestionsClear(t *testing.T) {
	m := suggestModel(map[string][]string{"y": {"yuri"}}, nil)
	m = typeKeys(t, m, "y")
	if m.query != "y" {
		t.Fatalf("query = %q, want y", m.query)
	}
	m = typeKeys(t, m, "u", "r", "i")
	if m.query != "yuri" {
		t.Fatalf("query = %q, want yuri typed out normally", m.query)
	}
	m = typeKeys(t, m, "enter")
	if m.state != stateList {
		t.Errorf("state = %v, want the search to run", m.state)
	}
}

func TestExactWordIsNotOfferedAsPrediction(t *testing.T) {
	m := suggestModel(map[string][]string{
		"big": {"big", "big ass", "big tits"},
	}, []string{"big"})
	m = typeKeys(t, m, "b", "i", "g")
	for _, tag := range m.suggestions {
		if strings.EqualFold(tag, "big") {
			t.Fatalf("suggestions = %v, want the typed word filtered out", m.suggestions)
		}
	}
	if len(m.suggestions) != 2 {
		t.Fatalf("suggestions = %v, want the two longer tags", m.suggestions)
	}
	m = typeKeys(t, m, "tab")
	if m.query != "big ass" {
		t.Fatalf("query = %q, want tab to accept the first real prediction", m.query)
	}
}
