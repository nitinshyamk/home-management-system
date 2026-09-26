// Package app is the UI-to-backend contract.
//
// A plain Go interface with ordinary methods, deliberately not the previous
// system's CQRS message layer: that cost roughly 600 lines of triple
// declaration -- a Query type, a Result type, and a handler method -- feeding
// one enormous type switch, for the same surface a method signature expresses in
// a line.
//
// The Controller composes the four write paths and the read path. It is the only
// place they meet, which keeps their boundaries intact everywhere else.
package app

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"home-management-system/internal/annotate"
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/ops"
	"home-management-system/internal/origin"
	"home-management-system/internal/query"
	"home-management-system/internal/resolve"
)

// Controller is what the UI depends on.
//
// Reads return flat, display-ready rows. Writes go one way only: bind a line or
// a command, PLAN it, show what it would do, and apply the plan the person
// agreed to. There is deliberately no method here that commits something
// directly -- there were four, for Categories, and nothing ever called them
// because everything the interface writes has to be showable first.
type Controller interface {
	// The trees, optionally with what they CONTAIN spliced in beneath each
	// node -- a Category's Items, a Location's Holdings.
	CategoryTree(ctx context.Context, withContents bool) ([]TreeRow, error)
	LocationTree(ctx context.Context, withContents bool) ([]TreeRow, error)
	Items(ctx context.Context) ([]ItemRow, error)
	Holdings(ctx context.Context) ([]HoldingRow, error)

	// What is inside one node of a tree. A nil root means the whole house,
	// which is the one node a forest does not have.
	HoldingsUnder(ctx context.Context, root *domain.LocationID, deep bool) ([]HoldingRow, error)
	ItemsUnder(ctx context.Context, root *domain.CategoryID, deep bool) ([]ItemRow, error)
	HoldingsOfItem(ctx context.Context, id domain.ItemID) ([]HoldingRow, error)

	HoldingHistory(ctx context.Context, id domain.HoldingID) ([]EventRow, error)
	Integrity(ctx context.Context) (IntegrityRow, error)
	SearchIndex(ctx context.Context) (*resolve.Index, error)
	// Units is the unit vocabulary, in the reference table's own order --
	// grouped by dimension, smallest first. Seven rows of reference data, so
	// the UI can offer them as a closed choice rather than free text.
	Units(ctx context.Context) ([]string, error)

	// The write surface. A Plan holds an unexported Batch, so the interface can
	// show what will happen and commit it without ever being able to assemble a
	// write of its own.
	Vocabulary(ctx context.Context) (*command.Vocabulary, error)
	BindLine(ctx context.Context, line string, subject command.Subject) (command.BindResult, error)
	PlanCommand(ctx context.Context, cmd command.Command) (Plan, error)
	PlanAll(ctx context.Context, commands []command.Command) (Plan, int, error)
	ApplyPlan(ctx context.Context, plan Plan) error
	Describe(ctx context.Context, cmd command.Command) string
	Nudges(ctx context.Context) ([]NudgeRow, error)
	// Attention is everything that wants answering, worst first -- the
	// expiry and custody flags, the ledger's own disagreements, and the
	// classification nudges, which were three lists on three screens.
	Attention(ctx context.Context) ([]Nagging, error)
}

// ---------------------------------------------------------------------------
// View rows: flat, display-ready, and deliberately free of domain types the UI
// would otherwise have to understand.
// ---------------------------------------------------------------------------

// TreeRow is one node of a Category or Location tree.
type TreeRow struct {
	ID    int64
	Name  string
	Depth int
	Count int64 // rollup over the subtree
	// Kind says what this row IS: "Category", "Location", "Item", "Holding".
	//
	// A tree used to hold only its own kind, so the view could say what a row
	// was. It cannot any more -- the Locations tree shows Holdings when asked
	// -- and the difference is not cosmetic: the put destination reads a row's
	// ID as a LocationID, so a row whose kind is assumed rather than known is a
	// silent write to whatever place shares that number.
	Kind string
	// Measure is what a contained row says for itself in the count column --
	// "120 g" for a holding, "280 g" on hand for an item. Empty for a
	// container, which shows its rollup instead.
	Measure string
}

// ItemRow is one Item.
type ItemRow struct {
	ID       domain.ItemID
	Name     string
	Kind     string
	Category string
	// CategoryID is the identifier behind Category, which the name cannot
	// stand in for: two categories at different paths may share a name, so
	// grouping items by the name puts some of them under the wrong parent.
	CategoryID domain.CategoryID
	Measure    string // "g, 2 kg packages" or "one of a kind"
	OnHand     string
}

