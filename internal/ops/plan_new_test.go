package ops_test

import (
	"errors"
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
)

// The creation operations, through a real database. What matters is that the
// three presets land on the right variant and that the permanent fields are
// checked BEFORE anything permanent happens -- a wrong kind is the one error
// this system cannot undo.

func TestNewItemMapsTheThreePresets(t *testing.T) {
	tr := newTree(t)
	size := domain.FromMilli(2_000_000)

	for _, tc := range []struct {
		name     string
		req      ops.NewItemRequest
		wantKind domain.Kind
	}{
		{"one of a kind", ops.NewItemRequest{
			Name: "Hammer", Category: tr.cat, Counting: ops.CountingUnique,
		}, domain.KindUnique},
		{"a pile", ops.NewItemRequest{
			Name: "Batteries", Category: tr.cat, Counting: ops.CountingPile, ContentUnit: "count",
		}, domain.KindBulk},
		{"measured", ops.NewItemRequest{
			Name: "Turmeric", Category: tr.cat, Counting: ops.CountingMeasured,
			ContentUnit: "g", PackageSize: &size,
		}, domain.KindBulk},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batch, err := tr.pl.NewItem(tr.ctx, tc.req)
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			// Creation is never silent: the review screen learns this from the
			// Step rather than from a UI convention.
			if !batch.Steps[0].NeedsConfirmation() {
				t.Error("creating an item does not ask for confirmation")
			}
			res, err := tr.ex.Execute(tr.ctx, batch)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			item, err := tr.r.Item(tr.ctx, res.Created[0].Items[0])
			if err != nil {
				t.Fatalf("read item: %v", err)
			}
			if got := item.Kind(); got != tc.wantKind {
				t.Errorf("kind = %s, want %s", got, tc.wantKind)
			}
		})
	}
}

// The permanent fields are checked before anything permanent happens, which is
// the only moment the check is worth anything.
func TestNewItemRefusesIncoherentPresets(t *testing.T) {
	tr := newTree(t)
	size := domain.FromMilli(2_000_000)

	for _, tc := range []struct {
		name string
		req  ops.NewItemRequest
	}{
		{"no name", ops.NewItemRequest{Category: tr.cat, Counting: ops.CountingUnique}},
		{"no counting", ops.NewItemRequest{Name: "Mystery", Category: tr.cat}},
		{"unknown counting", ops.NewItemRequest{Name: "Mystery", Category: tr.cat, Counting: "fungible"}},
		{"measured with no unit", ops.NewItemRequest{
			Name: "Turmeric", Category: tr.cat, Counting: ops.CountingMeasured,
		}},
		{"one of a kind with a package size", ops.NewItemRequest{
			Name: "Hammer", Category: tr.cat, Counting: ops.CountingUnique, PackageSize: &size,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tr.pl.NewItem(tr.ctx, tc.req); !errors.Is(err, ops.ErrInvalidRequest) {
				t.Errorf("planned %v, want ErrInvalidRequest", err)
			}
		})
	}
	// And nothing was created along the way.
	items, err := tr.r.Items(tr.ctx)
	if err != nil {
		t.Fatalf("read items: %v", err)
	}
	if len(items) != 3 { // the fixture's rice, flour, and cable
		t.Errorf("%d items exist, want the 3 the fixture made", len(items))
	}
}

// NewHolding upholds O1 at the point of creation: a slot that is taken already
// HAS the Holding being asked for, by H8, so there is nothing to create.
func TestNewHoldingRefusesAnOccupiedSlot(t *testing.T) {
	tr := newTree(t)
	req := ops.NewHoldingRequest{Item: tr.item, Location: tr.pantry, Basis: domain.BasisContent}

	tr.apply(tr.pl.NewHolding(tr.ctx, req))
	if _, err := tr.pl.NewHolding(tr.ctx, req); !errors.Is(err, ops.ErrInvalidRequest) {
		t.Fatalf("created a duplicate slot: %v", err)
	}

	// A different basis is a different slot, so that one is fine.
	req.Basis = domain.BasisPackage
	tr.apply(tr.pl.NewHolding(tr.ctx, req))

	dupes, err := tr.led.FindDuplicateSlots(tr.ctx)
	if err != nil {
		t.Fatalf("find duplicate slots: %v", err)
	}
	if len(dupes) != 0 {
		t.Errorf("H8 violations: %v", dupes)
	}
}

// Two identical Unique Holdings in one place are two things, not one, so the
// slot rule must not reach them.
func TestNewHoldingAllowsTwoUniqueThingsInOnePlace(t *testing.T) {
	tr := newTree(t)
	req := ops.NewHoldingRequest{Item: tr.cableItem, Location: tr.shelf1}
	tr.apply(tr.pl.NewHolding(tr.ctx, req))
	tr.apply(tr.pl.NewHolding(tr.ctx, req))

	details, err := tr.r.HoldingsOfItem(tr.ctx, tr.cableItem)
	if err != nil {
		t.Fatalf("holdings of item: %v", err)
	}
	if len(details) != 2 {
		t.Errorf("%d holdings, want 2", len(details))
	}
}

// H7 at the input boundary: counting in packages requires the item to have them.
func TestNewHoldingRefusesPackagesWithoutAPackageSize(t *testing.T) {
	tr := newTree(t)
	unpackaged, err := tr.pl.NewItem(tr.ctx, ops.NewItemRequest{
		Name: "Loose Lentils", Category: tr.cat, Counting: ops.CountingMeasured, ContentUnit: "g",
	})
	if err != nil {
		t.Fatalf("plan item: %v", err)
	}
	res, err := tr.ex.Execute(tr.ctx, unpackaged)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	_, err = tr.pl.NewHolding(tr.ctx, ops.NewHoldingRequest{
		Item: res.Created[0].Items[0], Location: tr.pantry, Basis: domain.BasisPackage,
	})
	if !errors.Is(err, ops.ErrInvalidRequest) {
		t.Errorf("planned a Package holding of an item with no packages: %v", err)
	}
}
