// Package annotate is the ANNOTATION write path: it revises directly-mutable
// attributes on rows that already exist.
//
// It contains no INSERT, and archlint enforces the absence. Annotation is the
// lowest-consequence path — everything it writes is freely revisable and never
// verified, because replay does not reconstruct it and does not claim to.
//
// What is deliberately NOT here: an Item's kind, content unit, and package size.
// Those TYPE a Holding rather than label an Item, so changing one is a recorded
// event on the ledger, not an edit (schema §3.5).
package annotate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"home-management-system/internal/db"
	"home-management-system/internal/db/sqlc"
	"home-management-system/internal/domain"
)

var (
	// ErrCycle is C1: a Category may not be its own ancestor. Checked at write
	// time, which is what lets every read-side recursive query assume the tree
	// is acyclic.
	ErrCycle = errors.New("annotate: re-parenting would create a cycle")

	// ErrNotEmpty reports a Block resolution against a node that still has
	// children or contents.
	ErrNotEmpty = errors.New("annotate: node is not empty")

	// ErrNotFound reports a missing subject.
	ErrNotFound = errors.New("annotate: not found")

	// ErrInvalidInput reports a request that could never produce a valid row.
	ErrInvalidInput = errors.New("annotate: invalid input")

	// ErrItemInUse reports an attempt to archive an Item that still has active
	// Holdings, which would leave them unreachable through any active view.
	ErrItemInUse = errors.New("annotate: item still has active holdings")

	// ErrSlotTaken reports an expiry revision that would put a Holding where
	// another already is. H8 says those two would BE one Holding, and combining
	// them is a recording decision with events to show for it (O1) -- so the
	// annotation refuses rather than quietly making the invariant false.
	ErrSlotTaken = errors.New("annotate: another holding already occupies that slot")
)

// Annotator revises labels and knowledge.
type Annotator struct {
	scope db.Scope
	q     *sqlc.Queries
	now   func() time.Time
}

// New annotates against the pool, beginning a transaction per call.
func New(conn *sql.DB) *Annotator { return newIn(db.Pool(conn)) }

// NewTx annotates inside a transaction already in flight.
func NewTx(tx *sql.Tx) *Annotator { return newIn(db.Enlist(tx)) }

func newIn(s db.Scope) *Annotator {
	return &Annotator{scope: s, q: sqlc.New(s.Handle()), now: time.Now}
}

// WithClock replaces the time source, so tests can assert on exact timestamps.
func (a *Annotator) WithClock(now func() time.Time) *Annotator {
	clone := *a
	clone.now = now
	return &clone
}

// ---------------------------------------------------------------------------
// Category
// ---------------------------------------------------------------------------

func (a *Annotator) RenameCategory(ctx context.Context, id domain.CategoryID, name string) error {
	if name == "" {
		return fmt.Errorf("%w: category name is required", ErrInvalidInput)
	}
	if err := a.requireCategory(ctx, id); err != nil {
		return err
	}
	if err := a.q.UpdateCategoryName(ctx, sqlc.UpdateCategoryNameParams{
		Name: name, ID: int64(id),
	}); err != nil {
		return fmt.Errorf("annotate: rename category %d: %w", id, err)
	}
	return nil
}

func (a *Annotator) SetCategoryDescription(ctx context.Context, id domain.CategoryID, description string) error {
	if err := a.requireCategory(ctx, id); err != nil {
		return err
	}
	if err := a.q.UpdateCategoryDescription(ctx, sqlc.UpdateCategoryDescriptionParams{
		Description: db.NullString(description), ID: int64(id),
	}); err != nil {
		return fmt.Errorf("annotate: describe category %d: %w", id, err)
	}
	return nil
}

