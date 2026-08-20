package testing_test

import (
	"strings"
	"testing"

	"home-management-system/internal/domain"
	sim "home-management-system/internal/tui/testing"
)

// 10d: the first place the interface writes.
//
// Every test here presses keys and then asks the DATABASE, which is the whole
// argument for the harness -- and the Simulator's VerifyAll cleanup means none
// of them can leave a state the ledger cannot reproduce, whatever else they
// were written to check.

// TestTheCommandLineWrites is the baseline, end to end.
func TestTheCommandLineWrites(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	rice := s.HasItem("Ancho Chile")
	s.OnHand(rice, 300*domain.Scale)

	s.Send(sim.Press("4"))
	s.Send(sim.Press("/"))
	s.Send(sim.Type("garage"))
	s.Send(sim.Enter)

	s.Send(sim.Press(":"))
	s.Send(sim.Type("consume 40"))
	s.Send(sim.Enter)

	s.OnHand(rice, 260*domain.Scale)
	// Feedback in the terms of the receipt, not the ledger's.
	s.ShowsText("use 40")
	s.HidesText("Consumed")
}

// A contextual line leaves the subject out and the cursor supplies it -- both
// the Item and the PLACE, because a holdings row is about an Item in a place.
func TestAContextualLineTakesItsSubjectFromTheCursor(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	rice := s.HasItem("Ancho Chile")

	s.Send(sim.Press("4"))
	s.Send(sim.Press("/"))
	s.Send(sim.Type("loc:garage"))
	s.Send(sim.Enter)
	s.Send(sim.Press(":"))
	s.Send(sim.Type("consume 10"))
	s.Send(sim.Enter)

	// Exactly the Garage holding moved, and the other two did not.
	s.OnHand(rice, 290*domain.Scale)
	s.Send(sim.Esc)
	if got := strings.Count(s.PlainView(), "100 g"); got != 2 {
		t.Errorf("%d holdings still at 100 g, want the 2 that were not named", got)
	}
}

// Naming the subject explicitly beats the cursor. A person who types an item
// gets the item they typed, even pointing at something else.
func TestNamingTheSubjectBeatsTheCursor(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	adapter := s.HasItem("Thunderbolt 4 to Dual DisplayPort 1.4 Adapter (Space Grey, 0.8m)")

	s.Send(sim.Press("4"))
	s.Send(sim.Press(":"))
	s.Send(sim.Type(`rename "Ancho Chile" "Ancho Chilli"`))
	s.Send(sim.Enter)

	s.HasItem("Ancho Chilli")
	// And the thing under the cursor is untouched.
	if s.HasItem("Thunderbolt 4 to Dual DisplayPort 1.4 Adapter (Space Grey, 0.8m)") != adapter {
		t.Error("the cursor's row was renamed instead")
	}
}

// Editing happens INSIDE the list. Losing your place is the thing that made the
// old interface unusable for its actual job.
func TestRenamingInPlaceDoesNotMoveTheList(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("2"))
	s.Send(sim.Press("j"), sim.Press("j"))

	before := cursorLine(s)
	s.Send(sim.Press("e"))
	if after := cursorLine(s); after != before {
		t.Errorf("opening the editor moved the cursor:\n before %q\n after  %q", before, after)
	}
	s.ShowsText("rename")

	// Whatever jj lands on -- the test is about the list not moving, so it
	// reads the name off the screen rather than assuming the tree's shape.
	name := nameOn(before)
	if name == "" {
		t.Fatalf("could not read a name off %q", before)
	}
	s.Send(sim.Type(" Two"))
	s.Send(sim.Enter)
	s.HasLocation(name + " Two")
}

// Abandoning an edit changes nothing.
func TestAbandoningAnEditChangesNothing(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("2"))
	s.Send(sim.Press("j"))

	s.Send(sim.Press("e"))
	s.Send(sim.Type(" Nonsense"))
	s.Send(sim.Esc)

	s.HasLocation("Garage")
	s.HidesText("Garage Nonsense")
	s.ShowsText("unchanged")
}

// Creation is never silent and never one keystroke.
func TestCreatingAsksFirstAndSaysWhatIsPermanent(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("4"))
	s.Send(sim.Press(":"))
	s.Send(sim.Type("new item Turmeric counting measured unit g package 2000 category Spices"))
	s.Send(sim.Enter)

	// Nothing yet.
	s.HasNoItem("Turmeric")
	s.ShowsText("permanent")
	s.ShowsText("kind = Bulk")
	// The facts are not the point on their own -- the reason to care is.
	s.ShowsText("replaces every holding")

	// esc means nothing happened.
	s.Send(sim.Esc)
	s.HasNoItem("Turmeric")
	s.ShowsText("nothing was created")
}

func TestConfirmingCreates(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("4"))
	s.Send(sim.Press(":"))
	s.Send(sim.Type("new item Turmeric counting measured unit g category Spices"))
	s.Send(sim.Enter)
	s.Send(sim.Enter)

	s.HasItem("Turmeric")
}

