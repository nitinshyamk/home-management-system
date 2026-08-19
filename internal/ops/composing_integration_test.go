package ops_test

import (
	"testing"
	"time"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
)

// The composing operations against a real database. This is where a plan that
// is internally coherent but wrong about the domain shows up: the fold rejects
// it, the projection disagrees with a replay, or the quantities land somewhere
// other than where the plan said.

// onHandAt reports the quantity of one basis at one location, or -1 if there is
// no such Holding.
func (tr *tree) onHandAt(t *testing.T, item domain.ItemID, at domain.LocationID, basis domain.UnitBasis) int64 {
	t.Helper()
	details, err := tr.r.HoldingsOfItem(tr.ctx, item)
	if err != nil {
		t.Fatalf("holdings of item: %v", err)
	}
	for _, d := range details {
		b, ok := d.Holding.(domain.BulkHolding)
		if ok && b.StowedLocation == at && b.UnitBasis == basis {
			return b.Quantity.Milli()
		}
	}
	return -1
}

// apply plans, executes, and checks H10 -- the postcondition every operation
// must leave true, asserted every time rather than where it seemed likely to
// matter. It takes (Batch, error) so a planner call can be passed whole.
func (tr *tree) apply(b ops.Batch, err error) {
	tr.t.Helper()
	if err != nil {
		tr.t.Fatalf("plan: %v", err)
	}
	if _, err := tr.ex.Execute(tr.ctx, b); err != nil {
		tr.t.Fatalf("execute: %v", err)
	}
	tr.verifyClean(tr.t)
}

// TestBuyThenCookThroughTheLedger is the walkthrough end to end: buy two sealed
// bags, then use 100 g, and check the quantities land where the plan said.
func TestBuyThenCookThroughTheLedger(t *testing.T) {
	tr := newTree(t)

	tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
		Item: tr.item, Location: tr.pantry, Amount: domain.FromMilli(2 * domain.Scale),
		Basis: domain.BasisPackage, Source: "corner shop",
	}))
	if got := tr.onHandAt(t, tr.item, tr.pantry, domain.BasisPackage); got != 2*domain.Scale {
		t.Fatalf("packages = %d milli, want %d", got, 2*domain.Scale)
	}

	tr.apply(tr.pl.Consume(tr.ctx, ops.ConsumeRequest{
		Item: tr.item, Location: tr.pantry, Amount: domain.FromMilli(100 * domain.Scale),
		Reason: "dinner",
	}))

	// One bag broken open: one package left, and 2000 - 100 g of contents.
	if got := tr.onHandAt(t, tr.item, tr.pantry, domain.BasisPackage); got != 1*domain.Scale {
		t.Errorf("packages = %d milli, want %d", got, 1*domain.Scale)
	}
	if got := tr.onHandAt(t, tr.item, tr.pantry, domain.BasisContent); got != 1900*domain.Scale {
		t.Errorf("contents = %d milli, want %d", got, 1900*domain.Scale)
	}
}

// TestReceiveTwiceDoesNotDuplicate is O1 through the database: the H8 key is
// upheld by the planning, so a second purchase must find the first Holding.
func TestReceiveTwiceDoesNotDuplicate(t *testing.T) {
	tr := newTree(t)

	for i := 0; i < 3; i++ {
		tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
			Item: tr.item, Location: tr.pantry, Amount: domain.FromMilli(2 * domain.Scale),
			Basis: domain.BasisPackage,
		}))
	}

	details, err := tr.r.HoldingsOfItem(tr.ctx, tr.item)
	if err != nil {
		t.Fatalf("holdings: %v", err)
	}
	if len(details) != 1 {
		t.Fatalf("%d holdings after three purchases, want 1 -- H8 was violated", len(details))
	}
	if got := tr.onHandAt(t, tr.item, tr.pantry, domain.BasisPackage); got != 6*domain.Scale {
		t.Errorf("packages = %d milli, want %d", got, 6*domain.Scale)
	}
}

