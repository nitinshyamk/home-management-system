// Package domain holds the pure model: identifiers, values, entities, events,
// and the fold that gives events their meaning. It performs no I/O and imports
// nothing outside the standard library — scripts/archlint.sh enforces that.
//
// A dependency here would mean I/O, and the fold would stop being trivially
// testable and trivially replayable. That property is what makes H10 meaningful
// (plan §2): Apply and Replay call the same Fold, so ledger divergence cannot
// come from two implementations disagreeing.
package domain

// Identifiers are distinct types so that passing a LocationID where a HoldingID
// belongs is a compile error rather than a silent bug. Identity is permanent and
// never reused (schema §3.10): the ledger holds references indefinitely, so a
// reused identifier would rewrite history.
type (
	CategoryID int64
	LocationID int64
	ItemID     int64
	HoldingID  int64

	// EventID is also the sequence. AUTOINCREMENT is monotonic and never reuses
	// a value, which satisfies E2 without a second column.
	EventID int64
)

// Kind discriminates the two item and holding variants.
//
// Note that Kind is NOT stored on ItemBase or HoldingBase. The Go type is the
// discriminator, so a BulkItem claiming to be Unique is not merely rejected —
// it is unrepresentable.
type Kind string

const (
	KindUnique Kind = "Unique"
	KindBulk   Kind = "Bulk"
)

func (k Kind) Valid() bool { return k == KindUnique || k == KindBulk }

// SubjectKind names what an event is about. The ledger records existence,
// containment, content, and typing — never labels (schema §3.5) — which is why
// Category is absent: it has no such relationship to any Holding.
type SubjectKind string

const (
	SubjectHolding  SubjectKind = "Holding"
	SubjectLocation SubjectKind = "Location"
	SubjectItem     SubjectKind = "Item"
)

// UnitBasis records which unit a BulkHolding's quantity counts in: the item's
// content unit, or whole packages. This is the structural form of "sealed vs
// opened" (schema §3.8) — what makes that state derivable rather than stored.
//
// It is immutable. No event changes it, because Opened creates a *new* Holding
// with a different basis rather than converting one.
type UnitBasis string

const (
	BasisContent UnitBasis = "Content"
	BasisPackage UnitBasis = "Package"
)

func (b UnitBasis) Valid() bool { return b == BasisContent || b == BasisPackage }

// Custody applies only to Unique holdings.
//
// Lost is a custody state rather than a lifecycle terminal: it has a follow-up
// path (Found), and a state with a follow-up path is by definition not terminal.
// That leaves Gone as the single lifecycle terminal.
//
// Missing is *derived*, not stored: Out with no known displacement. It is
// transient ignorance; Lost is a conclusion.
type Custody string

const (
	CustodyAtRest Custody = "AtRest"
	CustodyOut    Custody = "Out"
	CustodyLost   Custody = "Lost"
)

func (c Custody) Valid() bool {
	return c == CustodyAtRest || c == CustodyOut || c == CustodyLost
}

// Resolution is how a tree archival disposes of the node's children and
// contents. Lift is the default: it matches how reorganization actually
// proceeds.
type Resolution string

const (
	ResolutionLift  Resolution = "Lift"
	ResolutionMove  Resolution = "Move"
	ResolutionBlock Resolution = "Block"
)

func (r Resolution) Valid() bool {
	return r == ResolutionLift || r == ResolutionMove || r == ResolutionBlock
}
