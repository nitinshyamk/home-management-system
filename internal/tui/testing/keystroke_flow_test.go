package testing_test

import (
	"strings"
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
	sim "home-management-system/internal/tui/testing"
)

// 10f: the six actions worth a single key.
//
// The claim this substage exists to make true is that every operation is
// reachable by keystroke AND by command, and that the two do the same thing. If
// they diverge, the command line is a second interface rather than the same one,
// and the argument for `:`, CSV, and the agent speaking one language collapses
// -- because the fastest path through the interface would not speak it.

// stocked is a house with something of each kind to act on.
func stocked(t *testing.T, s *sim.Simulator) (rice domain.ItemID, cable domain.ItemID) {
	t.Helper()
	p, ctx := s.Planner(), s.Context()

	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Left Pantry"}))
	pantry := s.HasLocation("Left Pantry")
	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Garage"}))
	s.Apply(p.NewCategory(ctx, ops.NewCategoryRequest{Name: "Grains"}))
	grains := s.HasCategory("Grains")

	s.Apply(p.NewItem(ctx, ops.NewItemRequest{
		Name: "Basmati Rice", Category: grains, Counting: ops.CountingMeasured, ContentUnit: "g",
	}))
	rice = s.HasItem("Basmati Rice")
	s.Apply(p.Receive(ctx, ops.ReceiveRequest{
		Item: rice, Location: pantry, Basis: domain.BasisContent,
		Amount: domain.FromMilli(500 * domain.Scale), Source: "shop",
	}))

	s.Apply(p.NewItem(ctx, ops.NewItemRequest{
		Name: "USB-C Cable", Category: grains, Counting: ops.CountingUnique,
	}))
	cable = s.HasItem("USB-C Cable")
	s.Apply(p.NewHolding(ctx, ops.NewHoldingRequest{Item: cable, Location: pantry}))
	return rice, cable
}

// TestTheKeystrokeAndTheLineDoTheSameThing is the substage's exit criterion.
//
// Two houses, identical to start with. One is consumed from by keystroke, the
// other by the equivalent typed line. They must end up in the same state, with
// the same events recorded -- the second half being the part a state assertion
// cannot reach.
func TestTheKeystrokeAndTheLineDoTheSameThing(t *testing.T) {
	byKeystroke := sim.New(t)
	rice, _ := stocked(t, byKeystroke)
	byKeystroke.ByPlace()
	byKeystroke.OnContents()
	byKeystroke.Send(sim.CtrlS)
	byKeystroke.Send(sim.Type("rice"))
	byKeystroke.Send(sim.Enter)
	byKeystroke.Send(sim.Press("c"))
	byKeystroke.Send(sim.Type("100"))
	byKeystroke.Send(sim.Enter)

	byLine := sim.New(t)
	riceToo, _ := stocked(t, byLine)
	byLine.ByPlace()
	byLine.OnContents()
	byLine.Send(sim.CtrlS)
	byLine.Send(sim.Type("rice"))
	byLine.Send(sim.Enter)
	byLine.Send(sim.AltX)
	byLine.Send(sim.Type("consume 100"))
	byLine.Send(sim.Enter)

	byKeystroke.OnHand(rice, 400*domain.Scale)
	byLine.OnHand(riceToo, 400*domain.Scale)

	// And by the same path. Consume reaching the right quantity the wrong way
	// -- adjusting instead of consuming -- is invisible to a state assertion.
	if a, b := events(t, byKeystroke, rice), events(t, byLine, riceToo); !equalStrings(a, b) {
		t.Errorf("the keystroke recorded %v and the line recorded %v", a, b)
	}
}

// The prompt opens AT the row, like the editor and the creation panel. Third
// time this answer has been given, and it is the same answer because it is the
// same reason.
func TestTheQuantityPromptOpensAtTheRow(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnContents()
	before := strings.Split(s.PlainView(), "\n")
	at := indexOfCursor(before)
	s.Send(sim.Press("c"))
	after := strings.Split(s.PlainView(), "\n")

	if indexOfCursor(after) != at {
		t.Errorf("the prompt moved the row from line %d to %d", at, indexOfCursor(after))
	}
	if !strings.Contains(after[at+1], "how much") {
		t.Errorf("the prompt is not at the row; line %d is %q", at+1, after[at+1])
	}
}

