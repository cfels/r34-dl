package tests

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"moxiu/r34-dl/conf"
)

func TestSearchTagsRespectAIFilter(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	on := newHarness(conf.Config{
		AgeVerified: true,
		ActiveAPI:   "rule34",
		FilterAI:    true,
	}, "touhou").start(t)
	if got := on.r34.searched; !reflect.DeepEqual(got, []string{"touhou -ai_generated"}) {
		t.Errorf("search tags = %v, want the filter applied", got)
	}

	blank := newHarness(conf.Config{
		AgeVerified: true,
		ActiveAPI:   "rule34",
		FilterAI:    true,
	}, "  ").start(t)
	if got := blank.r34.searched; !reflect.DeepEqual(got, []string{"-ai_generated"}) {
		t.Errorf("blank query search tags = %v, want just the filter", got)
	}

	sb := newHarness(conf.Config{
		AgeVerified: true,
		ActiveAPI:   "safebooru",
		FilterAI:    true,
	}, "touhou").start(t)
	if got := sb.sb.searched; !reflect.DeepEqual(got, []string{"touhou"}) {
		t.Errorf("safebooru search tags = %v, want the filter left to rule34", got)
	}
}

func TestSearchUsesAIFilter(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := newHarness(conf.Config{
		AgeVerified: true,
		ActiveAPI:   "rule34",
	}, "touhou").start(t)

	h.press(t, "ctrl+a")
	h.press(t, "ctrl+a")

	wantSearched := []string{"touhou", "touhou -ai_generated", "touhou"}
	if got := h.r34.searched; !reflect.DeepEqual(got, wantSearched) {
		t.Errorf("searches used %v, want the filter applied then removed", got)
	}
	if got := h.r34.counted; !reflect.DeepEqual(got, wantSearched) {
		t.Errorf("counts used %v, want the filter applied then removed", got)
	}
}

func TestAIFilterTogglePersistsAndShows(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	h := newHarness(conf.Config{
		AgeVerified: true,
		ActiveAPI:   "rule34",
	}, "")

	view := h.m.View()
	if !strings.Contains(view, "Filter AI posts") || !strings.Contains(view, "───────●") {
		t.Error("search screen should show the AI filter switch off")
	}

	h.press(t, "ctrl+a")
	if !strings.Contains(h.m.View(), "Filter AI posts") || !strings.Contains(h.m.View(), "●───────") {
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
	h := newHarness(conf.Config{
		AgeVerified: true,
		ActiveAPI:   "safebooru",
	}, "")

	if strings.Contains(h.m.View(), "Filter AI posts") {
		t.Error("safebooru has no AI switch, it should not be rendered")
	}
	h.press(t, "ctrl+a")
	h.press(t, "tab")

	if !strings.Contains(h.m.View(), "Filter AI posts") {
		t.Fatal("rule34 should show the AI switch")
	}
	if !strings.Contains(h.m.View(), "───────●") {
		t.Error("ctrl+a should be ignored while safebooru is active")
	}
	h.press(t, "ctrl+a")
	if !strings.Contains(h.m.View(), "●───────") {
		t.Error("ctrl+a should toggle the AI filter for rule34")
	}
}