// A confirmation that a stray keystroke could dismiss is not a confirmation.
func TestOnlyEnterAndEscapeReachTheConfirmation(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("4"))
	s.Send(sim.Press(":"))
	s.Send(sim.Type("new item Turmeric counting measured unit g category Spices"))
	s.Send(sim.Enter)

	s.Send(sim.Press("j"), sim.Press("q"), sim.Press("2"), sim.Press("/"), sim.CtrlP)
	s.ShowsText("permanent")
	s.HasNoItem("Turmeric")
}

// A refusal is a sentence. The operations layer produces them; the question is
// whether the interface passes them through or buries them.
func TestARefusalIsASentence(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("4"))
	s.Send(sim.Press("/"))
	s.Send(sim.Type("loc:garage"))
	s.Send(sim.Enter)
	s.Send(sim.Press(":"))
	s.Send(sim.Type("consume 5kg"))
	s.Send(sim.Enter)

	view := s.PlainView()
	if strings.Contains(view, "constraint") || strings.Contains(view, "sql:") {
		t.Errorf("a refusal leaked the storage layer:\n%s", view)
	}
	// It names the thing and the numbers, which is what makes a refusal
	// actionable rather than merely polite.
	for _, want := range []string{"Ancho Chile", "have 100", "need 5000"} {
		if !strings.Contains(view, want) {
			t.Errorf("the refusal does not mention %q:\n%s", want, view)
		}
	}
	// And nothing happened.
	s.OnHand(s.HasItem("Ancho Chile"), 300*domain.Scale)
}

// A command that cannot be built says which FIELD is wrong, not which
// constraint failed.
func TestAnUnbuildableCommandNamesTheField(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("4"))
	s.Send(sim.Press(":"))
	s.Send(sim.Type("new item Turmeric counting measured unit g"))
	s.Send(sim.Enter)

	s.ShowsText("category")
	s.ShowsText("required")
	s.HidesText("FOREIGN KEY")
	s.HasNoItem("Turmeric")
}

// TestEscapeLeavesExactlyOneMode is the substage's exit criterion, and the old
// interface's defect stated as a test.
//
// StatePickingCategory was reachable from inside item editing, so esc sometimes
// left one mode and sometimes two, and which it was depended on how you got
// there. Here every mode is entered, then escaped one at a time, and each esc
// has to move exactly one level.
func TestEscapeLeavesExactlyOneMode(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	// Three things on at once: a filter, a selection, and an open command line.
	s.Send(sim.Press("4"))
	s.Send(sim.Press("/"))
	s.Send(sim.Type("ancho"))
	s.Send(sim.Enter)
	s.Send(sim.Space, sim.Space)
	s.Send(sim.Press(":"))
	s.Send(sim.Type("consume 1"))

	// 1: the command line closes. The filter and the selection stay.
	s.Send(sim.Esc)
	s.ShowsText("/ancho")
	s.ShowsText("2 selected")

	// 2: the selection clears. The filter stays.
	s.Send(sim.Esc)
	s.HidesText("selected")
	s.ShowsText("/ancho")

	// 3: the filter clears.
	s.Send(sim.Esc)
	s.HidesText("/ancho")
	s.ShowsText("Thunderbolt") // the rows the filter had been hiding

	// And more escapes do nothing rather than something surprising.
	settled := s.PlainView()
	s.Send(sim.Esc, sim.Esc, sim.Esc)
	if got := s.PlainView(); got != settled {
		t.Errorf("escaping from the plain list changed the screen:\n%s", got)
	}
}

// The same, from the deepest state the interface has: a confirmation inside a
// command line inside a filtered view.
func TestEscapeUnwindsTheDeepestState(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("4"))
	s.Send(sim.Press("/"))
	s.Send(sim.Type("ancho"))
	s.Send(sim.Enter)
	s.Send(sim.Press(":"))
	s.Send(sim.Type("new item Turmeric counting measured unit g category Spices"))
	s.Send(sim.Enter)
	s.ShowsText("permanent")

	s.Send(sim.Esc) // the confirmation
	s.HidesText("permanent")
	s.ShowsText("/ancho")

	s.Send(sim.Esc) // the filter
	s.HidesText("/ancho")
	s.HasNoItem("Turmeric")
}

// And an editor is its own level, not two.
func TestEscapeFromAnEditorLeavesOnlyTheEditor(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("2"))
	s.Send(sim.Press("/"))
	s.Send(sim.Type("garage"))
	s.Send(sim.Enter)
	s.Send(sim.Press("e"))
	s.Send(sim.Type(" Nope"))

	s.Send(sim.Esc)
	s.HidesText("enter save")
	s.ShowsText("/garage") // the filter survived
}

// Browsing still writes nothing, now that writing is possible.
func TestBrowsingStillWritesNothing(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	before := s.CountHoldings()
	rice := s.HasItem("Ancho Chile")

	for _, view := range []string{"1", "2", "3", "4"} {
		s.Send(sim.Press(view))
		s.Send(sim.Press("j"), sim.Press("k"), sim.Press("G"), sim.Press("s"))
		s.Send(sim.Space, sim.Esc, sim.Press("n"), sim.Press("N"))
		s.Send(sim.Press("e"), sim.Esc)
		s.Send(sim.Press(":"), sim.Esc)
	}
	if after := s.CountHoldings(); after != before {
		t.Errorf("browsing changed the holdings from %d to %d", before, after)
	}
	s.OnHand(rice, 300*domain.Scale)
}
