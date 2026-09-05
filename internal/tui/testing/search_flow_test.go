package testing_test

import (
	"strings"
	"testing"

	sim "home-management-system/internal/tui/testing"
)

// 10c through the Simulator. The widget tests decide whether filtering works;
// what needs a real database is whether the two MODES stay distinct once they
// are wired to real data and real navigation.

// TestFilteringNarrowsAndSaysSo is the whole of the filter's contract.
func TestFilteringNarrowsAndSaysSo(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("4"))

	before := strings.Count(s.PlainView(), "Ancho Chile")
	if before != 3 {
		t.Fatalf("expected 3 Ancho rows to start, got %d", before)
	}

	s.Send(sim.CtrlS)
	s.Send(sim.Type("thunder"))
	s.Send(sim.Enter)

	s.HidesText("Ancho Chile")
	s.ShowsText("Thunderbolt")
	// The count says what it did AND what it is a fraction of.
	s.ShowsText("of 4")
	// And the filter is visible while it is in force.
	s.ShowsText("thunder")

	// esc in the LIST clears it -- a different escape from the one that closes
	// the input line.
	s.Send(sim.Esc)
	s.ShowsText("Ancho Chile")
	s.HidesText("thunder")
}

// A facet restricts a field. It must not reach the other columns, or `loc:` is
// just a longer way of typing text.
func TestAFacetRestrictsItsFieldOnly(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("4"))

	s.Send(sim.CtrlS)
	s.Send(sim.Type("loc:garage"))
	s.Send(sim.Enter)
	s.ShowsText("of 4")
	if got := strings.Count(s.PlainView(), "Ancho Chile"); got != 1 {
		t.Errorf("loc:garage matched %d Ancho rows, want the 1 in the Garage", got)
	}

	// Facet and text together, one line.
	s.Send(sim.Esc)
	s.Send(sim.CtrlS)
	s.Send(sim.Type("loc:tray thunder"))
	s.Send(sim.Enter)
	s.ShowsText("Thunderbolt")
	s.HidesText("Ancho Chile")
}

// Filtering and jumping look different before a word has been read.
func TestFilterAndJumpAreVisiblyDifferent(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("4"))

	s.Send(sim.CtrlS)
	s.Send(sim.Type("shelf"))
	filtering := s.PlainView()

	s.Send(sim.Esc)
	s.Send(sim.AltG)
	s.Send(sim.Type("shelf"))
	jumping := s.PlainView()

	if filtering == jumping {
		t.Fatal("filtering and jumping render identically")
	}
	if !strings.Contains(jumping, "JUMP") {
		t.Errorf("the jump does not say what it is:\n%s", jumping)
	}
	// A jump result says what KIND of thing it is, or Enter lands somewhere
	// surprising.
	if !strings.Contains(jumping, "Location") {
		t.Errorf("jump results do not say their kind:\n%s", jumping)
	}
}

// The jump goes THERE: the right view, cursor on the right row.
func TestJumpingGoesToTheThing(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("4")) // start in Holdings

	s.Send(sim.AltG)
	s.Send(sim.Type("blue crate"))
	s.Send(sim.Enter)

	// A Location, so it lands in the Locations tree with the cursor on it.
	s.ShowsText("Locations")
	cursor := ""
	for _, line := range strings.Split(s.PlainView(), "\n") {
		if strings.HasPrefix(line, ">") {
			cursor = line
		}
	}
	if !strings.Contains(cursor, "Blue Crate") {
		t.Errorf("the jump landed on %q, want Blue Crate", cursor)
	}
}

// Escape is never destructive: cancelling a jump leaves you exactly where you
// were, with any filter still applied.
func TestCancellingAJumpChangesNothing(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("4"))
	s.Send(sim.CtrlS)
	s.Send(sim.Type("ancho"))
	s.Send(sim.Enter)
	before := s.PlainView()

	s.Send(sim.AltG)
	s.Send(sim.Type("garage"))
	s.Send(sim.Esc)

	if after := s.PlainView(); after != before {
		t.Errorf("cancelling a jump changed the screen:\n before:\n%s\n after:\n%s", before, after)
	}
}

