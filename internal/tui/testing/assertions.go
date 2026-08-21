package testing

import (
	"strings"

	"home-management-system/internal/domain"
	"home-management-system/internal/query"
)

// The assertions.
//
// Two families, and the distinction is the point of the harness. The ones about
// the SCREEN look at View(); the ones about DATA look at the database. A test
// presses keys and then checks the database, so the input and the assertion sit
// on opposite sides of the whole stack and cannot both be satisfied by wiring
// that is wrong in the middle.

// ---------------------------------------------------------------------------
// The screen
// ---------------------------------------------------------------------------

// ShowsText fails unless the rendered view contains text.
func (s *Simulator) ShowsText(text string) {
	s.t.Helper()
	if !strings.Contains(s.View(), text) {
		s.t.Errorf("expected the screen to show %q; it showed:\n%s", text, s.View())
	}
}

// HidesText fails if the rendered view contains text.
func (s *Simulator) HidesText(text string) {
	s.t.Helper()
	if strings.Contains(s.View(), text) {
		s.t.Errorf("expected the screen NOT to show %q; it showed:\n%s", text, s.View())
	}
}

// FitsWidth fails if any line is wider than the terminal.
//
// Narrow terminals are where truncation breaks, and a line that overflows wraps
// -- which shifts every row below it and makes the whole screen unreadable
// rather than merely ugly.
func (s *Simulator) FitsWidth(width int) {
	s.t.Helper()
	for i, line := range strings.Split(s.View(), "\n") {
		if w := visibleWidth(line); w > width {
			s.t.Errorf("line %d is %d columns wide in a %d-column terminal: %q",
				i+1, w, width, line)
		}
	}
}

// visibleWidth counts printable columns, ignoring ANSI escape sequences, which
// carry colour and occupy no space on screen.
func visibleWidth(line string) int {
	n, inEscape := 0, false
	for _, r := range line {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
		default:
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// The database, on the far side of the whole stack
// ---------------------------------------------------------------------------

// HasCategory fails unless a Category of that name exists and is active.
func (s *Simulator) HasCategory(name string) domain.CategoryID {
	s.t.Helper()
	nodes, err := s.r.CategoryForest(s.ctx)
	if err != nil {
		s.t.Fatalf("read categories: %v", err)
	}
	for _, n := range nodes {
		if n.Category.Name == name && !n.Category.IsArchived() {
			return n.Category.ID
		}
	}
	s.t.Errorf("no active category called %q; there are %s", name, names(nodes))
	return 0
}

// HasLocation fails unless a Location of that name exists and is active.
func (s *Simulator) HasLocation(name string) domain.LocationID {
	s.t.Helper()
	nodes, err := s.r.LocationForest(s.ctx)
	if err != nil {
		s.t.Fatalf("read locations: %v", err)
	}
	for _, n := range nodes {
		if n.Location.Name == name && !n.Location.IsArchived() {
			return n.Location.ID
		}
	}
	var have []string
	for _, n := range nodes {
		have = append(have, n.Location.Name)
	}
	s.t.Errorf("no active location called %q; there are %s", name, strings.Join(have, ", "))
	return 0
}

// HasItem fails unless an Item of that name exists and is active.
func (s *Simulator) HasItem(name string) domain.ItemID {
	s.t.Helper()
	items, err := s.r.Items(s.ctx)
	if err != nil {
		s.t.Fatalf("read items: %v", err)
	}
	for _, it := range items {
		if it.Base().Name == name && it.Base().ArchivedAt == nil {
			return it.Base().ID
		}
	}
	var have []string
	for _, it := range items {
		have = append(have, it.Base().Name)
	}
	s.t.Errorf("no active item called %q; there are %s", name, strings.Join(have, ", "))
	return 0
}

// HasNoItem fails if an active Item of that name exists.
func (s *Simulator) HasNoItem(name string) {
	s.t.Helper()
	items, err := s.r.Items(s.ctx)
	if err != nil {
		s.t.Fatalf("read items: %v", err)
	}
	for _, it := range items {
		if it.Base().Name == name && it.Base().ArchivedAt == nil {
			s.t.Errorf("item %q exists and should not", name)
		}
	}
}

// OnHand fails unless the Item's total across every active Holding is want.
func (s *Simulator) OnHand(item domain.ItemID, want int64) {
	s.t.Helper()
	details, err := s.r.HoldingsOfItem(s.ctx, item)
	if err != nil {
		s.t.Fatalf("read holdings: %v", err)
	}
	var total int64
	for _, d := range details {
		b, ok := d.Holding.(domain.BulkHolding)
		if ok && b.RetiredAt == nil {
			total += b.Quantity.Milli()
		}
	}
	if total != want {
		s.t.Errorf("on hand = %d milli, want %d", total, want)
	}
}

// RecordedEvents is the assertion the legacy harness could not make.
//
// It could only ask what is true NOW. This asks what was RECORDED, which is the
// only way to test the interesting operations at all: Consume reaching the right
// quantity by the wrong path -- adjusting instead of opening -- is invisible to
// a state assertion and obvious to this one.
func (s *Simulator) RecordedEvents(holding domain.HoldingID, want ...string) {
	s.t.Helper()
	rows, err := s.ctrlHistory(holding)
	if err != nil {
		s.t.Fatalf("read history: %v", err)
	}
	if len(rows) != len(want) {
		s.t.Errorf("holding %d recorded %v, want %v", holding, rows, want)
		return
	}
	for i, w := range want {
		if rows[i] != w {
			s.t.Errorf("holding %d recorded %v, want %v", holding, rows, want)
			return
		}
	}
}

// ctrlHistory reads what was recorded through the Controller, so the harness
// sees exactly what the interface sees rather than reaching past it.
func (s *Simulator) ctrlHistory(id domain.HoldingID) ([]string, error) {
	rows, err := s.ctrl.HoldingHistory(s.ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Type)
	}
	return out, nil
}

func names(nodes []query.Node) string {
	var have []string
	for _, n := range nodes {
		have = append(have, n.Category.Name)
	}
	return strings.Join(have, ", ")
}

// CountHoldings is how many Holdings exist, for the tests that assert browsing
// changed nothing.
func (s *Simulator) CountHoldings() int {
	s.t.Helper()
	holdings, err := s.r.Holdings(s.ctx)
	if err != nil {
		s.t.Fatalf("read holdings: %v", err)
	}
	return len(holdings)
}

// EventTypes is what was recorded against a Holding, in sequence order.
func (s *Simulator) EventTypes(id domain.HoldingID) []string {
	s.t.Helper()
	rows, err := s.ctrlHistory(id)
	if err != nil {
		s.t.Fatalf("read history: %v", err)
	}
	return rows
}
