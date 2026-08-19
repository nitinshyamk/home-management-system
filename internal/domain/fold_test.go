package domain

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

var at = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

const (
	pantry = LocationID(1)
	garage = LocationID(2)
	hold   = HoldingID(7)
)

func base(id EventID) EventBase { return EventBase{ID: id, OccurredAt: at, RecordedAt: at} }

// createdBulk and createdUnique give a projection that has already been created,
// so tests of later events do not all have to restate the setup.
func createdBulk(t *testing.T) BulkProjection {
	t.Helper()
	p, err := Fold(ZeroProjection(KindBulk), HoldingCreated{
		EventBase: base(1), Holding: hold, StowedLocation: pantry,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	return p.(BulkProjection)
}

func createdUnique(t *testing.T) UniqueProjection {
	t.Helper()
	p, err := Fold(ZeroProjection(KindUnique), HoldingCreated{
		EventBase: base(1), Holding: hold, StowedLocation: pantry,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	return p.(UniqueProjection)
}

// ---------------------------------------------------------------------------
// Exhaustiveness — the reason the registry is generated
// ---------------------------------------------------------------------------

// TestFoldHandlesEveryEventType walks the generated registry and fails on any
// type Fold has no case for. Because AllEventTypes is derived from the AST
// rather than maintained by hand, adding an event type without handling it is a
// test failure rather than a silent gap.
//
// Location and Item events legitimately do not apply to a Holding projection,
// so ErrWrongKind counts as handled. ErrUnhandledEvent does not.
func TestFoldHandlesEveryEventType(t *testing.T) {
	if len(AllEventTypes) != 23 {
		t.Fatalf("registry has %d types, want 23 — regenerate with `go generate ./...`", len(AllEventTypes))
	}

	for _, e := range AllEventTypes {
		t.Run(fmt.Sprintf("%T", e), func(t *testing.T) {
			for _, kind := range []Kind{KindBulk, KindUnique} {
				_, err := Fold(ZeroProjection(kind), e)
				if errors.Is(err, ErrUnhandledEvent) {
					t.Fatalf("Fold has no case for %T", e)
				}
			}
		})
	}
}

// TestEveryEventTypeHasADistinctTypeString guards the mapping to the database's
// CHECK constraint: a duplicate or empty Type() would let one event masquerade
// as another in the ledger.
func TestEveryEventTypeHasADistinctTypeString(t *testing.T) {
	seen := map[EventType]string{}
	for _, e := range AllEventTypes {
		got := e.Type()
		if got == "" {
			t.Errorf("%T has an empty Type()", e)
			continue
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%T and %s share Type() %q", e, prev, got)
			continue
		}
		seen[got] = fmt.Sprintf("%T", e)
	}
}

// TestSubjectKindMatchesTypeFamily mirrors the CHECK in migration 0005. If these
// disagree, valid domain events would be rejected by the database at runtime.
func TestSubjectKindMatchesTypeFamily(t *testing.T) {
	want := map[EventType]SubjectKind{
		TypeHoldingCreated: SubjectHolding, TypeAcquired: SubjectHolding,
		TypeMoved: SubjectHolding, TypeRehomed: SubjectHolding,
		TypeConsumed: SubjectHolding, TypeDiscarded: SubjectHolding,
		TypeOpened: SubjectHolding, TypeSplit: SubjectHolding,
		TypeMerged: SubjectHolding, TypeAdjusted: SubjectHolding,
		TypeCheckedOut: SubjectHolding, TypeReturned: SubjectHolding,
		TypeMarkedLost: SubjectHolding, TypeFound: SubjectHolding,
		TypeCounted: SubjectHolding, TypeVerified: SubjectHolding,
		TypeGone: SubjectHolding,

		TypeNodeCreated: SubjectLocation, TypeNodeReparented: SubjectLocation,
		TypeNodeArchived: SubjectLocation, TypeNodeRestored: SubjectLocation,

		TypeItemUnitChanged:        SubjectItem,
		TypeItemPackageSizeChanged: SubjectItem,
	}
	if len(want) != len(AllEventTypes) {
		t.Fatalf("expectation covers %d types, registry has %d", len(want), len(AllEventTypes))
	}
	for _, e := range AllEventTypes {
		got, _ := e.Subject()
		if want[e.Type()] != got {
			t.Errorf("%s: subject kind %s, want %s", e.Type(), got, want[e.Type()])
		}
	}
}

// ---------------------------------------------------------------------------
// Creation — H11, and the case a missing HoldingCreated would break
// ---------------------------------------------------------------------------

// TestCreatedAndNeverTouched is acceptance walkthrough 00. A Holding put on a
// shelf and never moved must replay to exactly its stored state. It is the
// cheapest possible test, and the one a missing HoldingCreated fails first.
func TestCreatedAndNeverTouched(t *testing.T) {
	for _, kind := range []Kind{KindBulk, KindUnique} {
		t.Run(string(kind), func(t *testing.T) {
			p, err := FoldAll(ZeroProjection(kind), []Event{
				HoldingCreated{EventBase: base(1), Holding: hold, StowedLocation: pantry},
			})
			if err != nil {
				t.Fatalf("fold: %v", err)
			}
			if got := p.Base().StowedLocation; got != pantry {
				t.Errorf("stowed location = %d, want %d", got, pantry)
			}
			if IsRetired(p) {
				t.Error("a newly created holding is retired")
			}
		})
	}
}

func TestEventsBeforeCreationAreRejected(t *testing.T) {
	_, err := Fold(ZeroProjection(KindBulk), Consumed{
		EventBase: base(2), Holding: hold, Delta: FromMilli(-100),
	})
	if !errors.Is(err, ErrNotCreated) {
		t.Errorf("err = %v, want ErrNotCreated", err)
	}
}

func TestCreationHappensOnce(t *testing.T) {
	p := createdBulk(t)
	_, err := Fold(p, HoldingCreated{EventBase: base(2), Holding: hold, StowedLocation: garage})
	if !errors.Is(err, ErrAlreadyCreated) {
		t.Errorf("err = %v, want ErrAlreadyCreated", err)
	}
}

// ---------------------------------------------------------------------------
// Events partition by kind — they are never interpreted by kind
// ---------------------------------------------------------------------------

func TestEventsPartitionByKind(t *testing.T) {
	bulkOnly := []Event{
		Acquired{EventBase: base(2), Holding: hold, Delta: FromMilli(100)},
		Consumed{EventBase: base(2), Holding: hold, Delta: FromMilli(-100)},
		Discarded{EventBase: base(2), Holding: hold, Delta: FromMilli(-100)},
		Opened{EventBase: base(2), Holding: hold, Delta: FromMilli(100)},
		Split{EventBase: base(2), Holding: hold, Delta: FromMilli(-100)},
		Merged{EventBase: base(2), Holding: hold, Delta: FromMilli(100)},
		Adjusted{EventBase: base(2), Holding: hold, Delta: FromMilli(-1)},
		Counted{EventBase: base(2), Holding: hold, Observed: FromMilli(100)},
	}
	uniqueOnly := []Event{
		CheckedOut{EventBase: base(2), Holding: hold},
		Returned{EventBase: base(2), Holding: hold},
		MarkedLost{EventBase: base(2), Holding: hold},
		Found{EventBase: base(2), Holding: hold},
		Verified{EventBase: base(2), Holding: hold, Present: true},
	}

	// Bulk events are exercised against a holding with stock, so that a
	// negative delta is testing kind-partitioning rather than tripping H6.
	stocked := func() BulkProjection {
		p := createdBulk(t)
		p.Quantity = FromMilli(1_000)
		return p
	}

	for _, e := range bulkOnly {
		if _, err := Fold(createdUnique(t), e); !errors.Is(err, ErrWrongKind) {
			t.Errorf("%s against a Unique holding: err = %v, want ErrWrongKind", e.Type(), err)
		}
		if _, err := Fold(stocked(), e); err != nil {
			t.Errorf("%s against a Bulk holding: %v", e.Type(), err)
		}
	}
	for _, e := range uniqueOnly {
		if _, err := Fold(stocked(), e); !errors.Is(err, ErrWrongKind) {
			t.Errorf("%s against a Bulk holding: err = %v, want ErrWrongKind", e.Type(), err)
		}
		if _, err := Fold(createdUnique(t), e); err != nil {
			t.Errorf("%s against a Unique holding: %v", e.Type(), err)
		}
	}
}

func TestNonHoldingEventsAreRejected(t *testing.T) {
	for _, e := range []Event{
		NodeCreated{EventBase: base(2), Location: pantry},
		NodeReparented{EventBase: base(2), Location: pantry},
		NodeArchived{EventBase: base(2), Location: pantry, Resolution: ResolutionLift},
		NodeRestored{EventBase: base(2), Location: pantry},
		ItemUnitChanged{EventBase: base(2)},
		ItemPackageSizeChanged{EventBase: base(2)},
	} {
		if _, err := Fold(createdBulk(t), e); !errors.Is(err, ErrWrongKind) {
			t.Errorf("%s: err = %v, want ErrWrongKind", e.Type(), err)
		}
	}
}

// ---------------------------------------------------------------------------
// Quantity semantics
// ---------------------------------------------------------------------------

func TestQuantityAccumulates(t *testing.T) {
	twoKg := FromMilli(2_000_000)

	p, err := FoldAll(createdBulk(t), []Event{
		Acquired{EventBase: base(2), Holding: hold, Delta: twoKg},
		Consumed{EventBase: base(3), Holding: hold, Delta: FromMilli(-100_000)},
		Consumed{EventBase: base(4), Holding: hold, Delta: FromMilli(-100_000)},
	})
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	want := FromMilli(1_800_000)
	if got := p.(BulkProjection).Quantity; got.Cmp(want) != 0 {
		t.Errorf("quantity = %s, want %s", got, want)
	}
}

func TestQuantityMayReachZeroButNotGoNegative(t *testing.T) {
	p, err := FoldAll(createdBulk(t), []Event{
		Acquired{EventBase: base(2), Holding: hold, Delta: FromMilli(100)},
		Consumed{EventBase: base(3), Holding: hold, Delta: FromMilli(-100)},
	})
	if err != nil {
		t.Fatalf("depleting to zero: %v", err)
	}
	if !p.(BulkProjection).Quantity.IsZero() {
		t.Errorf("quantity = %s, want 0", p.(BulkProjection).Quantity)
	}

	_, err = Fold(p, Consumed{EventBase: base(4), Holding: hold, Delta: FromMilli(-1)})
	if !errors.Is(err, ErrNegativeQuantity) {
		t.Errorf("err = %v, want ErrNegativeQuantity", err)
	}
}

func TestDeltaSignIsEnforcedPerType(t *testing.T) {
	cases := []struct {
		event Event
		valid bool
	}{
		{Acquired{EventBase: base(2), Holding: hold, Delta: FromMilli(100)}, true},
		{Acquired{EventBase: base(2), Holding: hold, Delta: FromMilli(-100)}, false},
		{Consumed{EventBase: base(2), Holding: hold, Delta: FromMilli(-100)}, true},
		{Consumed{EventBase: base(2), Holding: hold, Delta: FromMilli(100)}, false},
		{Opened{EventBase: base(2), Holding: hold, Delta: FromMilli(100)}, true},
		{Split{EventBase: base(2), Holding: hold, Delta: FromMilli(-100)}, true},
		{Split{EventBase: base(2), Holding: hold, Delta: FromMilli(100)}, false},
		// Adjusted corrects in either direction, but never by nothing.
		{Adjusted{EventBase: base(2), Holding: hold, Delta: FromMilli(5)}, true},
		{Adjusted{EventBase: base(2), Holding: hold, Delta: Zero}, false},
	}
	for _, tc := range cases {
		p := createdBulk(t)
		p.Quantity = FromMilli(1000)
		_, err := Fold(p, tc.event)
		switch {
		case tc.valid && err != nil:
			t.Errorf("%s with delta %s: %v", tc.event.Type(), deltaOf(tc.event), err)
		case !tc.valid && !errors.Is(err, ErrBadDelta):
			t.Errorf("%s with delta %s: err = %v, want ErrBadDelta",
				tc.event.Type(), deltaOf(tc.event), err)
		}
	}
}

func deltaOf(e Event) Quantity {
	switch typed := e.(type) {
	case Acquired:
		return typed.Delta
	case Consumed:
		return typed.Delta
	case Opened:
		return typed.Delta
	case Split:
		return typed.Delta
	case Adjusted:
		return typed.Delta
	default:
		return Zero
	}
}

// TestCountedDoesNotOverwrite is the property that makes the ledger trustworthy
// enough to derive consumption rates from: an observation records what was seen
// and never silently corrects the books.
func TestCountedDoesNotOverwrite(t *testing.T) {
	p, err := Fold(createdBulk(t), Acquired{EventBase: base(2), Holding: hold, Delta: FromMilli(500)})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	after, err := Fold(p, Counted{EventBase: base(3), Holding: hold, Observed: FromMilli(480)})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if got := after.(BulkProjection).Quantity; got.Cmp(FromMilli(500)) != 0 {
		t.Errorf("quantity = %s after a disagreeing count, want 500 unchanged", got)
	}
}

// ---------------------------------------------------------------------------
// Custody semantics
// ---------------------------------------------------------------------------

func TestCustodyRoundTrip(t *testing.T) {
	p, err := Fold(createdUnique(t), CheckedOut{
		EventBase: base(2), Holding: hold, DisplacedTo: ptr(garage),
	})
	if err != nil {
		t.Fatalf("check out: %v", err)
	}
	out := p.(UniqueProjection)
	if out.Custody != CustodyOut {
		t.Errorf("custody = %s, want Out", out.Custody)
	}
	if out.CustodySince == nil {
		t.Error("CustodySince is nil while Out — HU1 violated")
	}
	if out.DisplacedTo == nil || *out.DisplacedTo != garage {
		t.Errorf("displaced to %v, want %d", out.DisplacedTo, garage)
	}

	p, err = Fold(p, Returned{EventBase: base(3), Holding: hold})
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	back := p.(UniqueProjection)
	if back.Custody != CustodyAtRest {
		t.Errorf("custody = %s, want AtRest", back.Custody)
	}
	if back.CustodySince != nil {
		t.Error("CustodySince set while AtRest — HU1 violated")
	}
	if back.DisplacedTo != nil {
		t.Error("DisplacedTo set while AtRest — HU2 violated")
	}
}

// TestCheckedOutWithoutDestinationIsMissing covers the derived state: Out with
// no known displacement is transient ignorance, distinct from Lost.
func TestCheckedOutWithoutDestinationIsMissing(t *testing.T) {
	p, err := Fold(createdUnique(t), CheckedOut{EventBase: base(2), Holding: hold})
	if err != nil {
		t.Fatalf("check out: %v", err)
	}
	out := p.(UniqueProjection)
	if out.Custody != CustodyOut || out.DisplacedTo != nil {
		t.Fatalf("unexpected state: %+v", out)
	}

	h := UniqueHolding{Custody: out.Custody, DisplacedTo: out.DisplacedTo}
	if !h.IsMissing() {
		t.Error("Out with no displacement should be derived-Missing")
	}
	h.Custody = CustodyLost
	if h.IsMissing() {
		t.Error("Lost is a conclusion, not Missing")
	}
}

func TestFoundReversesLost(t *testing.T) {
	p, err := FoldAll(createdUnique(t), []Event{
		MarkedLost{EventBase: base(2), Holding: hold},
	})
	if err != nil {
		t.Fatalf("mark lost: %v", err)
	}
	if p.(UniqueProjection).Custody != CustodyLost {
		t.Fatalf("custody = %s, want Lost", p.(UniqueProjection).Custody)
	}

	// Lost has a follow-up path, which is precisely why it is a custody state
	// and not a lifecycle terminal.
	p, err = Fold(p, Found{EventBase: base(3), Holding: hold})
	if err != nil {
		t.Fatalf("found: %v", err)
	}
	if p.(UniqueProjection).Custody != CustodyAtRest {
		t.Errorf("custody = %s, want AtRest", p.(UniqueProjection).Custody)
	}
}

// ---------------------------------------------------------------------------
// Placement and retirement
// ---------------------------------------------------------------------------

func TestMovedAndRehomedBothChangeStowedLocation(t *testing.T) {
	for _, e := range []Event{
		Moved{EventBase: base(2), Holding: hold, From: pantry, To: garage},
		Rehomed{EventBase: base(2), Holding: hold, From: pantry, To: garage},
	} {
		p, err := Fold(createdBulk(t), e)
		if err != nil {
			t.Fatalf("%s: %v", e.Type(), err)
		}
		if got := p.Base().StowedLocation; got != garage {
			t.Errorf("%s: stowed location = %d, want %d", e.Type(), got, garage)
		}
	}
}

// TestGoneIsTerminal is H9: a retired holding accepts nothing further.
func TestGoneIsTerminal(t *testing.T) {
	p, err := Fold(createdBulk(t), Gone{EventBase: base(2), Holding: hold, Reason: "donated"})
	if err != nil {
		t.Fatalf("gone: %v", err)
	}
	if !IsRetired(p) {
		t.Fatal("holding is not retired after Gone")
	}

	for _, e := range []Event{
		Acquired{EventBase: base(3), Holding: hold, Delta: FromMilli(1)},
		Moved{EventBase: base(3), Holding: hold, From: pantry, To: garage},
		Gone{EventBase: base(3), Holding: hold},
	} {
		if _, err := Fold(p, e); !errors.Is(err, ErrRetired) {
			t.Errorf("%s after Gone: err = %v, want ErrRetired", e.Type(), err)
		}
	}
}

// ---------------------------------------------------------------------------
// Purity — the property the whole design rests on
// ---------------------------------------------------------------------------

// TestFoldIsPure asserts Fold neither mutates its input nor varies its output.
// This is what makes Apply and Replay incapable of disagreeing, and therefore
// what reduces H10 to a completeness check.
func TestFoldIsPure(t *testing.T) {
	for _, kind := range []Kind{KindBulk, KindUnique} {
		start := ZeroProjection(kind)
		create := HoldingCreated{EventBase: base(1), Holding: hold, StowedLocation: pantry}

		first, err := Fold(start, create)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		// The input must be untouched.
		if start.Base().StowedLocation != 0 {
			t.Errorf("%s: Fold mutated its input projection", kind)
		}
		// And the same call must produce the same result.
		second, err := Fold(start, create)
		if err != nil {
			t.Fatalf("%s: second call: %v", kind, err)
		}
		if first.Base() != second.Base() {
			t.Errorf("%s: Fold is not deterministic: %+v vs %+v", kind, first, second)
		}
	}
}

func ptr[T any](v T) *T { return &v }
