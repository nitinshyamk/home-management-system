package annotate_test

// Stage 4: the non-ledger write paths and the read path, exercised together —
// a Category operation is not meaningful without a way to observe its result.

import (
	"context"
	"errors"
	"testing"
	"time"

	"home-management-system/internal/annotate"
	"home-management-system/internal/domain"
	"home-management-system/internal/origin"
	"home-management-system/internal/query"
	"home-management-system/internal/testsupport"
)

type harness struct {
	ctx context.Context
	o   *origin.Originator
	a   *annotate.Annotator
	r   *query.Reader
}

var clock = time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

func newHarness(t *testing.T) *harness {
	t.Helper()
	conn := testsupport.NewDB(t)
	return &harness{
		ctx: context.Background(),
		o:   origin.New(conn),
		a:   annotate.New(conn).WithClock(func() time.Time { return clock }),
		r:   query.New(conn),
	}
}

func (h *harness) category(t *testing.T, name string, parent *domain.CategoryID) domain.CategoryID {
	t.Helper()
	id, err := h.o.CreateCategory(h.ctx, origin.CreateCategoryInput{Name: name, Parent: parent})
	if err != nil {
		t.Fatalf("create category %q: %v", name, err)
	}
	return id
}

// tree builds Spices -> Dried Peppers, the running example from the domain model.
func (h *harness) tree(t *testing.T) (spices, peppers domain.CategoryID) {
	t.Helper()
	spices = h.category(t, "Spices", nil)
	peppers = h.category(t, "Dried Peppers", &spices)
	return spices, peppers
}

func names(nodes []query.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Category.Name)
	}
	return out
}

// ---------------------------------------------------------------------------
// Origination
// ---------------------------------------------------------------------------

func TestCreateCategoryTree(t *testing.T) {
	h := newHarness(t)
	spices, peppers := h.tree(t)

	path, err := h.r.CategoryPath(h.ctx, peppers)
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if len(path) != 2 || path[0].ID != spices || path[1].ID != peppers {
		t.Fatalf("path = %v, want [Spices, Dried Peppers]", path)
	}
	// D13 is root-first: the breadcrumb reads the way it is displayed.
	if path[0].Name != "Spices" || path[1].Name != "Dried Peppers" {
		t.Errorf("path names = %s/%s, want Spices/Dried Peppers", path[0].Name, path[1].Name)
	}
}

// TestSiblingNamesMayCollide: name uniqueness was never an integrity rule, and
// real homes have two drawers called "junk drawer".
func TestSiblingNamesMayCollide(t *testing.T) {
	h := newHarness(t)
	parent := h.category(t, "Garage", nil)

	first := h.category(t, "junk drawer", &parent)
	second := h.category(t, "junk drawer", &parent)
	if first == second {
		t.Fatal("expected two distinct categories")
	}
	kids, err := h.r.ChildCategories(h.ctx, parent)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if len(kids) != 2 {
		t.Errorf("got %d children, want 2", len(kids))
	}
}

func TestCreateItemsOfBothKinds(t *testing.T) {
	h := newHarness(t)
	spices, _ := h.tree(t)

	size := domain.FromMilli(2_000_000) // a 2 kg bag
	riceID, err := h.o.CreateBulkItem(h.ctx, origin.CreateBulkItemInput{
		Name: "Basmati Rice", Category: spices, ContentUnit: "g", PackageSize: &size,
	})
	if err != nil {
		t.Fatalf("create bulk item: %v", err)
	}
	cableID, err := h.o.CreateUniqueItem(h.ctx, origin.CreateUniqueItemInput{
		Name: "USB-C Cable", Category: spices,
	})
	if err != nil {
		t.Fatalf("create unique item: %v", err)
	}

	rice, err := h.r.Item(h.ctx, riceID)
	if err != nil {
		t.Fatalf("read bulk item: %v", err)
	}
	bulk, ok := rice.(domain.BulkItem)
	if !ok {
		t.Fatalf("rice hydrated as %T, want BulkItem", rice)
	}
	if bulk.Kind() != domain.KindBulk {
		t.Errorf("kind = %s, want Bulk", bulk.Kind())
	}
	if bulk.ContentUnit != "g" {
		t.Errorf("content unit = %s, want g", bulk.ContentUnit)
	}
	if !bulk.HasPackage() || bulk.PackageSize.Cmp(size) != 0 {
		t.Errorf("package size = %v, want %s", bulk.PackageSize, size)
	}

	cable, err := h.r.Item(h.ctx, cableID)
	if err != nil {
		t.Fatalf("read unique item: %v", err)
	}
	if _, ok := cable.(domain.UniqueItem); !ok {
		t.Fatalf("cable hydrated as %T, want UniqueItem", cable)
	}
}

