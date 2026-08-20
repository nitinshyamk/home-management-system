// Command seed builds a sample house.
//
// Everything here goes through the real write paths: origin for Categories and
// Items, ledger for Locations, Holdings, and every event. Nothing touches the
// database directly. If the event primitives were insufficient to construct
// realistic state, this is where that would show up -- which is why the seed is
// part of the acceptance criteria rather than a convenience.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"home-management-system/internal/db"
	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/origin"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "seed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dbPath := flag.String("db-path", "", "path to the SQLite database")
	flag.Parse()

	cfg := db.DefaultConfig()
	if *dbPath != "" {
		cfg.DSN = *dbPath
	}
	conn, err := db.Open(cfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := db.Migrate(conn); err != nil {
		return err
	}

	ctx := context.Background()
	o := origin.New(conn)
	l := ledger.New(conn)

	s := &seeder{ctx: ctx, o: o, l: l, now: time.Now().UTC().Add(-90 * 24 * time.Hour)}
	if err := s.build(); err != nil {
		return err
	}

	fmt.Printf("seeded %s\n", cfg.DSN)
	fmt.Printf("  %d categories, %d locations, %d items, %d holdings, %d events\n",
		s.categories, s.locations, s.items, s.holdings, s.events)
	return nil
}

type seeder struct {
	ctx context.Context
	o   *origin.Originator
	l   *ledger.Processor
	now time.Time

	categories, locations, items, holdings, events int
}

// tick advances the seed clock, so events land in a plausible order rather than
// all at the same instant. The ledger refuses a zero OccurredAt, so every event
// carries a real one.
func (s *seeder) tick(d time.Duration) time.Time {
	s.now = s.now.Add(d)
	return s.now
}

func (s *seeder) at() domain.EventBase {
	return domain.EventBase{OccurredAt: s.tick(6 * time.Hour)}
}

func (s *seeder) category(name string, parent *domain.CategoryID) (domain.CategoryID, error) {
	id, err := s.o.CreateCategory(s.ctx, origin.CreateCategoryInput{Name: name, Parent: parent})
	if err != nil {
		return 0, fmt.Errorf("category %q: %w", name, err)
	}
	s.categories++
	return id, nil
}

func (s *seeder) location(name string, parent *domain.LocationID) (domain.LocationID, error) {
	id, err := s.l.CreateLocation(s.ctx, name, parent, "")
	if err != nil {
		return 0, fmt.Errorf("location %q: %w", name, err)
	}
	s.locations, s.events = s.locations+1, s.events+1
	return id, nil
}

func (s *seeder) apply(events ...domain.Event) error {
	if _, err := s.l.ApplyBatch(s.ctx, events); err != nil {
		return err
	}
	s.events += len(events)
	return nil
}

