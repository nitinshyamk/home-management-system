package ops

import (
	"context"
	"fmt"

	"home-management-system/internal/domain"
)

// Promote and Demote, as RE-IDENTIFICATION.
//
// An earlier design changed an Item's kind in place. It cannot work once the
// Item has been stocked: holdings carries FOREIGN KEY (item_id, kind)
// REFERENCES items(id, kind), a holding row keeps the kind it was created with,
// and retiring only sets retired_at. So the parent update is refused while any
// holding row of the old kind exists, and removing those rows would orphan
// their events -- E4, which is what makes an event interpretable at all.
//
// The deeper point is that identity was never required to survive. Section 3.9
// argued only that surviving was EASY, rebutting a claim that it was hard;
// section 3.10's rule is about identity never being REUSED, which is a
// different claim. And the schema already abandons identity here: promotion
// "retires the Bulk Holding and creates N Unique ones, each with its own
// identity". Holding identity changes by design, so requiring Item identity to
// survive was an asymmetry with nothing behind it.
//
// Re-identifying is also structurally simpler. The old Item stays Bulk forever
// and its retired holdings keep referencing (old_id, 'Bulk'), so the composite
// key is never stressed; no ItemKindChanged is emitted at all; and because the
// bulk_items row is never destroyed, backward closure holds without the
// three-event trick that existed to record a definition being discarded.
//
// What it costs: the old Item is archived and hidden from the default item
// list, and nothing points from the new Item to it. Nothing is lost -- every
// row and event remains queryable by identifier -- but the connection is not
// discoverable without searching archived items by name. Recording succession
// is deliberately deferred.

// PromoteRequest turns a counted Item into an individually-tracked one.
type PromoteRequest struct{ Item domain.ItemID }

// DemoteRequest turns an individually-tracked Item into a counted one.
type DemoteRequest struct {
	Item        domain.ItemID
	ContentUnit domain.UnitCode
	PackageSize *domain.Quantity
}

// Promote plans the re-identification.
//
// One Bulk Holding of quantity N becomes N Unique Holdings under a NEW Item,
// each with its own identity and its own HoldingCreated. The old Holding is
// debited to zero and retired; the old Item is archived.
//
// Everything is one Step across all three write paths -- origin creates the
// Item, the ledger creates the Holdings and records the retirement, annotate
// archives the old Item -- so a partially promoted Item is never observable.
// That is O2, and it is the reason ops owns transactions.
func (p *Planner) Promote(ctx context.Context, req PromoteRequest) (Batch, error) {
	s, err := p.SnapshotItem(ctx, req.Item)
	if err != nil {
		return Batch{}, err
	}
	old, err := s.bulkItem(req.Item)
	if err != nil {
		return Batch{}, err
	}

	// Individually tracking something means each unit is one object, so the
	// quantity has to BE a number of objects. Grams of rice cannot become
	// individually tracked units of anything.
	dimension, err := p.r.UnitDimension(ctx, old.ContentUnit)
	if err != nil {
		return Batch{}, err
	}
	if dimension != "Count" {
		return Batch{}, fmt.Errorf(
			"%w: %q is measured in %s, which counts no individual things; only a counted item can be tracked individually",
			ErrInvalidRequest, old.Name, old.ContentUnit)
	}

	var plan Plan
	plan.originates = append(plan.originates, NewUniqueItem{
		Name: old.Name, Category: old.Category, Notes: old.Notes,
	})

	units := 0
	for _, id := range s.ByItem[req.Item] {
		h, ok := s.Holdings[id].(domain.BulkHolding)
		if !ok || h.RetiredAt != nil {
			continue
		}
		// A Package-basis Holding counts packages, not objects. Two boxes of
		// thumbtacks are not two thumbtacks, and there is no honest number of
		// individual things to create.
		if h.UnitBasis == domain.BasisPackage {
			return Batch{}, fmt.Errorf(
				"%w: holding %d counts packages, not individual things; open them first",
				ErrInvalidRequest, id)
		}
		n, err := wholeUnits(h.Quantity)
		if err != nil {
			return Batch{}, fmt.Errorf("%w: holding %d holds %s, which is not a whole number of things",
				ErrInvalidRequest, id, h.Quantity)
		}

		for i := int64(0); i < n; i++ {
			plan.originateHolding(NewUniqueHolding{
				Item:     nil, // the Item this Step creates
				Location: h.StowedLocation,
				Label:    fmt.Sprintf("%s %d", old.Name, i+1),
			})
			units++
		}

		// Debit to zero, then retire. One Split rather than one per unit:
		// the per-unit correspondence is carried by each new Holding's own
		// HoldingCreated, so N debits would say nothing the events do not.
		if !h.Quantity.IsZero() {
			debit, err := h.Quantity.Neg()
			if err != nil {
				return Batch{}, err
			}
			subject := existing(id)
			plan.record(subject, func(hid domain.HoldingID) domain.Event {
				return domain.Split{EventBase: s.base(), Holding: hid, Delta: debit, Reason: "promoted"}
			})
		}
		gone := existing(id)
		plan.record(gone, func(hid domain.HoldingID) domain.Event {
			return domain.Gone{EventBase: s.base(), Holding: hid, Reason: "promoted"}
		})
	}

	// Archiving runs last, and only works because the Gone events above have
	// already retired every live Holding -- ArchiveItem refuses otherwise.
	plan.annotates = append(plan.annotates, ArchiveItem{Item: req.Item})

	return plan.Batch(fmt.Sprintf("track %q individually: %d units, replacing item %d",
		old.Name, units, req.Item)), nil
}

