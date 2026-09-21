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

// Every row of the rail is somewhere a thing can go.
//
// Four tests used to live here, and all four were about one decision: the
// tree showed what its nodes contained, so half its rows were places and half
// were not. Each verb needed its own guard. The put was the dangerous one --
// destination() reads a row's identifier as a LocationID, so a holding row
// taken for a place was a silent write to whatever shelf shared that number.
// Rename needed a refusal of its own, folding needed kind-tagged keys, and
// while something was in hand the cursor had to skip the jars, because a
// shelf with nine jars on it was nine rows the put was going to refuse.
//
// The contents pane took the contents, so the question dissolved rather than
// being answered better. What is left to assert is that it cannot come back.
func TestEveryRailRowIsSomewhereAThingCanGo(t *testing.T) {
	s := sim.New(t)
	rice, _ := stockedTree(t, s)

	s.ByPlace()
	s.OnContents()
	s.Send(sim.AltW)
	s.ShowsText("carrying")

	// Walk the whole rail with something in hand. Every row it stops on is a
	// place, so none of them is a row the put would have to refuse.
	s.OnRail()
	before := s.CountHoldings()
	for range 6 {
		s.Send(sim.CtrlN)
		if line := cursorLine(s); strings.Contains(line, "Basmati Rice") {
			t.Fatalf("the rail stopped on a holding: %q", line)
		}
	}
	if after := s.CountHoldings(); after != before {
		t.Errorf("walking the rail changed the holdings from %d to %d", before, after)
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

// The trees show what they hold, so the thing you most want to do to something
// you can see in the wrong place is put it in the right one.
//
// Two commands behind one gesture, because they are two events: a Holding MOVES
// to a place, an Item is RECLASSIFIED under a classification. The tree decides
// which by what the row IS, not by which tree it is in -- reading the view would
// be reading it twice and getting the answer from the wrong one the day a tree
// shows something else.
func TestCopyAndPutMovesAHoldingInTheLocationsTree(t *testing.T) {
	s := sim.New(t)
	rice, pantry := stockedTree(t, s)

	// Pick up in the contents pane, put down on the rail: the gesture spans
	// the two halves now rather than two tabs.
	s.ByPlace()
	s.OnContents()
	s.Send(sim.AltW)
	s.ShowsText("carrying")

	s.GoTo("Kitchen")
	s.Send(sim.CtrlY)

	s.OnHand(rice, 500*domain.Scale) // moved, not consumed
	if held := holdingsIn(t, s, pantry); held != 0 {
		t.Errorf("%d holdings left in the pantry, want 0", held)
	}
}

func TestCopyAndPutReclassifiesAnItemInTheCategoriesTree(t *testing.T) {
	s := sim.New(t)
	rice, _ := stockedTree(t, s)
	s.Apply(s.Planner().NewCategory(s.Context(), ops.NewCategoryRequest{Name: "Grains"}))
	grains := s.HasCategory("Grains")

	s.ByKind()
	s.OnContents()
	s.ShowsText("Basmati Rice")
	s.Send(sim.AltW)
	s.ShowsText("carrying")

	s.GoTo("Grains")
	s.Send(sim.CtrlY)

	if got := categoryOf(t, s, rice); got != grains {
		t.Errorf("the rice is filed under %d, want Grains (%d)", got, grains)
	}
}

// The prompt is the other half: for when you already know where it goes.
func TestThePromptMovesAThing(t *testing.T) {
	s := sim.New(t)
	rice, pantry := stockedTree(t, s)
	s.ByPlace()
	s.OnContents()

	s.Send(sim.Press("m"))
	s.ShowsText("where to")
	s.Send(sim.Type("Kitchen"))
	s.Send(sim.Enter)

	s.OnHand(rice, 500*domain.Scale)
	if held := holdingsIn(t, s, pantry); held != 0 {
		t.Errorf("%d holdings left in the pantry, want 0", held)
	}
}

// And it says what it is doing in the thing's own words: stock moves, a kind of
// thing is filed.
func TestThePromptAsksTheRightQuestionForTheKind(t *testing.T) {
	s := sim.New(t)
	stockedTree(t, s)

	s.ByKind()
	s.OnContents()
	s.Send(sim.Press("m"))
	s.ShowsText("file it under")
	s.HidesText("where to")
}

// The two mismatches are refused in terms of the things. Putting stock into a
// classification is not a near miss to be coerced: a classification is not
// anywhere.
func TestPuttingAThingWhereItCannotGoIsRefused(t *testing.T) {
	s := sim.New(t)
	rice, pantry := stockedTree(t, s)

	s.ByPlace()
	s.OnContents()
	s.Send(sim.AltW)

	s.ByKind()
	s.OnRail()
	s.Send(sim.CtrlY)
	s.ShowsText("is stock")

	// And nothing happened.
	s.OnHand(rice, 500*domain.Scale)
	if held := holdingsIn(t, s, pantry); held != 1 {
		t.Errorf("%d holdings in the pantry, want the 1 that was never moved", held)
	}
}

// A container is not a thing to pick up. Moving a PLACE is a different command
// with different consequences -- everything inside it goes too -- so it is not
// something to reach by the same gesture as moving one jar.
func TestCopyingAContainerIsRefused(t *testing.T) {
	s := sim.New(t)
	stockedTree(t, s)
	s.ByPlace()
	s.OnRail()
	s.Send(sim.Press("v"))

	s.Send(sim.AltW)
	s.ShowsText("reparent location")
	s.HidesText("carrying")
}

// Both read through the query path rather than the screen, which is the point
// of the harness: keys go in the front and the assertion is on the far side.

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

// ---------------------------------------------------------------------------
// Carrying a thing to where it goes.
//
// The gesture already worked. What it did not do is SAY anything: carry() spoke
// through the status line, and every view change overwrites the status line --
// so the one keystroke the gesture needs in the middle destroyed the only
// evidence that anything was in hand. You picked a thing up, went to the tree,
// and the screen said nothing at all.

// The banner is the fix, and this is the case that was broken.
func TestWhatIsCarriedIsSaidInEveryView(t *testing.T) {
	s := sim.New(t)
	rice, _ := stockedTree(t, s)
	_ = rice

	s.ByPlace()
	s.OnContents()
	s.Send(sim.AltW)
	s.ShowsText("carrying")

	// The view switch is the middle of the gesture, and it used to be where the
	// screen went quiet.
	for _, view := range []string{"2", "1", "3", "4"} {
		s.Send(sim.Press(view))
		if !s.Contains("carrying") {
			t.Fatalf("view %s forgot what was in hand:\n%s", view, s.PlainView())
		}
	}
}

// It says where the thing can go, which is the half worth reading: a Holding
// goes in a place and an Item is filed under a classification, and being told
// which by a refusal is being told too late.
func TestTheBannerSaysWhereTheThingCanGo(t *testing.T) {
	s := sim.New(t)
	stockedTree(t, s)

	s.ByPlace()
	s.OnContents()
	s.Send(sim.AltW)
	s.ShowsText("puts it in a place")

	s.Send(sim.Esc)
	// An Item, picked up in the kind lens, where the contents are Items.
	s.ByKind()
	s.OnContents()
	s.Send(sim.AltW)
	s.ShowsText("files it under a classification")
}

// esc puts it down. It was the only mode in this interface with no way out.
func TestEscPutsDownWhatIsBeingCarried(t *testing.T) {
	s := sim.New(t)
	rice, _ := stockedTree(t, s)

	s.ByPlace()
	s.OnContents()
	s.Send(sim.AltW)
	s.ShowsText("carrying")
	s.Send(sim.Esc)
	s.HidesText("carrying")

	// And it really is down: a put afterwards has nothing to put.
	s.ByPlace()
	s.OnRail()
	s.Send(sim.CtrlY)
	s.ShowsText("nothing in hand")
	s.OnHand(rice, 500*domain.Scale)
}

// `m` picks the thing up as well as asking where it goes, so one key starts a
// move and either half can finish it -- name the destination, or go and point
// at it.
func TestTheMoveKeyPicksItUpAsWell(t *testing.T) {
	s := sim.New(t)
	rice, pantry := stockedTree(t, s)

	s.ByPlace()
	s.OnContents()
	s.Send(sim.Press("m"))
	s.ShowsText("carrying")

	// Out of the dropdown, then out of the prompt. Each esc leaves exactly one
	// mode, and neither of them is the carry.
	s.Send(sim.Esc, sim.Esc)
	s.HidesText("where to")
	s.ShowsText("carrying")

	s.GoTo("Kitchen")
	s.Send(sim.CtrlY)

	s.HidesText("carrying")
	s.OnHand(rice, 500*domain.Scale)
	if at := locationOf(t, s, rice); at == pantry {
		t.Error("the rice never left the pantry")
	}
}

// Answering the prompt finishes the same move, and puts down what it picked up.
// Otherwise the banner would insist a thing was still in hand after it had been
// relocated.
func TestATypedAnswerPutsDownWhatMPickedUp(t *testing.T) {
	s := sim.New(t)
	rice, pantry := stockedTree(t, s)

	s.ByPlace()
	s.OnContents()
	s.Send(sim.Press("m"))
	s.Send(sim.Type("Kitchen"))
	s.Send(sim.Enter)
	s.Send(sim.Enter) // the first took the completion, the second confirms

	s.HidesText("carrying")
	s.OnHand(rice, 500*domain.Scale)
	if at := locationOf(t, s, rice); at == pantry {
		t.Errorf("the typed move did not happen:\n%s", s.PlainView())
	}
}

// The seam between the two kinds of list.
//
// While the prompt is open every keystroke is the prompt's, so C-n walks the
// DROPDOWN and the cursor underneath does not move. Close the prompt and the
// same key walks the house. One key, two meanings, and which one is in force
// is exactly what the prompt being open decides.
func TestCNWalksTheDropdownThenTheHouse(t *testing.T) {
	s := sim.New(t)
	stockedTree(t, s)
	s.ByPlace()
	s.GoTo("Kitchen")

	under := s.Model().RailName()
	s.OnContents()
	s.Send(sim.Press("m"))
	s.Send(sim.Type("k")) // enough to offer the Kitchen and its pantry
	s.Send(sim.CtrlN)
	if got := s.Model().RailName(); got != under {
		t.Errorf("C-n moved the rail from %q to %q while a prompt was open", under, got)
	}
	s.ShowsText("TAB to take it")

	// Out of the dropdown, then out of the prompt, then the same key walks
	// the contents pane it was opened over.
	s.Send(sim.Esc, sim.Esc)
	s.HidesText("TAB to take it")
	s.OnRail()
	s.Send(sim.CtrlN)
	if got := s.Model().RailName(); got == under {
		t.Errorf("C-n did not move the rail once the prompt was closed: %q", got)
	}
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
