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
	"fmt"
	"time"

	"home-management-system/internal/annotate"
	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/origin"
	"home-management-system/internal/query"
)

// Controller is what the UI depends on. v01 is read-only apart from Category
// operations, which exist so the application makes sense; the TUI does not call
// them.
type Controller interface {
	CategoryTree(ctx context.Context) ([]TreeRow, error)
	LocationTree(ctx context.Context) ([]TreeRow, error)
	Items(ctx context.Context) ([]ItemRow, error)
	Holdings(ctx context.Context) ([]HoldingRow, error)
	HoldingHistory(ctx context.Context, id domain.HoldingID) ([]EventRow, error)
	Integrity(ctx context.Context) (IntegrityRow, error)
	Nudges(ctx context.Context) ([]NudgeRow, error)

	CreateCategory(ctx context.Context, name string, parent *domain.CategoryID) (domain.CategoryID, error)
	RenameCategory(ctx context.Context, id domain.CategoryID, name string) error
	ReparentCategory(ctx context.Context, id domain.CategoryID, parent *domain.CategoryID) error
	ArchiveCategory(ctx context.Context, id domain.CategoryID, resolution domain.Resolution, moveTo *domain.CategoryID) error
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
}

// ItemRow is one Item.
type ItemRow struct {
	ID       domain.ItemID
	Name     string
	Kind     string
	Category string
	Measure  string // "g, 2 kg packages" or "one of a kind"
	OnHand   string
}

// HoldingRow is one Holding.
type HoldingRow struct {
	ID       domain.HoldingID
	Item     string
	Kind     string
	Location string
	State    string // "1.8 kg" or "Out (garage)"
	Note     string // expiry, retirement, and other flags
}

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
}

// New wires the paths together.
func New(read *query.Reader, proc *ledger.Processor, o *origin.Originator, a *annotate.Annotator) Controller {
	return &controller{read: read, proc: proc, origin: o, annotate: a}
}

func (c *controller) CategoryTree(ctx context.Context) ([]TreeRow, error) {
	nodes, err := c.read.CategoryForest(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TreeRow, 0, len(nodes))
	for _, n := range nodes {
		count, err := c.read.CountItemsInCategoryTree(ctx, n.Category.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, TreeRow{
			ID: int64(n.Category.ID), Name: n.Category.Name, Depth: n.Depth, Count: count,
		})
	}
	return out, nil
}

func (c *controller) LocationTree(ctx context.Context) ([]TreeRow, error) {
	nodes, err := c.read.LocationForest(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TreeRow, 0, len(nodes))
	for _, n := range nodes {
		count, err := c.read.CountHoldingsInLocationTree(ctx, n.Location.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, TreeRow{
			ID: int64(n.Location.ID), Name: n.Location.Name, Depth: n.Depth, Count: count,
		})
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
			ID:       item.Base().ID,
			Name:     item.Base().Name,
			Kind:     string(item.Kind()),
			Category: category.Name,
			Measure:  describeMeasure(item),
			OnHand:   onHand,
		})
	}
	return out, nil
}

func (c *controller) Holdings(ctx context.Context) ([]HoldingRow, error) {
	details, err := c.read.Holdings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]HoldingRow, 0, len(details))
	for _, d := range details {
		out = append(out, HoldingRow{
			ID:       d.Holding.Base().ID,
			Item:     d.ItemName,
			Kind:     string(d.Holding.Kind()),
			Location: d.LocationName,
			State:    describeState(d),
			Note:     describeFlags(d),
		})
	}
	return out, nil
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
	return row, nil
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

func (c *controller) CreateCategory(ctx context.Context, name string, parent *domain.CategoryID) (domain.CategoryID, error) {
	return c.origin.CreateCategory(ctx, origin.CreateCategoryInput{Name: name, Parent: parent})
}

func (c *controller) RenameCategory(ctx context.Context, id domain.CategoryID, name string) error {
	return c.annotate.RenameCategory(ctx, id, name)
}

func (c *controller) ReparentCategory(ctx context.Context, id domain.CategoryID, parent *domain.CategoryID) error {
	return c.annotate.ReparentCategory(ctx, id, parent)
}

func (c *controller) ArchiveCategory(ctx context.Context, id domain.CategoryID, resolution domain.Resolution, moveTo *domain.CategoryID) error {
	return c.annotate.ArchiveCategory(ctx, id, resolution, moveTo)
}
