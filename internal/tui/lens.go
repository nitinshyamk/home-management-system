package tui

import (
	"slices"

	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/table"
)

// A lens is which tree the rail is showing. Categories and Locations are one
// widget asking two questions about the same holdings, so a flip keeps the
// subject instead of dropping it the way a tab switch did.
type lens int

const (
	lensPlace lens = iota // where things are
	lensKind              // what things are
)

// lensSpec is everything that differs between them. One table, so a lens
// cannot be half-converted.
type lensSpec struct {
	name string       // header label
	rail resolve.Kind // what the rail's nodes are
	body resolve.Kind // what the contents pane holds
	// root names the synthetic top. A house is a forest, so "all of it" is
	// the one node the tree does not have -- and it is where the flat
	// Holdings tab went.
	root string
	unit string // the rail's rollup, in words
	noun string // what the contents pane counts
	node string // one rail node, singular
	kids string // rail nodes, plural

	columns []table.Column
	facets  map[string]int
}

// Column positions, shared by the load that fills the cells and the fitters
// that decide whether a column earns its width.
const (
	colName = iota
	colAmount
	colWhere
	colTail
)

// without drops columns this set of rows has nothing to put in. Two panes
// cost width and this buys some back; the first two are never candidates.
func without(cols []table.Column, drop ...int) []table.Column {
	out := make([]table.Column, 0, len(cols))
	for i, c := range cols {
		if !slices.Contains(drop, i) {
			out = append(out, c)
		}
	}
	return out
}

var lenses = map[lens]lensSpec{
	lensPlace: {
		name: "BY PLACE", rail: resolve.KindLocation, body: resolve.KindHolding,
		root: "the house", unit: "holdings", noun: "holdings",
		node: "place", kids: "places",
		columns: []table.Column{
			{Title: "ITEM", Min: 10, Grow: true},
			{Title: "QTY", Min: 5, Align: table.Right},
			// The rail points at a SUBTREE, so which shelf inside it is the
			// question three identical rows exist to raise.
			{Title: "WHERE", Min: 10, Drop: 2, Elide: table.ElideStart, Path: true},
			{Title: "FLAGS", Min: 6, Drop: 3},
		},
		facets: map[string]int{
			"item": colName, "name": colName, "qty": colAmount, "state": colAmount,
			"loc": colWhere, "at": colWhere, "flag": colTail,
		},
	},
	lensKind: {
		name: "BY KIND", rail: resolve.KindCategory, body: resolve.KindItem,
		root: "everything", unit: "items", noun: "items",
		node: "category", kids: "categories",
		columns: []table.Column{
			{Title: "ITEM", Min: 10, Grow: true},
			{Title: "ON HAND", Min: 6, Align: table.Right},
			{Title: "WHERE", Min: 8, Drop: 2},
			{Title: "MEASURE", Min: 8, Drop: 3},
		},
		facets: map[string]int{
			"item": colName, "name": colName, "on": colAmount,
			"loc": colWhere, "at": colWhere, "where": colWhere, "unit": colTail,
		},
	},
}

func (l lens) spec() lensSpec { return lenses[l] }

func (l lens) other() lens {
	if l == lensPlace {
		return lensKind
	}
	return lensPlace
}

// lensFor is where a jump destination lives, and whether either lens has one.
func lensFor(k resolve.Kind) (lens, bool) {
	switch k {
	case resolve.KindLocation, resolve.KindHolding:
		return lensPlace, true
	case resolve.KindCategory, resolve.KindItem:
		return lensKind, true
	}
	return lensPlace, false
}
