package ops_test

import (
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/ops"
)

// The pure tests in plan_holding_test.go assert what the Plan functions
// produce. They cannot assert that the ledger ACCEPTS it -- a plan can be
// perfectly self-consistent and still describe a transition the fold refuses,
// or fold to a state the projection does not match.
//
// This file is the bridge. Every thin operation runs against a real database,
// and every one is followed by VerifyAll: replay the Holding from its events
// and compare with the stored projection. If a planned event folds differently
// from the way it was written, H10 says so here rather than in the UI.

func (tr *tree) unique(t *testing.T, at domain.LocationID) domain.HoldingID {
	t.Helper()
	return tr.uniqueOf(t, tr.cableItem, at)
}

func (tr *tree) uniqueOf(t *testing.T, item domain.ItemID, at domain.LocationID) domain.HoldingID {
	t.Helper()
	id, err := tr.led.CreateUniqueHolding(tr.ctx, ledger.CreateUniqueHoldingInput{
		Item: item, Location: at, Label: "black, 2m",
	})
	if err != nil {
		t.Fatalf("create unique holding: %v", err)
	}
	return id
}

// verifyClean is the postcondition of every operation, asserted every time
// rather than where it seemed likely to matter.
func (tr *tree) verifyClean(t *testing.T) {
	t.Helper()
	report, err := tr.led.VerifyAll(tr.ctx)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !report.Clean() {
		t.Errorf("ledger disagrees with the projection: %+v", report)
	}
}

func (tr *tree) custody(t *testing.T, id domain.HoldingID) domain.Custody {
	t.Helper()
	d, err := tr.r.Holding(tr.ctx, id)
	if err != nil {
		t.Fatalf("read holding: %v", err)
	}
	u, ok := d.Holding.(domain.UniqueHolding)
	if !ok {
		t.Fatalf("holding %d is %s, not Unique", id, d.Holding.Kind())
	}
	return u.Custody
}

// TestCustodyRoundTripThroughTheLedger walks the whole Unique state machine
// against a real database, checking H10 after every step.
func TestCustodyRoundTripThroughTheLedger(t *testing.T) {
	tr := newTree(t)
	cable := tr.unique(t, tr.shelf1)

	steps := []struct {
		name string
		plan func() (ops.Batch, error)
		want domain.Custody
	}{
		{"check out", func() (ops.Batch, error) {
			return tr.pl.CheckOut(tr.ctx, ops.CheckOutRequest{Holding: cable, To: &tr.garage})
		}, domain.CustodyOut},
		{"return", func() (ops.Batch, error) {
			return tr.pl.Return(tr.ctx, ops.ReturnRequest{Holding: cable})
		}, domain.CustodyAtRest},
		{"mark lost", func() (ops.Batch, error) {
			return tr.pl.MarkLost(tr.ctx, ops.MarkLostRequest{Holding: cable})
		}, domain.CustodyLost},
		{"found", func() (ops.Batch, error) {
			return tr.pl.Found(tr.ctx, ops.FoundRequest{Holding: cable})
		}, domain.CustodyAtRest},
	}

	for _, step := range steps {
		batch, err := step.plan()
		if err != nil {
			t.Fatalf("%s: plan: %v", step.name, err)
		}
		if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
			t.Fatalf("%s: execute: %v", step.name, err)
		}
		if got := tr.custody(t, cable); got != step.want {
			t.Fatalf("after %s custody = %s, want %s", step.name, got, step.want)
		}
		tr.verifyClean(t)
	}
}

// TestVerifyAbsentConcludesLostThroughTheLedger: two events, one operation,
// and the second must actually take effect.
func TestVerifyAbsentConcludesLostThroughTheLedger(t *testing.T) {
	tr := newTree(t)
	cable := tr.unique(t, tr.shelf1)

	batch, err := tr.pl.Verify(tr.ctx, ops.VerifyRequest{Holding: cable, Present: false})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	res, err := tr.ex.Execute(tr.ctx, batch)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := len(res.Events[0]); got != 2 {
		t.Errorf("recorded %d events, want 2 (the observation and the conclusion)", got)
	}
	if got := tr.custody(t, cable); got != domain.CustodyLost {
		t.Errorf("custody = %s, want Lost", got)
	}
	tr.verifyClean(t)
}

