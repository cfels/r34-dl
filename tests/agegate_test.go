package tests

import (
	"fmt"
	"testing"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
	"moxiu/r34-dl/ui"

	tea "github.com/charmbracelet/bubbletea"
)

type mockClient struct{ name string }

func (m *mockClient) SearchPosts(tags string, limit, page int) ([]api.Post, error) {
	return []api.Post{}, nil
}
func (m *mockClient) CountPosts(tags string) (int, error) { return 0, nil }
func (m *mockClient) Name() string                        { return m.name }

func newTestModel() ui.Model {
	sb := &mockClient{name: "safebooru"}
	r34 := &mockClient{name: "rule34"}
	cfg := conf.Config{
		AgeVerified: false,
		ActiveAPI:   "safebooru",
	}
	return ui.NewModel(sb, r34, cfg, "", 30)
}

func sendKey(m ui.Model, key string) ui.Model {
	var msg tea.KeyMsg
	switch key {
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+c":
		msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	updated, _ := m.Update(msg)
	return updated.(ui.Model)
}

func preview(t *testing.T, label, view string) {
	t.Helper()
	t.Logf("\n┌─ %s %s\n%s\n└%s", label, line(60-len(label)), view, line(62))
}

func line(n int) string {
	r := make([]rune, n)
	for i := range r {
		r[i] = '─'
	}
	return string(r)
}

func TestAgeGate_InitialState(t *testing.T) {
	m := newTestModel()
	view := m.View()
	preview(t, "age gate popup", view)
	if view == "" {
		t.Fatal("got empty view")
	}
	if !contains(view, "18") {
		t.Error("should mention '18'")
	}
	if !contains(view, "yes (rule34)") {
		t.Error("should show yes (rule34)")
	}
	if !contains(view, "no (safebooru)") {
		t.Error("should show no (safebooru)")
	}
}

func TestAgeGate_DefaultIsYes(t *testing.T) {
	m := newTestModel()
	preview(t, "default selection", m.View())
	m = sendKey(m, "enter")
	preview(t, "after enter (no arrow pressed)", m.View())
	if m.Cfg().ActiveAPI != "rule34" {
		t.Errorf("default should select rule34, got %q", m.Cfg().ActiveAPI)
	}
	t.Logf("config: AgeVerified=%v  ActiveAPI=%s", m.Cfg().AgeVerified, m.Cfg().ActiveAPI)
}

func TestAgeGate_RightThenEnter(t *testing.T) {
	m := newTestModel()
	preview(t, "before  →  press right", m.View())
	m = sendKey(m, "right")
	preview(t, "after   →  press right", m.View())
	m = sendKey(m, "enter")
	preview(t, "after   →  press enter", m.View())
	if m.Cfg().ActiveAPI != "safebooru" {
		t.Errorf("expected safebooru after right+enter, got %q", m.Cfg().ActiveAPI)
	}
	t.Logf("config: AgeVerified=%v  ActiveAPI=%s", m.Cfg().AgeVerified, m.Cfg().ActiveAPI)
}

func TestAgeGate_RightLeftThenEnter(t *testing.T) {
	m := newTestModel()
	m = sendKey(m, "right")
	preview(t, "after right", m.View())
	m = sendKey(m, "left")
	preview(t, "after left (back to yes)", m.View())
	m = sendKey(m, "enter")
	if m.Cfg().ActiveAPI != "rule34" {
		t.Errorf("expected rule34 after right+left+enter, got %q", m.Cfg().ActiveAPI)
	}
	t.Logf("config: AgeVerified=%v  ActiveAPI=%s", m.Cfg().AgeVerified, m.Cfg().ActiveAPI)
}

func TestAgeGate_QuitKey(t *testing.T) {
	m := newTestModel()
	preview(t, "before  →  press q", m.View())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	t.Logf("quit command returned: %v", cmd != nil)
	if cmd == nil {
		t.Error("expected quit command")
	}
}

func TestAgeGate_CtrlC(t *testing.T) {
	m := newTestModel()
	preview(t, "before  →  ctrl+c", m.View())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	t.Logf("quit command returned: %v", cmd != nil)
	if cmd == nil {
		t.Error("expected quit command")
	}
}

func TestAgeGate_IrrelevantKeys(t *testing.T) {
	m := newTestModel()
	for _, key := range []string{"a", "b", "x", "1"} {
		after := sendKey(m, key)
		preview(t, fmt.Sprintf("after  →  press %q", key), after.View())
		if !contains(after.View(), "18") {
			t.Errorf("pressing %q should not leave the age gate", key)
		}
	}
}

func TestAgeGate_SkippedWhenAlreadyVerified(t *testing.T) {
	sb := &mockClient{name: "safebooru"}
	r34 := &mockClient{name: "rule34"}
	cfg := conf.Config{
		AgeVerified: true,
		ActiveAPI:   "safebooru",
	}
	m := ui.NewModel(sb, r34, cfg, "", 30)
	preview(t, "already verified  →  initial view", m.View())
	if contains(m.View(), "are you 18") {
		t.Error("age gate should be skipped")
	}
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