func TestAbandoningAPromptDoesNothing(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("c"))
	s.Send(sim.Type("100"))
	s.Send(sim.Esc)
	s.OnHand(rice, 500*domain.Scale)
}

// One key, not two. Two would mean remembering which state a thing is in before
// you can act, which is what looking at the screen was supposed to be for.
func TestToggleCustodyIsOneKey(t *testing.T) {
	s := sim.New(t)
	_, cable := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("cable"))
	s.Send(sim.Enter)

	s.Send(sim.Press("t"))
	s.ShowsText("out")
	s.Send(sim.Press("t"))
	s.HidesText("out,")

	held := events(t, s, cable)
	if len(held) < 2 || held[len(held)-2] != "CheckedOut" || held[len(held)-1] != "Returned" {
		t.Errorf("recorded %v, want a CheckedOut then a Returned", held)
	}
}

// space lists what can be done to the row, and what each would change.
//
// Thirty-five commands behind six keymap contexts is a vocabulary, not an
// interface. This is how the whole of it stays reachable without thirty-five
// keys.
func TestSpaceListsWhatCanBeDoneHere(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnContents()

	s.Send(sim.Space)
	s.ShowsText("ACT ON")
	s.ShowsText("use some")
	// Annotated with the state it would act on, which is the difference
	// between a key and an offer.
	s.ShowsText("500 g here")
	// And verbs with no key of their own are reachable from here.
	s.ShowsText("discard some")

	s.Send(sim.Esc)
	s.HidesText("ACT ON")
}

// What is NOT offered is as much the point: a cable has no amount, so
// consuming it is absent rather than present and then refused.
func TestThePaletteLeavesOutWhatCannotBeDone(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("cable"))
	s.Send(sim.Enter)

	s.Send(sim.Space)
	s.ShowsText("ACT ON")
	s.ShowsText("take it out")
	s.HidesText("use some")
	s.HidesText("count what is there")
}

// space acts and x selects. The space bar was the selection key, and this is
// the one binding most likely to annoy on the first day -- so C-space still
// selects too.
func TestSpaceActsAndXSelects(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnContents()

	s.Send(sim.Press("x"))
	s.ShowsText("1 selected")
	s.HidesText("ACT ON")

	s.Send(sim.Press("x")) // and off again
	s.HidesText("1 selected")

	s.Send(sim.CtrlSpace)
	s.ShowsText("1 selected")
}

// A write reloads the view, and the cursor has to come back to the row it was
// on -- or acting twice on one thing is impossible, and the second t lands on
// whatever sorted first.
func TestActingTwiceStaysOnTheRow(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnContents()
	// Deliberately NOT filtered to one row. With a single row on screen the
	// cursor lands back on it whatever the code does, so a test that filters
	// first passes even when nothing restores anything.
	s.Send(sim.CtrlN) // onto the cable, the second of two
	before := cursorLine(s)
	if !strings.Contains(before, "Cable") {
		t.Fatalf("expected to be on the cable, on %q", before)
	}

	s.Send(sim.Press("t"))
	if after := cursorLine(s); !strings.Contains(after, "Cable") {
		t.Errorf("the cursor moved from %q to %q after a write", before, after)
	}
	// And again, which is the gesture that was impossible before.
	s.Send(sim.Press("t"))
	if after := cursorLine(s); !strings.Contains(after, "Cable") {
		t.Errorf("the cursor moved after the second write: %q", after)
	}
}

// A filter lives on the omnibox rather than on the surface a reload replaces,
// so re-applying it is what keeps the line and the list agreeing.
func TestAFilterSurvivesAWrite(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("cable"))
	s.Send(sim.Enter)

	s.Send(sim.Press("t"))
	s.ShowsText("cable")
	s.HidesText("Basmati")
}

// A refusal is about the THING, not the keystroke.
func TestAnActionThatDoesNotApplyExplainsItself(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("cable"))
	s.Send(sim.Enter)

	s.Send(sim.Press("c"))
	s.ShowsText("one of a kind")
	s.HidesText("how much") // the prompt never opened
}

// Move by name, with the destination resolved because a person typed it -- and
// the subject by identifier, because the cursor was already on it.
func TestMoveByKeystroke(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("m"))
	s.Send(sim.Type("Garage"))
	s.Send(sim.Enter)

	s.ShowsText("Garage")
	s.OnHand(rice, 500*domain.Scale) // moved, not consumed
}

