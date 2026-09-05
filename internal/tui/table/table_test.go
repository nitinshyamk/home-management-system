package table_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	tuikeys "home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/table"
)

// The widget crosses no boundary, so it is tested here rather than through the
// Simulator: "does C-n move the cursor" needs no database. Everything that
// crosses a boundary is tested through the Simulator instead.

func columns() []table.Column {
	return []table.Column{
		{Title: "ITEM", Min: 8, Grow: true},
		{Title: "QTY", Min: 4, Align: table.Right, Drop: 0},
		{Title: "LOCATION", Min: 8, Drop: 2, Elide: table.ElideStart},
		{Title: "EXPIRES", Min: 7, Drop: 3},
	}
}

func rows(n int) []table.Row {
	out := make([]table.Row, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, table.Row{
			Key:   int64(i + 1),
			Cells: []string{name(i), "100 g", "Left Pantry", "2027-03"},
		})
	}
	return out
}

func name(i int) string {
	return string(rune('A'+i%26)) + " Item"
}

func newTable(n int) table.Model {
	return table.New(columns()).SetRows(rows(n)).SetSize(80, 12)
}

// press names keys the way the keymap names them, so a test cannot press a key
// the interface does not bind.
func press(m table.Model, names ...string) table.Model {
	for _, name := range names {
		msg, ok := tuikeys.Named(name)
		if !ok {
			panic(name + " is not a key")
		}
		m, _ = m.Update(msg)
	}
	return m
}

func TestMotion(t *testing.T) {
	m := newTable(20)
	if got := press(m, "ctrl+n", "ctrl+n", "ctrl+n").Cursor(); got != 3 {
		t.Errorf("three C-n put the cursor at %d, want 3", got)
	}
	if got := press(m, "ctrl+p").Cursor(); got != 0 {
		t.Errorf("C-p from the top moved to %d, want 0 -- the cursor left the table", got)
	}
	if got := press(m, "alt+>").Cursor(); got != 19 {
		t.Errorf("M-> put the cursor at %d, want the last row", got)
	}
	if got := press(m, "alt+>", "ctrl+n").Cursor(); got != 19 {
		t.Errorf("C-n from the bottom moved to %d, want the last row", got)
	}
	if got := press(m, "alt+>", "alt+<").Cursor(); got != 0 {
		t.Errorf("M-< put the cursor at %d, want the first row", got)
	}
	// An unbound letter is not motion, and not a half-finished gesture either.
	// The two-key sequences are gone: there is no state between keystrokes for
	// a stray key to be the first half of.
	if got := press(m, "ctrl+n", "ctrl+n", "g", "z", "d").Cursor(); got != 2 {
		t.Errorf("unbound letters moved the cursor to %d, want 2", got)
	}
}

func TestMotionOnAnEmptyTable(t *testing.T) {
	m := table.New(columns()).SetSize(80, 12)
	if got := press(m, "ctrl+n", "ctrl+p", "alt+>", "alt+<", "ctrl+space").Cursor(); got != -1 {
		t.Errorf("cursor = %d on an empty table, want -1", got)
	}
	if !strings.Contains(m.View(), "nothing here") {
		t.Errorf("an empty table renders as:\n%s", m.View())
	}
}

func TestSelection(t *testing.T) {
	m := newTable(10)
	// space selects AND advances, so picking three in a row is three presses.
	m = press(m, "ctrl+space", "ctrl+space", "ctrl+space")
	if got := m.SelectionCount(); got != 3 {
		t.Errorf("selected %d rows, want 3", got)
	}
	if got := m.Selected(); len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("selected keys = %v, want the first three", got)
	}
	// Toggling off.
	m = press(m, "ctrl+p", "ctrl+space")
	if got := m.SelectionCount(); got != 2 {
		t.Errorf("after toggling off, %d rows selected, want 2", got)
	}
	if got := press(m, "esc").SelectionCount(); got != 0 {
		t.Errorf("esc left %d rows selected", got)
	}
}

