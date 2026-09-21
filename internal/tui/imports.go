package tui

import (
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/config"
	"home-management-system/internal/intake"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/table"
)

// Reaching an import from inside the house.
//
// `hms import` makes a folder, writes the contract into it, waits while you
// fill it, and runs the planner. That walk is a conversation on stdin and it
// stays on the command line, where a conversation belongs -- the pause in the
// middle of it is measured in hours, not keystrokes.
//
// What does not belong out there is the half that comes AFTER: a plan is
// sitting in a folder and you want to look at it. That is reviewing, the
// house is what you review it against, and quitting the house to do it was
// the "unlinked from the import" complaint in its plainest form.

// waiting is one import folder with a plan in it.
type waiting struct {
	name string
	// plan is the file to open, and shown is the same thing said shortly.
	// The whole path is ~/hms/imports/<name>/plan/<file>, which is mostly the
	// name again -- it filled the column and said nothing.
	plan  string
	shown string
}

// importsMsg carries the folders that have something to review.
type importsMsg struct {
	found []waiting
	err   error
}

// openImports lists what is waiting. It reads the filesystem, so it is a
// command rather than something done while handling a keystroke.
func (m Model) openImports() tea.Cmd {
	return func() tea.Msg {
		root, err := config.ImportsDir()
		if err != nil {
			return importsMsg{err: err}
		}
		names, err := intake.List(root)
		if err != nil {
			return importsMsg{err: err}
		}
		var found []waiting
		for _, name := range names {
			w := intake.Workspace{Name: name, Dir: filepath.Join(root, name)}
			plan, ok, err := w.Plan()
			if err != nil || !ok {
				// A folder mid-walk has no plan yet, which is not an error --
				// it is the normal state of an import somebody is still
				// filling. It simply has nothing to review.
				continue
			}
			found = append(found, waiting{
				name: name, plan: plan, shown: filepath.Base(plan),
			})
		}
		return importsMsg{found: found}
	}
}

// pickImport is the list of plans waiting, as a drawer.
type pickImport struct {
	found []waiting
	tbl   table.Model
}

func newPickImport(found []waiting, width, height int) pickImport {
	rows := make([]table.Row, 0, len(found))
	for i, w := range found {
		rows = append(rows, table.Row{Key: int64(i), Cells: []string{w.name, w.shown}})
	}
	return pickImport{
		found: found,
		tbl: table.New([]table.Column{
			{Title: "IMPORT", Min: 12, Grow: true},
			{Title: "PLAN", Min: 10, Drop: 2},
		}).Fixed().SetRows(rows).SetSize(width, height),
	}
}

func (p pickImport) chosen() (waiting, bool) {
	row, ok := p.tbl.Current()
	if !ok || int(row.Key) >= len(p.found) {
		return waiting{}, false
	}
	return p.found[row.Key], true
}

// handlePick takes the keystroke while the picker is up.
func (m Model) handlePick(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch keys.Lookup(keys.Browse, msg) {
	case keys.Cancel, keys.Quit:
		m.picking = nil
		return m, nil
	case keys.Confirm:
		chosen, ok := m.picking.chosen()
		if !ok {
			m.picking = nil
			return m, nil
		}
		m.picking = nil
		return m.reviewFile(chosen.plan)
	}
	next, _ := m.picking.tbl.Update(msg)
	m.picking.tbl = next
	return m, nil
}

// reviewFile opens a plan file in the drawer, on the house already loaded.
//
// tui.Import builds a whole Model because it is called before the terminal
// exists. Here one is already running, so only the flow is replaced -- which
// is the point: the house you were looking at is the house the plan is
// reviewed against.
func (m Model) reviewFile(path string) (Model, tea.Cmd) {
	flow, err := m.flowFor(path)
	if err != nil {
		return m.refuse("%v", err), nil
	}
	m.flow = flow
	m, _ = m.steer()
	return m, m.load(viewShell)
}

// pickerView draws the picker as a drawer, in the place the plan will take.
func (m Model) pickerView() []string {
	head := fmt.Sprintf("IMPORT  %d waiting to review", len(m.picking.found))
	if len(m.picking.found) == 0 {
		return []string{
			head,
			"  nothing in ~/hms/imports has a plan in it yet -- " +
				"`hms import NAME` starts one",
		}
	}
	return append([]string{head}, lines(m.picking.tbl.View())...)
}
