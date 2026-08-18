package ops_test

import (
	"errors"
	"testing"
	"time"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
)

// Every test in this file builds its Snapshot as a literal and calls a Plan
// function directly. No database, no transaction, no fixture. That is the
// property Stage 8c exists to establish: the interesting logic of an operation
// is a pure function, so it can be tested by reading the events it produces
// rather than by inspecting a database afterwards.

var (
	shelf domain.LocationID = 1
	desk  domain.LocationID = 2
	cable domain.HoldingID  = 10
	rice  domain.HoldingID  = 20
	item  domain.ItemID     = 100
)

var at = time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)

// snap builds a Snapshot holding exactly the given Holdings.
func snap(hs ...domain.Holding) ops.Snapshot {
	s := ops.Snapshot{
		Now:      at,
		Holdings: map[domain.HoldingID]domain.Holding{},
		Items:    map[domain.ItemID]domain.Item{item: domain.BulkItem{ItemBase: domain.ItemBase{ID: item}}},
		ByItem:   map[domain.ItemID][]domain.HoldingID{},
	}
	for _, h := range hs {
		s.Holdings[h.Base().ID] = h
		s.ByItem[h.Base().Item] = append(s.ByItem[h.Base().Item], h.Base().ID)
	}
	return s
}

func unique(custody domain.Custody) domain.UniqueHolding {
	return domain.UniqueHolding{
		HoldingBase: domain.HoldingBase{ID: cable, Item: item, StowedLocation: shelf},
		Custody:     custody,
	}
}

func bulk(milli int64) domain.BulkHolding {
	return domain.BulkHolding{
		HoldingBase: domain.HoldingBase{ID: rice, Item: item, StowedLocation: shelf},
		Quantity:    domain.FromMilli(milli),
		UnitBasis:   domain.BasisContent,
	}
}

func retired(h domain.UniqueHolding) domain.UniqueHolding {
	t := at
	h.RetiredAt = &t
	return h
}

// ---------------------------------------------------------------------------
// Custody transitions
// ---------------------------------------------------------------------------

// TestCustodyTransitions is the Unique state machine as a table: which starting
// states each operation accepts, and which it refuses.
func TestCustodyTransitions(t *testing.T) {
	plan := map[string]func(ops.Snapshot) ([]domain.Event, error){
		"checkout": func(s ops.Snapshot) ([]domain.Event, error) {
			return ops.PlanCheckOut(s, ops.CheckOutRequest{Holding: cable, To: &desk})
		},
		"return": func(s ops.Snapshot) ([]domain.Event, error) {
			return ops.PlanReturn(s, ops.ReturnRequest{Holding: cable})
		},
		"lost": func(s ops.Snapshot) ([]domain.Event, error) {
			return ops.PlanMarkLost(s, ops.MarkLostRequest{Holding: cable})
		},
		"found": func(s ops.Snapshot) ([]domain.Event, error) {
			return ops.PlanFound(s, ops.FoundRequest{Holding: cable})
		},
	}

	// want[op][custody] is the event type produced, or "" for a refusal.
	want := map[string]map[domain.Custody]string{
		"checkout": {domain.CustodyAtRest: "CheckedOut", domain.CustodyOut: "", domain.CustodyLost: ""},
		"return":   {domain.CustodyAtRest: "", domain.CustodyOut: "Returned", domain.CustodyLost: ""},
		"lost":     {domain.CustodyAtRest: "MarkedLost", domain.CustodyOut: "MarkedLost", domain.CustodyLost: ""},
		"found":    {domain.CustodyAtRest: "", domain.CustodyOut: "", domain.CustodyLost: "Found"},
	}

	for op, fn := range plan {
		for _, custody := range []domain.Custody{domain.CustodyAtRest, domain.CustodyOut, domain.CustodyLost} {
			t.Run(op+"/"+string(custody), func(t *testing.T) {
				events, err := fn(snap(unique(custody)))
				expected := want[op][custody]
				if expected == "" {
					if !errors.Is(err, ops.ErrCustody) {
						t.Errorf("planned %v (err %v) from %s; want a custody refusal", types(events), err, custody)
					}
					return
				}
				if err != nil {
					t.Fatalf("plan: %v", err)
				}
				if got := types(events); len(got) != 1 || got[0] != expected {
					t.Errorf("plan = %v, want [%s]", got, expected)
				}
			})
		}
	}
}

