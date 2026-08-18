package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"home-management-system/internal/db/sqlc"
	"home-management-system/internal/domain"
)

// ErrItemHasHoldings reports a kind change against an Item that has ever been
// stocked.
//
// This is a limit of the physical schema, not a policy: holdings carries
// FOREIGN KEY (item_id, kind) REFERENCES items(id, kind), and a holding row
// keeps the kind it was created with. Changing the Item's kind therefore
// breaks the reference from every holding of the old kind, INCLUDING retired
// ones, because retirement sets retired_at and leaves the row in place.
//
// Conceptual schema section 3.12 says "Promote retires the Bulk Holding and
// creates N Unique ones". That is not reachable through this constraint:
// retiring does not remove the row, and removing it would break E4, which is
// what makes the holding's events interpretable at all. Recorded as a finding
// rather than worked around.
var ErrItemHasHoldings = errors.New("ledger: item has holdings, so its kind cannot change")

// PlanPromote turns a measured Item into an individually-tracked one.
//
// Three events, not one. The kind change alone would leave the discarded
// definition -- the unit, the package size -- unrecoverable, and with it the
// claim that an Item's history closes backwards. So the unit and package size
// are recorded going to nil, which is why both sides of those events are
// nullable.
//
// The kind change goes LAST. Its handler deletes the bulk_items row, and the
// other two events write to that row, so they must run while it still exists.
func (p *Processor) PlanPromote(ctx context.Context, id domain.ItemID) ([]domain.Event, error) {
	state, err := p.typingStateForKindChange(ctx, id)
	if err != nil {
		return nil, err
	}
	if domain.Kind(state.Kind) != domain.KindBulk {
		return nil, fmt.Errorf("%w: item %d is already Unique", ErrInvalidInput, id)
	}
	unit := domain.UnitCode(state.ContentUnit.String)

	events := []domain.Event{
		domain.ItemUnitChanged{
			EventBase: domain.EventBase{OccurredAt: p.now()},
			Item:      id, FromUnit: &unit, ToUnit: nil,
		},
	}
	if state.PackageSize.Valid {
		size := domain.FromMilli(state.PackageSize.Int64)
		events = append(events, domain.ItemPackageSizeChanged{
			EventBase: domain.EventBase{OccurredAt: p.now()},
			Item:      id, FromSize: &size, ToSize: nil,
		})
	}
	return append(events, domain.ItemKindChanged{
		EventBase: domain.EventBase{OccurredAt: p.now()},
		Item:      id, FromKind: domain.KindBulk, ToKind: domain.KindUnique,
	}), nil
}

// PlanDemote turns an individually-tracked Item into a measured one.
//
// The content unit is required and cannot be derived: a Unique Item has no
// measurement, so there is nothing to recover it from. The kind change goes
// FIRST here, for the mirror of the reason it goes last in a promotion -- its
// handler creates the bulk_items row, and the other two events need it to
// exist. That handler reads the unit from the ItemUnitChanged in this same
// batch, so the events cannot be separated.
func (p *Processor) PlanDemote(
	ctx context.Context,
	id domain.ItemID,
	unit domain.UnitCode,
	packageSize *domain.Quantity,
) ([]domain.Event, error) {
	state, err := p.typingStateForKindChange(ctx, id)
	if err != nil {
		return nil, err
	}
	if domain.Kind(state.Kind) != domain.KindUnique {
		return nil, fmt.Errorf("%w: item %d is already Bulk", ErrInvalidInput, id)
	}
	if unit == "" {
		return nil, fmt.Errorf("%w: demoting item %d needs a content unit", ErrInvalidInput, id)
	}
	if packageSize != nil && !packageSize.IsPositive() {
		return nil, fmt.Errorf("%w: package size must be positive, got %s", ErrInvalidInput, packageSize)
	}

	events := []domain.Event{
		domain.ItemKindChanged{
			EventBase: domain.EventBase{OccurredAt: p.now()},
			Item:      id, FromKind: domain.KindUnique, ToKind: domain.KindBulk,
		},
		domain.ItemUnitChanged{
			EventBase: domain.EventBase{OccurredAt: p.now()},
			Item:      id, FromUnit: nil, ToUnit: &unit,
		},
	}
	if packageSize != nil {
		events = append(events, domain.ItemPackageSizeChanged{
			EventBase: domain.EventBase{OccurredAt: p.now()},
			Item:      id, FromSize: nil, ToSize: packageSize,
		})
	}
	return events, nil
}

// typingStateForKindChange loads the three typing attributes and refuses the
// change if the schema cannot carry it.
func (p *Processor) typingStateForKindChange(ctx context.Context, id domain.ItemID) (sqlc.GetItemTypingStateRow, error) {
	state, err := p.q.GetItemTypingState(ctx, int64(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return state, fmt.Errorf("%w: item %d", ErrNotFound, id)
		}
		return state, fmt.Errorf("ledger: read item %d: %w", id, err)
	}
	holdings, err := p.q.CountAnyHoldingsOfItem(ctx, int64(id))
	if err != nil {
		return state, fmt.Errorf("ledger: count holdings of item %d: %w", id, err)
	}
	if holdings > 0 {
		return state, fmt.Errorf("%w: item %d has %d holdings (retired ones count, "+
			"because the row keeps the kind it was created with)", ErrItemHasHoldings, id, holdings)
	}
	return state, nil
}
