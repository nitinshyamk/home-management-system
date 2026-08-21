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
	byKeystroke.Send(sim.Press("4"))
	byKeystroke.Send(sim.Press("/"))
	byKeystroke.Send(sim.Type("rice"))
	byKeystroke.Send(sim.Enter)
	byKeystroke.Send(sim.Press("c"))
	byKeystroke.Send(sim.Type("100"))
	byKeystroke.Send(sim.Enter)

	byLine := sim.New(t)
	riceToo, _ := stocked(t, byLine)
	byLine.Send(sim.Press("4"))
	byLine.Send(sim.Press("/"))
	byLine.Send(sim.Type("rice"))
	byLine.Send(sim.Enter)
	byLine.Send(sim.Press(":"))
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
	s.Send(sim.Press("4"))

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
	s.Send(sim.Press("4"), sim.Press("/"))
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
	s.Send(sim.Press("4"), sim.Press("/"))
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

// A write reloads the view, and the cursor has to come back to the row it was
// on -- or acting twice on one thing is impossible, and the second t lands on
// whatever sorted first.
func TestActingTwiceStaysOnTheRow(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.Send(sim.Press("4"))

	// Deliberately NOT filtered to one row. With a single row on screen the
	// cursor lands back on it whatever the code does, so a test that filters
	// first passes even when nothing restores anything.
	s.Send(sim.Press("j")) // onto the cable, the second of two
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
	s.Send(sim.Press("4"), sim.Press("/"))
	s.Send(sim.Type("cable"))
	s.Send(sim.Enter)

	s.Send(sim.Press("t"))
	s.ShowsText("/cable")
	s.HidesText("Basmati")
}

// A refusal is about the THING, not the keystroke.
func TestAnActionThatDoesNotApplyExplainsItself(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.Send(sim.Press("4"), sim.Press("/"))
	s.Send(sim.Type("cable"))
	s.Send(sim.Enter)

	s.Send(sim.Press("c"))
	s.ShowsText("one of a kind")
	s.HidesText("how much") // the prompt never opened
}

// dd, because a single d is the start of an operator in every editor that has
// one, and retiring on a slip is not a thing to allow.
func TestRetiringNeedsTwoKeys(t *testing.T) {
	s := sim.New(t)
	stocked(t, s)
	s.Send(sim.Press("4"), sim.Press("/"))
	s.Send(sim.Type("cable"))
	s.Send(sim.Enter)

	s.Send(sim.Press("d"))
	s.HidesText("retire")
	s.Send(sim.Press("d"))
	s.ShowsText("retire")
}

// Move by name, with the destination resolved because a person typed it -- and
// the subject by identifier, because the cursor was already on it.
func TestMoveByKeystroke(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.Send(sim.Press("4"), sim.Press("/"))
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)

	s.Send(sim.Press("m"))
	s.Send(sim.Type("Garage"))
	s.Send(sim.Enter)

	s.ShowsText("Garage")
	s.OnHand(rice, 500*domain.Scale) // moved, not consumed
}

// Yank and put: the same operation as m, for when you would rather look for the
// destination than name it.
func TestYankAndPut(t *testing.T) {
	s := sim.New(t)
	rice, _ := stocked(t, s)
	s.Send(sim.Press("4"), sim.Press("/"))
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)
	s.Send(sim.Press("y"))
	s.ShowsText("yanked")

	s.Send(sim.Press("2"))
	moveTo(t, s, "Garage")
	s.Send(sim.Press("p"))

	s.Send(sim.Press("4"))
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

	s.Send(sim.Press("4"), sim.Press("/"))
	s.Send(sim.Type("rice"))
	s.Send(sim.Enter)
	s.Send(sim.Press("g"), sim.Press("g"))
	s.Send(sim.Space, sim.Space)
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
	s.Send(sim.Press("4"), sim.Press("/"))
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
