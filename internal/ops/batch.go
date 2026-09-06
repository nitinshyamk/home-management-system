// Package ops is the operations layer: it turns one user intent into the writes
// that intent implies, and applies them as a single unit of work.
//
// It exists because intents do not respect the write paths. "I bought rice for
// the first time" originates an Item, records a Holding into existence, and
// records what arrived -- three writes across two paths that must land together
// or not at all. v01 had no unit of work spanning paths, so a partial failure
// left an Item with no Holding and nothing detected it.
//
// ops is also the ONLY package outside internal/db that opens a transaction.
// One place owning transaction boundaries is what makes atomicity auditable
// rather than a property one has to trust.
package ops

import (
	"context"
	"fmt"

	"home-management-system/internal/annotate"
	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/origin"
)

// Batch is a reviewed unit of work: every Step commits, or none does.
type Batch struct {
	Steps []Step

	// fills is which H8 slots this Batch puts a Holding into, and which
	// Holding ends up in each.
	//
	// It exists because a Batch is planned against a SNAPSHOT, and two Batches
	// merged into one unit of work were each planned against a photograph taken
	// before either ran. Plan.filled solves exactly this problem inside one
	// plan; nothing solved it between them, so moving two jars of rice onto one
	// shelf in a single gesture planned two whole moves onto a slot that was
	// free in both photographs, and produced two active Holdings on one H8 key
	// -- silently, with the integrity report as the only witness.
	//
	// Carried on the Batch rather than recomputed, because only the planner
	// knows which slot an operation lands in: the basis and the expiry come
	// from the Holding being moved, not from the request.
	fills map[slot]occupant
}

// occupant is who ends up in an H8 slot: a Holding that already exists, or one
// the Batch will create, whose identifier is not knowable until it runs.
type occupant struct {
	id      domain.HoldingID
	created bool
}

// Absorb takes another Batch's Steps and its slot claims, which is what merging
// two units of work into one has to mean: the claims travel with the Steps, or
// a third Batch merged afterwards would be compared against only the first.
func (b Batch) Absorb(other Batch) Batch {
	b.Steps = append(b.Steps, other.Steps...)
	return b.filling(other.fills)
}

// filling records that this Batch puts an occupant into a slot.
//
// A fresh map every time, never a write into the one already there. A Batch is
// a value and is copied freely, so mutating its map in place would reach every
// copy -- and merging a plan into two different batches would quietly give both
// of them the other's claims.
func (b Batch) filling(fills map[slot]occupant) Batch {
	if len(fills) == 0 {
		return b
	}
	merged := make(map[slot]occupant, len(b.fills)+len(fills))
	for k, v := range b.fills {
		merged[k] = v
	}
	for k, v := range fills {
		merged[k] = v
	}
	b.fills = merged
	return b
}

// ConflictsWith reports two Batches that cannot be merged into one unit of
// work, because between them they would put two different Holdings on one H8
// key.
//
// Two Batches filling the same slot with the SAME existing Holding is not a
// conflict and must not be reported as one: two receipts of the same rice onto
// the same shelf are two Acquired events against one Holding, which is what
// they should be. The conflict is two DIFFERENT Holdings, and a Holding that
// does not exist yet is different from everything -- there is no way to say
// "the one the previous step is about to create" in a plan, because a reference
// resolves against the Step that made it.
//
// This is a refusal rather than a merge, and the difference matters. O1 says a
// write that would violate H8 merges into the existing Holding, and that is
// what happens WITHIN a plan. Doing it across plans needs a reference that
// spans Steps, which the execution model does not have. Refusing is the honest
// half: nothing is corrupted, and the person is told to do it in two goes.
func (b Batch) ConflictsWith(other Batch) (string, bool) {
	for k, mine := range b.fills {
		theirs, both := other.fills[k]
		if !both {
			continue
		}
		if !mine.created && !theirs.created && mine.id == theirs.id {
			continue
		}
		return "two of these would end up as one holding -- the same thing, " +
			"in the same place, on the same basis. Do them one at a time, or " +
			"send one of them somewhere else", true
	}
	return "", false
}

// Step is one intent.
//
// Its shape is the three write paths for the third time -- schema section 3.3
// gave the columns, the package layout gave the boundaries, and this gives the
// plan. That correspondence is what makes "creation is never silent" mechanical
// rather than a UI convention: a non-empty Originates forces confirmation, and
// no review screen can fail to notice.
type Step struct {
	// Summary is what a person reads. Rendered from the command that produced
	// this Step.
	Summary string

	// Ends names what this step puts beyond recovery, and is empty when nothing
	// is. Gone is the single lifecycle terminal -- no event clears RetiredAt --
	// so a step that emits one is as permanent IN EFFECT as an origination, and
	// friction is proportional to permanence rather than to write path.
	//
	// Derived from the EVENTS rather than declared per operation, so an
	// operation that ends something inherits the confirmation instead of having
	// to remember to ask for it.
	Ends string

	// Originates is PERMANENT, so it is data: the review screen renders each
	// one in full and requires an explicit confirmation. A wrong kind or
	// content unit has no remedy short of retiring the entity, which is why
	// this is the only bucket a person is asked to approve field by field.
	Originates []Origination

	// Records is RECORDED -- reversible by a compensating event -- and
	// Annotates is REVISABLE. Both are built at execution time from whatever
	// Originates produced, because that is how an identifier flows from a
	// creation to the events about it without a general dependency graph.
	// Neither is reviewed field by field; Summary is what a person reads.
	Records   func(Created) ([]domain.Event, error)
	Annotates func(Created) ([]Annotation, error)
}