// HoldingRow is one Holding.
//
// It carries both the NAMES and the IDENTIFIERS, and the identifiers are the
// point: a keystroke on this row builds a Command directly rather than
// serialising to a name and fuzzy-matching it back. That round trip is lossy in
// the worst way -- an ambiguous name could resolve to a different row than the
// one under the cursor, so `c` on the second of three Ancho Chiles would
// consume from the first.
type HoldingRow struct {
	ID       domain.HoldingID
	Item     string
	ItemID   domain.ItemID
	Kind     string
	Location string
	// LocationPath is the same place with its ancestors, root first
	// ("Garage > Bay 3 > Blue Crate"). It is what the table shows when the
	// column is wide enough for it, so that three rows of one item in three
	// different Shelf 1s can be told apart without leaving the table.
	//
	// Carried ALONGSIDE Location rather than replacing it, because the table
	// filters and sorts on the cell it is given: making the path the cell would
	// quietly turn `loc:garage` into "anywhere in the Garage" and sort the
	// column by branch instead of by name.
	LocationPath string
	// LocationID is where it is STOWED, which is what the stock commands mean
	// by a place -- not where a checked-out thing happens to be.
	LocationID domain.LocationID
	State      string // "1.8 kg" or "Out (garage)"
	Note       string // expiry, retirement, and other flags
	// Attention is how loudly Note reads. It is decided here, beside the
	// sentence, because this is the layer that knows why each flag is there.
	Attention Attention
	// Custody is "AtRest", "Out", "Lost", or "" for a measured Holding. It is
	// what lets one key toggle rather than two keys remember.
	Custody string
	// Retired marks a Holding that is done with, so an action can refuse it
	// before the ledger has to.
	Retired bool

	// OnHand is what the ledger says is there, for a Bulk Holding, and zero
	// for a Unique one -- which has a custody instead of an amount.
	//
	// Alongside State rather than instead of it. State SAYS the amount, in
	// the item's own unit and in words a person reads; this is the number.
	// The walk needs the number: confirming a count means sending the figure
	// the ledger holds back to it, and parsing that figure out of the
	// sentence the ledger wrote about itself would be a round trip through
	// prose for something nobody had to render in the first place.
	OnHand domain.Quantity
	// Unit is what OnHand is counted in, for the one place that has to write
	// the number and the unit itself rather than taking State whole.
	Unit domain.UnitCode
}

// Bulk reports the Holding having an amount rather than a custody.
//
// Named here, once, because it was spelled `Custody == ""` at four call
// sites -- a test of one field to answer a question about a different one,
// which reads as a bug every time somebody meets it.
func (h HoldingRow) Bulk() bool { return h.Kind == string(domain.KindBulk) }

// Attention is how urgently a row reads. Ordered, so the loudest flag on a
// row wins without the caller comparing strings.
type Attention int

const (
	// AttentionNone is the great majority of rows.
	AttentionNone Attention = iota
	// AttentionSoon wants an answer: a cable out three weeks, a jar with a
	// date coming up.
	AttentionSoon
	// AttentionOver is past it: expired, or gone.
	AttentionOver
)

// EventRow is one ledger entry, rendered.
type EventRow struct {
	Sequence int64
	When     time.Time
	Type     string
	Summary  string
}

// IntegrityRow is the outcome of the verification job.
type IntegrityRow struct {
	HoldingsChecked int
	Discrepancies   []string
	Orphans         []string
}

func (r IntegrityRow) Clean() bool {
	return len(r.Discrepancies) == 0 && len(r.Orphans) == 0
}

// NudgeRow is a classification nudge: an Item filed at a Category that has
// children, where a plausible sibling therefore exists.
type NudgeRow struct {
	Item     string
	Category string
	Siblings int64
}

// ---------------------------------------------------------------------------

type controller struct {
	read     *query.Reader
	proc     *ledger.Processor
	origin   *origin.Originator
	annotate *annotate.Annotator

	// planner and executor are the write surface. They are held here rather
	// than reached for, so there is exactly one place that can commit anything
	// on the interface's behalf.
	planner  *ops.Planner
	executor *ops.Executor
}

// Open assembles a Controller over a connection.
//
// One place knows how the paths fit together, which is what lets everything
// above this layer -- the entry point, the test harness -- reach a Controller
// without importing a write path itself. That is not tidiness: archlint asserts
// that internal/tui imports none of origin, ledger, or annotate, and before
// this existed the only way to get a Controller was to import all three.
func Open(conn *sql.DB) Controller { return OpenWithClock(conn, time.Now) }

