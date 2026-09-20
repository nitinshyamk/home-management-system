package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"home-management-system/internal/domain"
)

// ErrRootNotEmpty reports a Lift that has nowhere to put what it lifted.
//
// holdings.stowed_location_id is NOT NULL, so lifting a root's contents would
// have to set them to NULL. That is not a limitation to work around: a Holding
// with no location is a thing whose physical whereabouts the system claims not
// to know, which is exactly what this schema refuses to represent.
var ErrRootNotEmpty = errors.New("ledger: cannot lift the contents of a root location")

// ErrNotEmpty reports a Block resolution against a node that still holds
// something. Named to match annotate.ErrNotEmpty, because the two archivals
// differ in write path rather than in what they refuse.
var ErrNotEmpty = errors.New("ledger: location is not empty")

// ArchiveLocation removes a place, disposing of its children and contents.
//
// This is the Location counterpart to annotate.ArchiveCategory, and the two
// differ in write path rather than in behaviour. Archiving a Category lifts its
// items by ANNOTATION -- reclassification is a relabelling. Archiving a
// Location lifts its holdings by RECORDING -- the objects physically moved, and
// a reconstruction that could not account for where they went would be
// incomplete.
//
// Stage 5 built the NodeArchived handler and nothing emitted it. This is what
// emits it.
func (p *Processor) ArchiveLocation(
	ctx context.Context,
	id domain.LocationID,
	resolution domain.Resolution,
	moveTo *domain.LocationID,
) error {
	events, err := p.PlanArchiveLocation(ctx, id, resolution, moveTo)
	if err != nil {
		return err
	}
	_, err = p.ApplyBatch(ctx, events)
	return err
}

// PlanArchiveLocation works out the events an archival implies without applying
// any of them, so an operation can fold them into a larger unit of work and a
// review screen can say what will happen before it does.
//
// The order is load-bearing: contents leave before the node is archived, so no
// intermediate state has a live Holding inside an archived Location.
func (p *Processor) PlanArchiveLocation(
	ctx context.Context,
	id domain.LocationID,
	resolution domain.Resolution,
	moveTo *domain.LocationID,
) ([]domain.Event, error) {
	if !resolution.Valid() {
		return nil, fmt.Errorf("%w: unknown resolution %q", ErrInvalidInput, resolution)
	}

	state, err := p.q.GetLocationState(ctx, int64(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: location %d", ErrNotFound, id)
		}
		return nil, fmt.Errorf("ledger: read location %d: %w", id, err)
	}
	if state.ArchivedAt.Valid {
		return nil, fmt.Errorf("%w: location %d is already archived", ErrInvalidInput, id)
	}

	destination, err := p.archiveDestination(ctx, id, state.ParentID, resolution, moveTo)
	if err != nil {
		return nil, err
	}

	children, err := p.q.LiveChildLocationIDs(ctx, sql.NullInt64{Int64: int64(id), Valid: true})
	if err != nil {
		return nil, fmt.Errorf("ledger: read children of %d: %w", id, err)
	}
	holdings, err := p.q.HoldingsStowedAt(ctx, int64(id))
	if err != nil {
		return nil, fmt.Errorf("ledger: read holdings at %d: %w", id, err)
	}

	if resolution == domain.ResolutionBlock {
		if len(children) > 0 || len(holdings) > 0 {
			return nil, fmt.Errorf("%w: location %d has %d children and %d holdings",
				ErrNotEmpty, id, len(children), len(holdings))
		}
		return []domain.Event{p.nodeArchived(id, resolution)}, nil
	}

	events := make([]domain.Event, 0, len(children)+len(holdings)+1)

	// Children move first, taking their own contents with them. Their holdings
	// need no event: NodeReparented is precisely the record of a container
	// moving its contents without each one having an event of its own.
	var toParent *domain.LocationID
	if destination.Valid {
		d := domain.LocationID(destination.Int64)
		toParent = &d
	}
	from := id
	for _, child := range children {
		events = append(events, domain.NodeReparented{
			EventBase:  domain.EventBase{OccurredAt: p.now()},
			Location:   domain.LocationID(child),
			FromParent: &from,
			ToParent:   toParent,
		})
	}

	// Holdings kept here directly did physically move, so each gets a Moved --
	// unless the destination already has the same slot, which O1 says is the
	// same Holding and must be merged into rather than duplicated.
	//
	// A root being lifted has no parent to lift into. Children simply become
	// roots themselves, but Holdings cannot: stowed_location_id is NOT NULL, so
	// there is nowhere to put them. The check sits here, adjacent to the
	// dereference it protects, rather than earlier where the two could drift.
	if len(holdings) > 0 {
		if toParent == nil {
			return nil, fmt.Errorf("%w: location %d is a root holding %d things; "+
				"lifting has nowhere to put them, so use Move", ErrRootNotEmpty, id, len(holdings))
		}
		disposal, err := p.disposeContents(ctx, id, *toParent, holdings)
		if err != nil {
			return nil, err
		}
		events = append(events, disposal...)
	}

	return append(events, p.nodeArchived(id, resolution)), nil
}

// slotKey identifies a Bulk Holding the way H8 does: item, basis, and expiry,
// with an unknown expiry as one value rather than many. Location is absent
// because both sides of a lift are being compared AT the destination.
type slotKey struct {
	item      int64
	basis     string
	expiresOn string
}

