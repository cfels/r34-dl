package ui

import (
	"reflect"
	"strings"
	"testing"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
)

type pagedClient struct {
	pages  map[int][]api.Post
	asked  []int
	counts map[string]int
}

func (c *pagedClient) SearchPosts(tags string, limit, page int) ([]api.Post, error) {
	c.asked = append(c.asked, page)
	return c.pages[page], nil
}

func (c *pagedClient) CountPosts(tags string) (int, error) {
	if c.counts == nil {
		return 0, nil
	}
	return c.counts[tags], nil
}

func (c *pagedClient) Name() string                          { return "paged" }
func (c *pagedClient) Autocomplete(string) ([]string, error) { return nil, nil }

func listModel(client api.Client, limit int) Model {
	m := NewModel(testClients(client), conf.Config{AgeVerified: true, ActiveAPI: "safebooru"}, "touhou", limit)
	m.width, m.height = 100, 30
	m.state = stateList
	m.posts = []api.Post{{ID: 9, FileURL_: "/missing/nine.mp4"}}
	return m
}

func bulkModel(client api.Client, limit int) Model {
	return listModel(client, limit).WithBulk(true)
}

func TestBulkModeComesFromTheFlag(t *testing.T) {
	m := listModel(&pagedClient{}, 30)

	if strings.Contains(m.View(), "[bulk]") || strings.Contains(m.View(), "bulk download") {
		t.Errorf("results should not be in bulk mode by default:\n%s", m.View())
	}

	m = m.WithBulk(true)
	view := m.View()
	if !strings.Contains(view, "[bulk]") || !strings.Contains(view, "enter: bulk download") {
		t.Errorf("bulk mode should label the results:\n%s", view)
	}
	m.state = stateSearch
	if !strings.Contains(m.View(), "bulk download mode (--bulk)") {
		t.Errorf("search screen should show the bulk mode:\n%s", m.View())
	}
}

func TestBulkModeOpensTheBulkForm(t *testing.T) {
	m := bulkModel(&pagedClient{}, 30)
	m.query = "remielle_dan"

	m = typeKeys(t, m, "enter")
	if m.state != stateBulk || m.bulkStage != bulkForm {
		t.Fatalf("state = %d stage = %d, want the bulk form", m.state, m.bulkStage)
	}
	if m.bulkTags != "remielle_dan" {
		t.Errorf("form tags = %q, want the searched tags pre-filled", m.bulkTags)
	}
	if m.bulkInput != "" {
		t.Errorf("form count = %q, want it empty so the default applies", m.bulkInput)
	}
	if !strings.Contains(m.View(), "tags:") || !strings.Contains(m.View(), "[default 30]") {
		t.Errorf("form should show the tags and default count:\n%s", m.View())
	}

	m = typeKeys(t, m, "esc")
	if m.state != stateList {
		t.Error("esc should go back to the results")
	}
}

func TestBulkFormSwitchesSite(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	client := &pagedClient{}
	m := bulkModel(client, 30)
	m.query = "touhou"
	m.openBulkForm()
	m.bulkFocus = 1
	m.bulkInput = "7"

	m = typeKeys(t, m, "tab")
	if m.cfg.ActiveAPI != "rule34" {
		t.Errorf("active site = %q, want the form to switch sites", m.cfg.ActiveAPI)
	}
	if m.bulkTags != "touhou" || m.bulkInput != "7" {
		t.Errorf("switching sites should keep the typed values, got %q and %q", m.bulkTags, m.bulkInput)
	}
}

func TestBulkFormOpensWithoutResults(t *testing.T) {
	m := listModel(&pagedClient{}, 30)
	m.posts = nil
	m = m.WithBulk(true)

	m = typeKeys(t, m, "enter")
	if m.state != stateBulk {
		t.Fatalf("state = %d, want the bulk form even with an empty result list", m.state)
	}
}

func TestBulkFormDownloadsRequestedCount(t *testing.T) {
	client := &pagedClient{pages: map[int][]api.Post{
		0: {{ID: 1, FileURL_: "/missing/one.mp4"}, {ID: 2, FileURL_: "/missing/two.mp4"}},
		1: {{ID: 3, FileURL_: "/missing/three.mp4"}},
	}}
	m := bulkModel(client, 30)

	m = typeKeys(t, m, "enter", "enter", "3", "enter")

	if !reflect.DeepEqual(client.asked, []int{0, 1}) {
		t.Errorf("walker asked pages %v, want the pages that cover the count", client.asked)
	}
	if m.bulkTotal != 3 {
		t.Errorf("bulk download covers %d posts, want 3", m.bulkTotal)
	}
	if m.bulkActive {
		t.Error("bulk download should be finished")
	}
	if m.bulkDone != 0 || m.bulkFailed != 3 {
		t.Errorf("bulk result = %d saved, %d failed, want 3 refused downloads", m.bulkDone, m.bulkFailed)
	}
	if !strings.Contains(m.View(), "done: 0 saved, 3 failed") {
		t.Errorf("results should report the bulk download:\n%s", m.View())
	}
	if !strings.Contains(m.View(), "failed #1") {
		t.Errorf("results should log every download like the CLI flow:\n%s", m.View())
	}
}