// With nothing picked, "act on the selection" and "act on this row" are the
// same gesture. But a status line must not claim a selection nobody made, which
// is why the count is separate.
func TestSelectedFallsBackToTheCursorButTheCountDoesNot(t *testing.T) {
	m := press(newTable(10), "ctrl+n", "ctrl+n")
	if got := m.Selected(); len(got) != 1 || got[0] != 3 {
		t.Errorf("with nothing selected, Selected() = %v, want the cursor row", got)
	}
	if got := m.SelectionCount(); got != 0 {
		t.Errorf("SelectionCount() = %d with nothing selected", got)
	}
}

// A sort that moves the row out from under the cursor is how you act on the
// wrong thing.
func TestSortKeepsTheCursorOnItsRow(t *testing.T) {
	m := newTable(10)
	m = press(m, "ctrl+n", "ctrl+n", "ctrl+n")
	before, _ := m.Current()

	m = press(m, "s")
	after, ok := m.Current()
	if !ok || after.Key != before.Key {
		t.Errorf("after sorting the cursor is on %v, want %v", after.Key, before.Key)
	}
	// Column 0 already IS the sort, so s reverses it.
	if !strings.Contains(strip(m.View()), "v") {
		t.Errorf("a reversed sort does not say so:\n%s", strip(m.View()))
	}
	m = press(m, "s")
	if !strings.Contains(strip(m.View()), "^") {
		t.Errorf("sorting back does not say so:\n%s", strip(m.View()))
	}
	if again, _ := m.Current(); again.Key != before.Key {
		t.Errorf("reversing moved the cursor off its row")
	}
}

// A table always has an order, so the only question is whether it says what
// that order is. It says so from the first frame, before anything is pressed.
func TestTheOrderIsStatedBeforeAnythingIsPressed(t *testing.T) {
	m := newTable(5)
	view := strip(m.View())
	if !strings.Contains(view, "ITEM ^") {
		t.Errorf("the initial order is not stated in the header:\n%s", view)
	}
	if got := m.SortDescription(); got != "by item a-z" {
		t.Errorf("SortDescription() = %q, want a plain statement of the order", got)
	}
	// And the rows really are in that order, rather than merely claiming to be.
	rows := m.Rows()
	for i := 1; i < len(rows); i++ {
		if rows[i-1].Cells[0] > rows[i].Cells[0] {
			t.Fatalf("the header says a-z and the rows are not: %q before %q",
				rows[i-1].Cells[0], rows[i].Cells[0])
		}
	}
	// A narrow terminal can drop the sorted column, and then the header cannot
	// say it -- which is why the description exists separately.
	narrow := m.SetSize(30, 12)
	if narrow.SortDescription() == "" {
		t.Error("a narrow table cannot say how it is ordered")
	}
}

func TestSortOrdersByTheFocusedColumn(t *testing.T) {
	m := table.New(columns()).SetSize(80, 12).SetRows([]table.Row{
		{Key: 1, Cells: []string{"Cumin", "90 g", "Shelf 2", ""}},
		{Key: 2, Cells: []string{"Ancho", "200 g", "Shelf 2", ""}},
		{Key: 3, Cells: []string{"Basmati", "800 g", "Pantry", ""}},
	})
	// Already sorted by column 0 ascending on arrival.
	got := []int64{}
	for _, r := range m.Rows() {
		got = append(got, r.Key)
	}
	if len(got) != 3 || got[0] != 2 || got[1] != 3 || got[2] != 1 {
		t.Errorf("sorted keys = %v, want Ancho, Basmati, Cumin", got)
	}
}

// A line wider than the terminal wraps, and one wrapped line shifts every row
// below it. Overflowing is never the lesser evil.
func TestTheTableNeverOverflows(t *testing.T) {
	long := table.Row{Key: 99, Cells: []string{
		"Thunderbolt 4 to Dual DisplayPort 1.4 Adapter (Space Grey, 0.8m)",
		"1", "Garage > Metal Shelving Unit > Bay 3 > Blue Crate > Small Parts Tray",
		"2027-03-01",
	}}
	for _, width := range []int{20, 40, 60, 80, 120, 200} {
		m := table.New(columns()).SetRows(append(rows(5), long)).SetSize(width, 12)
		for i, line := range strings.Split(m.View(), "\n") {
			if w := len([]rune(strip(line))); w > width {
				t.Errorf("at %d columns, line %d is %d wide: %q", width, i+1, w, strip(line))
			}
		}
	}
}

