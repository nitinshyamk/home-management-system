package testing_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
	sim "home-management-system/internal/tui/testing"
)

// The harness has to be shown to work before anything is tested with it. A
// broken harness does not fail -- it passes, quietly, for the wrong reason.

// TestKeysGoInAndTheDatabaseComesOut is the property the whole harness exists
// for: the input and the assertion are on opposite sides of the stack.
func TestKeysGoInAndTheDatabaseComesOut(t *testing.T) {
	s := sim.New(t)
	seedHouse(t, s)

	s.Send(sim.Press("2")) // the Locations view
	s.ShowsText("Pantry")

	s.Send(sim.Press("4")) // the Holdings view
	s.ShowsText("Basmati Rice")
}

// TestBatchMsgIsExpanded is the second porting change, tested directly rather
// than trusted.
//
// v01's helper ran cmd() once and handed the result straight to Update, so a
// tea.BatchMsg reached a model with no case for it and was silently dropped --
// along with every command inside it. v01 never noticed because it never
// batches; v02 does immediately.
//
// This drives the expansion through a hand-built batch, because waiting for the
// application to produce one would make the test depend on a batching that does
// not exist yet. It goes through the Simulator's own loop rather than a helper:
// the first version of this test called a separate flatten(), and deleting the
// expansion from runCmd left it passing.
func TestBatchMsgIsExpanded(t *testing.T) {
	s := sim.New(t)

	var ran []string
	s.RunCommand(tea.Batch(
		func() tea.Msg { ran = append(ran, "first"); return nil },
		func() tea.Msg { ran = append(ran, "second"); return nil },
	))

	if len(ran) != 2 || ran[0] != "first" || ran[1] != "second" {
		t.Errorf("the batch ran %v, want both halves in order", ran)
	}
}

// TestVerifyAllRunsAfterEveryTest is the postcondition no test author has to
// think of. Proving it fires means corrupting a projection and watching the
// cleanup complain -- which cannot be done from inside a passing test, so what
// is checked here is that a clean run reports clean.
//
// The failing direction is verified by breaking it, recorded in the commit.
func TestVerifyAllRunsAfterEveryTest(t *testing.T) {
	s := sim.New(t)
	seedHouse(t, s)
	// Nothing else: the assertion is in t.Cleanup.
}

// TestTheViewFitsNarrowTerminals is the check that catches the layout bug that
// makes every other layout bug unreadable -- a line that overflows wraps, which
// shifts every row below it.
func TestTheViewFitsNarrowTerminals(t *testing.T) {
	s := sim.New(t)
	seedHouse(t, s)

	for _, width := range []int{60, 80, 120, 200} {
		s.Resize(width, 30)
		for _, view := range []string{"1", "2", "3", "4", "5"} {
			s.Send(sim.Press(view))
			s.FitsWidth(width)
		}
	}
}

// TestTheViewNeverPanics walks every view at every width, which is worth its
// own name because a panic in View() takes the whole application down.
func TestTheViewNeverPanics(t *testing.T) {
	s := sim.New(t)
	seedHouse(t, s)
	for _, width := range []int{20, 60, 200} {
		s.Resize(width, 10)
		for _, key := range []string{"1", "2", "3", "4", "5", "j", "k", "G", "r"} {
			s.Send(sim.Press(key))
			if strings.TrimSpace(s.View()) == "" {
				t.Fatalf("the view is empty at %d columns after %q", width, key)
			}
		}
	}
}

// seedHouse builds enough of a house to look at, through the OPERATIONS layer
// rather than through keystrokes.
//
// That is the legacy system's pragmatic choice and it is the right one:
// otherwise every test is fifty keystrokes of setup and the keystrokes under
// test are lost in them.
func seedHouse(t *testing.T, s *sim.Simulator) {
	t.Helper()
	p, ctx := s.Planner(), s.Context()

	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Left Pantry"}))
	pantry := s.HasLocation("Left Pantry")

	s.Apply(p.NewCategory(ctx, ops.NewCategoryRequest{Name: "Grains"}))
	grains := s.HasCategory("Grains")

	size := domain.FromMilli(2_000_000)
	s.Apply(p.NewItem(ctx, ops.NewItemRequest{
		Name: "Basmati Rice", Category: grains, Counting: ops.CountingMeasured,
		ContentUnit: "g", PackageSize: &size,
	}))
	rice := s.HasItem("Basmati Rice")

	s.Apply(p.Receive(ctx, ops.ReceiveRequest{
		Item: rice, Location: pantry, Basis: domain.BasisPackage,
		Amount: domain.FromMilli(2 * domain.Scale), Source: "corner shop",
	}))
}
