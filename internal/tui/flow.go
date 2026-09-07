package tui

import "home-management-system/internal/tui/planview"

// flow is the import review: a plan on screen, and the row a panel was opened
// to settle.
//
// One value rather than three fields, because the three were only ever
// meaningful together and one of them carried a sentinel that four call sites
// had to remember. The zero value is "no import", which is what most of the
// program's life looks like.
type flow struct {
	plan   planview.Model
	active bool
	// settling is the row a field or panel was opened for, or noRow. It ties
	// "something is open" back to "which row asked for it", and nothing else
	// distinguishes a panel opened to settle row 3 from one opened to create
	// something in the ordinary way.
	settling int
}

// noRow is "no row is being settled". A named constant rather than a bare -1,
// because -1 read as an index at every site that had to test for it.
const noRow = -1

func newFlow() flow { return flow{settling: noRow} }

// reviewing reports whether the plan screen owns the interface.
func (f flow) reviewing() bool { return f.active }

// settlingRow is the row a panel was opened for, if one was.
func (f flow) settlingRow() (int, bool) { return f.settling, f.active && f.settling != noRow }

// opened records that a field or panel was opened to settle a row.
func (f flow) opened(at int) flow { f.settling = at; return f }

// settled ends whatever settle was in progress, however it ended.
//
// Called on the way out of a field or panel whether it was confirmed or
// cancelled -- which is the fix for the bug it replaces. settling was cleared
// only on the success paths, so escaping a panel opened for row 3 left the
// index behind: "row 3 is being settled" while nothing on screen agreed.
func (f flow) settled() flow { f.settling = noRow; return f }

// done ends the import.
func (f flow) done() flow { return flow{settling: noRow} }
