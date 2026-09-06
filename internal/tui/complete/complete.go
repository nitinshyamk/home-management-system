// Package complete is the one completion dropdown.
//
// One, because there were two and they had drifted. The creation panel completed
// a whole field, the inline editor completed a trailing token; the panel's Tab
// moved on when there was nothing to take, the editor's Tab did nothing; they
// disagreed about what "already exact" meant. Neither had a selection at all --
// both took suggestions[0], so the second option on the list was decoration.
//
// The keys are the same wherever a dropdown appears:
//
//	Tab            take the highlighted option
//	C-n / C-p, arrows  move the highlight
//	enter          take the highlighted option, once there is one that was
//	               typed towards or moved to; otherwise the owner's
//	S-Tab          put the list away, stay in the field
//	esc / C-g      the same
//	anything else falls through, so typing refines the list
//
// The last line is the important one. Update reports what it did NOT use, which
// is what lets an owner treat Tab as "next field" and C-n as "next field" while
// knowing nothing about whether a list happens to be open over one.
package complete

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/keys"
)

// Model is a list of options over a field.
type Model struct {
	options  []string
	selected int
	open     bool
	// typed is what the field already holds, so the list can tell when taking
	// the highlighted option would change nothing.
	typed string
	// moved records that the highlight was put where it is on purpose.
	//
	// Without it, keeping the highlight across a recompute meant keeping
	// whatever ranked first for the first letter typed: `s` highlighted a
	// shelf, and by `shel` that same shelf had sunk to the bottom of the list
	// while the highlight rode down with it, so the best match was never the
	// one Tab would take.
	moved bool
	// dismissed is per visit to a field, not per field.
	//
	// It is what makes esc mean something here. Without it the list came
	// straight back on the next keystroke, because the next keystroke is what
	// recomputes it -- so esc read as "flicker" rather than as "not this time".
	// Moving to another field clears it, which is what "this visit" means.
	//
	// TAKING an option does NOT set it. It used to, and that was the bug that
	// made a hierarchy impossible to walk: taking "Garage" put the list away
	// for good, so the way to reach "Garage > Metal Shelving Unit > Bay 3" was
	// to type the rest of it by hand, and backspacing to correct a take left a
	// field with no completions at all.
	dismissed bool
	// offset is the first option drawn, so a list longer than the window
	// scrolls instead of being truncated.
	//
	// It is state rather than something derived from selected because a
	// derived window scrolls on every step -- the list slides under a highlight
	// that stays put, which is the opposite of what moving a highlight should
	// look like.
	offset int
}

// Offer puts options on the list.
//
// The highlight goes back to the best match unless it was MOVED on purpose. The
// options are recomputed on every keystroke, so a highlight that always reset
// would make C-n unusable -- the act of typing would undo it -- and one that
// never reset would strand the highlight on whatever happened to rank first for
// the first letter.
func (m Model) Offer(options []string, typed string) Model {
	m.typed = strings.TrimSpace(typed)
	if m.dismissed || len(options) == 0 {
		m.options, m.open = nil, false
		return m
	}
	previous, had := m.Selected()
	m.options, m.open = options, true
	m.selected = 0
	if had && m.moved {
		for i, option := range options {
			if option == previous {
				m.selected = i
				break
			}
		}
	}
	return m.scroll()
}

// scroll moves the window the least amount that puts the highlight inside it.
func (m Model) scroll() Model {
	if m.selected < m.offset {
		m.offset = m.selected
	}
	if m.selected >= m.offset+Window {
		m.offset = m.selected - Window + 1
	}
	if top := len(m.options) - Window; m.offset > top {
		m.offset = top
	}
	if m.offset < 0 {
		m.offset = 0
	}
	return m
}

// Close puts the list away without dismissing it, for an owner that has just
// taken an option or moved on.
func (m Model) Close() Model {
	m.options, m.open, m.selected, m.moved, m.offset = nil, false, 0, false, 0
	return m
}