// Copy and put: the same operation as the move prompt, for when you would
// rather look for the destination than name it.
//
// M-w then C-y, which is emacs's copy and paste. Note that the words swap sides
// coming from vim, where a yank is the COPY rather than the paste.
func TestCopyAndPut(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)
	s.Send(sim.AltW)
	s.ShowsText("carrying")

	s.ByPlace()
	s.OnRail()
	moveTo(t, s, "Garage")
	s.Send(sim.CtrlY)

	s.ByPlace()
	s.OnContents()
	s.OnHand(rice, 500*domain.Scale)
	if !strings.Contains(s.PlainView(), "Garage") {
		t.Errorf("the rice did not move:\n%s", s.PlainView())
	}
}

// Three rows means three. Already true for the `:` line; the keystrokes must
// not have their own answer.
func TestAKeystrokeActsOnEverySelectedRow(t *testing.T) {
	s := sim.New(t)
	p, ctx := s.Planner(), s.Context()
	rice, _ := stocked(t, s)
	garage := s.HasLocation("Garage")
	s.Apply(p.Receive(ctx, ops.ReceiveRequest{
		Item: rice, Location: garage, Basis: domain.BasisContent,
		Amount: domain.FromMilli(500 * domain.Scale), Source: "shop",
	}))

	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)
	s.Send(sim.AltLess)
	s.Send(sim.CtrlSpace, sim.CtrlSpace)
	s.ShowsText("2 selected")

	s.Send(sim.Press("c"))
	s.Send(sim.Type("100"))
	s.Send(sim.Enter)

	s.OnHand(rice, 800*domain.Scale) // 1000 less 100 twice
	s.ShowsText("2 rows")
}

// A count records what was seen AND corrects, as two events. Silently
// overwriting would destroy the only evidence the two ever disagreed.
func TestCountByKeystrokeRecordsAndCorrects(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("#"))
	s.Send(sim.Type("450"))
	s.Send(sim.Enter)

	s.OnHand(rice, 450*domain.Scale)
	recorded := events(t, s, rice)
	if !contains(recorded, "Counted") || !contains(recorded, "Adjusted") {
		t.Errorf("recorded %v, want both a Counted and an Adjusted", recorded)
	}
}

// events is what was recorded against an item's first holding.
func events(t *testing.T, s *sim.Simulator, item domain.ItemID) []string {
	t.Helper()
	details, err := s.Reader().HoldingsOfItem(s.Context(), item)
	if err != nil {
		t.Fatalf("read holdings: %v", err)
	}
	if len(details) == 0 {
		return nil
	}
	return s.EventTypes(details[0].Holding.Base().ID)
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A refusal replaces what the last successful thing said. Leaving the old
// status underneath reads as though both were true -- the screen saying
// "counted 50" while also saying the action was impossible.
func TestARefusalClearsTheLastSuccess(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnContents()
	// Something that works, on the rice.
	s.Send(sim.Press("c"))
	s.Send(sim.Type("100"))
	s.Send(sim.Enter)
	s.ShowsText("use 100")

	// Then something that cannot, on the cable.
	s.Send(sim.CtrlN)
	s.Send(sim.Press("c"))
	s.ShowsText("one of a kind")
	s.HidesText("use 100")
}

// TestMovePromptCompletes is what the 10f review found missing: the machinery
// was there and nothing was wired to it.
//
// It goes through the same completer the creation panel uses, which goes
// through the same resolve index as the jump palette, the `:` line, and the
// bulk importer. A second completer would be a second opinion about what a name
// nearly is.
func TestMovePromptCompletes(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("m"))
	s.Send(sim.Type("gar"))

	s.ShowsText("Garage")
	s.ShowsText("TAB to take it")
	s.ShowsText("gar") // offered, not applied

	s.Send(sim.Tab)
	s.HidesText("TAB to take it")
	s.Send(sim.Enter)

	s.ByPlace()
	s.OnContents()
	s.OnHand(rice, 500*domain.Scale) // moved, not consumed
	if !strings.Contains(s.PlainView(), "Garage") {
		t.Errorf("the rice did not move:\n%s", s.PlainView())
	}
}

// A destination completes against PLACES. Completing it against classifications
// would offer names that cannot possibly be right.
func TestTheMovePromptCompletesOnlyPlaces(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.Press("m"))
	s.Send(sim.Type("grain")) // the Grains CATEGORY, and no location

	s.HidesText("TAB to take it")
	// The dropdown, not the screen: the inspector names the item's own
	// classification, correctly, on the line below the house.
	s.DoesNotOffer("Grains")
}

