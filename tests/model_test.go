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
	msg, ok := runCmd(cmd, cmdTimeout)
	if !ok {
		t.Fatal("init command did not return in time")
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("init = %T, want a batch of commands", msg)
	}
	cleared := false
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		got, ok := runCmd(sub, cmdTimeout)
		if ok && got == tea.ClearScreen() {
			cleared = true
		}
	}
	if !cleared {
		t.Error("init batch should clear the terminal")
	}
}
