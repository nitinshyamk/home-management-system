package testing_test

import (
	"testing"

	sim "home-management-system/internal/tui/testing"
)

// The golden frames for 10a, captured after sign-off.
//
// They mean UNCHANGED, never good. What makes them worth anything is that the
// layout they hold was reviewed by a person first -- docs/review/10a-table.md,
// accepted 2026-08-19 -- so a failure here says "this differs from what was
// approved" rather than "this differs from whatever came out first".
//
// A failure is not automatically a bug. It is a prompt to re-review the screen
// and, if the change is wanted, re-capture with -update-golden.

func TestGoldenHoldingsTable(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	for _, tc := range []struct {
		name          string
		width, height int
	}{
		{"10a-holdings-100", 100, 22},
		// 60 columns is where the layout has to shed something, so it is the
		// frame most worth guarding.
		{"10a-holdings-60", 60, 22},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s.Resize(tc.width, tc.height)
			s.Send(sim.Press("4"))
			s.AssertFrame(tc.name)
		})
	}
}

// The selected state has its own frame: the gutter marks are the part of a
// selection that survives a monochrome terminal, and they are what a
// plain-text golden can hold.
func TestGoldenHoldingsWithASelection(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(100, 22)
	s.Send(sim.Press("4"))
	s.Send(sim.Space, sim.Space, sim.Space)
	s.AssertFrame("10a-holdings-selected")
}

func TestGoldenItemsTable(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(100, 22)
	s.Send(sim.Press("3"))
	s.AssertFrame("10a-items-100")
}

// 10b and 10c, provisionally accepted 2026-08-20 with more UI shifts expected.
//
// Captured anyway, and on purpose: 10d edits these same screens, and what a
// golden is for here is the change nobody intended. A deliberate shift means
// re-capturing, which is one command and a legible diff.

func TestGoldenLocationTree(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(84, 22)
	s.Send(sim.Press("2"))
	s.AssertFrame("10b-locations-84")

	// Collapsed is the overview, and it is a different layout question.
	s.Send(sim.ShiftTab)
	s.AssertFrame("10b-locations-collapsed")
}

func TestGoldenFilterAndJumpDoNotLookAlike(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(90, 20)

	s.Send(sim.Press("4"), sim.CtrlS)
	s.Send(sim.Type("loc:tray"))
	s.Send(sim.Enter)
	s.AssertFrame("10c-filtered")

	s.Send(sim.Esc)
	s.Send(sim.AltG)
	s.Send(sim.Type("shelf"))
	s.AssertFrame("10c-jump")
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
	s.Send(sim.Press("2"))
	s.Send(sim.CtrlN, sim.CtrlN)
	s.Send(sim.Press("e"))
	s.AssertFrame("10d-editor-inline")
}

func TestGoldenTheConfirmation(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(92, 18)
	s.Send(sim.Press("4"), sim.AltX)
	s.Send(sim.Type("new item Turmeric counting measured unit g package 2000 category Spices"))
	s.Send(sim.Enter)
	s.AssertFrame("10d-confirmation")
}

func TestGoldenARefusal(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(64, 18)
	s.Send(sim.Press("4"), sim.AltX)
	s.Send(sim.Type("consume 5kg"))
	s.Send(sim.Enter)
	s.AssertFrame("10d-refusal-wrapped")
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
	s.Send(sim.Press("4"), sim.CtrlS)
	s.Send(sim.Type("thunder"))
	s.Send(sim.Enter)
	s.Send(sim.CtrlK)
	s.AssertFrame("10f-retire-confirmation")
}
