package ops

import (
	"context"
	"fmt"
	"time"

	"home-management-system/internal/domain"
)

// NewStockedItemRequest brings an Item into existence with stock already in it.
//
// "I bought rice for the first time" is one intent, and it is the intent that
// motivated the whole operations layer: it originates an Item, records a
// Holding into existence, and records what arrived -- three writes across two
// write paths, which before this layer could half-fail.
type NewStockedItemRequest struct {
	Name        string
	Category    domain.CategoryID
	Notes       string
	ContentUnit domain.UnitCode
	PackageSize *domain.Quantity

	Location  domain.LocationID
	Amount    domain.Quantity
	Basis     domain.UnitBasis
	ExpiresOn *time.Time
	Source    string
	Price     *int64
}

// NewStockedItem plans the composite.
//
// Everything happens inside ONE Step, which is what lets the Holding refer to
// the Item this same operation creates without a general dependency graph:
// NewBulkHolding with a nil Item means "the first Item this Step created", and
// the Acquired is built from the identifiers the originations turned out to
// get.
//
// It needs no snapshot. Nothing exists yet to read.
func (p *Planner) NewStockedItem(ctx context.Context, req NewStockedItemRequest) (Batch, error) {
	if req.Name == "" {
		return Batch{}, fmt.Errorf("%w: item name is required", ErrInvalidRequest)
	}
	if req.ContentUnit == "" {
		return Batch{}, fmt.Errorf("%w: a measured item needs a content unit", ErrInvalidRequest)
	}
	if !req.Amount.IsPositive() {
		return Batch{}, fmt.Errorf("%w: received amount must be positive, got %s",
			ErrInvalidRequest, req.Amount)
	}
	if !req.Basis.Valid() {
		return Batch{}, fmt.Errorf("%w: unknown unit basis %q", ErrInvalidRequest, req.Basis)
	}
	// H7 at the input boundary: counting in packages requires the item to have
	// them, and this is the one moment where getting it wrong is permanent.
	if req.Basis == domain.BasisPackage && req.PackageSize == nil {
		return Batch{}, fmt.Errorf("%w: %q has no package size, so it cannot be counted in packages",
			ErrInvalidRequest, req.Name)
	}

	now := p.now()
	var plan Plan
	plan.originates = append(plan.originates, NewBulkItem{
		Name: req.Name, Category: req.Category, ContentUnit: req.ContentUnit,
		PackageSize: req.PackageSize, Notes: req.Notes,
	})
	holding := plan.originateHolding(NewBulkHolding{
		Item: nil, Location: req.Location, UnitBasis: req.Basis, ExpiresOn: req.ExpiresOn,
	})
	plan.record(holding, func(id domain.HoldingID) domain.Event {
		return domain.Acquired{
			EventBase: domain.EventBase{OccurredAt: now},
			Holding:   id, Delta: req.Amount, Source: req.Source, Price: req.Price,
		}
	})

	return plan.Batch(fmt.Sprintf("new item %q with %s at location %d",
		req.Name, req.Amount, req.Location)), nil
}

// ---------------------------------------------------------------------------
// Promote and Demote
// ---------------------------------------------------------------------------

// PromoteRequest turns a measured Item into an individually-tracked one.
type PromoteRequest struct{ Item domain.ItemID }

// DemoteRequest turns an individually-tracked Item into a measured one. The
// content unit is required: a Unique Item has no measurement, so there is
// nothing to recover it from and ItemKindChanged cannot carry it (E7).
type DemoteRequest struct {
	Item        domain.ItemID
	ContentUnit domain.UnitCode
	PackageSize *domain.Quantity
}

func (p *Planner) Promote(ctx context.Context, req PromoteRequest) (Batch, error) {
	events, err := p.led.PlanPromote(ctx, req.Item)
	if err != nil {
		return Batch{}, err
	}
	return oneStep(fmt.Sprintf("track item %d individually", req.Item), events), nil
}

func (p *Planner) Demote(ctx context.Context, req DemoteRequest) (Batch, error) {
	events, err := p.led.PlanDemote(ctx, req.Item, req.ContentUnit, req.PackageSize)
	if err != nil {
		return Batch{}, err
	}
	return oneStep(fmt.Sprintf("measure item %d in %s", req.Item, req.ContentUnit), events), nil
}

// oneStep wraps a fixed event list as a Batch.
func oneStep(summary string, events []domain.Event) Batch {
	return Batch{Steps: []Step{{Summary: summary, Records: fixed(events)}}}
}
