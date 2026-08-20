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
type Batch struct{ Steps []Step }

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
