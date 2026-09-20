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
	width int
}

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

func (m Model) SetSize(width, height int) Model {
	m.width = width
	m.tbl = m.tbl.SetSize(width, height-3)
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
				why(entry),
			},
		})
	}
	m.tbl = m.tbl.SetRows(rows)
	return m
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
func why(entry importer.Entry) string {
	switch entry.State {
	case importer.Ready:
		return ""
	case importer.Dropped:
		return "dropped"
	}
	if len(entry.Creates) > 0 {
		return "would create " + text.Article(strings.ToLower(string(entry.Creates[0].Kind)))
	}
	if len(entry.Issues) > 0 {
		return entry.Issues[0].String()
	}
	return "needs confirming"
}

// View renders the screen: a heading that says what file this is, the rows, and
// the arithmetic that has to add up to it.
// View is what the plan IS: its heading and its rows.
//
// It used to draw its own counts line and its own key hints underneath, which
// made this the fifth place in the interface that rendered chrome -- and the
// only screen whose refusals, filter and status block appeared in a different
// order from everywhere else. The counts are Facts now, and the keys are what
// the layer offers the input line, so this screen ends the way every other
// screen ends.
func (m Model) View() string {
	rows := fmt.Sprintf("   %d rows", len(m.plan.Entries))
	// The file's NAME, not the path to it. You chose the file a moment ago; the
	// directory it happens to sit in is not what you are reviewing, and an
	// absolute path ran the heading past the terminal -- 84 columns on an
	// 80-column screen for an ordinary temporary directory. A line wider than
	// the screen wraps, and one wrapped line shifts every row below it.
	//
	// Still elided, from the START, in case the name itself is long: the end of
	// a filename is the part that distinguishes it.
	source := elideStart("IMPORT  "+filepath.Base(m.plan.Source), max(12, m.width-len(rows)))
	return style.Strong.Render(source) + style.Dim.Render(rows) + "\n" + m.tbl.View()
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
	if reason := m.plan.Why(); reason != "" {
		return append(facts, style.Dim.Render(apply+" is unavailable: "+reason))
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
