package testing_test

import (
	"testing"
	"time"

	"home-management-system/internal/ops"
	sim "home-management-system/internal/tui/testing"
)

// The golden frames for 10a, captured after sign-off.
//
// They mean UNCHANGED, never good. What makes them worth anything is that the
// layout they hold was reviewed by a person and accepted, so a failure here
// says "this differs from what was approved" rather than "this differs from
// whatever came out first".
//
// A failure is not automatically a bug. It is a prompt to re-review the screen
// and, if the change is wanted, re-capture with -update-golden.

func TestGoldenTheHouse(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	for _, tc := range []struct {
		name          string
		width, height int
	}{
		{"shell-place-100", 100, 22},
		// 60 columns is below narrowWidth, where the shell gives up the
		// second pane entirely. That fallback is a design decision rather
		// than a resize rule, so it is the frame most worth guarding.
		{"shell-narrow-60", 60, 22},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.Resize(tc.width, tc.height)
			s.ByPlace()
			s.OnContents()
			s.AssertFrame(tc.name)
		})
	}
}

// Standing on a shelf rather than at the top of the house: the contents pane
// narrows to what is in there, and WHERE drops out because every row is in
// the same place.
func TestGoldenAShelf(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(100, 22)
	s.ByPlace()
	s.GoTo("Left Pantry")
	s.AssertFrame("shell-one-place")
}

// The selected state has its own frame: the gutter marks are the part of a
// selection that survives a monochrome terminal, and they are what a
// plain-text golden can hold.
func TestGoldenHoldingsWithASelection(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(100, 22)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlSpace, sim.CtrlSpace, sim.CtrlSpace)
	s.AssertFrame("shell-selection")
}

func TestGoldenTheKindLens(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(100, 22)
	s.ByKind()
	s.OnContents()
	s.AssertFrame("shell-kind-100")
}

// 10b and 10c, provisionally accepted 2026-08-20 with more UI shifts expected.
//
// Captured anyway, and on purpose: 10d edits these same screens, and what a
// golden is for here is the change nobody intended. A deliberate shift means
// re-capturing, which is one command and a legible diff.

func TestGoldenTheRail(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(84, 22)
	s.ByPlace()
	s.OnRail()
	s.AssertFrame("shell-rail-84")

	// Collapsed is the overview, and it is a different layout question.
	s.Send(sim.ShiftTab)
	s.AssertFrame("shell-rail-collapsed")
}

func TestGoldenFilterAndJumpDoNotLookAlike(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(90, 20)

	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("loc:tray"))
	s.Send(sim.Enter)
	s.AssertFrame("shell-filtered")

	s.Send(sim.Esc)
	s.Send(sim.AltG)
	s.Send(sim.Type("crate"))
	s.AssertFrame("shell-jump")
}

// 10d, accepted 2026-08-20 after one round of rework.
//
// The editor frame is the one most worth guarding: "inline" is a claim about
// WHERE lines are, and a plain-text frame is exactly the thing that can hold
// that claim still.

func TestGoldenInlineEditor(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(84, 20)
	s.ByPlace()
	s.OnRail()
	s.Send(sim.CtrlN, sim.CtrlN)
	s.Send(sim.Press("e"))
	s.AssertFrame("shell-editor-inline")
}

func TestGoldenTheConfirmation(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(92, 18)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.AltX)
	s.Send(sim.Type("new item Turmeric counting measured unit g package 2000 category Spices"))
	s.Send(sim.Enter)
	s.AssertFrame("shell-confirmation")
}

func TestGoldenARefusal(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(64, 18)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.AltX)
	s.Send(sim.Type("consume 5kg"))
	s.Send(sim.Enter)
	s.AssertFrame("shell-refusal-wrapped")
}

// 10f's confirmation, which asks for a different reason than 10d's.
//
// Both frames are kept because the two labels have to line up with each other:
// "permanent" and "no way back" sit in the same column, so a change to either
// shows in both.
func TestGoldenTheRetirementConfirmation(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(92, 18)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("thunder"))
	s.Send(sim.Enter)
	s.Send(sim.CtrlK)
	s.AssertFrame("shell-retire-confirmation")
}

