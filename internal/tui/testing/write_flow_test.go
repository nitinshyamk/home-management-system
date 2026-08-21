package testing_test

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

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
	for _, want := range []string{"Ancho Chile", "only 100 g", "5000 g was asked for"} {
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

// ---------------------------------------------------------------------------
// From the 10d review
// ---------------------------------------------------------------------------

// TestACommandActsOnEverySelectedRow is the worst of the five problems the
// review found: three rows selected, and the command acted on the cursor row.
// It did something, it did not do what was asked, and it said nothing about the
// difference.
func TestACommandActsOnEverySelectedRow(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	ancho := s.HasItem("Ancho Chile")
	s.OnHand(ancho, 300*domain.Scale)

	s.Send(sim.Press("4"))
	s.Send(sim.Press("/"))
	s.Send(sim.Type("ancho"))
	s.Send(sim.Enter)
	// From the top, explicitly. The cursor tracks its row through a filter --
	// deliberately, so you do not lose your place while typing -- so where it
	// ends up is not something a test should assume.
	s.Send(sim.Press("g"), sim.Press("g"))
	s.Send(sim.Space, sim.Space, sim.Space) // all three
	s.ShowsText("3 selected")

	s.Send(sim.Press(":"))
	s.Send(sim.Type("consume 10"))
	s.Send(sim.Enter)

	// Thirty, not ten: three rows, each by ten.
	s.OnHand(ancho, 270*domain.Scale)
	s.ShowsText("3 rows")
}

// One transaction across the whole selection: three rows either all move or
// none do. Applying them one at a time would leave a half-done batch on any
// refusal, and the half would be silent.
func TestASelectionIsOneUnitOfWork(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	ancho := s.HasItem("Ancho Chile")

	s.Send(sim.Press("4"))
	s.Send(sim.Press("/"))
	s.Send(sim.Type("ancho"))
	s.Send(sim.Enter)
	s.Send(sim.Press("g"), sim.Press("g"))
	s.Send(sim.Space, sim.Space, sim.Space)

	// More than one of them holds: two have 100, one has 100 -- ask for 150 and
	// every row refuses, but the point is that the FIRST would have succeeded
	// if they were applied one by one.
	s.Send(sim.Press(":"))
	s.Send(sim.Type("consume 150"))
	s.Send(sim.Enter)

	s.OnHand(ancho, 300*domain.Scale)
	s.ShowsText("not enough")
}

// The summary collapses identical lines. A summary that repeats itself is one
// nobody finishes reading, and the end is where the differences would be.
func TestABatchSummaryDoesNotRepeatItself(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("4"))
	s.Send(sim.Press("/"))
	s.Send(sim.Type("ancho"))
	s.Send(sim.Enter)
	s.Send(sim.Press("g"), sim.Press("g"))
	s.Send(sim.Space, sim.Space, sim.Space)
	s.Send(sim.Press(":"))
	s.Send(sim.Type("consume 10"))
	s.Send(sim.Enter)

	if got := strings.Count(s.PlainView(), "use 10 of"); got != 1 {
		t.Errorf("the summary says the same thing %d times", got)
	}
}

// Truly inline: the field opens AT the row, and the rows below move down rather
// than the field appearing under the whole list.
func TestTheEditorOpensAtTheRow(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("2"))
	s.Send(sim.Press("j"), sim.Press("j"))

	before := strings.Split(s.PlainView(), "\n")
	cursorAt := indexOfCursor(before)
	if cursorAt < 0 {
		t.Fatal("no cursor")
	}

	s.Send(sim.Press("e"))
	after := strings.Split(s.PlainView(), "\n")

	// The row under the cursor has not moved.
	if indexOfCursor(after) != cursorAt {
		t.Errorf("the edited row moved from line %d to %d", cursorAt, indexOfCursor(after))
	}
	// And the field is immediately after it, not at the bottom of the screen.
	if !strings.Contains(after[cursorAt+1], "rename") {
		t.Errorf("the field is not at the row; line %d is %q", cursorAt+1, after[cursorAt+1])
	}
	// The rows that were below are still below, pushed down.
	if !strings.Contains(strings.Join(after, "\n"), nameOn(before[cursorAt+1])) {
		t.Errorf("the row below the edit disappeared instead of moving down")
	}
}