// Demote plans the reverse: N individually-tracked things become a count.
//
// Lossy going forward, and the loss is worth stating: history is retained on
// the old Holdings, but no future event can address a single unit. Holdings are
// grouped by stowed location, because a count is a count of things in one
// place -- three cables in the garage and two in the study become two Holdings,
// not one of five.
func (p *Planner) Demote(ctx context.Context, req DemoteRequest) (Batch, error) {
	s, err := p.SnapshotItem(ctx, req.Item)
	if err != nil {
		return Batch{}, err
	}
	item, err := s.Item(req.Item)
	if err != nil {
		return Batch{}, err
	}
	old, ok := item.(domain.UniqueItem)
	if !ok {
		return Batch{}, fmt.Errorf("%w: item %d is already counted", ErrInvalidRequest, req.Item)
	}
	if req.ContentUnit == "" {
		return Batch{}, fmt.Errorf("%w: a counted item needs a content unit", ErrInvalidRequest)
	}
	if req.PackageSize != nil && !req.PackageSize.IsPositive() {
		return Batch{}, fmt.Errorf("%w: package size must be positive, got %s",
			ErrInvalidRequest, req.PackageSize)
	}

	var plan Plan
	plan.originates = append(plan.originates, NewBulkItem{
		Name: old.Name, Category: old.Category, Notes: old.Notes,
		ContentUnit: req.ContentUnit, PackageSize: req.PackageSize,
	})

	// Group by where they are kept. Order is taken from ByItem so the plan is
	// deterministic for the same snapshot.
	counts := map[domain.LocationID]int64{}
	var places []domain.LocationID
	var retire []domain.HoldingID
	for _, id := range s.ByItem[req.Item] {
		h, ok := s.Holdings[id].(domain.UniqueHolding)
		if !ok || h.RetiredAt != nil {
			continue
		}
		if _, seen := counts[h.StowedLocation]; !seen {
			places = append(places, h.StowedLocation)
		}
		counts[h.StowedLocation]++
		retire = append(retire, id)
	}

	for _, at := range places {
		n := counts[at]
		target := plan.originateHolding(NewBulkHolding{
			Item: nil, Location: at, UnitBasis: domain.BasisContent,
		})
		amount, err := domain.FromWhole(n)
		if err != nil {
			return Batch{}, err
		}
		// Merged rather than Acquired: nothing arrived from outside. N things
		// were folded into one count, which is exactly what Merged records.
		plan.record(target, func(hid domain.HoldingID) domain.Event {
			return domain.Merged{EventBase: s.base(), Holding: hid, Delta: amount, Reason: "demoted"}
		})
	}
	for _, id := range retire {
		subject := existing(id)
		plan.record(subject, func(hid domain.HoldingID) domain.Event {
			return domain.Gone{EventBase: s.base(), Holding: hid, Reason: "demoted"}
		})
	}

	plan.annotates = append(plan.annotates, ArchiveItem{Item: req.Item})

	return plan.Batch(fmt.Sprintf("count %q in %s: %d units across %d places, replacing item %d",
		old.Name, req.ContentUnit, len(retire), len(places), req.Item)), nil
}

// wholeUnits converts a quantity to a whole count, refusing fractions.
func wholeUnits(q domain.Quantity) (int64, error) {
	if q.Milli()%domain.Scale != 0 {
		return 0, fmt.Errorf("not whole")
	}
	return q.Milli() / domain.Scale, nil
}
