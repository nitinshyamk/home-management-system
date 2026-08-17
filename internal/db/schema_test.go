package db_test

// Stage 2: data-layer invariant tests.
//
// These assert that the SCHEMA rejects violating writes — before any domain
// code exists. Every attempt goes through a generated, typed interface: the
// `probe` package, whose queries take every constrained value as a parameter
// precisely so a violation can be expressed. Production queries cannot express
// one (that is their job), so they cannot prove the database rejects it.
//
// STRUCTURAL invariants are deliberately absent from this file. No INSERT can
// put a `quantity` on `unique_holdings` because the column does not exist —
// there is nothing to test and no test could express the violation.

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"home-management-system/internal/db/probe"
	"home-management-system/internal/testsupport"
)

// fixture holds valid rows for violations to be attempted against.
type fixture struct {
	q          *probe.Queries
	ctx        context.Context
	category   int64
	location   int64
	uniqueItem int64
	bulkItem   int64
	uniqueHold int64
	bulkHold   int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	conn := testsupport.NewDB(t)
	f := &fixture{q: probe.New(conn), ctx: context.Background()}

	var err error
	if f.category, err = f.q.NewCategory(f.ctx, "Spices"); err != nil {
		t.Fatalf("fixture category: %v", err)
	}
	if f.location, err = f.q.NewLocation(f.ctx, "Pantry"); err != nil {
		t.Fatalf("fixture location: %v", err)
	}
	if f.uniqueItem, err = f.q.NewItem(f.ctx, probe.NewItemParams{
		Kind: "Unique", Name: "USB-C Cable", CategoryID: f.category,
	}); err != nil {
		t.Fatalf("fixture unique item: %v", err)
	}
	if f.bulkItem, err = f.q.NewItem(f.ctx, probe.NewItemParams{
		Kind: "Bulk", Name: "Basmati Rice", CategoryID: f.category,
	}); err != nil {
		t.Fatalf("fixture bulk item: %v", err)
	}
	if f.uniqueHold, err = f.q.NewHolding(f.ctx, probe.NewHoldingParams{
		ItemID: f.uniqueItem, Kind: "Unique", StowedLocationID: f.location,
	}); err != nil {
		t.Fatalf("fixture unique holding: %v", err)
	}
	if f.bulkHold, err = f.q.NewHolding(f.ctx, probe.NewHoldingParams{
		ItemID: f.bulkItem, Kind: "Bulk", StowedLocationID: f.location,
	}); err != nil {
		t.Fatalf("fixture bulk holding: %v", err)
	}
	return f
}

// newEvent creates an event of the given type for violations to hang off.
func (f *fixture) newEvent(t *testing.T, subjectKind string, subjectID int64, eventType string) int64 {
	t.Helper()
	id, err := f.q.NewEvent(f.ctx, probe.NewEventParams{
		SubjectKind: subjectKind, SubjectID: subjectID,
		Type: eventType, OccurredAt: "2026-08-16T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("create %s event: %v", eventType, err)
	}
	return id
}

// rejects asserts the write failed. A passing write here means an invariant the
// design depends on is not actually enforced.
func rejects(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: accepted, want rejected", what)
	}
}

func accepts(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: rejected (%v), want accepted", what, err)
	}
}

func nullInt(v int64) sql.NullInt64   { return sql.NullInt64{Int64: v, Valid: true} }
func nullStr(v string) sql.NullString { return sql.NullString{String: v, Valid: true} }

// ---------------------------------------------------------------------------
// Variant correspondence — I2, H3, H4. The composite-FK pattern.
// ---------------------------------------------------------------------------

func TestVariantRowCannotAttachToWrongKind(t *testing.T) {
	f := newFixture(t)

	// I2: a bulk variant row against an item declared Unique.
	rejects(t, "bulk_items against a Unique item", f.q.AttachBulkItem(f.ctx, probe.AttachBulkItemParams{
		ItemID: f.uniqueItem, Kind: "Bulk", ContentUnit: "g",
	}))
	// I2: a unique variant row against an item declared Bulk.
	rejects(t, "unique_items against a Bulk item", f.q.AttachUniqueItem(f.ctx, probe.AttachUniqueItemParams{
		ItemID: f.bulkItem, Kind: "Unique",
	}))
	// The CHECK on the variant's own kind column.
	rejects(t, "bulk_items claiming kind Unique", f.q.AttachBulkItem(f.ctx, probe.AttachBulkItemParams{
		ItemID: f.bulkItem, Kind: "Unique", ContentUnit: "g",
	}))

	// The matching pairs must work, or the tests above prove nothing.
	accepts(t, "unique_items against a Unique item", f.q.AttachUniqueItem(f.ctx, probe.AttachUniqueItemParams{
		ItemID: f.uniqueItem, Kind: "Unique",
	}))
	accepts(t, "bulk_items against a Bulk item", f.q.AttachBulkItem(f.ctx, probe.AttachBulkItemParams{
		ItemID: f.bulkItem, Kind: "Bulk", ContentUnit: "g",
	}))
}