// Columns are dropped in the order declared, so what a narrow terminal loses is
// a decision rather than an accident.
func TestNarrowTerminalsDropColumnsInPriorityOrder(t *testing.T) {
	m := table.New(columns()).SetRows(rows(3))

	wide := strip(m.SetSize(120, 12).View())
	if !strings.Contains(wide, "EXPIRES") || !strings.Contains(wide, "LOCATION") {
		t.Fatalf("at 120 columns something is already missing:\n%s", wide)
	}
	// 30 columns cannot hold all four even truncated to their minimums.
	narrow := strip(m.SetSize(30, 12).View())
	if strings.Contains(narrow, "EXPIRES") {
		t.Errorf("EXPIRES survived at 30 columns; it has the highest Drop:\n%s", narrow)
	}
	if !strings.Contains(narrow, "QTY") {
		t.Errorf("QTY was dropped though its Drop is 0:\n%s", narrow)
	}
}

// TestATruncatableColumnIsNotDropped is the regression for the layout's first
// version, which decided what to drop from NATURAL widths.
//
// A location column's natural width is the longest path in the house -- sixty
// characters -- so LOCATION and FLAGS both vanished from a hundred-column
// terminal that had ample room for them truncated. Three rows of one item in
// three places then became indistinguishable, which is the exact question a
// holdings table exists to answer.
func TestATruncatableColumnIsNotDropped(t *testing.T) {
	// Both awkward cases at once, which is the state the sample house is pinned
	// to be in and the only arrangement that reproduces the bug: a long name
	// and a deep path together exceed a hundred columns at their natural
	// widths, so the old layout dropped rather than truncated.
	deep := "Garage > Metal Shelving Unit > Bay 3 > Blue Crate > Small Parts Tray"
	long := "Thunderbolt 4 to Dual DisplayPort 1.4 Adapter (Space Grey, 0.8m)"
	m := table.New(columns()).SetSize(100, 12).SetRows([]table.Row{
		{Key: 1, Cells: []string{long, "1", deep, "2027-03"}},
		{Key: 2, Cells: []string{"Ancho Chile", "120 g", deep, "2027-03"}},
		{Key: 3, Cells: []string{"Ancho Chile", "120 g", "Garage", "2027-03"}},
	})
	view := strip(m.View())
	for _, want := range []string{"LOCATION", "EXPIRES", "Small Parts Tray", "Garage"} {
		if !strings.Contains(view, want) {
			t.Errorf("a hundred-column table is missing %q; it had room for it truncated:\n%s",
				want, view)
		}
	}
}

// A path identifies by its LEAF, so it is cut at the front: "Garage > Metal
// Shelvin…" says almost nothing, while "…> Blue Crate > Small Parts Tray" says
// where the thing is.
func TestAPathIsCutAtTheFront(t *testing.T) {
	m := table.New([]table.Column{{Title: "LOCATION", Min: 10, Elide: table.ElideStart}}).
		SetSize(24, 6).
		SetRows([]table.Row{{Key: 1, Cells: []string{
			"Garage > Metal Shelving Unit > Bay 3 > Blue Crate > Small Parts Tray"}}})
	view := strip(m.View())
	if !strings.Contains(view, "Small Parts Tray") {
		t.Errorf("the path lost its leaf, which is the part that identifies it:\n%s", view)
	}
}