// While the line is open a keystroke is a CHARACTER, and that is the classic
// way a modal interface betrays the person using it: the `q` in a search for
// "quinoa" quitting, or the `2` in "2mm" changing the view.
func TestTypingIsNotNavigation(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("4"))
	s.Send(sim.CtrlS)
	s.Send(sim.Type("jar"))

	if !strings.Contains(s.PlainView(), "I-search: jar") {
		t.Errorf("typing jar did not reach the input line:\n%s", s.PlainView())
	}
	// And q did not quit, and 2 did not change view.
	s.Send(sim.Type("q2"))
	if !strings.Contains(s.PlainView(), "I-search: jarq2") {
		t.Errorf("q and 2 were taken as commands while typing:\n%s", s.PlainView())
	}
}

// A tree filter keeps the ancestors of a match, because what a tree adds over a
// list is where the thing sits.
func TestFilteringATreeKeepsThePath(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("2"))
	s.Send(sim.CtrlS)
	s.Send(sim.Type("small parts"))
	s.Send(sim.Enter)

	for _, ancestor := range []string{"Garage", "Metal Shelving Unit", "Bay 3", "Blue Crate"} {
		s.ShowsText(ancestor)
	}
	s.ShowsText("Small Parts Tray")
	s.HidesText("Left Pantry")
}

// Searching is a read.
func TestSearchingWritesNothing(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	before := s.CountHoldings()

	for _, view := range []string{"1", "2", "3", "4"} {
		s.Send(sim.Press(view))
		s.Send(sim.CtrlS)
		s.Send(sim.Type("a"))
		s.Send(sim.Enter)
		s.Send(sim.AltN, sim.AltP)
		s.Send(sim.Esc)
		s.Send(sim.AltG)
		s.Send(sim.Type("shelf"))
		s.Send(sim.Esc)
	}
	if after := s.CountHoldings(); after != before {
		t.Errorf("searching changed the holdings from %d to %d", before, after)
	}
}

// The omnibox costs a line, and everything still has to fit.
func TestSearchFitsNarrowTerminals(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	for _, width := range []int{60, 80, 120} {
		s.Resize(width, 24)
		s.Send(sim.Press("4"), sim.CtrlS)
		s.Send(sim.Type("loc:garage ancho"))
		s.FitsWidth(width)
		s.Send(sim.Enter)
		s.FitsWidth(width)
		s.Send(sim.AltG)
		s.Send(sim.Type("shelf"))
		s.FitsWidth(width)
		s.Send(sim.Esc, sim.Esc)
	}
}

// TestAbandoningAFilterEditRestoresTheOldOne is the bug a rendered frame found:
// rows narrow as you type, so an abandoned edit was leaving the half-typed
// filter on the table. It showed as "0 of 12 holdings" underneath a jump
// palette, long after the filter that produced it had been cancelled.
func TestAbandoningAFilterEditRestoresTheOldOne(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("4"))

	s.Send(sim.CtrlS)
	s.Send(sim.Type("ancho"))
	s.Send(sim.Enter)
	s.ShowsText("3 of 4")

	// Start refining, change your mind.
	s.Send(sim.CtrlS)
	s.Send(sim.Type("zzzz"))
	s.HidesText("Ancho Chile") // narrowing as you type
	s.Send(sim.Esc)

	s.ShowsText("3 of 4")
	s.ShowsText("Ancho Chile")
	s.ShowsText("ancho")
}

// With no filter to restore, abandoning an edit leaves the list whole.
func TestAbandoningAFirstFilterLeavesEverything(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("4"))

	s.Send(sim.CtrlS)
	s.Send(sim.Type("zzzz"))
	s.Send(sim.Esc)

	s.ShowsText("Ancho Chile")
	s.HidesText(" of 4")
}

// While the palette is up it is what the person is looking at, so the footer
// describes IT. Counting the list underneath describes a screen nobody reads.
func TestTheFooterDescribesThePaletteWhileJumping(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("4"))
	s.Send(sim.AltG)
	s.Send(sim.Type("shelf"))

	s.ShowsText("matches across every kind")
	s.HidesText("holdings -")
}
