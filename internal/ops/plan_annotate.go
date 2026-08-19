package ops

import (
	"context"
	"fmt"
	"time"

	"home-management-system/internal/domain"
)

// The Planner surface for annotations and the tree operations that are not
// about stock.
//
// Every method here has the same shape -- (ctx, Request) (Batch, error) -- and
// that uniformity is load-bearing rather than tidy: the command layer dispatches
// on it, and a test walks the command registry asserting that every Op has a
// Planner method of the same name. A method with a different signature would be
// a command that cannot be executed generically.
//
// Several take a ctx they never read. That is the price of the uniformity, and
// it is the cheaper side of the trade: a rename needs nothing from the database
// at plan time, but a caller that had to know which ones do would be a caller
// re-deriving what the registry already says.

// Target names something without saying which kind of thing it is, which is
// what a person does. "rename kitchen" does not say whether Kitchen is a
// Location or a Category, and the resolver is what decides.
//
// It carries a raw int64 rather than a typed identifier, deliberately and
// narrowly: this is the one boundary where the kind is data instead of a type,
// and the accessors below put the type back before anything else sees it.
type Target struct {
	Kind domain.EntityKind
	ID   int64
}

func (t Target) String() string { return fmt.Sprintf("%s %d", t.Kind, t.ID) }

// annotationBatch wraps one annotation as a Batch. Annotations never originate
// and never record, so a Step of one is the whole plan.
func annotationBatch(a Annotation) (Batch, error) {
	return Batch{Steps: []Step{{
		Summary:   a.Describe(),
		Annotates: fixedAnnotations(a),
	}}}, nil
}

// ---------------------------------------------------------------------------
// Polymorphic labels
// ---------------------------------------------------------------------------

// RenameRequest changes what something is called, whatever it is.
type RenameRequest struct {
	Target Target
	Name   string
}

// Rename dispatches on what the target turned out to be.
//
// One operation rather than three because renaming is one act: a person does
// not think "I am performing a Category rename", they think "this is called
// something else now". Holdings are absent because a Holding's name is its
// Item's -- what a Holding has is a label, which is a different command.
func (p *Planner) Rename(ctx context.Context, req RenameRequest) (Batch, error) {
	if req.Name == "" {
		return Batch{}, fmt.Errorf("%w: a new name is required", ErrInvalidRequest)
	}
	switch req.Target.Kind {
	case domain.EntityCategory:
		return annotationBatch(RenameCategory{Category: domain.CategoryID(req.Target.ID), Name: req.Name})
	case domain.EntityLocation:
		return annotationBatch(RenameLocation{Location: domain.LocationID(req.Target.ID), Name: req.Name})
	case domain.EntityItem:
		return annotationBatch(RenameItem{Item: domain.ItemID(req.Target.ID), Name: req.Name})
	}
	return Batch{}, fmt.Errorf("%w: %s has no name of its own; label it instead", ErrWrongKind, req.Target)
}

// DescribeRequest revises the free text on a tree node.
type DescribeRequest struct {
	Target      Target
	Description string
}

// Describe covers the two tree kinds. An Item's free text is knowledge about
// the thing rather than a description of a node, so it has its own command.
func (p *Planner) Describe(ctx context.Context, req DescribeRequest) (Batch, error) {
	switch req.Target.Kind {
	case domain.EntityCategory:
		return annotationBatch(DescribeCategory{
			Category: domain.CategoryID(req.Target.ID), Description: req.Description,
		})
	case domain.EntityLocation:
		return annotationBatch(DescribeLocation{
			Location: domain.LocationID(req.Target.ID), Description: req.Description,
		})
	}
	return Batch{}, fmt.Errorf("%w: %s is not a tree node; use note", ErrWrongKind, req.Target)
}

// ---------------------------------------------------------------------------
// Item
// ---------------------------------------------------------------------------

// NoteRequest revises what is known about a kind of thing.
type NoteRequest struct {
	Item  domain.ItemID
	Notes string
}

func (p *Planner) Note(ctx context.Context, req NoteRequest) (Batch, error) {
	return annotationBatch(NoteItem{Item: req.Item, Notes: req.Notes})
}

// ReclassifyRequest files an Item under a different Category.
type ReclassifyRequest struct {
	Item     domain.ItemID
	Category domain.CategoryID
}

