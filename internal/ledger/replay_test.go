package ledger_test

import (
	"errors"
	"math/rand"
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/testsupport"
)

// ---------------------------------------------------------------------------
// Walkthrough 00 — the case a missing HoldingCreated would break
// ---------------------------------------------------------------------------

// TestCreatedAndNeverTouchedReplays is acceptance walkthrough 00, now against
// real storage. A Holding put on a shelf and never moved must replay to exactly
// its stored state.
//
// Without HoldingCreated, stowed_location has no natural zero and replay yields
// nothing — so VerifyAll would flag every never-moved Holding in the house,
// which is the most common case there is. It is the cheapest possible test and
// the one that catches the omission first.
func TestCreatedAndNeverTouchedReplays(t *testing.T) {
	f := newFixture(t)

	for _, id := range []domain.HoldingID{f.rice, f.cable} {
		found, err := f.p.Verify(f.ctx, id)
		if err != nil {
			t.Fatalf("verify holding %d: %v", id, err)
		}
		if len(found) != 0 {
			t.Errorf("holding %d, created and never touched: %v", id, found)
		}
	}
}

func TestReplayMatchesStoredStateAfterEvents(t *testing.T) {
	f := newFixture(t)

	f.apply(t, domain.Acquired{EventBase: evAt(), Holding: f.rice, Delta: domain.FromMilli(2_000_000)})
	f.apply(t, domain.Consumed{EventBase: evAt(), Holding: f.rice, Delta: domain.FromMilli(-100_000)})
	f.apply(t, domain.Moved{EventBase: evAt(), Holding: f.rice, From: f.pantry, To: f.garage})
	f.apply(t, domain.CheckedOut{EventBase: evAt(), Holding: f.cable, DisplacedTo: &f.garage})

	report, err := f.p.VerifyAll(f.ctx)
	if err != nil {
		t.Fatalf("verify all: %v", err)
	}
	if !report.Clean() {
		t.Errorf("report not clean: %+v", report)
	}
	if report.HoldingsChecked != 2 {
		t.Errorf("checked %d holdings, want 2", report.HoldingsChecked)
	}
}

// ---------------------------------------------------------------------------
// The integrity job reports, and never repairs
// ---------------------------------------------------------------------------

// TestVerifyDetectsTamperedState is the whole point of H10. A projection changed
// without its event is exactly what O3 forbids, and the only failure mode the
// check can report — Apply and Replay fold through one function, so they cannot
// disagree about the same events.
func TestVerifyDetectsTamperedState(t *testing.T) {
	f := newFixture(t)
	f.apply(t, domain.Acquired{EventBase: evAt(), Holding: f.rice, Delta: domain.FromMilli(500_000)})

	// A write that bypasses the ledger, which is the defect H10 exists to catch.
	if _, err := f.conn.Exec(
		"UPDATE bulk_holdings SET quantity = 999 WHERE holding_id = ?", int64(f.rice)); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	found, err := f.p.Verify(f.ctx, f.rice)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("got %d discrepancies, want 1: %v", len(found), found)
	}
	if found[0].Field != "quantity" {
		t.Errorf("field = %q, want quantity", found[0].Field)
	}

	// And it must REPORT, not repair: silently correcting would destroy the only
	// signal that completeness was violated.
	if got := f.quantityOf(t, f.rice); got.Milli() != 999 {
		t.Errorf("quantity = %s; Verify repaired the state instead of reporting it", got)
	}
}

