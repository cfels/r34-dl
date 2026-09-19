package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
)

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