// Arrive is a new visit to a field: nothing is offered yet, and whatever was
// dismissed last time is forgotten.
func (m Model) Arrive() Model { return Model{} }

// IsOpen reports whether the list is showing.
func (m Model) IsOpen() bool { return m.open && len(m.options) > 0 }

// Options is what is on the list.
func (m Model) Options() []string { return m.options }

// Selected is the highlighted option.
func (m Model) Selected() (string, bool) {
	if !m.IsOpen() || m.selected < 0 || m.selected >= len(m.options) {
		return "", false
	}
	return m.options[m.selected], true
}

// Index is which option is highlighted, for rendering.
func (m Model) Index() int { return m.selected }

// settled reports that the highlight is what the field already holds, so taking
// it would change nothing.
func (m Model) settled() bool {
	option, ok := m.Selected()
	return ok && strings.EqualFold(option, m.typed)
}

// Update handles a keystroke.
//
// taken is the option Tab accepted, if any. handled is false for every key the
// list does not own -- including every key at all when it is closed -- so the
// owner sees Tab and C-n as its own bindings unless a list is actually up.
func (m Model) Update(msg tea.KeyMsg) (next Model, taken string, handled bool) {
	if !m.IsOpen() {
		// C-n brings back a list that was put away.
		//
		// Without this the key is DEAD: a dismissed list is closed, so Update
		// declined it, line.Edit has no motion of its own to give it, and the
		// field swallowed it. Having pressed esc to see the row underneath,
		// there was then no way back to the list short of retyping the field.
		//
		// Only when it was dismissed. A field that has simply not been offered
		// anything yet must still let C-n through, because in the creation
		// panel that is how you reach the next field.
		if m.dismissed {
			switch keys.Lookup(keys.Line, msg) {
			case keys.MoveDown, keys.MoveUp:
				// moved goes with it: the list comes back highlighting the best
				// match, not whatever was highlighted when it was put away.
				m.dismissed, m.moved = false, false
				return m, "", true
			}
		}
		return m, "", false
	}
	switch action := keys.Lookup(keys.Line, msg); action {
	case keys.Complete, keys.Confirm:
		option, ok := m.Selected()
		if !ok {
			return m, "", false
		}
		// Taking what is already there changes nothing, so Tab is not ours:
		// it falls through and moves on, which is what makes Tab one key
		// rather than two behaviours to keep track of. Typing `g` into the
		// unit field and pressing Tab should reach the next field, not spend a
		// keystroke re-taking the `g`.
		if m.settled() {
			return m, "", false
		}
		// Enter takes the highlight too, but only once there is a highlight
		// somebody chose -- by typing towards it or by moving to it.
		//
		// It has to. The highlight is the answer being pointed at, and enter is
		// what a person presses when they have finished pointing: walking to
		// "Bedroom > Bedroom Shelf 2" and pressing enter used to submit the
		// "shelf 2" that had been TYPED, which came back as "could be Basement
		// Shelf 2, Bedroom Shelf 2, Garage Shelf 2, ..." -- the interface
		// refusing to guess at the very thing it had just been told.
		//
		// The condition is what keeps enter from answering a question nobody
		// answered: on an untouched empty field the first option is highlighted
		// because something has to be, and taking it would be the interface
		// choosing a location on the person's behalf.
		if action == keys.Confirm && m.typed == "" && !m.moved {
			return m, "", false
		}
		// Closed, not dismissed. The next recompute decides whether there is
		// still a choice worth showing -- and after taking a parent out of a
		// hierarchy there usually is, which is how the list becomes a way to
		// walk DOWN a path rather than a one-shot guess at the end of one.
		return m.Close(), option, true
	case keys.MoveDown:
		m.selected, m.moved = (m.selected+1)%len(m.options), true
		return m.scroll(), "", true
	case keys.MoveUp:
		m.selected, m.moved = (m.selected-1+len(m.options))%len(m.options), true
		return m.scroll(), "", true
	case keys.Cancel, keys.Dismiss:
		m = m.Close()
		m.dismissed = true
		return m, "", true
	}
	return m, "", false
}
