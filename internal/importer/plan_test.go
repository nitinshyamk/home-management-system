package importer_test

import (
	"context"
	"encoding/json"
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
	return houseWith(t, []string{"Basmati Rice", "Turmeric", "Brown Rice"}, nil)
}

// houseWith builds a kitchen holding the named items.
//
// packaged names the ones that come in packages, because a house where nothing
// does cannot exercise the package grammar -- and `2bag` refusing everywhere
// looks exactly like `2bag` being understood and rejected for a good reason.
func houseWith(t *testing.T, items, packaged []string) *command.Vocabulary {
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
	comesInPackages := map[string]bool{}
	for _, name := range packaged {
		comesInPackages[name] = true
	}
	size := domain.FromMilli(2_000_000)
	for _, name := range items {
		in := origin.CreateBulkItemInput{Name: name, Category: spices, ContentUnit: "g"}
		if comesInPackages[name] {
			in.PackageSize = &size
		}
		item, err := orig.CreateBulkItem(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := led.CreateBulkHolding(ctx, ledger.CreateBulkHoldingInput{
			Item: item, Location: pantry, UnitBasis: domain.BasisContent,
		}); err != nil {
			t.Fatal(err)
		}
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

// ---------------------------------------------------------------------------
// 11c: the dry run
// ---------------------------------------------------------------------------

// The dry run reports the same three states the plan screen shows, from the
// same Bind. Two renderings that could disagree would mean an agent iterating
// against a different contract from the one that finally judges it.
func TestTheDryRunReportsWhatTheScreenWould(t *testing.T) {
	plan := bind(t, strings.Join([]string{
		"op,item,qty,at,name,counting,unit,category",
		"acquire,Basmati Rice,100,Left Pantry,,,,",
		"consume,Tumeric,10,Left Pantry,,,,",
		"consume,rice,10,Left Pantry,,,,",
	}, "\n")+"\n")

	report := importer.NewDryRun(plan, nil)
	ready, confirmable, blocked, _ := plan.Counts()
	if report.Ready != ready || report.Confirmable != confirmable || report.Blocked != blocked {
		t.Errorf("the dry run says %d/%d/%d and the plan says %d/%d/%d",
			report.Ready, report.Confirmable, report.Blocked, ready, confirmable, blocked)
	}
	if report.Rows != len(plan.Entries) {
		t.Errorf("%d rows reported, %d in the plan", report.Rows, len(plan.Entries))
	}

	// Every row carries its line, so an agent can point at what to change.
	for i, entry := range report.Entries {
		if entry.Line != plan.Entries[i].Row.Line {
			t.Errorf("row %d reports line %d", i, entry.Line)
		}
		if entry.State != plan.Entries[i].State.String() {
			t.Errorf("row %d reports %q, the plan says %q", i, entry.State, plan.Entries[i].State)
		}
	}
}

// The issues are the same sentences, so an agent fixing what the report says is
// fixing what a person would have been shown.
func TestTheDryRunCarriesTheIssues(t *testing.T) {
	plan := bind(t, "op,item,qty,at\nconsume,rice,10,Left Pantry\n")
	report := importer.NewDryRun(plan, nil)

	if len(report.Entries[0].Issues) == 0 {
		t.Fatal("a blocked row reports no issues")
	}
	if got := report.Entries[0].Issues[0]; got != plan.Entries[0].Issues[0].String() {
		t.Errorf("issue = %q, the plan says %q", got, plan.Entries[0].Issues[0])
	}
}

// A row that would create something says so. An agent that sees this and did
// not mean it has guessed at something permanent.
func TestTheDryRunNamesWhatWouldBeCreated(t *testing.T) {
	plan := bind(t, "op,item,qty,at,name,counting,unit,category\nnew item,,,,Cardamom,measured,g,Spices\n")
	report := importer.NewDryRun(plan, nil)
	if got := report.Entries[0].Creates; len(got) != 1 || !strings.Contains(got[0], "Cardamom") {
		t.Errorf("creates = %v", got)
	}
}

// It is readable by a program, which is the whole point of it being JSON.
func TestTheDryRunIsValidJSON(t *testing.T) {
	plan := bind(t, "op,item,qty,at\nacquire,Basmati Rice,100,Left Pantry\n")
	var b strings.Builder
	if err := importer.NewDryRun(plan, nil).WriteJSON(&b); err != nil {
		t.Fatal(err)
	}
	var decoded importer.DryRun
	if err := json.Unmarshal([]byte(b.String()), &decoded); err != nil {
		t.Fatalf("not readable: %v", err)
	}
	if decoded.Ready != 1 {
		t.Errorf("ready = %d after a round trip", decoded.Ready)
	}
}
