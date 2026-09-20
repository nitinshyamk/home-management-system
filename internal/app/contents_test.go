package app_test

import (
	"context"
	"testing"

	"home-management-system/internal/app"
	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
	"home-management-system/internal/testsupport"
)

// A house with depth, so "under" means something: the tray is four levels
// below the garage, and a rollup that stopped one level short would still look
// right on a flatter one.
//
//	Garage > Metal Shelving Unit > Bay 3 > Blue Crate > Small Parts Tray
//	Kitchen > Left Pantry
//
//	Spices > Dried Peppers
type house struct {
	ctrl                      app.Controller
	garage, bay, tray, pantry domain.LocationID
	spices, peppers           domain.CategoryID
	chile, rice               domain.ItemID
}

func build(t *testing.T) house {
	t.Helper()
	conn := testsupport.NewDB(t)
	ctx := context.Background()
	pl, ex := ops.NewPlanner(conn), ops.New(conn)

	apply := func(b ops.Batch, err error) ops.Result {
		t.Helper()
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		res, err := ex.Execute(ctx, b)
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		return res
	}

	loc := func(name string, parent *domain.LocationID) domain.LocationID {
		t.Helper()
		return apply(pl.NewLocation(ctx, ops.NewLocationRequest{Name: name, Parent: parent})).
			Created[0].Locations[0]
	}
	cat := func(name string, parent *domain.CategoryID) domain.CategoryID {
		t.Helper()
		return apply(pl.NewCategory(ctx, ops.NewCategoryRequest{Name: name, Parent: parent})).
			Created[0].Categories[0]
	}

	var h house
	h.ctrl = app.Open(conn)

	h.garage = loc("Garage", nil)
	shelving := loc("Metal Shelving Unit", &h.garage)
	h.bay = loc("Bay 3", &shelving)
	crate := loc("Blue Crate", &h.bay)
	h.tray = loc("Small Parts Tray", &crate)
	kitchen := loc("Kitchen", nil)
	h.pantry = loc("Left Pantry", &kitchen)

	h.spices = cat("Spices", nil)
	h.peppers = cat("Dried Peppers", &h.spices)

	h.chile = apply(pl.NewItem(ctx, ops.NewItemRequest{
		Name: "Ancho Chile", Category: h.peppers,
		Counting: ops.CountingMeasured, ContentUnit: "g",
	})).Created[0].Items[0]
	h.rice = apply(pl.NewItem(ctx, ops.NewItemRequest{
		Name: "Basmati Rice", Category: h.spices,
		Counting: ops.CountingMeasured, ContentUnit: "g",
	})).Created[0].Items[0]

	// Three piles of chile: one on the tray at the bottom of the garage, one
	// at the garage ITSELF -- filing at a coarse node is never blocked, and it
	// is exactly the case a shallow reading has to be able to find -- and one
	// in the pantry, which is under a different root entirely.
	receive := func(item domain.ItemID, at domain.LocationID, grams int64) {
		t.Helper()
		apply(pl.Receive(ctx, ops.ReceiveRequest{
			Item: item, Location: at, Basis: domain.BasisContent,
			Amount: domain.FromMilli(grams * domain.Scale),
		}))
	}
	receive(h.chile, h.tray, 80)
	receive(h.chile, h.garage, 100)
	receive(h.chile, h.pantry, 120)
	receive(h.rice, h.pantry, 2000)

	return h
}

func places(rows []app.HoldingRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Item+" @ "+r.Location)
	}
	return out
}

func TestHoldingsUnderIsTheWholeHouseWhenRootIsNil(t *testing.T) {
	h := build(t)
	rows, err := h.ctrl.HoldingsUnder(context.Background(), nil, true)
	if err != nil {
		t.Fatalf("HoldingsUnder(nil): %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("the whole house is 4 holdings, got %d: %v", len(rows), places(rows))
	}
}

// The case the shell exists for: a rollup reaches the bottom of the tree, and
// picks up what was filed at the coarse node on the way down.
func TestHoldingsUnderRollsUpTheWholeSubtree(t *testing.T) {
	h := build(t)
	rows, err := h.ctrl.HoldingsUnder(context.Background(), &h.garage, true)
	if err != nil {
		t.Fatalf("HoldingsUnder(garage, deep): %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the garage holds 2 -- the tray's and its own -- got %d: %v", len(rows), places(rows))
	}
}

// Shallow is a different question, not a narrower one: it asks what was filed
// HERE, which is how something filed too coarsely becomes findable.
func TestHoldingsUnderShallowIsTheNodeAlone(t *testing.T) {
	h := build(t)
	rows, err := h.ctrl.HoldingsUnder(context.Background(), &h.garage, false)
	if err != nil {
		t.Fatalf("HoldingsUnder(garage, shallow): %v", err)
	}
	if len(rows) != 1 || rows[0].Location != "Garage" {
		t.Fatalf("only the pile filed at the garage itself, got %v", places(rows))
	}
}

// An empty shelf is an ordinary thing for a house to have, so it is an empty
// answer rather than an error.
func TestHoldingsUnderAnEmptyNodeIsEmptyRatherThanAnError(t *testing.T) {
	h := build(t)
	ctx := context.Background()

	deep, err := h.ctrl.HoldingsUnder(ctx, &h.bay, true)
	if err != nil {
		t.Fatalf("HoldingsUnder(bay, deep): %v", err)
	}
	if len(deep) != 1 {
		t.Fatalf("the bay rolls up the tray's 80 g, got %v", places(deep))
	}

	shallow, err := h.ctrl.HoldingsUnder(ctx, &h.bay, false)
	if err != nil {
		t.Fatalf("HoldingsUnder(bay, shallow): %v", err)
	}
	if len(shallow) != 0 {
		t.Fatalf("nothing is filed at the bay itself, got %v", places(shallow))
	}
}

func TestItemsUnderRollsUpTheCategoryTree(t *testing.T) {
	h := build(t)
	ctx := context.Background()

	deep, err := h.ctrl.ItemsUnder(ctx, &h.spices, true)
	if err != nil {
		t.Fatalf("ItemsUnder(spices, deep): %v", err)
	}
	if len(deep) != 2 {
		t.Fatalf("Spices and Dried Peppers hold 2 items between them, got %d", len(deep))
	}

	shallow, err := h.ctrl.ItemsUnder(ctx, &h.spices, false)
	if err != nil {
		t.Fatalf("ItemsUnder(spices, shallow): %v", err)
	}
	if len(shallow) != 1 || shallow[0].Name != "Basmati Rice" {
		t.Fatalf("only the rice is filed at Spices itself, got %+v", shallow)
	}
}

// One Item, three places -- the split the domain model exists to make visible,
// and what a search has to list once it has resolved a name.
func TestHoldingsOfItemFindsEveryPlacement(t *testing.T) {
	h := build(t)
	rows, err := h.ctrl.HoldingsOfItem(context.Background(), h.chile)
	if err != nil {
		t.Fatalf("HoldingsOfItem: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("the chile is in 3 places, got %d: %v", len(rows), places(rows))
	}
	for _, r := range rows {
		if r.Item != "Ancho Chile" {
			t.Errorf("HoldingsOfItem returned %q", r.Item)
		}
	}
}
