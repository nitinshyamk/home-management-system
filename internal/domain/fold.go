package domain

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrUnhandledEvent means Fold has no case for an event type. The generated
	// registry and TestFoldHandlesEveryEventType exist to make this unreachable.
	ErrUnhandledEvent = errors.New("fold: no case for event type")

	// ErrWrongKind means an event was applied to the wrong holding variant —
	// CheckedOut against a Bulk holding, say. Events partition by kind, so this
	// is a defect in whatever produced the event, not a legal state.
	ErrWrongKind = errors.New("fold: event does not apply to this holding kind")

	// ErrNegativeQuantity is H6. Zero is legal — depletion is a normal end
	// state — but a negative amount is not a quantity.
	ErrNegativeQuantity = errors.New("fold: quantity would go negative")

	// ErrRetired is H9: a retired holding accepts no further events.
	ErrRetired = errors.New("fold: holding is retired")

	// ErrAlreadyCreated means a second HoldingCreated arrived. Creation happens
	// once, first.
	ErrAlreadyCreated = errors.New("fold: holding already created")

	// ErrNotCreated means an event arrived before the holding's creation event.
	ErrNotCreated = errors.New("fold: holding has no creation event yet")

	// ErrBadDelta means a quantity event carried a sign its type forbids.
	ErrBadDelta = errors.New("fold: delta has the wrong sign for this event type")
)

// Fold applies one event to a Holding projection.
//
// This is the single place event semantics live. ledger.Apply (incremental,
// from current state) and ledger.Replay (from zero or a checkpoint) both call
// it, which is why they cannot disagree — and therefore why H10 can only fail
// from *incompleteness*, never from divergence. A discrepancy the integrity job
// reports is unambiguously a write that skipped its event.
//
// Fold is pure: same inputs, same outputs, no I/O, no clock.
func Fold(p Projection, e Event) (Projection, error) {
	if p == nil {
		return nil, fmt.Errorf("fold: nil projection")
	}

	// H9: a retired holding accepts nothing further. Checked before dispatch so
	// no individual case has to remember it.
	if IsRetired(p) {
		return p, fmt.Errorf("%w: %s", ErrRetired, e.Type())
	}

	switch typed := e.(type) {

	// --- shared: apply to either variant --------------------------------
	case HoldingCreated:
		// A zero StowedLocation means "not yet created": identifiers are
		// autoincrement and start at 1, so zero is never a real location.
		base := p.Base()
		if base.StowedLocation != 0 {
			return p, fmt.Errorf("%w: holding %d", ErrAlreadyCreated, typed.Holding)
		}
		base.StowedLocation = typed.StowedLocation
		return withBase(p, base), nil

	case Moved:
		base, err := requireCreated(p, typed.Type())
		if err != nil {
			return p, err
		}
		base.StowedLocation = typed.To
		return withBase(p, base), nil

	case Rehomed:
		base, err := requireCreated(p, typed.Type())
		if err != nil {
			return p, err
		}
		base.StowedLocation = typed.To
		return withBase(p, base), nil

	case Gone:
		base, err := requireCreated(p, typed.Type())
		if err != nil {
			return p, err
		}
		at := typed.OccurredAt
		base.RetiredAt = &at
		return withBase(p, base), nil

	// --- Bulk only: quantity ---------------------------------------------
	case Acquired:
		return applyDelta(p, typed.Type(), typed.Delta, signPositive)
	case Opened:
		return applyDelta(p, typed.Type(), typed.Delta, signPositive)
	case Consumed:
		return applyDelta(p, typed.Type(), typed.Delta, signNegative)
	case Discarded:
		return applyDelta(p, typed.Type(), typed.Delta, signNegative)
	case Split:
		return applyDelta(p, typed.Type(), typed.Delta, signNegative)
	case Merged:
		return applyDelta(p, typed.Type(), typed.Delta, signAny)
	case Adjusted:
		return applyDelta(p, typed.Type(), typed.Delta, signAny)

	// --- Bulk only: observation, which changes nothing --------------------
	case Counted:
		// Counted records what was seen. It never overwrites — a discrepancy
		// produces a separate Adjusted, and that separation is what makes the
		// ledger trustworthy enough to derive consumption rates from.
		if _, ok := p.(BulkProjection); !ok {
			return p, fmt.Errorf("%w: %s on %s", ErrWrongKind, typed.Type(), p.Kind())
		}
		if _, err := requireCreated(p, typed.Type()); err != nil {
			return p, err
		}
		return p, nil

	// --- Unique only: custody ---------------------------------------------
	case CheckedOut:
		return applyCustody(p, typed.Type(), CustodyOut, typed.OccurredAt, typed.DisplacedTo)
	case Returned:
		return applyCustody(p, typed.Type(), CustodyAtRest, typed.OccurredAt, nil)
	case MarkedLost:
		return applyCustody(p, typed.Type(), CustodyLost, typed.OccurredAt, nil)
	case Found:
		return applyCustody(p, typed.Type(), CustodyAtRest, typed.OccurredAt, nil)

	// --- Unique only: observation, which changes nothing ------------------
	case Verified:
		if _, ok := p.(UniqueProjection); !ok {
			return p, fmt.Errorf("%w: %s on %s", ErrWrongKind, typed.Type(), p.Kind())
		}
		if _, err := requireCreated(p, typed.Type()); err != nil {
			return p, err
		}
		return p, nil

	// --- not Holding events: no effect on a Holding projection ------------
	case NodeCreated, NodeReparented, NodeArchived, NodeRestored,
		ItemUnitChanged, ItemPackageSizeChanged:
		// Location and Item events are recorded in the same ledger but fold into
		// their own subjects. Reaching a Holding projection with one is a routing
		// error, so it is named rather than ignored.
		return p, fmt.Errorf("%w: %s is not a Holding event", ErrWrongKind, e.Type())

	default:
		return p, fmt.Errorf("%w: %T", ErrUnhandledEvent, e)
	}
}

