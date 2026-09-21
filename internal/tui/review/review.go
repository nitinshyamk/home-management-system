// Package review is the screen that shows a set of proposed changes against
// the house, and settles them one at a time.
//
// The highest-stakes screen in the system. Everywhere else one thing happens
// and you watch it happen; here something proposes a batch of changes, and
// the only thing between it and the house is whether this screen told the
// truth about what it was going to do.
//
// It does not know what proposed them. Three things do -- a file somebody
// wrote, a batch of structural edits staged in the house, and a walk that
// counted what is actually on a shelf -- and all three end in the same
// review: the same marks, the same drop, the same all-or-nothing apply. It
// began welded to the first of them, so its rows were rows of a FILE, with a
// line number and the raw text they were written in. A staged rename is not a
// row of a file, and making it pretend to be one would mean rendering a
// Command back to text and binding it again, which is the round trip the
// whole architecture exists to prevent.
//
// So a Change arrives already in words. Turning a Command into a sentence
// needs a vocabulary; deciding that one row is waiting for another needs the
// file both rows came from. Neither is this screen's business. What is its
// business is the marks, the counts, the cursor, the heading, and the
// arithmetic that has to add up.
//
// It reuses the table widget unchanged. A second table would be a second
// product, and the density decisions settled in 10a are the ones a person has
// already learned to read.
package review

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/style"
	"home-management-system/internal/tui/table"
)

// State is what a change is waiting for.
//
// Four, and the same four whatever proposed it. They are not a rendering
// decision -- they are what the person has to do next, which is the one thing
// this screen exists to say.
type State int

const (
	// Ready has nothing left to decide.
	Ready State = iota
	// Confirmable can proceed once a person says so: it would create
	// something, or it rests on a suggestion that must be accepted rather
	// than applied.
	Confirmable
	// Blocked cannot proceed at all. Only a person can settle it.
	Blocked
	// Dropped was set aside deliberately.
	Dropped
)

func (s State) String() string {
	switch s {
	case Ready:
		return "ready"
	case Confirmable:
		return "needs confirmation"
	case Blocked:
		return "blocked"
	}
	return "dropped"
}

// Change is one proposed change, as the screen needs it: already in words.
type Change struct {
	State State
	// At is where it came from, in whatever terms its producer counts in -- a
	// line number for a file, empty for an edit staged in the house, which
	// came from nowhere but the cursor. It is how you find the thing again,
	// not what the change is, so it sits in a narrow column that drops first.
	At string
	// What it would do, said the way a person would say it.
	What string
	// Why it cannot yet, or empty when nothing is in the way. The producer's
	// words: "waits for row 2" is a fact about a file, and this screen has
	// never seen one.
	Why string
}

// Stage is which stage of a staged review this screen is showing.
//
// A review with one stage says nothing about stages at all -- a file of
// holdings proposes no classification and no places, and telling somebody
// they are on "stage 1 of 1" is chrome that describes the screen rather than
// the work.
type Stage struct {
	// Name is what this stage is about, in the words the footer uses.
	Name string
	// Number and Of place it in the sequence.
	Number, Of int
	// Skippable says the whole stage can be set aside without applying any of
	// it. The structural stages are -- the categories and the places -- and
	// only where something follows: skipping leaves the house exactly as it
	// was, and the stages behind can still file a change into a category or a
	// place that already exists.
	Skippable bool
	// InPasses says this stage applies by the tree's rule rather than the
	// receipt's -- the ready changes now, the rest on another pass.
	InPasses bool
}

// shown reports whether there is a sequence worth naming.
func (s Stage) shown() bool { return s.Of > 1 }

// Lead names the stage at the start of a line reporting what it did, or says
// nothing at all when there is only one.
//
// The same rule the heading goes by, for the same reason: "stage 1 of 1"
// describes the screen rather than the work, and the one line saying what was
// written to the house is the last place to start being chrome about it.
func (s Stage) Lead() string {
	if !s.shown() {
		return ""
	}
	return fmt.Sprintf("stage %d (%s) ", s.Number, s.Name)
}

// more reports whether another stage follows this one, which is what makes
// applying this one something other than the end of the work.
func (s Stage) more() bool { return s.Of > 1 && s.Number < s.Of }

// columns are the review's. The state comes FIRST and is never dropped: if
// you have to read the issue text to know a change is blocked, the screen has
// failed at the only thing it is for.
var columns = []table.Column{
	{Title: "", Min: 2},
	{Title: "ROW", Min: 3, Align: table.Right, Drop: 4},
	{Title: "WHAT IT WOULD DO", Min: 20, Grow: true},
	{Title: "WHY NOT", Min: 12, Drop: 2},
}

