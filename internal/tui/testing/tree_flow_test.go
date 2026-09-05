package testing_test

import (
	"strings"
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
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
	s.Send(sim.Tab)

	s.HidesText("Small Parts Tray")
	s.ShowsText("Garage")
	if !strings.Contains(cursorLine(s), "Garage") {
		t.Errorf("folding moved the cursor off Garage: %q", cursorLine(s))
	}

	s.Send(sim.ShiftTab)
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
		s.Send(sim.CtrlN, sim.CtrlN, sim.CtrlF, sim.CtrlB)
		s.Send(sim.ShiftTab)
		s.Send(sim.ShiftTab)
		s.Send(sim.Tab)
		s.Send(sim.Press("s"), sim.Space, sim.Esc, sim.AltGreat)
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
			s.Send(sim.ShiftTab)
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
		s.Send(sim.CtrlN)
	}
	t.Fatalf("never reached a row containing %q; the screen is:\n%s", name, s.PlainView())
}

// The trees show what their nodes contain, when asked.
func TestTheContentsToggleShowsWhatIsInside(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("2"))
	s.HidesText("Ancho Chile")

	s.Send(sim.Press("v"))
	s.ShowsText("Ancho Chile")
	// Its own measure, where a container shows its rollup.
	s.ShowsText("100 g")
	// And the count still counts PLACES. A footer that said 10 locations the
	// moment the holdings appeared would be answering a different question
	// with the same words.
	s.ShowsText("6 locations")

	s.Send(sim.Press("v"))
	s.HidesText("Ancho Chile")
}

// Categories show Items, Locations show Holdings, and each remembers its own
// answer -- wanting one is no reason to want the other.
func TestEachTreeRemembersItsOwnMode(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("2"), sim.Press("v"))
	s.ShowsText("Ancho Chile")

	s.Send(sim.Press("1"))
	s.HidesText("Ancho Chile") // the Categories tree was not asked

	s.Send(sim.Press("2"))
	s.ShowsText("Ancho Chile") // and the Locations tree still is
}

// A contained row is shown, and is a target for nothing.
//
// The dangerous one is the put: destination() reads a tree row's identifier as
// a LocationID, so a holding row taken for a place would move stock to whatever
// shelf happens to share that number -- a silent write to the wrong place,
// which is the worst kind of wrong this interface can be.
func TestAContainedRowIsNotSomewhereToPut(t *testing.T) {
	s := sim.New(t)
	rice, _ := stockedTree(t, s)

	s.Send(sim.Press("4"), sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)
	s.Send(sim.AltW)
	s.ShowsText("copied")

	// Onto a holding row in the Locations tree, and put.
	s.Send(sim.Press("2"), sim.Press("v"))
	s.Send(sim.Esc)
	before := s.CountHoldings()
	for i := 0; i < 4; i++ {
		s.Send(sim.CtrlN)
	}
	s.Send(sim.CtrlY)

	s.ShowsText("place")
	if after := s.CountHoldings(); after != before {
		t.Errorf("the put changed the holdings from %d to %d", before, after)
	}
	s.OnHand(rice, 500*domain.Scale)
}

// And renaming one says so, in terms of the thing.
func TestRenamingAContainedRowIsRefused(t *testing.T) {
	s := sim.New(t)
	stockedTree(t, s)
	s.Send(sim.Press("1"), sim.Press("v"))

	// Down onto an item under its category.
	s.Send(sim.CtrlN)
	s.Send(sim.Press("e"))
	s.ShowsText("the tree is showing you")
	s.HidesText("rename ─")
}

// A Category and an Item numbered the same fold and select independently.
//
// They are numbered from different tables, and the identifier used to be the
// fold key, the selection key and the identity the cursor is restored by --
// so Category 7 and Item 7 shared all three.
func TestContainedRowsDoNotCollideWithTheirContainers(t *testing.T) {
	s := sim.New(t)
	stockedTree(t, s)
	s.Send(sim.Press("1"), sim.Press("v"))

	// Fold the first category. Its items go; nothing else does.
	s.Send(sim.Tab)
	s.HidesText("Basmati Rice")
	s.Send(sim.Tab)
	s.ShowsText("Basmati Rice")
}

// stockedTree is a house with one category, one item and two holdings, which is
// enough for a tree to have contents worth showing.
func stockedTree(t *testing.T, s *sim.Simulator) (rice domain.ItemID, pantry domain.LocationID) {
	t.Helper()
	p, ctx := s.Planner(), s.Context()

	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Kitchen"}))
	kitchen := s.HasLocation("Kitchen")
	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Left Pantry", Parent: &kitchen}))
	pantry = s.HasLocation("Left Pantry")

	s.Apply(p.NewCategory(ctx, ops.NewCategoryRequest{Name: "Pantry"}))
	category := s.HasCategory("Pantry")
	s.Apply(p.NewItem(ctx, ops.NewItemRequest{
		Name: "Basmati Rice", Category: category,
		Counting: ops.CountingMeasured, ContentUnit: "g",
	}))
	rice = s.HasItem("Basmati Rice")
	s.Apply(p.Receive(ctx, ops.ReceiveRequest{
		Item: rice, Location: pantry, Basis: domain.BasisContent,
		Amount: domain.FromMilli(500 * domain.Scale), Source: "shop",
	}))
	return rice, pantry
}

// Folds survive a reload, so showing contents does not force anything open.
//
// The tree is rebuilt from scratch after every write, every refresh and every
// toggle, and it used to arrive unfolded each time. That was invisible until
// the trees learned to show what they contain: asking for the contents reloaded
// the tree, the reload discarded the folds, and a house somebody had carefully
// collapsed sprang open with every holding in it.
func TestShowingContentsDoesNotForceAnythingOpen(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("2"))

	// The cursor starts on the Garage. Shut it: its own row stays, its
	// descendants go.
	s.Send(sim.Tab)
	s.ShowsText("Garage")
	s.HidesText("Metal Shelving Unit")

	// Contents on: what is open shows what it holds, what is shut stays shut.
	s.Send(sim.Press("v"))
	s.HidesText("Metal Shelving Unit")
	s.HidesText("Thunderbolt") // the holding five levels inside it
	s.ShowsText("Ancho Chile") // but the Left Pantry's, which is not folded

	// And the fold is still there afterwards.
	s.Send(sim.Tab)
	s.ShowsText("Metal Shelving Unit")
}

// The same for any other reload, which is what the toggle rides on.
func TestFoldsSurviveAWrite(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("2"))
	s.Send(sim.Tab)
	s.HidesText("Metal Shelving Unit")

	// Create a location, which reloads the view.
	s.Send(sim.Press("o"))
	s.Send(sim.Type("Cellar"))
	s.Send(sim.Enter)
	s.Send(sim.Enter)

	s.ShowsText("Cellar")
	s.HidesText("Metal Shelving Unit")
}

// Folds belong to the tree that has them. Carrying them across would fold
// whatever happened to share a number in the other one.
func TestFoldsDoNotCrossBetweenTrees(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("2"))
	s.Send(sim.Tab)
	s.HidesText("Metal Shelving Unit")

	s.Send(sim.Press("1"))
	s.ShowsText("Spices") // nothing in the Categories tree was folded

	s.Send(sim.Press("2"))
	s.HidesText("Metal Shelving Unit") // and the Locations tree kept its own
}
