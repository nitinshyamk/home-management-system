package ops_test

import (
	"errors"
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
	"home-management-system/internal/origin"
)

// countedItem is Bulk with unit "count" -- six cables recorded as a pile of six.
func (tr *tree) countedItem(t *testing.T, name string) domain.ItemID {
	t.Helper()
	id, err := origin.New(tr.conn).CreateBulkItem(tr.ctx, origin.CreateBulkItemInput{
		Name: name, Category: tr.cat, ContentUnit: "count",
	})
	if err != nil {
		t.Fatalf("create counted item: %v", err)
	}
	return id
}

func (tr *tree) liveHoldings(t *testing.T, item domain.ItemID) []domain.Holding {
	t.Helper()
	details, err := tr.r.HoldingsOfItem(tr.ctx, item)
	if err != nil {
		t.Fatalf("holdings of item %d: %v", item, err)
	}
	var out []domain.Holding
	for _, d := range details {
		if d.Holding.Base().RetiredAt == nil {
			out = append(out, d.Holding)
		}
	}
	return out
}

func (tr *tree) activeItemNames(t *testing.T) []string {
	t.Helper()
	items, err := tr.r.Items(tr.ctx)
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	var names []string
	for _, i := range items {
		names = append(names, i.Base().Name)
	}
	return names
}

// TestPromoteReIdentifies is the operation as redesigned: six cables counted as
// a pile become six individually tracked things under a NEW Item, and the old
// Item is archived.
//
// Identity does not survive, and that was never a requirement. Section 3.9 only
// argued that surviving was easy; section 3.10's rule is about identity never
// being REUSED. Meanwhile the schema already gives every promoted Holding a new
// identity, so requiring the Item to keep its own was an asymmetry with nothing
// behind it.
func TestPromoteReIdentifies(t *testing.T) {
	tr := newTree(t)
	cables := tr.countedItem(t, "USB-C Cable")

	tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
		Item: cables, Location: tr.shelf1, Amount: domain.FromMilli(6 * domain.Scale),
		Basis: domain.BasisContent, Source: "shop",
	}))

	batch, err := tr.pl.Promote(tr.ctx, ops.PromoteRequest{Item: cables})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	// One new Item plus six new Holdings.
	if got := len(batch.Steps[0].Originates); got != 7 {
		t.Fatalf("originations = %d, want 7 (the item and six holdings)", got)
	}
	if !batch.Steps[0].NeedsConfirmation() {
		t.Error("creating a named item did not ask for confirmation")
	}

	res, err := tr.ex.Execute(tr.ctx, batch)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	tr.verifyClean(t)

	newItem := res.Created[0].Items[0]
	if newItem == cables {
		t.Fatal("the item identity did not change; this operation re-identifies")
	}
	if got := tr.itemKind(t, newItem); got != domain.KindUnique {
		t.Errorf("new item kind = %s, want Unique", got)
	}
	if got := len(tr.liveHoldings(t, newItem)); got != 6 {
		t.Errorf("%d live holdings under the new item, want 6", got)
	}
	if got := len(tr.liveHoldings(t, cables)); got != 0 {
		t.Errorf("%d live holdings still under the old item, want 0", got)
	}

	// The old Item keeps its kind forever, which is exactly why the composite
	// foreign key is never stressed.
	if got := tr.itemKind(t, cables); got != domain.KindBulk {
		t.Errorf("old item kind = %s, want Bulk unchanged", got)
	}
}

// TestPromoteArchivesTheOldItem: only one "USB-C Cable" is offered afterwards.
func TestPromoteArchivesTheOldItem(t *testing.T) {
	tr := newTree(t)
	// A name the fixture does not already use, since sibling name uniqueness
	// is deliberately absent (3.10) and the fixture has its own USB-C Cable.
	cables := tr.countedItem(t, "HDMI Cable")
	tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
		Item: cables, Location: tr.shelf1, Amount: domain.FromMilli(2 * domain.Scale),
		Basis: domain.BasisContent,
	}))
	tr.apply(tr.pl.Promote(tr.ctx, ops.PromoteRequest{Item: cables}))

	var seen int
	for _, name := range tr.activeItemNames(t) {
		if name == "HDMI Cable" {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("%d active items named HDMI Cable, want 1", seen)
	}
}

