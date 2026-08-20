// Package command is the contract.
//
// The TUI is a fast way to author one, a CSV is a batch of them, an agent emits
// them directly. All three land in the same resolver, the same review, and the
// same transaction -- which is what makes the interactive and bulk flows one
// system rather than two that resemble each other.
//
// The package holds two shapes and one boundary between them. RawCommand is
// untrusted text, as it arrives from a CSV row, a JSON Lines record, or a typed
// `:` line. Command is typed, and its variants mirror the operation requests.
// Bind is the one place looseness becomes typed -- the role hydrateEvent plays
// for storage.
//
// It deliberately does not import internal/ops. Binding resolves names; it does
// not decide what will happen. The consequence is that the Command variants
// mirror the ops requests rather than embedding them, and the conversion lives
// in internal/app, which is the layer that legitimately knows both. That is a
// real cost in duplicated fields, paid so that "a command is an inert
// description" is a property of the package graph and not a convention.
package command

import (
	"time"

	"home-management-system/internal/domain"
)

// Op is the word a person or an agent writes. It is the vocabulary, so it is
// spelled the way a receipt would be rather than the way the operation is
// named internally -- "acquire" rather than "receive", "lost" rather than
// "mark lost".
type Op string

// Command is a sealed union. Every reference in one is an IDENTIFIER: there is
// no way to express an unresolved name, so by the time a Command exists there
// is nothing left to guess and ops.Plan never reasons about ambiguity.
//
// A generated command stream also cannot fabricate a reference to something
// that does not exist, because the only way to make a Command is to resolve a
// name that did.
type Command interface {
	isCommand()
	Op() Op
}

// RawCommand is what arrives before any of that is true: an op nobody has
// checked and a bag of strings nobody has parsed.
//
// It appears only in this package, in internal/importer, and in the `:` line
// handler -- never in the keystroke path. A keystroke on a selected row already
// holds an identifier, and serialising it to a name to fuzzy-match back would
// be lossy in the worst way: an ambiguous name could resolve to a DIFFERENT row
// than the one under the cursor.
type RawCommand struct {
	Op     string
	Fields map[string]string
	// Line is where it came from, for reporting. Zero when it was typed.
	Line int
}

// ---------------------------------------------------------------------------
// Origination -- permanent, always confirmed
// ---------------------------------------------------------------------------

const (
	OpNewItem     Op = "new item"
	OpNewCategory Op = "new category"
	OpNewLocation Op = "new location"
	OpNewHolding  Op = "new holding"
)

// Counting is how a person chooses an Item's kind: three presets, never two
// booleans. It mirrors ops.Counting.
type Counting string

const (
	CountingUnique   Counting = "unique"
	CountingPile     Counting = "pile"
	CountingMeasured Counting = "measured"
)

type NewItem struct {
	Name        string
	Category    domain.CategoryID
	Counting    Counting
	ContentUnit domain.UnitCode
	PackageSize *domain.Quantity
	Notes       string
}

type NewCategory struct {
	Name        string
	Parent      *domain.CategoryID
	Description string
}

type NewLocation struct {
	Name        string
	Parent      *domain.LocationID
	Description string
}

type NewHolding struct {
	Item      domain.ItemID
	Location  domain.LocationID
	Basis     domain.UnitBasis
	ExpiresOn *time.Time
	Label     string
}

// ---------------------------------------------------------------------------
// Recording -- stock
// ---------------------------------------------------------------------------

const (
	OpReceive Op = "acquire"
	OpConsume Op = "consume"
	OpDiscard Op = "discard"
	OpOpen    Op = "open"
	OpCount   Op = "count"
	OpMove    Op = "move"
)

// Receive is spelled "acquire" because that is what a receipt says. The
// operation is called Receive because that is what the system does with it.
type Receive struct {
	Item      domain.ItemID
	Location  domain.LocationID
	Amount    domain.Quantity
	Basis     domain.UnitBasis
	ExpiresOn *time.Time
	Source    string
	Price     *int64
}

type Consume struct {
	Item     domain.ItemID
	Location domain.LocationID
	Amount   domain.Quantity
	Reason   string
}

