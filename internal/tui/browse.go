package tui

import (
	"fmt"
	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/omnibox"

	tea "github.com/charmbracelet/bubbletea"
)

// Browsing: the layer underneath every mode -- views, verbs, and the keys
// that act on a row.

// handleKey is the application underneath every mode: views, leaders, quit, and
// the verbs that act on a row.
//
// It runs last, after every mode that could be open, and it is the only place
// that sees a keystroke nothing else wanted.
func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	// The surface sees motion, selection, and sorting first. It reports what it
	// did not use, so the keys that belong to the application -- views, quit,
	// enter -- still reach it.
	if next, handled := m.current.Update(msg); handled {
		m.current = next
		return m, nil
	}

	// The verbs come before the generic keys, so that consume, count, move and
	// custody mean what the plan says rather than falling through to motion.
	// Each one decides for itself which views it applies to.
	if next, cmd, handled := m.handleAction(msg); handled {
		return next, cmd
	}

	switch keys.Lookup(keys.Browse, msg) {
	case keys.Quit:
		return m, tea.Quit

	// The three leaders. C-s and M-g feel identical for exactly one keystroke
	// and then diverge completely, which is why the omnibox renders them
	// differently before a word has been read.
	//
	// C-s opens the line whether or not a filter is already on, because Open
	// prefills it with the applied filter -- so refining a filter is the same
	// gesture as making one. Stepping through the matches is M-n and M-p, in
	// the table.
	case keys.Search:
		m.say = m.say.Clear()
		m.box = m.box.Open(omnibox.Filter)
		m = m.applyLive()
		return m, nil
	case keys.Jump:
		m.box = m.box.Open(omnibox.Jump)
		m = m.refreshJump()
		return m, m.loadCandidates()
	case keys.CommandLine:
		m.say = m.say.Clear()
		m.box = m.box.Open(omnibox.Command)
		return m, nil

	// Renames whatever the cursor is on, in place.
	case keys.EditInPlace:
		return m.openEditor(), nil

	// Creates a new one of whatever this view holds, inside what the cursor is
	// on. Creating inside what you are looking at is what it means.
	case keys.Create:
		return m.openCreator(), m.loadCandidates()

	case keys.Confirm:
		// In a table C-f and C-b move between columns, so enter is the only way
		// down into a row -- which is also what it means everywhere else.
		if m.view == viewHoldings {
			if sel, ok := m.current.Current(); ok {
				m.fromView = m.view
				return m, m.loadHistory(domain.HoldingID(sel.Key))
			}
		}
	case keys.Cancel:
		// Escaping in the LIST clears an applied filter -- a different escape
		// from the one that closes the input line.
		if m.box.Applied() != "" {
			m.box = m.box.Clear()
			return m.filterWith(omnibox.Query{}), nil
		}
		// History and Help are both entered from somewhere and left back to
		// it. Neither has a number, so escaping is the only way out.
		if m.view == viewHistory || m.view == viewHelp {
			return m, m.load(m.fromView)
		}
		// Putting down what is being carried, LAST in this chain.
		//
		// Filtering and selecting are things you do while hunting for the
		// destination, so esc has to undo the most recent of those first --
		// otherwise looking for somewhere to put a thing would make you drop
		// it. Dropping a carry with a filter on takes two escapes, which is
		// the same shape as every other nested mode here.
		if m.copied != nil {
			name := m.copied.Name
			m = m.drop()
			m.say = m.say.Report(fmt.Sprintf("put %q down", name))
			return m, nil
		}

	case keys.ViewCategories:
		return m, m.load(viewCategories)
	case keys.ViewLocations:
		return m, m.load(viewLocations)
	case keys.ViewItems:
		return m, m.load(viewItems)
	case keys.ViewHoldings:
		return m, m.load(viewHoldings)
	case keys.ViewIntegrity:
		return m, m.load(viewIntegrity)
	case keys.Refresh:
		return m, m.load(m.view)
	case keys.ShowContents:
		// A tree only. The tables already show what they hold -- that is what
		// a table is -- so the key has nothing to say there.
		if !forest(m.view) {
			return m, nil
		}
		m.contents[m.view] = !m.contents[m.view]
		return m, m.load(m.view)
	}
	return m, nil
}

