package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/origin"
	"home-management-system/internal/testsupport"
)

var clock = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

func evAt() domain.EventBase { return domain.EventBase{OccurredAt: clock} }

type harness struct {
	ctx  context.Context
	ctrl app.Controller
	l    *ledger.Processor
	o    *origin.Originator

	spices, peppers domain.CategoryID
	pantry, garage  domain.LocationID
	rice, cable     domain.ItemID
	sealed, opened  domain.HoldingID
	cableHolding    domain.HoldingID
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	conn := testsupport.NewDB(t)
	l := ledger.New(conn).WithClock(func() time.Time { return clock })
	o := origin.New(conn)

	h := &harness{
		ctx:  context.Background(),
		ctrl: app.OpenWithClock(conn, func() time.Time { return clock }),
		l:    l,
		o:    o,
	}

	var err error
	if h.spices, err = o.CreateCategory(h.ctx, origin.CreateCategoryInput{Name: "Spices"}); err != nil {
		t.Fatalf("category: %v", err)
	}
	if h.peppers, err = o.CreateCategory(h.ctx, origin.CreateCategoryInput{
		Name: "Dried Peppers", Parent: &h.spices,
	}); err != nil {
		t.Fatalf("category: %v", err)
	}
	if h.pantry, err = l.CreateLocation(h.ctx, "Pantry", nil, ""); err != nil {
		t.Fatalf("location: %v", err)
	}
	if h.garage, err = l.CreateLocation(h.ctx, "Garage", nil, ""); err != nil {
		t.Fatalf("location: %v", err)
	}

	twoKg := domain.FromMilli(2_000_000)
	if h.rice, err = o.CreateBulkItem(h.ctx, origin.CreateBulkItemInput{
		Name: "Basmati Rice", Category: h.spices, ContentUnit: "g", PackageSize: &twoKg,
	}); err != nil {
		t.Fatalf("item: %v", err)
	}
	if h.cable, err = o.CreateUniqueItem(h.ctx, origin.CreateUniqueItemInput{
		Name: "USB-C Cable", Category: h.peppers,
	}); err != nil {
		t.Fatalf("item: %v", err)
	}

	if h.sealed, err = l.CreateBulkHolding(h.ctx, ledger.CreateBulkHoldingInput{
		Item: h.rice, Location: h.pantry, UnitBasis: domain.BasisPackage,
	}); err != nil {
		t.Fatalf("holding: %v", err)
	}
	if h.opened, err = l.CreateBulkHolding(h.ctx, ledger.CreateBulkHoldingInput{
		Item: h.rice, Location: h.pantry, UnitBasis: domain.BasisContent,
	}); err != nil {
		t.Fatalf("holding: %v", err)
	}
	if h.cableHolding, err = l.CreateUniqueHolding(h.ctx, ledger.CreateUniqueHoldingInput{
		Item: h.cable, Location: h.pantry, Label: "the good one",
	}); err != nil {
		t.Fatalf("holding: %v", err)
	}
	return h
}

