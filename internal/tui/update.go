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
	// headings are the rows that begin a section, for the prose views that have
	// sections. Only Help does; the others send none and their TAB does nothing.
	headings []int
	// hint is the view's standing hint -- "enter for history", "TAB fold" --
	// which is all a load has ever had to say. It was called status, and that
	// name is why three unrelated things ended up sharing one field.
	hint string
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
		m = m.refreshJump()
		return m, nil

	case loadedMsg:
		was := m.view
		// The hint, and ONLY the hint. A load used to overwrite the one status
		// string, which is what destroyed the feedback for every write and
		// made the carry invisible.
		m.view, m.say = msg.view, m.say.SetHint(msg.hint)
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
		// A fresh surface knows nothing about what is in hand, and a carry
		// outlives every load it takes to go and find the destination.
		m = m.aiming()

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
			m.say = m.say.Report("nothing to do")
			return m, nil
		}
		if msg.plan.NeedsConfirmation() {
			// Creation is never silent and never one keystroke. The plan says
			// so itself -- a non-empty Permanent IS the signal -- so the
			// interface cannot fail to notice.
			m.confirm = &pendingPlan{plan: msg.plan, summary: msg.summary}
			// The question IS the screen now, so the "working..." that got us
			// here has nothing left to say.
			m.say = m.say.Clear()
			return m, nil
		}
		return m, m.apply(msg.plan, msg.summary)

	case issuesMsg:
		// Humanised HERE, where an error crosses from the controller into
		// something a person reads. It used to happen in the renderer, which
		// meant every future reader of m.problem got the raw text and had to
		// remember: "ops: not enough on hand" is a call stack wearing a
		// sentence.
		m.say = m.say.Refuse(humaniseAll(msg.issues)...)
		return m, nil

	case stageAppliedMsg:
		// A stage committed, and another one follows. The import is not over,
		// so the screen is not given back -- the next stage takes its place.
		// Whatever the last stage refused is answered by its having committed,
		// and what it wrote is said again as the next stage's note.
		m.say = m.say.Clear()
		return m, m.stageApplied(msg.rows)

	case stagedMsg:
		return m.staged(msg), nil

	case importedMsg:
		m.flow = m.flow.done()
		// Said before the load, and it survives it: a load replaces the view's
		// hint and nothing else. This used to need reloadKeepingStatus, which
		// re-ran the load and then put the old string back over the fresh one.
		outcome := fmt.Sprintf("applied %d rows in one transaction", msg.rows)
		if msg.earlier > 0 {
			// A stage before this one, or a pass before it, wrote something
			// too -- and each of those was its own transaction. A report that
			// claimed one when there were two would be wrong about the only
			// thing an all-or-nothing import promises.
			outcome = fmt.Sprintf("applied %s, after %s earlier -- one transaction each",
				rowsPhrase(msg.rows), rowsPhrase(msg.earlier))
		}
		m.say = m.say.Report(outcome)
		m.view = viewHoldings
		return m, m.load(m.view)

	case appliedMsg:
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
		m.say = m.say.Report(msg.summary)
		return m, m.load(m.view)

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
		// A refusal rather than an outcome, which is what it always was: it
		// used to be written into the status string and rendered as though the
		// thing had worked.
		m.say = m.say.Refuse(humanise("error: " + msg.err.Error()))
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
