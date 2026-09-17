package ui

import "testing"

func TestInitClearsTerminal(t *testing.T) {
	m := newListModel()
	m.state = stateAgeGate
	if cmd := m.Init(); cmd == nil {
		t.Fatal("init should clear whatever was on the terminal before")
	}
}
