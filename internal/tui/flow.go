package tui

import (
	"home-management-system/internal/importer"
	"home-management-system/internal/tui/planview"
)

// flow is the import review: the stage on screen, the stage waiting behind it,
// and the row a panel was opened to settle.
//
// One value rather than five fields, because they are only ever meaningful
// together and one of them carries a sentinel that four call sites had to
// remember. The zero value is "no import", which is what most of the program's
// life looks like.
type flow struct {
	plan   planview.Model
	active bool
	// settling is the row a field or panel was opened for, or noRow. It ties
	// "something is open" back to "which row asked for it", and nothing else
	// distinguishes a panel opened to settle row 3 from one opened to create
	// something in the ordinary way.
	settling int
	// review is the stage that has not been shown yet -- the items and holdings
	// waiting behind the bulk category upload -- or nil when this import has
	// only one stage left.
	//
	// It is held as the BOUND plan rather than as a screen, because it will be
	// bound again before it is shown: the stage in front of it may have created
	// the very categories its rows name, and a screen built now would be a
	// screen built against a house that no longer exists.
	review *importer.Plan
	// applied is how many rows the earlier stages of this import have already
	// committed, so the last one can say what the import as a whole did rather
	// than reporting only its own share of it.
	applied int
	// inStage and passes are what the stage ON SCREEN has committed so far, and
	// in how many transactions. A stage that builds a tree applies in passes and
	// reports itself once, when it hands over: a report of the last pass alone
	// would be a report of a fraction of what was written.
	inStage int
	passes  int
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

// staged starts an import on its first stage.
func (f flow) staged(plan planview.Model, review *importer.Plan) flow {
	return flow{plan: plan, active: true, settling: noRow, review: review}
}

// waiting is the stage that has not been shown yet, if there is one. It is what
// decides whether applying the screen in front of you ends the import.
func (f flow) waiting() (importer.Plan, bool) {
	if f.review == nil {
		return importer.Plan{}, false
	}
	return *f.review, true
}

// onward moves to the stage that was waiting, counting what the one before it
// applied.
func (f flow) onward(plan planview.Model, applied int) flow {
	f.plan, f.settling, f.review = plan, noRow, nil
	f.applied += applied
	f.inStage, f.passes = 0, 0
	return f
}

// again brings the same stage back for another pass, keeping the stage that is
// still waiting behind it.
//
// onward would drop that one on the floor: it is the move to the NEXT stage,
// and a pass over this one is not that.
func (f flow) again(plan planview.Model, applied int) flow {
	f.plan, f.settling = plan, noRow
	f.applied += applied
	f.inStage += applied
	f.passes++
	return f
}

// done ends the import.
func (f flow) done() flow { return flow{settling: noRow} }
