package importer_test

import (
	"strings"
	"testing"

	"home-management-system/internal/command"
	"home-management-system/internal/importer"
)

// The split into stages.
//
// A file that proposes a shape -- a classification, a set of places, or both --
// and the things filed into it cannot be reviewed as one list: the later rows
// have nothing to resolve against until the earlier ones have been applied. The
// split is what makes that a sequence rather than a screen full of rows blocked
// for a reason that is not their fault.

// The structure goes first -- categories, then places -- and everything else
// keeps file order behind them.
func TestStagesPutTheStructureFirst(t *testing.T) {
	stages := bind(t, `op,name,under,location,item,qty,at
new category,Baking,,,,,
acquire,,,,Turmeric,10,Left Pantry
new location,Top Shelf,,,,,
new category,Flours,Baking,,,,
consume,,,,Basmati Rice,10,Left Pantry
reparent location,,Kitchen,Left Pantry,,,
`).Stages()

	want := []struct {
		stage importer.Stage
		rows  []int
	}{
		{importer.StageCategories, []int{2, 5}},
		{importer.StageLocations, []int{4, 7}},
		{importer.StageReview, []int{3, 6}},
	}
	if len(stages) != len(want) {
		t.Fatalf("%d stages, want %d", len(stages), len(want))
	}
	for i, w := range want {
		if stages[i].Stage != w.stage {
			t.Errorf("stage %d is the %s, want the %s", i+1, stages[i].Stage, w.stage)
		}
		if got := lines(stages[i].Plan); !equal(got, w.rows) {
			t.Errorf("the %s stage holds rows %v, want %v", w.stage, got, w.rows)
		}
		// Every stage knows which file it is about, because every one of them
		// is a screen that has to say so.
		if stages[i].Plan.Source != "receipt.csv" {
			t.Errorf("the %s stage says it came from %q", w.stage, stages[i].Plan.Source)
		}
	}
}

// A file that proposes no shape at all is one stage, and the split leaves it
// whole. Most files are this one.
func TestAFileWithoutStructureIsOneStage(t *testing.T) {
	stages := bind(t, `op,item,qty,at
acquire,Turmeric,10,Left Pantry
`).Stages()

	if len(stages) != 1 || stages[0].Stage != importer.StageReview {
		t.Fatalf("%d stages, starting with the %s", len(stages), stages[0].Stage)
	}
	if len(stages[0].Plan.Entries) != 1 {
		t.Errorf("%d review rows, want the one the file has", len(stages[0].Plan.Entries))
	}
}

// A file of places and the things kept in them is TWO stages, and the places
// are the first of them. An empty categories stage is not conjured up to hold
// the number 1.
func TestAFileOfPlacesIsTwoStages(t *testing.T) {
	stages := bind(t, `op,name,item,qty,at
new location,Top Shelf,,,
acquire,,Turmeric,10,Left Pantry
`).Stages()

	if len(stages) != 2 {
		t.Fatalf("%d stages, want 2", len(stages))
	}
	if stages[0].Stage != importer.StageLocations || stages[1].Stage != importer.StageReview {
		t.Errorf("the stages are %s then %s", stages[0].Stage, stages[1].Stage)
	}
}

// A file with no rows in it is still an import, and still a review screen
// saying so. Nothing at all would make "the first stage" a thing every caller
// had to check for.
func TestAnEmptyFileIsStillOneStage(t *testing.T) {
	stages := bind(t, "op,item,qty,at\n").Stages()
	if len(stages) != 1 || stages[0].Stage != importer.StageReview {
		t.Fatalf("an empty file has %d stages", len(stages))
	}
	if len(stages[0].Plan.Entries) != 0 {
		t.Errorf("%d rows in an empty file", len(stages[0].Plan.Entries))
	}
}

