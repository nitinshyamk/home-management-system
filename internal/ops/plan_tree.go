package ops

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/query"
)

// Planner turns an intent into a Batch without applying any of it.
//
// Planning is separated from executing so a review screen can say what will
// happen before it does, and so the interesting logic -- what archiving a node
// implies for its children and contents -- is testable by inspecting events
// rather than by inspecting a database afterwards.
type Planner struct {
	led *ledger.Processor
	r   *query.Reader
	now func() time.Time
}

func NewPlanner(conn *sql.DB) *Planner {
	return &Planner{led: ledger.New(conn), r: query.New(conn), now: time.Now}
}

// WithClock replaces the time source so tests can assert on exact timestamps.
func (p *Planner) WithClock(now func() time.Time) *Planner {
	clone := *p
	clone.led = p.led.WithClock(now)
	clone.now = now
	return &clone
}

// ArchiveLocationRequest removes a place, disposing of its children and
// contents. Resolution is required rather than defaulted: what happens to the
// contents is the caller's decision, and silently picking one is how things end
// up somewhere nobody expected. The UI defaults to Lift.
type ArchiveLocationRequest struct {
	Location   domain.LocationID
	Resolution domain.Resolution
	MoveTo     *domain.LocationID
}

// ArchiveLocation plans an archival.
//
// The Category counterpart lives in annotate and this one in ledger, and the
// split is the domain's rather than an implementation detail: archiving a
// Category relabels its items, archiving a Location moves its holdings. Same
// tree behaviour, different write path, because one holds classification and
// the other holds physical reality.
func (p *Planner) ArchiveLocation(ctx context.Context, req ArchiveLocationRequest) (Batch, error) {
	events, err := p.led.PlanArchiveLocation(ctx, req.Location, req.Resolution, req.MoveTo)
	if err != nil {
		return Batch{}, err
	}
	return Batch{Steps: []Step{{
		Summary: summariseArchive(req, events),
		Records: fixed(events),
	}}}, nil
}

// RestoreLocation plans the reversal of an archival. What was moved out stays
// where it went: undoing the archival does not un-happen the move.
func (p *Planner) RestoreLocation(ctx context.Context, id domain.LocationID) (Batch, error) {
	events, err := p.led.PlanRestoreLocation(ctx, id)
	if err != nil {
		return Batch{}, err
	}
	return Batch{Steps: []Step{{
		Summary: fmt.Sprintf("restore location %d", id),
		Records: fixed(events),
	}}}, nil
}

// RenameLocation plans a rename. No reads are needed, so no context.
func (p *Planner) RenameLocation(id domain.LocationID, name string) Batch {
	ann := RenameLocation{Location: id, Name: name}
	return Batch{Steps: []Step{{
		Summary:   ann.Describe(),
		Annotates: fixedAnnotations(ann),
	}}}
}

// DescribeLocation plans a description change.
func (p *Planner) DescribeLocation(id domain.LocationID, text string) Batch {
	ann := DescribeLocation{Location: id, Description: text}
	return Batch{Steps: []Step{{
		Summary:   ann.Describe(),
		Annotates: fixedAnnotations(ann),
	}}}
}

// fixed adapts events that need nothing from the Step's originations, which is
// every Step that creates nothing.
func fixed(events []domain.Event) func(Created) ([]domain.Event, error) {
	return func(Created) ([]domain.Event, error) { return events, nil }
}

func fixedAnnotations(as ...Annotation) func(Created) ([]Annotation, error) {
	return func(Created) ([]Annotation, error) { return as, nil }
}

// summariseArchive says what will happen in the terms a person cares about:
// how much moves, and where to. Counting the event types is deliberate -- the
// summary cannot claim something the plan does not actually do.
func summariseArchive(req ArchiveLocationRequest, events []domain.Event) string {
	var moved, reparented int
	for _, e := range events {
		switch e.(type) {
		case domain.Moved:
			moved++
		case domain.NodeReparented:
			reparented++
		}
	}
	if moved == 0 && reparented == 0 {
		return fmt.Sprintf("archive location %d (empty)", req.Location)
	}
	where := "to its parent"
	if req.Resolution == domain.ResolutionMove && req.MoveTo != nil {
		where = fmt.Sprintf("to location %d", *req.MoveTo)
	}
	return fmt.Sprintf("archive location %d, moving %d holdings and %d sublocations %s",
		req.Location, moved, reparented, where)
}
