// Package ledger is the RECORDING write path: the only writer of ledger-derived
// columns, and the only place events are appended.
//
// Every write to a ledger-derived attribute appends its event IN THE SAME
// TRANSACTION. That is O3, and it is the reason H10 is a completeness check
// rather than a correctness one: Apply and Replay both fold through
// domain.Fold, so they cannot disagree, and any discrepancy the integrity job
// reports is unambiguously a write that skipped its event.
//
// Holdings and Locations are created here rather than in internal/origin
// because their ledger-derived state is VERIFIED. Verification demands a
// reconstruction independent of the state being verified, so creation must be
// recorded as an event (schema section 3.11).
package ledger

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
	// ErrUnpersistable means writePayload has no case for an event type.
	// TestEveryEventTypePersists exists to make this unreachable.
	ErrUnpersistable = errors.New("ledger: no payload writer for event type")

	// ErrUnknownEventType means a stored row carries a type the domain does not
	// know. Reachable only if the schema and the domain drift apart.
	ErrUnknownEventType = errors.New("ledger: unknown event type in storage")

	// ErrMissingPayload is E5 read back: an event whose payload row is absent.
	// Not a zero value, because a silently empty payload would replay as a no-op.
	ErrMissingPayload = errors.New("ledger: event has no payload")

	// ErrNotFound reports a missing subject.
	ErrNotFound = errors.New("ledger: not found")

	// ErrWrongSubject means an event was applied to a subject of another kind.
	ErrWrongSubject = errors.New("ledger: event does not apply to this subject")

	// ErrInvalidInput reports a request that could never produce a valid row.
	ErrInvalidInput = errors.New("ledger: invalid input")

	// ErrCycle is L1: a Location may not be its own ancestor.
	ErrCycle = errors.New("ledger: re-parenting would create a cycle")
)

const ancestorScanLimit = 256

// Processor appends events and updates the projections they imply.
type Processor struct {
	scope db.Scope
	q     *sqlc.Queries
	now   func() time.Time
}

// New records against the pool, beginning a transaction per call.
func New(conn *sql.DB) *Processor { return newIn(db.Pool(conn)) }

// NewTx records inside a transaction already in flight, so events can be
// appended alongside writes from the other two paths as one unit of work.
func NewTx(tx *sql.Tx) *Processor { return newIn(db.Enlist(tx)) }

func newIn(s db.Scope) *Processor {
	return &Processor{scope: s, q: sqlc.New(s.Handle()), now: time.Now}
}

// WithClock replaces the time source so tests can assert on exact timestamps.
func (p *Processor) WithClock(now func() time.Time) *Processor {
	clone := *p
	clone.now = now
	return &clone
}

// ---------------------------------------------------------------------------
// Apply
// ---------------------------------------------------------------------------

// Apply records one event and updates the state it implies, atomically.
func (p *Processor) Apply(ctx context.Context, e domain.Event) (domain.EventID, error) {
	ids, err := p.ApplyBatch(ctx, []domain.Event{e})
	if err != nil {
		return 0, err
	}
	return ids[0], nil
}