// TestConsumeAcrossTwoPackagesThroughTheLedger: the second Split must debit a
// package the first Split already spent, which only works because the plan
// tracks its own changes rather than re-reading a stale snapshot.
func TestConsumeAcrossTwoPackagesThroughTheLedger(t *testing.T) {
	tr := newTree(t)
	tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
		Item: tr.item, Location: tr.pantry, Amount: domain.FromMilli(3 * domain.Scale),
		Basis: domain.BasisPackage,
	}))

	tr.apply(tr.pl.Consume(tr.ctx, ops.ConsumeRequest{
		Item: tr.item, Location: tr.pantry, Amount: domain.FromMilli(3000 * domain.Scale),
	}))

	if got := tr.onHandAt(t, tr.item, tr.pantry, domain.BasisPackage); got != 1*domain.Scale {
		t.Errorf("packages = %d milli, want %d", got, 1*domain.Scale)
	}
	if got := tr.onHandAt(t, tr.item, tr.pantry, domain.BasisContent); got != 1000*domain.Scale {
		t.Errorf("contents = %d milli, want %d (4000 opened, 3000 used)", got, 1000*domain.Scale)
	}
}

// TestMoveOntoAnOccupiedSlotThroughTheLedger: after merging there must still be
// exactly one Holding per slot, and the totals must be preserved.
func TestMoveOntoAnOccupiedSlotThroughTheLedger(t *testing.T) {
	tr := newTree(t)

	for _, at := range []domain.LocationID{tr.pantry, tr.garage} {
		tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
			Item: tr.item, Location: at, Amount: domain.FromMilli(2 * domain.Scale),
			Basis: domain.BasisPackage,
		}))
	}

	details, _ := tr.r.HoldingsOfItem(tr.ctx, tr.item)
	var fromPantry domain.HoldingID
	for _, d := range details {
		if d.Holding.Base().StowedLocation == tr.pantry {
			fromPantry = d.Holding.Base().ID
		}
	}

	tr.apply(tr.pl.Move(tr.ctx, ops.MoveRequest{Holding: fromPantry, To: tr.garage}))

	if got := tr.onHandAt(t, tr.item, tr.garage, domain.BasisPackage); got != 4*domain.Scale {
		t.Errorf("garage = %d milli, want %d", got, 4*domain.Scale)
	}
	// The emptied Holding stays where it was: an empty jar is still the jar.
	if got := tr.onHandAt(t, tr.item, tr.pantry, domain.BasisPackage); got != 0 {
		t.Errorf("pantry = %d milli, want 0", got)
	}
}

func TestMoveToAFreeSlotThroughTheLedger(t *testing.T) {
	tr := newTree(t)
	tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
		Item: tr.item, Location: tr.pantry, Amount: domain.FromMilli(2 * domain.Scale),
		Basis: domain.BasisPackage,
	}))
	details, _ := tr.r.HoldingsOfItem(tr.ctx, tr.item)
	id := details[0].Holding.Base().ID

	tr.apply(tr.pl.Move(tr.ctx, ops.MoveRequest{Holding: id, To: tr.garage}))

	if len(details) != 1 {
		t.Fatal("fixture assumption broken")
	}
	after, _ := tr.r.HoldingsOfItem(tr.ctx, tr.item)
	if len(after) != 1 {
		t.Errorf("%d holdings after relocating, want 1 -- the identity should be preserved", len(after))
	}
	if after[0].Holding.Base().ID != id {
		t.Errorf("holding id changed from %d to %d; a relocation is not a new Holding",
			id, after[0].Holding.Base().ID)
	}
}

