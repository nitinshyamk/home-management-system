package testing_test

import (
	"strings"
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
	sim "home-management-system/internal/tui/testing"
)

// Undo: the way back, where there is one.
//
// Every test here is about the second half of that sentence. An undo that
// silently did nothing on the operation people most want undone would be
// worse than no undo at all, so what is under test is mostly the refusals
// and the absence of the offer.

// TestUndoingAMovePutsItBack, and does it by moving rather than by deleting.
func TestUndoingAMovePutsItBack(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)
	holding := onlyHolding(t, s, rice)

	s.Send(sim.Press("m"))
	s.Send(sim.Type("Garage"))
	s.Send(sim.Enter)
	if at := whereIs(t, s, rice); at != "Garage" {
		t.Fatalf("the move did not happen: the holding is at %q", at)
	}

	s.Send(sim.Press("z"))
	if at := whereIs(t, s, rice); at != "Left Pantry" {
		t.Errorf("after undo the holding is at %q, want Left Pantry", at)
	}

	// Both moves are in the history. The ledger is append-only and that is
	// the point: you did move it, and then you moved it back.
	events := s.EventTypes(holding)
	var moves int
	for _, e := range events {
		if e == "Moved" {
			moves++
		}
	}
	if moves != 2 {
		t.Errorf("the history records %d moves in %v, want 2 -- undo writes, it does not erase", moves, events)
	}
}

// TestUndoIsOfferedOnlyWhenItIsTrue.
//
// The line at the bottom is permanent, so the only thing that makes it worth
// the row is that it can be believed.
func TestUndoIsOfferedOnlyWhenItIsTrue(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnContents()
	// No filter: an applied filter makes the input line draw its own state
	// instead of the offers, so the verb bar -- which is where undo is
	// offered -- is not on screen to assert about.

	if strings.Contains(s.PlainView(), "undo that") {
		t.Errorf("undo was offered before anything had been done:\n%s", s.PlainView())
	}

	s.Send(sim.Press("m"))
	s.Send(sim.Type("Garage"))
	s.Send(sim.Enter)
	if !strings.Contains(s.PlainView(), "undo that") {
		t.Errorf("undo was not offered after a move:\n%s", s.PlainView())
	}

	s.Send(sim.Press("z"))
	if strings.Contains(s.PlainView(), "undo that") {
		t.Errorf("undo was still offered after being used:\n%s", s.PlainView())
	}
}

// TestConsumingOffersNoUndo.
//
// Receiving is not the inverse of consuming: it would record an acquisition,
// with a source, that never happened -- and the ledger's whole value is that
// it does not contain events nobody caused. Counting it back is no better,
// because Counted means somebody looked. A clean inverse would need an
// Adjust command carrying a reason, which the domain does not have.
func TestConsumingOffersNoUndo(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("c"))
	s.Send(sim.Type("100"))
	s.Send(sim.Enter)
	s.OnHand(rice, 400*domain.Scale)

	if strings.Contains(s.PlainView(), "undo that") {
		t.Errorf("undo was offered for a consume:\n%s", s.PlainView())
	}
	s.Send(sim.Press("z"))
	s.ShowsText("no way back")
	s.OnHand(rice, 400*domain.Scale)
}

// TestRetiringOffersNoUndo. Gone is the single lifecycle terminal and
// nothing clears RetiredAt, so there is nothing an inverse could be.
func TestRetiringOffersNoUndo(t *testing.T) {
	s := sim.New(t)
	p, ctx := s.Planner(), s.Context()
	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Shed"}))
	shed := s.HasLocation("Shed")
	s.Apply(p.NewCategory(ctx, ops.NewCategoryRequest{Name: "Tools"}))
	tools := s.HasCategory("Tools")
	s.Apply(p.NewItem(ctx, ops.NewItemRequest{
		Name: "Old Drill", Category: tools, Counting: ops.CountingUnique,
	}))
	s.Apply(p.NewHolding(ctx, ops.NewHoldingRequest{
		Item: s.HasItem("Old Drill"), Location: shed,
	}))

	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlK)
	s.Send(sim.Enter) // the confirmation

	if strings.Contains(s.PlainView(), "undo that") {
		t.Errorf("undo was offered for a retirement:\n%s", s.PlainView())
	}
}

// TestUndoWithNothingBehindItSaysSo rather than doing nothing quietly.
func TestUndoWithNothingBehindItSaysSo(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnContents()

	s.Send(sim.Press("z"))
	s.ShowsText("nothing to undo")
}