// ApplyBatch records several events as one unit.
//
// Needed because some facts are only true together: promotion changes an Item's
// kind and restructures its Holdings, and a partially-promoted Item violates H1
// or H2 and must never be observable (O2).
func (p *Processor) ApplyBatch(ctx context.Context, events []domain.Event) ([]domain.EventID, error) {
	if len(events) == 0 {
		return nil, nil
	}
	ids := make([]domain.EventID, 0, len(events))

	err := p.scope.Run(ctx, func(tx *sql.Tx) error {
		q := p.q.WithTx(tx)
		ids = ids[:0]
		for i, e := range events {
			id, err := p.applyOne(ctx, q, e)
			if err != nil {
				return fmt.Errorf("event %d of %d (%s): %w", i+1, len(events), e.Type(), err)
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: apply: %w", err)
	}
	return ids, nil
}

// applyOne appends the event and updates its subject's projection.
//
// The order matters only in that both must happen inside the same transaction.
// Neither is permitted without the other — that is the completeness half of O3,
// and it is the only failure mode H10 can report.
func (p *Processor) applyOne(ctx context.Context, q *sqlc.Queries, e domain.Event) (domain.EventID, error) {
	// The ledger records WHEN something happened; it does not invent it.
	//
	// An earlier version substituted now() for a zero OccurredAt while storing
	// the event, then folded the caller's original — so Apply and Replay folded
	// the same function over DIFFERENT inputs, and custody_since diverged
	// permanently. Rejecting is the honest fix: a missing timestamp is missing
	// information, and the alternative is a silent, unrepairable divergence.
	if e.Base().OccurredAt.IsZero() {
		return 0, fmt.Errorf("%w: %s has no OccurredAt", ErrInvalidInput, e.Type())
	}

	kind, subject := e.Subject()

	id, err := p.append(ctx, q, e)
	if err != nil {
		return 0, err
	}

	switch kind {
	case domain.SubjectHolding:
		if err := p.applyToHolding(ctx, q, domain.HoldingID(subject), e); err != nil {
			return 0, err
		}
	case domain.SubjectLocation:
		if err := p.applyToLocation(ctx, q, domain.LocationID(subject), e); err != nil {
			return 0, err
		}
	case domain.SubjectItem:
		if err := p.applyToItem(ctx, q, domain.ItemID(subject), e); err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("%w: subject kind %q", ErrWrongSubject, kind)
	}
	return id, nil
}

// append writes the event base and its payload.
func (p *Processor) append(ctx context.Context, q *sqlc.Queries, e domain.Event) (domain.EventID, error) {
	kind, subject := e.Subject()
	base := e.Base()

	raw, err := q.InsertEvent(ctx, sqlc.InsertEventParams{
		SubjectKind: string(kind),
		SubjectID:   subject,
		Type:        string(e.Type()),
		OccurredAt:  db.FormatTime(base.OccurredAt),
		RecordedAt:  db.FormatTime(p.now()),
		Note:        db.NullString(base.Note),
	})
	if err != nil {
		return 0, fmt.Errorf("insert event: %w", err)
	}
	if err := writePayload(ctx, q, raw, e); err != nil {
		return 0, fmt.Errorf("insert payload: %w", err)
	}
	return domain.EventID(raw), nil
}

// ---------------------------------------------------------------------------
// Holdings
// ---------------------------------------------------------------------------

func (p *Processor) applyToHolding(ctx context.Context, q *sqlc.Queries, id domain.HoldingID, e domain.Event) error {
	current, kind, err := loadProjection(ctx, q, id)
	if err != nil {
		return err
	}

	// The single place event semantics live. Apply and Replay both call it,
	// which is why they cannot disagree.
	next, err := domain.Fold(current, e)
	if err != nil {
		return err
	}
	return writeProjection(ctx, q, id, kind, next)
}

// loadProjection reads a Holding's ledger-derived attributes, and only those.
func loadProjection(ctx context.Context, q *sqlc.Queries, id domain.HoldingID) (domain.Projection, domain.Kind, error) {
	row, err := q.GetHoldingProjection(ctx, int64(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", fmt.Errorf("%w: holding %d", ErrNotFound, id)
		}
		return nil, "", fmt.Errorf("read holding %d: %w", id, err)
	}

	retired, err := db.ParseNullTime(row.RetiredAt)
	if err != nil {
		return nil, "", fmt.Errorf("holding %d retired_at: %w", id, err)
	}
	base := domain.ProjectionBase{
		StowedLocation: domain.LocationID(row.StowedLocationID),
		RetiredAt:      retired,
	}

	switch domain.Kind(row.Kind) {
	case domain.KindBulk:
		if !row.Quantity.Valid {
			return nil, "", fmt.Errorf("holding %d is Bulk with no bulk_holdings row", id)
		}
		return domain.BulkProjection{
			ProjectionBase: base,
			Quantity:       domain.FromMilli(row.Quantity.Int64),
		}, domain.KindBulk, nil

	case domain.KindUnique:
		if !row.Custody.Valid {
			return nil, "", fmt.Errorf("holding %d is Unique with no unique_holdings row", id)
		}
		since, err := db.ParseNullTime(row.CustodySince)
		if err != nil {
			return nil, "", fmt.Errorf("holding %d custody_since: %w", id, err)
		}
		proj := domain.UniqueProjection{
			ProjectionBase: base,
			Custody:        domain.Custody(row.Custody.String),
			CustodySince:   since,
		}
		if row.DisplacedToID.Valid {
			to := domain.LocationID(row.DisplacedToID.Int64)
			proj.DisplacedTo = &to
		}
		return proj, domain.KindUnique, nil

	default:
		return nil, "", fmt.Errorf("holding %d has unknown kind %q", id, row.Kind)
	}
}

// writeProjection stores the folded state. Base and variant are separate
// statements because no single UPDATE spans two tables.
func writeProjection(ctx context.Context, q *sqlc.Queries, id domain.HoldingID, kind domain.Kind, p domain.Projection) error {
	base := p.Base()
	if err := q.UpdateHoldingBaseProjection(ctx, sqlc.UpdateHoldingBaseProjectionParams{
		StowedLocationID: int64(base.StowedLocation),
		RetiredAt:        db.FormatNullTime(base.RetiredAt),
		ID:               int64(id),
	}); err != nil {
		return fmt.Errorf("write holding %d base projection: %w", id, err)
	}

	switch typed := p.(type) {
	case domain.BulkProjection:
		if err := q.UpdateBulkHoldingProjection(ctx, sqlc.UpdateBulkHoldingProjectionParams{
			Quantity: typed.Quantity.Milli(), HoldingID: int64(id),
		}); err != nil {
			return fmt.Errorf("write holding %d quantity: %w", id, err)
		}
	case domain.UniqueProjection:
		var displaced sql.NullInt64
		if typed.DisplacedTo != nil {
			displaced = sql.NullInt64{Int64: int64(*typed.DisplacedTo), Valid: true}
		}
		if err := q.UpdateUniqueHoldingProjection(ctx, sqlc.UpdateUniqueHoldingProjectionParams{
			Custody:       string(typed.Custody),
			CustodySince:  db.FormatNullTime(typed.CustodySince),
			DisplacedToID: displaced,
			HoldingID:     int64(id),
		}); err != nil {
			return fmt.Errorf("write holding %d custody: %w", id, err)
		}
	default:
		return fmt.Errorf("unknown projection type %T for holding %d", p, id)
	}
	return nil
}

// CreateBulkHoldingInput describes a new measured Holding.
type CreateBulkHoldingInput struct {
	Item      domain.ItemID
	Location  domain.LocationID
	UnitBasis domain.UnitBasis
	ExpiresOn *time.Time
}

// CreateUniqueHoldingInput describes a new individually-tracked Holding.
type CreateUniqueHoldingInput struct {
	Item     domain.ItemID
	Location domain.LocationID
	Label    string
}

// CreateBulkHolding brings a Holding into existence at quantity zero.
//
// Zero is a real starting value needing no event; only stowed_location has no
// natural empty value, which is the whole reason HoldingCreated exists.
func (p *Processor) CreateBulkHolding(ctx context.Context, in CreateBulkHoldingInput) (domain.HoldingID, error) {
	if !in.UnitBasis.Valid() {
		return 0, fmt.Errorf("%w: unknown unit basis %q", ErrInvalidInput, in.UnitBasis)
	}
	return p.createHolding(ctx, in.Item, in.Location, domain.KindBulk, func(ctx context.Context, q *sqlc.Queries, id int64) error {
		return q.InsertBulkHoldingVariant(ctx, sqlc.InsertBulkHoldingVariantParams{
			HoldingID: id, UnitBasis: string(in.UnitBasis),
		})
	})
}

// CreateUniqueHolding brings an individually-tracked Holding into existence,
// AtRest at its stowed location.
func (p *Processor) CreateUniqueHolding(ctx context.Context, in CreateUniqueHoldingInput) (domain.HoldingID, error) {
	return p.createHolding(ctx, in.Item, in.Location, domain.KindUnique, func(ctx context.Context, q *sqlc.Queries, id int64) error {
		return q.InsertUniqueHoldingVariant(ctx, sqlc.InsertUniqueHoldingVariantParams{
			HoldingID: id, Label: db.NullString(in.Label),
		})
	})
}

func (p *Processor) createHolding(
	ctx context.Context,
	item domain.ItemID,
	location domain.LocationID,
	kind domain.Kind,
	variant func(context.Context, *sqlc.Queries, int64) error,
) (domain.HoldingID, error) {
	var id domain.HoldingID

	err := p.scope.Run(ctx, func(tx *sql.Tx) error {
		q := p.q.WithTx(tx)

		// The row must exist before an event can reference it: identity is
		// allocated by the storage layer and is an INPUT to the ledger, never an
		// output of it (schema section 3.12). Both land in one transaction, so
		// no observer sees an identifier without its creation event (H11).
		raw, err := q.InsertHolding(ctx, sqlc.InsertHoldingParams{
			ItemID:           int64(item),
			Kind:             string(kind),
			StowedLocationID: int64(location),
		})
		if err != nil {
			return fmt.Errorf("insert holding: %w", err)
		}
		if err := variant(ctx, q, raw); err != nil {
			return fmt.Errorf("insert %s variant: %w", kind, err)
		}
		if _, err := p.append(ctx, q, domain.HoldingCreated{
			EventBase:      domain.EventBase{OccurredAt: p.now()},
			Holding:        domain.HoldingID(raw),
			StowedLocation: location,
		}); err != nil {
			return err
		}
		id = domain.HoldingID(raw)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("ledger: create %s holding: %w", kind, err)
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// Locations
// ---------------------------------------------------------------------------

// CreateLocation brings a place into existence.
func (p *Processor) CreateLocation(ctx context.Context, name string, parent *domain.LocationID, description string) (domain.LocationID, error) {
	if name == "" {
		return 0, fmt.Errorf("%w: location name is required", ErrInvalidInput)
	}

	var id domain.LocationID
	err := p.scope.Run(ctx, func(tx *sql.Tx) error {
		q := p.q.WithTx(tx)
		raw, err := q.InsertLocation(ctx, sqlc.InsertLocationParams{
			ParentID:    nullLocation(parent),
			Name:        name,
			Description: db.NullString(description),
		})
		if err != nil {
			return fmt.Errorf("insert location: %w", err)
		}
		if _, err := p.append(ctx, q, domain.NodeCreated{
			EventBase: domain.EventBase{OccurredAt: p.now()},
			Location:  domain.LocationID(raw),
			Parent:    parent,
		}); err != nil {
			return err
		}
		id = domain.LocationID(raw)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("ledger: create location %q: %w", name, err)
	}
	return id, nil
}

// ReparentLocation moves a place, and with it everything inside it.
//
// This is the operation the Location ledger exists for: the contents' own
// stowed_location never changes, so without an event something moved and nothing
// recorded why.
func (p *Processor) ReparentLocation(ctx context.Context, id domain.LocationID, parent *domain.LocationID) error {
	state, err := p.q.GetLocationState(ctx, int64(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: location %d", ErrNotFound, id)
		}
		return fmt.Errorf("ledger: read location %d: %w", id, err)
	}
	if parent != nil {
		if *parent == id {
			return fmt.Errorf("%w: location %d cannot be its own parent", ErrCycle, id)
		}
		beneath, err := p.locationHasAncestor(ctx, *parent, id)
		if err != nil {
			return err
		}
		if beneath {
			return fmt.Errorf("%w: %d is beneath %d", ErrCycle, *parent, id)
		}
	}

	var from *domain.LocationID
	if state.ParentID.Valid {
		f := domain.LocationID(state.ParentID.Int64)
		from = &f
	}
	_, err = p.Apply(ctx, domain.NodeReparented{
		EventBase:  domain.EventBase{OccurredAt: p.now()},
		Location:   id,
		FromParent: from,
		ToParent:   parent,
	})
	return err
}

func (p *Processor) applyToLocation(ctx context.Context, q *sqlc.Queries, id domain.LocationID, e domain.Event) error {
	switch ev := e.(type) {
	case domain.NodeCreated:
		// The row was inserted in the same transaction; nothing further to do.
		return nil

	case domain.NodeReparented:
		return q.UpdateLocationParent(ctx, sqlc.UpdateLocationParentParams{
			ParentID: nullLocation(ev.ToParent), ID: int64(id),
		})

	case domain.NodeArchived:
		at := db.FormatTime(ev.OccurredAt)
		return q.SetLocationArchived(ctx, sqlc.SetLocationArchivedParams{
			ArchivedAt: sql.NullString{String: at, Valid: true}, ID: int64(id),
		})

	case domain.NodeRestored:
		return q.SetLocationArchived(ctx, sqlc.SetLocationArchivedParams{
			ArchivedAt: sql.NullString{}, ID: int64(id),
		})

	default:
		return fmt.Errorf("%w: %s is not a Location event", ErrWrongSubject, e.Type())
	}
}

func (p *Processor) locationHasAncestor(ctx context.Context, node, ancestor domain.LocationID) (bool, error) {
	ids, err := p.q.LocationAncestorIDs(ctx, int64(node))
	if err != nil {
		return false, fmt.Errorf("ledger: walk ancestors of location %d: %w", node, err)
	}
	if len(ids) >= ancestorScanLimit {
		return false, fmt.Errorf("ledger: ancestor chain of location %d exceeds %d nodes; tree is corrupt",
			node, ancestorScanLimit)
	}
	for _, id := range ids {
		if domain.LocationID(id) == ancestor {
			return true, nil
		}
	}
	return false, nil
}

// ---------------------------------------------------------------------------
// Items - the three attributes that type a Holding
// ---------------------------------------------------------------------------

func (p *Processor) applyToItem(ctx context.Context, q *sqlc.Queries, id domain.ItemID, e domain.Event) error {
	switch ev := e.(type) {
	case domain.ItemUnitChanged:
		if ev.ToUnit == nil {
			// The variant row is going away with a promotion; the kind change
			// carries that, and there is no column left to write.
			return nil
		}
		return q.UpdateBulkItemUnit(ctx, sqlc.UpdateBulkItemUnitParams{
			ContentUnit: string(*ev.ToUnit), ItemID: int64(id),
		})

	case domain.ItemPackageSizeChanged:
		return q.UpdateBulkItemPackageSize(ctx, sqlc.UpdateBulkItemPackageSizeParams{
			PackageSize: nullQuantity(ev.ToSize), ItemID: int64(id),
		})

	default:
		return fmt.Errorf("%w: %s is not an Item event", ErrWrongSubject, e.Type())
	}
}

// ---------------------------------------------------------------------------
// Integrity
// ---------------------------------------------------------------------------

// Orphans reports rows with no creation event: H11 for Holdings, L4 for
// Locations. Both are upheld transactionally, so a non-empty result means
// something wrote outside the ledger.
type Orphans struct {
	Holdings  []domain.HoldingID
	Locations []domain.LocationID
}

func (p *Processor) FindOrphans(ctx context.Context) (Orphans, error) {
	var out Orphans

	holdings, err := p.q.HoldingsWithoutCreationEvent(ctx)
	if err != nil {
		return out, fmt.Errorf("ledger: find holdings without creation events: %w", err)
	}
	for _, id := range holdings {
		out.Holdings = append(out.Holdings, domain.HoldingID(id))
	}

	locations, err := p.q.LocationsWithoutCreationEvent(ctx)
	if err != nil {
		return out, fmt.Errorf("ledger: find locations without creation events: %w", err)
	}
	for _, id := range locations {
		out.Locations = append(out.Locations, domain.LocationID(id))
	}
	return out, nil
}
