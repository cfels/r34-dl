package tests

import (
	"strings"
	"testing"
	"time"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
	"moxiu/r34-dl/ui"

	tea "github.com/charmbracelet/bubbletea"
)

const cmdTimeout = 250 * time.Millisecond

type recordingClient struct {
	name     string
	searched []string
	counted  []string
}

func (c *recordingClient) SearchPosts(tags string, limit, page int) ([]api.Post, error) {
	c.searched = append(c.searched, tags)
	return []api.Post{}, nil
}

func (c *recordingClient) CountPosts(tags string) (int, error) {
	c.counted = append(c.counted, tags)
	return 0, nil
}

func (c *recordingClient) Name() string { return c.name }

func (c *recordingClient) Autocomplete(prefix string) ([]string, error) { return nil, nil }

type harness struct {
	sb  *recordingClient
	r34 *recordingClient
	m   ui.Model
}

func newHarness(cfg conf.Config, initialTags string) *harness {
	sb := &recordingClient{name: "safebooru"}
	r34 := &recordingClient{name: "rule34"}
	return &harness{
		sb:  sb,
		r34: r34,
		m: ui.NewModel(map[string]api.Client{
			"safebooru": sb,
			"rule34":    r34,
		}, cfg, initialTags, 30),
	}
}

func (h *harness) start(t *testing.T) *harness {
	t.Helper()
	h.run(h.m.Init())
	return h
}

func (h *harness) press(t *testing.T, keys ...string) *harness {
	t.Helper()
	for _, key := range keys {
		updated, cmd := h.m.Update(keyMsg(key))
		h.m = updated.(ui.Model)
		h.run(cmd)
	}
	return h
}

func (h *harness) run(cmd tea.Cmd) {
	queue := []tea.Cmd{cmd}
	for i := 0; i < 64 && len(queue) > 0; i++ {
		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		msg, ok := runCmd(next, cmdTimeout)
		if !ok {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		updated, follow := h.m.Update(msg)
		h.m = updated.(ui.Model)
		queue = append(queue, follow)
	}
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

func keyMsg(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+a":
		return tea.KeyMsg{Type: tea.KeyCtrlA}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func preview(t *testing.T, label, view string) {
	t.Helper()
	t.Logf("\n┌─ %s %s\n%s\n└%s", label, line(60-len(label)), view, line(62))
}

func line(n int) string {
	if n < 0 {
		n = 0
	}
	r := make([]rune, n)
	for i := range r {
		r[i] = '─'
	}
	return string(r)
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
