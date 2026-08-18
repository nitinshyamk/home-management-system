package ops_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"home-management-system/internal/annotate"
	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/ops"
	"home-management-system/internal/origin"
	"home-management-system/internal/query"
	"home-management-system/internal/testsupport"
)

// tree builds  Kitchen > Pantry > Shelf1, with Garage as a sibling root.
type tree struct {
	ctx     context.Context
	pl      *ops.Planner
	ex      *ops.Executor
	led     *ledger.Processor
	ann     *annotate.Annotator
	r       *query.Reader
	kitchen domain.LocationID
	pantry  domain.LocationID
	shelf1  domain.LocationID
	garage  domain.LocationID
	item    domain.ItemID
}

func newTree(t *testing.T) *tree {
	t.Helper()
	conn := testsupport.NewDB(t)
	ctx := context.Background()
	led := ledger.New(conn).WithClock(func() time.Time { return clock })

	tr := &tree{ctx: ctx, pl: ops.NewPlanner(conn), ex: ops.New(conn),
		led: led, ann: annotate.New(conn), r: query.New(conn)}

	mk := func(name string, parent *domain.LocationID) domain.LocationID {
		id, err := led.CreateLocation(ctx, name, parent, "")
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return id
	}
	tr.kitchen = mk("Kitchen", nil)
	tr.pantry = mk("Pantry", &tr.kitchen)
	tr.shelf1 = mk("Shelf 1", &tr.pantry)
	tr.garage = mk("Garage", nil)

	cat, err := origin.New(conn).CreateCategory(ctx, origin.CreateCategoryInput{Name: "Pantry Goods"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	size := domain.FromMilli(2_000_000)
	if tr.item, err = origin.New(conn).CreateBulkItem(ctx, origin.CreateBulkItemInput{
		Name: "Basmati Rice", Category: cat, ContentUnit: "g", PackageSize: &size,
	}); err != nil {
		t.Fatalf("create item: %v", err)
	}
	return tr
}

func (tr *tree) stow(t *testing.T, at domain.LocationID) domain.HoldingID {
	t.Helper()
	id, err := tr.led.CreateBulkHolding(tr.ctx, ledger.CreateBulkHoldingInput{
		Item: tr.item, Location: at, UnitBasis: domain.BasisContent,
	})
	if err != nil {
		t.Fatalf("create holding: %v", err)
	}
	return id
}

func (tr *tree) location(t *testing.T, id domain.LocationID) domain.Location {
	t.Helper()
	loc, err := tr.r.Location(tr.ctx, id)
	if err != nil {
		t.Fatalf("read location %d: %v", id, err)
	}
	return loc
}

// types renders the event types of a plan, which is what the interesting
// assertions are about: an archival is defined by what it records.
func types(events []domain.Event) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = string(e.Type())
	}
	return out
}

// ---------------------------------------------------------------------------
// Annotation: the gap that made a Location unrenameable
// ---------------------------------------------------------------------------

func TestRenameLocation(t *testing.T) {
	tr := newTree(t)

	if _, err := tr.ex.Execute(tr.ctx, tr.pl.RenameLocation(tr.shelf1, "Top Shelf")); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if got := tr.location(t, tr.shelf1).Name; got != "Top Shelf" {
		t.Errorf("name = %q, want %q", got, "Top Shelf")
	}
}

// TestRenameLocationRecordsNothing is the reason a rename is annotation. The
// ledger reconstructs containment; a rename that appeared there would make
// replay claim something moved.
func TestRenameLocationRecordsNothing(t *testing.T) {
	tr := newTree(t)

	before, err := tr.led.History(tr.ctx, domain.SubjectLocation, int64(tr.shelf1))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if _, err := tr.ex.Execute(tr.ctx, tr.pl.RenameLocation(tr.shelf1, "Top Shelf")); err != nil {
		t.Fatalf("rename: %v", err)
	}
	after, err := tr.led.History(tr.ctx, domain.SubjectLocation, int64(tr.shelf1))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("rename appended %d events; a rename must be invisible to replay",
			len(after)-len(before))
	}
}

func TestDescribeLocation(t *testing.T) {
	tr := newTree(t)

	if _, err := tr.ex.Execute(tr.ctx, tr.pl.DescribeLocation(tr.shelf1, "eye level, left")); err != nil {
		t.Fatalf("describe: %v", err)
	}
	if got := tr.location(t, tr.shelf1).Description; got != "eye level, left" {
		t.Errorf("description = %q", got)
	}
}

