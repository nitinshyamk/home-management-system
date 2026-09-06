package ops_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"home-management-system/internal/annotate"
	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/origin"
	"home-management-system/internal/testsupport"
)

// Archiving a node with a Resolution is one policy implemented twice, in two
// write paths that cannot share code.
//
// A Location records events and a Category does plain UPDATEs -- different
// mechanisms, for the good reason that a Location has a ledger and a Category
// does not. What they must agree on is the DECISION: which resolutions are
// legal, where contents go, and what is refused. Sharing that through one
// function would mean a callback per difference and a signature nobody could
// read, so it stays written twice.
//
// Written twice and checked once. These cases run against both paths, and they
// exist because the policy had already drifted: archiving an already-archived
// node was refused for Locations and silently repeated for Categories, which
// overwrote the timestamp saying when it actually happened. Nothing failed,
// because the Location side had twelve archive tests and the Category side had
// five, and no test asked them the same question.
//
// A rule added to one path and not the other fails here.

// archiver is one write path's archive, reduced to what the policy cares about.
type archiver struct {
	kind string
	// make builds a node under an optional parent and returns its identifier.
	make func(t *testing.T, name string, parent int64) int64
	// fill puts something INSIDE the node, so emptiness rules can be tested.
	fill func(t *testing.T, node int64)
	// archive runs the operation. moveTo is 0 for "not given".
	archive func(node int64, r domain.Resolution, moveTo int64) error
}

const noParent = 0

func archivers(t *testing.T) []archiver {
	t.Helper()
	ctx := context.Background()

	catConn := testsupport.NewDB(t)
	catOrigin, catAnnotate := origin.New(catConn), annotate.New(catConn)

	locConn := testsupport.NewDB(t)
	locLedger := ledger.New(locConn)

	return []archiver{
		{
			kind: "category",
			make: func(t *testing.T, name string, parent int64) int64 {
				t.Helper()
				in := origin.CreateCategoryInput{Name: name}
				if parent != noParent {
					p := domain.CategoryID(parent)
					in.Parent = &p
				}
				id, err := catOrigin.CreateCategory(ctx, in)
				if err != nil {
					t.Fatalf("create category %q: %v", name, err)
				}
				return int64(id)
			},
			fill: func(t *testing.T, node int64) {
				t.Helper()
				p := domain.CategoryID(node)
				if _, err := catOrigin.CreateCategory(ctx,
					origin.CreateCategoryInput{Name: "child of " + strconv.FormatInt(node, 10), Parent: &p}); err != nil {
					t.Fatalf("fill category: %v", err)
				}
			},
			archive: func(node int64, r domain.Resolution, moveTo int64) error {
				var to *domain.CategoryID
				if moveTo != 0 {
					id := domain.CategoryID(moveTo)
					to = &id
				}
				return catAnnotate.ArchiveCategory(ctx, domain.CategoryID(node), r, to)
			},
		},
		{
			kind: "location",
			make: func(t *testing.T, name string, parent int64) int64 {
				t.Helper()
				var p *domain.LocationID
				if parent != noParent {
					id := domain.LocationID(parent)
					p = &id
				}
				id, err := locLedger.CreateLocation(ctx, name, p, "")
				if err != nil {
					t.Fatalf("create location %q: %v", name, err)
				}
				return int64(id)
			},
			fill: func(t *testing.T, node int64) {
				t.Helper()
				p := domain.LocationID(node)
				if _, err := locLedger.CreateLocation(ctx, "child of "+strconv.FormatInt(node, 10), &p, ""); err != nil {
					t.Fatalf("fill location: %v", err)
				}
			},
			archive: func(node int64, r domain.Resolution, moveTo int64) error {
				var to *domain.LocationID
				if moveTo != 0 {
					id := domain.LocationID(moveTo)
					to = &id
				}
				return locLedger.ArchiveLocation(ctx, domain.LocationID(node), r, to)
			},
		},
	}
}

