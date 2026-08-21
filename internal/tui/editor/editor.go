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
	// purpose says what pressing enter will do. The field is the same widget
	// whether it is collecting a name, a quantity, or a destination -- what
	// differs is only what the caller does with the answer, so that is what is
	// carried rather than three near-identical widgets.
	purpose string
	// suggestions complete a destination, offered and never applied. Empty for
	// the fields that are free text.
	suggestions []string
	width       int
}

var (
	labelStyle = lipgloss.NewStyle().Faint(true)
	valueStyle = lipgloss.NewStyle().Bold(true)
	hintStyle  = lipgloss.NewStyle().Faint(true)
	// A block cursor, so the field looks like somewhere text goes rather than
	// like a line of output that happens to be bold.
	cursorStyle = lipgloss.NewStyle().Reverse(true)
)

func New() Model { return Model{width: 80} }

func (m Model) SetWidth(w int) Model { m.width = w; return m }

// Open starts editing, pre-filled with what the field currently holds.
//
// Pre-filled rather than blank: a rename is nearly always a correction to a
// name rather than a replacement of one, and blanking it makes the common case
// the expensive one.
func (m Model) Open(kind string, subject int64, label, current string) Model {
	return m.OpenFor("rename", kind, subject, label, current)
}

// OpenFor starts a field whose answer the caller will use for something
// particular.
func (m Model) OpenFor(purpose, kind string, subject int64, label, current string) Model {
	m.open, m.purpose, m.kind, m.subject = true, purpose, kind, subject
	m.label, m.value, m.initial = label, current, current
	m.suggestions = nil
	return m
}

// Purpose is what enter will do with the answer.
func (m Model) Purpose() string { return m.purpose }

// SetSuggestions offers completions. Offered, never applied.
func (m Model) SetSuggestions(s []string) Model { m.suggestions = s; return m }

// Suggestions are what is on offer.
func (m Model) Suggestions() []string { return m.suggestions }

// Completion is what tab would take, if taking one would change anything.
func (m Model) Completion() (string, bool) {
	if len(m.suggestions) == 0 || m.suggestions[0] == strings.TrimSpace(m.value) {
		return "", false
	}
	return m.suggestions[0], true
}

// Take fills the field with the top suggestion.
func (m Model) Take() Model {
	if suggestion, ok := m.Completion(); ok {
		m.value = suggestion
		m.suggestions = nil
	}
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
	case tea.KeyTab:
		if _, ok := m.Completion(); ok {
			return m.Take(), true
		}
		return m, true
	}
	return m, false
}

// Lines renders the field as the lines that sit in the list, immediately after
// the row being edited.
//
// Lines rather than a block, because the table splices them in between rows --
// which is what "inline" has to mean for it to be worth anything. Under the
// whole list is not inline; it is a second place to look.
func (m Model) Lines() []string {
	if !m.open {
		return nil
	}
	head := "  " + m.label + " "
	if pad := m.width - len([]rune(head)) - 2; pad > 0 {
		head += strings.Repeat("─", pad)
	}
	out := []string{
		labelStyle.Render(head),
		"  " + valueStyle.Render(m.value) + cursorStyle.Render(" "),
	}
	for i, suggestion := range m.suggestions {
		if i == 0 {
			out = append(out, "  "+valueStyle.Render(suggestion)+hintStyle.Render("   tab to take it"))
			continue
		}
		out = append(out, hintStyle.Render("  "+suggestion))
	}
	return append(out, hintStyle.Render("  enter "+m.verb()+"   esc discard"))
}

// verb says what enter will do, in the words of the thing being done. "enter
// save" on a quantity prompt would be describing the wrong act.
func (m Model) verb() string {
	switch m.purpose {
	case "rename":
		return "save"
	case "":
		return "confirm"
	}
	return m.purpose
}

// View is Lines joined, for callers that want a block.
func (m Model) View() string { return strings.Join(m.Lines(), "\n") }

// Height is how many lines the editor occupies, which the layout needs before
// it knows what the editor will draw.
func (m Model) Height() int { return len(m.Lines()) }
