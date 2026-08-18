package ops_test

import (
	"errors"
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/ops"
	"home-management-system/internal/origin"
)

// ---------------------------------------------------------------------------
// NewStockedItem: the cross-path composite
// ---------------------------------------------------------------------------

func TestNewStockedItem(t *testing.T) {
	tr := newTree(t)
	size := domain.FromMilli(2000 * domain.Scale)

	batch, err := tr.pl.NewStockedItem(tr.ctx, ops.NewStockedItemRequest{
		Name: "Turmeric", Category: tr.cat, ContentUnit: "g", PackageSize: &size,
		Location: tr.pantry, Amount: domain.FromMilli(2 * domain.Scale),
		Basis: domain.BasisPackage, Source: "corner shop",
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// The Item is named, so it must be confirmed. The Holding is a placement
	// and rides along.
	if !batch.Steps[0].NeedsConfirmation() {
		t.Error("creating a named item did not ask for confirmation")
	}
	if got := len(batch.Steps[0].Originates); got != 2 {
		t.Fatalf("originations = %d, want 2 (the item and its holding)", got)
	}

	res, err := tr.ex.Execute(tr.ctx, batch)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := len(res.Created[0].Items); got != 1 {
		t.Errorf("created %d items, want 1", got)
	}
	newItem := res.Created[0].Items[0]

	// The Holding must belong to the Item this same Step created.
	details, err := tr.r.HoldingsOfItem(tr.ctx, newItem)
	if err != nil {
		t.Fatalf("holdings: %v", err)
	}
	if len(details) != 1 {
		t.Fatalf("%d holdings, want 1", len(details))
	}
	if got := details[0].Holding.(domain.BulkHolding).Quantity.Milli(); got != 2*domain.Scale {
		t.Errorf("quantity = %d milli, want %d", got, 2*domain.Scale)
	}
	tr.verifyClean(t)
}

// TestNewStockedItemIsAtomicAcrossPaths: the Item originates through one write
// path and the stock records through another, so a failure in the second must
// undo the first.
func TestNewStockedItemIsAtomicAcrossPaths(t *testing.T) {
	tr := newTree(t)
	before, err := tr.r.Items(tr.ctx)
	if err != nil {
		t.Fatalf("items: %v", err)
	}

	batch, err := tr.pl.NewStockedItem(tr.ctx, ops.NewStockedItemRequest{
		Name: "Turmeric", Category: tr.cat, ContentUnit: "g",
		Location: tr.pantry, Amount: domain.FromMilli(500 * domain.Scale),
		Basis: domain.BasisContent,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	batch.Steps[0].Records = func(ops.Created) ([]domain.Event, error) {
		return nil, errors.New("injected")
	}
	if _, err := tr.ex.Execute(tr.ctx, batch); err == nil {
		t.Fatal("execute succeeded despite the injected failure")
	}

	after, err := tr.r.Items(tr.ctx)
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("%d items after a failed composite, want %d -- the origination escaped",
			len(after), len(before))
	}
}

func TestNewStockedItemRejectsPackagesWithNoPackageSize(t *testing.T) {
	tr := newTree(t)
	_, err := tr.pl.NewStockedItem(tr.ctx, ops.NewStockedItemRequest{
		Name: "Turmeric", Category: tr.cat, ContentUnit: "g",
		Location: tr.pantry, Amount: domain.FromMilli(2 * domain.Scale),
		Basis: domain.BasisPackage,
	})
	if !errors.Is(err, ops.ErrInvalidRequest) {
		t.Errorf("error = %v, want ErrInvalidRequest", err)
	}
}

// ---------------------------------------------------------------------------
// Promote and Demote
// ---------------------------------------------------------------------------

func (tr *tree) itemKind(t *testing.T, id domain.ItemID) domain.Kind {
	t.Helper()
	it, err := tr.r.Item(tr.ctx, id)
	if err != nil {
		t.Fatalf("read item: %v", err)
	}
	return it.Kind()
}

// TestPromoteDropsTheDiscardedDefinition: three events, not one. Without the
// unit and package-size events the discarded definition is unrecoverable and
// the Item's history no longer closes backwards.
func TestPromoteRecordsTheDiscardedDefinition(t *testing.T) {
	tr := newTree(t)
	size := domain.FromMilli(2000 * domain.Scale)
	spare, err := origin.New(tr.conn).CreateBulkItem(tr.ctx, origin.CreateBulkItemInput{
		Name: "Spare", Category: tr.cat, ContentUnit: "g", PackageSize: &size,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	batch, err := tr.pl.Promote(tr.ctx, ops.PromoteRequest{Item: spare})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	want := []string{"ItemUnitChanged", "ItemPackageSizeChanged", "ItemKindChanged"}
	if got := shape(t, batch); !equal(got, want) {
		t.Fatalf("plan = %v, want %v", got, want)
	}
	// The kind change is last: its handler deletes the bulk_items row, and the
	// other two write to that row.
	if got := shape(t, batch); got[len(got)-1] != "ItemKindChanged" {
		t.Error("the kind change must come last, or the other events have no row to write to")
	}

	if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := tr.itemKind(t, spare); got != domain.KindUnique {
		t.Errorf("kind = %s, want Unique", got)
	}
}

// TestDemoteSuppliesTheUnitTheEventCannotCarry is what Stage 5 deferred to this
// layer. ItemKindChanged has nowhere to put a content unit, and duplicating it
// there would be derivable data inside the ledger (E7). It is derived instead
// from the ItemUnitChanged travelling in the same batch.
func TestDemoteSuppliesTheUnitTheEventCannotCarry(t *testing.T) {
	tr := newTree(t)
	spare, err := origin.New(tr.conn).CreateUniqueItem(tr.ctx, origin.CreateUniqueItemInput{
		Name: "Spare Cable", Category: tr.cat,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	size := domain.FromMilli(2000 * domain.Scale)

	batch, err := tr.pl.Demote(tr.ctx, ops.DemoteRequest{
		Item: spare, ContentUnit: "g", PackageSize: &size,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	// The kind change is FIRST here: its handler creates the bulk_items row,
	// and the other two need it to exist. The mirror of promotion.
	want := []string{"ItemKindChanged", "ItemUnitChanged", "ItemPackageSizeChanged"}
	if got := shape(t, batch); !equal(got, want) {
		t.Fatalf("plan = %v, want %v", got, want)
	}
	if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
		t.Fatalf("execute: %v", err)
	}

	it, err := tr.r.Item(tr.ctx, spare)
	if err != nil {
		t.Fatalf("read item: %v", err)
	}
	bulk, ok := it.(domain.BulkItem)
	if !ok {
		t.Fatalf("item is %s, want Bulk", it.Kind())
	}
	if bulk.ContentUnit != "g" {
		t.Errorf("content unit = %q, want g", bulk.ContentUnit)
	}
	if bulk.PackageSize == nil || bulk.PackageSize.Milli() != size.Milli() {
		t.Errorf("package size = %v, want %s", bulk.PackageSize, size)
	}
}

// TestDemotionWithoutItsUnitCannotBeApplied: the coupling is enforced by the
// ledger, not merely documented. A bare ItemKindChanged has no unit to read.
func TestDemotionWithoutItsUnitCannotBeApplied(t *testing.T) {
	tr := newTree(t)
	spare, err := origin.New(tr.conn).CreateUniqueItem(tr.ctx, origin.CreateUniqueItemInput{
		Name: "Spare Cable", Category: tr.cat,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	_, err = tr.led.Apply(tr.ctx, domain.ItemKindChanged{
		EventBase: domain.EventBase{OccurredAt: clock},
		Item:      spare, FromKind: domain.KindUnique, ToKind: domain.KindBulk,
	})
	if err == nil {
		t.Fatal("a lone ItemKindChanged demoted an item with no content unit")
	}
	if !errors.Is(err, ledger.ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput", err)
	}
}

func TestPromoteThenDemoteRoundTrips(t *testing.T) {
	tr := newTree(t)
	size := domain.FromMilli(2000 * domain.Scale)
	spare, err := origin.New(tr.conn).CreateBulkItem(tr.ctx, origin.CreateBulkItemInput{
		Name: "Spare", Category: tr.cat, ContentUnit: "g", PackageSize: &size,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	tr.apply(tr.pl.Promote(tr.ctx, ops.PromoteRequest{Item: spare}))
	if got := tr.itemKind(t, spare); got != domain.KindUnique {
		t.Fatalf("kind = %s, want Unique", got)
	}
	tr.apply(tr.pl.Demote(tr.ctx, ops.DemoteRequest{
		Item: spare, ContentUnit: "g", PackageSize: &size,
	}))
	if got := tr.itemKind(t, spare); got != domain.KindBulk {
		t.Errorf("kind = %s, want Bulk", got)
	}
	// Identity survives, which is what makes this a change of definition
	// rather than a replacement.
	it, _ := tr.r.Item(tr.ctx, spare)
	if it.Base().ID != spare {
		t.Errorf("identity changed from %d to %d", spare, it.Base().ID)
	}
}

// TestKindChangeIsRefusedOnceStocked records a real limit of the physical
// schema, found by building this.
//
// Conceptual schema 3.12 says Promote "retires the Bulk Holding and creates N
// Unique ones". That is not reachable: holdings carries FOREIGN KEY (item_id,
// kind) REFERENCES items(id, kind), a holding row keeps the kind it was created
// with, and retiring only sets retired_at. So the parent update is refused
// while ANY holding row of the old kind exists -- retired ones included --  and
// deleting those rows would break E4, which is what makes their events mean
// anything.
func TestKindChangeIsRefusedOnceStocked(t *testing.T) {
	tr := newTree(t)
	holding := tr.stow(t, tr.pantry)

	_, err := tr.pl.Promote(tr.ctx, ops.PromoteRequest{Item: tr.item})
	if !errors.Is(err, ledger.ErrItemHasHoldings) {
		t.Fatalf("error = %v, want ErrItemHasHoldings", err)
	}

	// And retiring the holding does not help, which is the part worth pinning:
	// the row survives, and so does its kind.
	if _, err := tr.led.Apply(tr.ctx, domain.Gone{
		EventBase: domain.EventBase{OccurredAt: clock}, Holding: holding, Reason: "promoting",
	}); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if _, err := tr.pl.Promote(tr.ctx, ops.PromoteRequest{Item: tr.item}); !errors.Is(err, ledger.ErrItemHasHoldings) {
		t.Errorf("error = %v, want ErrItemHasHoldings even after retiring", err)
	}
}