// TestPromoteLeavesTheOldHistoryIntact: nothing is deleted, so the purchase
// that put six cables on the shelf is still readable. It is not DISCOVERABLE
// from the new item -- succession is deliberately unrecorded -- but it is there.
func TestPromoteLeavesTheOldHistoryIntact(t *testing.T) {
	tr := newTree(t)
	cables := tr.countedItem(t, "USB-C Cable")
	tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
		Item: cables, Location: tr.shelf1, Amount: domain.FromMilli(6 * domain.Scale),
		Basis: domain.BasisContent, Source: "corner shop",
	}))
	old := tr.liveHoldings(t, cables)[0].Base().ID

	tr.apply(tr.pl.Promote(tr.ctx, ops.PromoteRequest{Item: cables}))

	history, err := tr.led.History(tr.ctx, domain.SubjectHolding, int64(old))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	want := []string{"HoldingCreated", "Acquired", "Split", "Gone"}
	if got := types(history); !equal(got, want) {
		t.Errorf("history = %v, want %v", got, want)
	}
	// And the old Item's definition survives, because nothing destroyed the
	// bulk_items row. Backward closure holds with no ItemKindChanged at all.
	it, err := tr.r.Item(tr.ctx, cables)
	if err != nil {
		t.Fatalf("read archived item: %v", err)
	}
	if b, ok := it.(domain.BulkItem); !ok || b.ContentUnit != "count" {
		t.Errorf("archived item = %#v, want a Bulk item measured in count", it)
	}
}

// TestPromoteRefusesAMeasuredItem: 800 grams of rice are not 800 things.
func TestPromoteRefusesAMeasuredItem(t *testing.T) {
	tr := newTree(t)
	if _, err := tr.pl.Promote(tr.ctx, ops.PromoteRequest{Item: tr.item}); !errors.Is(err, ops.ErrInvalidRequest) {
		t.Errorf("error = %v, want ErrInvalidRequest", err)
	}
}

// TestPromoteRefusesPackages: two boxes of thumbtacks are not two thumbtacks.
func TestPromoteRefusesPackages(t *testing.T) {
	tr := newTree(t)
	size := domain.FromMilli(100 * domain.Scale)
	tacks, err := origin.New(tr.conn).CreateBulkItem(tr.ctx, origin.CreateBulkItemInput{
		Name: "Thumbtacks", Category: tr.cat, ContentUnit: "count", PackageSize: &size,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
		Item: tacks, Location: tr.shelf1, Amount: domain.FromMilli(2 * domain.Scale),
		Basis: domain.BasisPackage,
	}))
	if _, err := tr.pl.Promote(tr.ctx, ops.PromoteRequest{Item: tacks}); !errors.Is(err, ops.ErrInvalidRequest) {
		t.Errorf("error = %v, want ErrInvalidRequest", err)
	}
}

// TestPromoteOfANeverStockedItemWorks: the same path, with nothing to move.
func TestPromoteOfANeverStockedItemWorks(t *testing.T) {
	tr := newTree(t)
	cables := tr.countedItem(t, "USB-C Cable")

	batch, err := tr.pl.Promote(tr.ctx, ops.PromoteRequest{Item: cables})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if got := len(batch.Steps[0].Originates); got != 1 {
		t.Errorf("originations = %d, want 1 (the item alone)", got)
	}
	res, err := tr.ex.Execute(tr.ctx, batch)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := tr.itemKind(t, res.Created[0].Items[0]); got != domain.KindUnique {
		t.Errorf("kind = %s, want Unique", got)
	}
}

// ---------------------------------------------------------------------------
// Demote
// ---------------------------------------------------------------------------

