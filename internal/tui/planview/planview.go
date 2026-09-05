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
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"home-management-system/internal/command"
	"home-management-system/internal/importer"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/table"
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

var (
	readyStyle   = lipgloss.NewStyle().Bold(true)
	askStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "94", Dark: "179"})
	blockedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"})
	droppedStyle = lipgloss.NewStyle().Faint(true)
	headingStyle = lipgloss.NewStyle().Bold(true)
	hintStyle    = lipgloss.NewStyle().Faint(true)
)

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
		return readyStyle.Render("OK")
	case importer.Confirmable:
		return askStyle.Render("? ")
	case importer.Blocked:
		return blockedStyle.Render("! ")
	}
	return droppedStyle.Render("- ")
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
		return "would create " + article(string(entry.Creates[0].Kind))
	}
	if len(entry.Issues) > 0 {
		return entry.Issues[0].String()
	}
	return "needs confirming"
}

// article is a or an, because "a item" on the screen a person is deciding
// from reads as carelessness about everything else on it.
func article(kind string) string {
	lower := strings.ToLower(kind)
	if strings.ContainsAny(lower[:1], "aeiou") {
		return "an " + lower
	}
	return "a " + lower
}

// View renders the screen: a heading that says what file this is, the rows, and
// the arithmetic that has to add up to it.
func (m Model) View() string {
	ready, confirmable, blocked, dropped := m.plan.Counts()

	heading := headingStyle.Render("IMPORT  "+m.plan.Source) + hintStyle.Render(
		fmt.Sprintf("   %d rows", len(m.plan.Entries)))

	counts := []string{
		readyStyle.Render(fmt.Sprintf("%d ready", ready)),
		askStyle.Render(fmt.Sprintf("%d need confirming", confirmable)),
		blockedStyle.Render(fmt.Sprintf("%d blocked", blocked)),
	}
	if dropped > 0 {
		counts = append(counts, droppedStyle.Render(fmt.Sprintf("%d dropped", dropped)))
	}

	apply := keys.Show(keys.Plan, keys.ApplyAll)
	footer := strings.Join(counts, "   ")
	if reason := m.plan.Why(); reason != "" {
		footer += hintStyle.Render("   -- " + apply + " is unavailable: " + reason)
	} else {
		footer += headingStyle.Render("   " + apply + " applies all of it, in one transaction")
	}

	return strings.Join([]string{
		heading,
		m.tbl.View(),
		footer,
		hintStyle.Render(keys.Hint(keys.Plan,
			[]keys.Action{keys.Confirm},
			[]keys.Action{keys.Drop},
			[]keys.Action{keys.Undrop},
			[]keys.Action{keys.ApplyAll},
			[]keys.Action{keys.Quit})),
	}, "\n")
}

// Issues are the whole reason a row is not ready, for the detail line.
func Issues(entry importer.Entry) []string {
	var out []string
	for _, issue := range entry.Issues {
		out = append(out, issue.String())
	}
	return out
}