func TestHoldingKindMustMatchItemKind(t *testing.T) {
	f := newFixture(t)

	// H3: a Unique holding of a Bulk item.
	_, err := f.q.NewHolding(f.ctx, probe.NewHoldingParams{
		ItemID: f.bulkItem, Kind: "Unique", StowedLocationID: f.location,
	})
	rejects(t, "Unique holding of a Bulk item", err)

	_, err = f.q.NewHolding(f.ctx, probe.NewHoldingParams{
		ItemID: f.uniqueItem, Kind: "Bulk", StowedLocationID: f.location,
	})
	rejects(t, "Bulk holding of a Unique item", err)
}

func TestHoldingVariantCannotAttachToWrongKind(t *testing.T) {
	f := newFixture(t)

	// H4.
	rejects(t, "bulk_holdings against a Unique holding", f.q.AttachBulkHolding(f.ctx, probe.AttachBulkHoldingParams{
		HoldingID: f.uniqueHold, Kind: "Bulk", Quantity: 1, UnitBasis: "Content",
	}))
	rejects(t, "unique_holdings against a Bulk holding", f.q.AttachUniqueHolding(f.ctx, probe.AttachUniqueHoldingParams{
		HoldingID: f.bulkHold, Kind: "Unique", Custody: "AtRest",
	}))

	accepts(t, "bulk_holdings against a Bulk holding", f.q.AttachBulkHolding(f.ctx, probe.AttachBulkHoldingParams{
		HoldingID: f.bulkHold, Kind: "Bulk", Quantity: 0, UnitBasis: "Content",
	}))
	accepts(t, "unique_holdings against a Unique holding", f.q.AttachUniqueHolding(f.ctx, probe.AttachUniqueHoldingParams{
		HoldingID: f.uniqueHold, Kind: "Unique", Custody: "AtRest",
	}))
}

func TestAtMostOneVariantRow(t *testing.T) {
	f := newFixture(t)

	accepts(t, "first bulk_items row", f.q.AttachBulkItem(f.ctx, probe.AttachBulkItemParams{
		ItemID: f.bulkItem, Kind: "Bulk", ContentUnit: "g",
	}))
	// The declarative half of I3. "At least one" is checked, and belongs later.
	rejects(t, "second bulk_items row for one item", f.q.AttachBulkItem(f.ctx, probe.AttachBulkItemParams{
		ItemID: f.bulkItem, Kind: "Bulk", ContentUnit: "kg",
	}))
}

// ---------------------------------------------------------------------------
// Payload correspondence — E5. The same composite-FK pattern, third use.
// ---------------------------------------------------------------------------

func TestPayloadCannotAttachToWrongEventType(t *testing.T) {
	f := newFixture(t)
	consumed := f.newEvent(t, "Holding", f.bulkHold, "Consumed")

	// E5: a Consumed event carrying a custody payload.
	rejects(t, "ev_custody on a Consumed event", f.q.AttachCustodyPayload(f.ctx, probe.AttachCustodyPayloadParams{
		EventID: consumed, Type: "CheckedOut", ToCustody: "Out",
	}))
	// The payload's own type CHECK: right table, wrong type value.
	rejects(t, "ev_quantity claiming type CheckedOut", f.q.AttachQuantityPayload(f.ctx, probe.AttachQuantityPayloadParams{
		EventID: consumed, Type: "CheckedOut", Delta: -100,
	}))
	// Right table, right type, but not this event's type.
	rejects(t, "ev_quantity claiming Split on a Consumed event", f.q.AttachQuantityPayload(f.ctx, probe.AttachQuantityPayloadParams{
		EventID: consumed, Type: "Split", Delta: -100,
	}))

	accepts(t, "ev_quantity on a Consumed event", f.q.AttachQuantityPayload(f.ctx, probe.AttachQuantityPayloadParams{
		EventID: consumed, Type: "Consumed", Delta: -100,
	}))
}

