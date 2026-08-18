package ops_test

import (
	"errors"
	"testing"
	"time"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
)

// Pure tests for the composing operations. Every Snapshot is a literal.

var (
	pantry  domain.LocationID = 3
	sealed  domain.HoldingID  = 30
	opened  domain.HoldingID  = 31
	riceID  domain.ItemID     = 200
	pkgSize                   = domain.FromMilli(2000 * domain.Scale) // a 2 kg bag
)

// riceItem is measured in grams and sold in 2 kg bags.
func riceItem() domain.BulkItem {
	size := pkgSize
	return domain.BulkItem{
		ItemBase:    domain.ItemBase{ID: riceID, Name: "Basmati Rice"},
		ContentUnit: "g",
		PackageSize: &size,
	}
}

// noPackageItem is measured but not packaged -- loose flour from a bin.
func noPackageItem() domain.BulkItem {
	return domain.BulkItem{
		ItemBase:    domain.ItemBase{ID: riceID, Name: "Loose Flour"},
		ContentUnit: "g",
	}
}

func holdingAt(id domain.HoldingID, at domain.LocationID, basis domain.UnitBasis, milli int64) domain.BulkHolding {
	return domain.BulkHolding{
		HoldingBase: domain.HoldingBase{ID: id, Item: riceID, StowedLocation: at},
		Quantity:    domain.FromMilli(milli),
		UnitBasis:   basis,
	}
}

// riceSnap builds a Snapshot for the rice item holding whatever is given.
func riceSnap(item domain.BulkItem, hs ...domain.Holding) ops.Snapshot {
	s := ops.Snapshot{
		Now:      at,
		Holdings: map[domain.HoldingID]domain.Holding{},
		Items:    map[domain.ItemID]domain.Item{riceID: item},
		ByItem:   map[domain.ItemID][]domain.HoldingID{},
	}
	for _, h := range hs {
		s.Holdings[h.Base().ID] = h
		s.ByItem[h.Base().Item] = append(s.ByItem[h.Base().Item], h.Base().ID)
	}
	return s
}

// shape returns the event types of a planned Step, resolving references to
// Holdings the Step creates.
func shape(t *testing.T, b ops.Batch) []string {
	t.Helper()
	step := b.Steps[0]
	created := ops.Created{}
	for range step.Originates {
		created.Holdings = append(created.Holdings, domain.HoldingID(900+len(created.Holdings)))
	}
	events, err := step.Records(created)
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	return types(events)
}

// asBatch runs a pure plan function and wraps the result, so the tests can use
// one shape helper for planned and gathered batches alike.
func asBatch(t *testing.T, p ops.Batch, err error) ops.Batch {
	t.Helper()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return p
}

// ---------------------------------------------------------------------------
// Consume: schema Walkthrough 02
// ---------------------------------------------------------------------------

