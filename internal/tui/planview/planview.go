// Package planview is the import review screen.
//
// The highest-stakes screen in the system. Everywhere else one thing happens
// and you watch it happen; here a file you did not write proposes a batch of
// changes, and the only thing between it and the house is whether this screen
// told the truth about what it was going to do.
//
// It reuses the table widget unchanged. A second table would be a second
// product, and the density decisions settled in 10a are the ones a person has
// already learned to read.
package planview

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/command"
	"home-management-system/internal/importer"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/table"

	"home-management-system/internal/tui/style"

	"home-management-system/internal/tui/text"
)

// Model is the review screen.
type Model struct {
	plan importer.Plan
	tbl  table.Model
	// names renders identifiers back into names for the summary column.
	names Namer
	// stage is which stage of a staged import this is, or the zero value for a
	// file that has only one.
	stage Stage
	// note is what the stage before this one did, said once at the top of the
	// screen. A staged import applies in a transaction per stage, and a screen
	// that did not say what the ones before it did would be asking for another
	// on the strength of something the person has no record of.
	note  string
	width int
}

// Stage is which stage of a staged import this screen is showing.
//
// An import with one stage says nothing about stages at all -- a file of
// holdings proposes no classification and no places, and telling somebody they
// are on "stage 1 of 1" is chrome that describes the screen rather than the
// file.
type Stage struct {
	// Name is what this stage is about, in the words the footer uses.
	Name string
	// Number and Of place it in the sequence.
	Number, Of int
	// Skippable says the whole stage can be set aside without applying any of
	// it. The structural stages are -- the categories and the places -- and
	// only where something follows: skipping leaves the house exactly as it
	// was, and the stages behind can still file a row into a category or a
	// place that already exists.
	Skippable bool
	// InPasses says this stage applies by the tree's rule rather than the
	// receipt's -- the ready rows now, the rest on another pass. See
	// importer.Plan.ApplicableInPasses for why the two rules differ.
	InPasses bool
}

// shown reports whether there is a sequence worth naming.
func (s Stage) shown() bool { return s.Of > 1 }

// Lead names the stage at the start of a line reporting what it did, or says
// nothing at all when the import has only one.
//
// The same rule the heading goes by, for the same reason: "stage 1 of 1"
// describes the screen rather than the file, and the one line saying what was
// written to the house is the last place to start being chrome about it.
func (s Stage) Lead() string {
	if !s.shown() {
		return ""
	}
	return fmt.Sprintf("stage %d (%s) ", s.Number, s.Name)
}

// more reports whether another stage follows this one, which is what makes
// applying this one something other than the end of the import.
func (s Stage) more() bool { return s.Of > 1 && s.Number < s.Of }

// Namer turns a Command into the line a person reads.
type Namer interface {
	Describe(row importer.Entry) string
}

var ()

// columns are the plan's. The state comes FIRST and is never dropped: if you
// have to read the issue text to know a row is blocked, the screen has failed
// at the only thing it is for.
var columns = []table.Column{
	{Title: "", Min: 2},
	{Title: "ROW", Min: 3, Align: table.Right, Drop: 4},
	{Title: "WHAT IT WOULD DO", Min: 20, Grow: true},
	{Title: "WHY NOT", Min: 12, Drop: 2},
}

// New builds a screen over a bound file.
func New(plan importer.Plan, names Namer) Model {
	m := Model{plan: plan, names: names, width: 100}
	m.tbl = table.New(columns).Fixed()
	return m.refresh()
}

// WithNames replaces how rows are described, for when the vocabulary has
// changed underneath them.
//
// It redraws, because the rows hold their descriptions as text: setting the
// namer without refreshing left every row saying what the OLD namer said, which
// is the opposite of what a caller asks for by calling this.
func (m Model) WithNames(names Namer) Model { m.names = names; return m.refresh() }

// WithStage says which stage of the import this screen is.
//
// It redraws, because the rows hold their reasons as text and one of those
// reasons -- "waits for row 2" -- is true only on a stage that applies in
// passes. Setting the stage without refreshing left every row saying what a
// stage-less screen would have said.
func (m Model) WithStage(stage Stage) Model { m.stage = stage; return m.refresh() }

// WithNote records what the stage before this one did.
func (m Model) WithNote(note string) Model { m.note = note; return m }

// Stage is which stage of the import is on screen, so the caller that has to
// decide what applying it means does not have to remember.
func (m Model) Stage() Stage { return m.stage }

