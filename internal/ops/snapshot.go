package ops

import (
	"context"
	"errors"
	"fmt"
	"time"

	"home-management-system/internal/domain"
)

var (
	// ErrSubjectMissing means the snapshot has no such entity. A planning bug
	// or a stale reference, never a domain refusal.
	ErrSubjectMissing = errors.New("ops: not in snapshot")

	// ErrWrongKind is a Bulk operation on a Unique holding or the reverse. The
	// type system prevents this inside the domain; it can still arrive from
	// outside, which is where this catches it.
	ErrWrongKind = errors.New("ops: operation does not apply to this kind")

	// ErrRetired reports an operation on something already gone. Gone is the
	// single lifecycle terminal, and it is terminal.
	ErrRetired = errors.New("ops: holding is retired")

	// ErrCustody reports a custody transition the state machine does not allow
	// -- returning something that was never out, finding something not lost.
	ErrCustody = errors.New("ops: custody state does not allow this")

	// ErrInsufficient reports removing more than is there.
	ErrInsufficient = errors.New("ops: not enough on hand")

	// ErrInvalidRequest reports a request that could not be valid for any state
	// -- a negative quantity, a missing destination.
	ErrInvalidRequest = errors.New("ops: invalid request")
)

// Snapshot is the state an operation reads.
//
// It is gathered once, before planning, and not consulted again. That is what
// makes a plan a pure function of the snapshot and the request, and it buys two
// things at once: the interesting logic -- implicit opening, merge-not-
// duplicate, count-then-adjust -- is testable with no database, and a Batch
// shown to a person cannot change between being reviewed and being applied.
//
// The fields are the domain's own types rather than a parallel set. A
// domain.Holding already carries its folded projection, so there is nothing to
// translate, and a test can write one as a literal.
type Snapshot struct {
	// Now is the operation's clock, captured once so every event in a Batch
	// that should share a timestamp does.
	Now time.Time

	// Holdings is every Holding the operation may touch.
	Holdings map[domain.HoldingID]domain.Holding

	// Items carries the typing facts a plan needs: content unit and package
	// size decide what "2 bags" means and whether opening one is possible.
	Items map[domain.ItemID]domain.Item

	// ByItem indexes Holdings by Item, for the operations that must choose
	// among them -- which package is already open, where a receipt merges.
	ByItem map[domain.ItemID][]domain.HoldingID
}

// Holding returns a Holding, refusing a missing one rather than returning a
// zero value that would plan as if it were a real thing at location zero.
func (s Snapshot) Holding(id domain.HoldingID) (domain.Holding, error) {
	h, ok := s.Holdings[id]
	if !ok {
		return nil, fmt.Errorf("%w: holding %d", ErrSubjectMissing, id)
	}
	return h, nil
}

// LiveHolding returns a Holding that is not retired. Almost every operation
// wants this rather than Holding: acting on something Gone is not a state to
// handle but a request to refuse.
func (s Snapshot) LiveHolding(id domain.HoldingID) (domain.Holding, error) {
	h, err := s.Holding(id)
	if err != nil {
		return nil, err
	}
	if h.Base().RetiredAt != nil {
		return nil, fmt.Errorf("%w: holding %d", ErrRetired, id)
	}
	return h, nil
}

// UniqueHolding returns a live Holding that is individually tracked.
func (s Snapshot) UniqueHolding(id domain.HoldingID) (domain.UniqueHolding, error) {
	h, err := s.LiveHolding(id)
	if err != nil {
		return domain.UniqueHolding{}, err
	}
	u, ok := h.(domain.UniqueHolding)
	if !ok {
		return domain.UniqueHolding{}, fmt.Errorf("%w: holding %d is %s, not Unique",
			ErrWrongKind, id, h.Kind())
	}
	return u, nil
}

// BulkHolding returns a live Holding that is a measured amount.
func (s Snapshot) BulkHolding(id domain.HoldingID) (domain.BulkHolding, error) {
	h, err := s.LiveHolding(id)
	if err != nil {
		return domain.BulkHolding{}, err
	}
	b, ok := h.(domain.BulkHolding)
	if !ok {
		return domain.BulkHolding{}, fmt.Errorf("%w: holding %d is %s, not Bulk",
			ErrWrongKind, id, h.Kind())
	}
	return b, nil
}

// Item returns an Item.
func (s Snapshot) Item(id domain.ItemID) (domain.Item, error) {
	i, ok := s.Items[id]
	if !ok {
		return nil, fmt.Errorf("%w: item %d", ErrSubjectMissing, id)
	}
	return i, nil
}

// base is the EventBase every planned event shares, so a Batch that happened at
// one moment records one moment.
func (s Snapshot) base() domain.EventBase {
	return domain.EventBase{OccurredAt: s.Now}
}

// ---------------------------------------------------------------------------
// Gathering
// ---------------------------------------------------------------------------

// SnapshotHoldings loads exactly the Holdings named, and the Items they are of.
//
// Deliberately narrow. An operation that reads more than it needs makes its
// plan depend on state it never examined, and the point of planning against a
// snapshot is that the dependency is visible.
func (p *Planner) SnapshotHoldings(ctx context.Context, ids ...domain.HoldingID) (Snapshot, error) {
	s := Snapshot{
		Now:      p.now(),
		Holdings: make(map[domain.HoldingID]domain.Holding, len(ids)),
		Items:    make(map[domain.ItemID]domain.Item),
		ByItem:   make(map[domain.ItemID][]domain.HoldingID),
	}
	for _, id := range ids {
		if _, seen := s.Holdings[id]; seen {
			continue
		}
		detail, err := p.r.Holding(ctx, id)
		if err != nil {
			return Snapshot{}, err
		}
		s.Holdings[id] = detail.Holding

		item := detail.Holding.Base().Item
		s.ByItem[item] = append(s.ByItem[item], id)
		if _, ok := s.Items[item]; !ok {
			it, err := p.r.Item(ctx, item)
			if err != nil {
				return Snapshot{}, err
			}
			s.Items[item] = it
		}
	}
	return s, nil
}
