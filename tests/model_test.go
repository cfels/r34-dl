package tests

import (
	"testing"

	"moxiu/r34-dl/conf"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInitClearsTerminal(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h := newHarness(conf.Config{AgeVerified: false}, "")
	cmd := h.m.Init()
	if cmd == nil {
		t.Fatal("init should clear whatever was on the terminal before")
	}
	msg := cmd()
	if msg != tea.ClearScreen() {
		t.Fatalf("init = %T, want the clear-screen command", msg)
	}
}