// A quantity has nothing to complete against, and offering it a list would be
// answering a question nobody asked.
func TestTheQuantityPromptOffersNothing(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.Press("c"))
	// Deliberately something that WOULD match a place. Typing "1" proves
	// nothing: no location in this house contains a 1, so the prompt stays
	// silent whether it is filtering by kind or not filtering at all.
	s.Send(sim.Type("gar"))
	s.HidesText("TAB to take it")
	// The rail is showing the Garage all the while, correctly. The claim is
	// about what the field offered.
	s.DoesNotOffer("Garage")
}

// Enter takes the highlighted place. It does not guess at the text, and it does
// not move anything on its own.
//
// It used to submit what had been TYPED, which meant that walking the list to
// the place you meant and pressing enter came back as "did you mean" -- the
// interface refusing to guess at the very thing it had just been told. Now the
// first enter fills the field in with the whole path, where it can be read, and
// the second one is what moves the stock.
func TestEnterTakesThePlaceItIsPointingAt(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("m"))
	s.Send(sim.Type("gar"))
	s.Send(sim.Enter) // takes the highlight

	s.HidesText("did you mean")
	s.ShowsText("Garage")
	// Taken, not applied. On the ROW rather than anywhere on the screen,
	// because the field itself now says Garage.
	if placed(s) {
		t.Error("taking a suggestion moved the stock by itself")
	}

	s.Send(sim.Enter) // and this moves it
	s.Send(sim.Esc)
	s.ByPlace()
	s.OnContents()
	s.OnHand(rice, 500*domain.Scale)
	if !placed(s) {
		t.Errorf("the move never happened:\n%s", s.PlainView())
	}
}

// With the list put away there is nothing to take, so a half-typed name is
// still a refusal rather than a guess -- the resolver reports, and the
// interface asks.
func TestAnUntakenSuggestionRefusesRatherThanGuessing(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("m"))
	s.Send(sim.Type("gar"))
	s.Send(sim.Esc)   // the list goes away, the field stays
	s.Send(sim.Enter) // on "gar", which is nobody's name

	s.ShowsText("did you mean")
	s.Send(sim.Esc)
	s.ByPlace()
	s.OnContents()
	s.OnHand(rice, 500*domain.Scale)
	if placed(s) {
		t.Error("a suggestion was applied without being taken")
	}
}

// placed reports whether the rice row says Garage, which is what a move shows.
func placed(s *sim.Simulator) bool {
	for _, line := range strings.Split(s.PlainView(), "\n") {
		if strings.Contains(line, "Basmati Rice") && strings.Contains(line, "Garage") {
			return true
		}
	}
	return false
}

// Retiring asks, and the reason is not the write path it uses.
//
// Gone is the single lifecycle terminal and nothing in the fold ever clears
// RetiredAt, so a retirement is permanent in the only sense a person cares
// about: the history stays and the holding does not. Friction is proportional
// to permanence rather than to whether the write happened to be a recording.
// Retiring is one key, and the guard is the confirmation rather than the key.
//
// It used to be dd, on the reasoning that retiring on a slip is not a thing to
// allow. The reasoning was right and the mechanism was redundant: a retirement
// is permanent, so its plan carries a permanent fact and this panel opens on it
// either way. Two keys in front of a panel that already asks was two answers to
// one question -- and the panel is the answer that cannot be forgotten, because
// it is the PLAN that decides it is needed, not the keystroke that asked.
func TestRetiringAsksFirst(t *testing.T) {
	s := sim.New(t)
	_, cable := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("cable"))
	s.Send(sim.Enter)

	// The identifier is taken BEFORE anything happens: a retired Holding is no
	// longer listed among its item's holdings, so looking it up afterwards
	// finds nothing and says nothing about why.
	held := holdingOf(t, s, cable)

	s.Send(sim.CtrlK)
	s.ShowsText("no way back")
	s.ShowsText("the history stays")
	if contains(s.EventTypes(held), "Gone") {
		t.Error("the holding was retired before the question was answered")
	}

	// esc means it did not happen.
	s.Send(sim.Esc)
	if contains(s.EventTypes(held), "Gone") {
		t.Error("escaping the confirmation retired it anyway")
	}

	s.Send(sim.CtrlK)
	s.Send(sim.Enter)
	if recorded := s.EventTypes(held); !contains(recorded, "Gone") {
		t.Errorf("confirming did not retire it: %v", recorded)
	}
}

