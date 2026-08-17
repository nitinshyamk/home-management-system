package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"home-management-system/internal/db"
	"home-management-system/internal/db/sqlc"
	"home-management-system/internal/domain"
)

// Replay reconstructs a Holding's ledger-derived state from the ledger alone.
//
// This is projection replay, not world reconstruction. The identifier is an
// INPUT -- the filter key -- never a folded value, and the Holding's kind comes
// off the row because it is immutable. The ledger is therefore not a backup: it
// reconstructs the ledger-derived attributes of entities that already exist, and
// nothing more (schema section 3.12).
//
// It folds through domain.Fold, the same function Apply uses. That is why the
// two cannot disagree, and why a discrepancy is always incompleteness.
func (p *Processor) Replay(ctx context.Context, id domain.HoldingID) (domain.Projection, error) {
	return p.replayWith(ctx, p.q, id)
}

func (p *Processor) replayWith(ctx context.Context, q *sqlc.Queries, id domain.HoldingID) (domain.Projection, error) {
	kind, err := holdingKind(ctx, q, id)
	if err != nil {
		return nil, err
	}

	start, through, err := p.checkpointFor(ctx, q, id, kind)
	if err != nil {
		return nil, err
	}

	rows, err := q.EventsForSubjectAfter(ctx, sqlc.EventsForSubjectAfterParams{
		SubjectKind: string(domain.SubjectHolding),
		SubjectID:   int64(id),
		ID:          through,
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: read events for holding %d: %w", id, err)
	}

	pl, err := loadPayloads(ctx, q, domain.SubjectHolding, int64(id))
	if err != nil {
		return nil, err
	}

	state := start
	for _, row := range rows {
		e, err := hydrateEvent(row, pl)
		if err != nil {
			return nil, err
		}
		state, err = domain.Fold(state, e)
		if err != nil {
			return nil, fmt.Errorf("ledger: replay holding %d at event %d (%s): %w",
				id, row.ID, row.Type, err)
		}
	}
	return state, nil
}

func holdingKind(ctx context.Context, q *sqlc.Queries, id domain.HoldingID) (domain.Kind, error) {
	row, err := q.GetHoldingProjection(ctx, int64(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: holding %d", ErrNotFound, id)
		}
		return "", fmt.Errorf("ledger: read holding %d: %w", id, err)
	}
	kind := domain.Kind(row.Kind)
	if !kind.Valid() {
		return "", fmt.Errorf("ledger: holding %d has unknown kind %q", id, row.Kind)
	}
	return kind, nil
}

// ---------------------------------------------------------------------------
// Checkpoints
// ---------------------------------------------------------------------------

// checkpointState is the serialized projection.
//
// It is a flat record with optional fields -- exactly the shape the domain model
// rejects for stored state. That is deliberate and is the proportionality rule
// in action (schema section 3.4): a checkpoint is rebuildable from the ledger,
// so a malformed one costs a rebuild, while a malformed event is permanent. The
// ledger gets 13 explicit payload tables; this gets a blob.
//
// It is never queried by field. The reader always wants all of it, to resume a
// replay.
type checkpointState struct {
	Kind           string     `json:"kind"`
	StowedLocation int64      `json:"stowed_location"`
	RetiredAt      *time.Time `json:"retired_at,omitempty"`

	QuantityMilli *int64 `json:"quantity_milli,omitempty"`

	Custody      *string    `json:"custody,omitempty"`
	CustodySince *time.Time `json:"custody_since,omitempty"`
	DisplacedTo  *int64     `json:"displaced_to,omitempty"`
}

func encodeProjection(p domain.Projection) ([]byte, error) {
	base := p.Base()
	state := checkpointState{
		Kind:           string(p.Kind()),
		StowedLocation: int64(base.StowedLocation),
		RetiredAt:      base.RetiredAt,
	}
	switch typed := p.(type) {
	case domain.BulkProjection:
		milli := typed.Quantity.Milli()
		state.QuantityMilli = &milli
	case domain.UniqueProjection:
		custody := string(typed.Custody)
		state.Custody = &custody
		state.CustodySince = typed.CustodySince
		if typed.DisplacedTo != nil {
			to := int64(*typed.DisplacedTo)
			state.DisplacedTo = &to
		}
	default:
		return nil, fmt.Errorf("ledger: cannot encode projection %T", p)
	}
	return json.Marshal(state)
}

func decodeProjection(raw []byte, kind domain.Kind) (domain.Projection, error) {
	var state checkpointState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("ledger: decode checkpoint: %w", err)
	}
	// The Holding's kind is authoritative, not the checkpoint's: kind is
	// immutable and lives on the row, so a checkpoint disagreeing means the
	// checkpoint is stale, and stale checkpoints are discarded rather than
	// trusted.
	if domain.Kind(state.Kind) != kind {
		return nil, fmt.Errorf("ledger: checkpoint says kind %q, holding says %q", state.Kind, kind)
	}

	base := domain.ProjectionBase{
		StowedLocation: domain.LocationID(state.StowedLocation),
		RetiredAt:      state.RetiredAt,
	}
	switch kind {
	case domain.KindBulk:
		if state.QuantityMilli == nil {
			return nil, fmt.Errorf("ledger: bulk checkpoint has no quantity")
		}
		return domain.BulkProjection{
			ProjectionBase: base,
			Quantity:       domain.FromMilli(*state.QuantityMilli),
		}, nil
	case domain.KindUnique:
		if state.Custody == nil {
			return nil, fmt.Errorf("ledger: unique checkpoint has no custody")
		}
		proj := domain.UniqueProjection{
			ProjectionBase: base,
			Custody:        domain.Custody(*state.Custody),
			CustodySince:   state.CustodySince,
		}
		if state.DisplacedTo != nil {
			to := domain.LocationID(*state.DisplacedTo)
			proj.DisplacedTo = &to
		}
		return proj, nil
	default:
		return nil, fmt.Errorf("ledger: unknown kind %q", kind)
	}
}

