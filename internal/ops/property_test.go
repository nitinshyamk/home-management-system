package ops_test

import (
	"math/rand"
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
	"home-management-system/internal/testsupport"
)

// The three-way property test, raised from events to OPERATIONS.
//
// Stage 6 generated random events and checked that the real system, a naive
// in-memory oracle, and a replay of the ledger all agreed. That made H10 and O3
// real. It could not reach the operations layer at all, because an operation is
// not an event: it decides which events to emit, how many, and against which
// Holdings -- and those decisions are where composition bugs live.
//
// So this generates random OPERATIONS, plans them, executes them, and feeds the
// events they turned out to emit into the oracle. Divergence localises the
// same way as before: real vs oracle is a planning or fold bug, real vs replay
// is a completeness bug, oracle vs replay is a replay bug.
//
// The oracle is seeded with each new Holding's kind and stowed location read
// from the row rather than from the ledger. That is not a shortcut: identity
// and immutable birth facts are inputs to the ledger, not outputs (schema
// 3.12), and the oracle models ledger-DERIVED state only.

func TestOperationsThreeWayAgreement(t *testing.T) {
	const sequences = 40
	const opsPerSequence = 30

	for seed := 0; seed < sequences; seed++ {
		seed := seed
		t.Run(seedName(seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(seed)))
			tr := newTree(t)
			oracle := testsupport.NewOracle()

			// Every sequence starts with stock in the pantry and a cable on the
			// shelf. Without them a sequence that happens to draw no Receive
			// has nothing to act on, and 30 refusals in a row test only the
			// refusals.
			//
			// The seeding goes through the SAME execute-and-sync path as
			// everything else. Registering the Holding directly and skipping
			// its Acquired left the oracle short by exactly the seeded amount,
			// which the test then reported as a system defect.
			run := func(batch ops.Batch, err error) bool {
				if err != nil {
					// The planner refused. Refusals are expected and are the
					// point: they exercise every guard the operations carry.
					return false
				}
				// A plan the PLANNER accepted must be one the LEDGER accepts.
				//
				// This is the strongest property here, and the one the
				// operations layer exists to make true: planning is where
				// legality is decided, so an execute failure means the
				// planner's model of what is legal disagrees with the fold's.
				// Treating it as a skip would have hidden the H8
				// self-collision found in 8d, where a valid-looking plan
				// opened two packages into two Holdings and then consumed
				// more than either held.
				res, err := tr.ex.Execute(tr.ctx, batch)
				if err != nil {
					t.Fatalf("the planner accepted %q but the ledger refused it: %v",
						batch.Steps[0].Summary, err)
				}
				syncOracle(t, tr, oracle, batch, res)
				return true
			}

			cable := tr.unique(t, tr.shelf1)
			d, err := tr.r.Holding(tr.ctx, cable)
			if err != nil {
				t.Fatalf("read cable: %v", err)
			}
			oracle.Create(cable, d.Holding.Kind(), d.Holding.Base().StowedLocation)

			if !run(tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
				Item: tr.item, Location: tr.pantry, Amount: domain.FromMilli(4 * domain.Scale),
				Basis: domain.BasisPackage, Source: "shop",
			})) {
				t.Fatal("the seeding receive was refused")
			}

			applied := 0
			for i := 0; i < opsPerSequence; i++ {
				if run(randomOperation(rng, tr)) {
					applied++
				}
			}

			if applied == 0 {
				t.Fatal("no operation was ever applied; the generator is not exercising anything")
			}

			for _, id := range oracle.IDs() {
				modelled, _ := oracle.Holding(id)

				replayed, err := tr.led.Replay(tr.ctx, id)
				if err != nil {
					t.Fatalf("replay holding %d: %v", id, err)
				}
				if diffs := modelled.Diff(replayed); len(diffs) != 0 {
					t.Fatalf("holding %d after %d operations, replay vs oracle: %v", id, applied, diffs)
				}
			}

			// And the stored projections agree with a replay, which is the
			// completeness half: any write that skipped its event shows here.
			report, err := tr.led.VerifyAll(tr.ctx)
			if err != nil {
				t.Fatalf("verify all: %v", err)
			}
			if !report.Clean() {
				t.Fatalf("report not clean after %d operations: %+v", applied, report)
			}
		})
	}
}

func seedName(seed int) string {
	return "seed" + string(rune('0'+seed/10)) + string(rune('0'+seed%10))
}