func TestEventSubjectKindMustMatchTypeFamily(t *testing.T) {
	f := newFixture(t)

	_, err := f.q.NewEvent(f.ctx, probe.NewEventParams{
		SubjectKind: "Holding", SubjectID: f.bulkHold,
		Type: "NodeReparented", OccurredAt: "2026-08-16T00:00:00Z",
	})
	rejects(t, "a Location event with subject_kind Holding", err)

	_, err = f.q.NewEvent(f.ctx, probe.NewEventParams{
		SubjectKind: "Item", SubjectID: f.bulkItem,
		Type: "Consumed", OccurredAt: "2026-08-16T00:00:00Z",
	})
	rejects(t, "a Holding event with subject_kind Item", err)

	_, err = f.q.NewEvent(f.ctx, probe.NewEventParams{
		SubjectKind: "Holding", SubjectID: f.bulkHold,
		Type: "Frobnicated", OccurredAt: "2026-08-16T00:00:00Z",
	})
	rejects(t, "an unknown event type", err)
}

// TestCreationIsTheOnlyPlacementFromNowhere encodes the rule that makes
// HoldingCreated distinguishable from an ordinary Moved at the data layer.
func TestCreationIsTheOnlyPlacementFromNowhere(t *testing.T) {
	f := newFixture(t)
	created := f.newEvent(t, "Holding", f.bulkHold, "HoldingCreated")
	moved := f.newEvent(t, "Holding", f.bulkHold, "Moved")

	accepts(t, "HoldingCreated with a null origin", f.q.AttachPlacementPayload(f.ctx, probe.AttachPlacementPayloadParams{
		EventID: created, Type: "HoldingCreated", ToLocationID: f.location,
	}))
	rejects(t, "HoldingCreated with an origin", f.q.AttachPlacementPayload(f.ctx, probe.AttachPlacementPayloadParams{
		EventID: created, Type: "HoldingCreated",
		FromLocationID: nullInt(f.location), ToLocationID: f.location,
	}))
	rejects(t, "Moved with a null origin", f.q.AttachPlacementPayload(f.ctx, probe.AttachPlacementPayloadParams{
		EventID: moved, Type: "Moved", ToLocationID: f.location,
	}))
	accepts(t, "Moved with an origin", f.q.AttachPlacementPayload(f.ctx, probe.AttachPlacementPayloadParams{
		EventID: moved, Type: "Moved",
		FromLocationID: nullInt(f.location), ToLocationID: f.location,
	}))
}

// ---------------------------------------------------------------------------
// Value constraints — I4, H6, HU1, HU2, and the enum CHECKs.
// ---------------------------------------------------------------------------

func TestPackageSizeMustBePositive(t *testing.T) {
	f := newFixture(t)

	for _, size := range []int64{0, -1} {
		rejects(t, "package_size not positive", f.q.AttachBulkItem(f.ctx, probe.AttachBulkItemParams{
			ItemID: f.bulkItem, Kind: "Bulk", ContentUnit: "g", PackageSize: nullInt(size),
		}))
	}
	accepts(t, "package_size absent", f.q.AttachBulkItem(f.ctx, probe.AttachBulkItemParams{
		ItemID: f.bulkItem, Kind: "Bulk", ContentUnit: "g",
	}))
}

func TestQuantityMayBeZeroButNotNegative(t *testing.T) {
	f := newFixture(t)

	rejects(t, "negative quantity", f.q.AttachBulkHolding(f.ctx, probe.AttachBulkHoldingParams{
		HoldingID: f.bulkHold, Kind: "Bulk", Quantity: -1, UnitBasis: "Content",
	}))
	// Zero is legal: depletion is a normal end state, not a violation.
	accepts(t, "zero quantity", f.q.AttachBulkHolding(f.ctx, probe.AttachBulkHoldingParams{
		HoldingID: f.bulkHold, Kind: "Bulk", Quantity: 0, UnitBasis: "Content",
	}))
}

