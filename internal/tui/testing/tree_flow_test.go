package testing_test

import (
	"strings"
	"testing"

	sim "home-management-system/internal/tui/testing"
)

// 10b through the Simulator: the trees against a real house, with the five-deep
// path in it. A tree that reads well at depth one proves nothing.

func TestTheLocationTreeShowsTheShapeOfTheHouse(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("2"))

	for _, want := range []string{"Garage", "Metal Shelving Unit", "Bay 3", "Blue Crate", "Small Parts Tray"} {
		s.ShowsText(want)
	}
	// Indentation increases with depth, which is the whole claim of a tree.
	lines := treeLines(s)
	garage := indentOf(lines, "Garage")
	tray := indentOf(lines, "Small Parts Tray")
	if tray <= garage {
		t.Errorf("Small Parts Tray is indented %d and Garage %d; the depth is not visible", tray, garage)
	}
}

func TestFoldingHidesASubtreeInTheRealTree(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("2"))

	// Down to Garage, then fold it.
	moveTo(t, s, "Garage")
	s.Send(sim.Press("z"), sim.Press("a"))

	s.HidesText("Small Parts Tray")
	s.ShowsText("Garage")
	if !strings.Contains(cursorLine(s), "Garage") {
		t.Errorf("folding moved the cursor off Garage: %q", cursorLine(s))
	}

	s.Send(sim.Press("z"), sim.Press("R"))
	s.ShowsText("Small Parts Tray")
}

// Browsing a tree is a read, and the Simulator's VerifyAll cleanup would catch
// a write -- but a count says what happened rather than that something did.
func TestBrowsingTheTreesWritesNothing(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	before := s.CountHoldings()

	for _, view := range []string{"1", "2"} {
		s.Send(sim.Press(view))
		s.Send(sim.Press("j"), sim.Press("j"), sim.Press("l"), sim.Press("h"))
		s.Send(sim.Press("z"), sim.Press("M"))
		s.Send(sim.Press("z"), sim.Press("R"))
		s.Send(sim.Press("z"), sim.Press("a"))
		s.Send(sim.Press("s"), sim.Space, sim.Esc, sim.Press("G"))
	}
	if after := s.CountHoldings(); after != before {
		t.Errorf("browsing the trees changed the holdings from %d to %d", before, after)
	}
}

// Every view still fits every width now that two of them are trees, and deep
// indentation is the thing most likely to push a row over the edge.
func TestTheTreesFitNarrowTerminals(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	for _, width := range []int{60, 80, 120, 200} {
		s.Resize(width, 30)
		for _, view := range []string{"1", "2"} {
			s.Send(sim.Press(view))
			s.FitsWidth(width)
			s.Send(sim.Press("z"), sim.Press("R"))
			s.FitsWidth(width)
		}
	}
}

func treeLines(s *sim.Simulator) []string {
	return strings.Split(s.PlainView(), "\n")
}

func indentOf(lines []string, name string) int {
	for _, line := range lines {
		if !strings.Contains(line, name) {
			continue
		}
		// Past the two-column gutter, count the leading spaces.
		body := line[2:]
		return len(body) - len(strings.TrimLeft(body, " "))
	}
	return -1
}

// nameOn reads the node name out of a rendered tree row: past the gutter, past
// the indentation and the fold marker, and stopping before the count column.
func nameOn(line string) string {
	if len(line) < 2 {
		return ""
	}
	body := strings.TrimLeft(line[2:], " ")
	body = strings.TrimLeft(body, "\u25be\u25b8 ")
	if cut := strings.Index(body, "  "); cut > 0 {
		body = body[:cut]
	}
	return strings.TrimSpace(body)
}

func cursorLine(s *sim.Simulator) string {
	for _, line := range strings.Split(s.PlainView(), "\n") {
		if strings.HasPrefix(line, ">") {
			return line
		}
	}
	return ""
}

// moveTo walks the cursor down to a named row, and gives up rather than
// looping forever when it is not there.
//
// An unbounded search in a test is a test that hangs instead of failing, and a
// hang says nothing about what went wrong.
func moveTo(t *testing.T, s *sim.Simulator, name string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if strings.Contains(cursorLine(s), name) {
			return
		}
		s.Send(sim.Press("j"))
	}
	t.Fatalf("never reached a row containing %q; the screen is:\n%s", name, s.PlainView())
}
