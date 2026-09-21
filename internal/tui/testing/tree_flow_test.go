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
	s.ByPlace()
	s.OnRail()
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
	s.ByPlace()
	s.OnRail()
	// Down to Garage, then fold it.
	moveTo(t, s, "Garage")
	s.Send(sim.Tab)

	// The RAIL, not the screen: folding a branch away leaves the tray named
	// in the contents pane's WHERE column, which is correct.
	s.RailHides("Small Parts Tray")
	s.RailShows("Garage")
	if !strings.Contains(cursorLine(s), "Garage") {
		t.Errorf("folding moved the cursor off Garage: %q", cursorLine(s))
	}

	s.Send(sim.ShiftTab)
	s.RailShows("Small Parts Tray")
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
		s.Send(sim.Press("s"), sim.CtrlSpace, sim.Esc, sim.AltGreat)
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
		if hasCursor(line) {
			return line
		}
	}
	return ""
}

// hasCursor finds the mark wherever the shell put it: at the start of a line
// in the rail, and just after the rule in the contents pane.
func hasCursor(line string) bool {
	return strings.HasPrefix(line, ">") || strings.Contains(line, "│>")
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

// What a node contains is in the pane beside it, always, rather than spliced
// into the tree when asked.
//
// The toggle it replaces had to exist because there was nowhere else to show
// contents. Its cost was a tree of two kinds of row -- see
// TestTheRailHoldsOnlyStructure for what that made possible.
func TestTheContentsPaneShowsWhatIsInTheNode(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.ByPlace()
	s.OnRail()

	// At the top of the house, everything; and the rail is still only places.
	s.ContentsShow("Ancho Chile")
	s.RailHides("Ancho Chile")
	s.RailShows("Garage")

	// Point at one place and the pane follows, without a keystroke that means
	// "now show me contents".
	//
	// GoTo rather than moveTo: a rendered line spans both panes, so matching
	// on it finds "Left Pantry" in the contents pane's WHERE column before
	// the rail has gone anywhere.
	s.GoTo("Left Pantry")
	s.ContentsShow("Ancho Chile")
	if got := s.CountRows("Ancho Chile"); got != 1 {
		t.Errorf("the Left Pantry holds 1 pile of chile, the pane shows %d", got)
	}
}

// v swaps the rollup for what is filed at the node itself, which is how
// something filed too coarsely becomes findable.
func TestTheDepthToggleSwapsRollupForHereOnly(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.ByPlace()
	s.GoTo("Garage")

	// Deep: the Garage's own jar and the one on the tray beneath it.
	if got := s.CountRows("Ancho Chile"); got != 2 {
		t.Fatalf("the Garage rolls up 2 piles of chile, got %d", got)
	}

	s.Send(sim.Press("v"))
	s.ShowsText("filed here")
	if got := s.CountRows("Ancho Chile"); got != 1 {
		t.Errorf("only one pile is filed at the Garage itself, got %d", got)
	}

	s.Send(sim.Press("v"))
	if got := s.CountRows("Ancho Chile"); got != 2 {
		t.Errorf("the rollup did not come back, got %d", got)
	}
}

// The rail is structure and nothing else, in both lenses.
//
// This is what three separate guards used to be for. A tree that showed its
// contents held two kinds of row, and every verb had to ask which it was on:
// a put read a row's identifier as a LocationID, so a holding row taken for a
// place was a silent write to whatever shelf shared that number. Renaming
// needed its own refusal, folding needed kind-tagged keys, and the cursor had
// to skip the jars while something was in hand.
//
// None of those questions exist now, so the assertion is the absence.
func TestTheRailHoldsOnlyStructure(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	for _, tc := range []struct {
		lens     func()
		contains string
		absent   []string
	}{
		{s.ByPlace, "Garage", []string{"Ancho Chile", "Thunderbolt"}},
		{s.ByKind, "Spices", []string{"Ancho Chile", "Thunderbolt"}},
	} {
		tc.lens()
		s.OnRail()
		s.RailShows(tc.contains)
		for _, gone := range tc.absent {
			s.RailHides(gone)
		}
	}
}

// Every destination offered is somewhere a thing can actually go.
//
// Five tests used to live around here, all about one decision: the tree
// showed what its nodes contained, so half its rows were places and half were
// not, and every verb needed its own guard. The dangerous one was the put --
// it read a row's identifier as a LocationID, so a holding row taken for a
// place was a silent write to whatever shelf shared that number.
//
// The contents pane took the contents and the move drawer took the
// destinations, so the question dissolved twice over rather than being
// answered better. What is left to assert is that it cannot come back.
func TestEveryDestinationIsAPlace(t *testing.T) {
	s := sim.New(t)
	rice, _ := stockedTree(t, s)

	before := s.CountHoldings()
	s.ByPlace()
	s.OnContents()
	s.Send(sim.Press("m"))
	s.ShowsText("MOVE")

	// Nothing in the list is a thing rather than a place.
	for _, thing := range []string{"Basmati Rice", "500 g"} {
		if strings.Contains(destinations(s), thing) {
			t.Errorf("%q was offered as a destination:\n%s", thing, s.PlainView())
		}
	}
	// And walking it writes nothing.
	s.Send(sim.CtrlN, sim.CtrlN, sim.Esc)
	if after := s.CountHoldings(); after != before {
		t.Errorf("walking the destinations changed the holdings from %d to %d", before, after)
	}
	s.OnHand(rice, 500*domain.Scale)
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

// A fold survives the reload the depth toggle causes.
//
// The tree is rebuilt from scratch after every write, every refresh and every
// toggle, and it used to arrive unfolded each time. Any reload will do to
// catch that; the depth toggle is the one a person presses most.
func TestTheDepthToggleDoesNotForceAnythingOpen(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.ByPlace()
	s.GoTo("Garage")

	// Shut the Garage: its own row stays, its descendants go.
	s.Send(sim.Tab)
	s.RailShows("Garage")
	s.RailHides("Metal Shelving Unit")

	s.Send(sim.Press("v"))
	s.RailHides("Metal Shelving Unit")
	s.Send(sim.Press("v"))
	s.RailHides("Metal Shelving Unit")

	// And the fold is still a fold, not a thing that stuck.
	s.Send(sim.Tab)
	s.RailShows("Metal Shelving Unit")
}

// The same for any other reload, which is what the toggle rides on.
func TestFoldsSurviveAWrite(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.ByPlace()
	s.OnRail()
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
	s.ByPlace()
	s.OnRail()
	s.Send(sim.Tab)
	s.HidesText("Metal Shelving Unit")

	s.ByKind()
	s.OnRail()
	s.ShowsText("Spices") // nothing in the Categories tree was folded

	s.ByPlace()
	s.OnRail()
	s.HidesText("Metal Shelving Unit") // and the Locations tree kept its own
}

// ---------------------------------------------------------------------------
// Moving a thing
// ---------------------------------------------------------------------------

// One key, one screen, whatever the thing is.
//
// It used to be two gestures and neither was good. Pointing meant M-w here, a
// tab switch, a walk down a tree, C-y there -- with the thing you were moving
// off screen for the middle of it. Naming meant knowing the name. And a PLACE
// could be moved by neither: re-parenting a shelf was a typed command, for
// the same idea.
//
// Three subtests rather than three tests, because the claim is that they are
// ONE gesture: if they need different setup or different keys, they are not.
func TestMoveIsOneGestureForEveryKind(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lens  func(*sim.Simulator)
		where string
		check func(*testing.T, *sim.Simulator, domain.ItemID, domain.LocationID)
	}{
		{
			name:  "a holding goes to a place",
			lens:  func(s *sim.Simulator) { s.ByPlace(); s.OnContents() },
			where: "Kitchen",
			check: func(t *testing.T, s *sim.Simulator, rice domain.ItemID, pantry domain.LocationID) {
				s.OnHand(rice, 500*domain.Scale) // moved, not consumed
				if held := holdingsIn(t, s, pantry); held != 0 {
					t.Errorf("%d holdings left in the pantry, want 0", held)
				}
			},
		},
		{
			name:  "an item is filed under a classification",
			lens:  func(s *sim.Simulator) { s.ByKind(); s.OnContents() },
			where: "Grains",
			check: func(t *testing.T, s *sim.Simulator, rice domain.ItemID, _ domain.LocationID) {
				if got, want := categoryOf(t, s, rice), s.HasCategory("Grains"); got != want {
					t.Errorf("the rice is filed under %d, want Grains (%d)", got, want)
				}
			},
		},
		{
			name:  "a place goes inside another place",
			lens:  func(s *sim.Simulator) { s.ByPlace(); s.GoTo("Left Pantry") },
			where: "Attic",
			check: func(t *testing.T, s *sim.Simulator, _ domain.ItemID, pantry domain.LocationID) {
				parent := placeUnder(t, s, pantry)
				if parent == nil || *parent != s.HasLocation("Attic") {
					t.Errorf("the pantry did not move into the Attic")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := sim.New(t)
			rice, pantry := stockedTree(t, s)
			p, ctx := s.Planner(), s.Context()
			s.Apply(p.NewCategory(ctx, ops.NewCategoryRequest{Name: "Grains"}))
			s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Attic"}))

			tc.lens(s)
			s.Send(sim.Press("m"))
			s.ShowsText("MOVE")
			s.Send(sim.Type(tc.where))
			s.Send(sim.Enter)

			tc.check(t, s, rice, pantry)
		})
	}
}

// Typing narrows the destinations by their whole path, which is the naming
// half of the gesture -- without being a different screen from the pointing
// half, which is what the old prompt was.
func TestMovingNarrowsByTyping(t *testing.T) {
	s := sim.New(t)
	stockedTree(t, s)
	s.ByPlace()
	s.OnContents()

	s.Send(sim.Press("m"))
	s.ShowsText("Kitchen")
	s.ShowsText("Kitchen > Left Pantry")

	s.Send(sim.Type("pantry"))
	s.ShowsText("Kitchen > Left Pantry")
	// The parent no longer matches on its own.
	if strings.Contains(s.PlainView(), "\n> Kitchen \n") {
		t.Errorf("the filter left a row it does not match:\n%s", s.PlainView())
	}
}

// A place cannot go inside itself or anything under it.
//
// Ruled out of the list rather than refused after the fact: the tree would
// stop being a tree, and a person should not be able to aim at an answer the
// cycle guard is going to reject.
func TestAPlaceCannotBeMovedIntoItself(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.ByPlace()
	s.GoTo("Garage")

	s.Send(sim.Press("m"))
	s.ShowsText("MOVE")
	for _, own := range []string{"Metal Shelving Unit", "Bay 3", "Blue Crate", "Small Parts Tray"} {
		if strings.Contains(destinations(s), own) {
			t.Errorf("the Garage was offered %q, which is inside it:\n%s", own, s.PlainView())
		}
	}
	// And somewhere it CAN go is still there.
	if !strings.Contains(destinations(s), "Left Pantry") {
		t.Errorf("no destination outside the Garage was offered:\n%s", s.PlainView())
	}
}

// A selection moves together, in one transaction.
//
// Two DIFFERENT items, because two holdings of one item arriving in one place
// would merge -- which the planner refuses, correctly, and which would make
// this a test of that refusal rather than of the gesture.
func TestMovingManyRowsIsOneGesture(t *testing.T) {
	s := sim.New(t)
	_, pantry := stockedTree(t, s)
	p, ctx := s.Planner(), s.Context()
	s.Apply(p.NewItem(ctx, ops.NewItemRequest{
		Name: "Cumin", Category: s.HasCategory("Pantry"),
		Counting: ops.CountingMeasured, ContentUnit: "g",
	}))
	s.Apply(p.Receive(ctx, ops.ReceiveRequest{
		Item: s.HasItem("Cumin"), Location: pantry, Basis: domain.BasisContent,
		Amount: domain.FromMilli(50 * domain.Scale),
	}))
	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Attic"}))
	attic := s.HasLocation("Attic")

	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlSpace)
	s.Send(sim.CtrlN)
	s.Send(sim.CtrlSpace)
	s.ShowsText("2 selected")

	s.Send(sim.Press("m"))
	s.ShowsText("2 holdings")
	s.Send(sim.Type("Attic"))
	s.Send(sim.Enter)

	if held := holdingsIn(t, s, attic); held != 2 {
		t.Errorf("the attic holds %d, want both of the moved rows", held)
	}
	if held := holdingsIn(t, s, pantry); held != 0 {
		t.Errorf("%d holdings left in the pantry, want none", held)
	}
}

func TestEscapingAMoveChangesNothing(t *testing.T) {
	s := sim.New(t)
	rice, pantry := stockedTree(t, s)
	s.ByPlace()
	s.OnContents()

	s.Send(sim.Press("m"))
	s.Send(sim.Type("Kitchen"))
	s.Send(sim.Esc)

	s.HidesText("MOVE")
	s.OnHand(rice, 500*domain.Scale)
	if held := holdingsIn(t, s, pantry); held != 1 {
		t.Errorf("%d holdings in the pantry, want the 1 that was never moved", held)
	}
}

// destinations is the move drawer's own rows: everything between its heading
// and the rule that ends it.
//
// Bounded at both ends on purpose. Taking everything after the heading swept
// up the inspector line below the drawer, which names the row being moved --
// so the test read "Basmati Rice was offered as a destination" about a line
// that was describing what was in hand.
func destinations(s *sim.Simulator) string {
	_, after, found := strings.Cut(s.PlainView(), "MOVE  ")
	if !found {
		return ""
	}
	// Past the heading's own line, which names the thing being carried, and
	// stopping at the rule below the rows.
	_, rows, found := strings.Cut(after, "\n")
	if !found {
		return ""
	}
	rows, _, _ = strings.Cut(rows, "\n---")
	return rows
}

// placeUnder is where a location sits. Named apart from parentOf, which is
// the categories' one.
func placeUnder(t *testing.T, s *sim.Simulator, id domain.LocationID) *domain.LocationID {
	t.Helper()
	loc, err := s.Reader().Location(s.Context(), id)
	if err != nil {
		t.Fatalf("location %d: %v", id, err)
	}
	return loc.Parent
}

func holdingsIn(t *testing.T, s *sim.Simulator, location domain.LocationID) int {
	t.Helper()
	held, err := s.Reader().Holdings(s.Context())
	if err != nil {
		t.Fatalf("holdings: %v", err)
	}
	n := 0
	for _, detail := range held {
		if detail.Holding.Base().StowedLocation == location {
			n++
		}
	}
	return n
}

func categoryOf(t *testing.T, s *sim.Simulator, item domain.ItemID) domain.CategoryID {
	t.Helper()
	items, err := s.Reader().Items(s.Context())
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	for _, it := range items {
		if it.Base().ID == item {
			return it.Base().Category
		}
	}
	t.Fatalf("no item %d", item)
	return 0
}

// locationOf is where a holding of an item is stowed.
func locationOf(t *testing.T, s *sim.Simulator, item domain.ItemID) domain.LocationID {
	t.Helper()
	rows, err := s.Reader().Holdings(s.Context())
	if err != nil {
		t.Fatalf("holdings: %v", err)
	}
	for _, h := range rows {
		if h.Holding.Base().Item == item {
			return h.Holding.Base().StowedLocation
		}
	}
	t.Fatalf("no holding of item %d", item)
	return 0
}
