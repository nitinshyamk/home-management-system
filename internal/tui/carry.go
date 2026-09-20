package tui

import (
	"fmt"

	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/tui/keys"

	tea "github.com/charmbracelet/bubbletea"
)

// Carrying: picking a thing up in one view and putting it down in another.

// carried is a thing picked up by copy, waiting for a put.
//
// Kinded, because there are two things worth relocating and they go to
// different sorts of place: a Holding moves to a Location, an Item is filed
// under a Category. An untyped identifier would let the second land in the
// first, and both are int64.
type carried struct {
	Kind string // "Holding" or "Item"
	ID   int64
	Name string
}

// copy picks up whatever the cursor is on, so a put can relocate it.
//
// M-w and C-y, which is emacs's copy and paste -- and note that the words swap
// sides coming from vim, where yank is the COPY. It is how you relocate
// something when you would rather look for the destination than name it, which
// in a tree is nearly always: the destination is on screen, and naming it would
// be the long way round.
//
// The interface says CARRYING rather than "copied", because nothing is copied:
// there is one thing and it ends up somewhere else. "Copied" belongs to the
// keys, which are emacs's, and it was the wrong word for what happens.
// The gesture crosses the panes now: pick up in the contents, put down on the
// rail. What is picked up is what the contents pane holds -- a Holding in the
// place lens, an Item in the kind lens -- and each goes to the kind of node
// the rail is made of.
func (m Model) copy() Model {
	if m.onRail() {
		return m.refuse("pick up one of the things inside -- a place is moved with %s",
			keys.Show(keys.Browse, keys.CommandLine)+" reparent location")
	}
	sel, ok := m.current.Current()
	if !ok {
		return m.refuse("nothing to pick up here")
	}
	return m.carry(carried{Kind: sel.Kind, ID: sel.ID, Name: sel.Name})
}

// carry picks a thing up, and says so.
//
// What is in hand outlives everything until it is put down -- including the
// view switch that is the middle step of this gesture. It used to be announced
// through the one status string, which every load overwrote, so picking a thing
// up and going to the tree to find its destination left the screen saying
// nothing at all. That is what the separate lifetimes in status are for.
func (m Model) carry(what carried) Model {
	m.copied = &what
	m.say = m.say.Carrying(carryText(what))
	return m.aiming()
}

// aiming keeps the surface's idea of the carry in step with the model's.
//
// Called wherever m.copied changes and wherever the surface is rebuilt, which
// is the second half and the easy one to forget: every load makes a fresh
// surface, and a tree that arrived not knowing something was in hand would
// quietly start offering its items again -- mid-gesture, which is the only
// time it matters.
//
// NOT while the prompt is open, although the thing is just as much in hand
// there. `m` picks the row up AND opens the field over it, and the field is
// drawn after the cursor's row -- so ruling that row out would move the cursor
// to the parent and take the open field with it, away from the thing it is
// asking about. The prompt is the other half of the gesture anyway: you are
// naming the destination rather than going to look for it, and nothing is
// being pointed at. Closing the prompt with the thing still in hand is where
// pointing begins, and that is where this is called again.
func (m Model) aiming() Model {
	if s, ok := m.current.(targeting); ok {
		m.current = s.SetTargeting(m.copied != nil && !m.editor.IsOpen())
	}
	return m
}

// drop puts down whatever is in hand.
//
// The ONE place m.copied becomes nil, paired with the one place it is set.
// Two facts -- what is held, and what the screen says is held -- can only
// agree if one function moves both, and this gesture crosses views, which is
// exactly where a forgotten second update would show. The third fact, the
// tree's idea of what it is offering, comes along through aiming for the same
// reason.
func (m Model) drop() Model {
	m.copied = nil
	m.say = m.say.Dropped()
	return m.aiming()
}

// carrySubject picks up what a prompt was just opened over, so the same gesture
// can be finished by pointing instead of by typing.
//
// Only one thing, and only if the prompt actually opened. `m` on the Holdings
// table acts on the whole selection, and a carry holds exactly one thing -- so
// with several rows picked, `m` stays the typed prompt it has always been, and
// what it writes is unchanged.
func (m Model) carrySubject() Model {
	if !m.editor.IsOpen() {
		return m
	}
	rows := m.selectedHoldings()
	if len(rows) != 1 {
		return m
	}
	return m.carry(carried{Kind: "Holding", ID: int64(rows[0].ID), Name: rows[0].Item})
}

// put relocates what was copied to whatever the cursor is on now.
//
// The pairing is the whole of it: a Holding goes to a Location, an Item goes to
// a Category, and the two mismatches are refused in terms of the things rather
// than as a type error. Putting a holding into a classification is not a
// near-miss to be coerced -- a classification is not anywhere.
func (m Model) put() (Model, tea.Cmd) {
	if m.copied == nil {
		return m.refuse("nothing in hand -- %s picks up the row you are on",
			keys.Show(keys.Browse, keys.Copy)), nil
	}
	kind, id, ok := m.destination()
	if !ok {
		return m.refuse("put it on a place or a classification, not on a thing"), nil
	}
	copied := *m.copied

	switch {
	case copied.Kind == "Holding" && kind == "Location":
		// Said while it is in flight, not only once it has landed. A move is
		// the one gesture here that crosses two views and a round trip, so it
		// is the one with a gap to fall into.
		m = m.drop()
		m.say = m.say.Working(fmt.Sprintf("moving %q ...", copied.Name))
		return m, m.runCommands([]command.Command{
			command.Move{Holding: domain.HoldingID(copied.ID), To: domain.LocationID(id)},
		})
	case copied.Kind == "Item" && kind == "Category":
		m = m.drop()
		m.say = m.say.Working(fmt.Sprintf("filing %q ...", copied.Name))
		return m, m.runCommands([]command.Command{
			command.Reclassify{Item: domain.ItemID(copied.ID), Category: domain.CategoryID(id)},
		})
	case copied.Kind == "Holding":
		return m.refuse("%q is stock -- it goes in a place, and this is a classification",
			copied.Name), nil
	default:
		return m.refuse("%q is a kind of thing -- it is filed under a classification, not kept in a place",
			copied.Name), nil
	}
}

// destination is the container the cursor is on, and what sort of container it
// is, which is what a put needs to know before it can mean anything.
func (m Model) destination() (kind string, id int64, ok bool) {
	switch {
	case m.onRail():
		sel, found := m.current.Current()
		// The synthetic root is the whole house, not a shelf in it. Handing
		// its identifier over would put stock in location 0, which no table
		// has -- the same class of silent wrong write the contained-row guard
		// used to exist for.
		if !found || m.atRailRoot() {
			return "", 0, false
		}
		return sel.Kind, sel.ID, true
	case m.onHoldings():
		row, found := m.currentHolding()
		if !found {
			return "", 0, false
		}
		return "Location", int64(row.LocationID), true
	}
	return "", 0, false
}

// carryText says what is being held, and where it can be put down.
//
// Worded per kind, because where a thing can go is the useful half: a Holding
// goes in a place, an Item is filed under a classification, and a line that
// said only "carrying X" would leave the person to find that out by being
// refused.
//
// One line, unwrapped and unindented: the status block does both, for every
// line it holds, so the block has one left edge rather than four.
func carryText(what carried) string {
	goes := "files it under a classification"
	if what.Kind == "Holding" {
		goes = "puts it in a place"
	}
	return fmt.Sprintf("carrying %q -- %s %s, %s puts it down",
		what.Name,
		keys.Show(keys.Browse, keys.Paste), goes,
		keys.Show(keys.Browse, keys.Cancel))
}
