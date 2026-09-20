package omnibox

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/line"
	"home-management-system/internal/tui/table"

	"home-management-system/internal/tui/style"
	"home-management-system/internal/tui/text"
)

// Mode is what the line is for. The leader key chooses it, and the rendering
// has to make the choice obvious BEFORE a word is read -- C-s and M-g feel
// identical for exactly one keystroke and then diverge completely, so if a
// person has to work out which they are in, the widget has failed whatever else
// it does well.
type Mode int

const (
	// Closed is the resting state. The line is still drawn -- dim, naming the
	// keys that open it -- because a line that vanished was a line nobody knew
	// was there.
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
	// results is what a jump would land on, and candidates is everything there
	// is to land on. They live here because they are what this widget DISPLAYS:
	// the line owned the typing while the application owned the results, so the
	// screen had to ask the mode what it was doing twice.
	results       table.Model
	candidates    []resolve.Candidate
	resultsWidth  int
	resultsHeight int
	// offers is what the mode that owns the keyboard takes, which the line
	// shows while it is resting.
	//
	// Told rather than read from the keymap here, because only the application
	// knows which mode is on top -- the line is drawn under every screen,
	// including the modal ones, and an affordance naming C-s under a
	// confirmation that ignores it would be worse than a blank line. Empty
	// means the mode draws its own hints elsewhere, and this goes quiet.
	offers string
}

// A block cursor, so the line looks like somewhere text goes -- the same
// cursor the inline field draws, for the same reason.

// The command prompt is tinted rather than reversed, so the three modes
// differ from each other in KIND -- a slash, a reversed badge, a tinted
// badge -- rather than by degree of emphasis.
var commandStyle = lipgloss.NewStyle().Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "231", Dark: "231"}).
	Background(lipgloss.AdaptiveColor{Light: "24", Dark: "24"})

// The gutter labels, one per state of the line.
//
// They are declared together because their WIDTHS have to be compared: the
// input begins at the same column in every state, which means the gutter is as
// wide as the widest of these and no state may size itself.
const (
	labelFilter   = "I-search"
	labelJump     = "JUMP"
	labelFiltered = "filtered"
)

// labelCommand spells the key that opens it, the way emacs does -- so it is
// read from the keymap rather than written down, and a rebinding cannot leave
// the gutter advertising a key that is gone.
var labelCommand = keys.Show(keys.Browse, keys.CommandLine)

// indent is the left margin the whole bottom block shares, so the line's left
// edge agrees with the carry banner and the refusals above it.
const indent = "  "

// separator is the gap between the gutter and the text.
const separator = "  "

// gutterWidth is the widest label, computed rather than written down.
//
// A hardcoded column is a column that is right until somebody adds a mode, and
// wrong silently afterwards: the input would start one place in three states
// and another in the fourth, which is precisely the jitter the fixed gutter
// exists to remove.
var gutterWidth = func() int {
	width := 0
	for _, label := range []string{labelFilter, labelJump, labelCommand, labelFiltered} {
		if n := len([]rune(label)); n > width {
			width = n
		}
	}
	return width
}()

// textColumn is where the typed text begins, in every state without exception.
var textColumn = len(indent) + gutterWidth + len(separator)

func New() Model {
	return Model{
		width: 80, offers: browseKeys(),
		results:      table.New(resultColumns).Fixed(),
		resultsWidth: 80, resultsHeight: 12,
	}
}

// browseKeys is what the line offers when nothing has told it otherwise, so a
// Model built by hand in a test is not silently blank.
func browseKeys() string {
	return keys.Hint(keys.Browse,
		[]keys.Action{keys.Search},
		[]keys.Action{keys.Jump},
		[]keys.Action{keys.CommandLine})
}

func (m Model) SetWidth(w int) Model { m.width = w; return m }

// Offers tells the line what the mode on top of the stack takes, which only the
// application knows: it owns the stack, and the line is one of the modes in it.
// Told rather than guessed, because a widget guessing at what is open over it
// would be a second answer to a question layers.go already answers.
//
// An empty string means the line is out of reach and goes quiet.
func (m Model) Offers(hints string) Model { m.offers = hints; return m }

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
	if mode == Jump {
		return m.refresh()
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
		if m.mode == Jump {
			// Narrowed as you type, the way the filter narrows the list.
			m = m.refresh()
		}
		return m, true
	}
	// A key the line does not want. In a jump that is cursor motion through the
	// results, which this widget now owns.
	if m.mode == Jump {
		return m.updateResults(msg)
	}
	return m, false
}

