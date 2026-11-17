package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSecretInputMasksTypingAndPaste(t *testing.T) {
	m := secretInput{prompt: "key: "}
	for _, tt := range []struct {
		key  tea.KeyMsg
		want string
	}{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("example")}, "example"},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("&test=ą\r\n"), Paste: true}, "example&test=ą"},
		{tea.KeyMsg{Type: tea.KeyBackspace}, "example&test="},
		{tea.KeyMsg{Type: tea.KeyCtrlH}, "example&test"},
		{tea.KeyMsg{Type: tea.KeyCtrlU}, ""},
		{tea.KeyMsg{Type: tea.KeyBackspace}, ""},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("replacement")}, "replacement"},
	} {
		updated, _ := m.Update(tt.key)
		m = updated.(secretInput)
		if string(m.value) != tt.want {
			t.Fatal("input editing lost characters")
		}
		if view := m.View(); view != "key: "+strings.Repeat("*", len([]rune(tt.want))) {
			t.Fatal("input was not fully masked")
		}
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.(secretInput).err != nil || cmd == nil {
		t.Fatal("enter should submit the input")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("enter should close the prompt")
	}
}

func TestSecretInputCancel(t *testing.T) {
	for _, tt := range []struct {
		name string
		key  tea.KeyType
	}{
		{"interrupt", tea.KeyCtrlC},
		{"end of input", tea.KeyCtrlD},
	} {
		t.Run(tt.name, func(t *testing.T) {
			updated, cmd := (secretInput{}).Update(tea.KeyMsg{Type: tt.key})
			m := updated.(secretInput)
			if m.err == nil || cmd == nil {
				t.Fatal("cancel should stop input with an error")
			}
			if tt.key == tea.KeyCtrlD && !errors.Is(m.err, io.EOF) {
				t.Fatal("empty input should report EOF")
			}
		})
	}
}

func TestSecretInputWrapsLongKeys(t *testing.T) {
	m := secretInput{prompt: "key: ", value: []rune("example")}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 8, Height: 24})
	if updated.View() != "key: ***\n****" {
		t.Fatal("long keys should wrap without truncating the mask")
	}
}

func TestSecretLinePreservesBufferedPipedInput(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n", ""} {
		t.Run("ending="+strings.ReplaceAll(strings.ReplaceAll(ending, "\r", "CR"), "\n", "LF"), func(t *testing.T) {
			file, err := os.CreateTemp(t.TempDir(), "input")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := io.WriteString(file, "example-login\n example-key "+ending); err != nil {
				t.Fatal(err)
			}
			if _, err := file.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(file)
			if _, err := reader.ReadString('\n'); err != nil {
				t.Fatal(err)
			}
			value, err := readSecretLine(file, reader, "")
			if err != nil {
				t.Fatal(err)
			}
			if value != "example-key" {
				t.Fatal("buffered key was not read correctly")
			}
		})
	}
}