func (m Model) SetSize(width, height int) Model {
	m.width = width
	// The heading and the lines the screen ends with, plus the note when a
	// stage before this one left one. A line taken from around the table has to
	// be taken OFF the table, or the screen grows by one and the last row of it
	// is the one that scrolls away.
	chrome := 3
	if m.note != "" {
		chrome++
	}
	m.tbl = m.tbl.SetSize(width, height-chrome)
	return m
}

// SetOverlay draws lines immediately after the cursor's row, which is how a
// field opens ON the row it belongs to rather than under the whole plan.
//
// Under the plan is not inline; it is a second place to look, and on a screen
// where the row you are correcting is the whole point it is the wrong place.
func (m Model) SetOverlay(lines []string) Model {
	m.tbl = m.tbl.SetOverlay(lines)
	return m
}

// Plan is the file as it now stands.
func (m Model) Plan() importer.Plan { return m.plan }

// Applicable reports whether this stage can be applied, by the rule this stage
// goes by. Asking the plan directly would be asking it a question that has two
// answers, and the screen is the thing that knows which stage it is.
func (m Model) Applicable() bool {
	if m.stage.InPasses {
		return m.plan.ApplicableInPasses()
	}
	return m.plan.Applicable()
}

// WhyNot is the refusal that goes with Applicable, by the same rule.
func (m Model) WhyNot() string {
	if m.stage.InPasses {
		return m.plan.WhyNotInPasses()
	}
	return m.plan.Why()
}

// Current is the entry under the cursor.
func (m Model) Current() (importer.Entry, int, bool) {
	row, ok := m.tbl.Current()
	if !ok {
		return importer.Entry{}, -1, false
	}
	at := int(row.Key)
	if at < 0 || at >= len(m.plan.Entries) {
		return importer.Entry{}, -1, false
	}
	return m.plan.Entries[at], at, true
}

// Update handles a keystroke, reporting whether it was consumed.
func (m Model) Update(msg tea.KeyMsg) (Model, bool) {
	switch keys.Lookup(keys.Plan, msg) {
	case keys.Drop:
		// Dropping is how a row gets settled without being fixed. It is
		// reversible right up until the whole plan is applied, which is why it
		// does not ask.
		if entry, at, ok := m.Current(); ok && entry.State != importer.Dropped {
			m.plan.Entries[at].State = importer.Dropped
			return m.refresh(), true
		}
		return m, true
	case keys.Undrop:
		if _, at, ok := m.Current(); ok {
			m.plan.Entries[at] = importer.Rebind(m.plan.Entries[at])
			return m.refresh(), true
		}
		return m, true
	}
	next, handled := m.tbl.Update(msg)
	m.tbl = next
	return m, handled
}

// Settle replaces an entry, after a person has resolved it.
func (m Model) Settle(at int, entry importer.Entry) Model {
	if at < 0 || at >= len(m.plan.Entries) {
		return m
	}
	m.plan.Entries[at] = entry
	return m.refresh()
}

// RebindUnsettled re-binds every row that is not already settled, against a
// vocabulary that may have grown since the file was read.
func (m Model) RebindUnsettled(vocabulary *command.Vocabulary) Model {
	for i, entry := range m.plan.Entries {
		if entry.State == importer.Ready || entry.State == importer.Dropped {
			continue
		}
		m.plan.Entries[i] = importer.Settle(vocabulary, entry)
	}
	return m.refresh()
}

func (m Model) refresh() Model {
	rows := make([]table.Row, 0, len(m.plan.Entries))
	for i, entry := range m.plan.Entries {
		rows = append(rows, table.Row{
			Key: int64(i),
			Cells: []string{
				mark(entry.State),
				fmt.Sprintf("%d", entry.Row.Line),
				m.names.Describe(entry),
				m.why(i, entry),
			},
		})
	}
	m.tbl = m.tbl.SetRows(rows)
	return m
}

// plural counts rows in English, for the one line that says what A will write.
func plural(n int) string {
	if n == 1 {
		return "1 row"
	}
	return fmt.Sprintf("%d rows", n)
}

// mark is the state, in one character, before any words are read.
func mark(state importer.State) string {
	switch state {
	case importer.Ready:
		return style.Strong.Render("OK")
	case importer.Confirmable:
		return style.Warn.Render("? ")
	case importer.Blocked:
		return style.Error.Render("! ")
	}
	return style.Dim.Render("- ")
}