// The move drawer, which is a LAYOUT claim: the destinations sit below the
// house, the house stays visible and gives up rows to make room, and it
// stops drawing a cursor because the drawer has one.
//
// Kept as a frame because that is exactly the kind of claim a plain-text
// capture can hold still.
func TestGoldenMovingSomething(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(84, 22)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.Press("m"))
	s.AssertFrame("shell-moving")
}

// The import plan, and the same plan having refused to apply.
//
// It had no frame at all until now, which is exactly why its facts line ran to
// 102 columns on a 100-column terminal without anything noticing: a line wider
// than the screen wraps, and one wrapped line shifts every row above it. The
// screen nobody looks at is the screen that drifts.
//
// Two frames rather than one, because the refusal is the half that was drawn by
// this screen's own code rather than by the chrome every other screen uses.
func TestGoldenTheImportPlan(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, messy))
	s.FitsWidth(sim.Width)
	s.AssertFrame("11f-import-plan")

	// A is refused while rows are blocked, and the reason goes in the status
	// block -- the same block, in the same place, as everywhere else.
	s.Send(sim.Press("A"))
	s.FitsWidth(sim.Width)
	s.AssertFrame("11f-import-plan-refused")
}

// And it fits the narrow terminal too, where the facts line has to give up its
// tail rather than wrap.
func TestTheImportPlanFitsANarrowTerminal(t *testing.T) {
	s := sim.New(t)
	kitchen(t, s)
	s.Import(receipt(t, messy))
	for _, width := range []int{60, 80, 100, 120} {
		s.Resize(width, sim.Height)
		s.FitsWidth(width)
	}
}

// Organise mode, with a batch in it.
//
// Two claims a plain-text capture holds still that prose cannot. The house is
// still legible behind the batch -- the rail, the contents, and the cursor,
// which this is the one drawer that leaves where it is. And the batch says
// plainly that it has not happened: the heading, the counts and the key that
// would make it happen are all on screen at once.
func TestGoldenOrganising(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(84, 24)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.Press("O"))
	s.Send(sim.Press("m"))
	s.Send(sim.Enter)
	s.FitsWidth(84)
	s.AssertFrame("shell-organising")
}

// The walk, asking, and the same walk having been answered.
//
// Two frames because they are two faces of one drawer, and the second is the
// one that carries the claim: what was SAID, per stop, before any of it is
// written. A screen that only showed the question would make the walk a
// thing you do on trust.
func TestGoldenTheWalk(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(84, 24)
	s.ByPlace()
	s.OnRail()
	s.Send(sim.Press("V"))
	s.FitsWidth(84)
	s.AssertFrame("walk-asking")

	for range 4 {
		s.Send(sim.Press("y"))
	}
	s.FitsWidth(84)
	s.AssertFrame("walk-answered")
}

// The attention banner, and the queue it opens.
//
// A new permanent line in the chrome, which is exactly the kind of thing a
// frame holds still: it costs the house a row, so a version of it that
// forgot to subtract that row would run the screen off the bottom of the
// terminal and shift every line above it.
func TestGoldenAttention(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	p, ctx := s.Planner(), s.Context()

	// Something past its date, which is the condition the line is about.
	// The fixtures have nothing overdue, which is why no frame showed this
	// line until one was made to.
	rows, err := s.Reader().Holdings(ctx)
	if err != nil || len(rows) == 0 {
		t.Fatalf("reading holdings: %v", err)
	}
	past := time.Now().AddDate(0, 0, -30)
	s.Apply(p.SetExpiry(ctx, ops.SetExpiryRequest{
		Holding: rows[0].Holding.Base().ID, On: &past,
	}))

	s.Resize(84, 24)
	s.Send(sim.Press("g")) // refresh, which recounts
	s.ByPlace()
	s.FitsWidth(84)
	s.AssertFrame("attention-banner")

	s.Send(sim.Press("!"))
	s.FitsWidth(84)
	s.AssertFrame("attention-queue")
}
