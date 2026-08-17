package domain

import "time"

// Projection is the ledger-derived state of one Holding — and only that.
//
// It covers no immutable attribute and no directly-mutable one (K3). Replay
// reconstructs exactly this much, so claiming more would be claiming something
// replay cannot deliver: ExpiresOn is a correction to what you know about a bag
// of rice, not a thing that happened to it, and no event carries it.
//
// It is a sealed sum type for the same reason Holding is. A single struct with
// both Quantity and Custody would let a Bulk projection carry custody, and the
// whole point of the variant split is that such a state is unrepresentable
// rather than merely invalid.
type Projection interface {
	isProjection()
	Base() ProjectionBase
	Kind() Kind
}

// ProjectionBase is the ledger-derived state common to both kinds.
type ProjectionBase struct {
	// StowedLocation has no natural zero, which is why HoldingCreated exists.
	StowedLocation LocationID
	RetiredAt      *time.Time
}

// BulkProjection: quantity starts at zero, a real zero value needing no event.
type BulkProjection struct {
	ProjectionBase
	Quantity Quantity
}

// UniqueProjection: custody starts AtRest, likewise a real zero value.
type UniqueProjection struct {
	ProjectionBase
	Custody      Custody
	CustodySince *time.Time
	DisplacedTo  *LocationID
}

func (BulkProjection) isProjection()          {}
func (p BulkProjection) Base() ProjectionBase { return p.ProjectionBase }
func (BulkProjection) Kind() Kind             { return KindBulk }

func (UniqueProjection) isProjection()          {}
func (p UniqueProjection) Base() ProjectionBase { return p.ProjectionBase }
func (UniqueProjection) Kind() Kind             { return KindUnique }

// ZeroProjection is where replay begins.
//
// The kind comes from the Holding row, not from the ledger: it is immutable, so
// replay never reconstructs it. Everything else here is a genuine zero —
// quantity nought, custody AtRest, nothing retired — except StowedLocation,
// which has no meaningful empty value and is why HoldingCreated must be the
// first event of every Holding (H11).
func ZeroProjection(kind Kind) Projection {
	switch kind {
	case KindBulk:
		return BulkProjection{}
	default:
		return UniqueProjection{Custody: CustodyAtRest}
	}
}

// IsRetired reports whether the projection has reached its lifecycle terminal.
func IsRetired(p Projection) bool { return p.Base().RetiredAt != nil }
