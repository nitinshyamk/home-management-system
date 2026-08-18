package ops

import (
	"context"
	"fmt"
	"time"

	"home-management-system/internal/domain"
)

// The composing operations. One user action, several events, and the reason
// this layer exists.
//
// Two rules drive all of them:
//
//	H8  No two ACTIVE Holdings share (item, stowed_location, unit_basis,
//	    expires_on); two null expiries compare equal.
//	O1  Any write that would violate H8 merges into the existing Holding
//	    rather than creating a second.
//
// H8 is transactional rather than declarative -- the variant split put
// unit_basis on bulk_holdings while its other keys live on the base table, and
// no single-table uniqueness constraint spans two tables (schema 3.8). So it is
// upheld here, in the planning, which makes these functions the enforcement
// rather than merely the convenience.

// slot is the H8 key: the tuple that may not repeat among active Holdings.
type slot struct {
	item      domain.ItemID
	location  domain.LocationID
	basis     domain.UnitBasis
	expiresOn string // formatted, so two nil expiries compare equal
}

func slotOf(h domain.BulkHolding) slot {
	return slot{
		item:      h.Item,
		location:  h.StowedLocation,
		basis:     h.UnitBasis,
		expiresOn: expiryKey(h.ExpiresOn),
	}
}

// expiryKey renders an expiry for comparison. H8 requires two null expiries to
// compare EQUAL, which is the opposite of SQL's null semantics and a large part
// of why the rule could never have been declarative.
func expiryKey(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}

// findSlot returns the active Bulk Holding occupying a slot, if any.
func (s Snapshot) findSlot(want slot) (domain.BulkHolding, bool) {
	for _, id := range s.ByItem[want.item] {
		h, ok := s.Holdings[id].(domain.BulkHolding)
		if !ok || h.RetiredAt != nil {
			continue
		}
		if slotOf(h) == want {
			return h, true
		}
	}
	return domain.BulkHolding{}, false
}

// bulkItem returns the Item's measurement facts, which decide what a package
// is worth and whether one exists at all.
func (s Snapshot) bulkItem(id domain.ItemID) (domain.BulkItem, error) {
	it, err := s.Item(id)
	if err != nil {
		return domain.BulkItem{}, err
	}
	b, ok := it.(domain.BulkItem)
	if !ok {
		return domain.BulkItem{}, fmt.Errorf("%w: item %d is %s, not Bulk", ErrWrongKind, id, it.Kind())
	}
	return b, nil
}

// ---------------------------------------------------------------------------
// Receive
// ---------------------------------------------------------------------------

// ReceiveRequest credits stock that arrived.
type ReceiveRequest struct {
	Item      domain.ItemID
	Location  domain.LocationID
	Amount    domain.Quantity
	Basis     domain.UnitBasis
	ExpiresOn *time.Time
	Source    string
	Price     *int64
}

// PlanReceive credits an existing Holding, or creates one if the slot is free.
//
// This is O1 at its simplest. Buying more rice into a pantry that already has
// rice does NOT make a second pile: it credits the pile that is there. No
// Merged event is appended, because nothing was folded together -- the write
// simply landed on the Holding that already occupied the slot. Merged is for
// combining two Holdings that both already exist, which is what Move does when
// it collides.
func PlanReceive(s Snapshot, req ReceiveRequest) (Plan, error) {
	if !req.Amount.IsPositive() {
		return Plan{}, fmt.Errorf("%w: received amount must be positive, got %s", ErrInvalidRequest, req.Amount)
	}
	if !req.Basis.Valid() {
		return Plan{}, fmt.Errorf("%w: unknown unit basis %q", ErrInvalidRequest, req.Basis)
	}
	item, err := s.bulkItem(req.Item)
	if err != nil {
		return Plan{}, err
	}
	// H7: a Package-basis Holding is only legal for an Item that has packages.
	if req.Basis == domain.BasisPackage && !item.HasPackage() {
		return Plan{}, fmt.Errorf("%w: %q has no package size, so it cannot be counted in packages",
			ErrInvalidRequest, item.Name)
	}

	var p Plan
	into := p.holdingFor(s, slot{req.Item, req.Location, req.Basis, expiryKey(req.ExpiresOn)},
		func() Origination {
			return NewBulkHolding{
				Item: &req.Item, Location: req.Location, UnitBasis: req.Basis, ExpiresOn: req.ExpiresOn,
			}
		})
	p.record(into, func(h domain.HoldingID) domain.Event {
		return domain.Acquired{
			EventBase: s.base(), Holding: h, Delta: req.Amount,
			Source: req.Source, Price: req.Price,
		}
	})
	return p, nil
}