// A truncated name has to stay identifiable, and the ellipsis is what says it
// was cut -- without one, a shortened name reads as a name that is simply short.
func TestTruncationIsMarkedAndLeftAnchored(t *testing.T) {
	m := table.New([]table.Column{{Title: "ITEM", Min: 8}}).SetSize(20, 6).SetRows([]table.Row{
		{Key: 1, Cells: []string{"Thunderbolt 4 to Dual DisplayPort 1.4 Adapter"}},
	})
	view := strip(m.View())
	if !strings.Contains(view, "Thunderbolt") {
		t.Errorf("the truncation cut away the identifying part:\n%s", view)
	}
	if !strings.Contains(view, "…") {
		t.Errorf("a truncated cell does not say it was truncated:\n%s", view)
	}
}

// Numbers are elided at the FRONT, because a number cut from the right has lost
// its magnitude and reads as a different, smaller number.
func TestNumbersKeepTheirLeastSignificantEnd(t *testing.T) {
	m := table.New([]table.Column{{Title: "QTY", Min: 3, Align: table.Right, Elide: table.ElideStart}}).
		SetSize(8, 6).
		SetRows([]table.Row{{Key: 1, Cells: []string{"123456789"}}})
	view := strip(m.View())
	if !strings.Contains(view, "789") {
		t.Errorf("a truncated number lost its last digits, so it reads smaller than it is:\n%s", view)
	}
}

func TestScrollingKeepsTheCursorOnScreen(t *testing.T) {
	m := newTable(50).SetSize(80, 10)
	for _, gesture := range [][]string{
		{"alt+>"}, {"alt+<"}, {"ctrl+v"}, {"ctrl+v", "ctrl+v"}, {"alt+v"},
	} {
		m = press(m, gesture...)
		lines := strings.Split(strip(m.View()), "\n")
		found := false
		for _, line := range lines {
			if strings.HasPrefix(line, ">") {
				found = true
			}
		}
		if !found {
			t.Errorf("after %v the cursor is off screen:\n%s", gesture, strip(m.View()))
		}
	}
}

// SetRows must not strand the cursor past the end, which is what happens when a
// filter narrows the table under it.
func TestReplacingRowsKeepsTheCursorInBounds(t *testing.T) {
	m := press(newTable(20), "alt+>")
	m = m.SetRows(rows(3))
	if got := m.Cursor(); got != 2 {
		t.Errorf("cursor = %d after the table shrank to 3 rows", got)
	}
	// And a selection of rows that are gone does not linger.
	m = press(newTable(20), "ctrl+space", "ctrl+space").SetRows(rows(1))
	if got := m.SelectionCount(); got != 1 {
		t.Errorf("%d rows selected after the table shrank; the others no longer exist", got)
	}
}

func TestUnhandledKeysFallThrough(t *testing.T) {
	m := newTable(5)
	if _, handled := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); handled {
		t.Error("the table swallowed q; the parent can never quit")
	}
	// esc with nothing selected belongs to the parent, so it can leave a mode.
	if _, handled := m.Update(tea.KeyMsg{Type: tea.KeyEscape}); handled {
		t.Error("the table swallowed esc with nothing selected")
	}
	withSelection := press(m, "ctrl+space")
	if _, handled := withSelection.Update(tea.KeyMsg{Type: tea.KeyEscape}); !handled {
		t.Error("esc did not clear the selection")
	}
}

func TestColumnFocusMoves(t *testing.T) {
	m := newTable(5)
	if got := press(m, "ctrl+f", "ctrl+f").FocusedColumn(); got != 2 {
		t.Errorf("ll focused column %d, want 2", got)
	}
	if got := press(m, "ctrl+b").FocusedColumn(); got != 0 {
		t.Errorf("h from the first column focused %d, want 0", got)
	}
	if got := press(m, "ctrl+f", "ctrl+f", "ctrl+f", "ctrl+f", "ctrl+f", "ctrl+f").FocusedColumn(); got != 3 {
		t.Errorf("l past the last column focused %d, want the last", got)
	}
}

