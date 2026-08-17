package ledger_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"home-management-system/internal/db"
	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/origin"
	"home-management-system/internal/testsupport"
)

var clock = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

type fixture struct {
	ctx      context.Context
	conn     *sql.DB
	p        *ledger.Processor
	o        *origin.Originator
	pantry   domain.LocationID
	garage   domain.LocationID
	riceItem domain.ItemID
	cableID  domain.ItemID
	spare    domain.ItemID
	rice     domain.HoldingID
	cable    domain.HoldingID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	conn := testsupport.NewDB(t)
	f := &fixture{
		ctx:  context.Background(),
		conn: conn,
		p:    ledger.New(conn).WithClock(func() time.Time { return clock }),
		o:    origin.New(conn),
	}

	var err error
	if f.pantry, err = f.p.CreateLocation(f.ctx, "Pantry", nil, ""); err != nil {
		t.Fatalf("create location: %v", err)
	}
	if f.garage, err = f.p.CreateLocation(f.ctx, "Garage", nil, ""); err != nil {
		t.Fatalf("create location: %v", err)
	}

	cat, err := f.o.CreateCategory(f.ctx, origin.CreateCategoryInput{Name: "Pantry Goods"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	size := domain.FromMilli(2_000_000)
	if f.riceItem, err = f.o.CreateBulkItem(f.ctx, origin.CreateBulkItemInput{
		Name: "Basmati Rice", Category: cat, ContentUnit: "g", PackageSize: &size,
	}); err != nil {
		t.Fatalf("create bulk item: %v", err)
	}
	if f.cableID, err = f.o.CreateUniqueItem(f.ctx, origin.CreateUniqueItemInput{
		Name: "USB-C Cable", Category: cat,
	}); err != nil {
		t.Fatalf("create unique item: %v", err)
	}

	// An Item with no Holdings, so a kind change is permitted: the composite
	// foreign key blocks it only while Holdings of the old kind exist.
	if f.spare, err = f.o.CreateBulkItem(f.ctx, origin.CreateBulkItemInput{
		Name: "Spare Bulk Item", Category: cat, ContentUnit: "g",
	}); err != nil {
		t.Fatalf("create spare item: %v", err)
	}

	if f.rice, err = f.p.CreateBulkHolding(f.ctx, ledger.CreateBulkHoldingInput{
		Item: f.riceItem, Location: f.pantry, UnitBasis: domain.BasisContent,
	}); err != nil {
		t.Fatalf("create bulk holding: %v", err)
	}
	if f.cable, err = f.p.CreateUniqueHolding(f.ctx, ledger.CreateUniqueHoldingInput{
		Item: f.cableID, Location: f.pantry, Label: "the good one",
	}); err != nil {
		t.Fatalf("create unique holding: %v", err)
	}
	return f
}

func (f *fixture) apply(t *testing.T, e domain.Event) domain.EventID {
	t.Helper()
	id, err := f.p.Apply(f.ctx, e)
	if err != nil {
		t.Fatalf("apply %s: %v", e.Type(), err)
	}
	return id
}

// quantityOf reads the stored projection directly, which is what H10 will later
// compare replay against.
func (f *fixture) quantityOf(t *testing.T, id domain.HoldingID) domain.Quantity {
	t.Helper()
	var milli int64
	if err := f.conn.QueryRow("SELECT quantity FROM bulk_holdings WHERE holding_id = ?", int64(id)).
		Scan(&milli); err != nil {
		t.Fatalf("read quantity: %v", err)
	}
	return domain.FromMilli(milli)
}

func (f *fixture) custodyOf(t *testing.T, id domain.HoldingID) (string, sql.NullString, sql.NullInt64) {
	t.Helper()
	var custody string
	var since sql.NullString
	var displaced sql.NullInt64
	if err := f.conn.QueryRow(
		"SELECT custody, custody_since, displaced_to_id FROM unique_holdings WHERE holding_id = ?",
		int64(id)).Scan(&custody, &since, &displaced); err != nil {
		t.Fatalf("read custody: %v", err)
	}
	return custody, since, displaced
}

func (f *fixture) stowedAt(t *testing.T, id domain.HoldingID) domain.LocationID {
	t.Helper()
	var loc int64
	if err := f.conn.QueryRow("SELECT stowed_location_id FROM holdings WHERE id = ?", int64(id)).
		Scan(&loc); err != nil {
		t.Fatalf("read stowed location: %v", err)
	}
	return domain.LocationID(loc)
}

// ---------------------------------------------------------------------------
// Creation: H11 and L4
// ---------------------------------------------------------------------------

// TestCreationRecordsAnEvent is why Holdings are created here rather than in
// origin: their state is verified, and verification needs an origin independent
// of the state under test.
func TestCreationRecordsAnEvent(t *testing.T) {
	f := newFixture(t)

	events, err := f.p.History(f.ctx, domain.SubjectHolding, int64(f.rice))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	created, ok := events[0].(domain.HoldingCreated)
	if !ok {
		t.Fatalf("first event is %T, want HoldingCreated", events[0])
	}
	if created.StowedLocation != f.pantry {
		t.Errorf("stowed location = %d, want %d", created.StowedLocation, f.pantry)
	}

	locEvents, err := f.p.History(f.ctx, domain.SubjectLocation, int64(f.pantry))
	if err != nil {
		t.Fatalf("location history: %v", err)
	}
	if len(locEvents) != 1 {
		t.Fatalf("got %d location events, want 1", len(locEvents))
	}
	if _, ok := locEvents[0].(domain.NodeCreated); !ok {
		t.Errorf("first location event is %T, want NodeCreated", locEvents[0])
	}
}

// TestNoOrphans is H11 and L4 stated as a check: both are upheld
// transactionally, so a non-empty result means something wrote outside the
// ledger.
func TestNoOrphans(t *testing.T) {
	f := newFixture(t)
	f.apply(t, domain.Acquired{Holding: f.rice, Delta: domain.FromMilli(2_000_000)})

	orphans, err := f.p.FindOrphans(f.ctx)
	if err != nil {
		t.Fatalf("find orphans: %v", err)
	}
	if len(orphans.Holdings) != 0 || len(orphans.Locations) != 0 {
		t.Errorf("orphans: %+v, want none", orphans)
	}
}

// TestOrphanDetectionCatchesABypass proves the check is not vacuous: a Holding
// inserted without going through the ledger must be reported.
func TestOrphanDetectionCatchesABypass(t *testing.T) {
	f := newFixture(t)

	if _, err := f.conn.Exec(
		`INSERT INTO holdings (item_id, kind, stowed_location_id) VALUES (?, 'Bulk', ?)`,
		int64(f.riceItem), int64(f.pantry)); err != nil {
		t.Fatalf("bypass insert: %v", err)
	}

	orphans, err := f.p.FindOrphans(f.ctx)
	if err != nil {
		t.Fatalf("find orphans: %v", err)
	}
	if len(orphans.Holdings) != 1 {
		t.Errorf("got %d orphan holdings, want 1", len(orphans.Holdings))
	}
}

// ---------------------------------------------------------------------------
// Every event type persists and reads back
// ---------------------------------------------------------------------------

// TestEveryEventTypePersists walks the generated registry and proves each type
// has a payload writer. It is the persistence counterpart of
// TestFoldHandlesEveryEventType: the 13 shape tables exist to force an explicit
// remapping, and this is what makes "forced" true rather than hoped for.
func TestEveryEventTypePersists(t *testing.T) {
	if len(domain.AllEventTypes) != 24 {
		t.Fatalf("registry has %d types, want 24", len(domain.AllEventTypes))
	}
	for _, proto := range domain.AllEventTypes {
		t.Run(fmt.Sprintf("%T", proto), func(t *testing.T) {
			f := newFixture(t)
			e := sampleEvent(t, f, proto)
			if e == nil {
				// Only creation events land here: they happen exactly once, as
				// part of CreateHolding/CreateLocation, so there is no way to
				// apply a second one. TestCreationRecordsAnEvent covers both.
				switch proto.(type) {
				case domain.HoldingCreated, domain.NodeCreated:
					t.Skip("creation happens once; covered by TestCreationRecordsAnEvent")
				default:
					t.Fatalf("%T has no sample event, so its payload writer is untested", proto)
				}
			}
			if _, err := f.p.Apply(f.ctx, e); err != nil {
				if errors.Is(err, ledger.ErrUnpersistable) {
					t.Fatalf("no payload writer for %T", proto)
				}
				t.Fatalf("apply %s: %v", e.Type(), err)
			}
		})
	}
}

// sampleEvent builds a valid instance of the given event type against the
// fixture. Returning nil means the type needs setup this helper does not do.
func sampleEvent(t *testing.T, f *fixture, proto domain.Event) domain.Event {
	t.Helper()

	// Most Bulk events need stock, and most Unique events need to be checked out
	// first, so seed both.
	f.apply(t, domain.Acquired{Holding: f.rice, Delta: domain.FromMilli(1_000_000)})

	switch proto.(type) {
	case domain.HoldingCreated:
		return nil // covered by TestCreationRecordsAnEvent; creation happens once
	case domain.Acquired:
		return domain.Acquired{Holding: f.rice, Delta: domain.FromMilli(500), Source: "shop"}
	case domain.Moved:
		return domain.Moved{Holding: f.rice, From: f.pantry, To: f.garage}
	case domain.Rehomed:
		return domain.Rehomed{Holding: f.rice, From: f.pantry, To: f.garage}
	case domain.Consumed:
		return domain.Consumed{Holding: f.rice, Delta: domain.FromMilli(-100), Reason: "dinner"}
	case domain.Discarded:
		return domain.Discarded{Holding: f.rice, Delta: domain.FromMilli(-100), Reason: "spoiled"}
	case domain.Opened:
		return domain.Opened{Holding: f.rice, Delta: domain.FromMilli(2_000_000)}
	case domain.Split:
		return domain.Split{Holding: f.rice, Delta: domain.FromMilli(-100)}
	case domain.Merged:
		return domain.Merged{Holding: f.rice, Delta: domain.FromMilli(100)}
	case domain.Adjusted:
		return domain.Adjusted{Holding: f.rice, Delta: domain.FromMilli(-5), Reason: "recount"}
	case domain.Counted:
		return domain.Counted{Holding: f.rice, Observed: domain.FromMilli(999_000)}
	case domain.Gone:
		return domain.Gone{Holding: f.rice, Reason: "donated"}
	case domain.CheckedOut:
		return domain.CheckedOut{Holding: f.cable, DisplacedTo: &f.garage}
	case domain.Returned:
		f.apply(t, domain.CheckedOut{Holding: f.cable})
		return domain.Returned{Holding: f.cable}
	case domain.MarkedLost:
		return domain.MarkedLost{Holding: f.cable}
	case domain.Found:
		f.apply(t, domain.MarkedLost{Holding: f.cable})
		return domain.Found{Holding: f.cable}
	case domain.Verified:
		return domain.Verified{Holding: f.cable, Present: true}
	case domain.NodeCreated:
		return nil // creation happens once, in CreateLocation
	case domain.NodeReparented:
		return domain.NodeReparented{Location: f.garage, ToParent: &f.pantry}
	case domain.NodeArchived:
		return domain.NodeArchived{Location: f.garage, Resolution: domain.ResolutionLift}
	case domain.NodeRestored:
		return domain.NodeRestored{Location: f.garage}
	case domain.ItemKindChanged:
		// Applied to the spare Item, which has no Holdings. Against an Item that
		// does, the composite foreign key refuses — that is the schema enforcing
		// that Promote replaces Holdings rather than mutating them, and it has
		// its own test.
		return domain.ItemKindChanged{Item: f.spare, FromKind: domain.KindBulk, ToKind: domain.KindUnique}
	case domain.ItemUnitChanged:
		g, kg := domain.UnitCode("g"), domain.UnitCode("kg")
		return domain.ItemUnitChanged{Item: f.riceItem, FromUnit: &g, ToUnit: &kg}
	case domain.ItemPackageSizeChanged:
		from, to := domain.FromMilli(2_000_000), domain.FromMilli(1_000_000)
		return domain.ItemPackageSizeChanged{Item: f.riceItem, FromSize: &from, ToSize: &to}
	default:
		return nil
	}
}

// TestEventRoundTrip is what the 13 shape tables are for: every field written
// must come back. A payload silently dropped on write would show up here as a
// zero value rather than as a passing test.
func TestEventRoundTrip(t *testing.T) {
	f := newFixture(t)
	price := int64(499)
	twoKg := domain.FromMilli(2_000_000)

	written := []domain.Event{
		domain.Acquired{Holding: f.rice, Delta: twoKg, Source: "corner shop", Price: &price},
		domain.Consumed{Holding: f.rice, Delta: domain.FromMilli(-100_000), Reason: "dinner"},
		domain.Moved{Holding: f.rice, From: f.pantry, To: f.garage},
		domain.Counted{Holding: f.rice, Observed: domain.FromMilli(1_899_000)},
		domain.Adjusted{Holding: f.rice, Delta: domain.FromMilli(-1_000), Reason: "recount"},
		domain.Gone{Holding: f.rice, Reason: "donated"},
	}
	for _, e := range written {
		f.apply(t, e)
	}

	read, err := f.p.History(f.ctx, domain.SubjectHolding, int64(f.rice))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	// Creation plus the six above.
	if len(read) != len(written)+1 {
		t.Fatalf("got %d events, want %d", len(read), len(written)+1)
	}

	acquired, ok := read[1].(domain.Acquired)
	if !ok {
		t.Fatalf("event 1 is %T, want Acquired", read[1])
	}
	if acquired.Delta.Cmp(twoKg) != 0 {
		t.Errorf("delta = %s, want %s", acquired.Delta, twoKg)
	}
	if acquired.Source != "corner shop" {
		t.Errorf("source = %q, want %q", acquired.Source, "corner shop")
	}
	if acquired.Price == nil || *acquired.Price != price {
		t.Errorf("price = %v, want %d", acquired.Price, price)
	}

	consumed, ok := read[2].(domain.Consumed)
	if !ok {
		t.Fatalf("event 2 is %T, want Consumed", read[2])
	}
	if consumed.Reason != "dinner" {
		t.Errorf("reason = %q, want dinner", consumed.Reason)
	}

	moved, ok := read[3].(domain.Moved)
	if !ok {
		t.Fatalf("event 3 is %T, want Moved", read[3])
	}
	if moved.From != f.pantry || moved.To != f.garage {
		t.Errorf("moved %d->%d, want %d->%d", moved.From, moved.To, f.pantry, f.garage)
	}

	counted, ok := read[4].(domain.Counted)
	if !ok {
		t.Fatalf("event 4 is %T, want Counted", read[4])
	}
	if counted.Observed.Cmp(domain.FromMilli(1_899_000)) != 0 {
		t.Errorf("observed = %s", counted.Observed)
	}

	gone, ok := read[6].(domain.Gone)
	if !ok {
		t.Fatalf("event 6 is %T, want Gone", read[6])
	}
	if gone.Reason != "donated" {
		t.Errorf("reason = %q, want donated", gone.Reason)
	}
}

// TestEventIDsAreMonotonicAcrossSubjects: the sequence is global, so events for
// different subjects still have a total order.
func TestEventIDsAreMonotonicAcrossSubjects(t *testing.T) {
	f := newFixture(t)

	a := f.apply(t, domain.Acquired{Holding: f.rice, Delta: domain.FromMilli(100)})
	b := f.apply(t, domain.CheckedOut{Holding: f.cable})
	c := f.apply(t, domain.Consumed{Holding: f.rice, Delta: domain.FromMilli(-50)})

	if !(a < b && b < c) {
		t.Errorf("event ids %d, %d, %d are not strictly increasing", a, b, c)
	}
}

// ---------------------------------------------------------------------------
// O3: the write and its event are one unit
// ---------------------------------------------------------------------------

// TestProjectionAndEventLandTogether is O3, and the reason H10 can only ever
// report incompleteness. A failure anywhere in the unit must leave nothing
// behind — neither a projection change without its event, nor the reverse.
func TestProjectionAndEventLandTogether(t *testing.T) {
	f := newFixture(t)
	f.apply(t, domain.Acquired{Holding: f.rice, Delta: domain.FromMilli(1_000_000)})

	before := f.quantityOf(t, f.rice)
	eventsBefore := f.countEvents(t)

	// A Moved to a location that does not exist: the fold succeeds, then the
	// payload insert violates a foreign key.
	_, err := f.p.Apply(f.ctx, domain.Moved{
		Holding: f.rice, From: f.pantry, To: domain.LocationID(999999),
	})
	if err == nil {
		t.Fatal("expected a foreign key violation")
	}

	if after := f.quantityOf(t, f.rice); after.Cmp(before) != 0 {
		t.Errorf("quantity changed from %s to %s despite a failed apply", before, after)
	}
	if got := f.stowedAt(t, f.rice); got != f.pantry {
		t.Errorf("stowed location moved to %d despite a failed apply", got)
	}
	if after := f.countEvents(t); after != eventsBefore {
		t.Errorf("event count went %d -> %d despite a failed apply", eventsBefore, after)
	}
}

func (f *fixture) countEvents(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.conn.QueryRow("SELECT count(*) FROM events").Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

// TestApplyBatchIsAtomic is O2's mechanism: some facts are only true together.
func TestApplyBatchIsAtomic(t *testing.T) {
	f := newFixture(t)
	before := f.countEvents(t)

	_, err := f.p.ApplyBatch(f.ctx, []domain.Event{
		domain.Acquired{Holding: f.rice, Delta: domain.FromMilli(1_000)},
		// Invalid: a negative delta on Acquired, rejected by the fold.
		domain.Acquired{Holding: f.rice, Delta: domain.FromMilli(-1_000)},
	})
	if err == nil {
		t.Fatal("expected the batch to fail")
	}
	if got := f.countEvents(t); got != before {
		t.Errorf("event count went %d -> %d; the first event of a failed batch survived", before, got)
	}
	if q := f.quantityOf(t, f.rice); !q.IsZero() {
		t.Errorf("quantity = %s, want 0; the first event of a failed batch was applied", q)
	}
}

// ---------------------------------------------------------------------------
// Projections follow the fold
// ---------------------------------------------------------------------------

func TestQuantityProjectionFollowsEvents(t *testing.T) {
	f := newFixture(t)

	f.apply(t, domain.Acquired{Holding: f.rice, Delta: domain.FromMilli(2_000_000)})
	f.apply(t, domain.Consumed{Holding: f.rice, Delta: domain.FromMilli(-100_000)})
	f.apply(t, domain.Consumed{Holding: f.rice, Delta: domain.FromMilli(-100_000)})

	want := domain.FromMilli(1_800_000)
	if got := f.quantityOf(t, f.rice); got.Cmp(want) != 0 {
		t.Errorf("quantity = %s, want %s", got, want)
	}
}

func TestCustodyProjectionFollowsEvents(t *testing.T) {
	f := newFixture(t)

	f.apply(t, domain.CheckedOut{Holding: f.cable, DisplacedTo: &f.garage})
	custody, since, displaced := f.custodyOf(t, f.cable)
	if custody != string(domain.CustodyOut) {
		t.Errorf("custody = %s, want Out", custody)
	}
	if !since.Valid {
		t.Error("custody_since is null while Out - HU1 violated")
	}
	if !displaced.Valid || domain.LocationID(displaced.Int64) != f.garage {
		t.Errorf("displaced_to = %v, want %d", displaced, f.garage)
	}

	f.apply(t, domain.Returned{Holding: f.cable})
	custody, since, displaced = f.custodyOf(t, f.cable)
	if custody != string(domain.CustodyAtRest) {
		t.Errorf("custody = %s, want AtRest", custody)
	}
	if since.Valid || displaced.Valid {
		t.Error("custody_since or displaced_to set while AtRest - HU1/HU2 violated")
	}
}

// TestCountedLeavesTheProjectionAlone: an observation records what was seen and
// never silently corrects the books. The correction is a separate Adjusted.
func TestCountedLeavesTheProjectionAlone(t *testing.T) {
	f := newFixture(t)
	f.apply(t, domain.Acquired{Holding: f.rice, Delta: domain.FromMilli(500_000)})

	f.apply(t, domain.Counted{Holding: f.rice, Observed: domain.FromMilli(480_000)})
	if got := f.quantityOf(t, f.rice); got.Cmp(domain.FromMilli(500_000)) != 0 {
		t.Errorf("quantity = %s after a disagreeing count, want 500000 unchanged", got)
	}

	f.apply(t, domain.Adjusted{Holding: f.rice, Delta: domain.FromMilli(-20_000), Reason: "recount"})
	if got := f.quantityOf(t, f.rice); got.Cmp(domain.FromMilli(480_000)) != 0 {
		t.Errorf("quantity = %s after the adjustment, want 480000", got)
	}
}

// ---------------------------------------------------------------------------
// Legality: events partition by kind
// ---------------------------------------------------------------------------

func TestEventsRejectedAgainstTheWrongKind(t *testing.T) {
	f := newFixture(t)

	if _, err := f.p.Apply(f.ctx, domain.CheckedOut{Holding: f.rice}); !errors.Is(err, domain.ErrWrongKind) {
		t.Errorf("CheckedOut against a Bulk holding: err = %v, want ErrWrongKind", err)
	}
	if _, err := f.p.Apply(f.ctx, domain.Consumed{
		Holding: f.cable, Delta: domain.FromMilli(-1),
	}); !errors.Is(err, domain.ErrWrongKind) {
		t.Errorf("Consumed against a Unique holding: err = %v, want ErrWrongKind", err)
	}
	// And the rejection must be total: only the four creation events from the
	// fixture (two locations, two holdings) should exist.
	if n := f.countEvents(t); n != 4 {
		t.Errorf("event count = %d, want 4; a rejected event was still appended", n)
	}
}

func TestRetiredHoldingsAcceptNothing(t *testing.T) {
	f := newFixture(t)
	f.apply(t, domain.Acquired{Holding: f.rice, Delta: domain.FromMilli(1_000)})
	f.apply(t, domain.Gone{Holding: f.rice, Reason: "donated"})

	if _, err := f.p.Apply(f.ctx, domain.Consumed{
		Holding: f.rice, Delta: domain.FromMilli(-1),
	}); !errors.Is(err, domain.ErrRetired) {
		t.Errorf("err = %v, want ErrRetired", err)
	}
}

func TestQuantityCannotGoNegativeThroughTheLedger(t *testing.T) {
	f := newFixture(t)
	f.apply(t, domain.Acquired{Holding: f.rice, Delta: domain.FromMilli(100)})

	if _, err := f.p.Apply(f.ctx, domain.Consumed{
		Holding: f.rice, Delta: domain.FromMilli(-101),
	}); !errors.Is(err, domain.ErrNegativeQuantity) {
		t.Errorf("err = %v, want ErrNegativeQuantity", err)
	}
	if got := f.quantityOf(t, f.rice); got.Cmp(domain.FromMilli(100)) != 0 {
		t.Errorf("quantity = %s, want 100 unchanged", got)
	}
}

// ---------------------------------------------------------------------------
// Locations
// ---------------------------------------------------------------------------

// TestReparentIsAccountable is the reason the Location ledger exists. The
// contents' own stowed_location never changes, so without this event something
// moved and nothing recorded why.
func TestReparentIsAccountable(t *testing.T) {
	f := newFixture(t)

	tote, err := f.p.CreateLocation(f.ctx, "Storage Tote #3", &f.pantry, "")
	if err != nil {
		t.Fatalf("create tote: %v", err)
	}
	coat, err := f.p.CreateUniqueHolding(f.ctx, ledger.CreateUniqueHoldingInput{
		Item: f.cableID, Location: tote,
	})
	if err != nil {
		t.Fatalf("create holding in tote: %v", err)
	}

	if err := f.p.ReparentLocation(f.ctx, tote, &f.garage); err != nil {
		t.Fatalf("reparent: %v", err)
	}

	// The Holding did not move: its stowed location is still the tote.
	if got := f.stowedAt(t, coat); got != tote {
		t.Errorf("holding stowed at %d, want the tote %d", got, tote)
	}
	// But the ledger accounts for the apparent move.
	events, err := f.p.History(f.ctx, domain.SubjectLocation, int64(tote))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d location events, want 2", len(events))
	}
	reparented, ok := events[1].(domain.NodeReparented)
	if !ok {
		t.Fatalf("event 1 is %T, want NodeReparented", events[1])
	}
	if reparented.FromParent == nil || *reparented.FromParent != f.pantry {
		t.Errorf("from parent = %v, want %d", reparented.FromParent, f.pantry)
	}
	if reparented.ToParent == nil || *reparented.ToParent != f.garage {
		t.Errorf("to parent = %v, want %d", reparented.ToParent, f.garage)
	}
}

func TestReparentLocationRejectsCycles(t *testing.T) {
	f := newFixture(t)
	shelf, err := f.p.CreateLocation(f.ctx, "Shelf", &f.pantry, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := f.p.ReparentLocation(f.ctx, f.pantry, &shelf); !errors.Is(err, ledger.ErrCycle) {
		t.Errorf("err = %v, want ErrCycle", err)
	}
	if err := f.p.ReparentLocation(f.ctx, f.pantry, &f.pantry); !errors.Is(err, ledger.ErrCycle) {
		t.Errorf("self parent: err = %v, want ErrCycle", err)
	}
}

// ---------------------------------------------------------------------------
// Item typing
// ---------------------------------------------------------------------------

// TestKindChangeIsRefusedWhileHoldingsExist is the schema enforcing that Promote
// REPLACES Holdings rather than mutating them: kind and unit_basis are
// immutable, so the composite foreign key blocks the change.
func TestKindChangeIsRefusedWhileHoldingsExist(t *testing.T) {
	f := newFixture(t)

	_, err := f.p.Apply(f.ctx, domain.ItemKindChanged{
		Item: f.riceItem, FromKind: domain.KindBulk, ToKind: domain.KindUnique,
	})
	if err == nil {
		t.Fatal("expected the composite foreign key to refuse the kind change")
	}

	var kind string
	if err := f.conn.QueryRow("SELECT kind FROM items WHERE id = ?", int64(f.riceItem)).Scan(&kind); err != nil {
		t.Fatalf("read kind: %v", err)
	}
	if kind != string(domain.KindBulk) {
		t.Errorf("kind = %s, want Bulk unchanged", kind)
	}
}

// TestPromotionRecordsTheDiscardedDefinition: recording only "kind changed"
// destroys content_unit and package_size with no record, which breaks the claim
// that an Item's history closes backwards.
func TestPromotionRecordsTheDiscardedDefinition(t *testing.T) {
	f := newFixture(t)
	g := domain.UnitCode("g")
	size := domain.FromMilli(2_000_000)

	// The Item events alone, without the Holding restructuring that a real
	// Promote would also perform.
	if _, err := f.p.ApplyBatch(f.ctx, []domain.Event{
		domain.ItemUnitChanged{Item: f.riceItem, FromUnit: &g, ToUnit: nil},
		domain.ItemPackageSizeChanged{Item: f.riceItem, FromSize: &size, ToSize: nil},
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	events, err := f.p.History(f.ctx, domain.SubjectItem, int64(f.riceItem))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d item events, want 2", len(events))
	}

	unit, ok := events[0].(domain.ItemUnitChanged)
	if !ok {
		t.Fatalf("event 0 is %T, want ItemUnitChanged", events[0])
	}
	// The discarded definition survives, which is what makes the history
	// reconstructible.
	if unit.FromUnit == nil || *unit.FromUnit != g {
		t.Errorf("from unit = %v, want g", unit.FromUnit)
	}
	if unit.ToUnit != nil {
		t.Errorf("to unit = %v, want nil", unit.ToUnit)
	}

	pkg, ok := events[1].(domain.ItemPackageSizeChanged)
	if !ok {
		t.Fatalf("event 1 is %T, want ItemPackageSizeChanged", events[1])
	}
	if pkg.FromSize == nil || pkg.FromSize.Cmp(size) != 0 {
		t.Errorf("from size = %v, want %s", pkg.FromSize, size)
	}
	if pkg.ToSize != nil {
		t.Errorf("to size = %v, want nil", pkg.ToSize)
	}
}

// ---------------------------------------------------------------------------
// Immutability, through the production path
// ---------------------------------------------------------------------------

// TestLedgerHasNoMutationPath: the ledger package exposes append and read, and
// nothing else. The triggers are the backstop; this is the API-level statement.
func TestLedgerHasNoMutationPath(t *testing.T) {
	f := newFixture(t)
	id := f.apply(t, domain.Acquired{Holding: f.rice, Delta: domain.FromMilli(1_000)})

	// Raw SQL is the only way to attempt it, and the trigger refuses.
	if _, err := f.conn.Exec("UPDATE events SET note = 'revised' WHERE id = ?", int64(id)); err == nil {
		t.Error("updating an event succeeded; E1 is not enforced")
	}
	if _, err := f.conn.Exec("DELETE FROM events WHERE id = ?", int64(id)); err == nil {
		t.Error("deleting an event succeeded; E1 is not enforced")
	}
}

var _ = db.FormatTime // keep the db import meaningful if helpers are trimmed
