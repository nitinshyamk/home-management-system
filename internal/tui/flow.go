package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/importer"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/planview"
)

// flow is the import review: the stage on screen, the stages waiting behind
// it, and the row a panel was opened to settle.
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
	// rest are the stages that have not been shown yet, in the order they will
	// be -- the places waiting behind the categories, the items and holdings
	// waiting behind both -- and empty when the stage on screen is the last.
	//
	// A queue rather than one stage, because a file that proposes both trees
	// and the things filed into them has two screens still to come, and a
	// field that could hold one of them would have had to drop the other.
	//
	// They are held as BOUND plans rather than as screens, because each will be
	// bound again before it is shown: a stage in front of it may have created
	// the very categories or places its rows name, and a screen built now would
	// be a screen built against a house that no longer exists.
	rest []importer.StagePlan
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

// staged starts an import on its first stage, with the rest behind it.
func (f flow) staged(plan planview.Model, rest []importer.StagePlan) flow {
	return flow{plan: plan, active: true, settling: noRow, rest: rest}
}

// waiting is the next stage that has not been shown yet, if there is one. It is
// what decides whether applying the screen in front of you ends the import.
func (f flow) waiting() (importer.StagePlan, bool) {
	if len(f.rest) == 0 {
		return importer.StagePlan{}, false
	}
	return f.rest[0], true
}

// onward moves to the stage that was waiting, counting what the one before it
// applied.
func (f flow) onward(plan planview.Model, applied int) flow {
	f.plan, f.settling = plan, noRow
	if len(f.rest) > 0 {
		f.rest = f.rest[1:]
	}
	f.applied += applied
	f.inStage, f.passes = 0, 0
	return f
}

// again brings the same stage back for another pass, keeping the stages that
// are still waiting behind it.
//
// onward would drop one of them on the floor: it is the move to the NEXT stage,
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

// ---------------------------------------------------------------------------
// The flow as a drawer
// ---------------------------------------------------------------------------

// The plan is one occupant of the region below the house, like the others.
// It was the last to become one: it predated the drawer and kept its own
// return path in View, which is why it alone did not shrink the house behind
// it until somebody noticed.

func (f flow) name() string { return "import plan" }

// height is what the plan would like, bounded so the house keeps a few rows
// whatever the file proposes. A drawer that squeezed the house to nothing
// would be the full-screen takeover again with a rule drawn across it.
func (f flow) height(m Model) int {
	const (
		leastHouse  = 6
		leastDrawer = 6 // the chrome, and one row to look at
		theRule     = 1
	)
	return max(leastDrawer, min(f.plan.Wants(), m.room()-leastHouse-theRule))
}

func (f flow) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	return m.handleImport(msg)
}

// keys is what the plan screen takes, which depends on which stage it is.
//
// Skipping is offered only where it is possible. A key named under a screen
// that ignores it is worse than a key named nowhere: the line at the bottom
// is permanent, so the only thing that makes it worth the row is that it can
// be believed.
func (f flow) keys(Model) string {
	groups := [][]keys.Action{
		{keys.Confirm}, {keys.Drop}, {keys.Undrop}, {keys.ApplyAll},
	}
	if f.plan.Stage().Skippable {
		groups = append(groups, []keys.Action{keys.SkipStage})
	}
	return keys.Hint(keys.Plan, append(groups, []keys.Action{keys.Quit})...)
}

func (f flow) lines(m Model) []string {
	// The field goes INTO the plan, spliced after its row. The creation panel
	// does not: it is about a thing that does not exist yet rather than about
	// the row's text, and it is tall enough that splicing it would push the
	// plan off the screen it is confirming.
	return lines(f.plan.
		SetSize(m.width, f.height(m)).
		SetOverlay(m.editor.SetWidth(m.width).Lines()).
		View())
}

// facts is what the plan says about itself, for the line the chrome keeps.
func (f flow) facts(m Model) []string { return f.plan.Facts() }
