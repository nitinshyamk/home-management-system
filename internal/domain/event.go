package domain

import "time"

// EventType is the ledger's discriminator. Values match the CHECK constraint in
// migration 0005 exactly.
type EventType string

const (
	// Holding events — 17.
	TypeHoldingCreated EventType = "HoldingCreated"
	TypeAcquired       EventType = "Acquired"
	TypeMoved          EventType = "Moved"
	TypeRehomed        EventType = "Rehomed"
	TypeConsumed       EventType = "Consumed"
	TypeDiscarded      EventType = "Discarded"
	TypeOpened         EventType = "Opened"
	TypeSplit          EventType = "Split"
	TypeMerged         EventType = "Merged"
	TypeAdjusted       EventType = "Adjusted"
	TypeCheckedOut     EventType = "CheckedOut"
	TypeReturned       EventType = "Returned"
	TypeMarkedLost     EventType = "MarkedLost"
	TypeFound          EventType = "Found"
	TypeCounted        EventType = "Counted"
	TypeVerified       EventType = "Verified"
	TypeGone           EventType = "Gone"

	// Location events — 4. Note the absence of a rename: renaming a place
	// changes no Holding's position, so it fails the §3.5 boundary.
	TypeNodeCreated    EventType = "NodeCreated"
	TypeNodeReparented EventType = "NodeReparented"
	TypeNodeArchived   EventType = "NodeArchived"
	TypeNodeRestored   EventType = "NodeRestored"

	// Item events — 3. Only the three attributes that *type* a Holding, never
	// the ones that merely label an Item.
	TypeItemKindChanged        EventType = "ItemKindChanged"
	TypeItemUnitChanged        EventType = "ItemUnitChanged"
	TypeItemPackageSizeChanged EventType = "ItemPackageSizeChanged"
)

// EventBase carries what every event has.
type EventBase struct {
	ID EventID
	// OccurredAt is when it happened in the world; RecordedAt when it was
	// entered. Retrospective entry is legal, and replay orders by ID — never by
	// OccurredAt (E3).
	OccurredAt time.Time
	RecordedAt time.Time
	Note       string
}

// Event is a sealed interface. Adding a variant means adding a case to Fold,
// which the generated registry and TestFoldHandlesEveryEventType enforce.
//
// Events partition by kind rather than being interpreted by kind: CheckedOut
// always means custody := Out, and could never have been emitted against a Bulk
// holding. Replay therefore never consults an Item to know what an event means.
type Event interface {
	isEvent()
	Base() EventBase
	Type() EventType
	Subject() (SubjectKind, int64)
}

// ---------------------------------------------------------------------------
// Holding events
// ---------------------------------------------------------------------------

// HoldingCreated establishes where a Holding was first put.
//
// It exists because verification demands a reconstruction independent of the
// state being verified (schema §3.11). StowedLocation could be recovered
// backwards — the first Moved's origin, or the current column if it never
// moved — but backward closure begins from the value under test, so corruption
// would propagate into the derived origin and back out, and H10 would pass on
// bad data.
type HoldingCreated struct {
	EventBase
	Holding        HoldingID
	StowedLocation LocationID
}

// Acquired credits stock. Delta is positive.
type Acquired struct {
	EventBase
	Holding HoldingID
	Delta   Quantity
	Source  string
	// Price is in minor currency units.
	Price *int64
}

// Moved changes where a Holding is kept.
type Moved struct {
	EventBase
	Holding HoldingID
	From    LocationID
	To      LocationID
}

// Rehomed changes where a Holding belongs without the object moving —
// "this lives in the garage now".
type Rehomed struct {
	EventBase
	Holding HoldingID
	From    LocationID
	To      LocationID
}

// Consumed is normal use. Delta is negative.
type Consumed struct {
	EventBase
	Holding HoldingID
	Delta   Quantity
	Reason  string
}

// Discarded removes stock for a stated reason. Delta is negative.
type Discarded struct {
	EventBase
	Holding HoldingID
	Delta   Quantity
	Reason  string
}

// Opened credits the content-basis Holding when a package is broken into.
// Delta is positive, and is a *resolved* amount: +2000 g, never "one package's
// worth" (E6), so a later package-size edit cannot rewrite what it meant.
type Opened struct {
	EventBase
	Holding HoldingID
	Delta   Quantity
	Reason  string
}

// Split debits the source Holding. Delta is negative.
type Split struct {
	EventBase
	Holding HoldingID
	Delta   Quantity
	Reason  string
}

// Merged folds one Holding's quantity into another's. Delta may be either sign:
// the source is debited and the destination credited.
type Merged struct {
	EventBase
	Holding HoldingID
	Delta   Quantity
	Reason  string
}

// Adjusted is the correction a Count implied, carrying the discrepancy
// explicitly. A count that disagrees with the ledger never overwrites it.
type Adjusted struct {
	EventBase
	Holding HoldingID
	Delta   Quantity
	Reason  string
}

