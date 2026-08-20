package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/query"
	"home-management-system/internal/testsupport"
)

// The sample house is a review artefact, not a convenience.
//
// Every human-gated screen in Stage 10 is judged against it, and a layout is
// only ever judged against what is on screen -- so the cases that break layouts
// have to be IN it rather than in someone's imagination. This test is what
// keeps them there: an awkward case that quietly drops out of the seed is a
// case that stops being looked at, and nothing else would notice.
func TestTheSampleHouseKeepsItsAwkwardCases(t *testing.T) {
	ctx := context.Background()
	conn := testsupport.NewDB(t)
	if _, err := seed(ctx, conn, "test.db", time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("build: %v", err)
	}
	r := query.New(conn)

	t.Run("a path no breadcrumb fits", func(t *testing.T) {
		locations, err := r.LocationForest(ctx)
		if err != nil {
			t.Fatalf("read locations: %v", err)
		}
		deepest := 0
		for _, n := range locations {
			if n.Depth+1 > deepest {
				deepest = n.Depth + 1
			}
		}
		if deepest < 5 {
			t.Errorf("deepest location path is %d levels, want at least 5", deepest)
		}
	})

	t.Run("a name long enough to truncate", func(t *testing.T) {
		items, err := r.Items(ctx)
		if err != nil {
			t.Fatalf("read items: %v", err)
		}
		longest := ""
		for _, it := range items {
			if len(it.Base().Name) > len(longest) {
				longest = it.Base().Name
			}
		}
		// Wider than a sensible name column at 80 columns. A name that fits is a
		// name that proves nothing.
		if len(longest) < 40 {
			t.Errorf("longest item name is %q (%d chars), want one over 40", longest, len(longest))
		}
	})

	t.Run("one thing kept in three places", func(t *testing.T) {
		holdings, err := r.Holdings(ctx)
		if err != nil {
			t.Fatalf("read holdings: %v", err)
		}
		places := map[string]map[domain.LocationID]bool{}
		for _, h := range holdings {
			if places[h.ItemName] == nil {
				places[h.ItemName] = map[domain.LocationID]bool{}
			}
			places[h.ItemName][h.Holding.Base().StowedLocation] = true
		}
		most, name := 0, ""
		for item, p := range places {
			if len(p) > most {
				most, name = len(p), item
			}
		}
		if most < 3 {
			t.Errorf("the most-scattered item is %q in %d places, want one in 3", name, most)
		}
	})

	t.Run("something checked out", func(t *testing.T) {
		holdings, err := r.Holdings(ctx)
		if err != nil {
			t.Fatalf("read holdings: %v", err)
		}
		out := 0
		for _, h := range holdings {
			if u, ok := h.Holding.(domain.UniqueHolding); ok && u.Custody == domain.CustodyOut {
				out++
			}
		}
		if out == 0 {
			t.Error("nothing is checked out; the custody column is never exercised")
		}
	})

	t.Run("a classification with nothing in it", func(t *testing.T) {
		categories, err := r.CategoryForest(ctx)
		if err != nil {
			t.Fatalf("read categories: %v", err)
		}
		empty := 0
		for _, c := range categories {
			items, err := r.ItemsInCategory(ctx, c.Category.ID)
			if err != nil {
				t.Fatalf("read items: %v", err)
			}
			if len(items) == 0 {
				empty++
			}
		}
		if empty == 0 {
			t.Error("every classification has something in it; the empty state is never shown")
		}
	})

	t.Run("the deepest path also holds the longest name", func(t *testing.T) {
		// The two awkward cases have to meet on ONE row, or each is judged in
		// isolation and the layout is never asked the hard question.
		holdings, err := r.Holdings(ctx)
		if err != nil {
			t.Fatalf("read holdings: %v", err)
		}
		for _, h := range holdings {
			if len(h.ItemName) < 40 {
				continue
			}
			path, err := r.LocationPath(ctx, h.Holding.Base().StowedLocation)
			if err != nil {
				t.Fatalf("read path: %v", err)
			}
			if len(path) >= 5 {
				return
			}
		}
		t.Error("no row has both a long name and a deep path")
	})

	t.Run("it verifies", func(t *testing.T) {
		// The seed goes through the real write paths, so if the event
		// primitives were insufficient to build realistic state, it shows here.
		report, err := ledger.New(conn).VerifyAll(ctx)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if !report.Clean() {
			t.Errorf("the sample house does not verify: %+v", report)
		}
	})
}

// TestSeedingTwiceIsRefused is the defect a rendered review frame surfaced:
// two Kitchens, two Gardens, twenty-six locations.
//
// Nothing in the schema stops it, and nothing should -- duplicate names are
// legal by design (3.10), so a second Kitchen is a second Kitchen rather than
// an error. The refusal belongs here, in the program that knows it is building
// a SAMPLE house rather than adding to a real one.
func TestSeedingTwiceIsRefused(t *testing.T) {
	ctx := context.Background()
	conn := testsupport.NewDB(t)
	// Through seed(), which is the path the command takes. Calling
	// refuseIfOccupied directly would test that the guard exists rather than
	// that it is used, and deleting the call from run() would leave this green.
	build := func() error {
		_, err := seed(ctx, conn, "test.db", time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC))
		return err
	}

	if err := build(); err != nil {
		t.Fatalf("the first house: %v", err)
	}
	err := build()
	if err == nil {
		t.Fatal("a second house was built beside the first")
	}
	// The message has to say what to do about it, or it is just a refusal.
	if !strings.Contains(err.Error(), "--reset") {
		t.Errorf("the refusal does not say how to proceed: %v", err)
	}

	locations, readErr := query.New(conn).LocationForest(ctx)
	if readErr != nil {
		t.Fatalf("read locations: %v", readErr)
	}
	if len(locations) != 13 {
		t.Errorf("%d locations after a refused second seed, want the 13 of one house", len(locations))
	}
}
