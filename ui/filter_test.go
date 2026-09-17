package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"moxiu/r34-dl/api"
	"moxiu/r34-dl/conf"
)

type recordingClient struct {
	stubClient
	searched []string
	counted  []string
}

func (c *recordingClient) SearchPosts(tags string, limit, page int) ([]api.Post, error) {
	c.searched = append(c.searched, tags)
	return nil, nil
}

func (c *recordingClient) CountPosts(tags string) (int, error) {
	c.counted = append(c.counted, tags)
	return 0, nil
}

func TestAPITagsAddsAIFilter(t *testing.T) {
	m := newListModel()
	m.query = "touhou"
	m.cfg.ActiveAPI = "rule34"

	if got := m.apiTags(); got != "touhou" {
		t.Errorf("apiTags = %q, want touhou", got)
	}
	m.cfg.FilterAI = true
	if got := m.apiTags(); got != "touhou -ai_generated" {
		t.Errorf("apiTags = %q, want touhou -ai_generated", got)
	}
	m.query = "  "
	if got := m.apiTags(); got != "-ai_generated" {
		t.Errorf("apiTags = %q, want -ai_generated", got)
	}

	m.cfg.ActiveAPI = "safebooru"
	m.query = "touhou"
	if got := m.apiTags(); got != "touhou" {
		t.Errorf("apiTags = %q, want the filter left to rule34", got)
	}
}

func TestSearchUsesAIFilter(t *testing.T) {
	client := &recordingClient{}
	m := NewModel(client, client, conf.Config{AgeVerified: true, ActiveAPI: "rule34"}, "touhou", 30)
	m.width, m.height = 100, 30
	m.state = stateList

	m = typeKeys(t, m, "ctrl+a")
	if !m.cfg.FilterAI {
		t.Fatal("ctrl+a should enable the AI filter")
	}
	m = typeKeys(t, m, "ctrl+a")
	if m.cfg.FilterAI {
		t.Fatal("ctrl+a should disable the AI filter again")
	}

	if len(client.searched) < 2 {
		t.Fatalf("expected a search per toggle, got %v", client.searched)
	}
	if client.searched[0] != "touhou -ai_generated" {
		t.Errorf("first search tags = %q, want the filter applied", client.searched[0])
	}
	if client.searched[1] != "touhou" {
		t.Errorf("second search tags = %q, want the filter removed", client.searched[1])
	}
	if len(client.counted) < 2 {
		t.Fatalf("expected a count per toggle, got %v", client.counted)
	}
	if client.counted[0] != "touhou -ai_generated" || client.counted[1] != "touhou" {
		t.Errorf("counts used %v, want the filter applied then removed", client.counted)
	}
}

func TestAIFilterTogglePersistsAndShows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)

	m := newListModel()
	m.cfg.ActiveAPI = "rule34"
	m.state = stateSearch
	view := m.View()
	if !strings.Contains(view, "Filter AI posts") || !strings.Contains(view, "off") {
		t.Error("search screen should show the AI filter switch")
	}

	m = typeKeys(t, m, "ctrl+a")
	if !m.cfg.FilterAI {
		t.Fatal("ctrl+a should enable the AI filter")
	}
	if !strings.Contains(m.View(), "on") {
		t.Error("switch should show the enabled state")
	}

	data, err := os.ReadFile(filepath.Join(home, "r34-dl", "config.json"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(data), `"filter_ai": true`) {
		t.Errorf("filter preference was not saved: %s", data)
	}
}

func TestAIFilterIsRule34Only(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := newListModel()
	m.state = stateSearch

	if strings.Contains(m.View(), "Filter AI posts") {
		t.Error("safebooru has no AI switch, it should not be rendered")
	}
	m = typeKeys(t, m, "ctrl+a")
	if m.cfg.FilterAI {
		t.Error("ctrl+a should be ignored while safebooru is active")
	}

	m.cfg.ActiveAPI = "rule34"
	if !strings.Contains(m.View(), "Filter AI posts") {
		t.Error("rule34 should show the AI switch")
	}
	m = typeKeys(t, m, "ctrl+a")
	if !m.cfg.FilterAI {
		t.Error("ctrl+a should toggle the AI filter for rule34")
	}
}