// ---------------------------------------------------------------------------
// Open
// ---------------------------------------------------------------------------

// OpenRequest breaks into one package.
type OpenRequest struct {
	Item      domain.ItemID
	Location  domain.LocationID
	ExpiresOn *time.Time
}

// PlanOpen debits one package and credits its contents.
//
// The two events land on DIFFERENT Holdings: Split on the package-basis one,
// Opened on the content-basis one. Opened's delta is a RESOLVED amount -- +2000
// g, never "one package's worth" -- so editing the item's package size later
// cannot rewrite what this event meant (E6).
func PlanOpen(s Snapshot, req OpenRequest) (Plan, error) {
	item, err := s.bulkItem(req.Item)
	if err != nil {
		return Plan{}, err
	}
	var p Plan
	if _, err := openOnePackage(s, &p, item, req.Location, req.ExpiresOn); err != nil {
		return Plan{}, err
	}
	return p, nil
}

// openOnePackage appends the events for breaking into one package, creating the
// content-basis Holding if this is the first one ever opened here.
func openOnePackage(
	s Snapshot,
	p *Plan,
	item domain.BulkItem,
	at domain.LocationID,
	expires *time.Time,
) (ref, error) {
	if !item.HasPackage() {
		return ref{}, fmt.Errorf("%w: %q has no package size, so there is nothing to open",
			ErrInvalidRequest, item.Name)
	}
	packages, ok := s.findSlot(slot{item.ID, at, domain.BasisPackage, expiryKey(expires)})
	if !ok {
		return ref{}, fmt.Errorf("%w: no sealed packages of %q at location %d",
			ErrInsufficient, item.Name, at)
	}
	sealed := existing(packages.ID)
	one := domain.FromMilli(domain.Scale)
	// Packages this plan has already opened are gone even though the snapshot
	// still shows them, so opening two from a stock of one must fail here.
	if packages.Quantity.Milli()+p.shifted(sealed) < one.Milli() {
		return ref{}, fmt.Errorf("%w: no whole packages of %q left at location %d",
			ErrInsufficient, item.Name, at)
	}

	debit, err := one.Neg()
	if err != nil {
		return ref{}, err
	}
	p.record(sealed, func(h domain.HoldingID) domain.Event {
		return domain.Split{EventBase: s.base(), Holding: h, Delta: debit, Reason: "opened"}
	})
	p.shift(sealed, debit.Milli())

	target := p.holdingFor(s, slot{item.ID, at, domain.BasisContent, expiryKey(expires)},
		func() Origination {
			return NewBulkHolding{
				Item: &item.ID, Location: at, UnitBasis: domain.BasisContent, ExpiresOn: expires,
			}
		})
	p.record(target, func(h domain.HoldingID) domain.Event {
		return domain.Opened{EventBase: s.base(), Holding: h, Delta: *item.PackageSize}
	})
	p.shift(target, item.PackageSize.Milli())
	return target, nil
}

// ---------------------------------------------------------------------------
// Consume
// ---------------------------------------------------------------------------

// ConsumeRequest is normal use.
type ConsumeRequest struct {
	Item      domain.ItemID
	Location  domain.LocationID
	Amount    domain.Quantity
	ExpiresOn *time.Time
	Reason    string
}