// CheckedOut displaces a Unique holding with intent to return. DisplacedTo may
// be nil, which is what makes the holding derived-Missing.
type CheckedOut struct {
	EventBase
	Holding     HoldingID
	DisplacedTo *LocationID
}

// Returned restores a Unique holding to its stowed location.
type Returned struct {
	EventBase
	Holding HoldingID
}

// MarkedLost concludes that a Unique holding is not findable.
type MarkedLost struct {
	EventBase
	Holding HoldingID
}

// Found reverses MarkedLost, and may re-home.
type Found struct {
	EventBase
	Holding HoldingID
}

// Counted observes a Bulk quantity. It never overwrites: a discrepancy produces
// a separate Adjusted.
type Counted struct {
	EventBase
	Holding  HoldingID
	Observed Quantity
}

// Verified observes a Unique holding's presence. This and Counted were one event
// type until it became clear it meant two different things — an observed
// quantity on Bulk, an observed presence on Unique.
type Verified struct {
	EventBase
	Holding HoldingID
	Present bool
}

// Gone is the single lifecycle terminal. The record persists in history.
type Gone struct {
	EventBase
	Holding HoldingID
	Reason  string
}

// ---------------------------------------------------------------------------
// Location events
// ---------------------------------------------------------------------------

// NodeCreated records a place coming into existence. Parent is nil for a root.
// It carries no name: names are labels and carry current state only.
type NodeCreated struct {
	EventBase
	Location LocationID
	Parent   *LocationID
}

// NodeReparented is what makes reorganization accountable. Re-parenting a
// container moves its contents with no Holding event of their own, so without
// this something moved and nothing recorded why.
type NodeReparented struct {
	EventBase
	Location   LocationID
	FromParent *LocationID
	ToParent   *LocationID
}

// NodeArchived removes a place, disposing of its contents per Resolution.
type NodeArchived struct {
	EventBase
	Location   LocationID
	Resolution Resolution
}

// NodeRestored reverses an archival.
type NodeRestored struct {
	EventBase
	Location LocationID
}

// ---------------------------------------------------------------------------
// Item events — structural attributes only
// ---------------------------------------------------------------------------

// ItemKindChanged records a Promote or Demote.
type ItemKindChanged struct {
	EventBase
	Item     ItemID
	FromKind Kind
	ToKind   Kind
}

// ItemUnitChanged records a change of measurement.
//
// Both sides are nullable because a Unique item has no unit: promotion records
// {g → nil}. Without it the discarded definition is unrecoverable, and the claim
// that an Item's history closes backwards becomes false.
type ItemUnitChanged struct {
	EventBase
	Item     ItemID
	FromUnit *UnitCode
	ToUnit   *UnitCode
}

// ItemPackageSizeChanged records a change of packaging, nullable for the same
// reason as ItemUnitChanged.
type ItemPackageSizeChanged struct {
	EventBase
	Item     ItemID
	FromSize *Quantity
	ToSize   *Quantity
}

// ---------------------------------------------------------------------------
// Interface implementations
// ---------------------------------------------------------------------------

func (HoldingCreated) isEvent() {}
func (Acquired) isEvent()       {}
func (Moved) isEvent()          {}
func (Rehomed) isEvent()        {}
func (Consumed) isEvent()       {}
func (Discarded) isEvent()      {}
func (Opened) isEvent()         {}
func (Split) isEvent()          {}
func (Merged) isEvent()         {}
func (Adjusted) isEvent()       {}
func (CheckedOut) isEvent()     {}
func (Returned) isEvent()       {}
func (MarkedLost) isEvent()     {}
func (Found) isEvent()          {}
func (Counted) isEvent()        {}
func (Verified) isEvent()       {}
func (Gone) isEvent()           {}

func (NodeCreated) isEvent()    {}
func (NodeReparented) isEvent() {}
func (NodeArchived) isEvent()   {}
func (NodeRestored) isEvent()   {}

func (ItemKindChanged) isEvent()        {}
func (ItemUnitChanged) isEvent()        {}
func (ItemPackageSizeChanged) isEvent() {}

func (e HoldingCreated) Base() EventBase { return e.EventBase }
func (e Acquired) Base() EventBase       { return e.EventBase }
func (e Moved) Base() EventBase          { return e.EventBase }
func (e Rehomed) Base() EventBase        { return e.EventBase }
func (e Consumed) Base() EventBase       { return e.EventBase }
func (e Discarded) Base() EventBase      { return e.EventBase }
func (e Opened) Base() EventBase         { return e.EventBase }
func (e Split) Base() EventBase          { return e.EventBase }
func (e Merged) Base() EventBase         { return e.EventBase }
func (e Adjusted) Base() EventBase       { return e.EventBase }
func (e CheckedOut) Base() EventBase     { return e.EventBase }
func (e Returned) Base() EventBase       { return e.EventBase }
func (e MarkedLost) Base() EventBase     { return e.EventBase }
func (e Found) Base() EventBase          { return e.EventBase }
func (e Counted) Base() EventBase        { return e.EventBase }
func (e Verified) Base() EventBase       { return e.EventBase }
func (e Gone) Base() EventBase           { return e.EventBase }

