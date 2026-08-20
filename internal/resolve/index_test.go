package resolve_test

import (
	"context"
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/origin"
	"home-management-system/internal/query"
	"home-management-system/internal/resolve"
	"home-management-system/internal/testsupport"
)

// TestBuildLabelsEveryKind is the one place the index meets the database. The
// resolver's own behaviour is pure and tested against hand-built candidates;
// what needs a real database is whether Build reconstructs the same hierarchy
// the trees show, since a wrong path is a silently wrong match rather than an
// error.
func TestBuildLabelsEveryKind(t *testing.T) {
	ctx := context.Background()
	conn := testsupport.NewDB(t)
	led := ledger.New(conn)
	orig := origin.New(conn)

	kitchen, err := led.CreateLocation(ctx, "Kitchen", nil, "")
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	pantry, err := led.CreateLocation(ctx, "Left Pantry", &kitchen, "")
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	shelf, err := led.CreateLocation(ctx, "Shelf 1", &pantry, "")
	if err != nil {
		t.Fatalf("create location: %v", err)
	}

	food, err := orig.CreateCategory(ctx, origin.CreateCategoryInput{Name: "Food"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	spices, err := orig.CreateCategory(ctx, origin.CreateCategoryInput{Name: "Spices", Parent: &food})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	size := domain.FromMilli(2_000_000)
	turmeric, err := orig.CreateBulkItem(ctx, origin.CreateBulkItemInput{
		Name: "Turmeric", Category: spices, ContentUnit: "g", PackageSize: &size,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	if _, err := led.CreateBulkHolding(ctx, ledger.CreateBulkHoldingInput{
		Item: turmeric, Location: shelf, UnitBasis: domain.BasisContent,
	}); err != nil {
		t.Fatalf("create holding: %v", err)
	}

	ix, err := resolve.Build(ctx, query.New(conn))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	paths := map[resolve.Kind][]string{}
	for _, c := range ix.All() {
		paths[c.Kind] = append(paths[c.Kind], c.Path)
	}
	for _, want := range []struct {
		kind resolve.Kind
		path string
	}{
		{resolve.KindLocation, "Kitchen"},
		{resolve.KindLocation, "Kitchen > Left Pantry"},
		{resolve.KindLocation, "Kitchen > Left Pantry > Shelf 1"},
		{resolve.KindCategory, "Food"},
		{resolve.KindCategory, "Food > Spices"},
		{resolve.KindItem, "Food > Spices > Turmeric"},
		// A Holding's label carries what tells it from its sibling in the same
		// place, which is H8's key: the basis, and the expiry when set.
		{resolve.KindHolding, "Turmeric > Shelf 1 (loose)"},
	} {
		if !contains(paths[want.kind], want.path) {
			t.Errorf("no %s labelled %q; got %v", want.kind, want.path, paths[want.kind])
		}
	}

	// And the labels are usable, which is the only reason they are built.
	wantExact(t, ix.Resolve("Kitchen > Left Pantry > Shelf 1"), "Kitchen > Left Pantry > Shelf 1")
	wantSuggested(t, ix.Resolve("kitshelf1", resolve.KindLocation), "Kitchen > Left Pantry > Shelf 1")
}

// A sibling under a deeper branch must not inherit the previous branch's path.
// The forest arrives pre-order with a depth, and the stack that rebuilds paths
// from it is exactly the kind of code that works until the tree gets wide.
func TestBuildUnwindsTheTree(t *testing.T) {
	ctx := context.Background()
	conn := testsupport.NewDB(t)
	led := ledger.New(conn)

	kitchen, err := led.CreateLocation(ctx, "Kitchen", nil, "")
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	pantry, err := led.CreateLocation(ctx, "Left Pantry", &kitchen, "")
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	if _, err := led.CreateLocation(ctx, "Shelf 1", &pantry, ""); err != nil {
		t.Fatalf("create location: %v", err)
	}
	// A sibling of Left Pantry, created after the deeper node, so a stack that
	// never unwinds would label it "Kitchen > Left Pantry > Shelf 1 > Fridge".
	if _, err := led.CreateLocation(ctx, "Fridge", &kitchen, ""); err != nil {
		t.Fatalf("create location: %v", err)
	}
	// And a second root, which a stack that only ever grows would nest.
	if _, err := led.CreateLocation(ctx, "Garage", nil, ""); err != nil {
		t.Fatalf("create location: %v", err)
	}

	ix, err := resolve.Build(ctx, query.New(conn))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	var got []string
	for _, c := range ix.All() {
		got = append(got, c.Path)
	}
	for _, want := range []string{"Kitchen > Fridge", "Garage"} {
		if !contains(got, want) {
			t.Errorf("no candidate labelled %q; got %v", want, got)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestTwoHoldingsInOnePlaceAreDistinguishable is the state a normal pantry is
// in: a sealed bag and a loose one, of the same thing, on the same shelf.
//
// Both are legal, both are active, and H8 says they are different Holdings. If
// the index labels them identically then nothing downstream can target one --
// which is exactly what happened, and made `discard` tell a person to say which
// while giving them no way to say it.
func TestTwoHoldingsInOnePlaceAreDistinguishable(t *testing.T) {
	ctx := context.Background()
	conn := testsupport.NewDB(t)
	led, orig := ledger.New(conn), origin.New(conn)

	shelf, err := led.CreateLocation(ctx, "Shelf 1", nil, "")
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	cat, err := orig.CreateCategory(ctx, origin.CreateCategoryInput{Name: "Grains"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	size := domain.FromMilli(2_000_000)
	rice, err := orig.CreateBulkItem(ctx, origin.CreateBulkItemInput{
		Name: "Basmati Rice", Category: cat, ContentUnit: "g", PackageSize: &size,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	for _, basis := range []domain.UnitBasis{domain.BasisPackage, domain.BasisContent} {
		if _, err := led.CreateBulkHolding(ctx, ledger.CreateBulkHoldingInput{
			Item: rice, Location: shelf, UnitBasis: basis,
		}); err != nil {
			t.Fatalf("create holding: %v", err)
		}
	}

	ix, err := resolve.Build(ctx, query.New(conn))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	labels := map[string]bool{}
	for _, c := range ix.All() {
		if c.Kind != resolve.KindHolding {
			continue
		}
		if labels[c.Path] {
			t.Fatalf("two holdings share the label %q; neither can be targeted", c.Path)
		}
		labels[c.Path] = true
	}
	for _, want := range []string{
		"Basmati Rice > Shelf 1 (sealed)",
		"Basmati Rice > Shelf 1 (loose)",
	} {
		if !labels[want] {
			t.Errorf("no holding labelled %q; got %v", want, labels)
		}
	}
	// And each one actually resolves, which is the point of distinguishing them.
	wantExact(t, ix.Resolve("Basmati Rice > Shelf 1 (sealed)"), "Basmati Rice > Shelf 1 (sealed)")
}