// TestDemoteGroupsByLocation: a count is a count of things in ONE place, so
// three in the garage and two on the shelf become two Holdings, not one of five.
func TestDemoteGroupsByLocation(t *testing.T) {
	tr := newTree(t)
	for i := 0; i < 3; i++ {
		tr.uniqueOf(t, tr.cableItem, tr.garage)
	}
	for i := 0; i < 2; i++ {
		tr.uniqueOf(t, tr.cableItem, tr.shelf1)
	}

	batch, err := tr.pl.Demote(tr.ctx, ops.DemoteRequest{Item: tr.cableItem, ContentUnit: "count"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	res, err := tr.ex.Execute(tr.ctx, batch)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	tr.verifyClean(t)

	newItem := res.Created[0].Items[0]
	live := tr.liveHoldings(t, newItem)
	if len(live) != 2 {
		t.Fatalf("%d holdings, want 2 (one per place)", len(live))
	}
	byPlace := map[domain.LocationID]int64{}
	for _, h := range live {
		byPlace[h.Base().StowedLocation] = h.(domain.BulkHolding).Quantity.Milli()
	}
	if got := byPlace[tr.garage]; got != 3*domain.Scale {
		t.Errorf("garage = %d milli, want %d", got, 3*domain.Scale)
	}
	if got := byPlace[tr.shelf1]; got != 2*domain.Scale {
		t.Errorf("shelf = %d milli, want %d", got, 2*domain.Scale)
	}
	if got := len(tr.liveHoldings(t, tr.cableItem)); got != 0 {
		t.Errorf("%d unique holdings survived, want 0", got)
	}
}

func TestDemoteNeedsAContentUnit(t *testing.T) {
	tr := newTree(t)
	if _, err := tr.pl.Demote(tr.ctx, ops.DemoteRequest{Item: tr.cableItem}); !errors.Is(err, ops.ErrInvalidRequest) {
		t.Errorf("error = %v, want ErrInvalidRequest", err)
	}
}

func TestDemoteRefusesAnAlreadyCountedItem(t *testing.T) {
	tr := newTree(t)
	if _, err := tr.pl.Demote(tr.ctx, ops.DemoteRequest{
		Item: tr.item, ContentUnit: "count",
	}); !errors.Is(err, ops.ErrInvalidRequest) {
		t.Errorf("error = %v, want ErrInvalidRequest", err)
	}
}

// TestPromoteIsAtomicAcrossAllThreePaths: origin creates the Item, the ledger
// creates the Holdings and records the retirement, annotate archives the old
// Item. A failure anywhere leaves none of it.
func TestPromoteIsAtomicAcrossAllThreePaths(t *testing.T) {
	tr := newTree(t)
	cables := tr.countedItem(t, "USB-C Cable")
	tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
		Item: cables, Location: tr.shelf1, Amount: domain.FromMilli(3 * domain.Scale),
		Basis: domain.BasisContent,
	}))
	before := len(tr.activeItemNames(t))

	batch, err := tr.pl.Promote(tr.ctx, ops.PromoteRequest{Item: cables})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	inner := batch.Steps[0].Records
	batch.Steps[0].Records = func(c ops.Created) ([]domain.Event, error) {
		events, err := inner(c)
		if err != nil {
			return nil, err
		}
		return append(events, domain.Gone{
			EventBase: domain.EventBase{OccurredAt: clock},
			Holding:   domain.HoldingID(999_999), Reason: "injected",
		}), nil
	}
	if _, err := tr.ex.Execute(tr.ctx, batch); err == nil {
		t.Fatal("execute succeeded against a nonexistent holding")
	}

	if got := len(tr.activeItemNames(t)); got != before {
		t.Errorf("%d active items after a failed promote, want %d", got, before)
	}
	if got := len(tr.liveHoldings(t, cables)); got != 1 {
		t.Errorf("%d live holdings under the old item, want 1 -- the retirement escaped", got)
	}
	if got := tr.itemKind(t, cables); got != domain.KindBulk {
		t.Errorf("old item kind = %s, want Bulk", got)
	}
}