// The confirmation says the act's own verb. "[enter] create" on a retirement
// would be describing the wrong thing at the worst moment.
func TestTheConfirmationUsesTheActsOwnVerb(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("cable"))
	s.Send(sim.Enter)
	s.Send(sim.CtrlK)

	s.ShowsText("go ahead")
	s.HidesText("[enter] create")
}

// The reversible actions still do not ask. Everything else stays frictionless
// BECAUSE the permanent things are not.
func TestReversibleActionsStillDoNotAsk(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("c"))
	s.Send(sim.Type("100"))
	s.Send(sim.Enter)
	s.HidesText("no way back")
	s.OnHand(rice, 400*domain.Scale) // it just happened
}

// holdingOf is an item's first holding.
func holdingOf(t *testing.T, s *sim.Simulator, item domain.ItemID) domain.HoldingID {
	t.Helper()
	details, err := s.Reader().HoldingsOfItem(s.Context(), item)
	if err != nil || len(details) == 0 {
		t.Fatalf("no holdings of item %d: %v", item, err)
	}
	return details[0].Holding.Base().ID
}

// The views that are neither a table nor a tree scroll with the cursor.
//
// They did not. The table and the tree follow their own cursors; Help, History
// and Integrity moved a cursor and redrew the same screenful, so C-n past the
// last visible line moved something nobody could see and the screen sat still.
// It reads as a view that has stopped responding, and on the help -- which is
// four screens long -- everything past the first one was unreachable.
func TestTheHelpScrollsWithItsCursor(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(88, 20)
	s.Send(sim.AltX)
	// `help all` rather than `help`, which is the index now and fits on one
	// screen -- which is the whole point of it. The scrolling this is about
	// belongs to the long page.
	s.Send(sim.Type("help all"))
	s.Send(sim.Enter)

	first := s.PlainView()
	if !strings.Contains(first, "MOVING") {
		t.Fatalf("the help did not open:\n%s", first)
	}
	// Past the bottom of the first screenful.
	for i := 0; i < 40; i++ {
		s.Send(sim.CtrlN)
	}
	after := s.PlainView()
	if after == first {
		t.Errorf("the help never scrolled:\n%s", after)
	}
	// And the cursor is somewhere a person can see it.
	if !strings.Contains(after, ">") {
		t.Errorf("the cursor walked off the screen:\n%s", after)
	}
}

// `m` over a SELECTION does not pick anything up.
//
// A carry holds one thing and the prompt acts on every selected row, so with
// several picked there is nothing coherent to hold -- and a banner naming one
// of three rows would be describing a move that is not the one about to happen.
// What `m` writes is unchanged either way.
func TestTheMoveKeyDoesNotCarryASelection(t *testing.T) {
	s := sim.New(t)
	p, ctx := s.Planner(), s.Context()
	rice, _ := stocked(t, s)
	garage := s.HasLocation("Garage")
	// Two DIFFERENT measured items, so that moving both to one place is two
	// moves and not a merge. Two holdings of the SAME item sent to one shelf is
	// its own thing, and TestABatchThatWouldMergeIsRefused is where it lives.
	s.Apply(p.NewItem(ctx, ops.NewItemRequest{
		Name: "Wild Rice", Category: s.HasCategory("Grains"),
		Counting: ops.CountingMeasured, ContentUnit: "g",
	}))
	wild := s.HasItem("Wild Rice")
	s.Apply(p.Receive(ctx, ops.ReceiveRequest{
		Item: wild, Location: garage, Basis: domain.BasisContent,
		Amount: domain.FromMilli(500 * domain.Scale), Source: "shop",
	}))
	// Somewhere neither of them already is, so the batch is two real moves.
	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Shed"}))
	_ = rice

	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)
	s.Send(sim.AltLess)
	s.Send(sim.CtrlSpace, sim.CtrlSpace)
	s.ShowsText("2 selected")

	s.Send(sim.Press("m"))
	s.HidesText("carrying")

	// And it still moves both of them. One enter, not two: "Shed" is exactly a
	// place and nothing else is near it, so there is no list left open for the
	// first enter to take from.
	s.Send(sim.Type("Shed"))
	s.Send(sim.Enter)
	s.ShowsText("2 rows")
}

