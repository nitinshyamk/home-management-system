package testing_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
	sim "home-management-system/internal/tui/testing"
	"home-management-system/internal/tui/text"
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
//
// The refusal that guards it is still in destination(), and is now a backstop:
// while something is in hand the cursor does not go anywhere that could ask for
// it, which is what this asserts. A person cannot aim at the wrong answer, so
// they never have to read why it was wrong.
func TestAContainedRowIsNotSomewhereToPut(t *testing.T) {
	s := sim.New(t)
	rice, _ := stockedTree(t, s)

	s.Send(sim.Press("4"), sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)
	s.Send(sim.AltW)
	s.ShowsText("carrying")

	// Down the Locations tree as far as it will go, which is one row short of
	// the holding: Kitchen, Left Pantry, and then the rice, which is not a
	// place.
	//
	// No esc on the way: switching views clears the filter by itself, and esc
	// now PUTS DOWN what is being carried -- which would leave nothing in hand
	// and the test passing for the wrong reason.
	s.Send(sim.Press("2"), sim.Press("v"))
	before := s.CountHoldings()
	for i := 0; i < 4; i++ {
		s.Send(sim.CtrlN)
		if line := cursorLine(s); strings.Contains(line, "Basmati Rice") {
			t.Fatalf("the cursor came to rest on a holding row while something was in hand: %q", line)
		}
	}
	// Nothing moved on the way there, and nothing was created.
	if after := s.CountHoldings(); after != before {
		t.Errorf("walking the tree changed the holdings from %d to %d", before, after)
	}
	s.OnHand(rice, 500*domain.Scale)

	// And the row is reachable again the moment nothing is in hand, so this is
	// the carry ruling it out rather than the tree hiding it for good.
	s.Send(sim.Esc)
	s.HidesText("carrying")
	moveTo(t, s, "Basmati Rice")
}

// The cursor skips to the next PLACE, rather than stopping at every jar on the
// way.
//
// This is the clunkiness the whole thing is about. A tree showing its contents
// is mostly contents -- one shelf, nine jars -- and pointing at a destination
// meant walking through all nine, each of them a row the put was going to
// refuse. The keystrokes now count candidates instead of rows.
func TestWhileCarryingTheCursorSkipsToTheNextPlace(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("4"), sim.AltW)
	s.ShowsText("carrying")
	s.Send(sim.Press("2"), sim.Press("v"))
	s.ShowsText("Ancho Chile") // the Garage's own jar, shown and not offered

	// The Garage holds a jar, and its first sub-place is below that jar. One
	// C-n from the Garage reaches the shelving unit.
	moveTo(t, s, "Garage")
	s.Send(sim.CtrlN)
	if got := cursorLine(s); !strings.Contains(got, "Metal Shelving Unit") {
		t.Errorf("C-n out of the Garage landed on %q, want the Metal Shelving Unit -- "+
			"the jar in between is not a place", got)
	}
	// And C-f, which aims at the first child, does the same rather than aiming
	// at the jar and going nowhere.
	s.Send(sim.AltLess) // back to the Garage, which is the first row
	s.Send(sim.CtrlF)
	if got := cursorLine(s); !strings.Contains(got, "Metal Shelving Unit") {
		t.Errorf("C-f into the Garage landed on %q, want the Metal Shelving Unit", got)
	}
}

// And they are drawn as ruled out, so the skipping is explained before it
// happens rather than felt as a cursor that will not go where it is pushed.
func TestTheThingsInsideGoFaintWhileCarrying(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)

	s := sim.New(t)
	stockedTree(t, s)
	s.Send(sim.Press("2"), sim.Press("v"))
	calm := styledLine(s, "500 g")

	s.Send(sim.Press("4"), sim.AltW)
	s.Send(sim.Press("2"))
	s.ShowsText("carrying")
	carrying := styledLine(s, "500 g")

	if carrying == calm {
		t.Errorf("the holding row looks the same carrying as not: %q", carrying)
	}
	if !strings.Contains(carrying, "\x1b[2m") && !strings.Contains(carrying, ";2m") {
		t.Errorf("the holding row is not faint while something is in hand: %q", carrying)
	}
}