// checkpointFor returns the state to resume from and the sequence it covers.
// A missing or unreadable checkpoint is not an error: it means replay starts
// from zero, which is always correct and only slower.
func (p *Processor) checkpointFor(ctx context.Context, q *sqlc.Queries, id domain.HoldingID, kind domain.Kind) (domain.Projection, int64, error) {
	row, err := q.GetCheckpoint(ctx, int64(id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ZeroProjection(kind), 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("ledger: read checkpoint for holding %d: %w", id, err)
	}

	state, err := decodeProjection([]byte(row.Projection), kind)
	if err != nil {
		// Advisory only (K2). A checkpoint that cannot be read is discarded, not
		// fatal -- replaying from zero produces the same answer.
		return domain.ZeroProjection(kind), 0, nil
	}
	return state, row.ThroughSequence, nil
}

// WriteCheckpoint records a Holding's replayed state so later replays can
// resume from it. Bounded work: the nightly job replays one day of events
// rather than all history.
func (p *Processor) WriteCheckpoint(ctx context.Context, id domain.HoldingID) error {
	state, err := p.Replay(ctx, id)
	if err != nil {
		return err
	}
	through, err := p.q.LatestEventID(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		through = 0 // an empty ledger checkpoints at sequence zero
	} else if err != nil {
		return fmt.Errorf("ledger: read latest event id: %w", err)
	}
	blob, err := encodeProjection(state)
	if err != nil {
		return err
	}
	if err := p.q.UpsertCheckpoint(ctx, sqlc.UpsertCheckpointParams{
		HoldingID:       int64(id),
		ThroughSequence: through,
		AsOf:            db.FormatTime(p.now()),
		Projection:      string(blob),
	}); err != nil {
		return fmt.Errorf("ledger: write checkpoint for holding %d: %w", id, err)
	}
	return nil
}

// DiscardCheckpoints removes every checkpoint. K2 in action: this changes
// performance, never results, which is what makes the format a free choice.
func (p *Processor) DiscardCheckpoints(ctx context.Context) error {
	if err := p.q.DeleteAllCheckpoints(ctx); err != nil {
		return fmt.Errorf("ledger: discard checkpoints: %w", err)
	}
	return nil
}