// disposeContents works out what happens to each Holding kept in a Location
// that is being archived.
//
// The interesting case is a collision. Lifting a shelf of rice into a cupboard
// that already holds rice on the same basis with the same date produces, by
// H8's definition, one Holding and not two -- so the contents merge and the
// emptied Holding goes, rather than both arriving and quietly violating the
// invariant. That was not hypothetical: the H8 check in VerifyAll found it the
// day it was written.
func (p *Processor) disposeContents(
	ctx context.Context,
	from, to domain.LocationID,
	holdings []int64,
) ([]domain.Event, error) {
	occupied := map[slotKey]int64{}
	rows, err := p.q.BulkHoldingSlotsAt(ctx, int64(to))
	if err != nil {
		return nil, fmt.Errorf("ledger: read holdings at %d: %w", to, err)
	}
	for _, r := range rows {
		occupied[slotKey{r.ItemID, r.UnitBasis, r.ExpiresOn}] = r.ID
	}

	// Only Bulk Holdings have a slot. A Unique one is a specific object and two
	// of them in one place are two things, so a Unique Holding always moves.
	leaving := map[int64]BulkHoldingSlot{}
	rows, err = p.q.BulkHoldingSlotsAt(ctx, int64(from))
	if err != nil {
		return nil, fmt.Errorf("ledger: read holdings at %d: %w", from, err)
	}
	for _, r := range rows {
		leaving[r.ID] = BulkHoldingSlot{Key: slotKey{r.ItemID, r.UnitBasis, r.ExpiresOn}, Quantity: r.Quantity}
	}

	at := func() domain.EventBase { return domain.EventBase{OccurredAt: p.now()} }
	var events []domain.Event
	for _, h := range holdings {
		bulk, isBulk := leaving[h]
		target, collides := occupied[bulk.Key]
		if !isBulk || !collides {
			events = append(events, domain.Moved{
				EventBase: at(), Holding: domain.HoldingID(h), From: from, To: to,
			})
			continue
		}
		// The contents move by value, then the container ceases to exist --
		// it cannot stay, because the Location it is in is being archived.
		if bulk.Quantity > 0 {
			amount := domain.FromMilli(bulk.Quantity)
			debit, err := amount.Neg()
			if err != nil {
				return nil, err
			}
			events = append(events,
				domain.Merged{EventBase: at(), Holding: domain.HoldingID(h), Delta: debit, Reason: "archived"},
				domain.Merged{EventBase: at(), Holding: domain.HoldingID(target), Delta: amount, Reason: "archived"},
			)
		}
		events = append(events, domain.Gone{
			EventBase: at(), Holding: domain.HoldingID(h),
			Reason: fmt.Sprintf("merged into holding %d when location %d was archived", target, from),
		})
	}
	return events, nil
}

// BulkHoldingSlot pairs a Holding's H8 key with how much is in it.
type BulkHoldingSlot struct {
	Key      slotKey
	Quantity int64
}

// archiveDestination resolves where children and contents go, and refuses the
// destinations that would strand them.
func (p *Processor) archiveDestination(
	ctx context.Context,
	id domain.LocationID,
	parent sql.NullInt64,
	resolution domain.Resolution,
	moveTo *domain.LocationID,
) (sql.NullInt64, error) {
	switch resolution {
	case domain.ResolutionLift:
		// NULL for a root, which makes its children roots in turn.
		return parent, nil

	case domain.ResolutionMove:
		if moveTo == nil {
			return sql.NullInt64{}, fmt.Errorf("%w: Move resolution needs a destination", ErrInvalidInput)
		}
		if *moveTo == id {
			return sql.NullInt64{}, fmt.Errorf("%w: cannot move contents into the node being archived", ErrInvalidInput)
		}
		exists, err := p.q.LocationExists(ctx, int64(*moveTo))
		if err != nil {
			return sql.NullInt64{}, fmt.Errorf("ledger: read location %d: %w", *moveTo, err)
		}
		if !exists {
			return sql.NullInt64{}, fmt.Errorf("%w: location %d", ErrNotFound, *moveTo)
		}
		// Moving contents into a descendant would strand them under an
		// archived ancestor, so it is refused for the same reason a cycle is.
		beneath, err := p.locationHasAncestor(ctx, *moveTo, id)
		if err != nil {
			return sql.NullInt64{}, err
		}
		if beneath {
			return sql.NullInt64{}, fmt.Errorf("%w: destination %d is beneath %d", ErrCycle, *moveTo, id)
		}
		return sql.NullInt64{Int64: int64(*moveTo), Valid: true}, nil
	}
	return sql.NullInt64{}, nil
}

func (p *Processor) nodeArchived(id domain.LocationID, r domain.Resolution) domain.Event {
	return domain.NodeArchived{
		EventBase:  domain.EventBase{OccurredAt: p.now()},
		Location:   id,
		Resolution: r,
	}
}

// RestoreLocation reverses an archival.
//
// Contents are not restored with it, for the same reason RestoreCategory does
// not restore its items: they were moved, the move was recorded, and where they
// went is now where they are. Undoing the archival does not un-happen the move.
func (p *Processor) RestoreLocation(ctx context.Context, id domain.LocationID) error {
	events, err := p.PlanRestoreLocation(ctx, id)
	if err != nil {
		return err
	}
	_, err = p.ApplyBatch(ctx, events)
	return err
}

// PlanRestoreLocation works out the events a restoration implies.
func (p *Processor) PlanRestoreLocation(ctx context.Context, id domain.LocationID) ([]domain.Event, error) {
	state, err := p.q.GetLocationState(ctx, int64(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: location %d", ErrNotFound, id)
		}
		return nil, fmt.Errorf("ledger: read location %d: %w", id, err)
	}
	if !state.ArchivedAt.Valid {
		return nil, fmt.Errorf("%w: location %d is not archived", ErrInvalidInput, id)
	}
	return []domain.Event{domain.NodeRestored{
		EventBase: domain.EventBase{OccurredAt: p.now()},
		Location:  id,
	}}, nil
}