// PlanConsume debits contents, opening packages as needed.
//
// This is the operation the whole layer was designed around. "Use 100 g of
// rice" when the only rice is a sealed 2 kg bag is three recorded events and a
// created Holding, and a person should have to think about none of it.
//
// Opening is implicit but never silent: the events say a package was broken
// into, so the history reads the way the kitchen actually went.
func PlanConsume(s Snapshot, req ConsumeRequest) (Plan, error) {
	if !req.Amount.IsPositive() {
		return Plan{}, fmt.Errorf("%w: consumed amount must be positive, got %s", ErrInvalidRequest, req.Amount)
	}
	item, err := s.bulkItem(req.Item)
	if err != nil {
		return Plan{}, err
	}

	var p Plan
	contents, hasContents := s.findSlot(slot{req.Item, req.Location, domain.BasisContent, expiryKey(req.ExpiresOn)})
	target := existing(contents.ID)
	available := int64(0)
	if hasContents {
		available = contents.Quantity.Milli()
	}

	// Open whole packages until there is enough. Opening as many as the amount
	// needs, rather than one and then giving up, is what makes "use 3 kg" work
	// when packages hold 2 kg.
	for available < req.Amount.Milli() {
		opened, err := openOnePackage(s, &p, item, req.Location, req.ExpiresOn)
		if err != nil {
			return Plan{}, fmt.Errorf("%w (have %s, need %s)", err,
				domain.FromMilli(available), req.Amount)
		}
		target = opened
		available += item.PackageSize.Milli()
	}

	debit, err := req.Amount.Neg()
	if err != nil {
		return Plan{}, err
	}
	p.record(target, func(h domain.HoldingID) domain.Event {
		return domain.Consumed{EventBase: s.base(), Holding: h, Delta: debit, Reason: req.Reason}
	})
	return p, nil
}

// ---------------------------------------------------------------------------
// Count
// ---------------------------------------------------------------------------

// CountRequest observes how much is actually there.
type CountRequest struct {
	Holding  domain.HoldingID
	Observed domain.Quantity
}