// TestCheckOutWithNoDestinationIsLegal: out, whereabouts unknown, is transient
// ignorance -- a real state, distinct from the conclusion MarkedLost draws.
func TestCheckOutWithNoDestinationIsLegal(t *testing.T) {
	events, err := ops.PlanCheckOut(snap(unique(domain.CustodyAtRest)),
		ops.CheckOutRequest{Holding: cable, To: nil})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if ev, ok := events[0].(domain.CheckedOut); !ok || ev.DisplacedTo != nil {
		t.Errorf("plan = %+v, want CheckedOut with no destination", events[0])
	}
}

// ---------------------------------------------------------------------------
// Verify: an observation, and the conclusion it implies
// ---------------------------------------------------------------------------

func TestVerify(t *testing.T) {
	cases := []struct {
		name    string
		custody domain.Custody
		present bool
		want    []string
	}{
		{"present as expected", domain.CustodyAtRest, true, []string{"Verified"}},
		{"absent, so concluded lost", domain.CustodyAtRest, false, []string{"Verified", "MarkedLost"}},
		{"absent and already lost", domain.CustodyLost, false, []string{"Verified"}},
		{"turned up after being lost", domain.CustodyLost, true, []string{"Verified", "Found"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events, err := ops.PlanVerify(snap(unique(tc.custody)),
				ops.VerifyRequest{Holding: cable, Present: tc.present})
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			if got := types(events); !equal(got, tc.want) {
				t.Errorf("plan = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestVerifyAlwaysRecordsTheObservationFirst: the observation and the
// conclusion are separate records, so Found can reverse the conclusion without
// erasing what prompted it.
func TestVerifyAlwaysRecordsTheObservationFirst(t *testing.T) {
	events, err := ops.PlanVerify(snap(unique(domain.CustodyAtRest)),
		ops.VerifyRequest{Holding: cable, Present: false})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if types(events)[0] != "Verified" {
		t.Errorf("plan = %v, want the observation first", types(events))
	}
}

// ---------------------------------------------------------------------------
// Placement and lifecycle
// ---------------------------------------------------------------------------

func TestRehome(t *testing.T) {
	events, err := ops.PlanRehome(snap(unique(domain.CustodyAtRest)),
		ops.RehomeRequest{Holding: cable, To: desk})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	ev, ok := events[0].(domain.Rehomed)
	if !ok {
		t.Fatalf("plan = %T, want Rehomed", events[0])
	}
	if ev.From != shelf || ev.To != desk {
		t.Errorf("Rehomed %d -> %d, want %d -> %d", ev.From, ev.To, shelf, desk)
	}
}

func TestRehomeToWhereItAlreadyLivesIsRefused(t *testing.T) {
	_, err := ops.PlanRehome(snap(unique(domain.CustodyAtRest)),
		ops.RehomeRequest{Holding: cable, To: shelf})
	if !errors.Is(err, ops.ErrInvalidRequest) {
		t.Errorf("error = %v, want ErrInvalidRequest", err)
	}
}

func TestRetire(t *testing.T) {
	events, err := ops.PlanRetire(snap(unique(domain.CustodyAtRest)),
		ops.RetireRequest{Holding: cable, Reason: "frayed"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if ev, ok := events[0].(domain.Gone); !ok || ev.Reason != "frayed" {
		t.Errorf("plan = %+v, want Gone with a reason", events[0])
	}
}

// TestOperationsRefuseARetiredHolding: Gone is the single lifecycle terminal,
// and it is terminal.
func TestOperationsRefuseARetiredHolding(t *testing.T) {
	s := snap(retired(unique(domain.CustodyAtRest)))
	cases := map[string]func() ([]domain.Event, error){
		"checkout": func() ([]domain.Event, error) {
			return ops.PlanCheckOut(s, ops.CheckOutRequest{Holding: cable})
		},
		"verify": func() ([]domain.Event, error) {
			return ops.PlanVerify(s, ops.VerifyRequest{Holding: cable, Present: true})
		},
		"rehome": func() ([]domain.Event, error) {
			return ops.PlanRehome(s, ops.RehomeRequest{Holding: cable, To: desk})
		},
		"retire": func() ([]domain.Event, error) {
			return ops.PlanRetire(s, ops.RetireRequest{Holding: cable})
		},
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := fn(); !errors.Is(err, ops.ErrRetired) {
				t.Errorf("error = %v, want ErrRetired", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Discard
// ---------------------------------------------------------------------------

func TestDiscard(t *testing.T) {
	events, err := ops.PlanDiscard(snap(bulk(800*domain.Scale)),
		ops.DiscardRequest{Holding: rice, Amount: domain.FromMilli(100 * domain.Scale), Reason: "weevils"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	ev, ok := events[0].(domain.Discarded)
	if !ok {
		t.Fatalf("plan = %T, want Discarded", events[0])
	}
	// Removals are negative deltas. A positive one would credit stock.
	if ev.Delta.Milli() != -100*domain.Scale {
		t.Errorf("delta = %s, want -100", ev.Delta)
	}
}

func TestDiscardMoreThanIsThereIsRefused(t *testing.T) {
	_, err := ops.PlanDiscard(snap(bulk(50*domain.Scale)),
		ops.DiscardRequest{Holding: rice, Amount: domain.FromMilli(100 * domain.Scale)})
	if !errors.Is(err, ops.ErrInsufficient) {
		t.Errorf("error = %v, want ErrInsufficient", err)
	}
}

func TestDiscardEverythingDoesNotRetireTheHolding(t *testing.T) {
	events, err := ops.PlanDiscard(snap(bulk(800*domain.Scale)),
		ops.DiscardRequest{Holding: rice, Amount: domain.FromMilli(800 * domain.Scale)})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	// An empty jar is still the jar you keep turmeric in. Retiring it would
	// make the next purchase create a second one.
	if got := types(events); !equal(got, []string{"Discarded"}) {
		t.Errorf("plan = %v, want [Discarded] only", got)
	}
}

func TestDiscardRejectsANonPositiveAmount(t *testing.T) {
	for _, amount := range []int64{0, -100} {
		_, err := ops.PlanDiscard(snap(bulk(800*domain.Scale)),
			ops.DiscardRequest{Holding: rice, Amount: domain.FromMilli(amount)})
		if !errors.Is(err, ops.ErrInvalidRequest) {
			t.Errorf("amount %d: error = %v, want ErrInvalidRequest", amount, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Kind and presence
// ---------------------------------------------------------------------------

func TestOperationsRefuseTheWrongKind(t *testing.T) {
	s := snap(unique(domain.CustodyAtRest), bulk(800*domain.Scale))

	if _, err := ops.PlanDiscard(s, ops.DiscardRequest{
		Holding: cable, Amount: domain.FromMilli(1),
	}); !errors.Is(err, ops.ErrWrongKind) {
		t.Errorf("discard on Unique: error = %v, want ErrWrongKind", err)
	}
	if _, err := ops.PlanCheckOut(s, ops.CheckOutRequest{Holding: rice}); !errors.Is(err, ops.ErrWrongKind) {
		t.Errorf("checkout on Bulk: error = %v, want ErrWrongKind", err)
	}
}

// TestPlanningRefusesAHoldingNotInTheSnapshot: a zero value would plan as a
// real thing at location zero, which is exactly the silent nonsense the
// accessors exist to prevent.
func TestPlanningRefusesAHoldingNotInTheSnapshot(t *testing.T) {
	_, err := ops.PlanRetire(snap(), ops.RetireRequest{Holding: cable})
	if !errors.Is(err, ops.ErrSubjectMissing) {
		t.Errorf("error = %v, want ErrSubjectMissing", err)
	}
}

// TestEveryPlannedEventCarriesTheSnapshotClock: OccurredAt must be non-zero or
// the ledger refuses it -- the fix for the divergence found in Stage 6, where
// Apply and Replay folded over different inputs.
func TestEveryPlannedEventCarriesTheSnapshotClock(t *testing.T) {
	s := snap(unique(domain.CustodyAtRest))
	events, err := ops.PlanVerify(s, ops.VerifyRequest{Holding: cable, Present: false})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for i, e := range events {
		if got := e.Base().OccurredAt; !got.Equal(at) {
			t.Errorf("event %d (%s) OccurredAt = %v, want the snapshot clock %v", i, e.Type(), got, at)
		}
	}
}