// TestCountThroughTheLedger: the correction lands, and the observation survives
// in the history alongside it.
func TestCountThroughTheLedger(t *testing.T) {
	tr := newTree(t)
	rice := tr.stow(t, tr.pantry)
	if _, err := tr.led.Apply(tr.ctx, domain.Acquired{
		EventBase: domain.EventBase{OccurredAt: clock},
		Holding:   rice, Delta: domain.FromMilli(800 * domain.Scale),
	}); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	tr.apply(tr.pl.Count(tr.ctx, ops.CountRequest{
		Holding: rice, Observed: domain.FromMilli(750 * domain.Scale),
	}))

	d, _ := tr.r.Holding(tr.ctx, rice)
	if got := d.Holding.(domain.BulkHolding).Quantity.Milli(); got != 750*domain.Scale {
		t.Errorf("quantity = %d milli, want %d", got, 750*domain.Scale)
	}

	history, err := tr.led.History(tr.ctx, domain.SubjectHolding, int64(rice))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	var sawCount, sawAdjust bool
	for _, e := range history {
		switch e.Type() {
		case domain.TypeCounted:
			sawCount = true
		case domain.TypeAdjusted:
			sawAdjust = true
		}
	}
	if !sawCount || !sawAdjust {
		t.Errorf("history has Counted=%v Adjusted=%v; both must survive", sawCount, sawAdjust)
	}
}

// TestOpenThroughTheLedger exercises the explicit form of what Consume does
// implicitly.
func TestOpenThroughTheLedger(t *testing.T) {
	tr := newTree(t)
	tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
		Item: tr.item, Location: tr.pantry, Amount: domain.FromMilli(2 * domain.Scale),
		Basis: domain.BasisPackage,
	}))

	tr.apply(tr.pl.Open(tr.ctx, ops.OpenRequest{Item: tr.item, Location: tr.pantry}))

	if got := tr.onHandAt(t, tr.item, tr.pantry, domain.BasisPackage); got != 1*domain.Scale {
		t.Errorf("packages = %d milli, want %d", got, 1*domain.Scale)
	}
	if got := tr.onHandAt(t, tr.item, tr.pantry, domain.BasisContent); got != 2000*domain.Scale {
		t.Errorf("contents = %d milli, want %d", got, 2000*domain.Scale)
	}
}

// TestOpenedDeltaIsResolvedNotSymbolic is E6: the event carries +2000 g, not
// "one package's worth", so changing the package size later cannot rewrite what
// this event meant.
func TestOpenedDeltaIsResolvedNotSymbolic(t *testing.T) {
	tr := newTree(t)
	tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
		Item: tr.item, Location: tr.pantry, Amount: domain.FromMilli(2 * domain.Scale),
		Basis: domain.BasisPackage,
	}))
	tr.apply(tr.pl.Open(tr.ctx, ops.OpenRequest{Item: tr.item, Location: tr.pantry}))

	details, _ := tr.r.HoldingsOfItem(tr.ctx, tr.item)
	var contents domain.HoldingID
	for _, d := range details {
		if b, ok := d.Holding.(domain.BulkHolding); ok && b.UnitBasis == domain.BasisContent {
			contents = b.ID
		}
	}
	history, err := tr.led.History(tr.ctx, domain.SubjectHolding, int64(contents))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	for _, e := range history {
		if ev, ok := e.(domain.Opened); ok {
			if ev.Delta.Milli() != 2000*domain.Scale {
				t.Errorf("Opened delta = %s, want 2000 g resolved at the time of opening", ev.Delta)
			}
			return
		}
	}
	t.Error("no Opened event in the content holding's history")
}

