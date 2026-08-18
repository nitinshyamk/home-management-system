package ops

import (
	"context"
	"fmt"

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