func (s *seeder) build() error {
	// --- the place tree, mirroring the domain model's example ---------------
	kitchen, err := s.location("Kitchen", nil)
	if err != nil {
		return err
	}
	spiceCabinet, err := s.location("Spice Cabinet", &kitchen)
	if err != nil {
		return err
	}
	shelf2, err := s.location("Shelf 2", &spiceCabinet)
	if err != nil {
		return err
	}
	pantry, err := s.location("Left Pantry", &kitchen)
	if err != nil {
		return err
	}
	shelf1, err := s.location("Shelf 1", &pantry)
	if err != nil {
		return err
	}
	office, err := s.location("Office", nil)
	if err != nil {
		return err
	}
	drawer, err := s.location("Desk Drawer 2", &office)
	if err != nil {
		return err
	}
	attic, err := s.location("Attic", nil)
	if err != nil {
		return err
	}
	garage, err := s.location("Garage", nil)
	if err != nil {
		return err
	}

	// --- the awkward cases, on purpose --------------------------------------
	//
	// A layout is only judged against what is on screen, so the cases that
	// break layouts have to be in the sample house rather than in someone's
	// imagination. Every one of these is here to be looked at:
	//
	//   a five-deep path, which no breadcrumb fits
	//   a name long enough to truncate in a column
	//   an Item held in three places at once
	//   a Unique thing checked out (the cable, below)
	//   an empty Category (Electronics, below)
	//
	// If a screen reads well with this house in it, it reads well.
	shelving, err := s.location("Metal Shelving Unit", &garage)
	if err != nil {
		return err
	}
	bay, err := s.location("Bay 3", &shelving)
	if err != nil {
		return err
	}
	// Five deep: Garage > Metal Shelving Unit > Bay 3 > Blue Crate > Small Parts
	crate, err := s.location("Blue Crate", &bay)
	if err != nil {
		return err
	}
	smallParts, err := s.location("Small Parts Tray", &crate)
	if err != nil {
		return err
	}

	// --- the classification tree -------------------------------------------
	spices, err := s.category("Spices", nil)
	if err != nil {
		return err
	}
	peppers, err := s.category("Dried Peppers", &spices)
	if err != nil {
		return err
	}
	pantryGoods, err := s.category("Pantry", nil)
	if err != nil {
		return err
	}
	electronics, err := s.category("Electronics", nil)
	if err != nil {
		return err
	}
	cables, err := s.category("Cables", &electronics)
	if err != nil {
		return err
	}
	storage, err := s.category("Storage", nil)
	if err != nil {
		return err
	}
	stationery, err := s.category("Stationery", nil)
	if err != nil {
		return err
	}

	// --- items --------------------------------------------------------------
	twoKg := domain.FromMilli(2_000_000)
	rice, err := s.bulkItem("Basmati Rice", pantryGoods, "g", &twoKg)
	if err != nil {
		return err
	}
	ancho, err := s.bulkItem("Ancho Chile", peppers, "g", nil)
	if err != nil {
		return err
	}
	// Filed at a branch category, which is what the classification nudge is for.
	cumin, err := s.bulkItem("Cumin", spices, "g", nil)
	if err != nil {
		return err
	}
	thumbtacks, err := s.bulkItem("Brass Thumbtack", stationery, "count", nil)
	if err != nil {
		return err
	}
	stamps, err := s.bulkItem("Forever Stamp", stationery, "count", nil)
	if err != nil {
		return err
	}
	// Long enough to truncate in any sensible column width, which is the point:
	// a name that fits is a name that proves nothing about the layout.
	adapter, err := s.uniqueItem(
		"Thunderbolt 4 to Dual DisplayPort 1.4 Adapter (Space Grey, 0.8m)", cables)
	if err != nil {
		return err
	}
	cable, err := s.uniqueItem("USB-C to HDMI Cable 2m", cables)
	if err != nil {
		return err
	}
	box, err := s.uniqueItem("Empty Computer Box", storage)
	if err != nil {
		return err
	}
	coat, err := s.uniqueItem("Winter Coat", storage)
	if err != nil {
		return err
	}

	// --- the rice: three sealed bags and one open one -----------------------
	//
	// The domain model's central example. Sealed and opened are separate
	// Holdings of the same Item, distinguished only by unit basis -- which is
	// what makes "sealed vs opened" derivable rather than stored.
	sealed, err := s.bulk(rice, shelf1, domain.BasisPackage)
	if err != nil {
		return err
	}
	opened, err := s.bulk(rice, shelf1, domain.BasisContent)
	if err != nil {
		return err
	}
	threeBags, err := domain.FromWhole(4)
	if err != nil {
		return err
	}
	if err := s.apply(
		domain.Acquired{EventBase: s.at(), Holding: sealed, Delta: threeBags, Source: "corner shop"},
	); err != nil {
		return err
	}
	// Opening a bag: one package off the sealed pool, its contents credited to
	// the open one. Both deltas are RESOLVED amounts, so a later package-size
	// change cannot rewrite what they meant.
	onePackage, err := domain.FromWhole(1)
	if err != nil {
		return err
	}
	negOne, err := onePackage.Neg()
	if err != nil {
		return err
	}
	if err := s.apply(
		domain.Split{EventBase: s.at(), Holding: sealed, Delta: negOne, Reason: "opened a bag"},
		domain.Opened{EventBase: s.at(), Holding: opened, Delta: twoKg},
	); err != nil {
		return err
	}
	// Twelve dinners at 100 g each.
	for i := 0; i < 12; i++ {
		if err := s.apply(domain.Consumed{
			EventBase: s.at(), Holding: opened, Delta: domain.FromMilli(-100_000), Reason: "dinner",
		}); err != nil {
			return err
		}
	}

	// --- spices -------------------------------------------------------------
	anchoHolding, err := s.bulk(ancho, shelf2, domain.BasisContent)
	if err != nil {
		return err
	}
	if err := s.apply(
		domain.Acquired{EventBase: s.at(), Holding: anchoHolding, Delta: domain.FromMilli(200_000)},
		domain.Consumed{EventBase: s.at(), Holding: anchoHolding, Delta: domain.FromMilli(-80_000), Reason: "chili"},
	); err != nil {
		return err
	}
	// The same Item in two places, which is what the Item/Holding split is for.
	anchoSpare, err := s.bulk(ancho, garage, domain.BasisContent)
	if err != nil {
		return err
	}
	if err := s.apply(domain.Acquired{
		EventBase: s.at(), Holding: anchoSpare, Delta: domain.FromMilli(120_000), Source: "bulk order",
	}); err != nil {
		return err
	}
	// And a third, five levels down, so that "where is my ancho chile" is a
	// question the interface has to answer well rather than incidentally.
	anchoDeep, err := s.bulk(ancho, smallParts, domain.BasisContent)
	if err != nil {
		return err
	}
	if err := s.apply(domain.Acquired{
		EventBase: s.at(), Holding: anchoDeep, Delta: domain.FromMilli(40_000), Source: "bulk order",
	}); err != nil {
		return err
	}

	cuminHolding, err := s.bulk(cumin, shelf2, domain.BasisContent)
	if err != nil {
		return err
	}
	if err := s.apply(domain.Acquired{
		EventBase: s.at(), Holding: cuminHolding, Delta: domain.FromMilli(90_000),
	}); err != nil {
		return err
	}

	// --- pooled durables: never reconciled exactly, corrected by counting ----
	tacks, err := s.bulk(thumbtacks, drawer, domain.BasisContent)
	if err != nil {
		return err
	}
	hundred, err := domain.FromWhole(100)
	if err != nil {
		return err
	}
	used, err := domain.FromWhole(-14)
	if err != nil {
		return err
	}
	observed, err := domain.FromWhole(82)
	if err != nil {
		return err
	}
	correction, err := domain.FromWhole(-4)
	if err != nil {
		return err
	}
	if err := s.apply(
		domain.Acquired{EventBase: s.at(), Holding: tacks, Delta: hundred, Source: "stationers"},
		domain.Consumed{EventBase: s.at(), Holding: tacks, Delta: used, Reason: "corkboard"},
		// A count that disagrees, and the separate adjustment it implies. The
		// observation never silently corrects the books.
		domain.Counted{EventBase: s.at(), Holding: tacks, Observed: observed},
		domain.Adjusted{EventBase: s.at(), Holding: tacks, Delta: correction, Reason: "recount"},
	); err != nil {
		return err
	}

	stampHolding, err := s.bulk(stamps, drawer, domain.BasisContent)
	if err != nil {
		return err
	}
	twenty, err := domain.FromWhole(20)
	if err != nil {
		return err
	}
	spent, err := domain.FromWhole(-17)
	if err != nil {
		return err
	}
	if err := s.apply(
		domain.Acquired{EventBase: s.at(), Holding: stampHolding, Delta: twenty},
		domain.Consumed{EventBase: s.at(), Holding: stampHolding, Delta: spent, Reason: "post"},
	); err != nil {
		return err
	}

	// --- the cable: checked out and not returned ----------------------------
	cableHolding, err := s.unique(cable, drawer, "the good 100W one")
	if err != nil {
		return err
	}
	if err := s.apply(domain.CheckedOut{
		EventBase: s.at(), Holding: cableHolding, DisplacedTo: &office,
	}); err != nil {
		return err
	}

	// The long-named adapter lives five levels down, so the widest name and the
	// deepest path appear on the same row.
	adapterHolding, err := s.unique(adapter, smallParts, "boxed, unopened")
	if err != nil {
		return err
	}
	_ = adapterHolding

	// --- the empty computer box: a possession that holds nothing ------------
	if _, err := s.unique(box, attic, ""); err != nil {
		return err
	}

	// --- the coat: stored, then donated -------------------------------------
	coatHolding, err := s.unique(coat, attic, "")
	if err != nil {
		return err
	}
	if err := s.apply(
		domain.Moved{EventBase: s.at(), Holding: coatHolding, From: attic, To: garage},
		domain.Gone{EventBase: s.at(), Holding: coatHolding, Reason: "donated"},
	); err != nil {
		return err
	}

	// --- a reorganization that must remain accountable ----------------------
	//
	// Re-parenting the drawer moves its contents with it, and none of their own
	// stowed locations change -- so without this event something moved and
	// nothing recorded why.
	if err := s.l.ReparentLocation(s.ctx, drawer, &garage); err != nil {
		return err
	}
	s.events++
	// Put it back, so the sample house reads sensibly.
	if err := s.l.ReparentLocation(s.ctx, drawer, &office); err != nil {
		return err
	}
	s.events++

	return nil
}