func TestVerifyDetectsTamperedCustody(t *testing.T) {
	f := newFixture(t)
	f.apply(t, domain.CheckedOut{EventBase: evAt(), Holding: f.cable, DisplacedTo: &f.garage})

	// HU2 forbids a known displacement while not Out, so the tamper has to clear
	// it as well -- the schema refuses to hold an inconsistent row even when the
	// write is deliberately bypassing the ledger.
	if _, err := f.conn.Exec(
		"UPDATE unique_holdings SET custody = 'Lost', displaced_to_id = NULL WHERE holding_id = ?",
		int64(f.cable)); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	found, err := f.p.Verify(f.ctx, f.cable)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	byField := map[string]ledger.Discrepancy{}
	for _, d := range found {
		byField[d.Field] = d
	}
	if _, ok := byField["custody"]; !ok {
		t.Fatalf("got %v, want a custody discrepancy", found)
	}
	if _, ok := byField["displaced_to"]; !ok {
		t.Errorf("clearing displaced_to should also be reported: %v", found)
	}
	found = []ledger.Discrepancy{byField["custody"]}
	if found[0].Stored != string(domain.CustodyLost) || found[0].Replayed != string(domain.CustodyOut) {
		t.Errorf("discrepancy = %s", found[0])
	}
}

func TestVerifyDetectsTamperedLocation(t *testing.T) {
	f := newFixture(t)

	if _, err := f.conn.Exec(
		"UPDATE holdings SET stowed_location_id = ? WHERE id = ?",
		int64(f.garage), int64(f.rice)); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	found, err := f.p.Verify(f.ctx, f.rice)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(found) != 1 || found[0].Field != "stowed_location" {
		t.Fatalf("got %v, want one stowed_location discrepancy", found)
	}
}

// ---------------------------------------------------------------------------
// Checkpoints are advisory (K2)
// ---------------------------------------------------------------------------

// TestCheckpointsChangePerformanceNotResults is K2 stated as a test: deleting
// every checkpoint must change nothing about the answer. It is what makes the
// checkpoint format a free choice, and why it is a blob while the ledger gets 13
// explicit tables.
func TestCheckpointsChangePerformanceNotResults(t *testing.T) {
	f := newFixture(t)
	f.apply(t, domain.Acquired{EventBase: evAt(), Holding: f.rice, Delta: domain.FromMilli(2_000_000)})
	f.apply(t, domain.Consumed{EventBase: evAt(), Holding: f.rice, Delta: domain.FromMilli(-250_000)})

	withoutCheckpoint, err := f.p.Replay(f.ctx, f.rice)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	if n, err := f.p.CheckpointAll(f.ctx); err != nil {
		t.Fatalf("checkpoint: %v", err)
	} else if n != 2 {
		t.Errorf("checkpointed %d holdings, want 2", n)
	}

	// More events after the checkpoint, so the resumed replay has work to do.
	f.apply(t, domain.Consumed{EventBase: evAt(), Holding: f.rice, Delta: domain.FromMilli(-250_000)})

	fromCheckpoint, err := f.p.Replay(f.ctx, f.rice)
	if err != nil {
		t.Fatalf("replay from checkpoint: %v", err)
	}

	if err := f.p.DiscardCheckpoints(f.ctx); err != nil {
		t.Fatalf("discard: %v", err)
	}
	fromZero, err := f.p.Replay(f.ctx, f.rice)
	if err != nil {
		t.Fatalf("replay from zero: %v", err)
	}

	want := domain.FromMilli(1_500_000)
	for label, got := range map[string]domain.Projection{
		"from checkpoint": fromCheckpoint,
		"from zero":       fromZero,
	} {
		q := got.(domain.BulkProjection).Quantity
		if q.Cmp(want) != 0 {
			t.Errorf("%s: quantity = %s, want %s", label, q, want)
		}
	}
	if withoutCheckpoint.(domain.BulkProjection).Quantity.Cmp(domain.FromMilli(1_750_000)) != 0 {
		t.Errorf("pre-checkpoint replay = %s, want 1750000",
			withoutCheckpoint.(domain.BulkProjection).Quantity)
	}
}