func (e NodeCreated) Base() EventBase    { return e.EventBase }
func (e NodeReparented) Base() EventBase { return e.EventBase }
func (e NodeArchived) Base() EventBase   { return e.EventBase }
func (e NodeRestored) Base() EventBase   { return e.EventBase }

func (e ItemKindChanged) Base() EventBase        { return e.EventBase }
func (e ItemUnitChanged) Base() EventBase        { return e.EventBase }
func (e ItemPackageSizeChanged) Base() EventBase { return e.EventBase }

func (HoldingCreated) Type() EventType { return TypeHoldingCreated }
func (Acquired) Type() EventType       { return TypeAcquired }
func (Moved) Type() EventType          { return TypeMoved }
func (Rehomed) Type() EventType        { return TypeRehomed }
func (Consumed) Type() EventType       { return TypeConsumed }
func (Discarded) Type() EventType      { return TypeDiscarded }
func (Opened) Type() EventType         { return TypeOpened }
func (Split) Type() EventType          { return TypeSplit }
func (Merged) Type() EventType         { return TypeMerged }
func (Adjusted) Type() EventType       { return TypeAdjusted }
func (CheckedOut) Type() EventType     { return TypeCheckedOut }
func (Returned) Type() EventType       { return TypeReturned }
func (MarkedLost) Type() EventType     { return TypeMarkedLost }
func (Found) Type() EventType          { return TypeFound }
func (Counted) Type() EventType        { return TypeCounted }
func (Verified) Type() EventType       { return TypeVerified }
func (Gone) Type() EventType           { return TypeGone }

func (NodeCreated) Type() EventType    { return TypeNodeCreated }
func (NodeReparented) Type() EventType { return TypeNodeReparented }
func (NodeArchived) Type() EventType   { return TypeNodeArchived }
func (NodeRestored) Type() EventType   { return TypeNodeRestored }

func (ItemKindChanged) Type() EventType        { return TypeItemKindChanged }
func (ItemUnitChanged) Type() EventType        { return TypeItemUnitChanged }
func (ItemPackageSizeChanged) Type() EventType { return TypeItemPackageSizeChanged }

func (e HoldingCreated) Subject() (SubjectKind, int64) { return SubjectHolding, int64(e.Holding) }
func (e Acquired) Subject() (SubjectKind, int64)       { return SubjectHolding, int64(e.Holding) }
func (e Moved) Subject() (SubjectKind, int64)          { return SubjectHolding, int64(e.Holding) }
func (e Rehomed) Subject() (SubjectKind, int64)        { return SubjectHolding, int64(e.Holding) }
func (e Consumed) Subject() (SubjectKind, int64)       { return SubjectHolding, int64(e.Holding) }
func (e Discarded) Subject() (SubjectKind, int64)      { return SubjectHolding, int64(e.Holding) }
func (e Opened) Subject() (SubjectKind, int64)         { return SubjectHolding, int64(e.Holding) }
func (e Split) Subject() (SubjectKind, int64)          { return SubjectHolding, int64(e.Holding) }
func (e Merged) Subject() (SubjectKind, int64)         { return SubjectHolding, int64(e.Holding) }
func (e Adjusted) Subject() (SubjectKind, int64)       { return SubjectHolding, int64(e.Holding) }
func (e CheckedOut) Subject() (SubjectKind, int64)     { return SubjectHolding, int64(e.Holding) }
func (e Returned) Subject() (SubjectKind, int64)       { return SubjectHolding, int64(e.Holding) }
func (e MarkedLost) Subject() (SubjectKind, int64)     { return SubjectHolding, int64(e.Holding) }
func (e Found) Subject() (SubjectKind, int64)          { return SubjectHolding, int64(e.Holding) }
func (e Counted) Subject() (SubjectKind, int64)        { return SubjectHolding, int64(e.Holding) }
func (e Verified) Subject() (SubjectKind, int64)       { return SubjectHolding, int64(e.Holding) }
func (e Gone) Subject() (SubjectKind, int64)           { return SubjectHolding, int64(e.Holding) }

func (e NodeCreated) Subject() (SubjectKind, int64)    { return SubjectLocation, int64(e.Location) }
func (e NodeReparented) Subject() (SubjectKind, int64) { return SubjectLocation, int64(e.Location) }
func (e NodeArchived) Subject() (SubjectKind, int64)   { return SubjectLocation, int64(e.Location) }
func (e NodeRestored) Subject() (SubjectKind, int64)   { return SubjectLocation, int64(e.Location) }

func (e ItemKindChanged) Subject() (SubjectKind, int64) { return SubjectItem, int64(e.Item) }
func (e ItemUnitChanged) Subject() (SubjectKind, int64) { return SubjectItem, int64(e.Item) }
func (e ItemPackageSizeChanged) Subject() (SubjectKind, int64) {
	return SubjectItem, int64(e.Item)
}

//go:generate go run ../../tools/eventgen -dir . -out registry_gen.go