// Moving two holdings of one item onto one shelf is refused, and says why.
//
// This is the gesture that found the defect: select both rows of rice, press m,
// name the Shed. Each command was planned against a snapshot taken before the
// batch began, so both found the destination free and both moved -- leaving two
// active Holdings on one H8 key, which nothing but the integrity report would
// ever have mentioned.
//
// Refused rather than merged. Merging is what O1 asks for and what happens
// inside a single plan; doing it across plans needs a reference that spans
// Steps, and the execution model has none. The refusal is the honest half.
func TestABatchThatWouldMergeIsRefused(t *testing.T) {
	s := sim.New(t)
	p, ctx := s.Planner(), s.Context()
	rice, _ := stocked(t, s)
	garage := s.HasLocation("Garage")
	s.Apply(p.Receive(ctx, ops.ReceiveRequest{
		Item: rice, Location: garage, Basis: domain.BasisContent,
		Amount: domain.FromMilli(500 * domain.Scale), Source: "shop",
	}))
	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Shed"}))

	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)
	s.Send(sim.AltLess)
	s.Send(sim.CtrlSpace, sim.CtrlSpace)
	s.ShowsText("2 selected")

	s.Send(sim.Press("m"))
	s.Send(sim.Type("Shed"))
	s.Send(sim.Enter)

	s.ShowsText("one holding")
	// The Shed is on the rail as an empty place, correctly; what must not
	// have happened is a row landing in it.
	s.ContentsHide("Shed")
	// The harness verifies every holding against its events on the way out, so
	// the assertion that matters most is made for us.
	s.OnHand(rice, 1000*domain.Scale)
}

// TAB steps between the sections of the long help page.
//
// Line-at-a-time is the only motion a page of prose has, and the whole listing
// is four screens: without this, finding the section you want means scrolling
// past the ones you do not. The keys are the tree's fold keys, because on a
// page of sections they mean what the tree means by them -- move by structure
// rather than by line.
func TestTabStepsThroughTheHelpSections(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(88, 20)
	s.Send(sim.AltX)
	s.Send(sim.Type("help all"))
	s.Send(sim.Enter)

	// The cursor starts on the first heading, so one TAB reaches the second.
	s.Send(sim.Tab)
	if got := cursorLine(s); !strings.Contains(got, "CHOOSING ROWS") {
		t.Errorf("TAB landed on %q, want the second section", got)
	}

	// And it keeps going, past the bottom of the first screen.
	for i := 0; i < 6; i++ {
		s.Send(sim.Tab)
	}
	if got := cursorLine(s); !strings.Contains(got, "TYPING IN A FIELD") {
		t.Errorf("seven TABs landed on %q, want the eighth section", got)
	}

	// S-TAB comes back one.
	s.Send(sim.ShiftTab)
	if got := cursorLine(s); !strings.Contains(got, "ACTING ON A ROW") {
		t.Errorf("S-TAB landed on %q, want the section before", got)
	}

	// The last section is the end of it: TAB there stops rather than wrapping
	// round to the top, which in a document reads as having lost your place.
	for i := 0; i < 20; i++ {
		s.Send(sim.Tab)
	}
	last := cursorLine(s)
	if !strings.Contains(last, "COMMANDS") {
		t.Errorf("TAB past the end landed on %q, want the last section", last)
	}
	s.Send(sim.Tab)
	if got := cursorLine(s); got != last {
		t.Errorf("TAB at the last section wrapped round to %q", got)
	}
}

// The index fits one screen, which is the whole reason it exists: the listing
// it replaces is four.
func TestTheHelpOpensOnAnIndexThatFits(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(88, 24)
	s.Send(sim.AltX)
	s.Send(sim.Type("help"))
	s.Send(sim.Enter)

	view := s.PlainView()
	s.FitsWidth(88)
	for _, want := range []string{"WHAT THERE IS", "carrying", "commands", "help all"} {
		if !strings.Contains(view, want) {
			t.Errorf("the help index does not mention %q:\n%s", want, view)
		}
	}
	// Nothing from inside a section: the index names the sections, it does not
	// unroll them.
	if strings.Contains(view, "put the cursor back in the middle") {
		t.Errorf("the index unrolled a section into itself:\n%s", view)
	}
}
