package ops_test

import (
	"errors"
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
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

func (tr *tree) itemKind(t *testing.T, id domain.ItemID) domain.Kind {
	t.Helper()
	it, err := tr.r.Item(tr.ctx, id)
	if err != nil {
		t.Fatalf("read item: %v", err)
	}
	return it.Kind()
}
