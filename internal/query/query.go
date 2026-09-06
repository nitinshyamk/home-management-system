// Package query is the read path. It performs no writes of any kind, and
// archlint enforces that.
//
// Depth is derived from the parent chain, never stored — the previous system
// stored it and had to maintain it on every re-parent. It is computed here in Go
// rather than in SQL because sqlc's SQLite analyser cannot type a CTE column
// with no base-table origin; see the note in query_categories.sql.
package query

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"home-management-system/internal/db"
	"home-management-system/internal/db/sqlc"
	"home-management-system/internal/domain"
)

var (
	// ErrNotFound reports a missing subject.
	ErrNotFound = errors.New("query: not found")

	// ErrMissingVariant is the remaining half of I3/H5. "At most one variant
	// row" is declarative — a primary key. "At least one" cannot be expressed
	// declaratively, so it surfaces here, at hydration, rather than as a nil
	// dereference somewhere downstream.
	ErrMissingVariant = errors.New("query: item has no variant row")

	// ErrTreeTooLarge means a recursive query reached its row cap, which means
	// the tree is corrupt rather than merely large.
	ErrTreeTooLarge = errors.New("query: tree exceeds the traversal limit")
)

// Row caps matching the LIMIT clauses in query_categories.sql.
const (
	pathLimit       = 256
	descendantLimit = 4096
)

// Reader answers questions about current state.
type Reader struct {
	q *sqlc.Queries
}

func New(conn *sql.DB) *Reader { return &Reader{q: sqlc.New(conn)} }

// Node is a tree node with its derived depth, ready for display.
type Node struct {
	Category domain.Category
	Depth    int
}

// ---------------------------------------------------------------------------
// Categories
// ---------------------------------------------------------------------------

func (r *Reader) Category(ctx context.Context, id domain.CategoryID) (domain.Category, error) {
	row, err := r.q.GetCategory(ctx, int64(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Category{}, fmt.Errorf("%w: category %d", ErrNotFound, id)
		}
		return domain.Category{}, fmt.Errorf("query: get category %d: %w", id, err)
	}
	return categoryFrom(row.ID, row.ParentID, row.Name, row.Description, row.CreatedAt, row.ArchivedAt)
}

