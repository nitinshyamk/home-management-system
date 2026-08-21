package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"home-management-system/internal/app"
)

// Render replays a key script and prints the frame after each step.
//
// It is the Simulator with a main() around it: the same model, the same
// keystrokes, the same loop, printing each frame instead of asserting on it.
// That sameness is the point -- a review looks at frames a test could have
// produced, so what is signed off and what is guarded are the same screen.
//
// Colour is stripped, so a frame is diffable plain text. A golden that only
// differs by escape sequences is a golden nobody reads.
func Render(ctx context.Context, ctrl app.Controller, script io.Reader, out io.Writer, width, height int, colour bool) error {
	steps, err := parseScript(script)
	if err != nil {
		return err
	}

	// Plain text by default, so a frame is diffable and a golden that differs
	// only by escape sequences is not a golden nobody reads.
	//
	// With colour, the same frames become the way to review the decisions that
	// have no plain-text form at all -- row banding, the selection underline --
	// which otherwise could only be judged by running the application and
	// describing what you saw.
	if !colour {
		lipgloss.SetColorProfile(termenv.Ascii)
	} else {
		lipgloss.SetColorProfile(termenv.TrueColor)
	}

	m := New(ctx, ctrl)
	var model tea.Model = m
	run := func(cmd tea.Cmd) {
		for cmd != nil {
			msg := cmd()
			if msg == nil {
				return
			}
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, c := range batch {
					var inner tea.Cmd = c
					for inner != nil {
						im := inner()
						if im == nil {
							break
						}
						model, inner = model.Update(im)
					}
				}
				return
			}
			model, cmd = model.Update(msg)
		}
	}

	model, _ = model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	run(m.Init())

	frame := func(label string) {
		fmt.Fprintf(out, "\n===== %s  (%dx%d) %s\n", label, width, height,
			strings.Repeat("=", max(0, width-len(label)-18)))
		fmt.Fprintln(out, model.View())
	}
	frame("start")

	for _, step := range steps {
		var cmd tea.Cmd
		model, cmd = model.Update(step.key)
		run(cmd)
		frame(step.label)
	}
	return nil
}

type step struct {
	key   tea.KeyMsg
	label string
}

// parseScript reads a .keys file: one keystroke per line, `#` for comments, and
// an optional `: label` so a frame says what it is showing.
//
// A file rather than a flag, because a review script is a thing that gets
// checked in next to the walkthrough that explains it.
func parseScript(r io.Reader) ([]step, error) {
	var steps []step
	scanner := bufio.NewScanner(r)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || isComment(text) {
			continue
		}
		// Split on COLON-SPACE, not on a bare colon. `:` is itself a key -- it
		// is the command leader, and a facet script types one too -- so a
		// format that cannot express it is a format that cannot drive the
		// interface it exists to photograph.
		name, label, _ := strings.Cut(text, ": ")
		name = strings.TrimSpace(name)
		label = strings.TrimSpace(label)
		key, ok := keyByName(name)
		if !ok {
			return nil, fmt.Errorf("line %d: %q is not a key", line, name)
		}
		if label == "" {
			label = name
		}
		steps = append(steps, step{key: key, label: label})
	}
	return steps, scanner.Err()
}

// isComment distinguishes a note from a keystroke.
//
// `#` is a key -- it counts -- and it was also the comment marker, so the count
// step of a review script was silently skipped and the digits after it went to
// the application as view switches. That is the second key this format could
// not express, after `:`, and both failed the same way: quietly, producing a
// frame of something else.
//
// A comment is a hash followed by a SPACE or another hash. A bare `#`, and `#`
// with a label, are keys.
func isComment(line string) bool {
	if !strings.HasPrefix(line, "#") {
		return false
	}
	rest := line[1:]
	return strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "#")
}

func keyByName(name string) (tea.KeyMsg, bool) {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}, true
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}, true
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}, true
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, true
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}, true
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}, true
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}, true
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}, true
	case "ctrl+b":
		return tea.KeyMsg{Type: tea.KeyCtrlB}, true
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}, true
	case "ctrl+n":
		return tea.KeyMsg{Type: tea.KeyCtrlN}, true
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}, true
	case "ctrl+f":
		return tea.KeyMsg{Type: tea.KeyCtrlF}, true
	}
	if len([]rune(name)) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}, true
	}
	return tea.KeyMsg{}, false
}

// RenderFile is Render over a script on disk.
func RenderFile(ctx context.Context, ctrl app.Controller, path string, out io.Writer, width, height int, colour bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return Render(ctx, ctrl, f, out, width, height, colour)
}
