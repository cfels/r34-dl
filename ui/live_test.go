package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLiveCounterReportsSiteTotalForFilteredSearch(t *testing.T) {
	if os.Getenv("R34_LIVE") == "" {
		t.Skip("set R34_LIVE=1 to run")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("locating home: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	saved, err := conf.Load()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if saved.APIKey == "" || saved.UserID == "" {
		t.Skip("rule34 needs stored api credentials")
	}
	cfg := conf.Config{
		AgeVerified: true,
		ActiveAPI:   "rule34",
		FilterAI:    true,
		APIKey:      saved.APIKey,
		UserID:      saved.UserID,
	}
	clients := map[string]api.Client{
		"rule34":    api.NewRule34Client(cfg.UserID, cfg.APIKey),
		"safebooru": api.NewSafebooruClient(),
	}
	for _, query := range []string{"remielle_dan", "remielle_dan video", "jane_doe_(zenless_zone_zero)"} {
		m := NewModel(clients, cfg, query, 30)
		m.width, m.height = 120, 40

		msg, ok := runCmd(m.startSearch(), 30*time.Second)
		if !ok {
			t.Fatal("search timed out")
		}
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			t.Fatalf("search returned %T, want a batch of commands", msg)
		}
		for _, cmd := range batch {
			msg, ok := runCmd(cmd, 30*time.Second)
			if !ok || msg == nil {
				continue
			}
			if _, tick := msg.(countTickMsg); tick {
				continue
			}
			updated, _ := m.Update(msg)
			m = updated.(Model)
		}
		if !m.totalKnown {
			t.Fatalf("%s: the counter was not reported", query)
		}
		want, err := clients["rule34"].CountPosts(query)
		if err != nil {
			t.Fatalf("%s: site count: %v", query, err)
		}
		t.Logf("%s: counter=%d website=%d", query, m.totalCount, want)
		if m.totalCount != want {
			t.Errorf("%s: counter = %d, the site shows %d", query, m.totalCount, want)
		}
		if label := fmt.Sprintf("[1/%d]", want); !strings.Contains(m.View(), label) {
			t.Errorf("%s: view should show %q:\n%s", query, label, m.View())
		}
	}
}

func TestLivePredictionsAcrossVideoSites(t *testing.T) {
	if os.Getenv("R34_LIVE") == "" {
		t.Skip("set R34_LIVE=1 to run")
	}
	clients := map[string]api.Client{
		"safebooru": api.NewSafebooruClient(),
		"rule34":    api.NewRule34Client("", ""),
		"pornhub":   api.NewPornHubClient(),
		"xvideos":   api.NewXVideosClient(),
		"xhamster":  api.NewXHamsterClient(),
	}
	for _, site := range []string{"pornhub", "xvideos", "xhamster"} {
		m := NewModel(clients, conf.Config{AgeVerified: true, ActiveAPI: site}, "", 30)
		m.width, m.height = 120, 40
		m.state = stateSearch
		m = typeKeys(t, m, "b", "i", "g")
		msg, ok := runCmd(m.suggestLookup(m.suggestWord), 30*time.Second)
		if !ok {
			t.Fatalf("%s: prediction lookup timed out", site)
		}
		updated, _ := m.Update(msg)
		m = updated.(Model)
		if len(m.suggestions) == 0 {
			t.Fatalf("%s: no predictions for big", site)
		}
		if !strings.Contains(m.View(), "tab/Y") {
			t.Errorf("%s: prediction bar should advertise the accept key", site)
		}
		m = typeKeys(t, m, "tab")
		t.Logf("%s: accepted %q", site, m.query)
		if !strings.HasPrefix(strings.ToLower(m.query), "big ") {
			t.Errorf("%s: tab accepted %q, want one of the predictions", site, m.query)
		}
	}
}