// syncOracle teaches the oracle about Holdings the batch created, then applies
// the events it emitted.
func syncOracle(t *testing.T, tr *tree, oracle *testsupport.Oracle, batch ops.Batch, res ops.Result) {
	t.Helper()
	for i, step := range batch.Steps {
		for _, id := range res.Created[i].Holdings {
			d, err := tr.r.Holding(tr.ctx, id)
			if err != nil {
				t.Fatalf("read created holding %d: %v", id, err)
			}
			base := d.Holding.Base()
			oracle.Create(id, d.Holding.Kind(), base.StowedLocation)
		}
		if step.Records == nil {
			continue
		}
		events, err := step.Records(res.Created[i])
		if err != nil {
			t.Fatalf("re-resolve events: %v", err)
		}
		for _, e := range events {
			kind, _ := e.Subject()
			if kind != domain.SubjectHolding {
				// Location and Item events fold to nothing on a Holding, and
				// the oracle models Holdings only.
				continue
			}
			if err := oracle.Apply(e); err != nil {
				t.Fatalf("oracle rejected %s that the real system accepted: %v", e.Type(), err)
			}
		}
	}
}

// randomOperation builds one arbitrary operation against the fixture.
//
// It deliberately includes operations that will be refused -- consuming from an
// empty pantry, returning something that was never out -- because a generator
// that only produces legal input tests half the system.
func randomOperation(rng *rand.Rand, tr *tree) (ops.Batch, error) {
	amount := func(max int64) domain.Quantity {
		return domain.FromMilli(int64(rng.Intn(int(max))) + 1)
	}
	place := func() domain.LocationID {
		switch rng.Intn(3) {
		case 0:
			return tr.pantry
		case 1:
			return tr.garage
		default:
			return tr.shelf1
		}
	}
	// aHolding picks an existing Holding, or zero if there are none yet.
	aHolding := func(of domain.ItemID) domain.HoldingID {
		details, err := tr.r.HoldingsOfItem(tr.ctx, of)
		if err != nil || len(details) == 0 {
			return 0
		}
		return details[rng.Intn(len(details))].Holding.Base().ID
	}

	switch rng.Intn(13) {
	case 0, 1: // receive is weighted, or nothing else ever has anything to act on
		basis := domain.BasisContent
		if rng.Intn(2) == 0 {
			basis = domain.BasisPackage
		}
		max := int64(500 * domain.Scale)
		if basis == domain.BasisPackage {
			max = 4 * domain.Scale
		}
		return tr.pl.Receive(tr.ctx, ops.ReceiveRequest{
			Item: tr.item, Location: place(), Amount: amount(max), Basis: basis, Source: "shop",
		})
	case 2, 3:
		return tr.pl.Consume(tr.ctx, ops.ConsumeRequest{
			Item: tr.item, Location: place(), Amount: amount(600 * domain.Scale), Reason: "used",
		})
	case 4:
		return tr.pl.Open(tr.ctx, ops.OpenRequest{Item: tr.item, Location: place()})
	case 5:
		return tr.pl.Move(tr.ctx, ops.MoveRequest{Holding: aHolding(tr.item), To: place()})
	case 6:
		h := aHolding(tr.item)
		half := amount(200 * domain.Scale)
		return tr.pl.Move(tr.ctx, ops.MoveRequest{Holding: h, To: place(), Amount: &half})
	case 7:
		return tr.pl.Count(tr.ctx, ops.CountRequest{
			Holding: aHolding(tr.item), Observed: amount(900 * domain.Scale),
		})
	case 8:
		return tr.pl.Discard(tr.ctx, ops.DiscardRequest{
			Holding: aHolding(tr.item), Amount: amount(300 * domain.Scale), Reason: "spoiled",
		})
	case 9:
		to := place()
		return tr.pl.CheckOut(tr.ctx, ops.CheckOutRequest{Holding: aHolding(tr.cableItem), To: &to})
	case 10:
		switch rng.Intn(3) {
		case 0:
			return tr.pl.Return(tr.ctx, ops.ReturnRequest{Holding: aHolding(tr.cableItem)})
		case 1:
			return tr.pl.MarkLost(tr.ctx, ops.MarkLostRequest{Holding: aHolding(tr.cableItem)})
		default:
			return tr.pl.Found(tr.ctx, ops.FoundRequest{Holding: aHolding(tr.cableItem)})
		}
	case 11:
		return tr.pl.Verify(tr.ctx, ops.VerifyRequest{
			Holding: aHolding(tr.cableItem), Present: rng.Intn(2) == 0,
		})
	default:
		// Either kind, deliberately: Rehome is Unique-only, so drawing the Bulk
		// item exercises the refusal and drawing the cable exercises the
		// success. Narrowing this to the cable made a break of that guard
		// invisible, which is how the narrowing was noticed.
		if rng.Intn(2) == 0 {
			return tr.pl.Rehome(tr.ctx, ops.RehomeRequest{Holding: aHolding(tr.item), To: place()})
		}
		return tr.pl.Rehome(tr.ctx, ops.RehomeRequest{Holding: aHolding(tr.cableItem), To: place()})
	}
}