func (r *Reader) RootCategories(ctx context.Context) ([]domain.Category, error) {
	rows, err := r.q.ListRootCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("query: list root categories: %w", err)
	}
	out := make([]domain.Category, 0, len(rows))
	for _, row := range rows {
		c, err := categoryFrom(row.ID, row.ParentID, row.Name, row.Description, row.CreatedAt, row.ArchivedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func (r *Reader) ChildCategories(ctx context.Context, parent domain.CategoryID) ([]domain.Category, error) {
	rows, err := r.q.ListChildCategories(ctx, sql.NullInt64{Int64: int64(parent), Valid: true})
	if err != nil {
		return nil, fmt.Errorf("query: list children of %d: %w", parent, err)
	}
	out := make([]domain.Category, 0, len(rows))
	for _, row := range rows {
		c, err := categoryFrom(row.ID, row.ParentID, row.Name, row.Description, row.CreatedAt, row.ArchivedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// CategoryPath is D13: the ancestor chain, root first.
//
// The recursive query walks upward, so it yields the chain leaf-first. Rather
// than trust the traversal order, the chain is re-linked here from parent
// references — which is correct regardless of how SQLite orders its output.
func (r *Reader) CategoryPath(ctx context.Context, id domain.CategoryID) ([]domain.Category, error) {
	rows, err := r.q.CategoryPath(ctx, int64(id))
	if err != nil {
		return nil, fmt.Errorf("query: path of %d: %w", id, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: category %d", ErrNotFound, id)
	}
	if len(rows) >= pathLimit {
		return nil, fmt.Errorf("%w: ancestors of %d", ErrTreeTooLarge, id)
	}

	nodes := make([]domain.Category, 0, len(rows))
	for _, row := range rows {
		c, err := categoryFrom(row.ID, row.ParentID, row.Name, row.Description, row.CreatedAt, row.ArchivedAt)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, c)
	}
	return categoryTree.path(nodes, id), nil
}

// CategorySubtree is D14/D15: the node and everything beneath it, each with its
// derived depth, ordered for display.
//
// This is the derivation the whole non-leaf decision rests on — it is what lets
// Items sit at any node without breaking any report.
func (r *Reader) CategorySubtree(ctx context.Context, root domain.CategoryID) ([]Node, error) {
	rows, err := r.q.CategoryDescendants(ctx, int64(root))
	if err != nil {
		return nil, fmt.Errorf("query: descendants of %d: %w", root, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: category %d", ErrNotFound, root)
	}
	if len(rows) >= descendantLimit {
		return nil, fmt.Errorf("%w: descendants of %d", ErrTreeTooLarge, root)
	}

	nodes := make([]domain.Category, 0, len(rows))
	for _, row := range rows {
		c, err := categoryFrom(row.ID, row.ParentID, row.Name, row.Description, row.CreatedAt, row.ArchivedAt)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, c)
	}

	var out []Node
	if !categoryTree.subtree(nodes, root, func(c domain.Category, depth int) {
		out = append(out, Node{Category: c, Depth: depth})
	}) {
		return nil, fmt.Errorf("%w: category %d", ErrNotFound, root)
	}
	return out, nil
}

// CategoryForest returns every live root with its subtree, for the top-level
// browse view.
func (r *Reader) CategoryForest(ctx context.Context) ([]Node, error) {
	roots, err := r.RootCategories(ctx)
	if err != nil {
		return nil, err
	}
	var out []Node
	for _, root := range roots {
		nodes, err := r.CategorySubtree(ctx, root.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, nodes...)
	}
	return out, nil
}

// CountItemsInCategoryTree is a rollup: the metric summed over a node and its
// descendants.
func (r *Reader) CountItemsInCategoryTree(ctx context.Context, root domain.CategoryID) (int64, error) {
	n, err := r.q.CountItemsInCategoryTree(ctx, int64(root))
	if err != nil {
		return 0, fmt.Errorf("query: count items under %d: %w", root, err)
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// Items
// ---------------------------------------------------------------------------

func (r *Reader) Item(ctx context.Context, id domain.ItemID) (domain.Item, error) {
	row, err := r.q.GetItemWithVariant(ctx, int64(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: item %d", ErrNotFound, id)
		}
		return nil, fmt.Errorf("query: get item %d: %w", id, err)
	}
	return hydrateItem(itemRow{
		ID: row.ID, Kind: row.Kind, Name: row.Name, CategoryID: row.CategoryID,
		Notes: row.Notes, PlacementConfirmedAt: row.PlacementConfirmedAt,
		CreatedAt: row.CreatedAt, ArchivedAt: row.ArchivedAt,
		UniqueVariantID: row.UniqueVariantID, BulkVariantID: row.BulkVariantID,
		ContentUnit: row.ContentUnit, PackageSize: row.PackageSize,
	})
}

func (r *Reader) Items(ctx context.Context) ([]domain.Item, error) {
	rows, err := r.q.ListItemsWithVariant(ctx)
	if err != nil {
		return nil, fmt.Errorf("query: list items: %w", err)
	}
	out := make([]domain.Item, 0, len(rows))
	for _, row := range rows {
		item, err := hydrateItem(itemRow{
			ID: row.ID, Kind: row.Kind, Name: row.Name, CategoryID: row.CategoryID,
			Notes: row.Notes, PlacementConfirmedAt: row.PlacementConfirmedAt,
			CreatedAt: row.CreatedAt, ArchivedAt: row.ArchivedAt,
			UniqueVariantID: row.UniqueVariantID, BulkVariantID: row.BulkVariantID,
			ContentUnit: row.ContentUnit, PackageSize: row.PackageSize,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *Reader) ItemsInCategory(ctx context.Context, category domain.CategoryID) ([]domain.Item, error) {
	rows, err := r.q.ListItemsInCategoryWithVariant(ctx, int64(category))
	if err != nil {
		return nil, fmt.Errorf("query: list items in %d: %w", category, err)
	}
	out := make([]domain.Item, 0, len(rows))
	for _, row := range rows {
		item, err := hydrateItem(itemRow{
			ID: row.ID, Kind: row.Kind, Name: row.Name, CategoryID: row.CategoryID,
			Notes: row.Notes, PlacementConfirmedAt: row.PlacementConfirmedAt,
			CreatedAt: row.CreatedAt, ArchivedAt: row.ArchivedAt,
			UniqueVariantID: row.UniqueVariantID, BulkVariantID: row.BulkVariantID,
			ContentUnit: row.ContentUnit, PackageSize: row.PackageSize,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// ClassificationNudge is D20's raw material: Items filed directly at a Category
// that has children, whose placement has not been confirmed.
//
// It fires only where a plausible sibling exists — which is why the child count
// is part of the query rather than a post-filter. A generic "you filed this too
// shallow" nag is the version that becomes wallpaper.
type ClassificationNudge struct {
	Item         domain.ItemID
	ItemName     string
	Category     domain.CategoryID
	CategoryName string
	ChildCount   int64
}

func (r *Reader) ClassificationNudges(ctx context.Context) ([]ClassificationNudge, error) {
	rows, err := r.q.ListUnconfirmedItemsAtBranchCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("query: classification nudges: %w", err)
	}
	out := make([]ClassificationNudge, 0, len(rows))
	for _, row := range rows {
		out = append(out, ClassificationNudge{
			Item:         domain.ItemID(row.ID),
			ItemName:     row.Name,
			Category:     domain.CategoryID(row.CategoryID),
			CategoryName: row.CategoryName,
			ChildCount:   row.ChildCount,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Units
// ---------------------------------------------------------------------------

func (r *Reader) Units(ctx context.Context) ([]domain.Unit, error) {
	rows, err := r.q.ListUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("query: list units: %w", err)
	}
	out := make([]domain.Unit, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Unit{
			Code:         domain.UnitCode(row.Code),
			Dimension:    domain.Dimension(row.Dimension),
			ToBaseFactor: row.ToBaseFactor,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// hydration
// ---------------------------------------------------------------------------

func categoryFrom(id int64, parent sql.NullInt64, name string, description sql.NullString, createdAt string, archivedAt sql.NullString) (domain.Category, error) {
	created, err := db.ParseTime(createdAt)
	if err != nil {
		return domain.Category{}, fmt.Errorf("query: category %d created_at: %w", id, err)
	}
	archived, err := db.ParseNullTime(archivedAt)
	if err != nil {
		return domain.Category{}, fmt.Errorf("query: category %d archived_at: %w", id, err)
	}
	var parentID *domain.CategoryID
	if parent.Valid {
		p := domain.CategoryID(parent.Int64)
		parentID = &p
	}
	return domain.Category{
		ID:          domain.CategoryID(id),
		Parent:      parentID,
		Name:        name,
		Description: db.StringOrEmpty(description),
		CreatedAt:   created,
		ArchivedAt:  archived,
	}, nil
}

// itemRow is the shape every item query returns, so hydration has one
// implementation rather than three.
type itemRow struct {
	ID                   int64
	Kind                 string
	Name                 string
	CategoryID           int64
	Notes                sql.NullString
	PlacementConfirmedAt sql.NullString
	CreatedAt            string
	ArchivedAt           sql.NullString
	UniqueVariantID      sql.NullInt64
	BulkVariantID        sql.NullInt64
	ContentUnit          sql.NullString
	PackageSize          sql.NullInt64
}

func hydrateItem(row itemRow) (domain.Item, error) {
	created, err := db.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("query: item %d created_at: %w", row.ID, err)
	}
	confirmed, err := db.ParseNullTime(row.PlacementConfirmedAt)
	if err != nil {
		return nil, fmt.Errorf("query: item %d placement_confirmed_at: %w", row.ID, err)
	}
	archived, err := db.ParseNullTime(row.ArchivedAt)
	if err != nil {
		return nil, fmt.Errorf("query: item %d archived_at: %w", row.ID, err)
	}

	base := domain.ItemBase{
		ID:                   domain.ItemID(row.ID),
		Name:                 row.Name,
		Category:             domain.CategoryID(row.CategoryID),
		Notes:                db.StringOrEmpty(row.Notes),
		PlacementConfirmedAt: confirmed,
		CreatedAt:            created,
		ArchivedAt:           archived,
	}

	// The kind column selects the variant, and the variant row must be there.
	switch domain.Kind(row.Kind) {
	case domain.KindUnique:
		if !row.UniqueVariantID.Valid {
			return nil, fmt.Errorf("%w: item %d is Unique with no unique_items row", ErrMissingVariant, row.ID)
		}
		return domain.UniqueItem{ItemBase: base}, nil

	case domain.KindBulk:
		if !row.BulkVariantID.Valid {
			return nil, fmt.Errorf("%w: item %d is Bulk with no bulk_items row", ErrMissingVariant, row.ID)
		}
		item := domain.BulkItem{ItemBase: base, ContentUnit: domain.UnitCode(row.ContentUnit.String)}
		if row.PackageSize.Valid {
			size := domain.FromMilli(row.PackageSize.Int64)
			item.PackageSize = &size
		}
		return item, nil

	default:
		return nil, fmt.Errorf("query: item %d has unknown kind %q", row.ID, row.Kind)
	}
}

// ---------------------------------------------------------------------------
// Locations
// ---------------------------------------------------------------------------

// LocationNode is a Location with its derived depth.
type LocationNode struct {
	Location domain.Location
	Depth    int
}

func (r *Reader) Location(ctx context.Context, id domain.LocationID) (domain.Location, error) {
	row, err := r.q.GetLocation(ctx, int64(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Location{}, fmt.Errorf("%w: location %d", ErrNotFound, id)
		}
		return domain.Location{}, fmt.Errorf("query: get location %d: %w", id, err)
	}
	return locationFrom(row.ID, row.ParentID, row.Name, row.Description, row.CreatedAt, row.ArchivedAt)
}

func (r *Reader) RootLocations(ctx context.Context) ([]domain.Location, error) {
	rows, err := r.q.ListRootLocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("query: list root locations: %w", err)
	}
	out := make([]domain.Location, 0, len(rows))
	for _, row := range rows {
		l, err := locationFrom(row.ID, row.ParentID, row.Name, row.Description, row.CreatedAt, row.ArchivedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

// LocationPath renders with PRESENT-DAY names. History reading "Moved to Spice
// Shelf" tells you where to look today; "Moved to Shelf 2" is archaeologically
// faithful and practically useless.
func (r *Reader) LocationPath(ctx context.Context, id domain.LocationID) ([]domain.Location, error) {
	rows, err := r.q.LocationPath(ctx, int64(id))
	if err != nil {
		return nil, fmt.Errorf("query: path of location %d: %w", id, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: location %d", ErrNotFound, id)
	}
	if len(rows) >= pathLimit {
		return nil, fmt.Errorf("%w: ancestors of location %d", ErrTreeTooLarge, id)
	}

	nodes := make([]domain.Location, 0, len(rows))
	for _, row := range rows {
		l, err := locationFrom(row.ID, row.ParentID, row.Name, row.Description, row.CreatedAt, row.ArchivedAt)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, l)
	}
	return locationTree.path(nodes, id), nil
}

func (r *Reader) LocationSubtree(ctx context.Context, root domain.LocationID) ([]LocationNode, error) {
	rows, err := r.q.LocationDescendants(ctx, int64(root))
	if err != nil {
		return nil, fmt.Errorf("query: descendants of location %d: %w", root, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: location %d", ErrNotFound, root)
	}
	if len(rows) >= descendantLimit {
		return nil, fmt.Errorf("%w: descendants of location %d", ErrTreeTooLarge, root)
	}

	nodes := make([]domain.Location, 0, len(rows))
	for _, row := range rows {
		l, err := locationFrom(row.ID, row.ParentID, row.Name, row.Description, row.CreatedAt, row.ArchivedAt)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, l)
	}

	var out []LocationNode
	if !locationTree.subtree(nodes, root, func(l domain.Location, depth int) {
		out = append(out, LocationNode{Location: l, Depth: depth})
	}) {
		return nil, fmt.Errorf("%w: location %d", ErrNotFound, root)
	}
	return out, nil
}

func (r *Reader) LocationForest(ctx context.Context) ([]LocationNode, error) {
	roots, err := r.RootLocations(ctx)
	if err != nil {
		return nil, err
	}
	var out []LocationNode
	for _, root := range roots {
		nodes, err := r.LocationSubtree(ctx, root.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, nodes...)
	}
	return out, nil
}

func (r *Reader) CountHoldingsInLocationTree(ctx context.Context, root domain.LocationID) (int64, error) {
	n, err := r.q.CountHoldingsInLocationTree(ctx, int64(root))
	if err != nil {
		return 0, fmt.Errorf("query: count holdings under location %d: %w", root, err)
	}
	return n, nil
}

func locationFrom(id int64, parent sql.NullInt64, name string, description sql.NullString, createdAt string, archivedAt sql.NullString) (domain.Location, error) {
	created, err := db.ParseTime(createdAt)
	if err != nil {
		return domain.Location{}, fmt.Errorf("query: location %d created_at: %w", id, err)
	}
	archived, err := db.ParseNullTime(archivedAt)
	if err != nil {
		return domain.Location{}, fmt.Errorf("query: location %d archived_at: %w", id, err)
	}
	var parentID *domain.LocationID
	if parent.Valid {
		p := domain.LocationID(parent.Int64)
		parentID = &p
	}
	return domain.Location{
		ID: domain.LocationID(id), Parent: parentID, Name: name,
		Description: db.StringOrEmpty(description), CreatedAt: created, ArchivedAt: archived,
	}, nil
}

// ---------------------------------------------------------------------------
// Holdings
// ---------------------------------------------------------------------------

// HoldingDetail is a Holding with everything a display needs, already joined.
type HoldingDetail struct {
	Holding      domain.Holding
	ItemName     string
	LocationName string
	ContentUnit  domain.UnitCode
	PackageSize  *domain.Quantity
}

func (r *Reader) Holdings(ctx context.Context) ([]HoldingDetail, error) {
	rows, err := r.q.ListHoldingsWithDetail(ctx)
	if err != nil {
		return nil, fmt.Errorf("query: list holdings: %w", err)
	}
	out := make([]HoldingDetail, 0, len(rows))
	for _, row := range rows {
		d, err := hydrateHolding(holdingRow(row))
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// HoldingsOfItem returns every live Holding of one Item, wherever it is kept.
//
// This is what the composing operations plan against: consuming from a bag has
// to know which holdings exist and in which unit basis before it can decide
// whether a package must be opened.
// UnitDimension reports what a unit measures: Count, Mass, Volume, or Length.
func (r *Reader) UnitDimension(ctx context.Context, code domain.UnitCode) (string, error) {
	dimension, err := r.q.GetUnitDimension(ctx, string(code))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: unit %q", ErrNotFound, code)
		}
		return "", fmt.Errorf("query: unit %q: %w", code, err)
	}
	return dimension, nil
}

func (r *Reader) HoldingsOfItem(ctx context.Context, item domain.ItemID) ([]HoldingDetail, error) {
	rows, err := r.q.HoldingsOfItemWithDetail(ctx, int64(item))
	if err != nil {
		return nil, fmt.Errorf("query: holdings of item %d: %w", item, err)
	}
	out := make([]HoldingDetail, 0, len(rows))
	for _, row := range rows {
		d, err := hydrateHolding(holdingRow(sqlc.ListHoldingsWithDetailRow(row)))
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (r *Reader) Holding(ctx context.Context, id domain.HoldingID) (HoldingDetail, error) {
	row, err := r.q.GetHoldingWithDetail(ctx, int64(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return HoldingDetail{}, fmt.Errorf("%w: holding %d", ErrNotFound, id)
		}
		return HoldingDetail{}, fmt.Errorf("query: get holding %d: %w", id, err)
	}
	return hydrateHolding(holdingRow(sqlc.ListHoldingsWithDetailRow(row)))
}

// OnHand is D7, and it DISPATCHES ON KIND. For Bulk it is a sum of content
// quantities; for Unique it is a count of live holdings. The domain model
// assumed one formula, which is the clearest example of the variant split
// reaching the derivations.
func (r *Reader) OnHand(ctx context.Context, item domain.Item) (string, error) {
	switch typed := item.(type) {
	case domain.BulkItem:
		milli, err := r.q.BulkOnHand(ctx, int64(typed.ID))
		if err != nil {
			return "", fmt.Errorf("query: on hand for item %d: %w", typed.ID, err)
		}
		return fmt.Sprintf("%s %s", domain.FromMilli(toInt64(milli)), typed.ContentUnit), nil
	case domain.UniqueItem:
		n, err := r.q.UniqueOnHand(ctx, int64(typed.ID))
		if err != nil {
			return "", fmt.Errorf("query: on hand for item %d: %w", typed.ID, err)
		}
		if n == 1 {
			return "1 held", nil
		}
		return fmt.Sprintf("%d held", n), nil
	default:
		return "", fmt.Errorf("query: unknown item type %T", item)
	}
}

// toInt64 unwraps sqlc's interface{} result for an aggregate it cannot type.
func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		return 0
	}
}

type holdingRow sqlc.ListHoldingsWithDetailRow

func hydrateHolding(row holdingRow) (HoldingDetail, error) {
	created, err := db.ParseTime(row.CreatedAt)
	if err != nil {
		return HoldingDetail{}, fmt.Errorf("query: holding %d created_at: %w", row.ID, err)
	}
	expires, err := db.ParseNullTime(row.ExpiresOn)
	if err != nil {
		return HoldingDetail{}, fmt.Errorf("query: holding %d expires_on: %w", row.ID, err)
	}
	snoozed, err := db.ParseNullTime(row.SnoozedUntil)
	if err != nil {
		return HoldingDetail{}, fmt.Errorf("query: holding %d snoozed_until: %w", row.ID, err)
	}
	retired, err := db.ParseNullTime(row.RetiredAt)
	if err != nil {
		return HoldingDetail{}, fmt.Errorf("query: holding %d retired_at: %w", row.ID, err)
	}

	base := domain.HoldingBase{
		ID:             domain.HoldingID(row.ID),
		Item:           domain.ItemID(row.ItemID),
		StowedLocation: domain.LocationID(row.StowedLocationID),
		ExpiresOn:      expires,
		SnoozedUntil:   snoozed,
		RetiredAt:      retired,
		CreatedAt:      created,
	}
	detail := HoldingDetail{
		ItemName:     row.ItemName,
		LocationName: row.LocationName,
		ContentUnit:  domain.UnitCode(db.StringOrEmpty(row.ContentUnit)),
	}
	if row.PackageSize.Valid {
		size := domain.FromMilli(row.PackageSize.Int64)
		detail.PackageSize = &size
	}

	switch domain.Kind(row.Kind) {
	case domain.KindBulk:
		if !row.Quantity.Valid {
			return HoldingDetail{}, fmt.Errorf("%w: holding %d is Bulk with no bulk_holdings row",
				ErrMissingVariant, row.ID)
		}
		detail.Holding = domain.BulkHolding{
			HoldingBase: base,
			Quantity:    domain.FromMilli(row.Quantity.Int64),
			UnitBasis:   domain.UnitBasis(db.StringOrEmpty(row.UnitBasis)),
		}
	case domain.KindUnique:
		if !row.Custody.Valid {
			return HoldingDetail{}, fmt.Errorf("%w: holding %d is Unique with no unique_holdings row",
				ErrMissingVariant, row.ID)
		}
		since, err := db.ParseNullTime(row.CustodySince)
		if err != nil {
			return HoldingDetail{}, fmt.Errorf("query: holding %d custody_since: %w", row.ID, err)
		}
		h := domain.UniqueHolding{
			HoldingBase:  base,
			Label:        db.StringOrEmpty(row.Label),
			Custody:      domain.Custody(row.Custody.String),
			CustodySince: since,
		}
		if row.DisplacedToID.Valid {
			to := domain.LocationID(row.DisplacedToID.Int64)
			h.DisplacedTo = &to
		}
		detail.Holding = h
	default:
		return HoldingDetail{}, fmt.Errorf("query: holding %d has unknown kind %q", row.ID, row.Kind)
	}
	return detail, nil
}