// Model is the review screen.
type Model struct {
	changes []Change
	// source is what proposed them, for the heading -- a file's name, or what
	// a batch of staged edits is called.
	source string
	// lead is the word the heading opens with: IMPORT for a file, and
	// something else for whatever else ends up here.
	lead string
	tbl  table.Model
	// stage is which stage of a staged review this is, or the zero value for
	// a set that has only one.
	stage Stage
	// note is what the stage before this one did, said once at the top of the
	// screen. A staged review applies in a transaction per stage, and a
	// screen that did not say what the ones before it did would be asking for
	// another on the strength of something the person has no record of.
	note string
	// why is what stands in the way of applying the whole set, or empty when
	// nothing does.
	//
	// Handed in rather than worked out here. Whether a set can be applied is
	// its producer's rule: a receipt is all-or-nothing, a tree applies in
	// passes, and a screen that decided between them would be deciding
	// something it cannot see.
	why   string
	width int
}

// New builds a screen over a set of changes.
func New(lead, source string, changes []Change) Model {
	m := Model{lead: lead, source: source, width: 100}
	m.tbl = table.New(columns).Fixed()
	return m.WithChanges(changes)
}

// WithChanges replaces what is on screen, keeping the cursor where it was.
//
// The whole set each time rather than one at a time, because the changes are
// derived from something else -- a bound file, a batch of staged edits -- and
// two copies of what a change says is exactly the pair that comes apart. The
// producer settles one and hands the set back.
func (m Model) WithChanges(changes []Change) Model {
	m.changes = changes
	rows := make([]table.Row, 0, len(changes))
	for i, c := range changes {
		rows = append(rows, table.Row{
			Key:   int64(i),
			Cells: []string{mark(c.State), c.At, c.What, c.Why},
		})
	}
	m.tbl = m.tbl.SetRows(rows)
	return m
}

// WithStage says which stage of the work this screen is.
func (m Model) WithStage(stage Stage) Model { m.stage = stage; return m }

// WithNote records what the stage before this one did.
func (m Model) WithNote(note string) Model { m.note = note; return m }

// WithVerdict records what stands in the way of applying the whole set, or
// clears it with the empty string.
func (m Model) WithVerdict(why string) Model { m.why = why; return m }

// Stage is which stage is on screen, so the caller that has to decide what
// applying it means does not have to remember.
func (m Model) Stage() Stage { return m.stage }

// Changes is what is on screen.
func (m Model) Changes() []Change { return m.changes }

func (m Model) SetSize(width, height int) Model {
	m.width = width
	m.tbl = m.tbl.SetSize(width, height-m.chrome())
	return m
}

// chrome is the lines this screen spends on itself: the heading and the lines
// it ends with, plus the note when a stage before this one left one.
//
// A line taken from around the table has to be taken OFF the table, or the
// screen grows by one and the last row of it is the one that scrolls away.
func (m Model) chrome() int {
	if m.note != "" {
		return 4
	}
	return 3
}

// Wants is the height at which every change is on screen at once.
//
// Asked by the drawer, because the drawer decides how much of the screen to
// take and only this knows what it would need -- the table keeps two lines
// for itself on top of the chrome above.
func (m Model) Wants() int { return len(m.changes) + m.chrome() + 2 }

// SetOverlay draws lines immediately after the cursor's row, which is how a
// field opens ON the row it belongs to rather than under the whole set.
//
// Under the set is not inline; it is a second place to look, and on a screen
// where the row you are correcting is the whole point it is the wrong place.
func (m Model) SetOverlay(lines []string) Model {
	m.tbl = m.tbl.SetOverlay(lines)
	return m
}

// Applicable reports whether the set can be applied as it stands.
func (m Model) Applicable() bool { return m.why == "" }

// WhyNot is the refusal that goes with it.
func (m Model) WhyNot() string { return m.why }

// At is where the cursor is, or -1.
func (m Model) At() int {
	row, ok := m.tbl.Current()
	if !ok {
		return -1
	}
	at := int(row.Key)
	if at < 0 || at >= len(m.changes) {
		return -1
	}
	return at
}

// Current is the change under the cursor.
func (m Model) Current() (Change, int, bool) {
	at := m.At()
	if at < 0 {
		return Change{}, -1, false
	}
	return m.changes[at], at, true
}

// Counts is the arithmetic that has to add up to the whole set.
//
// Computed from the changes rather than tracked alongside them, because a
// change that vanished from this is a change that will surprise someone.
func (m Model) Counts() (ready, confirmable, blocked, dropped int) {
	for _, c := range m.changes {
		switch c.State {
		case Ready:
			ready++
		case Confirmable:
			confirmable++
		case Blocked:
			blocked++
		case Dropped:
			dropped++
		}
	}
	return
}

