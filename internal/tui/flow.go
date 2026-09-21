package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/command"
	"home-management-system/internal/importer"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/review"
)

// flow is the import review: the file as it now stands, the screen showing
// it, the stages waiting behind that one, and the row a panel was opened to
// settle.
//
// One value rather than eight fields, because they are only ever meaningful
// together and one of them carries a sentinel that four call sites had to
// remember. The zero value is "no import", which is what most of the
// program's life looks like.
type flow struct {
	// bound is the file, bound. The source of truth: every question about
	// what a row would do, whether the stage can be applied, and what is in
	// the way is asked of this.
	bound importer.Plan
	// names renders identifiers back into names. Kept because the screen is
	// rebuilt from bound every time a row is settled, and a rebuild without
	// it would put identifiers in front of a person.
	//
	// The names themselves rather than the Controller. Asking the Controller
	// meant asking it PER ROW, and app.Controller.Describe builds a whole
	// search index to answer -- every category, location, item and holding,
	// read from the database, once for each row, every time the screen
	// redrew. A five-row receipt cost five whole-vocabulary reads per drop
	// keystroke; a two-hundred-row one cost two hundred.
	names command.Namer
	// screen is bound, as the review drawer shows it: the same rows, already
	// in words. DERIVED, always, and never edited on its own -- two things
	// that have to agree about what a row says is exactly the pair that comes
	// apart. Everything that changes the file goes through bound and then
	// rebuilds this.
	screen review.Model

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

// ---------------------------------------------------------------------------
// The file, and the screen derived from it
// ---------------------------------------------------------------------------

// show builds the screen over a freshly bound file.
func (f flow) show(plan importer.Plan, names command.Namer, stage review.Stage, note string) flow {
	f.bound, f.names = plan, names
	f.screen = review.New("IMPORT", fileHeading(plan.Source), nil).
		WithStage(stage).
		WithNote(note)
	return f.rebuild()
}

// withScreen puts the screen back after a keystroke that moved only the
// cursor.
//
// The one change to the screen that is NOT derived from the file, which is
// why it is the only setter. Everything else goes through bound and rebuilds.
func (f flow) withScreen(s review.Model) flow { f.screen = s; return f }

// rebuild renders the file onto the screen again.
//
// After EVERY change to bound, which is why it is one call rather than a
// re-render written out at each site that settles a row. The wording depends
// on the whole file -- "waits for row 2" is about another row -- so a change
// to one row can change what a different row says, and refreshing only the
// row that moved would leave the other one lying.
func (f flow) rebuild() flow {
	stage := f.screen.Stage()
	f.screen = f.screen.
		WithChanges(asChanges(f.bound, f.names, stage)).
		WithVerdict(f.whyNot())
	return f
}

// applicable reports whether the stage on screen can be applied, by the rule
// this stage goes by.
//
// Asking the plan directly would be asking it a question that has two
// answers, and the flow is the thing that knows which stage it is.
func (f flow) applicable() bool {
	if f.screen.Stage().InPasses {
		return f.bound.ApplicableInPasses()
	}
	return f.bound.Applicable()
}

// whyNot is the refusal that goes with applicable, by the same rule.
func (f flow) whyNot() string {
	if f.screen.Stage().InPasses {
		return f.bound.WhyNotInPasses()
	}
	return f.bound.Why()
}

// current is the bound row under the cursor.
func (f flow) current() (importer.Entry, int, bool) {
	at := f.screen.At()
	if at < 0 || at >= len(f.bound.Entries) {
		return importer.Entry{}, -1, false
	}
	return f.bound.Entries[at], at, true
}

// settle replaces a row, after a person has resolved it.
func (f flow) settle(at int, entry importer.Entry) flow {
	if at < 0 || at >= len(f.bound.Entries) {
		return f
	}
	f.bound.Entries[at] = entry
	return f.rebuild()
}

// drop sets a row aside without fixing it.
//
// Reversible right up until the stage is applied, which is why it does not
// ask.
func (f flow) drop(at int) flow {
	if at < 0 || at >= len(f.bound.Entries) || f.bound.Entries[at].State == importer.Dropped {
		return f
	}
	f.bound.Entries[at].State = importer.Dropped
	return f.rebuild()
}

// undrop binds a row again, which is what picking it back up means.
func (f flow) undrop(at int) flow {
	if at < 0 || at >= len(f.bound.Entries) {
		return f
	}
	f.bound.Entries[at] = importer.Rebind(f.bound.Entries[at])
	return f.rebuild()
}

// withNames replaces how rows are described, for when the vocabulary has
// changed underneath them.
func (f flow) withNames(names command.Namer) flow { f.names = names; return f.rebuild() }

// rebindUnsettled binds every row that is not already settled against a
// vocabulary that may have grown since the file was read.
func (f flow) rebindUnsettled(vocabulary *command.Vocabulary) flow {
	for i, entry := range f.bound.Entries {
		if entry.State == importer.Ready || entry.State == importer.Dropped {
			continue
		}
		f.bound.Entries[i] = importer.Settle(vocabulary, entry)
	}
	return f.rebuild()
}

// ---------------------------------------------------------------------------
// The stages
// ---------------------------------------------------------------------------

// staged starts an import on its first stage, with the rest behind it.
func (f flow) staged(plan importer.Plan, names command.Namer, stage review.Stage, rest []importer.StagePlan) flow {
	f = flow{active: true, settling: noRow, rest: rest}
	return f.show(plan, names, stage, "")
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
func (f flow) onward(plan importer.Plan, names command.Namer, stage review.Stage, note string, applied int) flow {
	f.settling = noRow
	if len(f.rest) > 0 {
		f.rest = f.rest[1:]
	}
	f.applied += applied
	f.inStage, f.passes = 0, 0
	return f.show(plan, names, stage, note)
}

// again brings the same stage back for another pass, keeping the stages that
// are still waiting behind it.
//
// onward would drop one of them on the floor: it is the move to the NEXT stage,
// and a pass over this one is not that.
func (f flow) again(plan importer.Plan, names command.Namer, stage review.Stage, note string, applied int) flow {
	f.settling = noRow
	f.applied += applied
	f.inStage += applied
	f.passes++
	return f.show(plan, names, stage, note)
}

// done ends the import.
func (f flow) done() flow { return flow{settling: noRow} }

// ---------------------------------------------------------------------------
// The flow as a drawer
// ---------------------------------------------------------------------------

// The review is one occupant of the region below the house, like the others.
// It was the last to become one: it predated the drawer and kept its own
// return path in View, which is why it alone did not shrink the house behind
// it until somebody noticed.

func (f flow) name() string { return "import plan" }

// height is what the screen would like, bounded so the house keeps a few rows
// whatever the file proposes. A drawer that squeezed the house to nothing
// would be the full-screen takeover again with a rule drawn across it.
func (f flow) height(m Model) int {
	const (
		leastHouse  = 6
		leastDrawer = 6 // the chrome, and one row to look at
		theRule     = 1
	)
	return max(leastDrawer, min(f.screen.Wants(), m.room()-leastHouse-theRule))
}

func (f flow) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	return m.handleImport(msg)
}

// keys is what the review screen takes, which depends on which stage it is.
//
// Skipping is offered only where it is possible. A key named under a screen
// that ignores it is worse than a key named nowhere: the line at the bottom
// is permanent, so the only thing that makes it worth the row is that it can
// be believed.
func (f flow) keys(Model) string {
	groups := [][]keys.Action{
		{keys.Confirm}, {keys.Drop}, {keys.Undrop}, {keys.ApplyAll},
	}
	if f.screen.Stage().Skippable {
		groups = append(groups, []keys.Action{keys.SkipStage})
	}
	return keys.Hint(keys.Plan, append(groups, []keys.Action{keys.Quit})...)
}

func (f flow) lines(m Model) []string {
	// The field goes INTO the screen, spliced after its row. The creation
	// panel does not: it is about a thing that does not exist yet rather than
	// about the row's text, and it is tall enough that splicing it would push
	// the rows off the screen they are confirming.
	return lines(f.screen.
		SetSize(m.width, f.height(m)).
		SetOverlay(m.editor.SetWidth(m.width).Lines()).
		View())
}

// facts is what the review says about itself, for the line the chrome keeps.
func (f flow) facts(m Model) []string { return f.screen.Facts() }
