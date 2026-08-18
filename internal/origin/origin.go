// Package origin is the ORIGINATION write path: it brings entities into
// existence and writes their immutable birth facts.
//
// It contains no UPDATE and no DELETE, and archlint enforces the absence. That
// is not tidiness — origination is the highest-consequence path in the system
// with the narrowest window. An annotation error is a typo you fix. A recording
// error is caught by H10. An origination error is PERMANENT: set kind or
// content_unit wrong and the only remedy is retiring the entity and starting
// over.
//
// Holdings and Locations are absent here on purpose. Their ledger-derived state
// is *verified*, so their creation must be recorded as an event and therefore
// belongs to internal/ledger (schema §3.11). Category and Item are only
// audited, so backward closure suffices and they are created directly.
package origin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"home-management-system/internal/db"
	"home-management-system/internal/db/sqlc"
	"home-management-system/internal/domain"
)

// ErrInvalidInput reports a request that could never produce a valid row.
var ErrInvalidInput = errors.New("origin: invalid input")

// Originator creates entities.
type Originator struct {
	scope db.Scope
	q     *sqlc.Queries
}

// New originates against the pool, beginning a transaction per call.
func New(conn *sql.DB) *Originator { return newIn(db.Pool(conn)) }

// NewTx originates inside a transaction already in flight, so an origination
// can be part of a larger unit of work -- "I bought rice for the first time"
// creates the Item, creates the Holding, and records what arrived, and either
// all of it happens or none of it does.
func NewTx(tx *sql.Tx) *Originator { return newIn(db.Enlist(tx)) }

func newIn(s db.Scope) *Originator {
	return &Originator{scope: s, q: sqlc.New(s.Handle())}
}

// ---------------------------------------------------------------------------
// Category
// ---------------------------------------------------------------------------

// CreateCategoryInput describes a new Category.
type CreateCategoryInput struct {
	Name        string
	Parent      *domain.CategoryID
	Description string
}

// CreateCategory adds a classification node.
//
// No creation event: Category has no ledger at all, being a descriptive overlay
// rather than a model of physical reality.
func (o *Originator) CreateCategory(ctx context.Context, in CreateCategoryInput) (domain.CategoryID, error) {
	if in.Name == "" {
		return 0, fmt.Errorf("%w: category name is required", ErrInvalidInput)
	}

	var parent *int64
	if in.Parent != nil {
		p := int64(*in.Parent)
		parent = &p
	}

	id, err := o.q.InsertCategory(ctx, sqlc.InsertCategoryParams{
		ParentID:    db.NullInt64(parent),
		Name:        in.Name,
		Description: db.NullString(in.Description),
	})
	if err != nil {
		return 0, fmt.Errorf("origin: create category %q: %w", in.Name, err)
	}
	return domain.CategoryID(id), nil
}

// ---------------------------------------------------------------------------
// Item
// ---------------------------------------------------------------------------

// CreateUniqueItemInput describes a new one-of-a-kind Item.
type CreateUniqueItemInput struct {
	Name     string
	Category domain.CategoryID
	Notes    string
}

// CreateBulkItemInput describes a new measured Item.
type CreateBulkItemInput struct {
	Name        string
	Category    domain.CategoryID
	ContentUnit domain.UnitCode
	// PackageSize is the content-unit amount in one package. Nil means the item
	// has no package concept, which makes a Package-basis holding illegal (H7).
	PackageSize *domain.Quantity
	Notes       string
}

// CreateUniqueItem adds an Item whose holdings are individually tracked.
func (o *Originator) CreateUniqueItem(ctx context.Context, in CreateUniqueItemInput) (domain.ItemID, error) {
	if in.Name == "" {
		return 0, fmt.Errorf("%w: item name is required", ErrInvalidInput)
	}

	var id domain.ItemID
	err := o.scope.Run(ctx, func(tx *sql.Tx) error {
		q := o.q.WithTx(tx)
		raw, err := q.InsertItem(ctx, sqlc.InsertItemParams{
			Kind:       string(domain.KindUnique),
			Name:       in.Name,
			CategoryID: int64(in.Category),
			Notes:      db.NullString(in.Notes),
		})
		if err != nil {
			return fmt.Errorf("insert item: %w", err)
		}
		// O4: base and variant land together or not at all. A base row with no
		// variant row violates I3 and would surface much later as a hydration
		// failure with no obvious cause.
		if err := q.InsertUniqueItemVariant(ctx, raw); err != nil {
			return fmt.Errorf("insert unique variant: %w", err)
		}
		id = domain.ItemID(raw)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("origin: create unique item %q: %w", in.Name, err)
	}
	return id, nil
}

// CreateBulkItem adds an Item whose holdings are measured amounts.
func (o *Originator) CreateBulkItem(ctx context.Context, in CreateBulkItemInput) (domain.ItemID, error) {
	if in.Name == "" {
		return 0, fmt.Errorf("%w: item name is required", ErrInvalidInput)
	}
	if in.ContentUnit == "" {
		return 0, fmt.Errorf("%w: bulk item %q needs a content unit", ErrInvalidInput, in.Name)
	}
	if in.PackageSize != nil && !in.PackageSize.IsPositive() {
		return 0, fmt.Errorf("%w: package size must be positive, got %s", ErrInvalidInput, in.PackageSize)
	}

	packageSize := sql.NullInt64{}
	if in.PackageSize != nil {
		packageSize = sql.NullInt64{Int64: in.PackageSize.Milli(), Valid: true}
	}

	var id domain.ItemID
	err := o.scope.Run(ctx, func(tx *sql.Tx) error {
		q := o.q.WithTx(tx)
		raw, err := q.InsertItem(ctx, sqlc.InsertItemParams{
			Kind:       string(domain.KindBulk),
			Name:       in.Name,
			CategoryID: int64(in.Category),
			Notes:      db.NullString(in.Notes),
		})
		if err != nil {
			return fmt.Errorf("insert item: %w", err)
		}
		if err := q.InsertBulkItemVariant(ctx, sqlc.InsertBulkItemVariantParams{
			ItemID:      raw,
			ContentUnit: string(in.ContentUnit),
			PackageSize: packageSize,
		}); err != nil {
			return fmt.Errorf("insert bulk variant: %w", err)
		}
		id = domain.ItemID(raw)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("origin: create bulk item %q: %w", in.Name, err)
	}
	return id, nil
}
