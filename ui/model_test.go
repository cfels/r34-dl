package ui

import (
	"testing"

	"moxiu/r34-dl/api"
)

func testClients(client api.Client) map[string]api.Client {
	return map[string]api.Client{
		"safebooru": client,
		"rule34":    client,
	}
}

func TestInitClearsTerminal(t *testing.T) {
	m := newListModel()
	m.state = stateAgeGate
	if cmd := m.Init(); cmd == nil {
		t.Fatal("init should clear whatever was on the terminal before")
	}
}