// A refusal wraps rather than being cut off. The part that says what to do
// about it is at the end, so a truncated error reports a problem and withholds
// the answer.
func TestARefusalWrapsRatherThanTruncating(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(64, 24)
	s.Send(sim.Press("4"))
	s.Send(sim.Press("/"))
	s.Send(sim.Type("loc:garage"))
	s.Send(sim.Enter)
	s.Send(sim.Press(":"))
	s.Send(sim.Type("consume 5kg"))
	s.Send(sim.Enter)

	s.FitsWidth(64)
	// The whole sentence survived, across however many lines it took.
	flat := strings.Join(strings.Fields(s.PlainView()), " ")
	for _, want := range []string{"only 100 g", "5000 g was asked for", "packages to open"} {
		if !strings.Contains(flat, want) {
			t.Errorf("the refusal lost %q at 64 columns:\n%s", want, s.PlainView())
		}
	}
}

// And it is loud. Everything else here is deliberately quiet, which is exactly
// what makes one loud thing readable.
func TestARefusalIsNotQuiet(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)

	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("4"))
	s.Send(sim.Press("/"))
	s.Send(sim.Type("loc:garage"))
	s.Send(sim.Enter)
	calm := s.View()

	s.Send(sim.Press(":"))
	s.Send(sim.Type("consume 5kg"))
	s.Send(sim.Enter)
	angry := s.View()

	// A FOREGROUND colour, which nothing else on this screen uses -- banding
	// and selection are backgrounds, and everything else is weight.
	if !strings.Contains(angry, "38;5;") {
		t.Errorf("the refusal carries no colour:\n%q", angry)
	}
	if strings.Contains(calm, "38;5;") {
		t.Error("the calm screen already uses a foreground colour, so the refusal does not stand out")
	}
}

// A message wearing a call stack is a message a person has to decode. The
// layers are useful in a log and are noise on a screen.
func TestARefusalDoesNotWearACallStack(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("4"))
	s.Send(sim.Press(":"))
	s.Send(sim.Type("consume 5kg"))
	s.Send(sim.Enter)

	view := s.PlainView()
	for _, noise := range []string{"ops:", "command:", "invalid request:", "sql:", "constraint"} {
		if strings.Contains(view, noise) {
			t.Errorf("the refusal shows %q:\n%s", noise, view)
		}
	}
}

func indexOfCursor(lines []string) int {
	for i, line := range lines {
		if strings.HasPrefix(line, ">") {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// 10e: the creation panel
// ---------------------------------------------------------------------------

// The panel is in the LIST, like the editor. A creation form that replaces the
// view costs you the context that tells you whether the thing already exists.
func TestTheCreationPanelOpensInTheList(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("3"))

	before := strings.Split(s.PlainView(), "\n")
	cursorAt := indexOfCursor(before)

	s.Send(sim.Press("o"))
	after := strings.Split(s.PlainView(), "\n")

	if indexOfCursor(after) != cursorAt {
		t.Errorf("opening the panel moved the row from line %d to %d", cursorAt, indexOfCursor(after))
	}
	if !strings.Contains(after[cursorAt+1], "new item") {
		t.Errorf("the panel is not at the row; line %d is %q", cursorAt+1, after[cursorAt+1])
	}
	// The rows the panel pushed down are still there.
	s.ShowsText("Ancho Chile")
}

// All three kinds create, through the same panel and the same confirmation.
func TestAllThreeKindsCreate(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("1"), sim.Press("o"))
	s.Send(sim.Type("Preserves"))
	s.Send(sim.Enter, sim.Enter)
	s.HasCategory("Preserves")

	s.Send(sim.Press("2"), sim.Press("o"))
	s.Send(sim.Type("Cellar"))
	s.Send(sim.Enter, sim.Enter)
	s.HasLocation("Cellar")

	s.Send(sim.Press("3"), sim.Press("o"))
	s.Send(sim.Type("Turmeric"))
	s.Send(sim.Tab, sim.Tab) // past counting, onto unit
	s.Send(sim.Type("g"))
	s.Send(sim.Tab, sim.Tab) // past package, onto category
	s.Send(sim.Type("Spices"))
	s.Send(sim.Enter)
	s.ShowsText("permanent")
	s.Send(sim.Enter)
	s.HasItem("Turmeric")
}

// esc leaves exactly one mode: the confirmation returns to the PANEL with what
// was typed still in it, and only the next esc reaches the list.
func TestEscapeFromTheConfirmationReturnsToThePanel(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("3"), sim.Press("o"))
	s.Send(sim.Type("Turmeric"))
	s.Send(sim.Tab, sim.Tab)
	s.Send(sim.Type("g"))
	s.Send(sim.Tab, sim.Tab)
	s.Send(sim.Type("Spices"))
	s.Send(sim.Enter)
	s.ShowsText("permanent")

	s.Send(sim.Esc)
	s.HidesText("permanent")
	s.ShowsText("new item")
	s.ShowsText("Turmeric") // still typed
	s.HasNoItem("Turmeric")

	s.Send(sim.Esc)
	s.HidesText("new item")
	s.HasNoItem("Turmeric")
}