// TestComposingOperationsAreAtomic: a failure anywhere leaves the whole thing
// undone, including the Holding the operation was going to create.
func TestComposingOperationsAreAtomic(t *testing.T) {
	tr := newTree(t)
	tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
		Item: tr.item, Location: tr.pantry, Amount: domain.FromMilli(2 * domain.Scale),
		Basis: domain.BasisPackage,
	}))
	before, _ := tr.r.HoldingsOfItem(tr.ctx, tr.item)

	batch, err := tr.pl.Consume(tr.ctx, ops.ConsumeRequest{
		Item: tr.item, Location: tr.pantry, Amount: domain.FromMilli(100 * domain.Scale),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	inner := batch.Steps[0].Records
	batch.Steps[0].Records = func(c ops.Created) ([]domain.Event, error) {
		events, err := inner(c)
		if err != nil {
			return nil, err
		}
		return append(events, domain.Consumed{
			EventBase: domain.EventBase{OccurredAt: clock},
			Holding:   domain.HoldingID(999_999), Delta: domain.FromMilli(-1),
		}), nil
	}
	if _, err := tr.ex.Execute(tr.ctx, batch); err == nil {
		t.Fatal("execute succeeded against a nonexistent holding")
	}

	after, _ := tr.r.HoldingsOfItem(tr.ctx, tr.item)
	if len(after) != len(before) {
		t.Errorf("%d holdings after a failed consume, want %d -- the opened holding escaped",
			len(after), len(before))
	}
	if got := tr.onHandAt(t, tr.item, tr.pantry, domain.BasisPackage); got != 2*domain.Scale {
		t.Errorf("packages = %d milli, want %d -- the Split escaped", got, 2*domain.Scale)
	}
	tr.verifyClean(t)
}

// TestExpiryIsPartOfAHoldingsIdentity is the regression for a bug that was
// invisible from every layer above it.
//
// ExpiresOn was threaded from the request through the plan, through the
// origination, into ledger.CreateBulkHoldingInput -- and then dropped, because
// the INSERT never named the column. Every layer above believed it worked. The
// consequence is an H8 violation rather than a missing date: receiving stock
// with a printed expiry looks for a Holding on that expiry, is correctly told
// there is none, and creates one -- which is then stored with a null expiry,
// where the next receipt for the same date will fail to find it and create
// another. Two active Holdings on one H8 key, from a plan layer that did
// everything right.
//
// So the assertion is about identity, not about the date: same expiry merges,
// different expiry does not.
func TestExpiryIsPartOfAHoldingsIdentity(t *testing.T) {
	tr := newTree(t)
	december := clock.AddDate(0, 4, 0)
	january := clock.AddDate(0, 5, 0)

	receive := func(amount int64, expires *time.Time) {
		t.Helper()
		tr.apply(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
			Item: tr.item, Location: tr.pantry, Basis: domain.BasisContent,
			Amount: domain.FromMilli(amount * domain.Scale), ExpiresOn: expires,
		}))
	}

	receive(500, &december)
	receive(500, &december)
	if got := len(expiries(t, tr)); got != 1 {
		t.Fatalf("%d holdings after two receipts sharing an expiry, want 1 (O1 merges)", got)
	}

	receive(500, &january)
	receive(500, nil)
	got := expiries(t, tr)
	if len(got) != 3 {
		t.Fatalf("%d holdings for three distinct expiries, want 3: %v", len(got), got)
	}
	// And the dates actually survived the round trip, which is what made the
	// duplicate possible in the first place.
	for _, want := range []string{december.Format("2006-01-02"), january.Format("2006-01-02"), "none"} {
		if !contains(got, want) {
			t.Errorf("no holding expiring %s; got %v", want, got)
		}
	}
}

// expiries reports one label per live Holding of the tree's item, so a
// duplicate H8 key shows up as a repeated label rather than a count.
func expiries(t *testing.T, tr *tree) []string {
	t.Helper()
	details, err := tr.r.HoldingsOfItem(tr.ctx, tr.item)
	if err != nil {
		t.Fatalf("holdings of item: %v", err)
	}
	var out []string
	for _, d := range details {
		base := d.Holding.Base()
		if base.RetiredAt != nil {
			continue
		}
		if base.ExpiresOn == nil {
			out = append(out, "none")
			continue
		}
		out = append(out, base.ExpiresOn.Format("2006-01-02"))
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