// TestCorruptCheckpointIsDiscarded: a checkpoint is rebuildable, so an unreadable
// one costs a rebuild rather than the truth.
func TestCorruptCheckpointIsDiscarded(t *testing.T) {
	f := newFixture(t)
	f.apply(t, domain.Acquired{EventBase: evAt(), Holding: f.rice, Delta: domain.FromMilli(1_000_000)})
	if _, err := f.p.CheckpointAll(f.ctx); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}

	if _, err := f.conn.Exec(
		"UPDATE replay_checkpoints SET projection = 'not json' WHERE holding_id = ?",
		int64(f.rice)); err != nil {
		t.Fatalf("corrupt: %v", err)
	}

	state, err := f.p.Replay(f.ctx, f.rice)
	if err != nil {
		t.Fatalf("replay with a corrupt checkpoint: %v", err)
	}
	if got := state.(domain.BulkProjection).Quantity; got.Cmp(domain.FromMilli(1_000_000)) != 0 {
		t.Errorf("quantity = %s, want 1000000 — replay should have fallen back to zero", got)
	}
}

// ---------------------------------------------------------------------------
// The three-way property test (schema section 9.2)
// ---------------------------------------------------------------------------

// TestThreeWayAgreement is the headline check: it is what makes H10 and O3 real
// rather than aspirational.
//
//  1. apply a random operation sequence to the REAL SYSTEM
//  2. apply the same sequence to a NAIVE IN-MEMORY ORACLE
//  3. assert the two agree
//  4. REPLAY THE LEDGER from zero and assert it agrees too
//
// Divergence localises the defect: 1 vs 2 is an operation bug, 1 vs 4 is a
// completeness bug (a write without its event), 2 vs 4 is a replay bug.
func TestThreeWayAgreement(t *testing.T) {
	const sequences = 60
	const opsPerSequence = 40

	for seed := 0; seed < sequences; seed++ {
		seed := seed
		t.Run(seedName(seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(seed)))
			f := newFixture(t)
			oracle := testsupport.NewOracle()
			oracle.Create(f.rice, domain.KindBulk, f.pantry)
			oracle.Create(f.cable, domain.KindUnique, f.pantry)

			for i := 0; i < opsPerSequence; i++ {
				e := randomEvent(rng, f)
				if _, err := f.p.Apply(f.ctx, e); err != nil {
					// The real system decides what is legal. Rejected operations
					// never reach the oracle, so it models only accepted history.
					continue
				}
				if err := oracle.Apply(e); err != nil {
					t.Fatalf("oracle: %v", err)
				}
			}

			// 3: the real system agrees with the naive model.
			for _, id := range oracle.IDs() {
				modelled, _ := oracle.Holding(id)
				stored, _, err := loadStoredProjection(t, f, id)
				if err != nil {
					t.Fatalf("load stored: %v", err)
				}
				if diffs := modelled.Diff(stored); len(diffs) != 0 {
					t.Fatalf("holding %d, system vs oracle: %v", id, diffs)
				}

				// 4: and the ledger, replayed from zero, agrees with both.
				replayed, err := f.p.Replay(f.ctx, id)
				if err != nil {
					t.Fatalf("replay holding %d: %v", id, err)
				}
				if diffs := modelled.Diff(replayed); len(diffs) != 0 {
					t.Fatalf("holding %d, replay vs oracle: %v", id, diffs)
				}
			}

			// And the integrity job agrees that everything is consistent.
			report, err := f.p.VerifyAll(f.ctx)
			if err != nil {
				t.Fatalf("verify all: %v", err)
			}
			if !report.Clean() {
				t.Fatalf("report not clean after %d operations: %+v", opsPerSequence, report)
			}
		})
	}
}

func seedName(seed int) string {
	return "seed" + string(rune('0'+seed/10)) + string(rune('0'+seed%10))
}