type Discard struct {
	Holding domain.HoldingID
	Amount  domain.Quantity
	Reason  string
}

type Open struct {
	Item     domain.ItemID
	Location domain.LocationID
}

type Count struct {
	Holding  domain.HoldingID
	Observed domain.Quantity
}

type Move struct {
	Holding domain.HoldingID
	To      domain.LocationID
	Amount  *domain.Quantity // nil moves the whole Holding
}

// ---------------------------------------------------------------------------
// Recording -- custody, lifecycle, typing, the Location tree
// ---------------------------------------------------------------------------

const (
	OpCheckOut         Op = "checkout"
	OpReturn           Op = "return"
	OpMarkLost         Op = "lost"
	OpFound            Op = "found"
	OpVerify           Op = "verify"
	OpRetire           Op = "retire"
	OpRehome           Op = "rehome"
	OpPromote          Op = "promote"
	OpDemote           Op = "demote"
	OpReparentLocation Op = "reparent location"
	OpArchiveLocation  Op = "archive location"
	OpRestoreLocation  Op = "restore location"
)

type CheckOut struct {
	Holding domain.HoldingID
	To      *domain.LocationID
}

type Return struct {
	Holding domain.HoldingID
}

type MarkLost struct {
	Holding domain.HoldingID
}

// Found reverses a conclusion. At is where it turned up, which is the common
// case rather than the exotic one -- things are rarely lost and then found
// exactly where they were supposed to be.
//
// The EVENT carries no location, because Moved is already the one way to say
// "it is here now". The command does, and the operation composes the two. A
// command is an intent, not an event: consume emits three.
type Found struct {
	Holding domain.HoldingID
	At      *domain.LocationID
}

type Verify struct {
	Holding domain.HoldingID
	Present bool
}

type Retire struct {
	Holding domain.HoldingID
	Reason  string
}

type Rehome struct {
	Holding domain.HoldingID
	To      domain.LocationID
}

type Promote struct {
	Item domain.ItemID
}

// Demote carries the content unit and package size because ItemKindChanged
// cannot: an event may not contain derivable data, and the unit is not derivable
// from the fact that a kind changed (E7). This is the boundary Stage 5 deferred
// to exactly here.
type Demote struct {
	Item        domain.ItemID
	ContentUnit domain.UnitCode
	PackageSize *domain.Quantity
}

type ReparentLocation struct {
	Location domain.LocationID
	Parent   *domain.LocationID
}

type ArchiveLocation struct {
	Location   domain.LocationID
	Resolution domain.Resolution
	MoveTo     *domain.LocationID
}

type RestoreLocation struct {
	Location domain.LocationID
}

// ---------------------------------------------------------------------------
// Annotation -- revisable, never confirmed
// ---------------------------------------------------------------------------

const (
	OpRename           Op = "rename"
	OpDescribe         Op = "describe"
	OpNote             Op = "note"
	OpReclassify       Op = "reclassify"
	OpConfirm          Op = "confirm"
	OpSetExpiry        Op = "expires"
	OpLabel            Op = "label"
	OpSnooze           Op = "snooze"
	OpArchiveItem      Op = "archive item"
	OpArchiveCategory  Op = "archive category"
	OpRestoreCategory  Op = "restore category"
	OpReparentCategory Op = "reparent category"
)

// Target names something without saying which kind of thing it is, which is
// what a person does. "rename kitchen" does not say whether Kitchen is a
// Location or a Category; the resolver is what decides. Mirrors ops.Target.
type Target struct {
	Kind domain.EntityKind
	ID   int64
}

type Rename struct {
	Target Target
	Name   string
}

type Describe struct {
	Target      Target
	Description string
}

type Note struct {
	Item  domain.ItemID
	Notes string
}

type Reclassify struct {
	Item     domain.ItemID
	Category domain.CategoryID
}

type Confirm struct {
	Item domain.ItemID
}

type SetExpiry struct {
	Holding domain.HoldingID
	On      *time.Time
}

type Label struct {
	Holding domain.HoldingID
	Label   string
}

type Snooze struct {
	Holding domain.HoldingID
	Until   *time.Time
}

type ArchiveItem struct {
	Item domain.ItemID
}