// TestConsumeOpensASealedPackage is Walkthrough 02 from the conceptual schema,
// executable:
//
//	Consume(rice, 100 g) finds no Content-basis Holding, so it emits
//	Split{-1 package} on the sealed BulkHolding, Opened{+2000 g} on a newly
//	created one, then Consumed{-100 g}. Three events, one user action.
func TestConsumeOpensASealedPackage(t *testing.T) {
	s := riceSnap(riceItem(), holdingAt(sealed, pantry, domain.BasisPackage, 2*domain.Scale))

	p, err := ops.PlanConsume(s, ops.ConsumeRequest{
		Item: riceID, Location: pantry, Amount: domain.FromMilli(100 * domain.Scale),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	b := p.Batch("consume")

	if got := shape(t, b); !equal(got, []string{"Split", "Opened", "Consumed"}) {
		t.Errorf("plan = %v, want [Split Opened Consumed]", got)
	}
	// The content-basis Holding does not exist yet, so the operation creates
	// it. That is an origination, but not one a person is asked to approve: a
	// Holding is a placement, not a named entity.
	if len(b.Steps[0].Originates) != 1 {
		t.Fatalf("originations = %d, want 1 (the content-basis holding)", len(b.Steps[0].Originates))
	}
	if b.Steps[0].NeedsConfirmation() {
		t.Error("opening a bag asked for confirmation; only named entities should")
	}
}

// TestConsumeFromAnOpenPackageRecordsOneEvent: the common case stays cheap.
func TestConsumeFromAnOpenPackageRecordsOneEvent(t *testing.T) {
	s := riceSnap(riceItem(),
		holdingAt(sealed, pantry, domain.BasisPackage, 2*domain.Scale),
		holdingAt(opened, pantry, domain.BasisContent, 800*domain.Scale))

	p, err := ops.PlanConsume(s, ops.ConsumeRequest{
		Item: riceID, Location: pantry, Amount: domain.FromMilli(100 * domain.Scale),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if got := shape(t, p.Batch("consume")); !equal(got, []string{"Consumed"}) {
		t.Errorf("plan = %v, want [Consumed] -- a bag was already open", got)
	}
}

// TestConsumeOpensAsManyPackagesAsNeeded: "use 3 kg" from 2 kg bags opens two.
func TestConsumeOpensAsManyPackagesAsNeeded(t *testing.T) {
	s := riceSnap(riceItem(), holdingAt(sealed, pantry, domain.BasisPackage, 3*domain.Scale))

	p, err := ops.PlanConsume(s, ops.ConsumeRequest{
		Item: riceID, Location: pantry, Amount: domain.FromMilli(3000 * domain.Scale),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	b := p.Batch("consume")
	want := []string{"Split", "Opened", "Split", "Opened", "Consumed"}
	if got := shape(t, b); !equal(got, want) {
		t.Errorf("plan = %v, want %v", got, want)
	}

	// The shape alone is not enough, and this is the assertion that matters.
	// A plan enforces H8 against the snapshot easily; enforcing it against its
	// OWN earlier steps is the hard part, because the snapshot is a photograph
	// taken before the plan began. Two openings must fill one content-basis
	// Holding, not two.
	if got := len(b.Steps[0].Originates); got != 1 {
		t.Errorf("created %d holdings, want 1 -- two openings landed on the same H8 slot", got)
	}
	created := ops.Created{Holdings: []domain.HoldingID{901, 902}}
	events, err := b.Steps[0].Records(created)
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	first, second := events[1].(domain.Opened), events[3].(domain.Opened)
	if first.Holding != second.Holding {
		t.Errorf("openings credited holdings %d and %d; both are the same slot",
			first.Holding, second.Holding)
	}
	if consumed := events[4].(domain.Consumed); consumed.Holding != first.Holding {
		t.Errorf("consumed from holding %d, want the one that was opened into (%d)",
			consumed.Holding, first.Holding)
	}
}

// TestConsumeSecondOpenSeesTheFirst: the snapshot still shows one package after
// the plan has already spent it, so the plan must track its own changes.
func TestConsumeMoreThanCanBeOpenedIsRefused(t *testing.T) {
	// One 2 kg bag, and 3 kg wanted.
	s := riceSnap(riceItem(), holdingAt(sealed, pantry, domain.BasisPackage, 1*domain.Scale))

	_, err := ops.PlanConsume(s, ops.ConsumeRequest{
		Item: riceID, Location: pantry, Amount: domain.FromMilli(3000 * domain.Scale),
	})
	if !errors.Is(err, ops.ErrInsufficient) {
		t.Errorf("error = %v, want ErrInsufficient -- the plan spent a package it had already spent", err)
	}
}

func TestConsumeWithNothingThereIsRefused(t *testing.T) {
	s := riceSnap(riceItem())
	_, err := ops.PlanConsume(s, ops.ConsumeRequest{
		Item: riceID, Location: pantry, Amount: domain.FromMilli(100 * domain.Scale),
	})
	if !errors.Is(err, ops.ErrInsufficient) {
		t.Errorf("error = %v, want ErrInsufficient", err)
	}
}

// TestConsumeFromAnUnpackagedItemCannotOpenAnything: loose flour from a bin has
// no package to break into, so a shortfall is simply a shortfall.
func TestConsumeFromAnUnpackagedItemCannotOpenAnything(t *testing.T) {
	s := riceSnap(noPackageItem(), holdingAt(opened, pantry, domain.BasisContent, 50*domain.Scale))
	_, err := ops.PlanConsume(s, ops.ConsumeRequest{
		Item: riceID, Location: pantry, Amount: domain.FromMilli(100 * domain.Scale),
	})
	if !errors.Is(err, ops.ErrInvalidRequest) {
		t.Errorf("error = %v, want ErrInvalidRequest", err)
	}
}

// ---------------------------------------------------------------------------
// Receive: O1
// ---------------------------------------------------------------------------

// TestReceiveMergesIntoTheOccupiedSlot is O1. Buying more rice does not make a
// second pile.
func TestReceiveMergesIntoTheOccupiedSlot(t *testing.T) {
	s := riceSnap(riceItem(), holdingAt(opened, pantry, domain.BasisContent, 800*domain.Scale))

	p, err := ops.PlanReceive(s, ops.ReceiveRequest{
		Item: riceID, Location: pantry, Amount: domain.FromMilli(500 * domain.Scale),
		Basis: domain.BasisContent, Source: "corner shop",
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	b := p.Batch("receive")
	if len(b.Steps[0].Originates) != 0 {
		t.Errorf("created %d holdings; O1 says credit the one that is there",
			len(b.Steps[0].Originates))
	}
	events, _ := b.Steps[0].Records(ops.Created{})
	if ev := events[0].(domain.Acquired); ev.Holding != opened {
		t.Errorf("credited holding %d, want the existing %d", ev.Holding, opened)
	}
}

func TestReceiveCreatesAHoldingWhenTheSlotIsFree(t *testing.T) {
	s := riceSnap(riceItem())
	p, err := ops.PlanReceive(s, ops.ReceiveRequest{
		Item: riceID, Location: pantry, Amount: domain.FromMilli(2 * domain.Scale),
		Basis: domain.BasisPackage,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	b := p.Batch("receive")
	if len(b.Steps[0].Originates) != 1 {
		t.Fatalf("originations = %d, want 1", len(b.Steps[0].Originates))
	}
	if got := shape(t, b); !equal(got, []string{"Acquired"}) {
		t.Errorf("plan = %v, want [Acquired]", got)
	}
}

// TestSlotsDifferByBasis: packages and contents of the same item in the same
// place are different slots, which is what lets both exist at once.
func TestSlotsDifferByBasis(t *testing.T) {
	s := riceSnap(riceItem(), holdingAt(sealed, pantry, domain.BasisPackage, 2*domain.Scale))

	p, err := ops.PlanReceive(s, ops.ReceiveRequest{
		Item: riceID, Location: pantry, Amount: domain.FromMilli(500 * domain.Scale),
		Basis: domain.BasisContent,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(p.Batch("r").Steps[0].Originates) != 1 {
		t.Error("content stock merged into the package slot; the basis is part of the H8 key")
	}
}

// TestSlotsTreatTwoNullExpiriesAsEqual: H8 requires it, and SQL would not.
func TestSlotsTreatTwoNullExpiriesAsEqual(t *testing.T) {
	s := riceSnap(riceItem(), holdingAt(opened, pantry, domain.BasisContent, 800*domain.Scale))
	p, err := ops.PlanReceive(s, ops.ReceiveRequest{
		Item: riceID, Location: pantry, Amount: domain.FromMilli(100 * domain.Scale),
		Basis: domain.BasisContent, ExpiresOn: nil,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(p.Batch("r").Steps[0].Originates) != 0 {
		t.Error("two null expiries compared unequal, so a second holding was created")
	}
}

func TestSlotsDifferByExpiry(t *testing.T) {
	dated := holdingAt(opened, pantry, domain.BasisContent, 800*domain.Scale)
	when := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	dated.ExpiresOn = &when
	s := riceSnap(riceItem(), dated)

	p, err := ops.PlanReceive(s, ops.ReceiveRequest{
		Item: riceID, Location: pantry, Amount: domain.FromMilli(100 * domain.Scale),
		Basis: domain.BasisContent, ExpiresOn: nil,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(p.Batch("r").Steps[0].Originates) != 1 {
		t.Error("stock with no expiry merged into a dated holding")
	}
}

// TestReceiveInPackagesNeedsAPackageSize is H7 at the planning boundary.
func TestReceiveInPackagesNeedsAPackageSize(t *testing.T) {
	s := riceSnap(noPackageItem())
	_, err := ops.PlanReceive(s, ops.ReceiveRequest{
		Item: riceID, Location: pantry, Amount: domain.FromMilli(2 * domain.Scale),
		Basis: domain.BasisPackage,
	})
	if !errors.Is(err, ops.ErrInvalidRequest) {
		t.Errorf("error = %v, want ErrInvalidRequest", err)
	}
}

// ---------------------------------------------------------------------------
// Count
// ---------------------------------------------------------------------------

func TestCountAgreeingRecordsOnlyTheObservation(t *testing.T) {
	s := riceSnap(riceItem(), holdingAt(opened, pantry, domain.BasisContent, 800*domain.Scale))
	p, err := ops.PlanCount(s, ops.CountRequest{
		Holding: opened, Observed: domain.FromMilli(800 * domain.Scale),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if got := shape(t, p.Batch("count")); !equal(got, []string{"Counted"}) {
		t.Errorf("plan = %v, want [Counted]", got)
	}
}

// TestCountDisagreeingAdjustsWithoutOverwriting: the correction is a separate
// event carrying the discrepancy, because silently setting the quantity would
// destroy the only evidence the two ever disagreed.
func TestCountDisagreeingAdjustsWithoutOverwriting(t *testing.T) {
	s := riceSnap(riceItem(), holdingAt(opened, pantry, domain.BasisContent, 800*domain.Scale))
	p, err := ops.PlanCount(s, ops.CountRequest{
		Holding: opened, Observed: domain.FromMilli(750 * domain.Scale),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	b := p.Batch("count")
	if got := shape(t, b); !equal(got, []string{"Counted", "Adjusted"}) {
		t.Fatalf("plan = %v, want [Counted Adjusted]", got)
	}
	events, _ := b.Steps[0].Records(ops.Created{})
	if ev := events[1].(domain.Adjusted); ev.Delta.Milli() != -50*domain.Scale {
		t.Errorf("adjustment = %s, want -50", ev.Delta)
	}
	if events[0].Type() != domain.TypeCounted {
		t.Error("the observation must come first; the correction is a consequence of it")
	}
}

// ---------------------------------------------------------------------------
// Move
// ---------------------------------------------------------------------------

func TestMoveToAFreeSlotRelocatesTheHolding(t *testing.T) {
	s := riceSnap(riceItem(), holdingAt(opened, pantry, domain.BasisContent, 800*domain.Scale))
	p, err := ops.PlanMove(s, ops.MoveRequest{Holding: opened, To: shelf})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	// Relocating keeps the Holding's identity, its expiry, and its history.
	if got := shape(t, p.Batch("move")); !equal(got, []string{"Moved"}) {
		t.Errorf("plan = %v, want [Moved]", got)
	}
}

// TestMoveOntoAnOccupiedSlotMerges: Moved would put two active Holdings on one
// H8 key, so a collision has to become a merge instead.
func TestMoveOntoAnOccupiedSlotMerges(t *testing.T) {
	s := riceSnap(riceItem(),
		holdingAt(opened, pantry, domain.BasisContent, 800*domain.Scale),
		holdingAt(sealed, shelf, domain.BasisContent, 200*domain.Scale))

	p, err := ops.PlanMove(s, ops.MoveRequest{Holding: opened, To: shelf})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	b := p.Batch("move")
	if got := shape(t, b); !equal(got, []string{"Merged", "Merged"}) {
		t.Fatalf("plan = %v, want two Merged", got)
	}
	events, _ := b.Steps[0].Records(ops.Created{})
	src, dst := events[0].(domain.Merged), events[1].(domain.Merged)
	if src.Delta.Milli() != -800*domain.Scale || dst.Delta.Milli() != 800*domain.Scale {
		t.Errorf("deltas = %s / %s, want -800 / +800", src.Delta, dst.Delta)
	}
	if src.Holding != opened || dst.Holding != sealed {
		t.Errorf("merged %d -> %d, want %d -> %d", src.Holding, dst.Holding, opened, sealed)
	}
}

func TestPartialMoveSplitsWithoutRelocating(t *testing.T) {
	s := riceSnap(riceItem(), holdingAt(opened, pantry, domain.BasisContent, 800*domain.Scale))
	half := domain.FromMilli(300 * domain.Scale)

	p, err := ops.PlanMove(s, ops.MoveRequest{Holding: opened, To: shelf, Amount: &half})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	b := p.Batch("move")
	if got := shape(t, b); !equal(got, []string{"Merged", "Merged"}) {
		t.Errorf("plan = %v, want two Merged", got)
	}
	if len(b.Steps[0].Originates) != 1 {
		t.Errorf("originations = %d, want 1 (the destination holding)", len(b.Steps[0].Originates))
	}
}

func TestMoveMoreThanIsThereIsRefused(t *testing.T) {
	s := riceSnap(riceItem(), holdingAt(opened, pantry, domain.BasisContent, 100*domain.Scale))
	too := domain.FromMilli(500 * domain.Scale)
	_, err := ops.PlanMove(s, ops.MoveRequest{Holding: opened, To: shelf, Amount: &too})
	if !errors.Is(err, ops.ErrInsufficient) {
		t.Errorf("error = %v, want ErrInsufficient", err)
	}
}

func TestMoveAUniqueHoldingRejectsAQuantity(t *testing.T) {
	s := snap(unique(domain.CustodyAtRest))
	one := domain.FromMilli(domain.Scale)
	_, err := ops.PlanMove(s, ops.MoveRequest{Holding: cable, To: desk, Amount: &one})
	if !errors.Is(err, ops.ErrInvalidRequest) {
		t.Errorf("error = %v, want ErrInvalidRequest", err)
	}
}

func TestMoveToWhereItAlreadyIsIsRefused(t *testing.T) {
	s := riceSnap(riceItem(), holdingAt(opened, pantry, domain.BasisContent, 800*domain.Scale))
	_, err := ops.PlanMove(s, ops.MoveRequest{Holding: opened, To: pantry})
	if !errors.Is(err, ops.ErrInvalidRequest) {
		t.Errorf("error = %v, want ErrInvalidRequest", err)
	}
}

// TestMovingAnEmptyHoldingOntoAnOccupiedSlotIsRefused was found by the
// operations property test: the plan emitted Merged with a zero delta, which
// the fold rejects. Merging nothing into something is a no-op with two invalid
// events, not a move.
func TestMovingAnEmptyHoldingOntoAnOccupiedSlotIsRefused(t *testing.T) {
	s := riceSnap(riceItem(),
		holdingAt(opened, pantry, domain.BasisContent, 0),
		holdingAt(sealed, shelf, domain.BasisContent, 200*domain.Scale))

	_, err := ops.PlanMove(s, ops.MoveRequest{Holding: opened, To: shelf})
	if !errors.Is(err, ops.ErrInvalidRequest) {
		t.Errorf("error = %v, want ErrInvalidRequest", err)
	}
}

// TestMovingAnEmptyHoldingToAFreeSlotIsAllowed: the empty jar can still be put
// on a different shelf.
func TestMovingAnEmptyHoldingToAFreeSlotIsAllowed(t *testing.T) {
	s := riceSnap(riceItem(), holdingAt(opened, pantry, domain.BasisContent, 0))

	p, err := ops.PlanMove(s, ops.MoveRequest{Holding: opened, To: shelf})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if got := shape(t, p.Batch("move")); !equal(got, []string{"Moved"}) {
		t.Errorf("plan = %v, want [Moved]", got)
	}
}
