package tui

import (
	"fmt"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/omnibox"
	"home-management-system/internal/tui/tree"

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
	was := m.railAt()
	if next, handled := m.current.Update(msg); handled {
		m.current = next
		// The rail moving is the one motion that changes what the other half
		// should be showing, and the contents of a node is the controller's
		// answer rather than something the surface can filter its way to. So
		// the cursor landing on a different node is a load.
		//
		// Only on a CHANGE: stepping through the contents pane moves a cursor
		// inside a list that is already correct, and reloading for that would
		// be a query per keystroke for no new rows.
		if at := m.railAt(); at != was {
			m = m.rememberRail()
			return m, m.load(viewShell)
		}
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
		// Enter goes INTO the thing under the cursor, which is one idea with
		// two landings: on the rail it is the contents of the node, and in the
		// contents pane there is nothing further in but the ledger.
		if m.view == viewShell && m.onRail() {
			if next, handled := m.current.Update(rightKey); handled {
				m.current = next
			}
			return m, nil
		}
		if sel, ok := m.current.Current(); ok && sel.Kind == kindHolding {
			m.fromView = m.view
			return m, m.loadHistory(domain.HoldingID(sel.Key))
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

	case keys.ViewAttention:
		m.fromView = viewShell
		return m, m.load(viewIntegrity)

	case keys.OpenImport:
		m.say = m.say.Working("looking for plans to review ...")
		return m, m.openImports()

	case keys.Act:
		offers := m.offersFor()
		if len(offers) == 0 {
			return m.refuse("nothing to do to this"), nil
		}
		m.say = m.say.Clear()
		p := newPalette(offers, m.width, len(offers)+3)
		m.acting = &p
		return m, nil
	case keys.Refresh:
		return m, m.load(m.view)

	case keys.LensFlip:
		return m.flipLens()

	case keys.ToggleDepth:
		if m.view != viewShell {
			return m, nil
		}
		m.deep = !m.deep
		if m.deep {
			m.say = m.say.Report("showing everything below here")
		} else {
			m.say = m.say.Report("showing only what is filed here")
		}
		return m, m.load(m.view)
	}
	return m, nil
}

// rightKey is a synthetic C-f, for the one place the application means "do
// what the right arrow does" rather than handling a key itself.
//
// Enter on the rail and the right arrow on the rail are the same intent --
// go into this -- and writing it as a call rather than as duplicated pane
// logic is what keeps them from drifting apart.
var rightKey = tea.KeyMsg{Type: tea.KeyCtrlF}

// flipLens swaps the rail between the places and the kinds, KEEPING what is
// under the cursor.
//
// This is the whole reason the two trees stopped being two tabs. Standing on a
// pile of chile in the garage and asking "what kind of thing is this" should
// land on Dried Peppers, not at the top of the taxonomy -- and the reverse
// should come back to a shelf the item is actually on. A flip that lost the
// subject would be two tabs with a different key.
func (m Model) flipLens() (Model, tea.Cmd) {
	if m.view != viewShell {
		return m, nil
	}
	to := m.lens.other()
	if key, said, ok := m.crossing(to); ok {
		m.railKey[to] = key
		m.say = m.say.Report(said)
	} else {
		m.say = m.say.Report(to.spec().name + " - " + to.spec().root)
	}
	m.lens = to
	return m, m.load(m.view)
}

// crossing is where the thing under the cursor lives in the other lens.
//
// Four cases, because there are four kinds of row and each crosses
// differently: an Item is classified, a Holding is classified through its
// Item, a Location has no kind of its own, and a Category has no place.
// The two that cannot cross say so by returning false rather than by landing
// somewhere arbitrary.
func (m Model) crossing(to lens) (key int64, said string, ok bool) {
	sel, has := m.current.Current()
	if !has {
		return 0, "", false
	}
	switch to {
	case lensKind:
		var item app.ItemRow
		switch sel.Kind {
		case kindHolding:
			row, found := m.holding(sel.Key)
			if !found {
				return 0, "", false
			}
			item, found = m.item(row.ItemID)
			if !found {
				return 0, "", false
			}
		case kindItem:
			var found bool
			item, found = m.item(domain.ItemID(sel.ID))
			if !found {
				return 0, "", false
			}
		default:
			return 0, "", false
		}
		return tree.Node{ID: int64(item.CategoryID), Kind: kindCategory}.Key(),
			fmt.Sprintf("by kind - %q is filed in %s", item.Name, item.Category), true

	case lensPlace:
		var id domain.ItemID
		switch sel.Kind {
		case kindItem:
			id = domain.ItemID(sel.ID)
		case kindHolding:
			row, found := m.holding(sel.Key)
			if !found {
				return 0, "", false
			}
			id = row.ItemID
		default:
			return 0, "", false
		}
		rows, err := m.ctrl.HoldingsOfItem(m.ctx, id)
		if err != nil || len(rows) == 0 {
			return 0, "", false
		}
		where := rows[0].LocationPath
		if where == "" {
			where = rows[0].Location
		}
		if len(rows) > 1 {
			where = fmt.Sprintf("%s, and %d other places", where, len(rows)-1)
		}
		return tree.Node{ID: int64(rows[0].LocationID), Kind: kindLocation}.Key(),
			fmt.Sprintf("by place - %q is in %s", rows[0].Item, where), true
	}
	return 0, "", false
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
	// The rail is renamed with `e` and created into with `o`. Re-parenting a
	// place is still the typed command until the move gesture is one thing.
	if m.onRail() {
		if action == keys.MoveTo {
			return m.refuse("a place is moved with %s reparent location",
				keys.Show(keys.Browse, keys.CommandLine)), nil, true
		}
		return m, nil, false
	}
	// In the kind lens the contents are Items, and the one thing you do to an
	// Item from here is file it somewhere else. Consuming would have to ask
	// which pile.
	if m.lens == lensKind {
		if action == keys.MoveTo {
			next, cmd := m.promptForNode()
			return next, cmd, true
		}
		return m, nil, false
	}
	// Everything below acts on a Holding, and only the place lens has them:
	// the kind lens's rows are Items, which are a definition rather than a
	// thing on a shelf. Consuming one would have to ask which pile.
	if !m.onHoldings() {
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

// onHoldings reports that the cursor is in a contents pane whose rows are
// Holdings -- the place lens, in the shell, not on the rail.
//
// This is what `m.view == viewHoldings` used to ask, and the question has not
// changed: is the thing under the cursor a physical pile somebody can consume
// from. What changed is that the answer is no longer the name of a tab.
func (m Model) onHoldings() bool {
	return m.view == viewShell && m.lens == lensPlace && !m.onRail()
}

// currentHolding is the row under the cursor, where that is a Holding.
func (m Model) currentHolding() (app.HoldingRow, bool) {
	if !m.onHoldings() {
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
	if !m.onHoldings() {
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
