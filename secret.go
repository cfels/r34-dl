package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

type secretInput struct {
	prompt string
	value  []rune
	width  int
	err    error
}

func (m secretInput) Init() tea.Cmd { return nil }

func (m secretInput) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.Type {
	case tea.KeyEnter:
		return m, tea.Quit
	case tea.KeyCtrlC:
		m.err = errors.New("input cancelled")
		return m, tea.Quit
	case tea.KeyCtrlD:
		if len(m.value) == 0 {
			m.err = io.EOF
		}
		return m, tea.Quit
	case tea.KeyBackspace, tea.KeyCtrlH:
		if len(m.value) > 0 {
			m.value = m.value[:len(m.value)-1]
		}
	case tea.KeyCtrlU:
		m.value = nil
	case tea.KeyRunes:
		for _, r := range key.Runes {
			if unicode.IsPrint(r) {
				m.value = append(m.value, r)
			}
		}
	}
	return m, nil
}

func (m secretInput) View() string {
	return ansi.Hardwrap(m.prompt+strings.Repeat("*", len(m.value)), m.width, true)
}

func readSecretLine(r *os.File, reader *bufio.Reader, prompt string) (string, error) {
	if term.IsTerminal(int(r.Fd())) {
		model, err := tea.NewProgram(secretInput{prompt: prompt}, tea.WithInput(r)).Run()
		if err != nil {
			return "", fmt.Errorf("read masked input: %w", err)
		}
		input := model.(secretInput)
		if input.err != nil {
			return "", input.err
		}
		return strings.TrimSpace(string(input.value)), nil
	}
	fmt.Print(prompt)
	line, err := reader.ReadString('\n')
	fmt.Println()
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}