// OpenWithClock is Open with the time source replaced, so a test can assert on
// exact timestamps.
//
// It is the only other way to build one. There used to be a New that took the
// four paths and left the planner and executor nil, so the Controller it
// returned read correctly and panicked on the first write -- and Open finished
// the job by asserting its way back through the interface it had just returned.
// A constructor that can hand back a half-built thing will eventually hand one
// back to somebody who does not know that.
func OpenWithClock(conn *sql.DB, now func() time.Time) Controller {
	return &controller{
		read:     query.New(conn),
		proc:     ledger.New(conn).WithClock(now),
		origin:   origin.New(conn),
		annotate: annotate.New(conn).WithClock(now),
		planner:  ops.NewPlanner(conn).WithClock(now),
		executor: ops.New(conn),
	}
}

// CategoryTree is the classification, optionally with each category's own Items
// spliced in beneath it.
//
// The items are grouped by CategoryID rather than by the category NAME, because
// two categories at different paths may share a name and grouping by it would
// file some items under the wrong parent. They are read in one pass -- Items()
// already returns every one -- rather than a query per node, which is one N+1
// more than the counts above already cost.
func (c *controller) CategoryTree(ctx context.Context, withContents bool) ([]TreeRow, error) {
	nodes, err := c.read.CategoryForest(ctx)
	if err != nil {
		return nil, err
	}
	contained := map[domain.CategoryID][]ItemRow{}
	if withContents {
		items, err := c.Items(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			contained[item.CategoryID] = append(contained[item.CategoryID], item)
		}
	}

	out := make([]TreeRow, 0, len(nodes))
	for _, n := range nodes {
		count, err := c.read.CountItemsInCategoryTree(ctx, n.Category.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, TreeRow{
			ID: int64(n.Category.ID), Name: n.Category.Name, Depth: n.Depth,
			Count: count, Kind: string(domain.EntityCategory),
		})
		for _, item := range contained[n.Category.ID] {
			out = append(out, TreeRow{
				ID: int64(item.ID), Name: item.Name, Depth: n.Depth + 1,
				Kind: string(domain.EntityItem), Measure: item.OnHand,
			})
		}
	}
	return out, nil
}

// LocationTree is the house, optionally with each place's own Holdings spliced
// in beneath it.
//
// Holdings() already carries the LocationID of every row, so the grouping is a
// bucket in memory and there is no query to add. That matters: there is no
// "holdings in this location" query at any layer, and adding one to render a
// display mode would be a schema-level answer to a screen-level question.
func (c *controller) LocationTree(ctx context.Context, withContents bool) ([]TreeRow, error) {
	nodes, err := c.read.LocationForest(ctx)
	if err != nil {
		return nil, err
	}
	contained := map[domain.LocationID][]HoldingRow{}
	if withContents {
		holdings, err := c.Holdings(ctx)
		if err != nil {
			return nil, err
		}
		for _, h := range holdings {
			contained[h.LocationID] = append(contained[h.LocationID], h)
		}
	}

	out := make([]TreeRow, 0, len(nodes))
	for _, n := range nodes {
		count, err := c.read.CountHoldingsInLocationTree(ctx, n.Location.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, TreeRow{
			ID: int64(n.Location.ID), Name: n.Location.Name, Depth: n.Depth,
			Count: count, Kind: string(domain.EntityLocation),
		})
		for _, h := range contained[n.Location.ID] {
			out = append(out, TreeRow{
				ID: int64(h.ID), Name: h.Item, Depth: n.Depth + 1,
				Kind: string(domain.EntityHolding), Measure: h.State,
			})
		}
	}
	return out, nil
}

func (c *controller) Items(ctx context.Context) ([]ItemRow, error) {
	items, err := c.read.Items(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ItemRow, 0, len(items))
	for _, item := range items {
		category, err := c.read.Category(ctx, item.Base().Category)
		if err != nil {
			return nil, err
		}
		onHand, err := c.read.OnHand(ctx, item)
		if err != nil {
			return nil, err
		}
		out = append(out, ItemRow{
			ID:         item.Base().ID,
			Name:       item.Base().Name,
			Kind:       string(item.Kind()),
			Category:   category.Name,
			CategoryID: item.Base().Category,
			Measure:    describeMeasure(item),
			OnHand:     onHand,
		})
	}
	return out, nil
}

func (c *controller) Holdings(ctx context.Context) ([]HoldingRow, error) {
	details, err := c.read.Holdings(ctx)
	if err != nil {
		return nil, err
	}
	// Where a checked-out thing went is a NAME, not the identifier the ledger
	// records. Reading the location tree once is cheaper than a lookup per row
	// and gives every row the same answer.
	where, paths, err := c.locationNames(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]HoldingRow, 0, len(details))
	for _, d := range details {
		base := d.Holding.Base()
		note, attention := describeFlags(d)
		row := HoldingRow{
			ID:           base.ID,
			Item:         d.ItemName,
			ItemID:       base.Item,
			Kind:         string(d.Holding.Kind()),
			Location:     d.LocationName,
			LocationPath: paths[int64(base.StowedLocation)],
			LocationID:   base.StowedLocation,
			State:        describeState(d, where),
			Note:         note,
			Attention:    attention,
			Retired:      base.RetiredAt != nil,
		}
		if u, ok := d.Holding.(domain.UniqueHolding); ok {
			row.Custody = string(u.Custody)
		}
		if b, ok := d.Holding.(domain.BulkHolding); ok {
			row.OnHand, row.Unit = b.Quantity, d.ContentUnit
		}
		out = append(out, row)
	}
	return out, nil
}

