package testing_test

import (
	"strings"
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ops"
	sim "home-management-system/internal/tui/testing"
)

// 10a through the Simulator: the table against a real house, with the awkward
// cases in it.
//
// The widget's own behaviour is tested in internal/tui/table -- "does j move
// the cursor" needs no database. What needs one is whether the table shows the
// right thing when it is fed by the real controller over real data, which is a
// different question and the one that has historically been wrong.

// awkwardHouse is the sample house's hard cases, built through ops: a very long
// name five levels down, and one item kept in three places.
func awkwardHouse(t *testing.T, s *sim.Simulator) {
	t.Helper()
	p, ctx := s.Planner(), s.Context()

	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Garage"}))
	garage := s.HasLocation("Garage")
	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Metal Shelving Unit", Parent: &garage}))
	shelving := s.HasLocation("Metal Shelving Unit")
	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Bay 3", Parent: &shelving}))
	bay := s.HasLocation("Bay 3")
	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Blue Crate", Parent: &bay}))
	crate := s.HasLocation("Blue Crate")
	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Small Parts Tray", Parent: &crate}))
	tray := s.HasLocation("Small Parts Tray")
	s.Apply(p.NewLocation(ctx, ops.NewLocationRequest{Name: "Left Pantry"}))
	pantry := s.HasLocation("Left Pantry")

	s.Apply(p.NewCategory(ctx, ops.NewCategoryRequest{Name: "Spices"}))
	spices := s.HasCategory("Spices")

	s.Apply(p.NewItem(ctx, ops.NewItemRequest{
		Name:     "Thunderbolt 4 to Dual DisplayPort 1.4 Adapter (Space Grey, 0.8m)",
		Category: spices, Counting: ops.CountingUnique,
	}))
	adapter := s.HasItem("Thunderbolt 4 to Dual DisplayPort 1.4 Adapter (Space Grey, 0.8m)")
	s.Apply(p.NewHolding(ctx, ops.NewHoldingRequest{Item: adapter, Location: tray}))

	s.Apply(p.NewItem(ctx, ops.NewItemRequest{
		Name: "Ancho Chile", Category: spices, Counting: ops.CountingMeasured, ContentUnit: "g",
	}))
	ancho := s.HasItem("Ancho Chile")
	for _, at := range []domain.LocationID{pantry, garage, tray} {
		s.Apply(p.Receive(ctx, ops.ReceiveRequest{
			Item: ancho, Location: at, Basis: domain.BasisContent,
			Amount: domain.FromMilli(100 * domain.Scale), Source: "bulk order",
		}))
	}
}

// TestTheHoldingsTableShowsWhatIsThere is the baseline: real data reaches the
// real widget through the real controller.
func TestTheHoldingsTableShowsWhatIsThere(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.ByPlace()
	s.OnContents()
	s.ShowsText("ITEM")
	s.ShowsText("QTY")
	s.ShowsText("Ancho Chile")
	// One item in three places is three rows, not one.
	if got := s.CountRows("Ancho Chile"); got != 3 {
		t.Errorf("Ancho Chile is on %d rows, want 3 -- it is kept in three places", got)
	}
}

// The long name and the deep path meet on one row, which is the layout's hard
// question. What must survive is the IDENTIFYING part: a name cut to
// "Thunderb…" is not a name.
func TestALongNameStaysIdentifiable(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.ByPlace()
	s.OnContents()
	for _, width := range []int{80, 120, 200} {
		s.Resize(width, 30)
		s.ByPlace()
		s.OnContents()
		if !strings.Contains(s.View(), "Thunderbolt") {
			t.Errorf("at %d columns the adapter is unidentifiable:\n%s", width, s.View())
		}
		s.FitsWidth(width)
	}
}