func TestBulkFormDefaultsToResultLimit(t *testing.T) {
	client := &pagedClient{pages: map[int][]api.Post{
		0: {{ID: 1, FileURL_: "/missing/one.mp4"}, {ID: 2, FileURL_: "/missing/two.mp4"}},
	}}
	m := bulkModel(client, 2)

	m = typeKeys(t, m, "enter", "enter", "enter")

	if m.bulkActive {
		t.Fatal("bulk download should have finished")
	}
	if m.bulkTotal != 2 {
		t.Errorf("bulk download covers %d posts, want the two results", m.bulkTotal)
	}
}

func TestBulkRunWaitsForDownloadsToFinish(t *testing.T) {
	m := bulkModel(&pagedClient{}, 30)
	m.state = stateBulk
	m.bulkStage = bulkRunning
	m.bulkActive = true
	m.bulkTotal = 3

	for _, key := range []string{"q", "esc", "enter", "0"} {
		updated, cmd := m.Update(keyByName(key))
		m = updated.(Model)
		if cmd != nil {
			t.Errorf("%q should not act while downloads run", key)
		}
		if m.state != stateBulk || m.bulkStage != bulkRunning {
			t.Fatalf("%q left the running bulk screen", key)
		}
	}
	if !strings.Contains(m.View(), "waiting for downloads to finish") {
		t.Errorf("running screen should say it is waiting:\n%s", m.View())
	}
}

func TestBulkCountIsCapped(t *testing.T) {
	m := bulkModel(&pagedClient{}, 30)
	m.bulkInput = "999999"
	if got := m.bulkCount(); got != maxBulkPosts {
		t.Errorf("bulkCount = %d, want the cap %d", got, maxBulkPosts)
	}
	m.bulkInput = "0"
	if got := m.bulkCount(); got != 30 {
		t.Errorf("bulkCount = %d, want the default page size", got)
	}
	m.bulkInput = ""
	if got := m.bulkCount(); got != 30 {
		t.Errorf("bulkCount = %d, want the default page size", got)
	}
}

func TestEnterDownloadsOnePostWithoutBulkMode(t *testing.T) {
	m := newListModel()
	updated, cmd := m.Update(keyByName("enter"))
	m = updated.(Model)

	if m.bulkActive || m.state == stateBulk {
		t.Error("single download mode should not start a bulk download")
	}
	if cmd == nil {
		t.Fatal("enter should still download the selected post")
	}
	if !strings.Contains(m.notice, "downloading #1") {
		t.Errorf("notice = %q, want the selected post", m.notice)
	}
}

func TestCountCommandsAskForTheSiteTotal(t *testing.T) {
	client := &recordingClient{}
	m := NewModel(
		testClients(client),
		conf.Config{AgeVerified: true, ActiveAPI: "rule34", FilterAI: true},
		"remielle_dan video",
		30,
	)
	m.width, m.height = 100, 30
	m.state = stateSearch

	cmd := m.startSearch()
	m = drainCommands(t, m, cmd)

	want := []string{"remielle_dan video"}
	if !reflect.DeepEqual(client.counted, want) {
		t.Errorf("counts used %v, want the tags the site is asked about", client.counted)
	}
}

func TestCounterShowsTheSiteTotalWithFiltersOn(t *testing.T) {
	m := newListModel()
	m.cfg.ActiveAPI = "rule34"
	m.cfg.FilterAI = true
	m.query = "remielle_dan"
	m.state = stateList

	updated, _ := m.Update(totalCountMsg{count: 2789, gen: m.searchGen})
	m = updated.(Model)

	if !strings.Contains(m.View(), "[1/2789]") {
		t.Errorf("counter should show the real site total:\n%s", m.View())
	}
	if strings.Contains(m.View(), " of ") {
		t.Errorf("counter should be a single total:\n%s", m.View())
	}
}

func TestCounterStaysSingleWhenNothingIsFiltered(t *testing.T) {
	m := newListModel()
	m.cfg.ActiveAPI = "safebooru"
	m.query = "touhou"
	m.state = stateList

	updated, _ := m.Update(totalCountMsg{count: 2789, gen: m.searchGen})
	m = updated.(Model)

	if !strings.Contains(m.View(), "[1/2789]") {
		t.Errorf("counter should show the result total:\n%s", m.View())
	}
}