func (p *Planner) Reclassify(ctx context.Context, req ReclassifyRequest) (Batch, error) {
	return annotationBatch(ReclassifyItem{Item: req.Item, Category: req.Category})
}

// ConfirmRequest answers the classification nudge.
type ConfirmRequest struct {
	Item domain.ItemID
}

func (p *Planner) Confirm(ctx context.Context, req ConfirmRequest) (Batch, error) {
	return annotationBatch(ConfirmItem{Item: req.Item})
}

// ArchiveItemRequest retires a kind of thing from the active vocabulary.
type ArchiveItemRequest struct {
	Item domain.ItemID
}

func (p *Planner) ArchiveItem(ctx context.Context, req ArchiveItemRequest) (Batch, error) {
	return annotationBatch(ArchiveItem{Item: req.Item})
}

// ---------------------------------------------------------------------------
// Holding labels
// ---------------------------------------------------------------------------

// SetExpiryRequest corrects the date printed on a Holding. A nil date clears it.
type SetExpiryRequest struct {
	Holding domain.HoldingID
	On      *time.Time
}

func (p *Planner) SetExpiry(ctx context.Context, req SetExpiryRequest) (Batch, error) {
	return annotationBatch(SetHoldingExpiry{Holding: req.Holding, On: req.On})
}

// SnoozeRequest silences the expiry nudge until a date. A nil date clears it.
type SnoozeRequest struct {
	Holding domain.HoldingID
	Until   *time.Time
}

func (p *Planner) Snooze(ctx context.Context, req SnoozeRequest) (Batch, error) {
	return annotationBatch(SnoozeHolding{Holding: req.Holding, Until: req.Until})
}

// LabelRequest names one individually-tracked thing among several.
type LabelRequest struct {
	Holding domain.HoldingID
	Label   string
}

func (p *Planner) Label(ctx context.Context, req LabelRequest) (Batch, error) {
	return annotationBatch(LabelHolding{Holding: req.Holding, Label: req.Label})
}

// ---------------------------------------------------------------------------
// Category tree
// ---------------------------------------------------------------------------

// ReparentCategoryRequest re-files a classification node and its subtree.
type ReparentCategoryRequest struct {
	Category domain.CategoryID
	Parent   *domain.CategoryID // nil makes it a root
}

func (p *Planner) ReparentCategory(ctx context.Context, req ReparentCategoryRequest) (Batch, error) {
	return annotationBatch(ReparentCategory{Category: req.Category, Parent: req.Parent})
}

// ArchiveCategoryRequest puts a classification node away.
type ArchiveCategoryRequest struct {
	Category   domain.CategoryID
	Resolution domain.Resolution
	MoveTo     *domain.CategoryID
}

func (p *Planner) ArchiveCategory(ctx context.Context, req ArchiveCategoryRequest) (Batch, error) {
	if !req.Resolution.Valid() {
		return Batch{}, fmt.Errorf("%w: unknown resolution %q", ErrInvalidRequest, req.Resolution)
	}
	return annotationBatch(ArchiveCategory{
		Category: req.Category, Resolution: req.Resolution, MoveTo: req.MoveTo,
	})
}

// RestoreCategoryRequest brings a classification node back.
type RestoreCategoryRequest struct {
	Category domain.CategoryID
}

func (p *Planner) RestoreCategory(ctx context.Context, req RestoreCategoryRequest) (Batch, error) {
	return annotationBatch(RestoreCategory{Category: req.Category})
}

// ---------------------------------------------------------------------------
// Location tree
// ---------------------------------------------------------------------------

// ReparentLocationRequest moves a place, and everything inside it with it.
//
// Recording rather than annotation, unlike its Category counterpart: the
// contents' own stowed_location never changes, so without an event something
// moved and nothing recorded why.
type ReparentLocationRequest struct {
	Location domain.LocationID
	Parent   *domain.LocationID // nil makes it a root
}

func (p *Planner) ReparentLocation(ctx context.Context, req ReparentLocationRequest) (Batch, error) {
	events, err := p.led.PlanReparentLocation(ctx, req.Location, req.Parent)
	if err != nil {
		return Batch{}, err
	}
	where := "the top level"
	if req.Parent != nil {
		where = fmt.Sprintf("location %d", *req.Parent)
	}
	return oneStep(fmt.Sprintf("moved location %d to %s", req.Location, where), events), nil
}
