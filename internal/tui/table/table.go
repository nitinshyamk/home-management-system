// Package table is the dense working surface.
//
// One widget, used by the Holdings and Items views and — unchanged — by the
// import plan screen. That sharing is not an optimisation: it is what keeps the
// interactive and bulk flows from drifting into different products. If the plan
// screen ever needs this reworked, the two have already diverged.
//
// It knows nothing about the domain. Rows carry an opaque Key so the caller can
// map a cursor back to whatever it selected, and cells are strings the caller
// has already formatted.
package table

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Align says which edge a cell is anchored to. Numbers read right-aligned
// because that is what lets the eye compare magnitudes down a column.
type Align int

const (
	Left Align = iota
	Right
)

// Elide says which end of a too-long cell is cut away.
//
// It is a separate decision from alignment because the identifying part of a
// value is not always at the same end. A product name identifies from the
// front; a location path identifies from its LEAF, so "Garage > Metal Shelvin…"
// tells you almost nothing while "…> Blue Crate > Small Parts Tray" tells you
// where the thing is. Numbers cut from the right read as smaller numbers, which
// is worse than either.
type Elide int

const (
	ElideEnd Elide = iota
	ElideStart
)

// Column describes one field.
type Column struct {
	Title string
	Align Align
	Elide Elide

	// Min is the narrowest this column is useful at. A column squeezed below it
	// is dropped rather than shown as three characters and an ellipsis.
	Min int

	// Drop orders what goes first when the terminal is narrow: higher goes
	// sooner. A column with Drop == 0 is never dropped, and there must be at
	// least one -- a table with no columns is not a degraded table, it is a
	// blank screen.
	Drop int

	// Grow shares out space left over after every column has its natural
	// width. Only one column usually wants it: the one holding names.
	Grow bool
}

// Row is one line. Key is the caller's identifier, untouched.
type Row struct {
	Key   int64
	Cells []string
}

// Model is the widget's state.
type Model struct {
	cols []Column
	rows []Row

	cursor   int
	col      int // the focused column, which sorting and h/l act on
	top      int // first visible row, for scrolling
	selected map[int64]bool

	sortCol  int
	sortDesc bool

	// pending holds the first key of a two-key sequence (g, z), because gg and
	// zz are one gesture each and the widget has to remember it saw the first.
	pending rune

	width, height int
}

var (
	headerStyle  = lipgloss.NewStyle().Faint(true)
	focusedStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	emptyStyle   = lipgloss.NewStyle().Faint(true)

	// stripe is the banding that makes a dense table scannable across.
	//
	// Uniform rows are hard to read a whole screenful of at once -- the eye has
	// nothing to track along -- but alternating black and white is far too
	// loud for a surface this repetitive. This is one step off the background
	// in each direction, adaptive so it stays one step off in a light terminal
	// too. It costs no vertical space, which is the other thing a dense table
	// cannot spare.
	stripe = lipgloss.AdaptiveColor{Light: "254", Dark: "236"}
)

// rowStyle composes the three things a row can be saying at once.
//
// One style rather than three spans, so they layer predictably: a selected row
// under the cursor on a striped line is banded, underlined, and bold, and reads
// as all three rather than as whichever was applied last.
func rowStyle(striped, selected, cursor bool) lipgloss.Style {
	s := lipgloss.NewStyle()
	if striped {
		s = s.Background(stripe)
	}
	if selected {
		// Underline rather than more weight or more colour. The gutter mark
		// says WHICH rows are picked; the underline makes the set legible as a
		// set without competing with the cursor for attention.
		s = s.Underline(true)
	}
	if cursor {
		s = s.Bold(true)
	}
	return s
}

// gutter is the two fixed columns at the left: the cursor mark and the
// selection mark.
//
// Two separate marks rather than one shared one, because they answer different
// questions. "Where am I" is asked constantly and answered by the cursor; "what
// have I picked" is asked once before an action and must be answerable from
// anywhere on the screen, including rows the cursor is nowhere near.
const gutter = 2

// New builds a table over a fixed set of columns.
//
// It starts SORTED, by the first column ascending, rather than in whatever
// order the rows arrived. A table always has an order, so the only question is
// whether it says what that order is -- and one that does not is quietly lying
// about what "first" means. Sorting the rows here rather than trusting the
// caller's order is what makes the stated order true.
func New(cols []Column) Model {
	return Model{cols: cols, selected: map[int64]bool{}, width: 80, height: 20}
}

// SetRows replaces the contents, keeping the cursor in bounds and keeping any
// selection that still refers to something present.
func (m Model) SetRows(rows []Row) Model {
	m.rows = m.applySort(rows)
	if m.cursor >= len(m.rows) {
		m.cursor = max(0, len(m.rows)-1)
	}
	present := map[int64]bool{}
	for _, r := range rows {
		present[r.Key] = true
	}
	for key := range m.selected {
		if !present[key] {
			delete(m.selected, key)
		}
	}
	m.clampScroll()
	return m
}

// SetSize tells the table how much room it has. Height is the whole widget,
// header included.
func (m Model) SetSize(width, height int) Model {
	m.width, m.height = width, height
	m.clampScroll()
	return m
}