// styledLine is the rendered line for a row, escape sequences and all, which is
// what a claim about how something LOOKS has to read.
//
// Found by the row's MEASURE rather than by its name: the carry banner says the
// name too, and it is above the body -- so a search for the name reads back the
// banner's styling and the assertion is about the wrong line entirely.
func styledLine(s *sim.Simulator, needle string) string {
	for _, line := range strings.Split(s.View(), "\n") {
		if strings.Contains(text.StripANSI(line), needle) {
			return line
		}
	}
	return ""
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
	s.Send(sim.Press("2"), sim.Press("v"))

	// Onto the rice, inside the Left Pantry, and pick it up.
	s.Send(sim.CtrlN)
	s.Send(sim.CtrlN)
	s.Send(sim.CtrlN)
	s.Send(sim.AltW)
	s.ShowsText("carrying")

	// Onto the Kitchen, which is a place, and put it there.
	s.Send(sim.AltLess)
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

	s.Send(sim.Press("1"), sim.Press("v"))
	// Grains sorts before Pantry, and is empty, so the rice is two rows down.
	s.Send(sim.CtrlN)
	s.Send(sim.CtrlN)
	s.ShowsText("Basmati Rice")
	s.Send(sim.AltW)
	s.ShowsText("carrying")

	s.Send(sim.AltLess) // onto Grains
	s.Send(sim.CtrlY)

	if got := categoryOf(t, s, rice); got != grains {
		t.Errorf("the rice is filed under %d, want Grains (%d)", got, grains)
	}
}

// The prompt is the other half: for when you already know where it goes.
func TestThePromptMovesAThingInATree(t *testing.T) {
	s := sim.New(t)
	rice, pantry := stockedTree(t, s)
	s.Send(sim.Press("2"), sim.Press("v"))
	s.Send(sim.CtrlN)
	s.Send(sim.CtrlN)
	s.Send(sim.CtrlN)

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

	s.Send(sim.Press("1"), sim.Press("v"))
	s.Send(sim.CtrlN)
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

	s.Send(sim.Press("2"), sim.Press("v"))
	s.Send(sim.CtrlN)
	s.Send(sim.CtrlN)
	s.Send(sim.CtrlN)
	s.Send(sim.AltW)

	s.Send(sim.Press("1")) // the Categories tree
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
	s.Send(sim.Press("2"), sim.Press("v"))

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

	s.Send(sim.Press("4"))
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

	s.Send(sim.Press("4"), sim.AltW)
	s.ShowsText("puts it in a place")

	s.Send(sim.Esc)
	// An Item, picked up in the Categories tree.
	s.Send(sim.Press("1"), sim.Press("v"))
	moveTo(t, s, "Basmati Rice")
	s.Send(sim.AltW)
	s.ShowsText("files it under a classification")
}

// esc puts it down. It was the only mode in this interface with no way out.
func TestEscPutsDownWhatIsBeingCarried(t *testing.T) {
	s := sim.New(t)
	rice, _ := stockedTree(t, s)

	s.Send(sim.Press("4"), sim.AltW)
	s.ShowsText("carrying")
	s.Send(sim.Esc)
	s.HidesText("carrying")

	// And it really is down: a put afterwards has nothing to put.
	s.Send(sim.Press("2"))
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

	s.Send(sim.Press("4"), sim.Press("m"))
	s.ShowsText("carrying")

	// Out of the dropdown, then out of the prompt. Each esc leaves exactly one
	// mode, and neither of them is the carry.
	s.Send(sim.Esc, sim.Esc)
	s.HidesText("where to")
	s.ShowsText("carrying")

	s.Send(sim.Press("2"))
	moveTo(t, s, "Kitchen")
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

	s.Send(sim.Press("4"), sim.Press("m"))
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
// DROPDOWN and the tree cursor does not move. Close the prompt and the same key
// walks the TREE. One key, two meanings, and which one is in force is exactly
// what the prompt being open decides.
func TestCNWalksTheDropdownThenTheTree(t *testing.T) {
	s := sim.New(t)
	stockedTree(t, s)
	s.Send(sim.Press("2"), sim.Press("v"))
	moveTo(t, s, "Basmati Rice")

	row := cursorLine(s)
	s.Send(sim.Press("m"))
	s.Send(sim.CtrlN)
	if got := cursorLine(s); got != row {
		t.Errorf("C-n moved the tree cursor from %q to %q while a prompt was open", row, got)
	}
	s.ShowsText("TAB to take it")

	// C-p rather than C-n for the tree half: `m` only opens on a contained row,
	// and the only one in this house is the last row of the tree, so there is
	// nothing below it to move to.
	s.Send(sim.Esc, sim.Esc)
	s.Send(sim.CtrlP)
	if got := cursorLine(s); got == row {
		t.Errorf("C-p did not move the tree cursor once the prompt was closed: %q", got)
	}
	s.HidesText("TAB to take it")
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
