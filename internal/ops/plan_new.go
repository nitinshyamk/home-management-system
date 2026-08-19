package ops

import (
	"context"
	"fmt"
	"time"

	"home-management-system/internal/domain"
)

// The creation operations: one Batch each, all origination, all confirmed.
//
// Creation is never silent and never one keystroke, because an origination
// error cannot be fixed. An annotation error is a typo; a recording error is
// caught by H10 and reversed by a compensating event; a wrong kind or content
// unit is remedied only by making a NEW Item and archiving the old one, which
// is what Promote does and why it replaces every Holding.
//
// The friction is mechanical rather than a UI convention: every Origination
// here answers NeedsConfirmation with true, so a review screen cannot fail to
// notice.

// Counting is how a person chooses an Item's kind: three presets, never two
// booleans. Nobody reads the word "fungible" (domain model 2.2).
type Counting string

const (
	// CountingUnique is one of a kind -- each one tracked individually, with
	// its own custody and its own label.
	CountingUnique Counting = "unique"
	// CountingPile is a pile you count -- whole things, but interchangeable.
	CountingPile Counting = "pile"
	// CountingMeasured is something you measure -- grams, millilitres.
	CountingMeasured Counting = "measured"
)

func (c Counting) Valid() bool {
	return c == CountingUnique || c == CountingPile || c == CountingMeasured
}

// NewItemRequest brings a kind of thing into existence, with no stock.
//
// Counting decides the variant, and it is the one field here that can never be
// revised. NewStockedItem is the same act with an arrival attached, and is what
// a receipt usually produces.
type NewItemRequest struct {
	Name        string
	Category    domain.CategoryID
	Counting    Counting
	ContentUnit domain.UnitCode
	PackageSize *domain.Quantity
	Notes       string
}

func (p *Planner) NewItem(ctx context.Context, req NewItemRequest) (Batch, error) {
	if req.Name == "" {
		return Batch{}, fmt.Errorf("%w: item name is required", ErrInvalidRequest)
	}
	if !req.Counting.Valid() {
		return Batch{}, fmt.Errorf("%w: counting must be unique, pile, or measured, got %q",
			ErrInvalidRequest, req.Counting)
	}

	var origination Origination
	if req.Counting == CountingUnique {
		if req.ContentUnit != "" || req.PackageSize != nil {
			return Batch{}, fmt.Errorf(
				"%w: a one-of-a-kind item is not measured, so it has no unit or package size",
				ErrInvalidRequest)
		}
		origination = NewUniqueItem{Name: req.Name, Category: req.Category, Notes: req.Notes}
	} else {
		// A pile is measured too -- in whole things. The difference between a
		// pile and a measured item is which unit it counts in, not whether it
		// has one, which is why both land on the same variant.
		if req.ContentUnit == "" {
			return Batch{}, fmt.Errorf("%w: a %s item needs a unit to count in",
				ErrInvalidRequest, req.Counting)
		}
		origination = NewBulkItem{
			Name: req.Name, Category: req.Category, ContentUnit: req.ContentUnit,
			PackageSize: req.PackageSize, Notes: req.Notes,
		}
	}
	return originationBatch(origination), nil
}

// NewCategoryRequest adds a classification node.
type NewCategoryRequest struct {
	Name        string
	Parent      *domain.CategoryID
	Description string
}

func (p *Planner) NewCategory(ctx context.Context, req NewCategoryRequest) (Batch, error) {
	if req.Name == "" {
		return Batch{}, fmt.Errorf("%w: category name is required", ErrInvalidRequest)
	}
	return originationBatch(NewCategory{
		Name: req.Name, Parent: req.Parent, Description: req.Description,
	}), nil
}

// NewLocationRequest adds a place.
type NewLocationRequest struct {
	Name        string
	Parent      *domain.LocationID
	Description string
}

func (p *Planner) NewLocation(ctx context.Context, req NewLocationRequest) (Batch, error) {
	if req.Name == "" {
		return Batch{}, fmt.Errorf("%w: location name is required", ErrInvalidRequest)
	}
	return originationBatch(NewLocation{
		Name: req.Name, Parent: req.Parent, Description: req.Description,
	}), nil
}

// NewHoldingRequest puts an existing Item somewhere, at nothing.
//
// Rarely typed: acquiring stock creates the Holding it needs. It exists for the
// case a person genuinely means -- "there is an empty jar for this on that
// shelf" -- and for the operations that need to name one explicitly.
type NewHoldingRequest struct {
	Item      domain.ItemID
	Location  domain.LocationID
	Basis     domain.UnitBasis
	ExpiresOn *time.Time
	Label     string
}

func (p *Planner) NewHolding(ctx context.Context, req NewHoldingRequest) (Batch, error) {
	item, err := p.r.Item(ctx, req.Item)
	if err != nil {
		return Batch{}, err
	}

	if _, unique := item.(domain.UniqueItem); unique {
		if req.Basis != "" {
			return Batch{}, fmt.Errorf(
				"%w: item %d is one of a kind, so there is no unit basis to choose",
				ErrInvalidRequest, req.Item)
		}
		return originationBatch(NewUniqueHolding{
			Item: &req.Item, Location: req.Location, Label: req.Label, ExpiresOn: req.ExpiresOn,
		}), nil
	}

	bulk := item.(domain.BulkItem)
	if !req.Basis.Valid() {
		return Batch{}, fmt.Errorf("%w: unknown unit basis %q", ErrInvalidRequest, req.Basis)
	}
	// H7 at the input boundary: counting in packages requires the item to have
	// them.
	if req.Basis == domain.BasisPackage && bulk.PackageSize == nil {
		return Batch{}, fmt.Errorf("%w: %q has no package size, so it cannot be counted in packages",
			ErrInvalidRequest, bulk.Name)
	}
	if req.Label != "" {
		return Batch{}, fmt.Errorf("%w: a measured amount has nothing to label", ErrInvalidRequest)
	}

	// O1: if the slot is already taken, the two would BE one Holding by H8, so
	// there is nothing to create.
	s, err := p.SnapshotItem(ctx, req.Item)
	if err != nil {
		return Batch{}, err
	}
	if existing, taken := s.findSlot(slot{
		req.Item, req.Location, req.Basis, expiryKey(req.ExpiresOn),
	}); taken {
		return Batch{}, fmt.Errorf("%w: holding %d already keeps %q there on that basis",
			ErrInvalidRequest, existing.ID, bulk.Name)
	}

	return originationBatch(NewBulkHolding{
		Item: &req.Item, Location: req.Location, UnitBasis: req.Basis, ExpiresOn: req.ExpiresOn,
	}), nil
}

// originationBatch wraps one origination as a Batch.
func originationBatch(o Origination) Batch {
	return Batch{Steps: []Step{{
		Summary:    "create " + o.Describe(),
		Originates: []Origination{o},
	}}}
}
