package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// The drawer: one region below the house, and one interface into it.
//
// Everything transient in this interface is drawn there -- the destinations a
// move offers, the verbs a row takes, the plans waiting to be reviewed, the
// plan being reviewed -- because a person should have one place to look
// rather than one per kind of thing.
//
// It is an interface because the region had grown a special case per
// occupant, each wired into the model, the layer list, the renderer and the
// height arithmetic. Four edits to add one, and the edit nobody made was the
// bug: the import plan kept its own return path in View long after the drawer
// existed, so it alone did not shrink the house behind it.

// drawer is whatever is open below the house. It owns the keyboard while it
// is, which is why Update takes the whole Model: a drawer finishes by doing
// something to the house, not by reporting back.
type drawer interface {
	// name is what the mode is called, for mode() and for tests.
	name() string
	// lines is what it draws, full width, including its own heading.
	lines(m Model) []string
	// height is how many lines that comes to.
	height(m Model) int
	// update takes the keystroke.
	update(Model, tea.KeyMsg) (Model, tea.Cmd)
	// keys is what the bottom line advertises while it is open.
	keys(m Model) string
}

// stating is a drawer that describes ITSELF on the facts line, in place of
// the inspector's description of whatever the cursor is on in the house.
//
// Optional, because most do not want it: a list of destinations is described
// by its own heading, and while you are choosing one, what the house is
// pointing at is still worth reading. The plan is the exception -- its counts
// of ready, blocked and dropped rows are the state of the thing you are
// working on, and they were the facts line before the drawer existed.
type stating interface {
	facts(m Model) []string
}

// drawerFacts is what the open drawer says about itself, if it says anything.
func (m Model) drawerFacts() ([]string, bool) {
	d, ok := m.drawer.(stating)
	if !ok {
		return nil, false
	}
	said := d.facts(m)
	return said, len(said) > 0
}

// open puts a drawer up. One place, so the things that have to happen
// alongside -- clearing what the last action said -- cannot be forgotten by
// one caller and not another.
func (m Model) open(d drawer) Model {
	m.drawer = d
	m.say = m.say.Clear()
	return m
}

// close puts it away.
func (m Model) close() Model {
	m.drawer = nil
	return m
}

// flow is the import review, when one is the open drawer, and the zero flow
// otherwise -- whose reviewing() is false, which is what every caller was
// already asking.
//
// The import's state lives in the drawer rather than in a field beside it,
// because two fields that had to agree about whether an import was running
// is exactly the kind of pair that comes apart.
func (m Model) flow() flow {
	f, _ := m.drawer.(flow)
	return f
}

// withFlow puts the import back, or takes it away when it is over.
func (m Model) withFlow(f flow) Model {
	if !f.reviewing() {
		return m.close()
	}
	m.drawer = f
	return m
}

// settled ends whatever settle a field or panel was opened for.
//
// Only an import settles rows, so this does nothing when the region holds
// something else -- or nothing. It is called on the way out of every field and
// panel, including the ones no import opened, and those must not close a
// drawer they know nothing about.
func (m Model) settled() Model {
	f, ok := m.drawer.(flow)
	if !ok {
		return m
	}
	m.drawer = f.settled()
	return m
}

// drawerLines is the region, or nothing.
func (m Model) drawerLines() []string {
	if m.drawer == nil {
		return nil
	}
	return append([]string{m.rule()}, m.drawer.lines(m)...)
}

// drawerRoom is what the region costs the house, including its rule.
func (m Model) drawerRoom() int {
	if m.drawer == nil {
		return 0
	}
	return m.drawer.height(m) + 1
}
