package tui

import (
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/creator"
)

// The four kinds of row, as the strings selection.Kind carries. Named so a
// switch reads as a question about the row rather than a string comparison.
const (
	kindCategory = string(resolve.KindCategory)
	kindLocation = string(resolve.KindLocation)
	kindItem     = string(resolve.KindItem)
	kindHolding  = string(resolve.KindHolding)
)

// surfaceKind is what a view draws itself on.
type surfaceKind int

const (
	surfaceText surfaceKind = iota
	surfaceShell
)

// viewSpec describes the screens that are not the shell. Columns, facets and
// units moved to lensSpec, because what varies is which tree the rail shows.
type viewSpec struct {
	name string
	// noun is what the footer counts, empty where the view is not a list.
	noun string
	kind surfaceKind
}

// view is which screen is showing. The four browse tabs collapsed into
// viewShell; the rest are places you go to and come back from.
type view int

const (
	viewShell view = iota
	viewHistory
	viewHelp
)

var specs = map[view]viewSpec{
	viewShell:   {name: "House", kind: surfaceShell},
	viewHistory: {name: "History", kind: surfaceText},
	viewHelp:    {name: "Help", kind: surfaceText},
}

// spec is the view's description. An unknown view reads as empty prose rather
// than panicking.
func spec(v view) viewSpec { return specs[v] }

// creates is what `o` makes here: the rail makes what the rail is made of,
// the contents pane makes what it contains. No Holding, deliberately -- stock
// arrives by being acquired.
func (m Model) creates() creator.Kind {
	switch {
	case m.view != viewShell:
		return ""
	case m.onRail() && m.lens == lensPlace:
		return creator.KindLocation
	case m.onRail():
		return creator.KindCategory
	case m.lens == lensKind:
		return creator.KindItem
	}
	return ""
}

// onRail asks what forest(m.view) used to: is the cursor on structure. Naming
// the tab stopped answering it once one screen had both.
func (m Model) onRail() bool {
	sh, ok := m.shell()
	return ok && sh.on == paneRail
}

func (m Model) shell() (shellSurface, bool) {
	sh, ok := m.current.(shellSurface)
	return sh, ok
}

// atRailRoot reports the cursor sitting on the rail's synthetic top, which is
// the whole house rather than any node of it.
func (m Model) atRailRoot() bool {
	sh, ok := m.shell()
	return ok && sh.on == paneRail && sh.atRoot()
}
