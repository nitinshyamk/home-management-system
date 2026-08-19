package ledger

import (
	"context"
	"fmt"
	"sort"
	"time"

	"home-management-system/internal/domain"
)

// Discrepancy is one stored attribute disagreeing with what the ledger says it
// should be.
type Discrepancy struct {
	Holding  domain.HoldingID
	Field    string
	Stored   string
	Replayed string
}

func (d Discrepancy) String() string {
	return fmt.Sprintf("holding %d: %s is %s, ledger says %s", d.Holding, d.Field, d.Stored, d.Replayed)
}

// Report is the outcome of an integrity run.
type Report struct {
	HoldingsChecked int
	Discrepancies   []Discrepancy
	Orphans         Orphans
	Duplicates      []DuplicateSlot
}

// Clean reports whether nothing was found.
func (r Report) Clean() bool {
	return len(r.Discrepancies) == 0 && len(r.Orphans.Holdings) == 0 &&
		len(r.Orphans.Locations) == 0 && len(r.Duplicates) == 0
}

// Verify compares one Holding's stored state against the ledger.
//
// Because Apply and Replay fold through the same function, they cannot produce
// different answers from the same events. A discrepancy is therefore never a
// disagreement between two implementations -- it is always INCOMPLETENESS: a
// write that changed stored state without appending its event (O3 violated).
// That is what makes this check diagnostic rather than merely alarming.
func (p *Processor) Verify(ctx context.Context, id domain.HoldingID) ([]Discrepancy, error) {
	stored, _, err := loadProjection(ctx, p.q, id)
	if err != nil {
		return nil, err
	}
	replayed, err := p.Replay(ctx, id)
	if err != nil {
		return nil, err
	}
	return compare(id, stored, replayed), nil
}

// VerifyAll is the nightly integrity job.
//
// It REPORTS and never repairs. Silently correcting stored state would destroy
// the only signal that completeness was violated somewhere -- precisely the
// failure the ledger exists to make visible (schema section 9.3).
func (p *Processor) VerifyAll(ctx context.Context) (Report, error) {
	var report Report

	ids, err := p.q.ListHoldingIDs(ctx)
	if err != nil {
		return report, fmt.Errorf("ledger: list holdings: %w", err)
	}
	for _, raw := range ids {
		id := domain.HoldingID(raw)
		found, err := p.Verify(ctx, id)
		if err != nil {
			return report, err
		}
		report.HoldingsChecked++
		report.Discrepancies = append(report.Discrepancies, found...)
	}

	// A Holding with no creation event cannot be replayed meaningfully, so the
	// orphan check belongs in the same run.
	orphans, err := p.FindOrphans(ctx)
	if err != nil {
		return report, err
	}
	report.Orphans = orphans

	// H8 is upheld by the operations, so a violation means something below them
	// wrote a Holding the operations would never have created.
	duplicates, err := p.FindDuplicateSlots(ctx)
	if err != nil {
		return report, err
	}
	report.Duplicates = duplicates

	sort.Slice(report.Discrepancies, func(i, j int) bool {
		if report.Discrepancies[i].Holding != report.Discrepancies[j].Holding {
			return report.Discrepancies[i].Holding < report.Discrepancies[j].Holding
		}
		return report.Discrepancies[i].Field < report.Discrepancies[j].Field
	})
	return report, nil
}

// Checkpoint replays every Holding and records where it got to, so the next run
// resumes from there. Cost is bounded to the events since the last run rather
// than all history -- which is what makes checkpoints pay for themselves.
func (p *Processor) CheckpointAll(ctx context.Context) (int, error) {
	ids, err := p.q.ListHoldingIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("ledger: list holdings: %w", err)
	}
	for _, raw := range ids {
		if err := p.WriteCheckpoint(ctx, domain.HoldingID(raw)); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// compare diffs the ledger-derived attributes, and only those. Immutable and
// directly-mutable attributes are absent from a projection by construction (K3),
// so there is nothing here to accidentally over-claim.
func compare(id domain.HoldingID, stored, replayed domain.Projection) []Discrepancy {
	var out []Discrepancy
	add := func(field string, a, b any) {
		out = append(out, Discrepancy{
			Holding: id, Field: field,
			Stored: fmt.Sprint(a), Replayed: fmt.Sprint(b),
		})
	}

	sb, rb := stored.Base(), replayed.Base()
	if sb.StowedLocation != rb.StowedLocation {
		add("stowed_location", sb.StowedLocation, rb.StowedLocation)
	}
	if !sameTime(sb.RetiredAt, rb.RetiredAt) {
		add("retired_at", showTime(sb.RetiredAt), showTime(rb.RetiredAt))
	}

	switch s := stored.(type) {
	case domain.BulkProjection:
		r, ok := replayed.(domain.BulkProjection)
		if !ok {
			add("kind", stored.Kind(), replayed.Kind())
			return out
		}
		if s.Quantity.Cmp(r.Quantity) != 0 {
			add("quantity", s.Quantity, r.Quantity)
		}

	case domain.UniqueProjection:
		r, ok := replayed.(domain.UniqueProjection)
		if !ok {
			add("kind", stored.Kind(), replayed.Kind())
			return out
		}
		if s.Custody != r.Custody {
			add("custody", s.Custody, r.Custody)
		}
		if !sameTime(s.CustodySince, r.CustodySince) {
			add("custody_since", showTime(s.CustodySince), showTime(r.CustodySince))
		}
		if !sameLocation(s.DisplacedTo, r.DisplacedTo) {
			add("displaced_to", showLocation(s.DisplacedTo), showLocation(r.DisplacedTo))
		}
	}
	return out
}

// Timestamps are compared by instant rather than by pointer, and stored ones
// have been through a text round trip, so they are truncated to the second
// before comparison. Sub-second precision survives in the ledger but is not a
// difference worth reporting.
func sameTime(a, b *time.Time) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Truncate(time.Second).Equal(b.Truncate(time.Second))
	}
}

func showTime(t *time.Time) string {
	if t == nil {
		return "unset"
	}
	return t.UTC().Format(time.RFC3339)
}

func sameLocation(a, b *domain.LocationID) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

func showLocation(id *domain.LocationID) string {
	if id == nil {
		return "unset"
	}
	return fmt.Sprint(*id)
}
