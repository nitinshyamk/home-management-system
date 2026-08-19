package ops

import (
	"context"
	"fmt"
	"time"

	"home-management-system/internal/annotate"
	"home-management-system/internal/domain"
)

// The Annotation variants. Nothing here is confirmed before it is applied,
// because everything here is freely revisable -- friction is proportional to
// permanence, and these have none.

// RenameLocation changes a Location's label.
//
// Annotation rather than recording, and the distinction is the whole reason
// this operation was missing: replay reconstructs containment, and a rename
// must be invisible to that reconstruction. Recording it would make the ledger
// imply something moved.
type RenameLocation struct {
	Location domain.LocationID
	Name     string
}

func (RenameLocation) isAnnotation() {}

func (r RenameLocation) Describe() string {
	return fmt.Sprintf("rename location %d to %q", r.Location, r.Name)
}

func (r RenameLocation) annotate(ctx context.Context, a *annotate.Annotator) error {
	return a.RenameLocation(ctx, r.Location, r.Name)
}

// DescribeLocation revises the free text on a Location.
type DescribeLocation struct {
	Location    domain.LocationID
	Description string
}

func (DescribeLocation) isAnnotation() {}

func (d DescribeLocation) Describe() string {
	return fmt.Sprintf("describe location %d", d.Location)
}

func (d DescribeLocation) annotate(ctx context.Context, a *annotate.Annotator) error {
	return a.SetLocationDescription(ctx, d.Location, d.Description)
}

// ArchiveItem retires a kind of thing from the active vocabulary.
//
// Annotation, not recording: archiving changes what the UI offers, not what
// exists or where it is. Nothing is deleted, so the row, its variant, its
// retired Holdings and all their events remain queryable by identifier.
type ArchiveItem struct {
	Item domain.ItemID
}

func (ArchiveItem) isAnnotation() {}

func (a ArchiveItem) Describe() string { return fmt.Sprintf("archive item %d", a.Item) }

func (a ArchiveItem) annotate(ctx context.Context, an *annotate.Annotator) error {
	return an.ArchiveItem(ctx, a.Item)
}

// ---------------------------------------------------------------------------
// Category
// ---------------------------------------------------------------------------

// RenameCategory changes a classification node's label.
type RenameCategory struct {
	Category domain.CategoryID
	Name     string
}

func (RenameCategory) isAnnotation() {}

func (r RenameCategory) Describe() string {
	return fmt.Sprintf("rename category %d to %q", r.Category, r.Name)
}

func (r RenameCategory) annotate(ctx context.Context, a *annotate.Annotator) error {
	return a.RenameCategory(ctx, r.Category, r.Name)
}

// DescribeCategory revises the free text on a Category.
type DescribeCategory struct {
	Category    domain.CategoryID
	Description string
}

func (DescribeCategory) isAnnotation() {}

func (d DescribeCategory) Describe() string { return fmt.Sprintf("describe category %d", d.Category) }

func (d DescribeCategory) annotate(ctx context.Context, a *annotate.Annotator) error {
	return a.SetCategoryDescription(ctx, d.Category, d.Description)
}

// ReparentCategory moves a classification node, and its subtree with it.
//
// Annotation rather than recording, unlike its Location counterpart: no Holding
// attribute references a Category, so re-filing one moves no object and replay
// has nothing to reconstruct.
type ReparentCategory struct {
	Category domain.CategoryID
	Parent   *domain.CategoryID // nil makes it a root
}

func (ReparentCategory) isAnnotation() {}

func (r ReparentCategory) Describe() string {
	if r.Parent == nil {
		return fmt.Sprintf("make category %d a root", r.Category)
	}
	return fmt.Sprintf("file category %d under %d", r.Category, *r.Parent)
}

func (r ReparentCategory) annotate(ctx context.Context, a *annotate.Annotator) error {
	return a.ReparentCategory(ctx, r.Category, r.Parent)
}

// ArchiveCategory puts a classification node away, re-filing its children and
// items according to the resolution.
type ArchiveCategory struct {
	Category   domain.CategoryID
	Resolution domain.Resolution
	MoveTo     *domain.CategoryID
}

func (ArchiveCategory) isAnnotation() {}

func (a ArchiveCategory) Describe() string {
	return fmt.Sprintf("archive category %d (%s)", a.Category, a.Resolution)
}

func (a ArchiveCategory) annotate(ctx context.Context, an *annotate.Annotator) error {
	return an.ArchiveCategory(ctx, a.Category, a.Resolution, a.MoveTo)
}

// RestoreCategory brings a classification node back. Its contents are not
// restored with it: they were re-filed, and undoing the archival does not
// un-file them.
type RestoreCategory struct {
	Category domain.CategoryID
}