// NeedsConfirmation reports whether this Step brings a named entity into
// existence, and so must be approved before it may be applied.
func (s Step) NeedsConfirmation() bool {
	for _, o := range s.Originates {
		if o.NeedsConfirmation() {
			return true
		}
	}
	return false
}

// Created carries the identifiers a Step has produced so far, in origination
// order, so a later part of the SAME Step can refer to something that did not
// exist when the Step was planned.
//
// Deliberately scoped to one Step. Cross-Step dependencies would need a real
// dependency graph, and every intent this system has fits inside one Step --
// two receipt rows naming the same new item are merged into one Step by the
// binder rather than resolved by ordering.
type Created struct {
	Categories []domain.CategoryID
	Items      []domain.ItemID
	Locations  []domain.LocationID
	Holdings   []domain.HoldingID
}

// Item returns the n-th Item this Step created.
func (c Created) Item(n int) (domain.ItemID, error) {
	if n >= len(c.Items) {
		return 0, fmt.Errorf("%w: step referenced created item %d, but it created %d",
			ErrPlan, n, len(c.Items))
	}
	return c.Items[n], nil
}

// Holding returns the n-th Holding this Step created.
func (c Created) Holding(n int) (domain.HoldingID, error) {
	if n >= len(c.Holdings) {
		return 0, fmt.Errorf("%w: step referenced created holding %d, but it created %d",
			ErrPlan, n, len(c.Holdings))
	}
	return c.Holdings[n], nil
}

// Location returns the n-th Location this Step created.
func (c Created) Location(n int) (domain.LocationID, error) {
	if n >= len(c.Locations) {
		return 0, fmt.Errorf("%w: step referenced created location %d, but it created %d",
			ErrPlan, n, len(c.Locations))
	}
	return c.Locations[n], nil
}

// Category returns the n-th Category this Step created.
func (c Created) Category(n int) (domain.CategoryID, error) {
	if n >= len(c.Categories) {
		return 0, fmt.Errorf("%w: step referenced created category %d, but it created %d",
			ErrPlan, n, len(c.Categories))
	}
	return c.Categories[n], nil
}

// ---------------------------------------------------------------------------
// Origination
// ---------------------------------------------------------------------------

// Origination is a sealed union of the things a Step may bring into existence.
//
// Describe is not decoration: it is the text a person approves, and it must
// name every field that cannot be changed afterwards.
type Origination interface {
	isOrigination()

	// Describe renders the origination as prose, for a plan line.
	Describe() string

	// Permanent is what cannot be changed afterwards, set out as FACTS rather
	// than prose, for the confirmation panel to lay out on their own. Empty
	// when there is nothing of the sort -- a Category has no permanent fields.
	//
	// Separate from Describe because the confirmation is the highest-stakes
	// text in the application, and picking facts back out of a sentence is how
	// it would quietly start saying the wrong thing.
	Permanent() string

	// NeedsConfirmation reports whether a person must approve this before it
	// is applied.
	//
	// True for NAMED entities -- Item, Category, Location -- and the reason is
	// not permanence but duplication: a receipt that quietly creates a second
	// "Turmeric" is worse than one that stops and asks, and only a person can
	// tell whether the Turmeric on the receipt is the Turmeric in the cupboard.
	//
	// False for Holdings. A Holding is a placement rather than a name: it comes
	// into existence because something was put somewhere, and there is no
	// second one to be confused with. Consuming 100 g from a sealed bag creates
	// the content-basis Holding to open it into, and asking about that would
	// make the commonest action in the system a dialogue.
	NeedsConfirmation() bool

	// originate performs the creation inside the Step's transaction. Both write
	// paths are passed because origination is split across them by design:
	// Categories and Items are only audited, so backward closure suffices and
	// they are created directly; Holdings and Locations are VERIFIED, so their
	// creation must be recorded as an event (schema section 3.11).
	originate(ctx context.Context, o *origin.Originator, l *ledger.Processor, c *Created) error
}

// ---------------------------------------------------------------------------
// Annotation
// ---------------------------------------------------------------------------

// Annotation is a sealed union of revisions to labels and knowledge. Nothing
// here is confirmed, because everything here is freely revisable.
type Annotation interface {
	isAnnotation()
	Describe() string
	annotate(ctx context.Context, a *annotate.Annotator) error
}