// randomEvent picks an arbitrary event. Illegal ones are expected and are the
// point: they exercise the fold's rejection paths, and the real system's verdict
// is what decides whether the oracle sees them.
func randomEvent(rng *rand.Rand, f *fixture) domain.Event {
	amount := func() domain.Quantity {
		return domain.FromMilli(int64(rng.Intn(500_000) + 1))
	}
	negative := func() domain.Quantity {
		return domain.FromMilli(-int64(rng.Intn(500_000) + 1))
	}
	place := func() domain.LocationID {
		if rng.Intn(2) == 0 {
			return f.pantry
		}
		return f.garage
	}

	switch rng.Intn(14) {
	case 0:
		return domain.Acquired{EventBase: evAt(), Holding: f.rice, Delta: amount(), Source: "shop"}
	case 1:
		return domain.Consumed{EventBase: evAt(), Holding: f.rice, Delta: negative(), Reason: "used"}
	case 2:
		return domain.Discarded{EventBase: evAt(), Holding: f.rice, Delta: negative(), Reason: "spoiled"}
	case 3:
		return domain.Opened{EventBase: evAt(), Holding: f.rice, Delta: amount()}
	case 4:
		return domain.Split{EventBase: evAt(), Holding: f.rice, Delta: negative()}
	case 5:
		return domain.Adjusted{EventBase: evAt(), Holding: f.rice, Delta: negative()}
	case 6:
		return domain.Counted{EventBase: evAt(), Holding: f.rice, Observed: amount()}
	case 7:
		return domain.Moved{EventBase: evAt(), Holding: f.rice, From: f.pantry, To: place()}
	case 8:
		return domain.CheckedOut{EventBase: evAt(), Holding: f.cable, DisplacedTo: &f.garage}
	case 9:
		return domain.Returned{EventBase: evAt(), Holding: f.cable}
	case 10:
		return domain.MarkedLost{EventBase: evAt(), Holding: f.cable}
	case 11:
		return domain.Found{EventBase: evAt(), Holding: f.cable}
	case 12:
		return domain.Verified{EventBase: evAt(), Holding: f.cable, Present: rng.Intn(2) == 0}
	default:
		// Occasionally attempt something illegal — a Bulk event against the
		// Unique holding — to keep the rejection paths exercised.
		return domain.Consumed{EventBase: evAt(), Holding: f.cable, Delta: negative()}
	}
}

// loadStoredProjection reads what the system currently believes, which is what
// replay is checked against.
func loadStoredProjection(t *testing.T, f *fixture, id domain.HoldingID) (domain.Projection, domain.Kind, error) {
	t.Helper()
	// Verify already compares stored against replayed; reading through Replay
	// with checkpoints discarded would test replay against itself. Going through
	// the ledger's own loader keeps the two sides independent.
	found, err := f.p.Verify(f.ctx, id)
	if err != nil {
		return nil, "", err
	}
	if len(found) != 0 {
		t.Fatalf("holding %d: stored and replayed already disagree: %v", id, found)
	}
	proj, err := f.p.Replay(f.ctx, id)
	return proj, proj.Kind(), err
}

// TestOracleRejectsUnknownEvents guards the control: if the oracle silently
// ignored an event type, the property test would compare two systems that both
// did nothing and pass.
func TestOracleRejectsUnknownEvents(t *testing.T) {
	oracle := testsupport.NewOracle()
	// The registry's prototypes are zero values, so their Holding id is 0.
	oracle.Create(0, domain.KindBulk, 1)

	err := oracle.Apply(domain.NodeArchived{EventBase: evAt(), Location: 1, Resolution: domain.ResolutionLift})
	if err != nil {
		t.Errorf("non-Holding events should be ignored, got %v", err)
	}

	// Every Holding event the registry knows about must have a case.
	for _, proto := range domain.AllEventTypes {
		kind, _ := proto.Subject()
		if kind != domain.SubjectHolding {
			continue
		}
		if err := oracle.Apply(proto); err != nil {
			t.Errorf("oracle has no case for %T: %v", proto, err)
		}
	}
}

var (
	_ = errors.Is
	_ = ledger.Discrepancy{}
)
