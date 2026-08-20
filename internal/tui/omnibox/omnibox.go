package omnibox

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Mode is what the line is for. The leader key chooses it, and the rendering
// has to make the choice obvious BEFORE a word is read -- `/` and `C-p` feel
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
	m.mode, m.input = Closed, ""
	return m
}

// Cancel closes the line and changes nothing.
//
// Escape is never destructive: cancelling a jump must not move the cursor,
// change the view, or clear a filter that was already applied.
func (m Model) Cancel() Model {
	m.mode, m.input = Closed, ""
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
	switch msg.Type {
	case tea.KeyRunes:
		m.input += string(msg.Runes)
		return m, true
	case tea.KeySpace:
		// A space is a space. The first version appended msg.Runes AND a
		// literal space and then trimmed a double -- which trimmed both, so
		// typing a space did nothing at all. Only a real terminal sends
		// KeySpace, and the harness's Type() was sending KeyRunes, so every
		// test passed while `:consume 100g` arrived as "consume100g".
		m.input += " "
		return m, true
	case tea.KeyBackspace:
		if r := []rune(m.input); len(r) > 0 {
			m.input = string(r[:len(r)-1])
		}
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
	switch {
	case m.mode == Filter:
		return filterStyle.Render("/"+m.input) + hintStyle.Render("_  enter apply   esc cancel")
	case m.mode == Jump:
		return jumpStyle.Render(" JUMP ") + " " + filterStyle.Render(m.input) +
			hintStyle.Render("_  enter go   esc cancel")
	case m.mode == Command:
		// A third prompt, distinct from both. `:` reads as a command line
		// everywhere a terminal has ever had one, so it earns the leader; what
		// it must not do is look like `/`.
		return commandStyle.Render(" : ") + " " + filterStyle.Render(m.input) +
			hintStyle.Render("_  enter run   esc cancel")
	case m.applied != "":
		return hintStyle.Render("filtered ") + filterStyle.Render("/"+m.applied) +
			hintStyle.Render("   esc clear")
	}
	return ""
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