func strip(s string) string {
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

// The styling tests assert that a DISTINCTION exists, not which escape codes
// make it. Pinning the codes would make every visual adjustment a test failure,
// which is how a test stops being a guard and becomes a tax.

// Uniform rows are hard to read a whole screenful of at once -- the eye has
// nothing to track along. Banding costs no vertical space, which is the other
// thing a dense table cannot spare.
func TestAdjacentRowsAreVisuallyDistinct(t *testing.T) {
	defer withColour()()
	// The cursor is parked on the last row, so the rows being compared carry
	// only their banding. Leaving it on row 0 compares a bold row with a plain
	// one and passes for the wrong reason.
	lines := renderedRows(press(newTable(6), "alt+>"))
	if len(lines) < 4 {
		t.Fatalf("got %d rows", len(lines))
	}
	first, second := styleOf(lines[0]), styleOf(lines[1])
	if first == second {
		t.Error("adjacent rows are styled identically; there is nothing for the eye to track along")
	}
	// Every other row, so the banding is regular rather than arbitrary.
	if styleOf(lines[2]) != first || styleOf(lines[3]) != second {
		t.Errorf("the banding is not every-other-row: %q %q %q %q",
			first, second, styleOf(lines[2]), styleOf(lines[3]))
	}
}

// Selection has to be legible as a SET, from anywhere on the screen, without
// competing with the cursor for attention.
func TestASelectedRowIsMarkedBeyondItsGutter(t *testing.T) {
	defer withColour()()
	// Select rows 0 and 1 -- one on each band -- then park the cursor clear of
	// both, so what is compared is selection against nothing rather than
	// selection against the cursor.
	m := press(newTable(8), "ctrl+space", "ctrl+space", "alt+>")
	lines := renderedRows(m)

	if !strings.Contains(strip(lines[0]), "*") {
		t.Errorf("a selected row has no gutter mark: %q", strip(lines[0]))
	}
	// And something beyond the mark, since one character among thirty rows is
	// easy to lose. Checked on BOTH bands: a selection that is only visible on
	// the unbanded half is a selection you can miss half the time.
	for _, tc := range []struct{ selected, unselected int }{{0, 2}, {1, 3}} {
		if styleOf(lines[tc.selected]) == styleOf(lines[tc.unselected]) {
			t.Errorf("a selected row is styled exactly like an unselected one on the same band:\n"+
				" selected   %q\n unselected %q", lines[tc.selected], lines[tc.unselected])
		}
	}

	// A run of selected rows is one solid block rather than a banded one. That
	// is what makes a selection countable at a glance, and it is the reason
	// selection suppresses the banding rather than layering on top of it.
	if styleOf(lines[0]) != styleOf(lines[1]) {
		t.Errorf("consecutive selected rows are banded, so the selection does not read as a block:\n"+
			" %q\n %q", lines[0], lines[1])
	}
}

// Selection differs from banding in HUE, not in degree. Saying both with the
// same dimension -- a bit darker, a bit darker still -- gives four shades where
// the eye can reliably separate two.
func TestSelectionIsNotJustAnotherShadeOfTheBanding(t *testing.T) {
	defer withColour()()
	m := press(newTable(8), "ctrl+space", "alt+>")
	lines := renderedRows(m)

	selected := styleOf(lines[0])
	banded := styleOf(lines[1])
	if selected == banded {
		t.Errorf("a selected row and a banded row are the same colour:\n %q\n %q",
			lines[0], lines[1])
	}
	if selected == "" {
		t.Errorf("a selected row on the unbanded half carries no colour at all: %q", lines[0])
	}
}

// withColour turns styling on for a test and restores the profile afterwards,
// since the render path sets it globally.
func withColour() func() {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	return func() { lipgloss.SetColorProfile(previous) }
}

// renderedRows returns the body lines, header excluded.
func renderedRows(m table.Model) []string {
	lines := strings.Split(m.View(), "\n")
	if len(lines) <= 1 {
		return nil
	}
	return lines[1:]
}

// styleOf is the escape sequence a line opens with, which is what makes two
// rows look different without saying which difference it should be.
func styleOf(line string) string {
	if !strings.HasPrefix(line, "\x1b") {
		return ""
	}
	end := strings.IndexByte(line, 'm')
	if end < 0 {
		return ""
	}
	return line[:end+1]
}

// ---------------------------------------------------------------------------
// Filtering
// ---------------------------------------------------------------------------

func filterable() table.Model {
	return table.New(columns()).SetSize(90, 14).SetRows([]table.Row{
		{Key: 1, Cells: []string{"Ancho Chile", "120 g", "Shelf 2", ""}},
		{Key: 2, Cells: []string{"Ancho Chile", "40 g", "Small Parts Tray", ""}},
		{Key: 3, Cells: []string{"Basmati Rice", "800 g", "Shelf 1", ""}},
		{Key: 4, Cells: []string{"Cumin", "90 g", "Shelf 2", "expiring"}},
	})
}

func keys(m table.Model) []int64 {
	var out []int64
	for _, r := range m.Rows() {
		out = append(out, r.Key)
	}
	return out
}

// A narrowed list that cannot say what it is narrowed FROM is a list that lies
// about what you own, which is why the table keeps every row and shows a
// subset rather than being handed a shorter list.
func TestFilteringKeepsTheTotal(t *testing.T) {
	m := filterable().SetFilter(table.Filter{Text: "ancho"})
	shown, total := m.Counts()
	if shown != 2 || total != 4 {
		t.Errorf("counts = %d of %d, want 2 of 4", shown, total)
	}
	if !m.Filtered() {
		t.Error("the table does not know it is filtered")
	}
	if got := keys(m); len(got) != 2 {
		t.Errorf("rows = %v", got)
	}
	// And clearing brings everything back.
	m = m.SetFilter(table.Filter{})
	if shown, _ := m.Counts(); shown != 4 {
		t.Errorf("clearing the filter left %d rows", shown)
	}
}

// A field restriction is a statement about a field, so it is a substring test.
// Fuzzy-matching it would make loc:garage quietly also mean the Great Room.
func TestAFacetRestrictsOneColumnExactly(t *testing.T) {
	m := filterable().SetFilter(table.Filter{
		Facets: []table.FacetTest{{Column: 2, Value: "Shelf 2"}},
	})
	if got := keys(m); len(got) != 2 || got[0] != 1 || got[1] != 4 {
		t.Errorf("loc:Shelf 2 matched %v, want the two on Shelf 2", got)
	}
	// The same text as free text reaches other columns; as a facet it does not.
	m = filterable().SetFilter(table.Filter{
		Facets: []table.FacetTest{{Column: 2, Value: "ancho"}},
	})
	if got := keys(m); len(got) != 0 {
		t.Errorf("a location facet matched on the item name: %v", got)
	}
}

func TestFacetsAndTextCompose(t *testing.T) {
	m := filterable().SetFilter(table.Filter{
		Facets: []table.FacetTest{{Column: 2, Value: "Shelf 2"}},
		Text:   "cumin",
	})
	if got := keys(m); len(got) != 1 || got[0] != 4 {
		t.Errorf("facet plus text matched %v, want just Cumin", got)
	}
}

// A filter that moves the cursor makes you re-find your place every keystroke.
func TestFilteringKeepsTheCursorOnItsRow(t *testing.T) {
	m := press(filterable(), "ctrl+n", "ctrl+n", "ctrl+n") // Cumin
	before, _ := m.Current()

	m = m.SetFilter(table.Filter{Text: "cumin"})
	if after, ok := m.Current(); !ok || after.Key != before.Key {
		t.Errorf("the cursor moved off its row: %v", after)
	}
}

// n and N wrap, which is what makes them different from j and k rather than a
// second name for them.
func TestStepThroughMatchesWraps(t *testing.T) {
	m := filterable().SetFilter(table.Filter{Text: "ancho"})
	if got := m.Cursor(); got != 0 {
		t.Fatalf("cursor = %d", got)
	}
	m = press(m, "alt+n")
	if got := m.Cursor(); got != 1 {
		t.Errorf("n moved to %d, want 1", got)
	}
	m = press(m, "alt+n")
	if got := m.Cursor(); got != 0 {
		t.Errorf("n at the last match moved to %d, want it to wrap to 0", got)
	}
	m = press(m, "alt+p")
	if got := m.Cursor(); got != 1 {
		t.Errorf("N at the first match moved to %d, want it to wrap to the last", got)
	}
	// j and k do NOT wrap, or they would be the same key.
	m = press(filterable(), "alt+>", "ctrl+n")
	if got := m.Cursor(); got != 3 {
		t.Errorf("j at the bottom moved to %d; it wrapped", got)
	}
}

// "Nothing here" and "nothing matches what you typed" are different facts about
// the house, and the second one is the half that is easy to forget.
func TestTheEmptyStateSaysWhy(t *testing.T) {
	empty := strip(table.New(columns()).SetSize(90, 14).View())
	if !strings.Contains(empty, "nothing here") {
		t.Errorf("an empty table says: %q", empty)
	}
	filtered := strip(filterable().SetFilter(table.Filter{Text: "zzzz"}).View())
	if !strings.Contains(filtered, "nothing matches") {
		t.Errorf("a filtered-to-nothing table says: %q", filtered)
	}
	if !strings.Contains(filtered, "esc") {
		t.Errorf("it does not say how to get out of it: %q", filtered)
	}
}

// V picks what is SHOWN. Selecting rows a filter is hiding would mean acting on
// a set nobody has looked at.
func TestSelectAllPicksOnlyWhatIsShown(t *testing.T) {
	m := press(filterable().SetFilter(table.Filter{Text: "ancho"}), "alt+h")
	if got := m.SelectionCount(); got != 2 {
		t.Errorf("V selected %d rows out of 4, want the 2 shown", got)
	}
}

// ---------------------------------------------------------------------------
// Path columns
// ---------------------------------------------------------------------------

// pathColumns is a holdings-shaped table whose LOCATION column has a fuller
// hierarchical form behind each cell.
func pathColumns() []table.Column {
	return []table.Column{
		{Title: "ITEM", Min: 8, Grow: true},
		{Title: "LOCATION", Min: 8, Drop: 2, Elide: table.ElideStart, Path: true},
	}
}

func pathRows() []table.Row {
	return []table.Row{
		{Key: 1, Cells: []string{"Ancho", "Shelf 2"},
			Paths: []string{"", "Kitchen > Spice Cabinet > Shelf 2"}},
		{Key: 2, Cells: []string{"Cumin", "Garage"},
			Paths: []string{"", "Garage"}},
	}
}

func pathTable(width int) table.Model {
	return table.New(pathColumns()).SetRows(pathRows()).SetSize(width, 12)
}

// Given room, the column says where the shelf actually is. That is the whole
// point of it: three rows of one item in three different Shelf 1s cannot be
// told apart by the leaf alone.
func TestAPathColumnShowsTheWholeTreeWhenItFits(t *testing.T) {
	got := strip(pathTable(80).View())
	if !strings.Contains(got, "Kitchen > Spice Cabinet > Shelf 2") {
		t.Errorf("the full path is not shown in an 80-column terminal:\n%s", got)
	}
	// A root has no ancestors, and must not grow an ellipsis pretending it has.
	if !strings.Contains(got, "Garage") || strings.Contains(got, "… > Garage") {
		t.Errorf("a root location was rendered as though it had a parent:\n%s", got)
	}
}

// Narrowed, it gives up ANCESTORS rather than characters. Eliding the string
// reads "…t > Shelf 2", which cuts a name in half and spends a column on an
// ellipsis to say so; eliding by segment leaves names.
func TestAPathColumnGivesUpAncestorsNotCharacters(t *testing.T) {
	// Wide enough for "… > Spice Cabinet > Shelf 2", not for the Kitchen.
	got := strip(pathTable(38).View())
	if !strings.Contains(got, "… > Spice Cabinet > Shelf 2") {
		t.Errorf("the path did not shed its root cleanly:\n%s", got)
	}
	if strings.Contains(got, "n > Spice") {
		t.Errorf("the path was cut mid-name:\n%s", got)
	}
}

// The leaf is the floor. It is what the column held before it could show a path
// at all, so a terminal with no room to spare loses nothing it used to have.
func TestAPathColumnFallsBackToTheLeaf(t *testing.T) {
	got := strip(pathTable(20).View())
	if !strings.Contains(got, "Shelf 2") {
		t.Errorf("the leaf was lost in a narrow terminal:\n%s", got)
	}
	if strings.Contains(got, "Cabinet") {
		t.Errorf("a 20-column terminal found room for an ancestor:\n%s", got)
	}
	// And no ellipsis promising ancestors it did not have room to show.
	if strings.Contains(got, "…") {
		t.Errorf("the leaf alone was rendered with an ellipsis:\n%s", got)
	}
}

// The ancestors are context: worth having when they cost nothing, never worth
// taking the name column's room for.
//
// The natural width of a path column is its CELLS, so the growing column is
// sized as though the paths were not there and only genuine slack reaches them.
// Sizing on the paths instead is what dropped LOCATION and FLAGS from a
// hundred-column terminal that had ample room for them.
func TestAPathColumnTakesOnlySlack(t *testing.T) {
	long := []table.Row{{
		Key:   1,
		Cells: []string{"An item with a name long enough to fill the terminal by itself", "Shelf 2"},
		Paths: []string{"", "Kitchen > Spice Cabinet > Shelf 2"},
	}}
	withPath := table.New(pathColumns()).SetRows(long).SetSize(60, 12)

	plain := pathColumns()
	plain[1].Path = false
	without := table.New(plain).SetRows(long).SetSize(60, 12)

	if got, want := columnStart(strip(withPath.View())), columnStart(strip(without.View())); got != want {
		t.Errorf("the path column moved LOCATION from column %d to %d; it took room the name needed",
			want, got)
	}
}

// columnStart is where LOCATION begins in the header, which is the width every
// column before it was given.
func columnStart(view string) int {
	return strings.Index(strings.Split(view, "\n")[0], "LOCATION")
}

// Shown, never matched. Making the path the cell would quietly turn
// `loc:garage` into "anywhere in the Garage" and sort the column by branch
// rather than by name -- two behaviour changes hiding inside a rendering one.
func TestAPathIsShownButNotMatched(t *testing.T) {
	m := pathTable(80).SetFilter(table.Filter{
		Facets: []table.FacetTest{{Column: 1, Value: "kitchen"}},
	})
	if shown, _ := m.Counts(); shown != 0 {
		t.Errorf("a facet matched %d rows through the path; it may only see the cell", shown)
	}

	m = pathTable(80).SetFilter(table.Filter{
		Facets: []table.FacetTest{{Column: 1, Value: "shelf"}},
	})
	if shown, _ := m.Counts(); shown != 1 {
		t.Errorf("a facet on the cell matched %d rows, want 1", shown)
	}
}

// An elided path earns its space only while it still names something.
//
// "… > Shelf 2" is worse than the "Shelf 2" it replaces: four columns spent to
// say "this has a parent" without saying which. The column skips that form and
// stays on the leaf until a real ancestor fits.
func TestAPathColumnNeverSpendsSpaceOnAnEllipsisAlone(t *testing.T) {
	for width := 20; width <= 36; width++ {
		got := strip(pathTable(width).View())
		if strings.Contains(got, "… > Shelf 2") {
			t.Fatalf("at %d columns the path said only that it had a parent:\n%s", width, got)
		}
	}
}

// And it takes only the width it can actually USE.
//
// Handing it every spare column padded LOCATION out to the longest path in the
// house while still drawing the short form of every row -- space taken from the
// name column and spent on nothing.
func TestAPathColumnDoesNotHoardWidthItCannotUse(t *testing.T) {
	// 30 columns: too narrow for "… > Spice Cabinet > Shelf 2", so the path
	// column should be sitting at its cell width and the rest should have gone
	// to the growing column.
	narrow := columnStart(strip(pathTable(30).View()))
	wide := columnStart(strip(pathTable(36).View()))
	if narrow >= wide {
		t.Errorf("LOCATION starts at column %d in a 30-column terminal and %d in a 36-column one; "+
			"the extra width was padded into the path column rather than given to the name",
			narrow, wide)
	}
}