// Update handles a keystroke, reporting whether it was consumed.
//
// Only the cursor. Dropping and undropping used to happen here, and they were
// the reason this screen had to hold the bound file: undropping is BINDING a
// change again, against a vocabulary the screen has never seen. They are the
// producer's, and the drawer hands them over before this is reached.
func (m Model) Update(msg tea.KeyMsg) (Model, bool) {
	next, handled := m.tbl.Update(msg)
	m.tbl = next
	return m, handled
}

// plural counts changes in English, for the one line that says what A will
// write.
func plural(n int) string {
	if n == 1 {
		return "1 row"
	}
	return fmt.Sprintf("%d rows", n)
}

// mark is the state, in one character, before any words are read.
func mark(state State) string {
	switch state {
	case Ready:
		return style.Strong.Render("OK")
	case Confirmable:
		return style.Warn.Render("? ")
	case Blocked:
		return style.Error.Render("! ")
	}
	return style.Dim.Render("- ")
}

// View is what the review IS: its heading, what the stage before it did, and
// its rows.
//
// It used to draw its own counts line and its own key hints underneath, which
// made this the fifth place in the interface that rendered chrome -- and the
// only screen whose refusals, filter and status block appeared in a different
// order from everywhere else. The counts are Facts now, and the keys are what
// the layer offers the input line, so this screen ends the way every other
// screen ends.
func (m Model) View() string {
	rows := "   " + plural(len(m.changes))
	// Which stage this is, and only when there are several: a file of
	// holdings proposes no shape at all, and "stage 1 of 1" describes the
	// screen rather than the work.
	var stage string
	if m.stage.shown() {
		stage = fmt.Sprintf("   STAGE %d OF %d - %s",
			m.stage.Number, m.stage.Of, strings.ToUpper(m.stage.Name))
	}
	// Elided from the START, because the end of a name is the part that
	// distinguishes it. The stage counts against the budget too, for the same
	// reason the row count does: a line wider than the screen wraps, and one
	// wrapped line shifts every row below it.
	head := elideStart(m.lead+"  "+m.source, max(12, m.width-len(rows)-len(stage)))
	lines := []string{style.Strong.Render(head) + style.Warn.Render(stage) + style.Dim.Render(rows)}
	// What the stage before this one did, said once at the top: it applied in
	// its own transaction, and this screen is asking for another on the
	// strength of it.
	if m.note != "" {
		lines = append(lines, style.Dim.Render("  "+m.note))
	}
	return strings.Join(append(lines, m.tbl.View()), "\n")
}

// elideStart cuts a string to a width, keeping the END.
func elideStart(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return "…" + string(r[len(r)-width+1:])
}

// Facts is what is true of the set: how many changes are in each state, and
// whether the whole of it can be applied yet.
//
// The same role the row counts play under a table -- which is why it is
// handed to the screen to render in the same place rather than drawn here.
//
// Returned as PARTS, most useful first, so the screen can drop from the end
// on a narrow terminal the way it does for every other facts line. As one
// string it ran to 102 columns on a 100-column terminal with nothing
// watching: this was the screen with no frame, and the only one no width
// check ever saw.
func (m Model) Facts() []string {
	ready, confirmable, blocked, dropped := m.Counts()
	facts := []string{
		style.Strong.Render(fmt.Sprintf("%d ready", ready)),
		style.Warn.Render(fmt.Sprintf("%d need confirming", confirmable)),
		style.Error.Render(fmt.Sprintf("%d blocked", blocked)),
	}
	if dropped > 0 {
		facts = append(facts, style.Dim.Render(fmt.Sprintf("%d dropped", dropped)))
	}

	// Last, because it is the least surprising: the counts above already say
	// whether anything is in the way.
	//
	// The REASON is not here. It ends with what to do about it, and that
	// sentence is long enough that JoinWhatFits drops the whole part rather
	// than truncating it -- so the line silently lost the one thing it was
	// added for. Pressing the key is what asks the question, and the refusal
	// is where the answer belongs.
	apply := keys.Show(keys.Plan, keys.ApplyAll)
	switch {
	case m.why != "":
		return append(facts, style.Dim.Render(apply+" is unavailable"))
	case m.stage.InPasses && confirmable > 0:
		// What a pass applies is the ready changes, and saying so is the only
		// way the count to its left and the key it names agree with each
		// other.
		return append(facts, style.Strong.Render(fmt.Sprintf("%s applies the %s ready now",
			apply, plural(ready))))
	case m.stage.more():
		// What a stage applies is what a stage applies. Saying "all of it"
		// here would be a promise about changes that are not on this screen,
		// and the ones not on this screen are the whole reason there are two
		// stages.
		return append(facts, style.Strong.Render(fmt.Sprintf("%s applies the %s, then stage %d",
			apply, m.stage.Name, m.stage.Number+1)))
	}
	return append(facts, style.Strong.Render(apply+" applies all of it, in one transaction"))
}