// TestCheckOutWithNoDestinationThroughTheLedger: the nil DisplacedTo has to
// survive persistence, since it is what makes the holding derived-Missing.
func TestCheckOutWithNoDestinationThroughTheLedger(t *testing.T) {
	tr := newTree(t)
	cable := tr.unique(t, tr.shelf1)

	batch, err := tr.pl.CheckOut(tr.ctx, ops.CheckOutRequest{Holding: cable, To: nil})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
		t.Fatalf("execute: %v", err)
	}

	d, err := tr.r.Holding(tr.ctx, cable)
	if err != nil {
		t.Fatalf("read holding: %v", err)
	}
	u := d.Holding.(domain.UniqueHolding)
	if !u.IsMissing() {
		t.Errorf("holding is not derived-Missing: custody=%s displacedTo=%v", u.Custody, u.DisplacedTo)
	}
	tr.verifyClean(t)
}

func TestRehomeAndRetireThroughTheLedger(t *testing.T) {
	tr := newTree(t)
	cable := tr.unique(t, tr.shelf1)

	batch, err := tr.pl.Rehome(tr.ctx, ops.RehomeRequest{Holding: cable, To: tr.garage})
	if err != nil {
		t.Fatalf("plan rehome: %v", err)
	}
	if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
		t.Fatalf("execute rehome: %v", err)
	}
	d, err := tr.r.Holding(tr.ctx, cable)
	if err != nil {
		t.Fatalf("read holding: %v", err)
	}
	if got := d.Holding.Base().StowedLocation; got != tr.garage {
		t.Errorf("stowed at %d, want Garage (%d)", got, tr.garage)
	}
	tr.verifyClean(t)

	batch, err = tr.pl.Retire(tr.ctx, ops.RetireRequest{Holding: cable, Reason: "frayed"})
	if err != nil {
		t.Fatalf("plan retire: %v", err)
	}
	if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
		t.Fatalf("execute retire: %v", err)
	}
	tr.verifyClean(t)

	// And the terminal is terminal: planning against it now refuses, using the
	// state the database reports rather than a hand-built snapshot.
	if _, err := tr.pl.Rehome(tr.ctx, ops.RehomeRequest{Holding: cable, To: tr.shelf1}); err == nil {
		t.Error("rehomed a retired holding")
	}
}

func TestDiscardThroughTheLedger(t *testing.T) {
	tr := newTree(t)
	rice := tr.stow(t, tr.pantry)

	// Put something there to discard.
	if _, err := tr.led.Apply(tr.ctx, domain.Acquired{
		EventBase: domain.EventBase{OccurredAt: clock},
		Holding:   rice, Delta: domain.FromMilli(800 * domain.Scale),
	}); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	batch, err := tr.pl.Discard(tr.ctx, ops.DiscardRequest{
		Holding: rice, Amount: domain.FromMilli(100 * domain.Scale), Reason: "weevils",
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
		t.Fatalf("execute: %v", err)
	}

	d, err := tr.r.Holding(tr.ctx, rice)
	if err != nil {
		t.Fatalf("read holding: %v", err)
	}
	b := d.Holding.(domain.BulkHolding)
	if got := b.Quantity.Milli(); got != 700*domain.Scale {
		t.Errorf("quantity = %d milli, want %d", got, 700*domain.Scale)
	}
	tr.verifyClean(t)
}

// TestPlanningReadsTheDatabaseState: the snapshot is gathered from storage, so
// a refusal reflects what is actually there rather than what the caller
// believed. Checking out something already out must fail without the caller
// having to know it was out.
func TestPlanningReadsTheDatabaseState(t *testing.T) {
	tr := newTree(t)
	cable := tr.unique(t, tr.shelf1)

	batch, err := tr.pl.CheckOut(tr.ctx, ops.CheckOutRequest{Holding: cable, To: &tr.garage})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if _, err := tr.pl.CheckOut(tr.ctx, ops.CheckOutRequest{Holding: cable, To: &tr.garage}); err == nil {
		t.Error("checked out a holding that was already out")
	}
}
