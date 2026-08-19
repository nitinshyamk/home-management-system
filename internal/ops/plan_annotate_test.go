package ops_test

import (
	"errors"
	"testing"
	"time"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
)

// The annotation surface, which the command layer dispatches on uniformly.

// Rename is one operation rather than three because renaming is one act. The
// dispatch is on data the resolver produced, so getting it wrong renames the
// wrong thing -- which is why each kind is checked rather than one of them.
func TestRenameDispatchesOnTheTargetKind(t *testing.T) {
	tr := newTree(t)
	target := func(k domain.EntityKind, id int64) ops.Target {
		return ops.Target{Kind: k, ID: id}
	}

	tr.apply(tr.pl.Rename(tr.ctx, ops.RenameRequest{
		Target: target(domain.EntityLocation, int64(tr.pantry)), Name: "Larder",
	}))
	tr.apply(tr.pl.Rename(tr.ctx, ops.RenameRequest{
		Target: target(domain.EntityCategory, int64(tr.cat)), Name: "Dry Goods",
	}))
	tr.apply(tr.pl.Rename(tr.ctx, ops.RenameRequest{
		Target: target(domain.EntityItem, int64(tr.item)), Name: "Jasmine Rice",
	}))

	if got := tr.location(t, tr.pantry).Name; got != "Larder" {
		t.Errorf("location is %q, want Larder", got)
	}
	cat, err := tr.r.Category(tr.ctx, tr.cat)
	if err != nil {
		t.Fatalf("read category: %v", err)
	}
	if cat.Name != "Dry Goods" {
		t.Errorf("category is %q, want Dry Goods", cat.Name)
	}
	item, err := tr.r.Item(tr.ctx, tr.item)
	if err != nil {
		t.Fatalf("read item: %v", err)
	}
	if got := item.Base().Name; got != "Jasmine Rice" {
		t.Errorf("item is %q, want Jasmine Rice", got)
	}

	// A Holding's name is its Item's. What a Holding has is a label, which is
	// a different command, so this is a refusal rather than a fourth branch.
	rice := tr.stow(t, tr.pantry)
	_, err = tr.pl.Rename(tr.ctx, ops.RenameRequest{
		Target: target(domain.EntityHolding, int64(rice)), Name: "the big bag",
	})
	if !errors.Is(err, ops.ErrWrongKind) {
		t.Errorf("renamed a Holding: %v", err)
	}
	if _, err := tr.pl.Rename(tr.ctx, ops.RenameRequest{
		Target: target(domain.EntityItem, int64(tr.item)),
	}); !errors.Is(err, ops.ErrInvalidRequest) {
		t.Errorf("renamed to nothing: %v", err)
	}
}

// An Item's free text is knowledge about the thing, not a description of a tree
// node, so Describe covers the two tree kinds and Note covers the Item.
func TestDescribeCoversTheTreesAndNoteCoversTheItem(t *testing.T) {
	tr := newTree(t)

	tr.apply(tr.pl.Describe(tr.ctx, ops.DescribeRequest{
		Target:      ops.Target{Kind: domain.EntityLocation, ID: int64(tr.pantry)},
		Description: "the tall cupboard",
	}))
	if got := tr.location(t, tr.pantry).Description; got != "the tall cupboard" {
		t.Errorf("description = %q", got)
	}

	if _, err := tr.pl.Describe(tr.ctx, ops.DescribeRequest{
		Target: ops.Target{Kind: domain.EntityItem, ID: int64(tr.item)}, Description: "long grain",
	}); !errors.Is(err, ops.ErrWrongKind) {
		t.Errorf("described an Item: %v", err)
	}

	tr.apply(tr.pl.Note(tr.ctx, ops.NoteRequest{Item: tr.item, Notes: "long grain"}))
	item, err := tr.r.Item(tr.ctx, tr.item)
	if err != nil {
		t.Fatalf("read item: %v", err)
	}
	if got := item.Base().Notes; got != "long grain" {
		t.Errorf("notes = %q, want %q", got, "long grain")
	}
}

