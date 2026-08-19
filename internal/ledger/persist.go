package ledger

import (
	"context"
	"database/sql"
	"fmt"

	"home-management-system/internal/db"
	"home-management-system/internal/db/sqlc"
	"home-management-system/internal/domain"
)

// writePayload inserts an event's payload into its shape table.
//
// This is one of the two places the 24-way dispatch lives, and it is why the
// payload lives in 13 explicit tables rather than one flat row or a serialized
// blob: a shape change forces an explicit remapping here, caught at compile
// time. The alternative trades that for a coherency contract between stored rows
// and domain logic that nothing checks and nothing surfaces when it breaks.
//
// Every case is reachable from domain.AllEventTypes, and
// TestEveryEventTypePersists walks that registry to prove it.
func writePayload(ctx context.Context, q *sqlc.Queries, id int64, e domain.Event) error {
	t := string(e.Type())

	switch ev := e.(type) {

	// --- Placement: creation is a placement from nowhere -------------------
	case domain.HoldingCreated:
		return q.InsertPlacementPayload(ctx, sqlc.InsertPlacementPayloadParams{
			EventID:        id,
			Type:           t,
			FromLocationID: sql.NullInt64{},
			ToLocationID:   int64(ev.StowedLocation),
		})
	case domain.Moved:
		return q.InsertPlacementPayload(ctx, sqlc.InsertPlacementPayloadParams{
			EventID: id, Type: t,
			FromLocationID: sql.NullInt64{Int64: int64(ev.From), Valid: true},
			ToLocationID:   int64(ev.To),
		})
	case domain.Rehomed:
		return q.InsertPlacementPayload(ctx, sqlc.InsertPlacementPayloadParams{
			EventID: id, Type: t,
			FromLocationID: sql.NullInt64{Int64: int64(ev.From), Valid: true},
			ToLocationID:   int64(ev.To),
		})

	// --- Acquisition -------------------------------------------------------
	case domain.Acquired:
		var price sql.NullInt64
		if ev.Price != nil {
			price = sql.NullInt64{Int64: *ev.Price, Valid: true}
		}
		return q.InsertAcquisitionPayload(ctx, sqlc.InsertAcquisitionPayloadParams{
			EventID: id, Type: t, Delta: ev.Delta.Milli(),
			Source: db.NullString(ev.Source), Price: price,
		})

	// --- Quantity ----------------------------------------------------------
	case domain.Consumed:
		return writeQuantity(ctx, q, id, t, ev.Delta, ev.Reason)
	case domain.Discarded:
		return writeQuantity(ctx, q, id, t, ev.Delta, ev.Reason)
	case domain.Opened:
		return writeQuantity(ctx, q, id, t, ev.Delta, ev.Reason)
	case domain.Split:
		return writeQuantity(ctx, q, id, t, ev.Delta, ev.Reason)
	case domain.Merged:
		return writeQuantity(ctx, q, id, t, ev.Delta, ev.Reason)
	case domain.Adjusted:
		return writeQuantity(ctx, q, id, t, ev.Delta, ev.Reason)

	// --- Custody -----------------------------------------------------------
	case domain.CheckedOut:
		var to sql.NullInt64
		if ev.DisplacedTo != nil {
			to = sql.NullInt64{Int64: int64(*ev.DisplacedTo), Valid: true}
		}
		return q.InsertCustodyPayload(ctx, sqlc.InsertCustodyPayloadParams{
			EventID: id, Type: t, ToCustody: string(domain.CustodyOut), DisplacedToID: to,
		})
	case domain.Returned:
		return writeCustody(ctx, q, id, t, domain.CustodyAtRest)
	case domain.MarkedLost:
		return writeCustody(ctx, q, id, t, domain.CustodyLost)
	case domain.Found:
		return writeCustody(ctx, q, id, t, domain.CustodyAtRest)

	// --- Observation and presence: Counted and Verified are separate because
	// --- one event type meaning two things is what made them separate.
	case domain.Counted:
		return q.InsertObservationPayload(ctx, sqlc.InsertObservationPayloadParams{
			EventID: id, Type: t, ObservedQuantity: ev.Observed.Milli(),
		})
	case domain.Verified:
		present := int64(0)
		if ev.Present {
			present = 1
		}
		return q.InsertPresencePayload(ctx, sqlc.InsertPresencePayloadParams{
			EventID: id, Type: t, Present: present,
		})

	// --- Terminal ----------------------------------------------------------
	case domain.Gone:
		return q.InsertTerminalPayload(ctx, sqlc.InsertTerminalPayloadParams{
			EventID: id, Type: t, Reason: db.NullString(ev.Reason),
		})

	// --- Location ----------------------------------------------------------
	case domain.NodeCreated:
		var parent sql.NullInt64
		if ev.Parent != nil {
			parent = sql.NullInt64{Int64: int64(*ev.Parent), Valid: true}
		}
		return q.InsertNodeCreatedPayload(ctx, sqlc.InsertNodeCreatedPayloadParams{
			EventID: id, Type: t, ParentID: parent,
		})
	case domain.NodeReparented:
		return q.InsertNodeReparentedPayload(ctx, sqlc.InsertNodeReparentedPayloadParams{
			EventID: id, Type: t,
			FromParentID: nullLocation(ev.FromParent),
			ToParentID:   nullLocation(ev.ToParent),
		})
	case domain.NodeArchived:
		return q.InsertNodeLifecyclePayload(ctx, sqlc.InsertNodeLifecyclePayloadParams{
			EventID: id, Type: t,
			Resolution: sql.NullString{String: string(ev.Resolution), Valid: true},
		})
	case domain.NodeRestored:
		return q.InsertNodeLifecyclePayload(ctx, sqlc.InsertNodeLifecyclePayloadParams{
			EventID: id, Type: t, Resolution: sql.NullString{},
		})

	// --- Item typing -------------------------------------------------------
	case domain.ItemUnitChanged:
		return q.InsertUnitChangedPayload(ctx, sqlc.InsertUnitChangedPayloadParams{
			EventID: id, Type: t,
			FromUnit: nullUnit(ev.FromUnit), ToUnit: nullUnit(ev.ToUnit),
		})
	case domain.ItemPackageSizeChanged:
		return q.InsertPackageSizeChangedPayload(ctx, sqlc.InsertPackageSizeChangedPayloadParams{
			EventID: id, Type: t,
			FromSize: nullQuantity(ev.FromSize), ToSize: nullQuantity(ev.ToSize),
		})

	default:
		return fmt.Errorf("%w: %T", ErrUnpersistable, e)
	}
}

func writeQuantity(ctx context.Context, q *sqlc.Queries, id int64, t string, delta domain.Quantity, reason string) error {
	return q.InsertQuantityPayload(ctx, sqlc.InsertQuantityPayloadParams{
		EventID: id, Type: t, Delta: delta.Milli(), Reason: db.NullString(reason),
	})
}

func writeCustody(ctx context.Context, q *sqlc.Queries, id int64, t string, to domain.Custody) error {
	return q.InsertCustodyPayload(ctx, sqlc.InsertCustodyPayloadParams{
		EventID: id, Type: t, ToCustody: string(to), DisplacedToID: sql.NullInt64{},
	})
}

func nullLocation(id *domain.LocationID) sql.NullInt64 {
	if id == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*id), Valid: true}
}

func nullUnit(u *domain.UnitCode) sql.NullString {
	if u == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(*u), Valid: true}
}

func nullQuantity(q *domain.Quantity) sql.NullInt64 {
	if q == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: q.Milli(), Valid: true}
}
