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

	"home-management-system/internal/tui/complete"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/line"

	"home-management-system/internal/tui/style"
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
	// list is the completion dropdown over the field -- the same one the
	// creation panel uses, so the keys mean the same thing in both. Offered
	// and never applied.
	list complete.Model
	// cursor is where the next character goes, as a rune index into value.
	//
	// A field holding one short value barely needs one -- backspace reaches
	// the mistake in a keystroke or two. A field holding a whole COMMAND LINE
	// does: without it, correcting one word of "consume \"Basmati Rice\" lots
	// at \"Shelf 1\"" means backspacing over everything after it, which is not
	// editing so much as retyping.
	cursor int
	// cancel is what esc will do, when it is not simply "discard".
	//
	// A prompt opened by `m` is opened over a thing that is now IN HAND, so esc
	// there closes the prompt and leaves the thing held rather than throwing
	// anything away. A footer saying "discard" would be describing a keystroke
	// that does something else, which is the one thing a footer must never do.
	cancel string
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
// A block cursor, so the field looks like somewhere text goes rather than
// like a line of output that happens to be bold.
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
	m.list, m.keep, m.cancel = complete.Model{}.Arrive(), "", ""
	m.cursor = len([]rune(current))
	return m
}

// Purpose is what enter will do with the answer.
func (m Model) Purpose() string { return m.purpose }

// WithCancel names what esc will do, for a caller that knows something the
// field does not.
func (m Model) WithCancel(what string) Model { m.cancel = what; return m }

// SetSuggestions offers completions for the whole value. Offered, never applied.
func (m Model) SetSuggestions(s []string) Model {
	m.keep = ""
	m.list = m.list.Offer(s, strings.TrimSpace(m.value))
	return m
}

// SetSuggestionsAfter offers completions for the last token of a line, keeping
// everything before it.
//
// The typed text handed to the list is the TOKEN, not the whole line -- the
// list uses it to tell when taking an option would change nothing, and a line
// never equals one of its own tokens.
func (m Model) SetSuggestionsAfter(keep string, s []string) Model {
	m.keep = keep
	m.list = m.list.Offer(s, strings.TrimPrefix(strings.TrimSpace(m.value), strings.TrimSpace(keep)))
	return m
}

// Suggestions are what is on offer.
func (m Model) Suggestions() []string { return m.list.Options() }

// Completion is what tab would take, if taking one would change anything.
func (m Model) Completion() (string, bool) { return m.list.Selected() }

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
// Update handles a keystroke, reporting whether it was consumed.
//
// The dropdown goes first, for the same reason it does in the creation panel:
// while a list is open Tab takes the highlighted option and C-n moves the
// highlight, and with nothing open those keys belong to whatever contains the
// field. The list reports what it did not use, so neither layer has to ask.
func (m Model) Update(msg tea.KeyMsg) (Model, bool) {
	if !m.open {
		return m, false
	}
	if next, taken, handled := m.list.Update(msg); handled {
		m.list = next
		if taken != "" {
			// keep is what a taken option leaves alone: for a field holding a
			// whole command line, only the last token is being completed.
			m.value = m.keep + taken
			m.cursor = len([]rune(m.value))
		}
		return m, true
	}
	if value, cursor, ok := line.Edit(m.value, m.cursor, msg); ok {
		m.value, m.cursor = value, cursor
		return m, true
	}
	// Tab with no list is still the editor's: there is nowhere else in a
	// single field for it to go, and letting it through would move the plan
	// screen's cursor out from under the row being edited.
	if keys.Lookup(keys.Line, msg) == keys.Complete {
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
		style.Dim.Render(head),
		"  " + line.Render(m.value, m.cursor),
	}
	out = append(out, m.list.Lines("  ", m.width)...)
	if m.list.IsOpen() {
		// While a list is up the keys mean what they mean inside it, and a
		// footer naming only the field's keys would be wrong for as long as it
		// was showing.
		return append(out, style.Dim.Render("  "+complete.Hint()))
	}
	cancel := m.cancel
	if cancel == "" {
		cancel = "discard"
	}
	return append(out, style.Dim.Render("  "+keys.Show(keys.Line, keys.Confirm)+" "+m.verb()+
		"   "+keys.Show(keys.Line, keys.Cancel)+" "+cancel))
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