// The three Holding labels, which had no write path at all before Stage 9.
func TestHoldingLabelOperations(t *testing.T) {
	tr := newTree(t)
	rice := tr.stow(t, tr.pantry)
	cable := tr.unique(t, tr.shelf1)
	// Deliberately carrying a time of day: an expiry is what is printed on the
	// packet, and it is stored as a DATE because expires_on is part of H8's key
	// -- two spellings of one date would read as two Holdings.
	stamped := clock.AddDate(0, 7, 0)
	march := time.Date(2027, 3, 17, 0, 0, 0, 0, time.UTC)

	tr.apply(tr.pl.SetExpiry(tr.ctx, ops.SetExpiryRequest{Holding: rice, On: &stamped}))
	tr.apply(tr.pl.Snooze(tr.ctx, ops.SnoozeRequest{Holding: rice, Until: &stamped}))
	tr.apply(tr.pl.Label(tr.ctx, ops.LabelRequest{Holding: cable, Label: "the braided one"}))

	d, err := tr.r.Holding(tr.ctx, rice)
	if err != nil {
		t.Fatalf("read holding: %v", err)
	}
	if on := d.Holding.Base().ExpiresOn; on == nil || !on.Equal(march) {
		t.Errorf("expires = %v, want %v", on, march)
	}
	if on := d.Holding.Base().SnoozedUntil; on == nil || !on.Equal(stamped) {
		t.Errorf("snoozed = %v, want %v", on, stamped)
	}
	u, err := tr.r.Holding(tr.ctx, cable)
	if err != nil {
		t.Fatalf("read holding: %v", err)
	}
	if got := u.Holding.(domain.UniqueHolding).Label; got != "the braided one" {
		t.Errorf("label = %q", got)
	}

	// Annotation is one Step with nothing to confirm: everything here is freely
	// revisable, and friction is proportional to permanence.
	batch, err := tr.pl.Label(tr.ctx, ops.LabelRequest{Holding: cable, Label: "the short one"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if batch.Steps[0].NeedsConfirmation() {
		t.Error("labelling asks for confirmation; nothing about it is permanent")
	}
}

// Reparenting a Location is RECORDED while reparenting a Category is
// ANNOTATED, and the difference is not a stylistic one: a Location's contents
// keep their own stowed_location when the shelf moves, so without an event
// something moved and nothing recorded why. A Category references nothing
// physical, so re-filing it moves no object and replay has nothing to rebuild.
func TestReparentingLeavesAnEventOnlyForLocations(t *testing.T) {
	tr := newTree(t)

	tr.apply(tr.pl.ReparentLocation(tr.ctx, ops.ReparentLocationRequest{
		Location: tr.shelf1, Parent: &tr.garage,
	}))
	if got := tr.location(t, tr.shelf1).Parent; got == nil || *got != tr.garage {
		t.Errorf("Shelf 1 parent = %v, want Garage", got)
	}
	history, err := tr.led.History(tr.ctx, domain.SubjectLocation, int64(tr.shelf1))
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if got := types(history); !contains(got, "NodeReparented") {
		t.Errorf("Shelf 1 history = %v, want a NodeReparented", got)
	}

	// The Category counterpart has no history to appear in -- Category has no
	// ledger at all -- so what is asserted is that it moved and that the
	// annotation path is what moved it.
	sub, err := tr.pl.NewCategory(tr.ctx, ops.NewCategoryRequest{Name: "Grains", Parent: &tr.cat})
	if err != nil {
		t.Fatalf("plan category: %v", err)
	}
	res, err := tr.ex.Execute(tr.ctx, sub)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	grains := res.Created[0].Categories[0]
	batch, err := tr.pl.ReparentCategory(tr.ctx, ops.ReparentCategoryRequest{Category: grains})
	if err != nil {
		t.Fatalf("plan reparent: %v", err)
	}
	if batch.Steps[0].Records != nil {
		t.Error("re-filing a Category records events; it moves no object")
	}
	if _, err := tr.ex.Execute(tr.ctx, batch); err != nil {
		t.Fatalf("execute: %v", err)
	}
	cat, err := tr.r.Category(tr.ctx, grains)
	if err != nil {
		t.Fatalf("read category: %v", err)
	}
	if cat.Parent != nil {
		t.Errorf("Grains parent = %v, want none", cat.Parent)
	}
}

// Snoozing and expiry both accept nil, which is how a mistaken date is revised
// away rather than replaced.
func TestClearingADateIsAnAnnotationToo(t *testing.T) {
	tr := newTree(t)
	rice := tr.stow(t, tr.pantry)
	march := clock.AddDate(0, 7, 0)

	tr.apply(tr.pl.SetExpiry(tr.ctx, ops.SetExpiryRequest{Holding: rice, On: &march}))
	tr.apply(tr.pl.SetExpiry(tr.ctx, ops.SetExpiryRequest{Holding: rice, On: nil}))

	d, err := tr.r.Holding(tr.ctx, rice)
	if err != nil {
		t.Fatalf("read holding: %v", err)
	}
	if on := d.Holding.Base().ExpiresOn; on != nil {
		t.Errorf("expires = %v after clearing, want none", on)
	}
}