func TestRenameLocationRejectsUnknown(t *testing.T) {
	tr := newTree(t)
	_, err := tr.ex.Execute(tr.ctx, tr.pl.RenameLocation(domain.LocationID(9999), "Nowhere"))
	if !errors.Is(err, annotate.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// Archival
// ---------------------------------------------------------------------------

// TestArchiveWithLiftMovesContentsToTheParent is the composite the plan calls
// for: one intent, NodeArchived plus a Moved for every holding and a
// NodeReparented for every child.
func TestArchiveWithLiftMovesContentsToTheParent(t *testing.T) {
	tr := newTree(t)
	rice := tr.stow(t, tr.pantry)
	flour := tr.stow(t, tr.pantry)

	batch, err := tr.pl.ArchiveLocation(tr.ctx, ops.ArchiveLocationRequest{
		Location: tr.pantry, Resolution: domain.ResolutionLift,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// The plan is inspectable before anything happens, which is the whole
	// reason planning is separate from executing.
	events, err := batch.Steps[0].Records(ops.Created{})
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	want := []string{"NodeReparented", "Moved", "Moved", "NodeArchived"}
	if got := types(events); !equal(got, want) {
		t.Errorf("plan = %v, want %v", got, want)
	}
	// Contents leave before the node is archived, so no intermediate state has
	// a live Holding inside an archived Location.
	if types(events)[len(events)-1] != "NodeArchived" {
		t.Error("NodeArchived is not last; contents would be archived in place")
	}

	if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
		t.Fatalf("execute: %v", err)
	}

	for _, h := range []domain.HoldingID{rice, flour} {
		d, err := tr.r.Holding(tr.ctx, h)
		if err != nil {
			t.Fatalf("read holding: %v", err)
		}
		if got := d.Holding.Base().StowedLocation; got != tr.kitchen {
			t.Errorf("holding %d is at %d, want Kitchen (%d)", h, got, tr.kitchen)
		}
	}
	if got := tr.location(t, tr.shelf1).Parent; got == nil || *got != tr.kitchen {
		t.Errorf("Shelf 1 parent = %v, want Kitchen (%d)", got, tr.kitchen)
	}
	if tr.location(t, tr.pantry).ArchivedAt == nil {
		t.Error("Pantry was not archived")
	}

	// Four events against three subjects in one transaction. If any write
	// skipped its event, replay and the projection now disagree.
	report, err := tr.led.VerifyAll(tr.ctx)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !report.Clean() {
		t.Errorf("ledger disagrees with the projection after an archival: %+v", report)
	}
}

// TestArchiveWithMoveSendsContentsToTheNamedNode
func TestArchiveWithMoveSendsContentsToTheNamedNode(t *testing.T) {
	tr := newTree(t)
	rice := tr.stow(t, tr.pantry)

	batch, err := tr.pl.ArchiveLocation(tr.ctx, ops.ArchiveLocationRequest{
		Location: tr.pantry, Resolution: domain.ResolutionMove, MoveTo: &tr.garage,
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
	if got := d.Holding.Base().StowedLocation; got != tr.garage {
		t.Errorf("holding is at %d, want Garage (%d)", got, tr.garage)
	}
}

func TestArchiveWithBlockRefusesANonEmptyNode(t *testing.T) {
	tr := newTree(t)
	tr.stow(t, tr.pantry)

	_, err := tr.pl.ArchiveLocation(tr.ctx, ops.ArchiveLocationRequest{
		Location: tr.pantry, Resolution: domain.ResolutionBlock,
	})
	if !errors.Is(err, ledger.ErrNotEmpty) {
		t.Errorf("error = %v, want ErrNotEmpty", err)
	}
}

func TestArchiveWithBlockAllowsAnEmptyNode(t *testing.T) {
	tr := newTree(t)

	batch, err := tr.pl.ArchiveLocation(tr.ctx, ops.ArchiveLocationRequest{
		Location: tr.shelf1, Resolution: domain.ResolutionBlock,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if tr.location(t, tr.shelf1).ArchivedAt == nil {
		t.Error("Shelf 1 was not archived")
	}
}

// TestLiftingARootThatHoldsThingsIsRefused: stowed_location_id is NOT NULL, so
// there is nowhere for the contents to go. A Holding with no location is a
// thing whose whereabouts the system claims not to know.
func TestLiftingARootThatHoldsThingsIsRefused(t *testing.T) {
	tr := newTree(t)
	tr.stow(t, tr.kitchen)

	_, err := tr.pl.ArchiveLocation(tr.ctx, ops.ArchiveLocationRequest{
		Location: tr.kitchen, Resolution: domain.ResolutionLift,
	})
	if !errors.Is(err, ledger.ErrRootNotEmpty) {
		t.Errorf("error = %v, want ErrRootNotEmpty", err)
	}
}

// TestLiftingAnEmptyRootMakesItsChildrenRoots: the same operation is fine when
// there is nothing that needs a location.
func TestLiftingAnEmptyRootMakesItsChildrenRoots(t *testing.T) {
	tr := newTree(t)

	batch, err := tr.pl.ArchiveLocation(tr.ctx, ops.ArchiveLocationRequest{
		Location: tr.kitchen, Resolution: domain.ResolutionLift,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := tr.location(t, tr.pantry).Parent; got != nil {
		t.Errorf("Pantry parent = %v, want nil (it should have become a root)", *got)
	}
}

func TestArchiveRefusesADestinationBeneathTheNode(t *testing.T) {
	tr := newTree(t)

	_, err := tr.pl.ArchiveLocation(tr.ctx, ops.ArchiveLocationRequest{
		Location: tr.pantry, Resolution: domain.ResolutionMove, MoveTo: &tr.shelf1,
	})
	if !errors.Is(err, ledger.ErrCycle) {
		t.Errorf("error = %v, want ErrCycle -- contents would be stranded under an archived ancestor", err)
	}
}

func TestArchiveRefusesToArchiveTwice(t *testing.T) {
	tr := newTree(t)

	batch, _ := tr.pl.ArchiveLocation(tr.ctx, ops.ArchiveLocationRequest{
		Location: tr.shelf1, Resolution: domain.ResolutionLift,
	})
	if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, err := tr.pl.ArchiveLocation(tr.ctx, ops.ArchiveLocationRequest{
		Location: tr.shelf1, Resolution: domain.ResolutionLift,
	}); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput", err)
	}
}

// TestRestoreDoesNotBringContentsBack: they were moved, the move was recorded,
// and where they went is now where they are.
func TestRestoreLocationLeavesMovedContentsWhereTheyWent(t *testing.T) {
	tr := newTree(t)
	rice := tr.stow(t, tr.pantry)

	batch, _ := tr.pl.ArchiveLocation(tr.ctx, ops.ArchiveLocationRequest{
		Location: tr.pantry, Resolution: domain.ResolutionLift,
	})
	if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
		t.Fatalf("archive: %v", err)
	}

	restore, err := tr.pl.RestoreLocation(tr.ctx, tr.pantry)
	if err != nil {
		t.Fatalf("plan restore: %v", err)
	}
	if _, err := tr.ex.Execute(tr.ctx, restore); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if tr.location(t, tr.pantry).ArchivedAt != nil {
		t.Error("Pantry is still archived")
	}
	d, err := tr.r.Holding(tr.ctx, rice)
	if err != nil {
		t.Fatalf("read holding: %v", err)
	}
	if got := d.Holding.Base().StowedLocation; got != tr.kitchen {
		t.Errorf("holding returned to %d on restore; it should have stayed at Kitchen (%d)", got, tr.kitchen)
	}
}

func TestRestoreRefusesALiveLocation(t *testing.T) {
	tr := newTree(t)
	if _, err := tr.pl.RestoreLocation(tr.ctx, tr.pantry); !errors.Is(err, ledger.ErrInvalidInput) {
		t.Errorf("error = %v, want ErrInvalidInput", err)
	}
}

// TestArchiveIsAtomic: an archival that fails partway leaves the tree exactly
// as it was, contents included.
func TestArchiveIsAtomic(t *testing.T) {
	tr := newTree(t)
	rice := tr.stow(t, tr.pantry)

	batch, err := tr.pl.ArchiveLocation(tr.ctx, ops.ArchiveLocationRequest{
		Location: tr.pantry, Resolution: domain.ResolutionLift,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	inner := batch.Steps[0].Records
	batch.Steps[0].Records = func(c ops.Created) ([]domain.Event, error) {
		events, _ := inner(c)
		// A Moved for a Holding that does not exist, after the real ones.
		return append(events[:len(events)-1], domain.Moved{
			EventBase: domain.EventBase{OccurredAt: clock},
			Holding:   domain.HoldingID(999_999),
			From:      tr.pantry, To: tr.kitchen,
		}), nil
	}

	if _, err := tr.ex.Execute(tr.ctx, batch); err == nil {
		t.Fatal("execute succeeded against a nonexistent holding")
	}
	d, err := tr.r.Holding(tr.ctx, rice)
	if err != nil {
		t.Fatalf("read holding: %v", err)
	}
	if got := d.Holding.Base().StowedLocation; got != tr.pantry {
		t.Errorf("holding moved to %d despite the failure, want Pantry (%d)", got, tr.pantry)
	}
	if tr.location(t, tr.pantry).ArchivedAt != nil {
		t.Error("Pantry was archived despite the failure")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