func TestCustodyStateConsistency(t *testing.T) {
	cases := []struct {
		name         string
		custody      string
		since        sql.NullString
		displaced    sql.NullInt64
		wantRejected bool
	}{
		{"AtRest with no timestamp", "AtRest", sql.NullString{}, sql.NullInt64{}, false},
		{"Out with a timestamp", "Out", nullStr("2026-08-16T00:00:00Z"), sql.NullInt64{}, false},
		{"Lost with a timestamp", "Lost", nullStr("2026-08-16T00:00:00Z"), sql.NullInt64{}, false},
		// HU1: AtRest and custody_since must agree, in both directions.
		{"AtRest with a timestamp", "AtRest", nullStr("2026-08-16T00:00:00Z"), sql.NullInt64{}, true},
		{"Out with no timestamp", "Out", sql.NullString{}, sql.NullInt64{}, true},
		{"unknown custody state", "Wandering", sql.NullString{}, sql.NullInt64{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			err := f.q.AttachUniqueHolding(f.ctx, probe.AttachUniqueHoldingParams{
				HoldingID: f.uniqueHold, Kind: "Unique",
				Custody: tc.custody, CustodySince: tc.since, DisplacedToID: tc.displaced,
			})
			if tc.wantRejected {
				rejects(t, tc.name, err)
			} else {
				accepts(t, tc.name, err)
			}
		})
	}
}

func TestDisplacementOnlyWhileOut(t *testing.T) {
	f := newFixture(t)

	// HU2: a known displacement is meaningless unless the thing is Out.
	rejects(t, "displaced while Lost", f.q.AttachUniqueHolding(f.ctx, probe.AttachUniqueHoldingParams{
		HoldingID: f.uniqueHold, Kind: "Unique", Custody: "Lost",
		CustodySince: nullStr("2026-08-16T00:00:00Z"), DisplacedToID: nullInt(f.location),
	}))
	accepts(t, "displaced while Out", f.q.AttachUniqueHolding(f.ctx, probe.AttachUniqueHoldingParams{
		HoldingID: f.uniqueHold, Kind: "Unique", Custody: "Out",
		CustodySince: nullStr("2026-08-16T00:00:00Z"), DisplacedToID: nullInt(f.location),
	}))
}

func TestEnumColumnsRejectUnknownValues(t *testing.T) {
	f := newFixture(t)

	_, err := f.q.NewItem(f.ctx, probe.NewItemParams{Kind: "Sundry", Name: "x", CategoryID: f.category})
	rejects(t, "unknown item kind", err)

	rejects(t, "unknown unit_basis", f.q.AttachBulkHolding(f.ctx, probe.AttachBulkHoldingParams{
		HoldingID: f.bulkHold, Kind: "Bulk", Quantity: 1, UnitBasis: "Barrels",
	}))
}

// ---------------------------------------------------------------------------
// Referential integrity.
// ---------------------------------------------------------------------------

func TestForeignKeysRejectMissingParents(t *testing.T) {
	f := newFixture(t)
	const missing = 999999

	_, err := f.q.NewItem(f.ctx, probe.NewItemParams{Kind: "Bulk", Name: "x", CategoryID: missing})
	rejects(t, "item in a nonexistent category", err)

	_, err = f.q.NewHolding(f.ctx, probe.NewHoldingParams{
		ItemID: f.bulkItem, Kind: "Bulk", StowedLocationID: missing,
	})
	rejects(t, "holding at a nonexistent location", err)

	rejects(t, "bulk_items with an unknown unit", f.q.AttachBulkItem(f.ctx, probe.AttachBulkItemParams{
		ItemID: f.bulkItem, Kind: "Bulk", ContentUnit: "furlongs",
	}))
	rejects(t, "payload for a nonexistent event", f.q.AttachQuantityPayload(f.ctx, probe.AttachQuantityPayloadParams{
		EventID: missing, Type: "Consumed", Delta: -1,
	}))
}

func TestReferencedRowsCannotBeDeleted(t *testing.T) {
	f := newFixture(t)

	// E4/RESTRICT: nothing referenced may vanish. The ledger references entities
	// permanently, and an orphaned event is uninterpretable.
	rejects(t, "deleting a category with items", f.q.RemoveCategory(f.ctx, f.category))
	rejects(t, "deleting a location with holdings", f.q.RemoveLocation(f.ctx, f.location))
}

// ---------------------------------------------------------------------------
// Ledger immutability — E1. The 28 triggers.
// ---------------------------------------------------------------------------

