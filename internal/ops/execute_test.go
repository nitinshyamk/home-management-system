package ops_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"home-management-system/internal/db"
	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/ops"
	"home-management-system/internal/origin"
	"home-management-system/internal/query"
	"home-management-system/internal/testsupport"
)

var clock = time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)

type fixture struct {
	ctx    context.Context
	conn   *sql.DB
	ex     *ops.Executor
	r      *query.Reader
	pantry domain.LocationID
	cat    domain.CategoryID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	conn := testsupport.NewDB(t)
	ctx := context.Background()

	pantry, err := ledger.New(conn).CreateLocation(ctx, "Pantry", nil, "")
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	cat, err := origin.New(conn).CreateCategory(ctx, origin.CreateCategoryInput{Name: "Pantry Goods"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	return &fixture{ctx: ctx, conn: conn, ex: ops.New(conn), r: query.New(conn), pantry: pantry, cat: cat}
}

// countItems and countHoldings read through the query path, so the assertions
// see what the rest of the application would see rather than trusting the
// executor's own report.
func (f *fixture) countItems(t *testing.T) int {
	t.Helper()
	items, err := f.r.Items(f.ctx)
	if err != nil {
		t.Fatalf("read items: %v", err)
	}
	return len(items)
}

func (f *fixture) countHoldings(t *testing.T) int {
	t.Helper()
	holdings, err := f.r.Holdings(f.ctx)
	if err != nil {
		t.Fatalf("read holdings: %v", err)
	}
	return len(holdings)
}

// newStockedItem is the intent that motivates this whole layer: an Item that
// did not exist, a Holding of it, and the stock that arrived -- three writes
// across two write paths.
func (f *fixture) newStockedItem(name string, grams int64) ops.Batch {
	size := domain.FromMilli(2_000_000)
	return ops.Batch{Steps: []ops.Step{{
		Summary: "acquire " + name,
		Originates: []ops.Origination{
			ops.NewBulkItem{Name: name, Category: f.cat, ContentUnit: "g", PackageSize: &size},
			ops.NewBulkHolding{Location: f.pantry, UnitBasis: domain.BasisContent},
		},
		Records: func(c ops.Created) ([]domain.Event, error) {
			h, err := c.Holding(0)
			if err != nil {
				return nil, err
			}
			return []domain.Event{domain.Acquired{
				EventBase: domain.EventBase{OccurredAt: clock},
				Holding:   h,
				Delta:     domain.FromMilli(grams * domain.Scale),
				Source:    "corner shop",
			}}, nil
		},
	}}}
}

// TestCrossPathBatchCommitsTogether is the positive half of the gap v01 left
// open: one intent, two write paths, one transaction.
func TestCrossPathBatchCommitsTogether(t *testing.T) {
	f := newFixture(t)

	res, err := f.ex.Execute(f.ctx, f.newStockedItem("Basmati Rice", 2000))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if got := len(res.Created[0].Items); got != 1 {
		t.Errorf("created %d items, want 1", got)
	}
	if got := len(res.Events[0]); got != 1 {
		t.Errorf("recorded %d events, want 1", got)
	}
	if got := f.countItems(t); got != 1 {
		t.Errorf("%d items visible, want 1", got)
	}
	if got := f.countHoldings(t); got != 1 {
		t.Errorf("%d holdings visible, want 1", got)
	}

	// The stock arrived through the ledger, so the projection must agree with a
	// replay -- the same H10 check the integrity job runs.
	report, err := ledger.New(f.conn).VerifyAll(f.ctx)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !report.Clean() {
		t.Errorf("ledger disagrees with the projection after a batch: %+v", report)
	}
}

// TestFailureAfterOriginationLeavesNothing is the half that actually matters.
// The Item is created, the Holding is created, and THEN the recording fails.
// Without a shared transaction the two creations would survive.
func TestFailureAfterOriginationLeavesNothing(t *testing.T) {
	f := newFixture(t)

	boom := errors.New("injected failure")
	batch := f.newStockedItem("Basmati Rice", 2000)
	batch.Steps[0].Records = func(ops.Created) ([]domain.Event, error) { return nil, boom }

	if _, err := f.ex.Execute(f.ctx, batch); !errors.Is(err, boom) {
		t.Fatalf("execute error = %v, want the injected failure", err)
	}

	if got := f.countItems(t); got != 0 {
		t.Errorf("%d items survived a failed batch, want 0 -- origination escaped the transaction", got)
	}
	if got := f.countHoldings(t); got != 0 {
		t.Errorf("%d holdings survived a failed batch, want 0", got)
	}
}

// TestFailureInsideTheLedgerRollsBackOrigination injects the failure deeper --
// inside ApplyBatch rather than before it -- so the ledger has already written
// part of the step when it fails.
func TestFailureInsideTheLedgerRollsBackOrigination(t *testing.T) {
	f := newFixture(t)

	batch := f.newStockedItem("Basmati Rice", 2000)
	inner := batch.Steps[0].Records
	batch.Steps[0].Records = func(c ops.Created) ([]domain.Event, error) {
		events, err := inner(c)
		if err != nil {
			return nil, err
		}
		// A second event against a Holding that does not exist. The first is
		// written, then this one fails inside the ledger.
		return append(events, domain.Acquired{
			EventBase: domain.EventBase{OccurredAt: clock},
			Holding:   domain.HoldingID(999_999),
			Delta:     domain.FromMilli(1000),
		}), nil
	}

	if _, err := f.ex.Execute(f.ctx, batch); err == nil {
		t.Fatal("execute succeeded against a nonexistent holding")
	}
	if got := f.countItems(t); got != 0 {
		t.Errorf("%d items survived, want 0", got)
	}
	if got := f.countHoldings(t); got != 0 {
		t.Errorf("%d holdings survived, want 0", got)
	}
}

// TestOneStepFailureAbandonsTheWholeBatch is the all-or-nothing rule an import
// depends on: a receipt does not half-apply.
func TestOneStepFailureAbandonsTheWholeBatch(t *testing.T) {
	f := newFixture(t)

	good := f.newStockedItem("Basmati Rice", 2000).Steps[0]
	bad := f.newStockedItem("Turmeric", 500).Steps[0]
	bad.Records = func(ops.Created) ([]domain.Event, error) { return nil, errors.New("injected") }

	if _, err := f.ex.Execute(f.ctx, ops.Batch{Steps: []ops.Step{good, bad}}); err == nil {
		t.Fatal("execute succeeded despite a failing step")
	}
	if got := f.countItems(t); got != 0 {
		t.Errorf("%d items survived, want 0 -- the first step committed independently", got)
	}
}

// TestOriginationEscapesWithoutTheSharedTransaction demonstrates the defect the
// design fixes, so the guards above are known to be guarding something.
//
// It runs on a REAL pool, because that is what makes the defect silent. With
// v01's arrangement -- each write path bound to the connection rather than to
// the unit of work -- an origination inside a failing operation takes a
// DIFFERENT connection, commits on its own, and survives the rollback of the
// work it was part of. Nothing errors. An Item with no Holding simply exists.
//
// The single-connection harness NewDB uses cannot show this: there is no second
// connection to escape onto, so the same code deadlocks instead. A test that
// deadlocks looks like a slow test, which is how a defect like this survives.
func TestOriginationEscapesWithoutTheSharedTransaction(t *testing.T) {
	ctx := context.Background()
	conn := testsupport.NewPooledDB(t)

	cat, err := origin.New(conn).CreateCategory(ctx, origin.CreateCategoryInput{Name: "Pantry Goods"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	size := domain.FromMilli(2_000_000)

	// The v01 arrangement, inside a unit of work that then fails.
	pooled := origin.New(conn)
	err = db.InTx(ctx, conn, func(*sql.Tx) error {
		if _, err := pooled.CreateBulkItem(ctx, origin.CreateBulkItemInput{
			Name: "Basmati Rice", Category: cat, ContentUnit: "g", PackageSize: &size,
		}); err != nil {
			return err
		}
		return errors.New("the recording that was meant to accompany it failed")
	})
	if err == nil {
		t.Fatal("expected the unit of work to fail")
	}

	items, err := query.New(conn).Items(ctx)
	if err != nil {
		t.Fatalf("read items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("%d items, want 1 -- this test asserts the OLD behaviour on purpose", len(items))
	}
	t.Logf("pool-scoped origination survived the rollback: %q exists with no Holding, and nothing errored",
		items[0].Base().Name)

	// The same sequence through ops.Execute leaves nothing, which is the point.
	if _, err := ops.New(conn).Execute(ctx, ops.Batch{Steps: []ops.Step{{
		Summary:    "acquire Turmeric",
		Originates: []ops.Origination{ops.NewBulkItem{Name: "Turmeric", Category: cat, ContentUnit: "g"}},
		Records: func(ops.Created) ([]domain.Event, error) {
			return nil, errors.New("injected")
		},
	}}}); err == nil {
		t.Fatal("expected the batch to fail")
	}
	items, err = query.New(conn).Items(ctx)
	if err != nil {
		t.Fatalf("read items: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("%d items after the failed batch, want 1 (only the escapee) -- ops.Execute leaked one too", len(items))
	}
}

// TestEnlistedScopeDoesNotOpenItsOwnTransaction is the property NewTx exists
// for: a write path inside someone else's transaction must not commit early.
func TestEnlistedScopeDoesNotOpenItsOwnTransaction(t *testing.T) {
	f := newFixture(t)
	size := domain.FromMilli(2_000_000)

	// Roll back deliberately. If origin.NewTx had begun and committed its own
	// transaction, the Item would outlive this rollback.
	tx, err := f.conn.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := origin.NewTx(tx).CreateBulkItem(f.ctx, origin.CreateBulkItemInput{
		Name: "Basmati Rice", Category: f.cat, ContentUnit: "g", PackageSize: &size,
	}); err != nil {
		tx.Rollback()
		t.Fatalf("create item: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if got := f.countItems(t); got != 0 {
		t.Errorf("%d items survived a rollback, want 0 -- the enlisted path committed on its own", got)
	}
}