// FoldAll replays a sequence in order. Events must already be ordered by ID.
func FoldAll(p Projection, events []Event) (Projection, error) {
	var err error
	for i, e := range events {
		p, err = Fold(p, e)
		if err != nil {
			return p, fmt.Errorf("event %d of %d (%s, id=%d): %w",
				i+1, len(events), e.Type(), e.Base().ID, err)
		}
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type sign int

const (
	signAny sign = iota
	signPositive
	signNegative
)

// requireCreated enforces H11's ordering half: creation comes first.
func requireCreated(p Projection, t EventType) (ProjectionBase, error) {
	base := p.Base()
	if base.StowedLocation == 0 {
		return base, fmt.Errorf("%w: %s arrived before HoldingCreated", ErrNotCreated, t)
	}
	return base, nil
}

// withBase returns p with a replaced base, preserving the variant.
func withBase(p Projection, base ProjectionBase) Projection {
	switch typed := p.(type) {
	case BulkProjection:
		typed.ProjectionBase = base
		return typed
	case UniqueProjection:
		typed.ProjectionBase = base
		return typed
	default:
		return p
	}
}

func applyDelta(p Projection, t EventType, delta Quantity, want sign) (Projection, error) {
	bulk, ok := p.(BulkProjection)
	if !ok {
		return p, fmt.Errorf("%w: %s on %s", ErrWrongKind, t, p.Kind())
	}
	if _, err := requireCreated(p, t); err != nil {
		return p, err
	}

	switch want {
	case signPositive:
		if !delta.IsPositive() {
			return p, fmt.Errorf("%w: %s expects a positive delta, got %s", ErrBadDelta, t, delta)
		}
	case signNegative:
		if !delta.IsNegative() {
			return p, fmt.Errorf("%w: %s expects a negative delta, got %s", ErrBadDelta, t, delta)
		}
	case signAny:
		if delta.IsZero() {
			return p, fmt.Errorf("%w: %s expects a non-zero delta", ErrBadDelta, t)
		}
	}

	next, err := bulk.Quantity.Add(delta)
	if err != nil {
		return p, fmt.Errorf("%s: %w", t, err)
	}
	if next.IsNegative() {
		return p, fmt.Errorf("%w: %s %s would take %s to %s",
			ErrNegativeQuantity, t, delta, bulk.Quantity, next)
	}
	bulk.Quantity = next
	return bulk, nil
}

func applyCustody(p Projection, t EventType, to Custody, at time.Time, displaced *LocationID) (Projection, error) {
	unique, ok := p.(UniqueProjection)
	if !ok {
		return p, fmt.Errorf("%w: %s on %s", ErrWrongKind, t, p.Kind())
	}
	if _, err := requireCreated(p, t); err != nil {
		return p, err
	}

	unique.Custody = to
	unique.DisplacedTo = displaced
	if to == CustodyAtRest {
		// HU1: AtRest and CustodySince agree, in both directions.
		unique.CustodySince = nil
	} else {
		since := at
		unique.CustodySince = &since
	}
	return unique, nil
}