func TestLedgerIsAppendOnly(t *testing.T) {
	f := newFixture(t)
	id := f.newEvent(t, "Holding", f.bulkHold, "Consumed")
	accepts(t, "payload insert", f.q.AttachQuantityPayload(f.ctx, probe.AttachQuantityPayloadParams{
		EventID: id, Type: "Consumed", Delta: -100,
	}))

	rejects(t, "updating an event", f.q.MutateEvent(f.ctx, probe.MutateEventParams{
		Note: nullStr("revised"), ID: id,
	}))
	rejects(t, "deleting an event", f.q.RemoveEvent(f.ctx, id))
	rejects(t, "updating a payload", f.q.MutateQuantityPayload(f.ctx, probe.MutateQuantityPayloadParams{
		Delta: -5, EventID: id,
	}))
	rejects(t, "deleting a payload", f.q.RemoveQuantityPayload(f.ctx, id))
}

// TestEveryLedgerTableIsProtected walks sqlite_master rather than trusting that
// 28 hand-checked triggers exist. A payload table added later without triggers
// would otherwise be silently mutable.
func TestEveryLedgerTableIsProtected(t *testing.T) {
	conn := testsupport.NewDB(t)

	rows, err := conn.Query(`
		SELECT name FROM sqlite_master
		WHERE type = 'table' AND (name = 'events' OR name LIKE 'ev\_%' ESCAPE '\')`)
	if err != nil {
		t.Fatalf("list ledger tables: %v", err)
	}
	defer rows.Close()

	var ledgerTables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		ledgerTables = append(ledgerTables, name)
	}
	if len(ledgerTables) != 14 {
		t.Fatalf("found %d ledger tables, want 14 (events + 13 payload shapes)", len(ledgerTables))
	}

	for _, table := range ledgerTables {
		for _, op := range []string{"update", "delete"} {
			var n int
			err := conn.QueryRow(
				`SELECT count(*) FROM sqlite_master WHERE type='trigger' AND name = ?`,
				table+"_no_"+op).Scan(&n)
			if err != nil {
				t.Fatalf("check trigger for %s: %v", table, err)
			}
			if n != 1 {
				t.Errorf("%s has no %s trigger — the table is silently mutable", table, op)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Identity and seed data — E2, U2.
// ---------------------------------------------------------------------------

func TestEventIDsAreMonotonic(t *testing.T) {
	f := newFixture(t)

	for i := 0; i < 5; i++ {
		f.newEvent(t, "Holding", f.bulkHold, "Consumed")
	}
	ids, err := f.q.ListEventIDs(f.ctx)
	if err != nil {
		t.Fatalf("list event ids: %v", err)
	}
	if len(ids) != 5 {
		t.Fatalf("got %d events, want 5", len(ids))
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Errorf("event ids not strictly increasing: %v", ids)
			break
		}
	}
}

func TestUnitsAreSeededWithOneBasePerDimension(t *testing.T) {
	conn := testsupport.NewDB(t)

	var total int
	if err := conn.QueryRow("SELECT count(*) FROM units").Scan(&total); err != nil {
		t.Fatalf("count units: %v", err)
	}
	if total != 7 {
		t.Errorf("units = %d, want 7", total)
	}

	// U2: exactly one unit per dimension is the base.
	rows, err := conn.Query(`
		SELECT dimension, sum(CASE WHEN to_base_factor = 1 THEN 1 ELSE 0 END)
		FROM units GROUP BY dimension`)
	if err != nil {
		t.Fatalf("group units: %v", err)
	}
	defer rows.Close()

	seen := 0
	for rows.Next() {
		var dimension string
		var bases int
		if err := rows.Scan(&dimension, &bases); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen++
		if bases != 1 {
			t.Errorf("dimension %s has %d base units, want exactly 1", dimension, bases)
		}
	}
	if seen != 4 {
		t.Errorf("saw %d dimensions, want 4", seen)
	}
}

// ---------------------------------------------------------------------------
// Schema hygiene.
// ---------------------------------------------------------------------------

// TestNoCascadeInSchema is the runtime companion to archlint's static check.
// Nothing is ever hard-deleted, so a cascade clause can never fire — it is
// documentation pretending to be behaviour, and a footgun the day a delete path
// appears.
func TestNoCascadeInSchema(t *testing.T) {
	conn := testsupport.NewDB(t)

	rows, err := conn.Query(`SELECT name, sql FROM sqlite_master WHERE sql IS NOT NULL`)
	if err != nil {
		t.Fatalf("read sqlite_master: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if strings.Contains(strings.ToUpper(ddl), "CASCADE") {
			t.Errorf("%s uses CASCADE; every foreign key must be RESTRICT", name)
		}
	}
}