// ReparentCategory moves a node, refusing to create a cycle.
//
// Sibling name collisions are permitted: name uniqueness was never an integrity
// rule (schema §3.10), and real homes have two drawers called "junk drawer".
func (a *Annotator) ReparentCategory(ctx context.Context, id domain.CategoryID, parent *domain.CategoryID) error {
	if err := a.requireCategory(ctx, id); err != nil {
		return err
	}
	if parent != nil {
		if *parent == id {
			return fmt.Errorf("%w: category %d cannot be its own parent", ErrCycle, id)
		}
		if err := a.requireCategory(ctx, *parent); err != nil {
			return err
		}
		// C1: the prospective parent must not already be beneath the node.
		descendant, err := a.categoryHasAncestor(ctx, *parent, id)
		if err != nil {
			return err
		}
		if descendant {
			return fmt.Errorf("%w: %d is beneath %d", ErrCycle, *parent, id)
		}
	}

	var raw *int64
	if parent != nil {
		p := int64(*parent)
		raw = &p
	}
	if err := a.q.UpdateCategoryParent(ctx, sqlc.UpdateCategoryParentParams{
		ParentID: db.NullInt64(raw), ID: int64(id),
	}); err != nil {
		return fmt.Errorf("annotate: reparent category %d: %w", id, err)
	}
	return nil
}

// ArchiveCategory removes a node, disposing of its children and contents.
//
// Resolution is required rather than defaulted at this layer: what happens to
// the contents is the caller's decision, and silently picking one is how data
// ends up somewhere nobody expected. The UI defaults to Lift.
func (a *Annotator) ArchiveCategory(
	ctx context.Context,
	id domain.CategoryID,
	resolution domain.Resolution,
	moveTo *domain.CategoryID,
) error {
	if !resolution.Valid() {
		return fmt.Errorf("%w: unknown resolution %q", ErrInvalidInput, resolution)
	}
	if err := a.requireCategory(ctx, id); err != nil {
		return err
	}

	// Where children and contents go. Lift sends them to this node's parent —
	// which is NULL for a root, making its children roots in turn.
	var destination sql.NullInt64
	switch resolution {
	case domain.ResolutionLift:
		parent, err := a.q.GetCategoryParent(ctx, int64(id))
		if err != nil {
			return fmt.Errorf("annotate: read parent of %d: %w", id, err)
		}
		destination = parent
	case domain.ResolutionMove:
		if moveTo == nil {
			return fmt.Errorf("%w: Move resolution needs a destination", ErrInvalidInput)
		}
		if *moveTo == id {
			return fmt.Errorf("%w: cannot move contents into the node being archived", ErrInvalidInput)
		}
		if err := a.requireCategory(ctx, *moveTo); err != nil {
			return err
		}
		// Moving contents into a descendant would strand them under an archived
		// ancestor, so it is refused for the same reason a cycle is.
		beneath, err := a.categoryHasAncestor(ctx, *moveTo, id)
		if err != nil {
			return err
		}
		if beneath {
			return fmt.Errorf("%w: destination %d is beneath %d", ErrCycle, *moveTo, id)
		}
		destination = sql.NullInt64{Int64: int64(*moveTo), Valid: true}
	case domain.ResolutionBlock:
		children, err := a.q.CountLiveCategoryChildren(ctx, sql.NullInt64{Int64: int64(id), Valid: true})
		if err != nil {
			return fmt.Errorf("annotate: count children of %d: %w", id, err)
		}
		items, err := a.q.CountLiveCategoryItems(ctx, int64(id))
		if err != nil {
			return fmt.Errorf("annotate: count items of %d: %w", id, err)
		}
		if children > 0 || items > 0 {
			return fmt.Errorf("%w: category %d has %d children and %d items",
				ErrNotEmpty, id, children, items)
		}
	}

	at := db.FormatTime(a.now())
	return a.scope.Run(ctx, func(tx *sql.Tx) error {
		q := a.q.WithTx(tx)
		if resolution != domain.ResolutionBlock {
			if err := q.LiftCategoryChildren(ctx, sqlc.LiftCategoryChildrenParams{
				ParentID:   destination,
				ParentID_2: sql.NullInt64{Int64: int64(id), Valid: true},
			}); err != nil {
				return fmt.Errorf("reassign children: %w", err)
			}
			// Items must land somewhere real: category_id is NOT NULL, so a root
			// with items cannot be lifted without a destination.
			items, err := q.CountLiveCategoryItems(ctx, int64(id))
			if err != nil {
				return fmt.Errorf("count items: %w", err)
			}
			if items > 0 {
				if !destination.Valid {
					return fmt.Errorf("%w: category %d is a root with %d items; "+
						"lifting has nowhere to put them, so use Move", ErrInvalidInput, id, items)
				}
				if err := q.LiftCategoryItems(ctx, sqlc.LiftCategoryItemsParams{
					CategoryID: destination.Int64, CategoryID_2: int64(id),
				}); err != nil {
					return fmt.Errorf("reassign items: %w", err)
				}
			}
		}
		if err := q.SetCategoryArchived(ctx, sqlc.SetCategoryArchivedParams{
			ArchivedAt: sql.NullString{String: at, Valid: true}, ID: int64(id),
		}); err != nil {
			return fmt.Errorf("mark archived: %w", err)
		}
		return nil
	})
}

