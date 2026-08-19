package command_test

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"

	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/origin"
	"home-management-system/internal/query"
	"home-management-system/internal/testsupport"
)

// house is a small real database, because Bind reads names, units, items, and
// holdings, and a fake of four related things is a fake of the thing under test.
type house struct {
	ctx    context.Context
	conn   *sql.DB
	v      *command.Vocabulary
	rice   domain.ItemID
	cable  domain.ItemID
	pantry domain.LocationID
	garage domain.LocationID
}

func newHouse(t *testing.T) house {
	t.Helper()
	ctx := context.Background()
	conn := testsupport.NewDB(t)
	led, orig := ledger.New(conn), origin.New(conn)

	pantry, err := led.CreateLocation(ctx, "Left Pantry", nil, "")
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	garage, err := led.CreateLocation(ctx, "Garage", nil, "")
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	cat, err := orig.CreateCategory(ctx, origin.CreateCategoryInput{Name: "Grains"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	size := domain.FromMilli(2_000_000)
	rice, err := orig.CreateBulkItem(ctx, origin.CreateBulkItemInput{
		Name: "Basmati Rice", Category: cat, ContentUnit: "g", PackageSize: &size,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	// Loose Lentils has no package size, which is what makes `2bag` illegal.
	if _, err := orig.CreateBulkItem(ctx, origin.CreateBulkItemInput{
		Name: "Loose Lentils", Category: cat, ContentUnit: "g",
	}); err != nil {
		t.Fatalf("create item: %v", err)
	}
	cable, err := orig.CreateUniqueItem(ctx, origin.CreateUniqueItemInput{
		Name: "USB-C Cable", Category: cat,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	if _, err := led.CreateBulkHolding(ctx, ledger.CreateBulkHoldingInput{
		Item: rice, Location: pantry, UnitBasis: domain.BasisContent,
	}); err != nil {
		t.Fatalf("create holding: %v", err)
	}

	h := house{ctx: ctx, conn: conn, rice: rice, cable: cable, pantry: pantry, garage: garage}
	h.reload(t)
	return h
}

// reload rebuilds the vocabulary, which is loaded once per batch rather than
// per row: every row of a receipt must bind against the SAME picture of the
// world, or an all-or-nothing import is a lie.
func (h *house) reload(t *testing.T) {
	t.Helper()
	v, err := command.LoadVocabulary(h.ctx, query.New(h.conn))
	if err != nil {
		t.Fatalf("load vocabulary: %v", err)
	}
	h.v = v
}

// stockElsewhere puts the same Item in a second place, which is what turns an
// inferable `at` into a question.
func (h *house) stockElsewhere(t *testing.T) {
	t.Helper()
	if _, err := ledger.New(h.conn).CreateBulkHolding(h.ctx, ledger.CreateBulkHoldingInput{
		Item: h.rice, Location: h.garage, UnitBasis: domain.BasisContent,
	}); err != nil {
		t.Fatalf("create holding: %v", err)
	}
	h.reload(t)
}

func (h house) bind(t *testing.T, line string) command.BindResult {
	t.Helper()
	raw, err := command.Parse(line)
	if err != nil {
		t.Fatalf("parse %q: %v", line, err)
	}
	res, err := command.Bind(h.v, raw)
	if err != nil {
		t.Fatalf("bind %q: %v", line, err)
	}
	return res
}

func (h house) mustBind(t *testing.T, line string) command.Command {
	t.Helper()
	res := h.bind(t, line)
	if !res.Ready() {
		t.Fatalf("%q did not bind: %v", line, res.Issues)
	}
	return res.Command
}

// The three spellings survive all the way to the Command, converted where they
// have to be and never guessed at.
func TestBindResolvesTheQuantityGrammar(t *testing.T) {
	h := newHouse(t)

	// A bare number is the item's own unit.
	bare := h.mustBind(t, `consume "Basmati Rice" 100`).(command.Consume)
	if got := bare.Amount.Milli(); got != 100*domain.Scale {
		t.Errorf("`100` = %d milli, want %d", got, 100*domain.Scale)
	}
	// A named unit is converted, exactly.
	kilos := h.mustBind(t, `consume "Basmati Rice" 1.5kg`).(command.Consume)
	if got := kilos.Amount.Milli(); got != 1500*domain.Scale {
		t.Errorf("`1.5kg` = %d milli, want %d", got, 1500*domain.Scale)
	}
	// Packages select the basis rather than converting.
	bags := h.mustBind(t, `acquire "Basmati Rice" 2bag at "Left Pantry"`).(command.Receive)
	if bags.Basis != domain.BasisPackage {
		t.Errorf("`2bag` basis = %q, want Package", bags.Basis)
	}
	if got := bags.Amount.Milli(); got != 2*domain.Scale {
		t.Errorf("`2bag` = %d milli, want 2 whole", got)
	}
}

// H7 at the input boundary: the message says what is wrong, where a constraint
// violation would say only that something is.
func TestBindRejectsPackagesForAnItemThatHasNone(t *testing.T) {
	h := newHouse(t)
	res := h.bind(t, `acquire "Loose Lentils" 2bag at "Left Pantry"`)
	if res.Ready() {
		t.Fatal("bound `2bag` against an item with no package size")
	}
	if !strings.Contains(res.Issues[0].String(), "no package size") {
		t.Errorf("issue = %q", res.Issues[0])
	}
}

// Inexact conversion is rejected rather than rounded, because every quantity
// here is exact and H10 compares replayed state to stored state for equality.
func TestBindRejectsAnInexactConversion(t *testing.T) {
	h := newHouse(t)
	// Rice is measured in g; a volume is not a mass at all.
	res := h.bind(t, `consume "Basmati Rice" 100ml`)
	if res.Ready() {
		t.Fatal("converted millilitres into grams")
	}
	if !strings.Contains(res.Issues[0].String(), "dimension") {
		t.Errorf("issue = %q", res.Issues[0])
	}
}

// `at` disambiguates when an Item is kept in several places. Omitted with
// exactly one candidate it resolves; with several it blocks, because stock
// removed from the wrong shelf is invisible until someone looks.
func TestAtIsInferredOnlyWhenThereIsOneAnswer(t *testing.T) {
	h := newHouse(t)

	only := h.mustBind(t, `consume "Basmati Rice" 100`).(command.Consume)
	if only.Location != h.pantry {
		t.Errorf("location = %d, want the only place it is kept (%d)", only.Location, h.pantry)
	}

	// Put some in the garage too, and the same line stops being answerable.
	h2 := newHouse(t)
	h2.stockElsewhere(t)
	res := h2.bind(t, `consume "Basmati Rice" 100`)
	if res.Ready() {
		t.Fatalf("guessed a location: %+v", res.Command)
	}
	if !strings.Contains(res.Issues[0].String(), "2 places") {
		t.Errorf("issue = %q", res.Issues[0])
	}
	// Saying which resolves it.
	said := h2.mustBind(t, `consume "Basmati Rice" 100 at Garage`).(command.Consume)
	if said.Location != h2.garage {
		t.Errorf("location = %d, want Garage (%d)", said.Location, h2.garage)
	}
}

// A Suggested is an ISSUE, never an answer. This is the asymmetry the whole
// resolver is tuned for, and the place it has to hold.
func TestASuggestionBlocksRatherThanApplying(t *testing.T) {
	h := newHouse(t)
	res := h.bind(t, `consume Basmti 100`)
	if res.Ready() {
		t.Fatalf("a fuzzy match was applied silently: %+v", res.Command)
	}
	if !strings.Contains(res.Issues[0].String(), "did you mean") {
		t.Errorf("issue = %q, want a suggestion", res.Issues[0])
	}
	if !strings.Contains(res.Issues[0].String(), "Basmati Rice") {
		t.Errorf("issue = %q, want it to name the candidate", res.Issues[0])
	}
}

// Every problem with a row is reported at once, because a person fixing a
// receipt wants to see all of it rather than one thing per attempt.
func TestEveryProblemIsReportedTogether(t *testing.T) {
	h := newHouse(t)
	res := h.bind(t, `acquire Nonexistent lots at Nowhere`)
	if res.Ready() {
		t.Fatal("bound a row that is wrong three ways")
	}
	if len(res.Issues) < 2 {
		t.Errorf("%d issues, want one per broken field: %v", len(res.Issues), res.Issues)
	}
}

// A required field that was left blank is Missing rather than a default.
func TestABlankRequiredFieldBlocks(t *testing.T) {
	h := newHouse(t)
	raw := command.RawCommand{Op: "archive location", Fields: map[string]string{
		"location": "Left Pantry",
	}}
	res, err := command.Bind(h.v, raw)
	if err != nil {
		t.Fatal(err)
	}
	if res.Ready() {
		t.Fatalf("archived without saying what happens to the contents: %+v", res.Command)
	}
	if !strings.Contains(res.Issues[0].String(), "resolution") {
		t.Errorf("issue = %q", res.Issues[0])
	}
}

func TestBindRejectsAnUnknownOp(t *testing.T) {
	h := newHouse(t)
	if _, err := command.Bind(h.v, command.RawCommand{Op: "frobnicate"}); err == nil {
		t.Error("bound an op that does not exist")
	}
}

// TestEveryCommandHasASpec and TestEveryCommandBinds walk the generated
// registry, so a variant added without a spec or a binding fails here rather
// than the first time somebody types it.
func TestEveryCommandHasASpec(t *testing.T) {
	for _, c := range command.AllCommands {
		spec, ok := command.SpecOf(c.Op())
		if !ok {
			t.Errorf("%T (%q) has no spec", c, c.Op())
			continue
		}
		if spec.What == "" {
			t.Errorf("%q has no description; hms schema would emit a blank line", c.Op())
		}
		// Positional fields must precede pair-only ones, or the parser would
		// assign them in an order nobody could predict from the schema.
		seenPair := false
		for _, f := range spec.Fields {
			if !f.Positional {
				seenPair = true
				continue
			}
			if seenPair {
				t.Errorf("%q: positional field %q comes after a pair-only one", c.Op(), f.Key)
			}
		}
		for _, f := range spec.Fields {
			if f.Type == command.FieldName && len(f.Kinds) == 0 {
				t.Errorf("%q: field %q resolves a name against every kind at once", c.Op(), f.Key)
			}
		}
	}
}

func TestEveryCommandBinds(t *testing.T) {
	h := newHouse(t)
	for _, c := range command.AllCommands {
		spec, ok := command.SpecOf(c.Op())
		if !ok {
			continue
		}
		// Empty fields: everything will be Missing, and that is the point --
		// what is checked is that binding produced a RESULT rather than falling
		// through with no case.
		raw := command.RawCommand{Op: string(c.Op()), Fields: map[string]string{}}
		res, err := command.Bind(h.v, raw)
		if err != nil {
			t.Errorf("%q: %v", c.Op(), err)
			continue
		}
		for _, issue := range res.Issues {
			if issue.Field == "op" {
				t.Errorf("%q has a spec but no case in build()", c.Op())
			}
		}
		if res.Ready() && len(spec.Fields) > 0 {
			var required int
			for _, f := range spec.Fields {
				if f.Required {
					required++
				}
			}
			if required > 0 {
				t.Errorf("%q bound with nothing given, though %d fields are required",
					c.Op(), required)
			}
		}
	}
}

// TestOneContract is the claim the whole package exists to make true: a typed
// line and a set of named fields -- which is what a CSV row and a JSON Lines
// record are -- bind to the SAME Command.
//
// The moment they diverge, "one contract" is false and the CSV is a second
// interface that merely resembles the first. The CSV and JSON Lines readers
// (11a) produce exactly this RawCommand, so this is where the property is
// pinned rather than in either reader.
func TestOneContract(t *testing.T) {
	h := newHouse(t)

	typed := h.mustBind(t, `acquire "Basmati Rice" 2bag at "Left Pantry" from "corner shop" expires 2027-03-01`)

	// The same thing as columns, in a different order, as a CSV header would
	// give them.
	tabular, err := command.Bind(h.v, command.RawCommand{Op: "acquire", Fields: map[string]string{
		"expires": "2027-03-01",
		"at":      "Left Pantry",
		"item":    "Basmati Rice",
		"from":    "corner shop",
		"qty":     "2bag",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !tabular.Ready() {
		t.Fatalf("the tabular form did not bind: %v", tabular.Issues)
	}
	if !reflect.DeepEqual(typed, tabular.Command) {
		t.Errorf("the two surfaces disagree:\n typed %+v\n table %+v", typed, tabular.Command)
	}
}

// TestAKeystrokeSkipsBindAndLandsInTheSamePlace is the other half of the same
// claim, from the other direction.
//
// A keystroke on a selected row already holds an identifier, so it constructs
// the Command directly rather than serialising to a name and fuzzy-matching it
// back -- which would be lossy in the worst way, since an ambiguous name could
// resolve to a DIFFERENT row than the one under the cursor. What it must not
// skip is the value parsing, because `100` and `100g` have to mean the same
// thing wherever they are written.
func TestAKeystrokeSkipsBindAndLandsInTheSamePlace(t *testing.T) {
	h := newHouse(t)

	bound := h.mustBind(t, `consume "Basmati Rice" 1.5kg reason dinner`)

	// What the TUI does with a row already in hand: the identifiers come from
	// the selection, and only the typed quantity goes through a parser.
	written, err := command.ParseAmount("1.5kg")
	if err != nil {
		t.Fatal(err)
	}
	grams, err := domain.Convert(written.Value,
		domain.Unit{Code: "kg", Dimension: domain.DimensionMass, ToBaseFactor: 1000},
		domain.Unit{Code: "g", Dimension: domain.DimensionMass, ToBaseFactor: 1})
	if err != nil {
		t.Fatal(err)
	}
	constructed := command.Consume{
		Item: h.rice, Location: h.pantry, Amount: grams, Reason: "dinner",
	}

	if !reflect.DeepEqual(bound, command.Command(constructed)) {
		t.Errorf("the keystroke and the line disagree:\n bound %+v\n built %+v", bound, constructed)
	}
}