func (s *seeder) bulkItem(name string, category domain.CategoryID, unit domain.UnitCode, pkg *domain.Quantity) (domain.ItemID, error) {
	id, err := s.o.CreateBulkItem(s.ctx, origin.CreateBulkItemInput{
		Name: name, Category: category, ContentUnit: unit, PackageSize: pkg,
	})
	if err != nil {
		return 0, fmt.Errorf("item %q: %w", name, err)
	}
	s.items++
	return id, nil
}

func (s *seeder) uniqueItem(name string, category domain.CategoryID) (domain.ItemID, error) {
	id, err := s.o.CreateUniqueItem(s.ctx, origin.CreateUniqueItemInput{Name: name, Category: category})
	if err != nil {
		return 0, fmt.Errorf("item %q: %w", name, err)
	}
	s.items++
	return id, nil
}

func (s *seeder) bulk(item domain.ItemID, at domain.LocationID, basis domain.UnitBasis) (domain.HoldingID, error) {
	id, err := s.l.CreateBulkHolding(s.ctx, ledger.CreateBulkHoldingInput{
		Item: item, Location: at, UnitBasis: basis,
	})
	if err != nil {
		return 0, err
	}
	s.holdings, s.events = s.holdings+1, s.events+1
	return id, nil
}

func (s *seeder) unique(item domain.ItemID, at domain.LocationID, label string) (domain.HoldingID, error) {
	id, err := s.l.CreateUniqueHolding(s.ctx, ledger.CreateUniqueHoldingInput{
		Item: item, Location: at, Label: label,
	})
	if err != nil {
		return 0, err
	}
	s.holdings, s.events = s.holdings+1, s.events+1
	return id, nil
}