// TestArchivingTwiceIsRefused: the second archive would overwrite the timestamp
// recording when it actually happened, and report success for doing nothing.
func TestArchivingTwiceIsRefused(t *testing.T) {
	for _, a := range archivers(t) {
		t.Run(a.kind, func(t *testing.T) {
			node := a.make(t, "Twice", noParent)
			if err := a.archive(node, domain.ResolutionLift, 0); err != nil {
				t.Fatalf("first archive: %v", err)
			}
			err := a.archive(node, domain.ResolutionLift, 0)
			if err == nil {
				t.Fatal("archiving an already-archived node was allowed")
			}
			if !strings.Contains(err.Error(), "already archived") {
				t.Errorf("error = %v, want it to say the node is already archived", err)
			}
		})
	}
}

// TestMoveWithoutADestinationIsRefused: Move means "put the contents there",
// and there is no there.
func TestMoveWithoutADestinationIsRefused(t *testing.T) {
	for _, a := range archivers(t) {
		t.Run(a.kind, func(t *testing.T) {
			node := a.make(t, "NoDestination", noParent)
			if err := a.archive(node, domain.ResolutionMove, 0); err == nil {
				t.Fatal("Move with no destination was allowed")
			}
		})
	}
}

// TestMovingContentsIntoTheNodeBeingArchivedIsRefused.
func TestMovingContentsIntoTheNodeBeingArchivedIsRefused(t *testing.T) {
	for _, a := range archivers(t) {
		t.Run(a.kind, func(t *testing.T) {
			node := a.make(t, "Itself", noParent)
			if err := a.archive(node, domain.ResolutionMove, node); err == nil {
				t.Fatal("moving contents into the node being archived was allowed")
			}
		})
	}
}

// TestMovingContentsBeneathTheNodeIsRefused: the contents would be stranded
// under an archived ancestor, which is the same objection as a cycle.
func TestMovingContentsBeneathTheNodeIsRefused(t *testing.T) {
	for _, a := range archivers(t) {
		t.Run(a.kind, func(t *testing.T) {
			node := a.make(t, "Parent", noParent)
			below := a.make(t, "Below", node)
			err := a.archive(node, domain.ResolutionMove, below)
			if err == nil {
				t.Fatal("moving contents into a descendant was allowed")
			}
			if !errors.Is(err, errCycleFor(a.kind)) && !strings.Contains(err.Error(), "beneath") {
				t.Errorf("error = %v, want it to say the destination is beneath the node", err)
			}
		})
	}
}

// TestBlockRefusesANodeThatHoldsSomething: Block means "only if empty".
func TestBlockRefusesANodeThatHoldsSomething(t *testing.T) {
	for _, a := range archivers(t) {
		t.Run(a.kind, func(t *testing.T) {
			node := a.make(t, "Occupied", noParent)
			a.fill(t, node)
			if err := a.archive(node, domain.ResolutionBlock, 0); err == nil {
				t.Fatal("Block archived a node that still held something")
			}
		})
	}
}

// TestBlockAllowsAnEmptyNode is the other half: the rule is about contents, not
// about Block being a refusal in itself.
func TestBlockAllowsAnEmptyNode(t *testing.T) {
	for _, a := range archivers(t) {
		t.Run(a.kind, func(t *testing.T) {
			node := a.make(t, "Empty", noParent)
			if err := a.archive(node, domain.ResolutionBlock, 0); err != nil {
				t.Errorf("Block refused an empty node: %v", err)
			}
		})
	}
}

// TestAnUnknownResolutionIsRefused.
func TestAnUnknownResolutionIsRefused(t *testing.T) {
	for _, a := range archivers(t) {
		t.Run(a.kind, func(t *testing.T) {
			node := a.make(t, "Bogus", noParent)
			if err := a.archive(node, domain.Resolution("Vaporise"), 0); err == nil {
				t.Fatal("an unknown resolution was accepted")
			}
		})
	}
}

func errCycleFor(kind string) error {
	if kind == "category" {
		return annotate.ErrCycle
	}
	return ledger.ErrCycle
}
