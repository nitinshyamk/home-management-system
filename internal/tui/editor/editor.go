// Package editor is the in-place field editor.
//
// In place is the whole point. The old interface's defect was that editing
// REPLACED browsing: a right pane that meant five different things by turns, so
// changing a name cost you your place in the list and the context that told you
// which name you meant. Here the field opens inside the list, the rows around
// it stay where they are, and closing it puts you back exactly where you were.
package editor

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model is one field being edited.
type Model struct {
	open    bool
	label   string
	value   string
	initial string
	// subject is what is being edited, carried through so the caller builds the
	// command from the editor rather than from parallel state that could drift.
	subject int64
	kind    string
	width   int
}

var (
	labelStyle = lipgloss.NewStyle().Faint(true)
	valueStyle = lipgloss.NewStyle().Bold(true)
	hintStyle  = lipgloss.NewStyle().Faint(true)
)

func New() Model { return Model{width: 80} }

func (m Model) SetWidth(w int) Model { m.width = w; return m }

// Open starts editing, pre-filled with what the field currently holds.
//
// Pre-filled rather than blank: a rename is nearly always a correction to a
// name rather than a replacement of one, and blanking it makes the common case
// the expensive one.
func (m Model) Open(kind string, subject int64, label, current string) Model {
	m.open, m.kind, m.subject = true, kind, subject
	m.label, m.value, m.initial = label, current, current
	return m
}

func (m Model) IsOpen() bool   { return m.open }
func (m Model) Subject() int64 { return m.subject }
func (m Model) Kind() string   { return m.kind }
func (m Model) Value() string  { return m.value }
func (m Model) Changed() bool  { return strings.TrimSpace(m.value) != m.initial }
func (m Model) Close() Model   { m.open = false; return m }

// Update handles a keystroke, reporting whether it was consumed.
//
// While a field is open a keystroke is a CHARACTER. That is what makes an
// inline editor usable at all, and it is why the parent consults this before
// anything else.
func (m Model) Update(msg tea.KeyMsg) (Model, bool) {
	if !m.open {
		return m, false
	}
	switch msg.Type {
	case tea.KeyRunes:
		m.value += string(msg.Runes)
		return m, true
	case tea.KeySpace:
		m.value += " "
		return m, true
	case tea.KeyBackspace:
		if r := []rune(m.value); len(r) > 0 {
			m.value = string(r[:len(r)-1])
		}
		return m, true
	case tea.KeyCtrlU:
		m.value = ""
		return m, true
	}
	return m, false
}

// View renders the field as a small frame that sits inside the list.
func (m Model) View() string {
	if !m.open {
		return ""
	}
	head := "- " + m.label + " "
	if pad := m.width - len(head) - 2; pad > 0 {
		head += strings.Repeat("-", pad)
	}
	return strings.Join([]string{
		labelStyle.Render(head),
		"  " + valueStyle.Render(m.value) + "_",
		hintStyle.Render("  enter save   esc discard"),
	}, "\n")
}

// Height is how many lines the editor occupies, which the layout needs before
// it knows what the editor will draw.
func (m Model) Height() int {
	if !m.open {
		return 0
	}
	return 3
}
