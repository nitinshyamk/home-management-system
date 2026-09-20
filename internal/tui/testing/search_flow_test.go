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
	s.ByPlace()
	s.OnContents()
	if before := s.CountRows("Ancho Chile"); before != 3 {
		t.Fatalf("expected 3 Ancho rows to start, got %d", before)
	}

	s.Send(sim.CtrlS)
	s.Send(sim.Type("thunder"))
	s.Send(sim.Enter)

	if got := s.CountRows("Ancho Chile"); got != 0 {
		t.Errorf("the filter left %d Ancho rows, want none", got)
	}
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
	s.ByPlace()
	s.OnContents()
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
	s.ByPlace()
	s.OnContents()
	// A word this house actually contains, so the palette has rows to say the
	// kind of. It used to be "shelf", which matches nothing here -- the
	// assertion below passed on the tab bar's "2 Locations" instead, and only
	// stopped passing when the tabs went.
	s.Send(sim.CtrlS)
	s.Send(sim.Type("crate"))
	filtering := s.PlainView()

	s.Send(sim.Esc)
	s.Send(sim.AltG)
	s.Send(sim.Type("crate"))
	jumping := s.PlainView()

	if filtering == jumping {
		t.Fatal("filtering and jumping render identically")
	}
	if !strings.Contains(jumping, "JUMP") {
		t.Errorf("the jump does not say what it is:\n%s", jumping)
	}
	if !strings.Contains(jumping, "Blue Crate") {
		t.Fatalf("the jump found nothing to say the kind of:\n%s", jumping)
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
	s.ByPlace()
	s.OnContents()

	s.Send(sim.AltG)
	s.Send(sim.Type("blue crate"))
	s.Send(sim.Enter)

	// A Location, so it lands on the rail of the place lens with the cursor
	// on it -- and unfolds the branch to get there, since the crate is four
	// levels down.
	s.ShowsText("BY PLACE")
	if !s.Model().OnRail() {
		t.Error("the jump landed in the contents pane, not on the place it named")
	}
	if got := s.Model().RailName(); got != "Blue Crate" {
		t.Errorf("the jump landed on %q, want Blue Crate", got)
	}
}

// Escape is never destructive: cancelling a jump leaves you exactly where you
// were, with any filter still applied.
func TestCancellingAJumpChangesNothing(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.ByPlace()
	s.OnContents()
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
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("jar"))

	if !typedInto(s, "I-search", "jar") {
		t.Errorf("typing jar did not reach the input line:\n%s", s.PlainView())
	}
	// And q did not quit, and 2 did not change view.
	s.Send(sim.Type("q2"))
	if !typedInto(s, "I-search", "jarq2") {
		t.Errorf("q and 2 were taken as commands while typing:\n%s", s.PlainView())
	}
}

// typedInto reports whether the text landed on the line with this prompt.
//
// It asks for the two on ONE line rather than for a spelling like
// "I-search: jar", because what the test is about is that the keystroke reached
// the input line rather than the table -- and the prompt's exact spacing is a
// layout decision the goldens already guard. Pinning it here too meant a
// behavioural test failing for a cosmetic reason.
func typedInto(s *sim.Simulator, prompt, text string) bool {
	for _, line := range strings.Split(s.PlainView(), "\n") {
		if strings.Contains(line, prompt) && strings.Contains(line, text) {
			return true
		}
	}
	return false
}

// The filter narrows the CONTENTS, and leaves the rail alone.
//
// The asymmetry is deliberate. Filtering a tree gives you a tree with holes
// in it; what somebody typing a name wants is the things that match, which is
// what the contents pane holds. Finding a PLACE by name is the jump palette's
// job -- it searches every kind and moves the rail -- so the two searches
// stay distinguishable instead of one key meaning different things depending
// on which half the cursor was in.
//
// tree.SetFilter still keeps a match's ancestors; see the tree package, which
// is where that behaviour is now reachable from.
func TestFilteringNarrowsTheContentsAndNotTheRail(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("thunder"))
	s.Send(sim.Enter)

	s.ContentsShow("Thunderbolt")
	if got := s.CountRows("Ancho Chile"); got != 0 {
		t.Errorf("the filter left %d chile rows in the contents, want none", got)
	}

	// The house is still the house: you have not lost your way around it by
	// searching within it.
	for _, place := range []string{"Garage", "Metal Shelving Unit", "Left Pantry"} {
		s.RailShows(place)
	}
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
		s.ByPlace()
		s.OnContents()
		s.Send(sim.CtrlS)
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
	s.ByPlace()
	s.OnContents()
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
	s.ByPlace()
	s.OnContents()
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
	s.ByPlace()
	s.OnContents()
	s.Send(sim.AltG)
	s.Send(sim.Type("shelf"))

	s.ShowsText("matches across every kind")
	s.HidesText("holdings -")
}
