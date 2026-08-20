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