func (RestoreCategory) isAnnotation() {}

func (r RestoreCategory) Describe() string { return fmt.Sprintf("restore category %d", r.Category) }

func (r RestoreCategory) annotate(ctx context.Context, a *annotate.Annotator) error {
	return a.RestoreCategory(ctx, r.Category)
}

// ---------------------------------------------------------------------------
// Item
// ---------------------------------------------------------------------------

// RenameItem changes what a kind of thing is called.
type RenameItem struct {
	Item domain.ItemID
	Name string
}

func (RenameItem) isAnnotation() {}

func (r RenameItem) Describe() string { return fmt.Sprintf("rename item %d to %q", r.Item, r.Name) }

func (r RenameItem) annotate(ctx context.Context, a *annotate.Annotator) error {
	return a.RenameItem(ctx, r.Item, r.Name)
}

// NoteItem revises the free text on an Item. Named for what it is rather than
// "describe", because an Item's notes are knowledge about the thing rather than
// a description of a tree node.
type NoteItem struct {
	Item  domain.ItemID
	Notes string
}

func (NoteItem) isAnnotation() {}

func (n NoteItem) Describe() string { return fmt.Sprintf("note item %d", n.Item) }

func (n NoteItem) annotate(ctx context.Context, a *annotate.Annotator) error {
	return a.SetItemNotes(ctx, n.Item, n.Notes)
}

// ReclassifyItem files an Item under a different Category.
type ReclassifyItem struct {
	Item     domain.ItemID
	Category domain.CategoryID
}

func (ReclassifyItem) isAnnotation() {}

func (r ReclassifyItem) Describe() string {
	return fmt.Sprintf("file item %d under category %d", r.Item, r.Category)
}

func (r ReclassifyItem) annotate(ctx context.Context, a *annotate.Annotator) error {
	return a.ReclassifyItem(ctx, r.Item, r.Category)
}

// ConfirmItem silences the classification nudge -- "yes, this really does
// belong here".
type ConfirmItem struct {
	Item domain.ItemID
}

func (ConfirmItem) isAnnotation() {}

func (c ConfirmItem) Describe() string { return fmt.Sprintf("confirm placement of item %d", c.Item) }

func (c ConfirmItem) annotate(ctx context.Context, a *annotate.Annotator) error {
	return a.ConfirmPlacement(ctx, c.Item)
}

// ---------------------------------------------------------------------------
// Holding
// ---------------------------------------------------------------------------

// SetHoldingExpiry corrects the date printed on a Holding.
//
// The only annotation that can be refused for a reason from the ledger's world:
// expires_on is part of H8's key, so a date another Holding already occupies
// would merge two into one, and merging is a recording decision.
type SetHoldingExpiry struct {
	Holding domain.HoldingID
	On      *time.Time // nil clears it
}

func (SetHoldingExpiry) isAnnotation() {}

func (s SetHoldingExpiry) Describe() string {
	if s.On == nil {
		return fmt.Sprintf("clear the expiry on holding %d", s.Holding)
	}
	return fmt.Sprintf("holding %d expires %s", s.Holding, s.On.Format("2006-01-02"))
}

func (s SetHoldingExpiry) annotate(ctx context.Context, a *annotate.Annotator) error {
	return a.SetHoldingExpiry(ctx, s.Holding, s.On)
}

// SnoozeHolding silences the expiry nudge for a while. A note to the person
// about the interface's own nagging; it has no physical meaning at all.
type SnoozeHolding struct {
	Holding domain.HoldingID
	Until   *time.Time // nil clears it
}

func (SnoozeHolding) isAnnotation() {}

func (s SnoozeHolding) Describe() string {
	if s.Until == nil {
		return fmt.Sprintf("stop snoozing holding %d", s.Holding)
	}
	return fmt.Sprintf("snooze holding %d until %s", s.Holding, s.Until.Format("2006-01-02"))
}

func (s SnoozeHolding) annotate(ctx context.Context, a *annotate.Annotator) error {
	return a.SnoozeHolding(ctx, s.Holding, s.Until)
}

// LabelHolding names one individually-tracked thing among several. Unique only:
// a measured amount has nothing to distinguish.
type LabelHolding struct {
	Holding domain.HoldingID
	Label   string
}

func (LabelHolding) isAnnotation() {}

func (l LabelHolding) Describe() string {
	return fmt.Sprintf("label holding %d %q", l.Holding, l.Label)
}

func (l LabelHolding) annotate(ctx context.Context, a *annotate.Annotator) error {
	return a.LabelHolding(ctx, l.Holding, l.Label)
}
