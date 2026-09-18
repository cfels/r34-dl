package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
	"moxiu/r34-dl/ui"
)

var switchOrder = []string{"safebooru", "rule34", "pornhub", "xvideos", "xhamster"}

func newSiteHarness(cfg conf.Config, initialTags string) (*harness, map[string]*recordingClient) {
	sites := map[string]*recordingClient{}
	clients := map[string]api.Client{}
	for _, name := range switchOrder {
		client := &recordingClient{name: name}
		sites[name] = client
		clients[name] = client
	}
	h := &harness{
		sb:  sites["safebooru"],
		r34: sites["rule34"],
		m:   ui.NewModel(clients, cfg, initialTags, 30),
	}
	h.m.SetSize(100, 30)
	return h, sites
}

func TestTabCyclesThroughEverySite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h, sites := newSiteHarness(conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "touhou")
	h.start(t)

	for i := 1; i <= len(switchOrder); i++ {
		want := switchOrder[i%len(switchOrder)]
		h.press(t, "tab")
		if got := h.m.Cfg().ActiveAPI; got != want {
			t.Fatalf("after %d tab presses active site = %q, want %q", i, got, want)
		}
		if len(sites[want].searched) == 0 {
			t.Errorf("%s was switched to but never searched", want)
		}
	}
}

func TestShiftTabCyclesBackwards(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h, sites := newSiteHarness(conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "touhou")
	h.start(t)

	h.press(t, "shift+tab")
	if got := h.m.Cfg().ActiveAPI; got != "xhamster" {
		t.Fatalf("active site = %q, want xhamster", got)
	}
	if len(sites["xhamster"].searched) == 0 {
		t.Error("xhamster was switched to but never searched")
	}

	h.press(t, "shift+tab")
	if got := h.m.Cfg().ActiveAPI; got != "xvideos" {
		t.Fatalf("active site = %q, want xvideos", got)
	}
}

func TestSwitcherUsesActiveClientForSearch(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h, sites := newSiteHarness(conf.Config{AgeVerified: true, ActiveAPI: "pornhub"}, "touhou")
	h.start(t)

	if got := sites["pornhub"].searched; len(got) != 1 || got[0] != "touhou" {
		t.Errorf("pornhub searches = %v, want the query", got)
	}
	if len(sites["safebooru"].searched) != 0 {
		t.Error("the inactive site should not search")
	}
	if !strings.Contains(h.m.View(), "[pornhub]") {
		t.Errorf("view should label the active site:\n%s", h.m.View())
	}
}

func TestSwitcherSkipsAdultSitesWithoutAgeCheck(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h, sites := newSiteHarness(conf.Config{AgeVerified: false, ActiveAPI: "safebooru"}, "")
	h.start(t)

	h.press(t, "tab", "tab", "shift+tab")
	if got := h.m.Cfg().ActiveAPI; got != "safebooru" {
		t.Errorf("active site = %q, want safebooru while unverified", got)
	}
	for _, name := range []string{"rule34", "pornhub", "xvideos", "xhamster"} {
		if len(sites[name].searched) != 0 {
			t.Errorf("%s is age restricted but was searched anyway", name)
		}
	}
}

func TestSwitchPersistsToConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	h, _ := newSiteHarness(conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "")
	h.start(t)
	h.press(t, "tab")

	data, err := os.ReadFile(filepath.Join(home, "r34-dl", "config.json"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(data), `"active_api": "rule34"`) {
		t.Errorf("switch was not saved: %s", data)
	}
}

func TestSearchViewLabelsEverySite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h, _ := newSiteHarness(conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "")
	h.m.SetState(ui.StateSearch)

	for _, name := range switchOrder {
		h.m.SwitchAPI(name)
		if !strings.Contains(h.m.View(), "["+name+"]") {
			t.Errorf("view should label %s:\n%s", name, h.m.View())
		}
	}
}
