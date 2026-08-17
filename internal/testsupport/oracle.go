package testsupport

import (
	"fmt"
	"time"

	"home-management-system/internal/domain"
)

// Oracle is a deliberately naive model of Holding state: a flat record with
// optional fields, no variant tables, no ledger, no transactions.
//
// It is the third leg of the three-way property test. The point is that it is an
// INDEPENDENT implementation — written the obvious way rather than the careful
// way — so agreement between it, the real system, and ledger replay means the
// variant machinery and the ledger did not change the semantics. It represents
// exactly the shape the domain model rejects for stored state, which is what
// makes it a useful control.
//
// It applies events without re-deriving legality: the real system decides what
// is legal, and only accepted operations reach the Oracle.
type Oracle struct {
	holdings map[domain.HoldingID]*OracleHolding
}

// OracleHolding is the flat record.
type OracleHolding struct {
	Kind           domain.Kind
	StowedLocation domain.LocationID
	RetiredAt      *time.Time
	QuantityMilli  int64
	Custody        domain.Custody
	CustodySince   *time.Time
	DisplacedTo    *domain.LocationID
}

func NewOracle() *Oracle {
	return &Oracle{holdings: map[domain.HoldingID]*OracleHolding{}}
}

// Create records a Holding coming into existence, matching what the ledger's
// creation path does.
func (o *Oracle) Create(id domain.HoldingID, kind domain.Kind, at domain.LocationID) {
	o.holdings[id] = &OracleHolding{
		Kind:           kind,
		StowedLocation: at,
		Custody:        domain.CustodyAtRest,
	}
}

// Holding returns the modelled state.
func (o *Oracle) Holding(id domain.HoldingID) (*OracleHolding, bool) {
	h, ok := o.holdings[id]
	return h, ok
}

// IDs returns every modelled Holding, for iteration.
func (o *Oracle) IDs() []domain.HoldingID {
	out := make([]domain.HoldingID, 0, len(o.holdings))
	for id := range o.holdings {
		out = append(out, id)
	}
	return out
}

// Apply updates the model. Called only for events the real system accepted.
func (o *Oracle) Apply(e domain.Event) error {
	kind, subject := e.Subject()
	if kind != domain.SubjectHolding {
		return nil // the Oracle models Holdings only
	}
	h, ok := o.holdings[domain.HoldingID(subject)]
	if !ok {
		return fmt.Errorf("oracle: unknown holding %d", subject)
	}

	switch ev := e.(type) {
	case domain.HoldingCreated:
		h.StowedLocation = ev.StowedLocation

	case domain.Moved:
		h.StowedLocation = ev.To
	case domain.Rehomed:
		h.StowedLocation = ev.To

	case domain.Acquired:
		h.QuantityMilli += ev.Delta.Milli()
	case domain.Consumed:
		h.QuantityMilli += ev.Delta.Milli()
	case domain.Discarded:
		h.QuantityMilli += ev.Delta.Milli()
	case domain.Opened:
		h.QuantityMilli += ev.Delta.Milli()
	case domain.Split:
		h.QuantityMilli += ev.Delta.Milli()
	case domain.Merged:
		h.QuantityMilli += ev.Delta.Milli()
	case domain.Adjusted:
		h.QuantityMilli += ev.Delta.Milli()

	case domain.Counted:
		// Deliberately nothing. An observation records what was seen; the
		// correction is a separate Adjusted.
	case domain.Verified:
		// Likewise.

	case domain.CheckedOut:
		at := ev.OccurredAt
		h.Custody, h.CustodySince, h.DisplacedTo = domain.CustodyOut, &at, ev.DisplacedTo
	case domain.Returned:
		h.Custody, h.CustodySince, h.DisplacedTo = domain.CustodyAtRest, nil, nil
	case domain.MarkedLost:
		at := ev.OccurredAt
		h.Custody, h.CustodySince, h.DisplacedTo = domain.CustodyLost, &at, nil
	case domain.Found:
		h.Custody, h.CustodySince, h.DisplacedTo = domain.CustodyAtRest, nil, nil

	case domain.Gone:
		at := ev.OccurredAt
		h.RetiredAt = &at

	default:
		return fmt.Errorf("oracle: no case for %T", e)
	}
	return nil
}

// Diff compares the model against a real projection, returning human-readable
// differences.
func (h *OracleHolding) Diff(p domain.Projection) []string {
	var out []string
	base := p.Base()

	if h.StowedLocation != base.StowedLocation {
		out = append(out, fmt.Sprintf("stowed_location: oracle %d, system %d",
			h.StowedLocation, base.StowedLocation))
	}
	if (h.RetiredAt == nil) != (base.RetiredAt == nil) {
		out = append(out, fmt.Sprintf("retired_at: oracle %v, system %v",
			h.RetiredAt != nil, base.RetiredAt != nil))
	}

	switch typed := p.(type) {
	case domain.BulkProjection:
		if h.Kind != domain.KindBulk {
			out = append(out, fmt.Sprintf("kind: oracle %s, system Bulk", h.Kind))
			break
		}
		if h.QuantityMilli != typed.Quantity.Milli() {
			out = append(out, fmt.Sprintf("quantity: oracle %d, system %d",
				h.QuantityMilli, typed.Quantity.Milli()))
		}
	case domain.UniqueProjection:
		if h.Kind != domain.KindUnique {
			out = append(out, fmt.Sprintf("kind: oracle %s, system Unique", h.Kind))
			break
		}
		if h.Custody != typed.Custody {
			out = append(out, fmt.Sprintf("custody: oracle %s, system %s", h.Custody, typed.Custody))
		}
		if (h.CustodySince == nil) != (typed.CustodySince == nil) {
			out = append(out, fmt.Sprintf("custody_since set: oracle %v, system %v",
				h.CustodySince != nil, typed.CustodySince != nil))
		}
		oracleTo, systemTo := "unset", "unset"
		if h.DisplacedTo != nil {
			oracleTo = fmt.Sprint(*h.DisplacedTo)
		}
		if typed.DisplacedTo != nil {
			systemTo = fmt.Sprint(*typed.DisplacedTo)
		}
		if oracleTo != systemTo {
			out = append(out, fmt.Sprintf("displaced_to: oracle %s, system %s", oracleTo, systemTo))
		}
	}
	return out
}