// Rows returns the contents in display order.
func (m Model) Rows() []Row { return m.rows }

// Cursor is the index of the row under the cursor, or -1 when there are none.
func (m Model) Cursor() int {
	if len(m.rows) == 0 {
		return -1
	}
	return m.cursor
}

// Current is the row under the cursor.
func (m Model) Current() (Row, bool) {
	if len(m.rows) == 0 {
		return Row{}, false
	}
	return m.rows[m.cursor], true
}

// Selected returns the keys of the selected rows in display order. With nothing
// selected it returns the row under the cursor, because "act on the selection"
// and "act on this row" are the same gesture with and without a prior space.
func (m Model) Selected() []int64 {
	var out []int64
	for _, r := range m.rows {
		if m.selected[r.Key] {
			out = append(out, r.Key)
		}
	}
	if len(out) == 0 {
		if r, ok := m.Current(); ok {
			return []int64{r.Key}
		}
	}
	return out
}

// SelectionCount is how many rows were explicitly picked, which is what a
// status line should report -- Selected()'s fallback to the cursor row would
// make it claim a selection nobody made.
func (m Model) SelectionCount() int { return len(m.selected) }

// SortDescription says how the table is ordered, in words, for a status line.
//
// The header arrow says it too, but only if the sorted column happens to be on
// screen -- a narrow terminal can drop it, and then the table is ordered by
// something the person cannot see.
func (m Model) SortDescription() string {
	if m.sortCol >= len(m.cols) {
		return ""
	}
	direction := "a-z"
	if m.sortDesc {
		direction = "z-a"
	}
	return "by " + strings.ToLower(m.cols[m.sortCol].Title) + " " + direction
}

// FocusedColumn is the column h and l move between.
func (m Model) FocusedColumn() int { return m.col }

// Update handles a keystroke. It returns the model and whether the key was
// consumed, so a parent can fall through to its own bindings.
func (m Model) Update(msg tea.KeyMsg) (Model, bool) {
	key := msg.String()

	// Two-key gestures first: the widget remembers it saw a g or a z.
	if m.pending != 0 {
		first := m.pending
		m.pending = 0
		switch {
		case first == 'g' && key == "g":
			m.cursor = 0
			m.clampScroll()
			return m, true
		case first == 'z' && key == "z":
			m.centre()
			return m, true
		}
		// Not a gesture after all. Fall through and treat this key on its own.
	}

	switch key {
	case "g", "z":
		m.pending = rune(key[0])
		return m, true

	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "G":
		m.cursor = max(0, len(m.rows)-1)
		m.clampScroll()
	case "ctrl+d":
		m.move(m.page() / 2)
	case "ctrl+u":
		m.move(-m.page() / 2)

	case "h", "left":
		if m.col > 0 {
			m.col--
		}
	case "l", "right":
		if m.col < len(m.cols)-1 {
			m.col++
		}

	case " ":
		if r, ok := m.Current(); ok {
			if m.selected[r.Key] {
				delete(m.selected, r.Key)
			} else {
				m.selected[r.Key] = true
			}
			m.move(1)
		}
	case "V":
		for _, r := range m.rows {
			m.selected[r.Key] = true
		}
	case "esc":
		if len(m.selected) == 0 {
			return m, false // nothing to clear; let the parent leave a mode
		}
		m.selected = map[int64]bool{}

	case "s":
		m = m.sortBy(m.col)

	default:
		return m, false
	}
	return m, true
}

// sortBy sorts on a column, reversing if it is already the sort column.
func (m Model) sortBy(col int) Model {
	if m.sortCol == col {
		m.sortDesc = !m.sortDesc
	} else {
		m.sortCol, m.sortDesc = col, false
	}
	// The cursor follows its row rather than its index. A sort that moves the
	// selection out from under the cursor is how you act on the wrong thing.
	var under int64 = -1
	if r, ok := m.Current(); ok {
		under = r.Key
	}
	m.rows = m.applySort(m.rows)
	for i, r := range m.rows {
		if r.Key == under {
			m.cursor = i
		}
	}
	m.clampScroll()
	return m
}

func (m Model) applySort(rows []Row) []Row {
	out := append([]Row(nil), rows...)
	col := m.sortCol
	sort.SliceStable(out, func(i, j int) bool {
		a, b := cell(out[i], col), cell(out[j], col)
		if m.sortDesc {
			return strings.ToLower(a) > strings.ToLower(b)
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	return out
}

func cell(r Row, i int) string {
	if i < len(r.Cells) {
		return r.Cells[i]
	}
	return ""
}

func (m *Model) move(n int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = clamp(m.cursor+n, 0, len(m.rows)-1)
	m.clampScroll()
}

// page is how many rows are visible, which is what C-d and C-u move by.
func (m Model) page() int { return max(1, m.height-2) }

func (m *Model) centre() {
	m.top = clamp(m.cursor-m.page()/2, 0, max(0, len(m.rows)-m.page()))
}

// clampScroll keeps the cursor on screen, scrolling by the least that achieves
// it so the rows a person is reading stay where they were.
func (m *Model) clampScroll() {
	page := m.page()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+page {
		m.top = m.cursor - page + 1
	}
	m.top = clamp(m.top, 0, max(0, len(m.rows)-page))
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
