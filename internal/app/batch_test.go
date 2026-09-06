package app_test

import (
	"context"
	"strings"
	"testing"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/origin"
	"home-management-system/internal/testsupport"
)

// H8 across a batch.
//
// Every command in a batch is planned against a snapshot taken before the batch
// began, and against its own private record of which slots it has filled. So
// two commands sending the same thing to the same place each looked at a
// photograph in which the destination was free, and what committed was two
// active Holdings on one H8 key -- the invariant the whole ops layer exists to
// uphold, broken by the layer that staples plans together, silently, with the
// integrity report as the only witness.
//
// Plan.filled is the same fix one level down: "the snapshot is a photograph
// taken before the plan began, so anything the plan does is invisible to it."
// Nothing said the same thing about a plan and its neighbours.

type batchHouse struct {
	ctx                        context.Context
	ctrl                       app.Controller
	rice                       domain.ItemID
	pantry, garage, shed, loft domain.LocationID
	fromPantry, fromGarage     domain.HoldingID
}

func newBatchHouse(t *testing.T) *batchHouse {
	t.Helper()
	conn := testsupport.NewDB(t)
	ctx := context.Background()
	l, o := ledger.New(conn), origin.New(conn)
	h := &batchHouse{ctx: ctx, ctrl: app.Open(conn)}

	cat, err := o.CreateCategory(ctx, origin.CreateCategoryInput{Name: "Grains"})
	if err != nil {
		t.Fatal(err)
	}
	for _, place := range []struct {
		name string
		into *domain.LocationID
	}{{"Pantry", &h.pantry}, {"Garage", &h.garage}, {"Shed", &h.shed}, {"Loft", &h.loft}} {
		if *place.into, err = l.CreateLocation(ctx, place.name, nil, ""); err != nil {
			t.Fatal(err)
		}
	}
	two := domain.FromMilli(2_000_000)
	if h.rice, err = o.CreateBulkItem(ctx, origin.CreateBulkItemInput{
		Name: "Basmati Rice", Category: cat, ContentUnit: "g", PackageSize: &two,
	}); err != nil {
		t.Fatal(err)
	}
	// Two content-basis holdings of one item, in two places -- which is the
	// ordinary shape of a house and the shape that breaks a batch.
	for _, at := range []struct {
		where domain.LocationID
		into  *domain.HoldingID
	}{{h.pantry, &h.fromPantry}, {h.garage, &h.fromGarage}} {
		if *at.into, err = l.CreateBulkHolding(ctx, ledger.CreateBulkHoldingInput{
			Item: h.rice, Location: at.where, UnitBasis: domain.BasisContent,
		}); err != nil {
			t.Fatal(err)
		}
		h.mustRun(t, command.Receive{
			Item: h.rice, Location: at.where, Amount: grams(500), Basis: domain.BasisContent,
		})
	}
	return h
}

func grams(n int64) domain.Quantity { return domain.FromMilli(n * domain.Scale) }

func (h *batchHouse) run(cmds ...command.Command) error {
	plan, _, err := h.ctrl.PlanAll(h.ctx, cmds)
	if err != nil {
		return err
	}
	return h.ctrl.ApplyPlan(h.ctx, plan)
}

func (h *batchHouse) mustRun(t *testing.T, cmds ...command.Command) {
	t.Helper()
	if err := h.run(cmds...); err != nil {
		t.Fatalf("apply: %v", err)
	}
}

// intact asserts what the harness cannot: that the ledger can still reproduce
// every Holding from its events.
func (h *batchHouse) intact(t *testing.T) {
	t.Helper()
	row, err := h.ctrl.Integrity(h.ctx)
	if err != nil {
		t.Fatalf("integrity: %v", err)
	}
	if !row.Clean() {
		t.Errorf("the batch left a state the ledger cannot reproduce: %+v", row.Orphans)
	}
}

func (h *batchHouse) holdingsAt(t *testing.T, where domain.LocationID) int {
	t.Helper()
	rows, err := h.ctrl.Holdings(h.ctx)
	if err != nil {
		t.Fatalf("holdings: %v", err)
	}
	n := 0
	for _, r := range rows {
		if r.LocationID == where && !r.Retired {
			n++
		}
	}
	return n
}