// View renders the line. It is ALWAYS one line, in every state.
//
// The line used to disappear when it was closed with no filter on, which is
// most of the time, and that cost three things at once: nothing on screen said
// the line existed or which keys opened it; the body resized whenever it
// opened, so starting a search shifted the rows being read; and the four states
// began their text at four different columns, so switching modes slid what you
// were typing sideways.
//
// So: fixed position, fixed height, fixed text column. What changes between
// resting and interacting is EMPHASIS -- a dim gutter and a dim affordance at
// rest, the mode's own lit badge and a block cursor when focused.
//
// The three modes still differ from each other in KIND rather than by degree,
// which is a different question from telling focused from resting: a person has
// to know whether they are filtering or jumping before reading a word, and
// badges inside a fixed gutter answer both questions at once.
func (m Model) View() string {
	badge, body, tail := m.parts()
	if m.offers == "" {
		// Something modal is over the list and draws its own hints, so this
		// goes quiet rather than away: an applied filter is still SAID, because
		// that is a fact about the rows on screen rather than an invitation to
		// press anything.
		tail = ""
		if m.mode == Closed && m.applied == "" {
			body = ""
		}
	}
	// The gutter is padded to its column OUTSIDE the badge's own styling, so a
	// tinted background stays the width of its word rather than stretching to
	// the column.
	gutter := badge + strings.Repeat(" ", max(0, gutterWidth-text.VisibleWidth(badge)))
	return strings.TrimRight(indent+gutter+separator+m.withTail(body, tail), " ")
}

// parts is what each state puts in the gutter, in the text column, and in the
// tail.
//
// One function returning all three rather than a switch per region, because the
// states differ ONLY in these three strings -- and writing them together is
// what makes that visible to whoever adds the fourth.
func (m Model) parts() (badge, body, tail string) {
	confirm, cancel := keys.Show(keys.Line, keys.Confirm), keys.Show(keys.Line, keys.Cancel)
	switch {
	case m.mode == Filter:
		// The prompt is isearch's, because the key is isearch's. It used to be
		// a bare `/`, which named the key that opened it -- and that key is
		// gone, so the prompt would have been advertising a keystroke that no
		// longer does anything.
		return style.Strong.Render(labelFilter), line.Render(m.input, m.cursor),
			confirm + " apply   " + cancel + " cancel"
	case m.mode == Jump:
		return style.Highlight.Render(labelJump), line.Render(m.input, m.cursor),
			confirm + " go   " + cancel + " cancel"
	case m.mode == Command:
		// A third badge, distinct from both, and it spells its own key. M-x
		// reads as a command line to anyone who has ever used emacs; what it
		// must not do is look like the filter.
		return commandStyle.Render(labelCommand), line.Render(m.input, m.cursor),
			confirm + " run   " + cancel + " cancel"
	case m.applied != "":
		// Resting, but not idle: a narrowed list that does not say so is a list
		// that lies about what you own. Dim label, strong value.
		return style.Dim.Render(labelFiltered), style.Strong.Render(m.applied),
			keys.Show(keys.Browse, keys.Cancel) + " clear"
	}
	// Resting and empty. The gutter is blank and the affordance sits in the
	// TEXT column -- where what you type will appear -- so the line teaches its
	// own geometry along with its keys.
	return "", style.Dim.Render(m.affordance()), ""
}

// affordance is what the mode on top takes, cut to the room the line has.
//
// Every hint in it is read from the keymap by whoever supplied it, so a
// rebinding cannot leave the one permanently visible line on the screen naming
// a key that is gone.
func (m Model) affordance() string {
	return text.JoinWhatFits(m.width-textColumn, strings.Split(m.offers, " - "))
}

// withTail puts the hints flush right, so they occupy a fixed column too.
//
// Dropped rather than wrapped once the text grows into them: a long command is
// the thing being read, and a hint that pushed it onto a second line would
// shift every row on the screen to say something the person already knows.
func (m Model) withTail(body, tail string) string {
	if tail == "" {
		return body
	}
	room := m.width - textColumn - text.VisibleWidth(body) - text.VisibleWidth(tail)
	if room < 3 {
		return body
	}
	return body + strings.Repeat(" ", room) + style.Dim.Render(tail)
}

// Height is how many lines the omnibox occupies. Always one: the line is
// furniture, and furniture that moves is the thing this is here to stop.
func (m Model) Height() int { return 1 }

func (m Model) String() string { return fmt.Sprintf("omnibox(%v, %q)", m.mode, m.input) }
