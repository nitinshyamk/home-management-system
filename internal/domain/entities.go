package domain

import "time"

// ---------------------------------------------------------------------------
// Trees
// ---------------------------------------------------------------------------

// Category classifies. It is a pure descriptive overlay: no Holding attribute
// references it, and it has no ledger at all (schema §3.1).
type Category struct {
	ID          CategoryID
	Parent      *CategoryID
	Name        string
	Description string
	CreatedAt   time.Time
	ArchivedAt  *time.Time
}

func (c Category) IsRoot() bool     { return c.Parent == nil }
func (c Category) IsArchived() bool { return c.ArchivedAt != nil }

// Location is a physical place. Unlike Category it models reality that Holdings
// reference and the ledger tracks — the ledger boundary falls between them.
type Location struct {
	ID          LocationID
	Parent      *LocationID
	Name        string
	Description string
	CreatedAt   time.Time
	ArchivedAt  *time.Time
}

func (l Location) IsRoot() bool     { return l.Parent == nil }
func (l Location) IsArchived() bool { return l.ArchivedAt != nil }

// ---------------------------------------------------------------------------
// Item: base plus two variants
// ---------------------------------------------------------------------------

// ItemBase carries what both variants have.
//
// Note the absence of a Kind field. The Go type is the discriminator, so an
// item that claims one kind while carrying the other's attributes is
// unrepresentable rather than merely invalid.
type ItemBase struct {
	ID                   ItemID
	Name                 string
	Category             CategoryID
	Notes                string
	PlacementConfirmedAt *time.Time
	CreatedAt            time.Time
	ArchivedAt           *time.Time
}

// Item is a sealed interface: only this package can implement it.
type Item interface {
	isItem()
	Base() ItemBase
	Kind() Kind
}

// UniqueItem has no attributes beyond the base.
//
// That is a finding, not an oversight: everything that would have lived here —
// Condition, warranty — was cut from the domain model. Unique is the degenerate
// variant, not a peer of Bulk, and this is where any durable-specific attribute
// lands the moment one returns.
type UniqueItem struct {
	ItemBase
}

// BulkItem measures its contents.
type BulkItem struct {
	ItemBase
	ContentUnit UnitCode
	// PackageSize is the content-unit amount in one package. Nil means the item
	// has no package concept at all.
	PackageSize *Quantity
}

func (UniqueItem) isItem()          {}
func (i UniqueItem) Base() ItemBase { return i.ItemBase }
func (UniqueItem) Kind() Kind       { return KindUnique }

func (BulkItem) isItem()          {}
func (i BulkItem) Base() ItemBase { return i.ItemBase }
func (BulkItem) Kind() Kind       { return KindBulk }

// HasPackage reports whether the item is measured in packages as well as
// content units — which is what makes a Package-basis holding legal (H7).
func (i BulkItem) HasPackage() bool { return i.PackageSize != nil }

// ---------------------------------------------------------------------------
// Holding: base plus two variants
// ---------------------------------------------------------------------------

// HoldingBase carries what both variants have.
//
// StowedLocation, not Location: for Unique it is where the thing belongs, for
// Bulk where the stuff is. "Where it is kept" is one concept true of both, and
// it makes DisplacedTo legible as the deviation from stowed — representable
// only on the variant that can deviate.
type HoldingBase struct {
	ID             HoldingID
	Item           ItemID
	StowedLocation LocationID
	ExpiresOn      *time.Time
	SnoozedUntil   *time.Time
	RetiredAt      *time.Time
	CreatedAt      time.Time
}

// Holding is a sealed interface.
type Holding interface {
	isHolding()
	Base() HoldingBase
	Kind() Kind
}

// UniqueHolding is one thing. It has no quantity and no unit basis, so H1 is not
// an invariant to enforce but a shape that cannot express the violation.
type UniqueHolding struct {
	HoldingBase
	Label        string
	Custody      Custody
	CustodySince *time.Time
	DisplacedTo  *LocationID
}

// BulkHolding is an amount. It has no custody and no label: taking twenty zip
// ties to the garage is a Move, not a checkout.
type BulkHolding struct {
	HoldingBase
	Quantity Quantity
	// UnitBasis is immutable — no event changes it, because Opened creates a new
	// Holding with a different basis rather than converting one.
	UnitBasis UnitBasis
}

func (UniqueHolding) isHolding()          {}
func (h UniqueHolding) Base() HoldingBase { return h.HoldingBase }
func (UniqueHolding) Kind() Kind          { return KindUnique }

func (BulkHolding) isHolding()          {}
func (h BulkHolding) Base() HoldingBase { return h.HoldingBase }
func (BulkHolding) Kind() Kind          { return KindBulk }

// IsMissing is derived, never stored: Out with no known displacement. It is
// transient ignorance, distinct from Lost, which is a conclusion.
func (h UniqueHolding) IsMissing() bool {
	return h.Custody == CustodyOut && h.DisplacedTo == nil
}

// EffectiveLocation is where the thing actually is, as opposed to where it is
// kept.
func (h UniqueHolding) EffectiveLocation() LocationID {
	if h.DisplacedTo != nil {
		return *h.DisplacedTo
	}
	return h.StowedLocation
}

// IsDepleted is the Bulk end of life. It has no Unique analogue — a cable is
// Gone, never depleted — just as IsMissing has no Bulk analogue, since an
// unaccounted-for quantity is a Count discrepancy, not a lost object.
func (h BulkHolding) IsDepleted() bool { return h.Quantity.IsZero() }

// IsActive reports whether the holding still participates in rollups.
func IsActive(h Holding) bool { return h.Base().RetiredAt == nil }
