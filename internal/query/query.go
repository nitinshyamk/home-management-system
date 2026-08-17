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

	byID := make(map[domain.CategoryID]domain.Category, len(rows))
	for _, row := range rows {
		c, err := categoryFrom(row.ID, row.ParentID, row.Name, row.Description, row.CreatedAt, row.ArchivedAt)
		if err != nil {
			return nil, err
		}
		byID[c.ID] = c
	}

	// Walk from the requested node upward, then reverse.
	var reversed []domain.Category
	for cur, ok := byID[id], true; ok; {
		reversed = append(reversed, cur)
		if cur.Parent == nil {
			break
		}
		cur, ok = byID[*cur.Parent]
	}
	path := make([]domain.Category, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		path = append(path, reversed[i])
	}
	return path, nil
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

	children := map[domain.CategoryID][]domain.Category{}
	var start *domain.Category
	for _, row := range rows {
		c, err := categoryFrom(row.ID, row.ParentID, row.Name, row.Description, row.CreatedAt, row.ArchivedAt)
		if err != nil {
			return nil, err
		}
		if c.ID == root {
			copied := c
			start = &copied
			continue
		}
		if c.Parent != nil {
			children[*c.Parent] = append(children[*c.Parent], c)
		}
	}
	if start == nil {
		return nil, fmt.Errorf("%w: category %d", ErrNotFound, root)
	}

	// Depth-first, so the output reads as an indented tree. Names arrive sorted
	// from SQL, and map iteration never reorders a slice.
	var out []Node
	var walk func(c domain.Category, depth int)
	walk = func(c domain.Category, depth int) {
		out = append(out, Node{Category: c, Depth: depth})
		for _, child := range children[c.ID] {
			walk(child, depth+1)
		}
	}
	walk(*start, 0)
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