// Which stage a row belongs to is read off the spec, so it cannot drift from
// what the commands ARE.
//
// The table here is the assertion -- these ops and no others are about the
// shape of the house alone -- and it is checked by walking the whole
// vocabulary, so a command added tomorrow lands in a stage deliberately rather
// than wherever a forgotten list happened to put it.
func TestOnlyTheStructuralCommandsAreInAStructuralStage(t *testing.T) {
	want := map[string]importer.Stage{
		"new category": importer.StageCategories, "reparent category": importer.StageCategories,
		"archive category": importer.StageCategories, "restore category": importer.StageCategories,
		"new location": importer.StageLocations, "reparent location": importer.StageLocations,
		"archive location": importer.StageLocations, "restore location": importer.StageLocations,
	}
	for _, spec := range command.Specs() {
		row := importer.Row{Raw: command.RawCommand{Op: string(spec.Op)}}
		expected, structural := want[string(spec.Op)]
		if !structural {
			expected = importer.StageReview
		}
		if got := importer.StageOf(row); got != expected {
			t.Errorf("%q is in the %s stage, want the %s stage", spec.Op, got, expected)
		}
	}
}

// `new item` names a category and is emphatically not a category row. It is the
// case a rule about "commands that only name categories" gets wrong, because
// the only name it takes IS a category.
func TestNewItemIsNotACategoryRow(t *testing.T) {
	row := importer.Row{Raw: command.RawCommand{Op: "new item"}}
	if got := importer.StageOf(row); got != importer.StageReview {
		t.Errorf("`new item` is in the %s stage", got)
	}
}

// A row that moves a thing names a place and is about the THING. It is the case
// a rule reading "any command that names a location" gets wrong, and the reason
// the rule is "every name it takes is of one kind" instead.
func TestMovingAThingIsNotAPlaceRow(t *testing.T) {
	for _, op := range []string{"rehome", "check out", "move", "open", "found"} {
		row := importer.Row{Raw: command.RawCommand{Op: op}}
		if got := importer.StageOf(row); got != importer.StageReview {
			t.Errorf("`%s` is in the %s stage", op, got)
		}
	}
}

// `rename` and `describe` take a name that could mean either tree, and a field
// that could mean several kinds says nothing about which tree the row shapes.
func TestACommandThatCouldMeanEitherTreeIsReviewed(t *testing.T) {
	for _, op := range []string{"rename", "describe"} {
		row := importer.Row{Raw: command.RawCommand{Op: op}}
		if got := importer.StageOf(row); got != importer.StageReview {
			t.Errorf("`%s` is in the %s stage", op, got)
		}
	}
}

// The structural stages are the ones that build a tree. Both of the things a
// stage does beyond applying -- skipping, and applying in passes -- are read
// off that one property rather than decided per stage.
func TestOnlyTheStructuralStagesSayTheyAre(t *testing.T) {
	for stage, want := range map[importer.Stage]bool{
		importer.StageCategories: true,
		importer.StageLocations:  true,
		importer.StageReview:     false,
	} {
		if got := stage.Structural(); got != want {
			t.Errorf("the %s stage reports Structural() = %v", stage, got)
		}
	}
}

// An op nothing recognises goes to the review screen, which is the screen that
// reports it. A stage that swallowed it would hide the one row a person most
// needs to see.
func TestAnUnknownOpGoesToTheReview(t *testing.T) {
	row := importer.Row{Raw: command.RawCommand{Op: "frobnicate"}}
	if got := importer.StageOf(row); got != importer.StageReview {
		t.Errorf("an unknown op is in the %s stage", got)
	}
}

// A creation row IS the creation: it bound, it holds a Command, and agreeing to
// it is all that is left. Agreeing makes it ready, so it is applied in the same
// transaction as every other row of its stage.
func TestConfirmingACreationRowMakesItReady(t *testing.T) {
	plan := bind(t, `op,name,under
new category,Baking,
`)
	entry := plan.Entries[0]
	if entry.State != importer.Confirmable {
		t.Fatalf("a `new category` row is %s, want it to need confirming", entry.State)
	}
	if !entry.IsCreation() {
		t.Fatal("a `new category` row does not report itself as a creation")
	}

	confirmed, ok := importer.ConfirmCreation(entry)
	if !ok || confirmed.State != importer.Ready {
		t.Fatalf("confirming left the row %s (ok=%v)", confirmed.State, ok)
	}
	plan.Entries[0] = confirmed
	if !plan.Applicable() {
		t.Errorf("a confirmed creation row is not applicable: %s", plan.Why())
	}
}

