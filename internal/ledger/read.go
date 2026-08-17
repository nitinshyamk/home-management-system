package ledger

import (
	"context"
	"fmt"

	"home-management-system/internal/db"
	"home-management-system/internal/db/sqlc"
	"home-management-system/internal/domain"
)

// History returns a subject's events in sequence order.
//
// Reading costs a fixed number of queries regardless of how many events the
// subject has: one for the bases, plus one per payload shape that the subject
// kind can carry. Holding subjects use 7 shapes, Location 3, Item 3.
func (p *Processor) History(ctx context.Context, kind domain.SubjectKind, id int64) ([]domain.Event, error) {
	bases, err := p.q.EventsForSubject(ctx, sqlc.EventsForSubjectParams{
		SubjectKind: string(kind), SubjectID: id,
	})
	if err != nil {
		return nil, fmt.Errorf("ledger: read events for %s %d: %w", kind, id, err)
	}
	if len(bases) == 0 {
		return nil, nil
	}

	pl, err := loadPayloads(ctx, p.q, kind, id)
	if err != nil {
		return nil, err
	}

	out := make([]domain.Event, 0, len(bases))
	for _, row := range bases {
		e, err := hydrateEvent(row, pl)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// payloadSet holds every payload row for one subject, keyed by event id.
type payloadSet struct {
	quantity    map[int64]sqlc.QuantityPayloadsForSubjectRow
	acquisition map[int64]sqlc.AcquisitionPayloadsForSubjectRow
	placement   map[int64]sqlc.PlacementPayloadsForSubjectRow
	custody     map[int64]sqlc.CustodyPayloadsForSubjectRow
	observation map[int64]sqlc.ObservationPayloadsForSubjectRow
	presence    map[int64]sqlc.PresencePayloadsForSubjectRow
	terminal    map[int64]sqlc.TerminalPayloadsForSubjectRow

	nodeCreated    map[int64]sqlc.NodeCreatedPayloadsForSubjectRow
	nodeReparented map[int64]sqlc.NodeReparentedPayloadsForSubjectRow
	nodeLifecycle  map[int64]sqlc.NodeLifecyclePayloadsForSubjectRow

	kindChanged        map[int64]sqlc.KindChangedPayloadsForSubjectRow
	unitChanged        map[int64]sqlc.UnitChangedPayloadsForSubjectRow
	packageSizeChanged map[int64]sqlc.PackageSizeChangedPayloadsForSubjectRow
}

func loadPayloads(ctx context.Context, q *sqlc.Queries, kind domain.SubjectKind, id int64) (*payloadSet, error) {
	set := &payloadSet{}
	fail := func(shape string, err error) error {
		return fmt.Errorf("ledger: read %s payloads for %s %d: %w", shape, kind, id, err)
	}

	switch kind {
	case domain.SubjectHolding:
		rows, err := q.QuantityPayloadsForSubject(ctx, id)
		if err != nil {
			return nil, fail("quantity", err)
		}
		set.quantity = index(rows, func(r sqlc.QuantityPayloadsForSubjectRow) int64 { return r.EventID })

		acq, err := q.AcquisitionPayloadsForSubject(ctx, id)
		if err != nil {
			return nil, fail("acquisition", err)
		}
		set.acquisition = index(acq, func(r sqlc.AcquisitionPayloadsForSubjectRow) int64 { return r.EventID })

		plc, err := q.PlacementPayloadsForSubject(ctx, id)
		if err != nil {
			return nil, fail("placement", err)
		}
		set.placement = index(plc, func(r sqlc.PlacementPayloadsForSubjectRow) int64 { return r.EventID })

		cus, err := q.CustodyPayloadsForSubject(ctx, id)
		if err != nil {
			return nil, fail("custody", err)
		}
		set.custody = index(cus, func(r sqlc.CustodyPayloadsForSubjectRow) int64 { return r.EventID })

		obs, err := q.ObservationPayloadsForSubject(ctx, id)
		if err != nil {
			return nil, fail("observation", err)
		}
		set.observation = index(obs, func(r sqlc.ObservationPayloadsForSubjectRow) int64 { return r.EventID })

		pre, err := q.PresencePayloadsForSubject(ctx, id)
		if err != nil {
			return nil, fail("presence", err)
		}
		set.presence = index(pre, func(r sqlc.PresencePayloadsForSubjectRow) int64 { return r.EventID })

		term, err := q.TerminalPayloadsForSubject(ctx, id)
		if err != nil {
			return nil, fail("terminal", err)
		}
		set.terminal = index(term, func(r sqlc.TerminalPayloadsForSubjectRow) int64 { return r.EventID })

	case domain.SubjectLocation:
		nc, err := q.NodeCreatedPayloadsForSubject(ctx, id)
		if err != nil {
			return nil, fail("node_created", err)
		}
		set.nodeCreated = index(nc, func(r sqlc.NodeCreatedPayloadsForSubjectRow) int64 { return r.EventID })

		nr, err := q.NodeReparentedPayloadsForSubject(ctx, id)
		if err != nil {
			return nil, fail("node_reparented", err)
		}
		set.nodeReparented = index(nr, func(r sqlc.NodeReparentedPayloadsForSubjectRow) int64 { return r.EventID })

		nl, err := q.NodeLifecyclePayloadsForSubject(ctx, id)
		if err != nil {
			return nil, fail("node_lifecycle", err)
		}
		set.nodeLifecycle = index(nl, func(r sqlc.NodeLifecyclePayloadsForSubjectRow) int64 { return r.EventID })

	case domain.SubjectItem:
		kc, err := q.KindChangedPayloadsForSubject(ctx, id)
		if err != nil {
			return nil, fail("kind_changed", err)
		}
		set.kindChanged = index(kc, func(r sqlc.KindChangedPayloadsForSubjectRow) int64 { return r.EventID })

		uc, err := q.UnitChangedPayloadsForSubject(ctx, id)
		if err != nil {
			return nil, fail("unit_changed", err)
		}
		set.unitChanged = index(uc, func(r sqlc.UnitChangedPayloadsForSubjectRow) int64 { return r.EventID })

		pc, err := q.PackageSizeChangedPayloadsForSubject(ctx, id)
		if err != nil {
			return nil, fail("package_size_changed", err)
		}
		set.packageSizeChanged = index(pc, func(r sqlc.PackageSizeChangedPayloadsForSubjectRow) int64 { return r.EventID })
	}
	return set, nil
}

func index[T any](rows []T, key func(T) int64) map[int64]T {
	out := make(map[int64]T, len(rows))
	for _, r := range rows {
		out[key(r)] = r
	}
	return out
}

// hydrateEvent is the other half of the 24-way dispatch. A payload missing for
// an event that requires one is an error, not a zero value: E5 says the payload
// matches the type, and a silently empty one would replay as a no-op.
func hydrateEvent(row sqlc.Event, pl *payloadSet) (domain.Event, error) {
	occurred, err := db.ParseTime(row.OccurredAt)
	if err != nil {
		return nil, fmt.Errorf("ledger: event %d occurred_at: %w", row.ID, err)
	}
	recorded, err := db.ParseTime(row.RecordedAt)
	if err != nil {
		return nil, fmt.Errorf("ledger: event %d recorded_at: %w", row.ID, err)
	}
	base := domain.EventBase{
		ID:         domain.EventID(row.ID),
		OccurredAt: occurred,
		RecordedAt: recorded,
		Note:       db.StringOrEmpty(row.Note),
	}
	holding := domain.HoldingID(row.SubjectID)
	location := domain.LocationID(row.SubjectID)
	item := domain.ItemID(row.SubjectID)

	missing := func(shape string) error {
		return fmt.Errorf("%w: event %d (%s) has no %s payload", ErrMissingPayload, row.ID, row.Type, shape)
	}

	switch domain.EventType(row.Type) {

	case domain.TypeHoldingCreated:
		p, ok := pl.placement[row.ID]
		if !ok {
			return nil, missing("placement")
		}
		return domain.HoldingCreated{EventBase: base, Holding: holding,
			StowedLocation: domain.LocationID(p.ToLocationID)}, nil

	case domain.TypeMoved:
		p, ok := pl.placement[row.ID]
		if !ok {
			return nil, missing("placement")
		}
		return domain.Moved{EventBase: base, Holding: holding,
			From: domain.LocationID(p.FromLocationID.Int64), To: domain.LocationID(p.ToLocationID)}, nil

	case domain.TypeRehomed:
		p, ok := pl.placement[row.ID]
		if !ok {
			return nil, missing("placement")
		}
		return domain.Rehomed{EventBase: base, Holding: holding,
			From: domain.LocationID(p.FromLocationID.Int64), To: domain.LocationID(p.ToLocationID)}, nil

	case domain.TypeAcquired:
		p, ok := pl.acquisition[row.ID]
		if !ok {
			return nil, missing("acquisition")
		}
		e := domain.Acquired{EventBase: base, Holding: holding,
			Delta: domain.FromMilli(p.Delta), Source: db.StringOrEmpty(p.Source)}
		if p.Price.Valid {
			price := p.Price.Int64
			e.Price = &price
		}
		return e, nil

	case domain.TypeConsumed, domain.TypeDiscarded, domain.TypeOpened,
		domain.TypeSplit, domain.TypeMerged, domain.TypeAdjusted:
		p, ok := pl.quantity[row.ID]
		if !ok {
			return nil, missing("quantity")
		}
		delta, reason := domain.FromMilli(p.Delta), db.StringOrEmpty(p.Reason)
		switch domain.EventType(row.Type) {
		case domain.TypeConsumed:
			return domain.Consumed{EventBase: base, Holding: holding, Delta: delta, Reason: reason}, nil
		case domain.TypeDiscarded:
			return domain.Discarded{EventBase: base, Holding: holding, Delta: delta, Reason: reason}, nil
		case domain.TypeOpened:
			return domain.Opened{EventBase: base, Holding: holding, Delta: delta, Reason: reason}, nil
		case domain.TypeSplit:
			return domain.Split{EventBase: base, Holding: holding, Delta: delta, Reason: reason}, nil
		case domain.TypeMerged:
			return domain.Merged{EventBase: base, Holding: holding, Delta: delta, Reason: reason}, nil
		default:
			return domain.Adjusted{EventBase: base, Holding: holding, Delta: delta, Reason: reason}, nil
		}

	case domain.TypeCheckedOut:
		p, ok := pl.custody[row.ID]
		if !ok {
			return nil, missing("custody")
		}
		e := domain.CheckedOut{EventBase: base, Holding: holding}
		if p.DisplacedToID.Valid {
			to := domain.LocationID(p.DisplacedToID.Int64)
			e.DisplacedTo = &to
		}
		return e, nil

	case domain.TypeReturned:
		if _, ok := pl.custody[row.ID]; !ok {
			return nil, missing("custody")
		}
		return domain.Returned{EventBase: base, Holding: holding}, nil

	case domain.TypeMarkedLost:
		if _, ok := pl.custody[row.ID]; !ok {
			return nil, missing("custody")
		}
		return domain.MarkedLost{EventBase: base, Holding: holding}, nil

	case domain.TypeFound:
		if _, ok := pl.custody[row.ID]; !ok {
			return nil, missing("custody")
		}
		return domain.Found{EventBase: base, Holding: holding}, nil

	case domain.TypeCounted:
		p, ok := pl.observation[row.ID]
		if !ok {
			return nil, missing("observation")
		}
		return domain.Counted{EventBase: base, Holding: holding,
			Observed: domain.FromMilli(p.ObservedQuantity)}, nil

	case domain.TypeVerified:
		p, ok := pl.presence[row.ID]
		if !ok {
			return nil, missing("presence")
		}
		return domain.Verified{EventBase: base, Holding: holding, Present: p.Present != 0}, nil

	case domain.TypeGone:
		p, ok := pl.terminal[row.ID]
		if !ok {
			return nil, missing("terminal")
		}
		return domain.Gone{EventBase: base, Holding: holding, Reason: db.StringOrEmpty(p.Reason)}, nil

	case domain.TypeNodeCreated:
		p, ok := pl.nodeCreated[row.ID]
		if !ok {
			return nil, missing("node_created")
		}
		e := domain.NodeCreated{EventBase: base, Location: location}
		if p.ParentID.Valid {
			parent := domain.LocationID(p.ParentID.Int64)
			e.Parent = &parent
		}
		return e, nil

	case domain.TypeNodeReparented:
		p, ok := pl.nodeReparented[row.ID]
		if !ok {
			return nil, missing("node_reparented")
		}
		e := domain.NodeReparented{EventBase: base, Location: location}
		if p.FromParentID.Valid {
			from := domain.LocationID(p.FromParentID.Int64)
			e.FromParent = &from
		}
		if p.ToParentID.Valid {
			to := domain.LocationID(p.ToParentID.Int64)
			e.ToParent = &to
		}
		return e, nil

	case domain.TypeNodeArchived:
		p, ok := pl.nodeLifecycle[row.ID]
		if !ok {
			return nil, missing("node_lifecycle")
		}
		return domain.NodeArchived{EventBase: base, Location: location,
			Resolution: domain.Resolution(p.Resolution.String)}, nil

	case domain.TypeNodeRestored:
		if _, ok := pl.nodeLifecycle[row.ID]; !ok {
			return nil, missing("node_lifecycle")
		}
		return domain.NodeRestored{EventBase: base, Location: location}, nil

	case domain.TypeItemKindChanged:
		p, ok := pl.kindChanged[row.ID]
		if !ok {
			return nil, missing("kind_changed")
		}
		return domain.ItemKindChanged{EventBase: base, Item: item,
			FromKind: domain.Kind(p.FromKind), ToKind: domain.Kind(p.ToKind)}, nil

	case domain.TypeItemUnitChanged:
		p, ok := pl.unitChanged[row.ID]
		if !ok {
			return nil, missing("unit_changed")
		}
		e := domain.ItemUnitChanged{EventBase: base, Item: item}
		if p.FromUnit.Valid {
			from := domain.UnitCode(p.FromUnit.String)
			e.FromUnit = &from
		}
		if p.ToUnit.Valid {
			to := domain.UnitCode(p.ToUnit.String)
			e.ToUnit = &to
		}
		return e, nil

	case domain.TypeItemPackageSizeChanged:
		p, ok := pl.packageSizeChanged[row.ID]
		if !ok {
			return nil, missing("package_size_changed")
		}
		e := domain.ItemPackageSizeChanged{EventBase: base, Item: item}
		if p.FromSize.Valid {
			from := domain.FromMilli(p.FromSize.Int64)
			e.FromSize = &from
		}
		if p.ToSize.Valid {
			to := domain.FromMilli(p.ToSize.Int64)
			e.ToSize = &to
		}
		return e, nil

	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownEventType, row.Type)
	}
}
