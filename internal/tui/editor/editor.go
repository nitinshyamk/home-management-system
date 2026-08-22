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
	// cursor is where the next character goes, as a rune index into value.
	//
	// A field holding one short value barely needs one -- backspace reaches
	// the mistake in a keystroke or two. A field holding a whole COMMAND LINE
	// does: without it, correcting one word of "consume \"Basmati Rice\" lots
	// at \"Shelf 1\"" means backspacing over everything after it, which is not
	// editing so much as retyping.
	cursor int
	// keep is the part of the value a taken suggestion leaves alone.
	//
	// Empty for a field holding one value, where a completion replaces the
	// whole thing. Non-empty when the field holds a LINE and only its last
	// token is being completed -- the caller knows where that token starts,
	// because only the caller knows the grammar.
	keep  string
	width int
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
	m.suggestions, m.keep = nil, ""
	m.cursor = len([]rune(current))
	return m
}

// Purpose is what enter will do with the answer.
func (m Model) Purpose() string { return m.purpose }

// SetSuggestions offers completions for the whole value. Offered, never applied.
func (m Model) SetSuggestions(s []string) Model { m.suggestions, m.keep = s, ""; return m }

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
		m.value = m.keep + suggestion
		m.suggestions = nil
		m.cursor = len([]rune(m.value))
	}
	return m
}

// SetSuggestionsAfter offers completions for the last token of a line, keeping
// everything before it.
func (m Model) SetSuggestionsAfter(keep string, s []string) Model {
	m.keep, m.suggestions = keep, s
	return m
}

func (m Model) IsOpen() bool   { return m.open }
func (m Model) Subject() int64 { return m.subject }
func (m Model) Kind() string   { return m.kind }
func (m Model) Value() string  { return m.value }

// Label is the field's name, which for a fix IS the field being corrected.
func (m Model) Label() string { return m.label }
func (m Model) Changed() bool { return strings.TrimSpace(m.value) != m.initial }
func (m Model) Close() Model  { m.open = false; return m }

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
		return m.insert(string(msg.Runes)), true
	case tea.KeySpace:
		return m.insert(" "), true
	case tea.KeyBackspace:
		if m.cursor > 0 {
			r := []rune(m.value)
			m.value = string(r[:m.cursor-1]) + string(r[m.cursor:])
			m.cursor--
		}
		return m, true
	case tea.KeyDelete:
		if r := []rune(m.value); m.cursor < len(r) {
			m.value = string(r[:m.cursor]) + string(r[m.cursor+1:])
		}
		return m, true
	case tea.KeyLeft:
		if m.cursor > 0 {
			m.cursor--
		}
		return m, true
	case tea.KeyRight:
		if m.cursor < len([]rune(m.value)) {
			m.cursor++
		}
		return m, true
	case tea.KeyHome, tea.KeyCtrlA:
		m.cursor = 0
		return m, true
	case tea.KeyEnd, tea.KeyCtrlE:
		m.cursor = len([]rune(m.value))
		return m, true
	case tea.KeyCtrlU:
		// To the start of the line, as readline has it. With the cursor at the
		// end -- where it is unless someone moved it -- that clears the field,
		// which is what it has always done here.
		m.value = string([]rune(m.value)[m.cursor:])
		m.cursor = 0
		return m, true
	case tea.KeyCtrlW:
		// The word before the cursor, which on a command line is usually the
		// token that is wrong.
		r := []rune(m.value)
		end := m.cursor
		for end > 0 && r[end-1] == ' ' {
			end--
		}
		for end > 0 && r[end-1] != ' ' {
			end--
		}
		m.value = string(r[:end]) + string(r[m.cursor:])
		m.cursor = end
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
		"  " + m.renderValue(),
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

// insert puts text at the cursor and moves past it.
func (m Model) insert(text string) Model {
	r := []rune(m.value)
	m.value = string(r[:m.cursor]) + text + string(r[m.cursor:])
	m.cursor += len([]rune(text))
	return m
}

// renderValue draws the value with the block cursor sitting IN it rather than
// always after it, so a cursor that has been moved is visible where it is.
func (m Model) renderValue() string {
	r := []rune(m.value)
	at := m.cursor
	if at > len(r) {
		at = len(r)
	}
	if at == len(r) {
		return valueStyle.Render(m.value) + cursorStyle.Render(" ")
	}
	return valueStyle.Render(string(r[:at])) +
		cursorStyle.Render(string(r[at])) +
		valueStyle.Render(string(r[at+1:]))
}

// AtEnd reports whether the cursor is at the end of the value, which is the
// only place a completion of the last token means anything.
func (m Model) AtEnd() bool { return m.cursor >= len([]rune(m.value)) }

// verb says what enter will do, in the words of the thing being done. "enter
// save" on a quantity prompt would be describing the wrong act.
func (m Model) verb() string {
	switch m.purpose {
	case "rename":
		return "save"
	case "row":
		return "re-check the row"
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