// A row that would create something only because a name it uses is missing is
// NOT a creation to agree to. The thing has to be made before the row can mean
// anything, which is what the creation panel is for.
func TestARowThatNamesSomethingMissingIsNotAConfirmableCreation(t *testing.T) {
	plan := bind(t, `op,item,qty,at
acquire,Cardamom,50,Left Pantry
`)
	entry := plan.Entries[0]
	if len(entry.Creates) == 0 {
		t.Fatal("the row does not say it would create anything")
	}
	if entry.IsCreation() {
		t.Error("a row naming a missing item reports itself as a creation")
	}
	if _, ok := importer.ConfirmCreation(entry); ok {
		t.Error("confirming a row that has nothing bound yet was allowed")
	}
}

// Two rows naming one new thing must create it once. The domain permits two
// categories with one name -- sibling uniqueness was never an integrity rule --
// so the plan is the only place that can see it.
func TestASecondRowCreatingTheSameThingIsRecognised(t *testing.T) {
	plan := bind(t, `op,name,under
new category,Baking,
new category,baking,
`)
	confirmed, ok := importer.ConfirmCreation(plan.Entries[0])
	if !ok {
		t.Fatal("the first row could not be confirmed")
	}
	plan.Entries[0] = confirmed

	at, clash := plan.AlreadyCreatedBy(1)
	if !clash || at != 0 {
		t.Errorf("the second row clashes with row %d (clash=%v), want row 0", at, clash)
	}
	// And a row that creates something else does not clash with it.
	other := bind(t, `op,name,under
new category,Baking,
new category,Preserves,
`)
	other.Entries[0], _ = importer.ConfirmCreation(other.Entries[0])
	if _, clash := other.AlreadyCreatedBy(1); clash {
		t.Error("two different categories were reported as one")
	}
}

func lines(plan importer.Plan) []int {
	out := make([]int, 0, len(plan.Entries))
	for _, entry := range plan.Entries {
		out = append(out, entry.Row.Line)
	}
	return out
}

func equal(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// The tree's rule, beside the receipt's.
//
// A row filed under a category another row creates cannot bind until that row
// has been applied, so the category stage applies what is ready and keeps the
// rest for another pass. The receipt's rule cannot be relaxed the same way: two
// rows of a receipt about one item depend on each other completely.
func TestAStageAppliesInPassesWhatAReceiptWouldRefuse(t *testing.T) {
	plan := bind(t, `op,name,under
new category,Baking,
new category,Flours,Baking
`)
	plan.Entries[0], _ = importer.ConfirmCreation(plan.Entries[0])

	if plan.Applicable() {
		t.Error("the receipt's rule applied a file with a row still to settle")
	}
	if !plan.ApplicableInPasses() {
		t.Errorf("the tree's rule refused a pass with a ready row: %s", plan.WhyNotInPasses())
	}

	// And what the pass leaves behind is the row that was waiting, still
	// carrying the line it came from.
	left := plan.Unapplied()
	if len(left.Entries) != 1 || left.Entries[0].Row.Line != 3 {
		t.Errorf("a pass leaves %v behind, want row 3 alone", lines(left))
	}
	if left.Source != plan.Source {
		t.Errorf("what is left says it came from %q", left.Source)
	}
}

// A blocked row still stops a pass, because a row nobody has looked at is not a
// row to build a tree around.
func TestAPassIsRefusedWhileARowIsBlocked(t *testing.T) {
	plan := bind(t, `op,name,under,nonsense
new category,Baking,,
new category,Flours,,filed under what
`)
	plan.Entries[0], _ = importer.ConfirmCreation(plan.Entries[0])
	if plan.ApplicableInPasses() {
		t.Error("a pass ran with a blocked row on the screen")
	}
	if got := plan.WhyNotInPasses(); !strings.Contains(got, "blocked") {
		t.Errorf("the refusal is %q, and does not say what is blocked", got)
	}
}

// A row waiting for another row is waiting for a ROW, not for a person -- and
// the plan is the only thing that can tell the two apart.
func TestARowWaitingForAnotherRowIsRecognised(t *testing.T) {
	plan := bind(t, `op,name,under
new category,Baking,
new category,Flours,Baking
`)
	at, ok := plan.WillBeCreatedBy(1)
	if !ok || at != 0 {
		t.Errorf("row 3 waits for row %d (found=%v), want the row above it", at, ok)
	}
	// And a row waiting for something nobody is making is not waiting at all.
	alone := bind(t, `op,name,under
new category,Flours,Baking
`)
	if _, ok := alone.WillBeCreatedBy(0); ok {
		t.Error("a row was reported as waiting for a row that does not exist")
	}
}