func (h *harness) apply(t *testing.T, events ...domain.Event) {
	t.Helper()
	if _, err := h.l.ApplyBatch(h.ctx, events); err != nil {
		t.Fatalf("apply: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The third 24-way dispatch
// ---------------------------------------------------------------------------

// TestSummariseHandlesEveryEventType walks the generated registry, the same way
// the fold and payload tests do. Three switches over one sealed interface is the
// cost of not using visitors; the registry is what makes the cost safe.
func TestSummariseHandlesEveryEventType(t *testing.T) {
	if len(domain.AllEventTypes) != 23 {
		t.Fatalf("registry has %d types, want 23", len(domain.AllEventTypes))
	}
	for _, e := range domain.AllEventTypes {
		got := app.Summarise(e)
		if got == "" {
			t.Errorf("%T renders as an empty string", e)
		}
		if strings.Contains(got, "unrendered event") {
			t.Errorf("Summarise has no case for %T", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Views
// ---------------------------------------------------------------------------

func TestCategoryTreeRollsUp(t *testing.T) {
	h := newHarness(t)

	rows, err := h.ctrl.CategoryTree(h.ctx, false)
	if err != nil {
		t.Fatalf("category tree: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].Name != "Spices" || rows[0].Depth != 0 {
		t.Errorf("row 0 = %+v, want Spices at depth 0", rows[0])
	}
	if rows[1].Name != "Dried Peppers" || rows[1].Depth != 1 {
		t.Errorf("row 1 = %+v, want Dried Peppers at depth 1", rows[1])
	}
	// The rollup spans descendants, which is what lets Items sit at any node.
	if rows[0].Count != 2 {
		t.Errorf("Spices rolls up %d items, want 2", rows[0].Count)
	}
	if rows[1].Count != 1 {
		t.Errorf("Dried Peppers rolls up %d items, want 1", rows[1].Count)
	}
}

// TestOnHandDispatchesOnKind is D7: a sum for Bulk, a count for Unique. The
// domain model assumed one formula.
func TestOnHandDispatchesOnKind(t *testing.T) {
	h := newHarness(t)
	twoBags, err := domain.FromWhole(2)
	if err != nil {
		t.Fatal(err)
	}
	h.apply(t,
		domain.Acquired{EventBase: evAt(), Holding: h.sealed, Delta: twoBags},
		domain.Acquired{EventBase: evAt(), Holding: h.opened, Delta: domain.FromMilli(800_000)},
	)

	rows, err := h.ctrl.Items(h.ctx)
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	byName := map[string]app.ItemRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}

	// Bulk: two sealed 2 kg bags plus 800 g open, summed in content units across
	// both holdings.
	//
	// The expectation is DERIVED from domain.Scale rather than written out,
	// because the SQL divides by the scale as a literal. If the two ever
	// disagree, on-hand is wrong by a factor of a thousand and this fails.
	packages := int64(2) * domain.Scale       // 2 packages, in milli
	perPackage := int64(2_000) * domain.Scale // 2000 g, in milli
	openMilli := int64(800) * domain.Scale    // 800 g, in milli
	wantMilli := (packages*perPackage)/domain.Scale + openMilli
	wantOnHand := domain.FromMilli(wantMilli).String() + " g"

	rice := byName["Basmati Rice"]
	if rice.OnHand != wantOnHand {
		t.Errorf("rice on hand = %q, want %q", rice.OnHand, wantOnHand)
	}
	if rice.Measure != "g, 2000 per package" {
		t.Errorf("rice measure = %q", rice.Measure)
	}

	// Unique: a count, not a sum.
	cable := byName["USB-C Cable"]
	if cable.OnHand != "1 held" {
		t.Errorf("cable on hand = %q, want \"1 held\"", cable.OnHand)
	}
	if cable.Measure != "one of a kind" {
		t.Errorf("cable measure = %q", cable.Measure)
	}
}

// TestHoldingStateReadsDifferentlyPerKind: a Bulk holding has an amount, a
// Unique one has a custody state, and neither has the other's.
func TestHoldingStateReadsDifferentlyPerKind(t *testing.T) {
	h := newHarness(t)
	twoBags, err := domain.FromWhole(2)
	if err != nil {
		t.Fatal(err)
	}
	h.apply(t,
		domain.Acquired{EventBase: evAt(), Holding: h.sealed, Delta: twoBags},
		domain.Acquired{EventBase: evAt(), Holding: h.opened, Delta: domain.FromMilli(800_000)},
		domain.CheckedOut{EventBase: evAt(), Holding: h.cableHolding, DisplacedTo: &h.garage},
	)

	rows, err := h.ctrl.Holdings(h.ctx)
	if err != nil {
		t.Fatalf("holdings: %v", err)
	}
	byState := map[domain.HoldingID]string{}
	for _, r := range rows {
		byState[r.ID] = r.State
	}

	if got := byState[h.sealed]; !strings.Contains(got, "packages") {
		t.Errorf("sealed state = %q, want packages", got)
	}
	if got := byState[h.opened]; got != "800 g" {
		t.Errorf("opened state = %q, want \"800 g\"", got)
	}
	// The NAME, not the identifier the ledger records. "out at 6" is a number a
	// person cannot act on, and it passed a HasPrefix check for months.
	if got := byState[h.cableHolding]; got != "out at Garage" {
		t.Errorf("cable state = %q, want \"out at Garage\"", got)
	}
}

func TestCheckedOutWithNoDestinationReadsAsUnknown(t *testing.T) {
	h := newHarness(t)
	h.apply(t, domain.CheckedOut{EventBase: evAt(), Holding: h.cableHolding})

	rows, err := h.ctrl.Holdings(h.ctx)
	if err != nil {
		t.Fatalf("holdings: %v", err)
	}
	for _, r := range rows {
		if r.ID != h.cableHolding {
			continue
		}
		// Derived-Missing: transient ignorance, distinct from Lost.
		if !strings.Contains(r.State, "unknown") {
			t.Errorf("state = %q, want it to read as unknown", r.State)
		}
	}
}

// TestHistoryIsInSequenceOrderAndReadable is what the TUI exists to show.
func TestHistoryIsInSequenceOrderAndReadable(t *testing.T) {
	h := newHarness(t)
	h.apply(t,
		domain.Acquired{EventBase: evAt(), Holding: h.opened, Delta: domain.FromMilli(2_000_000), Source: "shop"},
		domain.Consumed{EventBase: evAt(), Holding: h.opened, Delta: domain.FromMilli(-100_000), Reason: "dinner"},
		domain.Moved{EventBase: evAt(), Holding: h.opened, From: h.pantry, To: h.garage},
	)

	rows, err := h.ctrl.HoldingHistory(h.ctx, h.opened)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(rows) != 4 { // creation plus three
		t.Fatalf("got %d rows, want 4", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].Sequence <= rows[i-1].Sequence {
			t.Fatalf("history is not in sequence order: %v", rows)
		}
	}

	want := []string{"put at location", "acquired 2000 from shop", "used 100 (dinner)", "moved from"}
	for i, prefix := range want {
		if !strings.Contains(rows[i].Summary, prefix) {
			t.Errorf("row %d summary = %q, want it to contain %q", i, rows[i].Summary, prefix)
		}
	}
}

func TestIntegrityViewReportsAndDoesNotRepair(t *testing.T) {
	h := newHarness(t)
	h.apply(t, domain.Acquired{EventBase: evAt(), Holding: h.opened, Delta: domain.FromMilli(500_000)})

	clean, err := h.ctrl.Integrity(h.ctx)
	if err != nil {
		t.Fatalf("integrity: %v", err)
	}
	if !clean.Clean() {
		t.Fatalf("expected a clean report, got %+v", clean)
	}
	if clean.HoldingsChecked != 3 {
		t.Errorf("checked %d holdings, want 3", clean.HoldingsChecked)
	}
}

func TestNudgeSurfacesOnlyWithASibling(t *testing.T) {
	h := newHarness(t)

	// Basmati Rice sits at Spices, which has a child. USB-C Cable sits at Dried
	// Peppers, which has none.
	nudges, err := h.ctrl.Nudges(h.ctx)
	if err != nil {
		t.Fatalf("nudges: %v", err)
	}
	if len(nudges) != 1 {
		t.Fatalf("got %d nudges, want 1: %+v", len(nudges), nudges)
	}
	if nudges[0].Item != "Basmati Rice" || nudges[0].Category != "Spices" {
		t.Errorf("nudge = %+v", nudges[0])
	}
	if nudges[0].Siblings != 1 {
		t.Errorf("siblings = %d, want 1", nudges[0].Siblings)
	}
}

// TestArchivingACategoryTakesItOutOfTheTree drives the write the way the
// interface does -- plan it, then apply the plan -- because that is now the
// only way in.
//
// It replaces a test that called four commit-immediately Controller methods
// which nothing else called. The invariant it was really checking is this one,
// and it is worth more asserted against the path that exists.
func TestArchivingACategoryTakesItOutOfTheTree(t *testing.T) {
	h := newHarness(t)

	apply := func(cmd command.Command) {
		t.Helper()
		plan, err := h.ctrl.PlanCommand(h.ctx, cmd)
		if err != nil {
			t.Fatalf("plan %T: %v", cmd, err)
		}
		if err := h.ctrl.ApplyPlan(h.ctx, plan); err != nil {
			t.Fatalf("apply %T: %v", cmd, err)
		}
	}

	apply(command.NewCategory{Name: "Baking"})
	id := h.categoryNamed(t, "Baking")

	apply(command.Rename{Target: command.Target{Kind: domain.EntityCategory, ID: int64(id)}, Name: "Baking Supplies"})
	apply(command.ReparentCategory{Category: id, Parent: &h.spices})
	apply(command.ArchiveCategory{Category: id, Resolution: domain.ResolutionLift})

	rows, err := h.ctrl.CategoryTree(h.ctx, false)
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	for _, r := range rows {
		if r.ID == int64(id) {
			t.Error("archived category still appears in the tree")
		}
	}
}

// categoryNamed finds a category the interface just created, by the name it was
// given. The command layer names things; only the tree knows their identifiers.
func (h *harness) categoryNamed(t *testing.T, name string) domain.CategoryID {
	t.Helper()
	rows, err := h.ctrl.CategoryTree(h.ctx, false)
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	for _, r := range rows {
		if r.Name == name {
			return domain.CategoryID(r.ID)
		}
	}
	t.Fatalf("no category called %q", name)
	return 0
}