// Every way two commands can land in one slot is refused, and refused BEFORE
// anything is written.
func TestABatchWillNotPutTwoHoldingsInOneSlot(t *testing.T) {
	for _, tc := range []struct {
		name string
		of   func(h *batchHouse) []command.Command
	}{
		{"two moves", func(h *batchHouse) []command.Command {
			return []command.Command{
				command.Move{Holding: h.fromPantry, To: h.shed},
				command.Move{Holding: h.fromGarage, To: h.shed},
			}
		}},
		{"two receipts", func(h *batchHouse) []command.Command {
			return []command.Command{
				command.Receive{Item: h.rice, Location: h.shed, Amount: grams(100), Basis: domain.BasisContent},
				command.Receive{Item: h.rice, Location: h.shed, Amount: grams(100), Basis: domain.BasisContent},
			}
		}},
		{"a move and a receipt", func(h *batchHouse) []command.Command {
			return []command.Command{
				command.Move{Holding: h.fromPantry, To: h.shed},
				command.Receive{Item: h.rice, Location: h.shed, Amount: grams(100), Basis: domain.BasisContent},
			}
		}},
		{"a receipt and a move", func(h *batchHouse) []command.Command {
			return []command.Command{
				command.Receive{Item: h.rice, Location: h.shed, Amount: grams(100), Basis: domain.BasisContent},
				command.Move{Holding: h.fromPantry, To: h.shed},
			}
		}},
		{"two new holdings", func(h *batchHouse) []command.Command {
			return []command.Command{
				command.NewHolding{Item: h.rice, Location: h.shed, Basis: domain.BasisContent},
				command.NewHolding{Item: h.rice, Location: h.shed, Basis: domain.BasisContent},
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newBatchHouse(t)
			err := h.run(tc.of(h)...)
			if err == nil {
				t.Fatal("the batch was accepted")
			}
			if !strings.Contains(err.Error(), "one holding") {
				t.Errorf("the refusal does not say what is wrong: %v", err)
			}
			// Refused before the transaction, not rolled back after it.
			if n := h.holdingsAt(t, h.shed); n != 0 {
				t.Errorf("%d holdings reached the Shed anyway", n)
			}
			h.intact(t)
		})
	}
}

// The check must not fire on the ordinary case it resembles.
//
// Two receipts of the same rice onto a shelf that already keeps some are two
// Acquired events against ONE Holding -- which is what they should be, and what
// holdingFor produces. Both commands fill the same slot with the same existing
// Holding, and same is not a conflict.
func TestTwoReceiptsIntoAnExistingSlotStillMerge(t *testing.T) {
	h := newBatchHouse(t)
	before := h.holdingsAt(t, h.pantry)

	h.mustRun(t,
		command.Receive{Item: h.rice, Location: h.pantry, Amount: grams(100), Basis: domain.BasisContent},
		command.Receive{Item: h.rice, Location: h.pantry, Amount: grams(200), Basis: domain.BasisContent},
	)

	if after := h.holdingsAt(t, h.pantry); after != before {
		t.Errorf("the pantry went from %d holdings to %d; the two receipts did not merge", before, after)
	}
	h.intact(t)
}

// And a batch whose commands land in DIFFERENT slots is untouched by any of
// this, which is nearly every batch anybody writes.
func TestABatchOfMovesToDifferentPlacesStillWorks(t *testing.T) {
	h := newBatchHouse(t)
	h.mustRun(t,
		command.Move{Holding: h.fromPantry, To: h.shed},
		command.Move{Holding: h.fromGarage, To: h.loft},
	)
	if n := h.holdingsAt(t, h.shed); n != 1 {
		t.Errorf("the Shed has %d holdings, want 1", n)
	}
	if n := h.holdingsAt(t, h.loft); n != 1 {
		t.Errorf("the Loft has %d holdings, want 1", n)
	}
	h.intact(t)
}