type ArchiveCategory struct {
	Category   domain.CategoryID
	Resolution domain.Resolution
	MoveTo     *domain.CategoryID
}

type RestoreCategory struct {
	Category domain.CategoryID
}

type ReparentCategory struct {
	Category domain.CategoryID
	Parent   *domain.CategoryID
}

// ---------------------------------------------------------------------------
// The seal, and the Op each variant answers to
// ---------------------------------------------------------------------------
//
// A variant's TYPE NAME is also the name of the ops.Planner method that carries
// it out, and a test walks the generated registry asserting exactly that. The
// correspondence is derived rather than maintained, so a command with nothing
// behind it is a test failure rather than a runtime surprise.

func (NewItem) isCommand()          {}
func (NewCategory) isCommand()      {}
func (NewLocation) isCommand()      {}
func (NewHolding) isCommand()       {}
func (Receive) isCommand()          {}
func (Consume) isCommand()          {}
func (Discard) isCommand()          {}
func (Open) isCommand()             {}
func (Count) isCommand()            {}
func (Move) isCommand()             {}
func (CheckOut) isCommand()         {}
func (Return) isCommand()           {}
func (MarkLost) isCommand()         {}
func (Found) isCommand()            {}
func (Verify) isCommand()           {}
func (Retire) isCommand()           {}
func (Rehome) isCommand()           {}
func (Promote) isCommand()          {}
func (Demote) isCommand()           {}
func (ReparentLocation) isCommand() {}
func (ArchiveLocation) isCommand()  {}
func (RestoreLocation) isCommand()  {}
func (Rename) isCommand()           {}
func (Describe) isCommand()         {}
func (Note) isCommand()             {}
func (Reclassify) isCommand()       {}
func (Confirm) isCommand()          {}
func (SetExpiry) isCommand()        {}
func (Label) isCommand()            {}
func (Snooze) isCommand()           {}
func (ArchiveItem) isCommand()      {}
func (ArchiveCategory) isCommand()  {}
func (RestoreCategory) isCommand()  {}
func (ReparentCategory) isCommand() {}

func (NewItem) Op() Op          { return OpNewItem }
func (NewCategory) Op() Op      { return OpNewCategory }
func (NewLocation) Op() Op      { return OpNewLocation }
func (NewHolding) Op() Op       { return OpNewHolding }
func (Receive) Op() Op          { return OpReceive }
func (Consume) Op() Op          { return OpConsume }
func (Discard) Op() Op          { return OpDiscard }
func (Open) Op() Op             { return OpOpen }
func (Count) Op() Op            { return OpCount }
func (Move) Op() Op             { return OpMove }
func (CheckOut) Op() Op         { return OpCheckOut }
func (Return) Op() Op           { return OpReturn }
func (MarkLost) Op() Op         { return OpMarkLost }
func (Found) Op() Op            { return OpFound }
func (Verify) Op() Op           { return OpVerify }
func (Retire) Op() Op           { return OpRetire }
func (Rehome) Op() Op           { return OpRehome }
func (Promote) Op() Op          { return OpPromote }
func (Demote) Op() Op           { return OpDemote }
func (ReparentLocation) Op() Op { return OpReparentLocation }
func (ArchiveLocation) Op() Op  { return OpArchiveLocation }
func (RestoreLocation) Op() Op  { return OpRestoreLocation }
func (Rename) Op() Op           { return OpRename }
func (Describe) Op() Op         { return OpDescribe }
func (Note) Op() Op             { return OpNote }
func (Reclassify) Op() Op       { return OpReclassify }
func (Confirm) Op() Op          { return OpConfirm }
func (SetExpiry) Op() Op        { return OpSetExpiry }
func (Label) Op() Op            { return OpLabel }
func (Snooze) Op() Op           { return OpSnooze }
func (ArchiveItem) Op() Op      { return OpArchiveItem }
func (ArchiveCategory) Op() Op  { return OpArchiveCategory }
func (RestoreCategory) Op() Op  { return OpRestoreCategory }
func (ReparentCategory) Op() Op { return OpReparentCategory }

//go:generate go run ../../tools/sealedgen -dir . -out registry_gen.go -marker isCommand -iface Command -var AllCommands
