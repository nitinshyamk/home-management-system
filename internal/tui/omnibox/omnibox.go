package omnibox

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/line"
)

// Mode is what the line is for. The leader key chooses it, and the rendering
// has to make the choice obvious BEFORE a word is read -- C-s and M-g feel
// identical for exactly one keystroke and then diverge completely, so if a
// person has to work out which they are in, the widget has failed whatever else
// it does well.
type Mode int

const (
	// Closed is the resting state: no line, no cost.
	Closed Mode = iota
	// Filter narrows the rows in front of you.
	Filter
	// Jump searches everything at once and goes somewhere.
	Jump
	// Command is the `:` line -- the same syntax a CSV row and an agent speak,
	// which is why learning it once pays three times.
	Command
)

// Model is the input line.
type Model struct {
	mode  Mode
	input string
	// cursor is where the next character goes, as a rune index into input.
	//
	// The line had none until the bindings became emacs, and typing was the
	// only thing it could do: append, and backspace. Correcting the second word
	// of `consume "Basmati Rice" lots at "Shelf 1"` meant backspacing over
	// everything after it, which is retyping rather than editing -- and the
	// inline editor two files away had had a cursor the whole time.
	cursor int
	// applied is the filter still in force after the line was closed with
	// Enter. A filter that vanished when you stopped typing would be a search
	// box, not a filter.
	applied string
	width   int
}

var (
	filterStyle = lipgloss.NewStyle().Bold(true)
	jumpStyle   = lipgloss.NewStyle().Bold(true).Reverse(true)
	hintStyle   = lipgloss.NewStyle().Faint(true)
	// A block cursor, so the line looks like somewhere text goes -- the same
	// cursor the inline field draws, for the same reason.
	cursorStyle = lipgloss.NewStyle().Reverse(true)

	// The command prompt is tinted rather than reversed, so the three modes
	// differ from each other in KIND -- a slash, a reversed badge, a tinted
	// badge -- rather than by degree of emphasis.
	commandStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "231", Dark: "231"}).
			Background(lipgloss.AdaptiveColor{Light: "24", Dark: "24"})
)

func New() Model { return Model{width: 80} }

func (m Model) SetWidth(w int) Model { m.width = w; return m }

// Mode reports what the line is doing.
func (m Model) Mode() Mode { return m.mode }

// Open starts a mode, beginning from the applied filter when filtering, so
// refining a filter does not mean retyping it.
func (m Model) Open(mode Mode) Model {
	m.mode = mode
	if mode == Filter {
		m.input = m.applied
	} else {
		m.input = ""
	}
	m.cursor = len([]rune(m.input))
	return m
}

// Input is what has been typed.
func (m Model) Input() string { return m.input }

// Applied is the filter in force, whether or not the line is open.
func (m Model) Applied() string { return m.applied }

// Query is the applied filter, parsed.
func (m Model) Query() Query { return ParseQuery(m.applied) }

// Live is what is being typed, parsed -- the filter as it would apply if
// accepted now, which is what makes narrowing happen as you type.
func (m Model) Live() Query { return ParseQuery(m.input) }

// Accept closes the line and keeps what was typed.
func (m Model) Accept() Model {
	if m.mode == Filter {
		m.applied = m.input
	}
	m.mode, m.input, m.cursor = Closed, "", 0
	return m
}

// Cancel closes the line and changes nothing.
//
// Escape is never destructive: cancelling a jump must not move the cursor,
// change the view, or clear a filter that was already applied.
func (m Model) Cancel() Model {
	m.mode, m.input, m.cursor = Closed, "", 0
	return m
}

// Clear removes the applied filter. This is what esc does in the LIST, which is
// a different escape from the one that closes the line.
func (m Model) Clear() Model {
	m.applied = ""
	return m
}

// Update handles a keystroke while the line is open, reporting whether it was
// consumed.
func (m Model) Update(msg tea.KeyMsg) (Model, bool) {
	if m.mode == Closed {
		return m, false
	}
	// The same line editing the inline field has, from the same place. A space
	// is a space: only a real terminal sends KeySpace, and an earlier version
	// that appended msg.Runes AND a literal space trimmed the double back to
	// nothing, so typing a space did nothing at all and `:consume 100g` arrived
	// as "consume100g".
	if input, cursor, ok := line.Edit(m.input, m.cursor, msg); ok {
		m.input, m.cursor = input, cursor
		return m, true
	}
	return m, false
}

// View renders the line, or nothing when it is closed and no filter is applied.
//
// The two modes are told apart by their PROMPT and their framing, not by their
// contents. A person glancing at the screen has to know which one they are in
// before reading what they typed.
func (m Model) View() string {
	hint := func(accept string) string {
		return hintStyle.Render("  " + keys.Show(keys.Line, keys.Confirm) + " " + accept +
			"   " + keys.Show(keys.Line, keys.Cancel) + " cancel")
	}
	switch {
	case m.mode == Filter:
		// The prompt is isearch's, because the key is isearch's. It used to be
		// a bare `/`, which named the key that opened it -- and that key is
		// gone, so the prompt would have been advertising a keystroke that no
		// longer does anything.
		return filterStyle.Render("I-search: ") + m.renderInput() + hint("apply")
	case m.mode == Jump:
		return jumpStyle.Render(" JUMP ") + " " + m.renderInput() + hint("go")
	case m.mode == Command:
		// A third prompt, distinct from both, and it spells its own key. M-x
		// reads as a command line to anyone who has ever used emacs; what it
		// must not do is look like the filter.
		return commandStyle.Render(" "+keys.Show(keys.Browse, keys.CommandLine)+" ") + " " +
			m.renderInput() + hint("run")
	case m.applied != "":
		return hintStyle.Render("filtered ") + filterStyle.Render(m.applied) +
			hintStyle.Render("   "+keys.Show(keys.Browse, keys.Cancel)+" clear")
	}
	return ""
}

// renderInput draws the line with a block cursor in it.
//
// A real cursor rather than the trailing underscore that stood in for one. The
// underscore was honest while the line could only be appended to; now that C-a
// and C-b move within it, a marker frozen at the end would be a lie about where
// the next character goes.
func (m Model) renderInput() string {
	r := []rune(m.input)
	at := m.cursor
	if at > len(r) {
		at = len(r)
	}
	if at == len(r) {
		return filterStyle.Render(m.input) + cursorStyle.Render(" ")
	}
	return filterStyle.Render(string(r[:at])) +
		cursorStyle.Render(string(r[at])) +
		filterStyle.Render(string(r[at+1:]))
}

// Height is how many lines the omnibox occupies, which the layout needs before
// it knows what the omnibox will draw.
func (m Model) Height() int {
	if m.mode == Closed && m.applied == "" {
		return 0
	}
	return 1
}

func (m Model) String() string { return fmt.Sprintf("omnibox(%v, %q)", m.mode, m.input) }