// Sorting is a display decision and must touch nothing. The Simulator's
// VerifyAll cleanup would catch a write, but a count of holdings catches it
// sooner and says what happened.
func TestSortingAndSelectingWriteNothing(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	before := s.CountHoldings()

	s.ByPlace()
	s.OnContents()
	s.Send(sim.Press("s"), sim.Press("s"), sim.CtrlF, sim.Press("s"))
	s.Send(sim.Space, sim.Space, sim.AltH, sim.Esc)
	s.Send(sim.AltGreat, sim.AltLess)

	if after := s.CountHoldings(); after != before {
		t.Errorf("browsing changed the holdings from %d to %d", before, after)
	}
}

// The cursor must stay on screen through every motion, at every height. A
// cursor that scrolls out of view is a cursor you act with by accident.
func TestTheCursorIsAlwaysVisible(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	for _, height := range []int{10, 14, 30} {
		s.Resize(100, height)
		s.ByPlace()
		s.OnContents()
		for _, script := range [][]any{
			{sim.AltGreat},
			{sim.AltLess},
			{sim.CtrlD},
			{sim.CtrlD, sim.CtrlD},
			{sim.CtrlU},
			{sim.Press("z"), sim.Press("z")},
		} {
			s.Send(script...)
			if !s.ShowsCursor() {
				t.Errorf("at height %d the cursor is off screen after %v:\n%s",
					height, script, stripEscapes(s.View()))
			}
		}
	}
}

// Every view fits every width, with the awkward cases in it. This is the check
// that catches the failure that makes all other layout failures unreadable.
func TestEveryViewFitsEveryWidth(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)

	for _, width := range []int{60, 80, 120, 200} {
		s.Resize(width, 30)
		for _, view := range []string{"1", "2", "3", "4", "5"} {
			s.Send(sim.Press(view))
			s.FitsWidth(width)
		}
	}
}

func stripEscapes(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// The LOCATION column says WHERE, not just which shelf.
//
// Three rows of Ancho Chile in three different places is the question the
// holdings table exists to answer, and the leaf name cannot: two of them read
// "Garage" and "Small Parts Tray" with nothing to say the tray is inside the
// garage. Given a terminal with room to spare, the column says so.
//
// Through the Simulator rather than against the widget, because the widget
// tests supply the path themselves -- what is being checked here is that the
// controller assembles one at all, and that it agrees with the tree.
func TestTheLocationColumnShowsTheWholeTreeWhenThereIsRoom(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(220, 24)
	s.ByPlace()
	s.OnContents()
	s.ShowsText("Garage > Metal Shelving Unit > Bay 3 > Blue Crate > Small Parts Tray")

	// A root has no ancestors and must not grow an ellipsis pretending it has.
	if strings.Contains(s.PlainView(), "… > Left Pantry") {
		t.Errorf("a root location was rendered as though it had a parent:\n%s", s.PlainView())
	}
}

// Narrow, it is the leaf again -- so a cramped terminal loses nothing it used
// to have, and the ancestors cost nothing when there is no room for them.
func TestTheLocationColumnGivesBackTheTreeWhenThereIsNot(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(80, 24)
	s.ByPlace()
	s.OnContents()
	s.ContentsShow("Small Parts Tray")
	s.ContentsHide("Metal Shelving Unit")
}

// The path is shown, never matched. `loc:garage` means the Garage itself, not
// everything hanging beneath it -- the cell is what the filter reads, and the
// path is only how the cell is drawn.
func TestTheLocationPathIsNotFiltered(t *testing.T) {
	s := sim.New(t)
	awkwardHouse(t, s)
	s.Resize(220, 24)
	s.ByPlace()
	s.OnContents()
	s.Send(sim.CtrlS)
	s.Send(sim.Type("loc:garage"))
	s.Send(sim.Enter)

	// One Ancho is IN the Garage; another is in a tray five levels beneath it,
	// and its path says "Garage" on screen without that making it a match.
	if got := strings.Count(s.PlainView(), "Ancho Chile"); got != 1 {
		t.Errorf("loc:garage matched %d Ancho rows through the path, want the 1 in the Garage itself", got)
	}
}
