package testing_test

import (
	"testing"

	"home-management-system/internal/domain"
	sim "home-management-system/internal/tui/testing"
)

// Organise mode: the arrangement, staged.
//
// The promise is exactly one sentence -- nothing is written until you apply
// -- and every test here is that sentence from a different angle. It is the
// only promise the mode makes, and it is the only one worth making: the
// domain model already says reorganising is free, so the interface owes a
// person the chance to try a shape before owning it.

// TestOrganisingStagesInsteadOfWriting, which is the whole of the mode.
func TestOrganisingStagesInsteadOfWriting(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("O"))
	s.ShowsText("ORGANISE")

	s.Send(sim.Press("m"))
	s.Send(sim.Type("Garage"))
	s.Send(sim.Enter)

	// Staged, and said so -- and NOT written. The jar is still where it was.
	s.ShowsText("Garage")
	s.ShowsText("1 ready")
	s.OnHand(rice, 500*domain.Scale)
	if at := whereIs(t, s, rice); at != "Left Pantry" {
		t.Errorf("staging moved the holding to %q; nothing should have been written", at)
	}
}

// TestApplyingTheBatchWritesItAllAtOnce.
func TestApplyingTheBatchWritesItAllAtOnce(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("O"))
	s.Send(sim.Press("m"))
	s.Send(sim.Type("Garage"))
	s.Send(sim.Enter)
	s.Send(sim.Press("A"))

	s.ShowsText("one transaction")
	if at := whereIs(t, s, rice); at != "Garage" {
		t.Errorf("after applying, the holding is at %q, want Garage", at)
	}
	// And the mode is over: what it was staging is the house now.
	s.HidesText("ORGANISE")
}

// TestTakingBackTheLastEdit. Undo for something that has not happened yet,
// which is why it does not ask.
func TestTakingBackTheLastEdit(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("O"))
	s.Send(sim.Press("m"))
	s.Send(sim.Type("Garage"))
	s.Send(sim.Enter)
	s.ShowsText("1 ready")

	s.Send(sim.Press("u"))
	s.ShowsText("took back")
	s.ShowsText("0 ready")

	// And applying an empty batch writes nothing rather than reporting a
	// transaction that did not happen.
	s.Send(sim.Press("A"))
	s.ShowsText("nothing is staged")
	if at := whereIs(t, s, rice); at != "Left Pantry" {
		t.Errorf("the holding moved to %q after the edit was taken back", at)
	}
}

// TestAbandoningSaysHowMuchWasPutDown.
//
// Nothing was written, so nothing can be recovered -- which is exactly why
// the count has to be said. A batch of nine vanishing in silence is nine
// pieces of work a person has no way of knowing they lost.
func TestAbandoningSaysHowMuchWasPutDown(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("O"))
	s.Send(sim.Press("m"))
	s.Send(sim.Type("Garage"))
	s.Send(sim.Enter)

	s.Send(sim.Esc)
	s.ShowsText("1 row put down")
	s.HidesText("ORGANISE")
	if at := whereIs(t, s, rice); at != "Left Pantry" {
		t.Errorf("abandoning the batch wrote it anyway: the holding is at %q", at)
	}
}

// TestTheHouseKeepsItsCursorWhileOrganising.
//
// Every other drawer takes the keyboard, because every other drawer has a
// list of its own to point at. This one does not: the tree in the rail is
// what is being edited, so blurring it would hide the one cursor the mode is
// about.
func TestTheHouseKeepsItsCursorWhileOrganising(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnRail()

	s.Send(sim.Press("O"))
	if !s.ShowsCursor() {
		t.Error("organise mode blurred the house; the rail is what it edits")
	}
	// And the rail still moves.
	s.Send(sim.CtrlN)
	s.ShowsText("ORGANISE")
}

// TestQuantityIsNeverStaged.
//
// Arranging is free and reversible, which is what makes holding a batch of
// it honest. Consuming a jar is neither, so a mode that promised to defer it
// would be promising a safety the system cannot provide -- it writes at once
// in organise mode exactly as it does everywhere else.
func TestQuantityIsNeverStaged(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("O"))
	s.Send(sim.Press("c"))
	s.Send(sim.Type("100"))
	s.Send(sim.Enter)

	s.OnHand(rice, 400*domain.Scale)
	// Nothing reached the batch.
	s.ShowsText("0 ready")
}

// whereIs is the place a holding of an item currently sits, read from the
// database rather than from the screen.
func whereIs(t *testing.T, s *sim.Simulator, item domain.ItemID) string {
	t.Helper()
	rows, err := s.Reader().HoldingsOfItem(s.Context(), item)
	if err != nil {
		t.Fatalf("reading holdings: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("no holdings of item %d", item)
	}
	return rows[0].LocationName
}
