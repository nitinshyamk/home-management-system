package importer_test

import (
	"strings"
	"testing"

	"home-management-system/internal/command"
	"home-management-system/internal/importer"
)

// The split into two stages.
//
// A file that proposes a classification and the things filed under it cannot be
// reviewed as one list: the second half's rows have nothing to resolve against
// until the first half has been applied. The split is what makes that a
// sequence rather than a screen full of rows blocked for a reason that is not
// their fault.

// The categories go first, and everything else keeps file order behind them.
func TestSplitPutsTheCategoriesFirst(t *testing.T) {
	categories, review := bind(t, `op,name,under,item,qty,at
new category,Baking,,,,
acquire,,,Turmeric,10,Left Pantry
new category,Flours,Baking,,,
consume,,,Basmati Rice,10,Left Pantry
`).Split()

	if got := lines(categories); !equal(got, []int{2, 4}) {
		t.Errorf("the category stage holds rows %v, want rows 2 and 4", got)
	}
	if got := lines(review); !equal(got, []int{3, 5}) {
		t.Errorf("the review stage holds rows %v, want rows 3 and 5", got)
	}
	// Both halves know which file they are about, because both are a screen
	// that has to say so.
	if categories.Source != "receipt.csv" || review.Source != "receipt.csv" {
		t.Errorf("a stage lost the file name: %q and %q", categories.Source, review.Source)
	}
}

// A file that says nothing about the classification is one stage, and the split
// leaves it whole.
func TestAFileWithoutCategoriesHasNoCategoryStage(t *testing.T) {
	categories, review := bind(t, `op,item,qty,at
acquire,Turmeric,10,Left Pantry
`).Split()

	if len(categories.Entries) != 0 {
		t.Errorf("%d category rows in a file that has none", len(categories.Entries))
	}
	if len(review.Entries) != 1 {
		t.Errorf("%d review rows, want the one the file has", len(review.Entries))
	}
}

// Which stage a row belongs to is read off the spec, so it cannot drift from
// what the commands ARE.
//
// The list here is the assertion -- these four ops and no others are about the
// classification alone -- and it is checked by walking the whole vocabulary,
// so a command added tomorrow lands in one of the two stages deliberately
// rather than wherever a forgotten list happened to put it.
func TestOnlyTheCategoryCommandsAreInTheCategoryStage(t *testing.T) {
	want := map[string]bool{
		"new category": true, "reparent category": true,
		"archive category": true, "restore category": true,
	}
	for _, spec := range command.Specs() {
		row := importer.Row{Raw: command.RawCommand{Op: string(spec.Op)}}
		got := importer.StageOf(row) == importer.StageCategories
		if got != want[string(spec.Op)] {
			t.Errorf("%q is in the %s stage", spec.Op, importer.StageOf(row))
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
