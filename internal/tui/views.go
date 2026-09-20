package tui

import (
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/creator"
	"home-management-system/internal/tui/table"
)

// surfaceKind is what a view draws itself on.
//
// Three answers, not two: hierarchy is not something a flat table shows, so
// Categories and Locations are trees; Holdings and Items are tables; and
// Integrity, History and Help are prose that scrolls.
type surfaceKind int

const (
	surfaceText surfaceKind = iota
	surfaceTable
	surfaceTree
)

// viewSpec is everything that differs between one view and the next.
//
// It exists because these facts used to live in nine separate switches, and a
// view was correct only if you remembered all nine. Adding one to a table is a
// thing the compiler and the reader can both see; forgetting a case in the
// ninth switch was silent, and stayed silent until somebody pressed a key.
type viewSpec struct {
	// name is the tab label and the header's suffix.
	name string
	// noun is what the footer counts -- "3 of 12 holdings". Empty for the
	// views that are not a list of anything, which is how countPhrase knows to
	// say nothing rather than to say "0".
	noun string
	kind surfaceKind

	// columns declares a table's shape, and with it what a narrow terminal
	// loses. Drop order is a decision recorded here rather than an accident of
	// layout arithmetic.
	columns []table.Column
	// facets says which column each facet name restricts. Only the view knows
	// that `loc:` means the LOCATION column here and nothing at all in Items.
	facets map[string]int

	// unit names what a tree's rollup counts, which is also the count column's
	// title.
	unit string

	// creates is what `o` makes here, or "" where it makes nothing. Holdings
	// create nothing on purpose: stock arrives by acquiring it, and a Holding
	// is a placement rather than a name.
	creates creator.Kind

	// holds are the kinds of thing that live in this view, which is what a jump
	// reads to know where it is going.
	holds []resolve.Kind
}

var specs = map[view]viewSpec{
	viewCategories: {
		name: "Categories", noun: "categories", kind: surfaceTree,
		unit: "items", creates: creator.KindCategory,
		holds: []resolve.Kind{resolve.KindCategory},
	},
	viewLocations: {
		name: "Locations", noun: "locations", kind: surfaceTree,
		unit: "holdings", creates: creator.KindLocation,
		holds: []resolve.Kind{resolve.KindLocation},
	},
	viewItems: {
		name: "Items", noun: "items", kind: surfaceTable,
		creates: creator.KindItem,
		holds:   []resolve.Kind{resolve.KindItem},
		columns: []table.Column{
			{Title: "ITEM", Min: 10, Grow: true},
			{Title: "ON HAND", Min: 6, Align: table.Right},
			{Title: "CATEGORY", Min: 8, Drop: 2},
			{Title: "MEASURE", Min: 8, Drop: 3},
			{Title: "KIND", Min: 6, Drop: 4},
		},
		facets: map[string]int{"item": 0, "name": 0, "cat": 2, "unit": 3, "kind": 4},
	},
	viewHoldings: {
		name: "Holdings", noun: "holdings", kind: surfaceTable,
		holds: []resolve.Kind{resolve.KindHolding},
		columns: []table.Column{
			{Title: "ITEM", Min: 10, Grow: true},
			// Quantity is never dropped: a holdings table that does not say how
			// much is a list of things you own, which you already knew.
			{Title: "QTY", Min: 5, Align: table.Right},
			// Path: the cell is the shelf's own name, and the ancestors above
			// it appear when the terminal has room to spare for them. Three
			// rows of one item in three different Shelf 1s is the case the
			// holdings table exists to answer, and the leaf alone cannot.
			{Title: "LOCATION", Min: 12, Drop: 2, Elide: table.ElideStart, Path: true},
			{Title: "FLAGS", Min: 6, Drop: 3},
		},
		facets: map[string]int{"item": 0, "qty": 1, "state": 1, "loc": 2, "at": 2, "flag": 3},
	},
	viewIntegrity: {name: "Integrity", kind: surfaceText},
	viewHistory:   {name: "History", kind: surfaceText},
	viewHelp:      {name: "Help", kind: surfaceText},
}

// rowKind is what a row of this view IS. A view holds one kind of thing; the
// trees are the exception, and their rows say their own kind.
func (v viewSpec) rowKind() resolve.Kind {
	if len(v.holds) == 0 {
		return ""
	}
	return v.holds[0]
}

// spec is the view's description. An unknown view reads as an empty text view,
// which renders as nothing rather than panicking -- the same answer the nine
// switches gave by falling through.
func spec(v view) viewSpec { return specs[v] }

// forest reports whether a view is a tree.
func forest(v view) bool { return specs[v].kind == surfaceTree }

// viewFor is where a kind of thing lives.
func viewFor(k resolve.Kind) view {
	for v, s := range specs {
		for _, held := range s.holds {
			if held == k {
				return v
			}
		}
	}
	return viewHoldings
}
