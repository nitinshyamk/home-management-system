package tui

import (
	"fmt"
	"home-management-system/internal/app"
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/table"
	"home-management-system/internal/tui/tree"

	tea "github.com/charmbracelet/bubbletea"
)

// The message loop: what arrives, and what it changes.

// loadedMsg carries a rendered view. Exactly one of rows and cells is set: the
// tree and report views render to lines, the table views to cells.
type loadedMsg struct {
	view        view
	rows        []string
	cells       []table.Row
	nodes       []tree.Node
	holdingRows map[int64]app.HoldingRow
	status      string
}

type errMsg struct{ err error }

// candidatesMsg carries the flat index the jump palette searches, and the unit
// vocabulary the creation panel completes against.
//
// Both in one message because both are read when a panel opens and neither is
// worth a round trip of its own -- the units are seven rows of reference data.
type candidatesMsg struct {
	candidates []resolve.Candidate
	units      []string
}

// planMsg carries a bound-and-planned command back from the controller.
type planMsg struct {
	plan    app.Plan
	summary string
}

// issuesMsg carries the reasons a line could not become a command.
type issuesMsg struct{ issues []string }

// appliedMsg reports that something happened, in the terms of the receipt.
type appliedMsg struct{ summary string }

func (m Model) Init() tea.Cmd { return m.load(m.view) }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.current = m.current.SetSize(msg.Width, m.bodyHeight())
		m.jump = m.jump.SetSize(msg.Width, m.bodyHeight())
		return m, nil

	case loadedMsg:
		was := m.view
		m.view, m.status = msg.view, msg.status
		m.holdingRows = msg.holdingRows

		// A filter belongs to the VIEW it narrowed. `/rice` means nothing in
		// the Locations tree, and carrying it there filtered the whole house
		// down to nothing while the line still claimed to be showing rice.
		if was != msg.view {
			m.box = m.box.Clear()
		}

		// Where the cursor was, so a RELOAD can put it back. Reloading happens
		// after every write, and a cursor that jumped to the top each time
		// would make acting twice on one row impossible -- the second t of a
		// checkout-and-return would land on whatever had sorted first.
		//
		// Only within the same view: carrying a cursor across views would
		// restore a position nobody was in.
		var wasOn int64 = -1
		if was == msg.view {
			if sel, ok := m.current.Current(); ok {
				wasOn = sel.Key
			}
		}

		// The folds come back, because the tree is rebuilt after every write
		// and every toggle and one that sprang open each time would make
		// collapsing it pointless. Saved under the view being LEFT, which is
		// the case that loses them: switching away and back is the reload that
		// does not mention the tree it is replacing.
		if folds, ok := m.current.(folding); ok && forest(was) {
			m.folds[was] = folds.Folds()
		}
		m.current = m.surfaceFor(msg)

		// And the filter, which lives on the omnibox rather than on the
		// surface -- so a fresh surface arrives unfiltered while the line still
		// says it is filtered. Re-applying is what keeps the two agreeing.
		m = m.applyFilter()
		if wasOn >= 0 {
			m = m.focusKey(wasOn)
		}
		if m.pending != nil {
			m = m.focusKey(keyOf(*m.pending))
			m.pending = nil
		}
		return m, nil

	case planMsg:
		if msg.plan.Empty() {
			m.status = "nothing to do"
			return m, nil
		}
		if msg.plan.NeedsConfirmation() {
			// Creation is never silent and never one keystroke. The plan says
			// so itself -- a non-empty Permanent IS the signal -- so the
			// interface cannot fail to notice.
			m.confirm = &pendingPlan{plan: msg.plan, summary: msg.summary}
			m.status = ""
			return m, nil
		}
		return m, m.apply(msg.plan, msg.summary)

	case issuesMsg:
		m.problem = msg.issues
		m.status = ""
		return m, nil

	case importedMsg:
		m.problem = nil
		m.flow = m.flow.done()
		// Said through the load rather than before it, because a loadedMsg
		// carries the view's own status and would otherwise overwrite the only
		// report a whole import ever makes.
		m.status = fmt.Sprintf("applied %d rows in one transaction", msg.rows)
		m.view = viewHoldings
		return m, m.reloadKeepingStatus()

	case appliedMsg:
		m.problem = nil
		// The panel closes only once something was actually created. Escaping
		// the confirmation returns to the panel with what was typed still in
		// it, which is what makes the confirmation a step rather than a
		// dead end.
		m.creator = m.creator.Close()
		// And when it was opened FOR a row of an import, that row is re-bound
		// now that the thing it named exists.
		if at, ok := m.flow.settlingRow(); ok {
			m.flow = m.flow.settled()
			entry, _, _ := m.flow.plan.Current()
			return m, m.rebindRow(at, entry)
		}
		// Reload, because something changed. The list a person is looking at
		// must not disagree with the house.
		m.status = msg.summary
		return m, m.reloadKeepingStatus()

	case reboundMsg:
		return m.rebound(msg), nil

	case candidatesMsg:
		m.candidates, m.units = msg.candidates, msg.units
		m.index = resolve.NewIndex(msg.candidates)
		m.creator = m.creator.WithUnits(msg.units)
		m = m.refreshJump()
		if m.creator.IsOpen() {
			m = m.suggest()
		}
		if m.editor.IsOpen() {
			m = m.suggestForPrompt()
		}
		return m, nil

	case errMsg:
		m.status = "error: " + msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		// The innermost open mode takes it; the list underneath takes what
		// nothing else wanted. See layers.go for the order and why it is that.
		if top, ok := m.topLayer(); ok {
			return top.handle(m, msg)
		}
		return m.handleKey(msg)
	}
	return m, nil
}