// locationNames maps location identifiers to their names.
type locationNames map[int64]string

func (n locationNames) Label(kind domain.EntityKind, id int64) string {
	if kind != domain.EntityLocation {
		return ""
	}
	return n[id]
}

// locationNames reads the tree once and returns both the leaf names and the
// full paths, because the two come from the same walk and a second pass over
// the forest to get the second of them would be a second answer to keep in
// step with the first.
//
// The path comes from resolve.PathStack -- the same assembly resolve.Build uses
// to give a Candidate its Path. It used to be a second copy of that walk, kept
// in step by a comment asking for it, and it has to agree: a path the table
// shows and a path the resolver accepts that disagreed would be two names for
// one shelf.
func (c *controller) locationNames(ctx context.Context) (locationNames, map[int64]string, error) {
	nodes, err := c.read.LocationForest(ctx)
	if err != nil {
		return nil, nil, err
	}
	out := make(locationNames, len(nodes))
	paths := make(map[int64]string, len(nodes))
	var stack resolve.PathStack
	for _, n := range nodes {
		out[int64(n.Location.ID)] = n.Location.Name
		paths[int64(n.Location.ID)] = stack.Push(n.Depth, n.Location.Name)
	}
	return out, paths, nil
}

func (c *controller) HoldingHistory(ctx context.Context, id domain.HoldingID) ([]EventRow, error) {
	events, err := c.proc.History(ctx, domain.SubjectHolding, int64(id))
	if err != nil {
		return nil, err
	}
	out := make([]EventRow, 0, len(events))
	for _, e := range events {
		out = append(out, EventRow{
			Sequence: int64(e.Base().ID),
			When:     e.Base().OccurredAt,
			Type:     string(e.Type()),
			Summary:  Summarise(e),
		})
	}
	return out, nil
}

func (c *controller) Integrity(ctx context.Context) (IntegrityRow, error) {
	report, err := c.proc.VerifyAll(ctx)
	if err != nil {
		return IntegrityRow{}, err
	}
	row := IntegrityRow{HoldingsChecked: report.HoldingsChecked}
	for _, d := range report.Discrepancies {
		row.Discrepancies = append(row.Discrepancies, d.String())
	}
	for _, id := range report.Orphans.Holdings {
		row.Orphans = append(row.Orphans, fmt.Sprintf("holding %d has no creation event", id))
	}
	for _, id := range report.Orphans.Locations {
		row.Orphans = append(row.Orphans, fmt.Sprintf("location %d has no creation event", id))
	}
	// H8 duplicates sit under Orphans rather than Discrepancies because they are
	// the same kind of finding: a row that exists but should not, as opposed to
	// a row whose value disagrees with its own history.
	for _, d := range report.Duplicates {
		row.Orphans = append(row.Orphans, d.String())
	}
	return row, nil
}

// SearchIndex is the one flat index of everything that can be referred to.
//
// One index, four consumers: the jump palette, inline autocomplete, the `:`
// line, and the bulk importer. If bulk import matched names differently from
// the interface, a CSV row and the equivalent typed command would resolve to
// different things, and the claim that they are one contract would be false.
// Units reads the unit codes, in the order the reference table declares.
//
// Straight off the read path rather than through Vocabulary, which holds them
// in a map -- and a map has no order to offer. It is also the whole vocabulary,
// which is a great deal of reading for seven rows the browse flow otherwise has
// no use for.
func (c *controller) Units(ctx context.Context) ([]string, error) {
	units, err := c.read.Units(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(units))
	for _, u := range units {
		out = append(out, string(u.Code))
	}
	return out, nil
}

func (c *controller) SearchIndex(ctx context.Context) (*resolve.Index, error) {
	return resolve.Build(ctx, c.read)
}

func (c *controller) Nudges(ctx context.Context) ([]NudgeRow, error) {
	nudges, err := c.read.ClassificationNudges(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]NudgeRow, 0, len(nudges))
	for _, n := range nudges {
		out = append(out, NudgeRow{Item: n.ItemName, Category: n.CategoryName, Siblings: n.ChildCount})
	}
	return out, nil
}