// handleAction takes the keys that act on whatever the cursor is on.
//
// Each key states its own scope rather than the whole set being gated to one
// view. Paste is why: you copy in the Holdings table and put in the Locations
// tree, so a gate around the lot meant the second half of the gesture never
// fired.
func (m Model) handleAction(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	action := keys.Lookup(keys.Browse, msg)

	// Paste puts wherever the cursor is, which is usually somewhere else.
	if action == keys.Paste {
		next, cmd := m.put()
		return next, cmd, true
	}
	// Copy picks up from wherever the cursor is too. The two halves of one
	// gesture have to have the same reach, and a tree is where the second half
	// usually lands.
	if action == keys.Copy {
		return m.copy(), nil, true
	}
	// Moving a thing the tree is SHOWING is the other half of the same idea:
	// the trees display holdings and items, and the one thing a person wants to
	// do to something they can see somewhere wrong is put it somewhere right.
	if forest(m.view) {
		if action == keys.MoveTo {
			next, cmd := m.promptForNode()
			return next, cmd, true
		}
		return m, nil, false
	}
	// Everything else acts on a Holding, so it needs the view that has them.
	if m.view != viewHoldings {
		return m, nil, false
	}

	switch action {
	case keys.Consume:
		return m.promptFor(editor.Consume, ""), nil, true
	case keys.Count:
		return m.promptFor(editor.Count, ""), nil, true
	case keys.MoveTo:
		// The vocabulary is loaded alongside the prompt, so the first
		// keystroke into it already has something to complete against.
		return m.promptFor(editor.Move, "").carrySubject(), m.loadCandidates(), true
	case keys.ToggleCustody:
		next, cmd := m.toggleCustody()
		return next, cmd, true
	case keys.Kill:
		// One key, where retiring used to take two.
		//
		// The doubled key was the guard against retiring on a slip, and it was
		// never the real one: a retirement is a permanent change, so its plan
		// carries a Permanent fact and the confirmation panel opens on it
		// regardless. The guard is the panel. Asking twice before the thing
		// that asks was two answers to one question.
		next, cmd := m.retire()
		return next, cmd, true
	}
	return m, nil, false
}

// refuse reports why something did not happen, and clears whatever the last
// thing that DID happen said.
//
// Leaving the old status underneath a refusal reads as though both were true --
// the screen saying "counted 50" while also saying the action was impossible.
func (m Model) refuse(format string, args ...any) Model {
	m.say = m.say.Refuse(fmt.Sprintf(format, args...))
	return m
}

// rowsByKey is the holdings currently on screen, by identifier, so a keystroke
// can reach the identifiers behind the row it is on.
func (m Model) holding(key int64) (app.HoldingRow, bool) {
	row, ok := m.holdingRows[key]
	return row, ok
}

// currentHolding is the row under the cursor, in the Holdings view.
func (m Model) currentHolding() (app.HoldingRow, bool) {
	if m.view != viewHoldings {
		return app.HoldingRow{}, false
	}
	sel, ok := m.current.Current()
	if !ok {
		return app.HoldingRow{}, false
	}
	return m.holding(sel.Key)
}

// selectedHoldings is what an action should act on: the explicit selection, or
// the row under the cursor when nothing is picked.
func (m Model) selectedHoldings() []app.HoldingRow {
	if m.view != viewHoldings {
		return nil
	}
	var out []app.HoldingRow
	for _, sel := range m.current.Selected() {
		if row, ok := m.holding(sel.Key); ok {
			out = append(out, row)
		}
	}
	return out
}

// toggleCustody is one key rather than two.
//
// Two keys would mean remembering which state a thing is in before you can act,
// which is what looking at the screen was supposed to be for.
func (m Model) toggleCustody() (Model, tea.Cmd) {
	rows := m.selectedHoldings()
	if len(rows) == 0 {
		return m, nil
	}
	var commands []command.Command
	for _, row := range rows {
		switch row.Custody {
		case "":
			return m.refuse("%q is measured, so there is no custody to change", row.Item), nil
		case "Out", "Lost":
			commands = append(commands, command.Return{Holding: row.ID})
		default:
			commands = append(commands, command.CheckOut{Holding: row.ID})
		}
	}
	return m, m.runCommands(commands)
}

// retire ends a Holding's life. The record persists in history.
func (m Model) retire() (Model, tea.Cmd) {
	rows := m.selectedHoldings()
	if len(rows) == 0 {
		return m, nil
	}
	var commands []command.Command
	for _, row := range rows {
		commands = append(commands, command.Retire{Holding: row.ID})
	}
	return m, m.runCommands(commands)
}