// A panel that remembers an abandoned attempt will eventually create it.
func TestASecondPanelStartsClean(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("3"), sim.Press("o"))
	s.Send(sim.Type("Abandoned"))
	s.Send(sim.Esc)

	s.Send(sim.Press("o"))
	s.HidesText("Abandoned")
}

// The panel goes through the `:` line's own path, so a name it cannot use is
// refused the same way and in the same words.
func TestThePanelIsRefusedLikeTheLine(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("3"), sim.Press("o"))
	s.Send(sim.Enter) // no name
	s.ShowsText("name is required")
	s.ShowsText("new item") // still open

	s.Send(sim.Type("Turmeric"))
	s.Send(sim.Tab, sim.Tab)
	s.Send(sim.Type("g"))
	s.Send(sim.Tab, sim.Tab)
	s.Send(sim.Type("Nowhere"))
	s.Send(sim.Enter)
	s.ShowsText("category")
	s.HasNoItem("Turmeric")
}

// Stock arrives by acquiring it. A Holding is a placement rather than a name,
// so there is no panel for one -- and saying so beats a panel that cannot work.
func TestThereIsNoPanelForAHolding(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("4"), sim.Press("o"))
	s.ShowsText("acquire")
	s.HidesText("new holding")
}

// Creating inside what you are looking at is what o means.
func TestCreatingInsideTheCursorsNode(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Send(sim.Press("2"))
	// onto the Garage
	for !strings.Contains(cursorLine(s), "Garage") {
		s.Send(sim.Press("j"))
	}
	s.Send(sim.Press("o"))
	s.ShowsText("Garage") // offered as the parent

	s.Send(sim.Type("Workbench"))
	s.Send(sim.Enter, sim.Enter)

	id := s.HasLocation("Workbench")
	path, err := s.Reader().LocationPath(s.Context(), id)
	if err != nil {
		t.Fatalf("read path: %v", err)
	}
	if len(path) < 2 || path[len(path)-2].Name != "Garage" {
		t.Errorf("Workbench was created at the top level, not inside the Garage")
	}
}

// TestAutocompleteInThePanel is step 7 of the 10e walkthrough, which the review
// found had a criterion and no implementation behind it.
//
// It goes through the resolve index -- the same index behind the jump palette,
// the `:` line, and the bulk importer. A panel that completed names differently
// would be teaching a vocabulary the rest of the application does not speak.
func TestAutocompleteInThePanel(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("3"), sim.Press("o"))
	s.Send(sim.Type("Turmeric"))
	s.Send(sim.Tab, sim.Tab) // unit
	s.Send(sim.Type("g"))
	s.Send(sim.Tab, sim.Tab) // category
	s.Send(sim.Type("spic"))

	// Offered, and named in full so a person can tell which one it is.
	s.ShowsText("Spices")
	s.ShowsText("tab to take it")
	// And not applied.
	s.ShowsText("spic")

	s.Send(sim.Tab)
	s.HidesText("tab to take it")
	s.Send(sim.Enter)
	s.ShowsText("permanent")
	s.Send(sim.Enter)
	s.HasItem("Turmeric")
}

// Completing a place against classifications would offer names that cannot
// possibly be right.
func TestThePanelCompletesTheRightKind(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("2"))
	s.Send(sim.Press("o"))
	s.Send(sim.Type("Cellar"))
	s.Send(sim.Tab)   // under, pre-filled with the cursor's node
	s.Send(sim.CtrlU) // which would otherwise be typed into
	s.Send(sim.Type("spic"))

	// No LOCATION in this house matches "spic" -- but the Spices CATEGORY
	// does, so an unfiltered completer would offer it here. Nothing on offer is
	// the right answer, and it is the only assertion that can tell the two
	// apart.
	s.HidesText("tab to take it")
	if strings.Contains(s.PlainView(), "Spices") {
		t.Errorf("a location's parent field offered a category:\n%s", s.PlainView())
	}

	// And it does complete a Location.
	s.Send(sim.CtrlU)
	s.Send(sim.Type("gar"))
	s.ShowsText("Garage")
	s.ShowsText("tab to take it")
}

// Autocomplete is only ever a suggestion, so a name it does not know still
// reaches Bind and is refused there in the usual words.
func TestAnUnknownNameStillRefuses(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	s.Send(sim.Press("3"), sim.Press("o"))
	s.Send(sim.Type("Turmeric"))
	s.Send(sim.Tab, sim.Tab)
	s.Send(sim.Type("g"))
	s.Send(sim.Tab, sim.Tab)
	s.Send(sim.Type("Nowhere At All"))
	s.Send(sim.Enter)

	s.ShowsText("category")
	s.HasNoItem("Turmeric")
}