// why is the first thing standing in the row's way.
func (m Model) why(at int, entry importer.Entry) string {
	switch entry.State {
	case importer.Ready:
		return ""
	case importer.Dropped:
		return "dropped"
	}
	// A row whose parent is made by another row of the same file is not waiting
	// for a person at all. Saying "would create a category" or "would create a
	// location" there invited somebody to make a second one, which is precisely
	// what it must not do.
	//
	// Only on a stage that applies in PASSES, because only there does waiting
	// come to anything: a stage that applies all at once can never make the
	// thing and then bind the row that names it, so on one of those the row's
	// only route really is the creation panel.
	if m.stage.InPasses && !entry.IsCreation() {
		if other, ok := m.plan.WillBeCreatedBy(at); ok {
			return fmt.Sprintf("waits for row %d", m.plan.Entries[other].Row.Line)
		}
	}
	if len(entry.Creates) > 0 {
		return "would create " + text.Article(strings.ToLower(string(entry.Creates[0].Kind)))
	}
	if len(entry.Issues) > 0 {
		return entry.Issues[0].String()
	}
	return "needs confirming"
}

// View is what the plan IS: its heading, what the stage before it did, and its
// rows.
//
// It used to draw its own counts line and its own key hints underneath, which
// made this the fifth place in the interface that rendered chrome -- and the
// only screen whose refusals, filter and status block appeared in a different
// order from everywhere else. The counts are Facts now, and the keys are what
// the layer offers the input line, so this screen ends the way every other
// screen ends.
func (m Model) View() string {
	rows := "   " + plural(len(m.plan.Entries))
	// Which stage of the import this is, and only when there are several: a
	// file of holdings proposes no shape at all, and "stage 1 of 1" describes
	// the screen rather than the file.
	var stage string
	if m.stage.shown() {
		stage = fmt.Sprintf("   STAGE %d OF %d - %s",
			m.stage.Number, m.stage.Of, strings.ToUpper(m.stage.Name))
	}
	// The file's NAME, not the path to it. You chose the file a moment ago; the
	// directory it happens to sit in is not what you are reviewing, and an
	// absolute path ran the heading past the terminal -- 84 columns on an
	// 80-column screen for an ordinary temporary directory. A line wider than
	// the screen wraps, and one wrapped line shifts every row below it.
	//
	// Still elided, from the START, in case the name itself is long: the end of
	// a filename is the part that distinguishes it. The stage counts against
	// the budget too, for the same reason the row count does.
	source := elideStart("IMPORT  "+filepath.Base(m.plan.Source),
		max(12, m.width-len(rows)-len(stage)))
	lines := []string{style.Strong.Render(source) + style.Warn.Render(stage) + style.Dim.Render(rows)}
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

// Facts is what is true of the plan: how many rows are in each state, and
// whether the whole file can be applied yet.
//
// The same role the row counts play under a table -- which is why it is handed
// to the screen to render in the same place rather than drawn here.
//
// Returned as PARTS, most useful first, so the screen can drop from the end on
// a narrow terminal the way it does for every other facts line. As one string
// it ran to 102 columns on a 100-column terminal with nothing watching: this
// was the screen with no frame, and the only one no width check ever saw.
func (m Model) Facts() []string {
	ready, confirmable, blocked, dropped := m.plan.Counts()
	facts := []string{
		style.Strong.Render(fmt.Sprintf("%d ready", ready)),
		style.Warn.Render(fmt.Sprintf("%d need confirming", confirmable)),
		style.Error.Render(fmt.Sprintf("%d blocked", blocked)),
	}
	if dropped > 0 {
		facts = append(facts, style.Dim.Render(fmt.Sprintf("%d dropped", dropped)))
	}

	// Last, because it is the longest and the least surprising: the counts
	// above already say whether anything is in the way.
	apply := keys.Show(keys.Plan, keys.ApplyAll)
	switch reason := m.WhyNot(); {
	case reason != "":
		return append(facts, style.Dim.Render(apply+" is unavailable: "+reason))
	case m.stage.InPasses && confirmable > 0:
		// What a pass applies is the ready rows, and saying so is the only way
		// the count to its left and the key it names agree with each other.
		return append(facts, style.Strong.Render(fmt.Sprintf("%s applies the %s ready now",
			apply, plural(ready))))
	case m.stage.more():
		// What a stage applies is what a stage applies. Saying "all of it" here
		// would be a promise about rows that are not on this screen, and the
		// rows not on this screen are the whole reason there are two stages.
		return append(facts, style.Strong.Render(fmt.Sprintf("%s applies the %s, then stage %d",
			apply, m.stage.Name, m.stage.Number+1)))
	}
	return append(facts, style.Strong.Render(apply+" applies all of it, in one transaction"))
}

// Issues are the whole reason a row is not ready, for the detail line.
func Issues(entry importer.Entry) []string {
	var out []string
	for _, issue := range entry.Issues {
		out = append(out, issue.String())
	}
	return out
}
