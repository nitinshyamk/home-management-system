package importer_test

import (
	"context"
	"strings"
	"testing"

	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/importer"
	"home-management-system/internal/ledger"
	"home-management-system/internal/origin"
	"home-management-system/internal/query"
	"home-management-system/internal/resolve"
	"home-management-system/internal/testsupport"
)

// house builds enough of a kitchen for a receipt to be about.
func house(t *testing.T) *command.Vocabulary {
	t.Helper()
	ctx := context.Background()
	conn := testsupport.NewDB(t)
	led, orig := ledger.New(conn), origin.New(conn)

	pantry, err := led.CreateLocation(ctx, "Left Pantry", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	spices, err := orig.CreateCategory(ctx, origin.CreateCategoryInput{Name: "Spices"})
	if err != nil {
		t.Fatal(err)
	}
	rice, err := orig.CreateBulkItem(ctx, origin.CreateBulkItemInput{
		Name: "Basmati Rice", Category: spices, ContentUnit: "g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orig.CreateBulkItem(ctx, origin.CreateBulkItemInput{
		Name: "Turmeric", Category: spices, ContentUnit: "g",
	}); err != nil {
		t.Fatal(err)
	}
	// A second rice, so that "rice" is genuinely AMBIGUOUS rather than merely
	// approximate. A house with one of everything cannot tell a suggestion from
	// a tie, and those are the two states the screen most has to separate.
	if _, err := orig.CreateBulkItem(ctx, origin.CreateBulkItemInput{
		Name: "Brown Rice", Category: spices, ContentUnit: "g",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := led.CreateBulkHolding(ctx, ledger.CreateBulkHoldingInput{
		Item: rice, Location: pantry, UnitBasis: domain.BasisContent,
	}); err != nil {
		t.Fatal(err)
	}

	vocabulary, err := command.LoadVocabulary(ctx, query.New(conn))
	if err != nil {
		t.Fatal(err)
	}
	return vocabulary
}

func bind(t *testing.T, body string) importer.Plan {
	t.Helper()
	rows, err := importer.ReadCSV(strings.NewReader(body))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return importer.Bind(context.Background(), house(t), "receipt.csv", rows)
}

// The three states come straight from BindResult, which is binary: a row that
// became a Command has nothing left to decide, and one that did not carries
// exactly the issues explaining why.
func TestTheThreeStates(t *testing.T) {
	plan := bind(t, strings.Join([]string{
		"op,item,qty,at,name,counting,unit,category",
		// Ready: everything resolves.
		"acquire,Basmati Rice,100,Left Pantry,,,,",
		// Confirmable: a suggestion, offered and never applied.
		"consume,Tumeric,10,Left Pantry,,,,",
		// Confirmable: nothing called that, and the panel could make one.
		"consume,Xylophone,10,Left Pantry,,,,",
		// Confirmable: the op itself creates.
		"new item,,,,Cardamom,measured,g,Spices",
		// Blocked: two rices, and only a person can say which.
		"consume,rice,10,Left Pantry,,,,",
		// Blocked: a value that is not a quantity, which no panel can settle.
		"consume,Basmati Rice,lots,Left Pantry,,,,",
	}, "\n")+"\n")

	ready, confirmable, blocked, dropped := plan.Counts()
	if ready != 1 || confirmable != 3 || blocked != 2 || dropped != 0 {
		t.Fatalf("counts = %d ready, %d confirmable, %d blocked, %d dropped",
			ready, confirmable, blocked, dropped)
	}
	// The arithmetic has to add up to the file, always.
	if ready+confirmable+blocked+dropped != len(plan.Entries) {
		t.Error("a row vanished from the arithmetic")
	}
}

// A ready row has a Command; a row that is not ready has none. There is no
// third possibility, which is what makes the states the shape of the result
// rather than a UI invention.
func TestOnlyReadyRowsHaveCommands(t *testing.T) {
	plan := bind(t, "op,item,qty,at\nacquire,Basmati Rice,100,Left Pantry\nconsume,Basmati Rice,lots,Left Pantry\n")
	for _, e := range plan.Entries {
		if (e.Command != nil) != (e.State == importer.Ready) {
			t.Errorf("row %d is %s and its Command is %v", e.Row.Line, e.State, e.Command != nil)
		}
	}
}

// All-or-nothing: A stays unavailable until every row is settled, and the
// refusal is a reason rather than a key that does nothing.
func TestApplyIsRefusedUntilEverythingIsSettled(t *testing.T) {
	plan := bind(t, "op,item,qty,at\nacquire,Basmati Rice,100,Left Pantry\nconsume,Basmati Rice,lots,Left Pantry\n")
	if plan.Applicable() {
		t.Error("a plan with a blocked row is applicable")
	}
	if got := plan.Why(); !strings.Contains(got, "blocked") {
		t.Errorf("Why() = %q", got)
	}

	// Dropping the blocked row settles it.
	plan.Entries[1].State = importer.Dropped
	if !plan.Applicable() {
		t.Errorf("still not applicable after dropping: %s", plan.Why())
	}
	if got := len(plan.Commands()); got != 1 {
		t.Errorf("%d commands, want the one ready row", got)
	}
}

// A plan of nothing is not applicable, or A would report success for having
// done nothing.
func TestAPlanOfNothingIsNotApplicable(t *testing.T) {
	plan := bind(t, "op,item,qty,at\nconsume,Basmati Rice,lots,Left Pantry\n")
	plan.Entries[0].State = importer.Dropped
	if plan.Applicable() {
		t.Error("a plan with everything dropped is applicable")
	}
	if got := plan.Why(); !strings.Contains(got, "nothing left") {
		t.Errorf("Why() = %q", got)
	}
}

// TestTwoRowsNamingOneNewItemCreateItOnce is the property the whole design is
// arranged around: a receipt cannot produce two Turmerics.
//
// Settled here, where both rows are visible at once, rather than by hoping the
// operations layer notices.
func TestTwoRowsNamingOneNewThingMergeIntoOne(t *testing.T) {
	plan := bind(t, strings.Join([]string{
		"op,item,qty,at,name,counting,unit,category",
		"new item,,,,Cardamom,measured,g,Spices",
		"new item,,,,cardamom,measured,g,Spices",
		"new item,,,,Nutmeg,measured,g,Spices",
	}, "\n")+"\n")

	creations := plan.MergeCreations()
	if len(creations) != 2 {
		t.Fatalf("%d creations, want 2: %v", len(creations), creations)
	}
	// And a dropped row's creation goes with it.
	plan.Entries[2].State = importer.Dropped
	if got := plan.MergeCreations(); len(got) != 1 {
		t.Errorf("%d creations after dropping Nutmeg: %v", len(got), got)
	}
}

// A `new …` row binds perfectly well and still needs a person, because
// creation is never silent. The op says so; nothing about the bind result
// could.
func TestACreatingRowNeedsConfirmingEvenThoughItBinds(t *testing.T) {
	plan := bind(t, "op,item,qty,at,name,counting,unit,category\nnew item,,,,Cardamom,measured,g,Spices\n")
	entry := plan.Entries[0]
	if entry.State != importer.Confirmable {
		t.Fatalf("a creating row is %s", entry.State)
	}
	if entry.Command == nil {
		t.Error("it did not bind; it should have, and still need confirming")
	}
	if got := entry.Creates; len(got) != 1 || got[0].Kind != resolve.KindItem || got[0].Name != "Cardamom" {
		t.Errorf("creates = %v", got)
	}
}

// A Missing name could be created -- the panel collects the fields the row
// could not carry. An AMBIGUOUS one could not: the name exists several times
// over, so making another is the last thing anybody wants.
func TestAnAmbiguousNameIsBlockedRatherThanCreatable(t *testing.T) {
	plan := bind(t, "op,item,qty,at\nconsume,rice,10,Left Pantry\n")
	entry := plan.Entries[0]
	if entry.State != importer.Blocked {
		t.Errorf("an ambiguous name is %s, want blocked", entry.State)
	}
	if len(entry.Creates) != 0 {
		t.Errorf("it offers to create %v", entry.Creates)
	}
}

// A missing name IS creatable, which is what the plan screen's "or create a new
// item" is for.
func TestAMissingNameIsCreatable(t *testing.T) {
	plan := bind(t, "op,item,qty,at\nacquire,Xylophone,10,Left Pantry\n")
	entry := plan.Entries[0]
	if entry.State != importer.Confirmable {
		t.Fatalf("a missing name is %s, want confirmable", entry.State)
	}
	if got := entry.Creates; len(got) != 1 || got[0].Name != "Xylophone" {
		t.Errorf("creates = %v", got)
	}
}

// A row with one creatable name and one genuinely broken field is not
// half-acceptable.
func TestOneBadFieldBlocksAnOtherwiseCreatableRow(t *testing.T) {
	plan := bind(t, "op,item,qty,at\nacquire,Xylophone,lots,Left Pantry\n")
	if got := plan.Entries[0].State; got != importer.Blocked {
		t.Errorf("state = %s, want blocked", got)
	}
}

// A typo'd column means the row does not say what it looks like it says, so it
// is blocked before anything tries to interpret it.
func TestAnUnknownColumnBlocksTheRow(t *testing.T) {
	plan := bind(t, "op,item,qty,at,resaon\nconsume,Basmati Rice,10,Left Pantry,dinner\n")
	if plan.Entries[0].State != importer.Blocked {
		t.Errorf("a row with a misspelt column is %s", plan.Entries[0].State)
	}
	if len(plan.Entries[0].Issues) == 0 || !strings.Contains(plan.Entries[0].Issues[0].Problem, "no such field") {
		t.Errorf("issues = %v", plan.Entries[0].Issues)
	}
}