func TestCreateBulkItemValidatesInput(t *testing.T) {
	h := newHarness(t)
	spices, _ := h.tree(t)

	zero := domain.Zero
	negative := domain.FromMilli(-1)
	for _, tc := range []struct {
		name string
		in   origin.CreateBulkItemInput
	}{
		{"no name", origin.CreateBulkItemInput{Category: spices, ContentUnit: "g"}},
		{"no unit", origin.CreateBulkItemInput{Name: "x", Category: spices}},
		{"zero package", origin.CreateBulkItemInput{Name: "x", Category: spices, ContentUnit: "g", PackageSize: &zero}},
		{"negative package", origin.CreateBulkItemInput{Name: "x", Category: spices, ContentUnit: "g", PackageSize: &negative}},
	} {
		if _, err := h.o.CreateBulkItem(h.ctx, tc.in); !errors.Is(err, origin.ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", tc.name, err)
		}
	}
}

// TestItemCreationIsAtomic is O4: a base row without its variant row violates I3
// and would surface much later as a hydration failure.
func TestItemCreationIsAtomic(t *testing.T) {
	h := newHarness(t)
	spices, _ := h.tree(t)

	// A nonexistent unit fails the variant insert, after the base insert.
	_, err := h.o.CreateBulkItem(h.ctx, origin.CreateBulkItemInput{
		Name: "Mystery", Category: spices, ContentUnit: "furlongs",
	})
	if err == nil {
		t.Fatal("expected an error for an unknown unit")
	}

	items, err := h.r.Items(h.ctx)
	if err != nil {
		t.Fatalf("list items: %v", err)
	}
	for _, item := range items {
		if item.Base().Name == "Mystery" {
			t.Fatal("the base row survived a failed variant insert — the transaction did not roll back")
		}
	}
}

// ---------------------------------------------------------------------------
// Annotation
// ---------------------------------------------------------------------------

