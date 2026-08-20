package table_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"home-management-system/internal/tui/table"
)

// The widget crosses no boundary, so it is tested here rather than through the
// Simulator: "does j move the cursor" needs no database. Everything that
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

func press(m table.Model, keys ...string) table.Model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case " ":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEscape}
		case "ctrl+d":
			msg = tea.KeyMsg{Type: tea.KeyCtrlD}
		case "ctrl+u":
			msg = tea.KeyMsg{Type: tea.KeyCtrlU}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m, _ = m.Update(msg)
	}
	return m
}

func TestMotion(t *testing.T) {
	m := newTable(20)
	if got := press(m, "j", "j", "j").Cursor(); got != 3 {
		t.Errorf("jjj put the cursor at %d, want 3", got)
	}
	if got := press(m, "k").Cursor(); got != 0 {
		t.Errorf("k from the top moved to %d, want 0 -- the cursor left the table", got)
	}
	if got := press(m, "G").Cursor(); got != 19 {
		t.Errorf("G put the cursor at %d, want the last row", got)
	}
	if got := press(m, "G", "j").Cursor(); got != 19 {
		t.Errorf("j from the bottom moved to %d, want the last row", got)
	}
	if got := press(m, "G", "g", "g").Cursor(); got != 0 {
		t.Errorf("gg put the cursor at %d, want the first row", got)
	}
	// A lone g is not a jump. It waits, which is what makes gg one gesture.
	if got := press(m, "j", "j", "g").Cursor(); got != 2 {
		t.Errorf("a pending g moved the cursor to %d", got)
	}
}

func TestMotionOnAnEmptyTable(t *testing.T) {
	m := table.New(columns()).SetSize(80, 12)
	if got := press(m, "j", "k", "G", "g", "g", " ").Cursor(); got != -1 {
		t.Errorf("cursor = %d on an empty table, want -1", got)
	}
	if !strings.Contains(m.View(), "nothing here") {
		t.Errorf("an empty table renders as:\n%s", m.View())
	}
}

func TestSelection(t *testing.T) {
	m := newTable(10)
	// space selects AND advances, so picking three in a row is three presses.
	m = press(m, " ", " ", " ")
	if got := m.SelectionCount(); got != 3 {
		t.Errorf("selected %d rows, want 3", got)
	}
	if got := m.Selected(); len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("selected keys = %v, want the first three", got)
	}
	// Toggling off.
	m = press(m, "k", " ")
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
	m := press(newTable(10), "j", "j")
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
	m = press(m, "j", "j", "j")
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
	for _, keys := range [][]string{{"G"}, {"g", "g"}, {"ctrl+d"}, {"ctrl+d", "ctrl+d"}, {"ctrl+u"}} {
		m = press(m, keys...)
		lines := strings.Split(strip(m.View()), "\n")
		found := false
		for _, line := range lines {
			if strings.HasPrefix(line, ">") {
				found = true
			}
		}
		if !found {
			t.Errorf("after %v the cursor is off screen:\n%s", keys, strip(m.View()))
		}
	}
}

// SetRows must not strand the cursor past the end, which is what happens when a
// filter narrows the table under it.
func TestReplacingRowsKeepsTheCursorInBounds(t *testing.T) {
	m := press(newTable(20), "G")
	m = m.SetRows(rows(3))
	if got := m.Cursor(); got != 2 {
		t.Errorf("cursor = %d after the table shrank to 3 rows", got)
	}
	// And a selection of rows that are gone does not linger.
	m = press(newTable(20), " ", " ").SetRows(rows(1))
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
	withSelection := press(m, " ")
	if _, handled := withSelection.Update(tea.KeyMsg{Type: tea.KeyEscape}); !handled {
		t.Error("esc did not clear the selection")
	}
}

func TestColumnFocusMoves(t *testing.T) {
	m := newTable(5)
	if got := press(m, "l", "l").FocusedColumn(); got != 2 {
		t.Errorf("ll focused column %d, want 2", got)
	}
	if got := press(m, "h").FocusedColumn(); got != 0 {
		t.Errorf("h from the first column focused %d, want 0", got)
	}
	if got := press(m, "l", "l", "l", "l", "l", "l").FocusedColumn(); got != 3 {
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
	lines := renderedRows(press(newTable(6), "G"))
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
	// Select row 0, then move the cursor well clear of it, so what is compared
	// is selection against nothing rather than selection against the cursor.
	m := press(newTable(6), " ", "G")
	lines := renderedRows(m)

	if !strings.Contains(strip(lines[0]), "*") {
		t.Errorf("a selected row has no gutter mark: %q", strip(lines[0]))
	}
	// And something beyond the mark, since one character among thirty rows is
	// easy to lose.
	if styleOf(lines[0]) == styleOf(lines[2]) {
		t.Errorf("a selected row is styled exactly like an unselected one of the same band:\n"+
			" selected %q\n unselected %q", lines[0], lines[2])
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