// RestoreCategory reverses an archival. Contents are not restored with it:
// they were reassigned, and where they went is now where they belong.
func (a *Annotator) RestoreCategory(ctx context.Context, id domain.CategoryID) error {
	if err := a.requireCategory(ctx, id); err != nil {
		return err
	}
	if err := a.q.SetCategoryArchived(ctx, sqlc.SetCategoryArchivedParams{
		ArchivedAt: sql.NullString{}, ID: int64(id),
	}); err != nil {
		return fmt.Errorf("annotate: restore category %d: %w", id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Location
// ---------------------------------------------------------------------------
//
// Only name and description. Everything else about a Location -- its parent,
// its existence, its archival -- is recorded, because Location models physical
// reality and Holdings reference it. The split is not arbitrary: a rename must
// be invisible to replay, and replay reconstructs containment, so a label may
// be annotated while a parent may not.

// RenameLocation changes a Location's label.
func (a *Annotator) RenameLocation(ctx context.Context, id domain.LocationID, name string) error {
	if name == "" {
		return fmt.Errorf("%w: location name is required", ErrInvalidInput)
	}
	if err := a.requireLocation(ctx, id); err != nil {
		return err
	}
	if err := a.q.UpdateLocationName(ctx, sqlc.UpdateLocationNameParams{Name: name, ID: int64(id)}); err != nil {
		return fmt.Errorf("annotate: rename location %d: %w", id, err)
	}
	return nil
}

// SetLocationDescription revises the free text on a Location.
func (a *Annotator) SetLocationDescription(ctx context.Context, id domain.LocationID, description string) error {
	if err := a.requireLocation(ctx, id); err != nil {
		return err
	}
	if err := a.q.UpdateLocationDescription(ctx, sqlc.UpdateLocationDescriptionParams{
		Description: db.NullString(description), ID: int64(id),
	}); err != nil {
		return fmt.Errorf("annotate: describe location %d: %w", id, err)
	}
	return nil
}

// requireLocation reports a missing Location as ErrNotFound rather than letting
// an UPDATE silently affect no rows.
func (a *Annotator) requireLocation(ctx context.Context, id domain.LocationID) error {
	ok, err := a.q.LocationIsLive(ctx, int64(id))
	if err != nil {
		return fmt.Errorf("annotate: read location %d: %w", id, err)
	}
	if ok == 0 {
		return fmt.Errorf("%w: location %d", ErrNotFound, id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Item
// ---------------------------------------------------------------------------

func (a *Annotator) RenameItem(ctx context.Context, id domain.ItemID, name string) error {
	if name == "" {
		return fmt.Errorf("%w: item name is required", ErrInvalidInput)
	}
	if err := a.q.UpdateItemName(ctx, sqlc.UpdateItemNameParams{Name: name, ID: int64(id)}); err != nil {
		return fmt.Errorf("annotate: rename item %d: %w", id, err)
	}
	return nil
}

// ReclassifyItem moves an Item to a different Category. A label change: no
// Holding attribute references a Category, so nothing about any Holding changes.
func (a *Annotator) ReclassifyItem(ctx context.Context, id domain.ItemID, category domain.CategoryID) error {
	if err := a.requireCategory(ctx, category); err != nil {
		return err
	}
	if err := a.q.UpdateItemCategory(ctx, sqlc.UpdateItemCategoryParams{
		CategoryID: int64(category), ID: int64(id),
	}); err != nil {
		return fmt.Errorf("annotate: reclassify item %d: %w", id, err)
	}
	return nil
}

func (a *Annotator) SetItemNotes(ctx context.Context, id domain.ItemID, notes string) error {
	if err := a.q.UpdateItemNotes(ctx, sqlc.UpdateItemNotesParams{
		Notes: db.NullString(notes), ID: int64(id),
	}); err != nil {
		return fmt.Errorf("annotate: annotate item %d: %w", id, err)
	}
	return nil
}

// ConfirmPlacement silences the classification nudge for one Item — the answer
// to "yes, this really does belong at Spices".
func (a *Annotator) ConfirmPlacement(ctx context.Context, id domain.ItemID) error {
	at := db.FormatTime(a.now())
	if err := a.q.SetItemPlacementConfirmed(ctx, sqlc.SetItemPlacementConfirmedParams{
		PlacementConfirmedAt: sql.NullString{String: at, Valid: true}, ID: int64(id),
	}); err != nil {
		return fmt.Errorf("annotate: confirm placement of item %d: %w", id, err)
	}
	return nil
}

// ArchiveItem retires a *kind*, which is a different act from retiring a thing.
// Blocked while active Holdings remain, since they would become unreachable
// through any active view.
func (a *Annotator) ArchiveItem(ctx context.Context, id domain.ItemID) error {
	holdings, err := a.q.CountLiveHoldingsOfItem(ctx, int64(id))
	if err != nil {
		return fmt.Errorf("annotate: count holdings of item %d: %w", id, err)
	}
	if holdings > 0 {
		return fmt.Errorf("%w: item %d has %d active holdings", ErrItemInUse, id, holdings)
	}
	at := db.FormatTime(a.now())
	if err := a.q.SetItemArchived(ctx, sqlc.SetItemArchivedParams{
		ArchivedAt: sql.NullString{String: at, Valid: true}, ID: int64(id),
	}); err != nil {
		return fmt.Errorf("annotate: archive item %d: %w", id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Holding
// ---------------------------------------------------------------------------
//
// Three fields, and the case for each being annotation rather than recording is
// slightly different.
//
// SetHoldingExpiry corrects what the packet says. It moves nothing and carries
// no quantity, so recording it would put a non-event in the ledger (E7) -- and
// a correction to a printed date is not a claim that the world changed.
//
// SnoozeHolding is a note to the person about the interface's own nagging. It
// has no physical meaning whatever.
//
// LabelHolding names which one of several this is, and names are annotation
// everywhere else in this system for the same reason.
//
// The consequence of the first one is worth stating plainly: expires_on is part
// of H8's key, so annotating it can MERGE two Holdings into one slot or split
// one apart. That is a real hazard and the reason SetHoldingExpiry refuses a
// date that would collide, rather than merging -- merging is a recording
// decision (O1) and belongs to the operations, not here.

// SetHoldingExpiry revises the date printed on a Holding.
func (a *Annotator) SetHoldingExpiry(ctx context.Context, id domain.HoldingID, on *time.Time) error {
	if err := a.requireHolding(ctx, id); err != nil {
		return err
	}
	if err := a.expiryWouldCollide(ctx, id, on); err != nil {
		return err
	}
	if err := a.q.UpdateHoldingExpiry(ctx, sqlc.UpdateHoldingExpiryParams{
		ExpiresOn: db.FormatNullDate(on), ID: int64(id),
	}); err != nil {
		return fmt.Errorf("annotate: set expiry on holding %d: %w", id, err)
	}
	return nil
}

// expiryWouldCollide refuses a date that would put this Holding in a slot
// another already occupies -- see ErrSlotTaken.
func (a *Annotator) expiryWouldCollide(ctx context.Context, id domain.HoldingID, on *time.Time) error {
	slot, err := a.q.BulkHoldingSlotOf(ctx, int64(id))
	if errors.Is(err, sql.ErrNoRows) {
		// Unique: no unit basis, so no H8 key, so nothing to collide with.
		return nil
	}
	if err != nil {
		return fmt.Errorf("annotate: read holding %d: %w", id, err)
	}
	n, err := a.q.CountHoldingsInSlot(ctx, sqlc.CountHoldingsInSlotParams{
		ID:               int64(id),
		ItemID:           slot.ItemID,
		StowedLocationID: slot.StowedLocationID,
		UnitBasis:        slot.UnitBasis,
		ExpiresOn:        expiryKey(on),
	})
	if err != nil {
		return fmt.Errorf("annotate: check slot of holding %d: %w", id, err)
	}
	if n > 0 {
		return fmt.Errorf("%w: %d other holdings already keep this item here on that date", ErrSlotTaken, n)
	}
	return nil
}

// expiryKey renders an expiry the way H8 compares it: by DATE, with an unknown
// date as one value rather than many, so two Holdings with no date share a slot.
func expiryKey(on *time.Time) sql.NullString {
	if on == nil {
		return sql.NullString{String: "", Valid: true}
	}
	return sql.NullString{String: on.UTC().Format(db.DateLayout), Valid: true}
}

// SnoozeHolding silences the expiry nudge until a date. A nil date clears it.
func (a *Annotator) SnoozeHolding(ctx context.Context, id domain.HoldingID, until *time.Time) error {
	if err := a.requireHolding(ctx, id); err != nil {
		return err
	}
	if err := a.q.UpdateHoldingSnooze(ctx, sqlc.UpdateHoldingSnoozeParams{
		SnoozedUntil: db.FormatNullTime(until), ID: int64(id),
	}); err != nil {
		return fmt.Errorf("annotate: snooze holding %d: %w", id, err)
	}
	return nil
}

// LabelHolding names one individually-tracked thing among several.
//
// Unique only, and not by omission: label lives on unique_holdings because a
// measured amount has nothing to distinguish. Asking to label a Bulk Holding is
// a category error rather than an unimplemented feature.
func (a *Annotator) LabelHolding(ctx context.Context, id domain.HoldingID, label string) error {
	unique, err := a.q.UniqueHoldingIsLive(ctx, int64(id))
	if err != nil {
		return fmt.Errorf("annotate: read holding %d: %w", id, err)
	}
	if unique == 0 {
		if err := a.requireHolding(ctx, id); err != nil {
			return err
		}
		return fmt.Errorf("%w: holding %d is measured, so there is nothing to label", ErrInvalidInput, id)
	}
	if err := a.q.UpdateUniqueHoldingLabel(ctx, sqlc.UpdateUniqueHoldingLabelParams{
		Label: db.NullString(label), HoldingID: int64(id),
	}); err != nil {
		return fmt.Errorf("annotate: label holding %d: %w", id, err)
	}
	return nil
}

// requireHolding reports a missing Holding as ErrNotFound rather than letting an
// UPDATE silently affect no rows.
func (a *Annotator) requireHolding(ctx context.Context, id domain.HoldingID) error {
	ok, err := a.q.HoldingIsLive(ctx, int64(id))
	if err != nil {
		return fmt.Errorf("annotate: read holding %d: %w", id, err)
	}
	if ok == 0 {
		return fmt.Errorf("%w: holding %d", ErrNotFound, id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (a *Annotator) requireCategory(ctx context.Context, id domain.CategoryID) error {
	exists, err := a.q.CategoryExists(ctx, int64(id))
	if err != nil {
		return fmt.Errorf("annotate: look up category %d: %w", id, err)
	}
	if exists == 0 {
		return fmt.Errorf("%w: category %d", ErrNotFound, id)
	}
	return nil
}

// categoryHasAncestor reports whether `ancestor` lies on the parent chain above
// `node`. It walks upward, so its cost is the depth of the tree.
func (a *Annotator) categoryHasAncestor(ctx context.Context, node, ancestor domain.CategoryID) (bool, error) {
	chain, err := a.q.CategoryAncestorIDs(ctx, int64(node))
	if err != nil {
		return false, fmt.Errorf("annotate: walk ancestors of %d: %w", node, err)
	}
	has, err := domain.HasAncestor(chain, ancestor)
	if err != nil {
		return false, fmt.Errorf("annotate: category %d: %w", node, err)
	}
	return has, nil
}