// PlanCount records the observation, and corrects only if it disagrees.
//
// A count NEVER overwrites the ledger. It appends what was seen, and if that
// differs from what the ledger says, a separate Adjusted carries the
// discrepancy explicitly. Silently setting the quantity would destroy the only
// evidence that the two ever disagreed -- which is the whole signal a count
// exists to produce.
func PlanCount(s Snapshot, req CountRequest) (Plan, error) {
	h, err := s.BulkHolding(req.Holding)
	if err != nil {
		return Plan{}, err
	}
	if req.Observed.IsNegative() {
		return Plan{}, fmt.Errorf("%w: observed amount cannot be negative, got %s",
			ErrInvalidRequest, req.Observed)
	}

	var p Plan
	subject := existing(req.Holding)
	p.record(subject, func(id domain.HoldingID) domain.Event {
		return domain.Counted{EventBase: s.base(), Holding: id, Observed: req.Observed}
	})

	if diff := req.Observed.Milli() - h.Quantity.Milli(); diff != 0 {
		p.record(subject, func(id domain.HoldingID) domain.Event {
			return domain.Adjusted{
				EventBase: s.base(), Holding: id, Delta: domain.FromMilli(diff),
				Reason: fmt.Sprintf("count said %s, ledger said %s", req.Observed, h.Quantity),
			}
		})
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Move
// ---------------------------------------------------------------------------

// MoveRequest relocates stock. Amount nil means all of it.
type MoveRequest struct {
	Holding domain.HoldingID
	To      domain.LocationID
	Amount  *domain.Quantity
}

// PlanMove relocates a Holding, merging if the destination is occupied.
//
// The shape differs by whether anything is already there, and that is H8 rather
// than a choice: Moved changes stowed_location, so moving onto an occupied slot
// would put two active Holdings on the same key. So a collision becomes a pair
// of Merged events -- the source debited, the destination credited -- and the
// emptied source stays where it is, because an empty jar is still the jar.
func PlanMove(s Snapshot, req MoveRequest) (Plan, error) {
	h, err := s.LiveHolding(req.Holding)
	if err != nil {
		return Plan{}, err
	}
	if h.Base().StowedLocation == req.To {
		return Plan{}, fmt.Errorf("%w: holding %d is already at location %d",
			ErrInvalidRequest, req.Holding, req.To)
	}

	var p Plan

	// A Unique Holding is one thing. There is nothing to divide and nothing to
	// merge with, so it simply moves.
	if u, ok := h.(domain.UniqueHolding); ok {
		if req.Amount != nil {
			return Plan{}, fmt.Errorf("%w: holding %d is one thing, so a quantity means nothing",
				ErrInvalidRequest, req.Holding)
		}
		p.record(existing(req.Holding), func(id domain.HoldingID) domain.Event {
			return domain.Moved{EventBase: s.base(), Holding: id, From: u.StowedLocation, To: req.To}
		})
		return p, nil
	}

	b := h.(domain.BulkHolding)
	amount := b.Quantity
	whole := true
	if req.Amount != nil {
		amount = *req.Amount
		whole = amount.Milli() == b.Quantity.Milli()
		if !amount.IsPositive() {
			return Plan{}, fmt.Errorf("%w: moved amount must be positive, got %s", ErrInvalidRequest, amount)
		}
		if amount.Milli() > b.Quantity.Milli() {
			return Plan{}, fmt.Errorf("%w: holding %d has %s, cannot move %s",
				ErrInsufficient, req.Holding, b.Quantity, amount)
		}
	}

	destinationSlot := slot{b.Item, req.To, b.UnitBasis, expiryKey(b.ExpiresOn)}
	_, occupied := s.findSlot(destinationSlot)

	// Whole move onto a free slot: the Holding itself relocates, keeping its
	// identity, its expiry, and its history.
	if whole && !occupied {
		p.record(existing(req.Holding), func(id domain.HoldingID) domain.Event {
			return domain.Moved{EventBase: s.base(), Holding: id, From: b.StowedLocation, To: req.To}
		})
		return p, nil
	}

	target := p.holdingFor(s, destinationSlot, func() Origination {
		return NewBulkHolding{
			Item: &b.Item, Location: req.To, UnitBasis: b.UnitBasis, ExpiresOn: b.ExpiresOn,
		}
	})
	debit, err := amount.Neg()
	if err != nil {
		return Plan{}, err
	}
	p.record(existing(req.Holding), func(id domain.HoldingID) domain.Event {
		return domain.Merged{EventBase: s.base(), Holding: id, Delta: debit, Reason: "moved"}
	})
	p.record(target, func(id domain.HoldingID) domain.Event {
		return domain.Merged{EventBase: s.base(), Holding: id, Delta: amount, Reason: "moved"}
	})
	return p, nil
}

// ---------------------------------------------------------------------------
// Planner surface
// ---------------------------------------------------------------------------

// planItem gathers every Holding of an Item, plans against it, and wraps the
// result. The composing operations all need this breadth.
func planItem[R any](
	ctx context.Context,
	p *Planner,
	item domain.ItemID,
	req R,
	build func(Snapshot, R) (Plan, error),
	summary string,
) (Batch, error) {
	s, err := p.SnapshotItem(ctx, item)
	if err != nil {
		return Batch{}, err
	}
	planned, err := build(s, req)
	if err != nil {
		return Batch{}, err
	}
	return planned.Batch(summary), nil
}

func (p *Planner) Receive(ctx context.Context, req ReceiveRequest) (Batch, error) {
	return planItem(ctx, p, req.Item, req, PlanReceive,
		fmt.Sprintf("received %s of item %d at location %d", req.Amount, req.Item, req.Location))
}

func (p *Planner) Consume(ctx context.Context, req ConsumeRequest) (Batch, error) {
	return planItem(ctx, p, req.Item, req, PlanConsume,
		fmt.Sprintf("used %s of item %d", req.Amount, req.Item))
}

func (p *Planner) Open(ctx context.Context, req OpenRequest) (Batch, error) {
	return planItem(ctx, p, req.Item, req,
		func(s Snapshot, r OpenRequest) (Plan, error) { return PlanOpen(s, r) },
		fmt.Sprintf("opened a package of item %d", req.Item))
}

func (p *Planner) Count(ctx context.Context, req CountRequest) (Batch, error) {
	s, err := p.SnapshotHoldings(ctx, req.Holding)
	if err != nil {
		return Batch{}, err
	}
	planned, err := PlanCount(s, req)
	if err != nil {
		return Batch{}, err
	}
	return planned.Batch(fmt.Sprintf("counted %s in holding %d", req.Observed, req.Holding)), nil
}

// Move snapshots by ITEM rather than by Holding: it has to see whether the
// destination is already occupied, and that is a different Holding.
func (p *Planner) Move(ctx context.Context, req MoveRequest) (Batch, error) {
	detail, err := p.r.Holding(ctx, req.Holding)
	if err != nil {
		return Batch{}, err
	}
	return planItem(ctx, p, detail.Holding.Base().Item, req, PlanMove,
		fmt.Sprintf("moved holding %d to location %d", req.Holding, req.To))
}