func TestRenameAndDescribeCategory(t *testing.T) {
	h := newHarness(t)
	spices, _ := h.tree(t)

	if err := h.a.RenameCategory(h.ctx, spices, "Seasonings"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := h.a.SetCategoryDescription(h.ctx, spices, "jars and tins"); err != nil {
		t.Fatalf("describe: %v", err)
	}
	got, err := h.r.Category(h.ctx, spices)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Name != "Seasonings" || got.Description != "jars and tins" {
		t.Errorf("got %q/%q, want Seasonings/jars and tins", got.Name, got.Description)
	}
}

// TestReparentRejectsCycles is C1, and it is what lets every read-side recursive
// query assume the tree is acyclic.
func TestReparentRejectsCycles(t *testing.T) {
	h := newHarness(t)
	spices, peppers := h.tree(t)
	ancho := h.category(t, "Ancho", &peppers)

	// Direct: a node beneath its own child.
	if err := h.a.ReparentCategory(h.ctx, spices, &peppers); !errors.Is(err, annotate.ErrCycle) {
		t.Errorf("direct cycle: err = %v, want ErrCycle", err)
	}
	// Transitive: a node beneath its own grandchild.
	if err := h.a.ReparentCategory(h.ctx, spices, &ancho); !errors.Is(err, annotate.ErrCycle) {
		t.Errorf("transitive cycle: err = %v, want ErrCycle", err)
	}
	// Self.
	if err := h.a.ReparentCategory(h.ctx, spices, &spices); !errors.Is(err, annotate.ErrCycle) {
		t.Errorf("self parent: err = %v, want ErrCycle", err)
	}
}

func TestReparentMovesSubtree(t *testing.T) {
	h := newHarness(t)
	spices, peppers := h.tree(t)
	pantry := h.category(t, "Pantry", nil)

	if err := h.a.ReparentCategory(h.ctx, spices, &pantry); err != nil {
		t.Fatalf("reparent: %v", err)
	}
	path, err := h.r.CategoryPath(h.ctx, peppers)
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if got := len(path); got != 3 {
		t.Fatalf("path length = %d, want 3", got)
	}
	if path[0].ID != pantry || path[1].ID != spices || path[2].ID != peppers {
		t.Errorf("path = %v, want Pantry/Spices/Dried Peppers", names(toNodes(path)))
	}
}

func TestReparentToRoot(t *testing.T) {
	h := newHarness(t)
	_, peppers := h.tree(t)

	if err := h.a.ReparentCategory(h.ctx, peppers, nil); err != nil {
		t.Fatalf("reparent to root: %v", err)
	}
	got, err := h.r.Category(h.ctx, peppers)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !got.IsRoot() {
		t.Error("category is not a root after re-parenting to nil")
	}
}

// ---------------------------------------------------------------------------
// Archive with resolution
// ---------------------------------------------------------------------------

func TestArchiveWithLiftReassignsChildrenAndItems(t *testing.T) {
	h := newHarness(t)
	pantry := h.category(t, "Pantry", nil)
	spices := h.category(t, "Spices", &pantry)
	peppers := h.category(t, "Dried Peppers", &spices)

	itemID, err := h.o.CreateUniqueItem(h.ctx, origin.CreateUniqueItemInput{
		Name: "Cumin Tin", Category: spices,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	// Archiving Spices lifts Dried Peppers and Cumin Tin to Pantry.
	if err := h.a.ArchiveCategory(h.ctx, spices, domain.ResolutionLift, nil); err != nil {
		t.Fatalf("archive with lift: %v", err)
	}

	child, err := h.r.Category(h.ctx, peppers)
	if err != nil {
		t.Fatalf("read child: %v", err)
	}
	if child.Parent == nil || *child.Parent != pantry {
		t.Errorf("child parent = %v, want Pantry(%d)", child.Parent, pantry)
	}

	item, err := h.r.Item(h.ctx, itemID)
	if err != nil {
		t.Fatalf("read item: %v", err)
	}
	if item.Base().Category != pantry {
		t.Errorf("item category = %d, want Pantry(%d)", item.Base().Category, pantry)
	}

	archived, err := h.r.Category(h.ctx, spices)
	if err != nil {
		t.Fatalf("read archived: %v", err)
	}
	if !archived.IsArchived() {
		t.Error("category is not archived")
	}
	// Archived nodes leave the live views but remain resolvable.
	roots, err := h.r.RootCategories(h.ctx)
	if err != nil {
		t.Fatalf("roots: %v", err)
	}
	for _, r := range roots {
		if r.ID == spices {
			t.Error("archived category still appears among live roots")
		}
	}
}

// TestLiftFromRootWithItemsIsRefused: category_id is NOT NULL, so lifting a
// root's items has nowhere to put them. Refusing beats inventing a destination.
func TestLiftFromRootWithItemsIsRefused(t *testing.T) {
	h := newHarness(t)
	root := h.category(t, "Loose Ends", nil)
	if _, err := h.o.CreateUniqueItem(h.ctx, origin.CreateUniqueItemInput{
		Name: "Orphan", Category: root,
	}); err != nil {
		t.Fatalf("create item: %v", err)
	}

	err := h.a.ArchiveCategory(h.ctx, root, domain.ResolutionLift, nil)
	if !errors.Is(err, annotate.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
	// And the refusal must be total: nothing archived, nothing moved.
	got, err := h.r.Category(h.ctx, root)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.IsArchived() {
		t.Error("category was archived despite the refusal")
	}
}

func TestArchiveWithMoveSendsContentsToChosenNode(t *testing.T) {
	h := newHarness(t)
	spices := h.category(t, "Spices", nil)
	baking := h.category(t, "Baking", nil)
	itemID, err := h.o.CreateUniqueItem(h.ctx, origin.CreateUniqueItemInput{
		Name: "Cinnamon", Category: spices,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}

	if err := h.a.ArchiveCategory(h.ctx, spices, domain.ResolutionMove, &baking); err != nil {
		t.Fatalf("archive with move: %v", err)
	}
	item, err := h.r.Item(h.ctx, itemID)
	if err != nil {
		t.Fatalf("read item: %v", err)
	}
	if item.Base().Category != baking {
		t.Errorf("item category = %d, want Baking(%d)", item.Base().Category, baking)
	}
}

func TestArchiveWithMoveRejectsDescendantDestination(t *testing.T) {
	h := newHarness(t)
	spices, peppers := h.tree(t)

	// Moving contents into a descendant would strand them under an archived
	// ancestor.
	if err := h.a.ArchiveCategory(h.ctx, spices, domain.ResolutionMove, &peppers); !errors.Is(err, annotate.ErrCycle) {
		t.Errorf("err = %v, want ErrCycle", err)
	}
}

func TestArchiveWithBlockRefusesNonEmpty(t *testing.T) {
	h := newHarness(t)
	spices, _ := h.tree(t)

	if err := h.a.ArchiveCategory(h.ctx, spices, domain.ResolutionBlock, nil); !errors.Is(err, annotate.ErrNotEmpty) {
		t.Errorf("non-empty: err = %v, want ErrNotEmpty", err)
	}
	// An empty node archives cleanly under Block.
	empty := h.category(t, "Empty", nil)
	if err := h.a.ArchiveCategory(h.ctx, empty, domain.ResolutionBlock, nil); err != nil {
		t.Errorf("empty node under Block: %v", err)
	}
}

func TestArchiveRejectsUnknownResolution(t *testing.T) {
	h := newHarness(t)
	spices, _ := h.tree(t)
	if err := h.a.ArchiveCategory(h.ctx, spices, domain.Resolution("Vaporize"), nil); !errors.Is(err, annotate.ErrInvalidInput) {
		t.Errorf("err = %v, want ErrInvalidInput", err)
	}
}

func TestRestoreCategory(t *testing.T) {
	h := newHarness(t)
	empty := h.category(t, "Empty", nil)

	if err := h.a.ArchiveCategory(h.ctx, empty, domain.ResolutionBlock, nil); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := h.a.RestoreCategory(h.ctx, empty); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, err := h.r.Category(h.ctx, empty)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.IsArchived() {
		t.Error("category is still archived after restore")
	}
}

func TestOperationsOnMissingCategoriesAreRejected(t *testing.T) {
	h := newHarness(t)
	const missing = domain.CategoryID(999999)

	if err := h.a.RenameCategory(h.ctx, missing, "x"); !errors.Is(err, annotate.ErrNotFound) {
		t.Errorf("rename: err = %v, want ErrNotFound", err)
	}
	if err := h.a.ReparentCategory(h.ctx, missing, nil); !errors.Is(err, annotate.ErrNotFound) {
		t.Errorf("reparent: err = %v, want ErrNotFound", err)
	}
	if err := h.a.ArchiveCategory(h.ctx, missing, domain.ResolutionLift, nil); !errors.Is(err, annotate.ErrNotFound) {
		t.Errorf("archive: err = %v, want ErrNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// Item annotation
// ---------------------------------------------------------------------------

func TestItemAnnotations(t *testing.T) {
	h := newHarness(t)
	spices, peppers := h.tree(t)
	id, err := h.o.CreateUniqueItem(h.ctx, origin.CreateUniqueItemInput{Name: "Ancho", Category: spices})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := h.a.RenameItem(h.ctx, id, "Ancho Chile"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := h.a.ReclassifyItem(h.ctx, id, peppers); err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if err := h.a.SetItemNotes(h.ctx, id, "mild, smoky"); err != nil {
		t.Fatalf("notes: %v", err)
	}
	if err := h.a.ConfirmPlacement(h.ctx, id); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	item, err := h.r.Item(h.ctx, id)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	b := item.Base()
	if b.Name != "Ancho Chile" || b.Category != peppers || b.Notes != "mild, smoky" {
		t.Errorf("got %+v, want renamed/reclassified/annotated", b)
	}
	if b.PlacementConfirmedAt == nil || !b.PlacementConfirmedAt.Equal(clock) {
		t.Errorf("placement confirmed at %v, want %v", b.PlacementConfirmedAt, clock)
	}
}

// TestClassificationNudgeFiresOnlyWithASibling: the nudge exists only where a
// plausible alternative exists. A generic "filed too shallow" nag is the version
// that becomes wallpaper.
func TestClassificationNudgeFiresOnlyWithASibling(t *testing.T) {
	h := newHarness(t)

	// A leaf category with an item: no children, so no nudge.
	stamps := h.category(t, "Stamps", nil)
	if _, err := h.o.CreateUniqueItem(h.ctx, origin.CreateUniqueItemInput{
		Name: "Forever Stamp", Category: stamps,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	nudges, err := h.r.ClassificationNudges(h.ctx)
	if err != nil {
		t.Fatalf("nudges: %v", err)
	}
	if len(nudges) != 0 {
		t.Errorf("got %d nudges for a childless category, want 0", len(nudges))
	}

	// Now a branch category with an item filed directly at it.
	spices, _ := h.tree(t)
	cuminID, err := h.o.CreateUniqueItem(h.ctx, origin.CreateUniqueItemInput{
		Name: "Cumin", Category: spices,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	nudges, err = h.r.ClassificationNudges(h.ctx)
	if err != nil {
		t.Fatalf("nudges: %v", err)
	}
	if len(nudges) != 1 || nudges[0].Item != cuminID {
		t.Fatalf("got %+v, want one nudge for Cumin", nudges)
	}
	if nudges[0].ChildCount != 1 {
		t.Errorf("child count = %d, want 1", nudges[0].ChildCount)
	}

	// Confirming placement silences it — which is what makes it dismissible.
	if err := h.a.ConfirmPlacement(h.ctx, cuminID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	nudges, err = h.r.ClassificationNudges(h.ctx)
	if err != nil {
		t.Fatalf("nudges: %v", err)
	}
	if len(nudges) != 0 {
		t.Errorf("got %d nudges after confirmation, want 0", len(nudges))
	}
}

// ---------------------------------------------------------------------------
// Rollups — the derivation the non-leaf decision rests on
// ---------------------------------------------------------------------------

func TestSubtreeAndRollupSpanDescendants(t *testing.T) {
	h := newHarness(t)
	spices, peppers := h.tree(t)
	ancho := h.category(t, "Ancho", &peppers)

	for _, c := range []domain.CategoryID{spices, peppers, ancho} {
		if _, err := h.o.CreateUniqueItem(h.ctx, origin.CreateUniqueItemInput{
			Name: "item", Category: c,
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	nodes, err := h.r.CategorySubtree(h.ctx, spices)
	if err != nil {
		t.Fatalf("subtree: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("subtree = %v, want 3 nodes", names(nodes))
	}
	for i, wantDepth := range []int{0, 1, 2} {
		if nodes[i].Depth != wantDepth {
			t.Errorf("node %d depth = %d, want %d", i, nodes[i].Depth, wantDepth)
		}
	}

	// The rollup counts the node and everything beneath it — which is what makes
	// filing at a non-leaf harmless.
	total, err := h.r.CountItemsInCategoryTree(h.ctx, spices)
	if err != nil {
		t.Fatalf("rollup: %v", err)
	}
	if total != 3 {
		t.Errorf("rollup = %d, want 3", total)
	}
	leaf, err := h.r.CountItemsInCategoryTree(h.ctx, ancho)
	if err != nil {
		t.Fatalf("leaf rollup: %v", err)
	}
	if leaf != 1 {
		t.Errorf("leaf rollup = %d, want 1", leaf)
	}
}

func toNodes(cs []domain.Category) []query.Node {
	out := make([]query.Node, 0, len(cs))
	for i, c := range cs {
		out = append(out, query.Node{Category: c, Depth: i})
	}
	return out
}
