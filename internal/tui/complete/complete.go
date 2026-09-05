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
//	Tab        take the highlighted option
//	C-n / C-p  move the highlight
//	S-Tab      put the list away, stay in the field
//	esc / C-g  the same
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
	dismissed bool
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
	return m
}

// Close puts the list away without dismissing it, for an owner that has just
// taken an option or moved on.
func (m Model) Close() Model {
	m.options, m.open, m.selected, m.moved = nil, false, 0, false
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

// Update handles a keystroke.
//
// taken is the option Tab accepted, if any. handled is false for every key the
// list does not own -- including every key at all when it is closed -- so the
// owner sees Tab and C-n as its own bindings unless a list is actually up.
func (m Model) Update(msg tea.KeyMsg) (next Model, taken string, handled bool) {
	if !m.IsOpen() {
		return m, "", false
	}
	switch keys.Lookup(keys.Line, msg) {
	case keys.Complete:
		option, ok := m.Selected()
		if !ok {
			return m, "", false
		}
		// Taking what is already there changes nothing, so Tab is not ours:
		// it falls through and moves on, which is what makes Tab one key
		// rather than two behaviours to keep track of. Typing `g` into the
		// unit field and pressing Tab should reach the next field, not spend a
		// keystroke re-taking the `g`.
		if strings.EqualFold(option, m.typed) {
			return m, "", false
		}
		// Taken, and put away for the rest of the visit. Recomputing the list
		// against the value just taken would re-offer it immediately -- the
		// field would keep saying "TAB to take it" about the thing it is
		// already holding, and the next TAB would have to fight past it.
		m = m.Close()
		m.dismissed = true
		return m, option, true
	case keys.MoveDown:
		m.selected, m.moved = (m.selected+1)%len(m.options), true
		return m, "", true
	case keys.MoveUp:
		m.selected, m.moved = (m.selected-1+len(m.options))%len(m.options), true
		return m, "", true
	case keys.Cancel, keys.Dismiss:
		m = m.Close()
		m.dismissed = true
		return m, "", true
	}
	return m, "", false
}
