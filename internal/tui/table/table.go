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

	"home-management-system/internal/tui/keys"
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

	// Path marks a column whose cells have a fuller, hierarchical form in
	// Row.Paths -- "Garage > Bay 3 > Blue Crate" for a cell reading
	// "Blue Crate".
	//
	// Its natural width stays the width of the CELLS, so a house with a deep
	// tree does not squeeze every other column on the strength of one long
	// path. It grows toward the fuller form only out of space nothing else
	// claimed, and gives back the ancestors first when there is less.
	Path bool
}

// Row is one line. Key is the caller's identifier, untouched.
type Row struct {
	Key   int64
	Cells []string
	// Paths is the fuller form of a cell, index-parallel to Cells, and empty
	// where a cell has none.
	//
	// It is shown, never matched: the filter and the sort read Cells. Making
	// the path the cell instead would quietly turn `loc:garage` into "anywhere
	// in the Garage" and sort the column by branch rather than by name -- two
	// behaviour changes hiding inside a rendering one.
	Paths []string
}

// Model is the widget's state.
type Model struct {
	cols []Column
	// rows is everything; visible is what survives the filter. Keeping both is
	// what lets the count say "12 of 34" rather than just "12".
	rows    []Row
	visible []Row
	filter  Filter

	cursor   int
	col      int // the focused column, which sorting and h/l act on
	top      int // first visible row, for scrolling
	selected map[int64]bool

	sortCol  int
	sortDesc bool

	// fixed means the caller's order IS the order. A tree's rows are in
	// hierarchy order, and sorting them would destroy the containment the view
	// exists to show -- so a fixed table offers no sort and claims none.
	fixed bool

	// overlay is drawn immediately after the cursor's row, pushing the rows
	// below it down. It is how an inline editor is inline: the field opens
	// AT the row it belongs to rather than under the whole list, so the row
	// being edited and the rows around it stay where the eye left them.
	overlay []string

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

	// picked is the selection, and it differs from the banding in HUE rather
	// than in lightness.
	//
	// That is the whole reason it works. Banding and selection are two
	// independent things a row can be saying, and saying both with the same
	// dimension -- a bit darker, a bit darker still -- makes them compete: four
	// greys where the eye can reliably tell two apart. A tint is categorically
	// different from a grey step, so "which band am I on" and "is this picked"
	// never have to be distinguished by degree.
	picked = lipgloss.AdaptiveColor{Light: "189", Dark: "17"}
)

// rowStyle composes what a row is saying.
//
// Three background states, not four: a selected row is TINTED and does not
// band. Banding on top of the tint would give four shades where the eye can
// reliably separate two, and it would earn nothing -- the tint already gives
// the eye something to track along, which is banding's only job. The gain is
// that a run of selected rows reads as one solid block, which is exactly what
// makes a selection countable at a glance.
//
// One style rather than nested spans, so a selected row under the cursor reads
// as both rather than as whichever was applied last.
func rowStyle(striped, selected, cursor bool) lipgloss.Style {
	s := lipgloss.NewStyle()
	switch {
	case selected:
		s = s.Background(picked)
	case striped:
		s = s.Background(stripe)
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

// Fixed marks the rows as arriving in an order the caller owns.
func (m Model) Fixed() Model {
	m.fixed = true
	return m
}

// SetRows replaces the contents, keeping the cursor in bounds and keeping any
// selection that still refers to something present.
func (m Model) SetRows(rows []Row) Model {
	m.rows = rows
	if !m.fixed {
		m.rows = m.applySort(rows)
	}
	m.visible = m.matching()
	if m.cursor >= len(m.visible) {
		m.cursor = max(0, len(m.visible)-1)
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

// Rows returns what is SHOWN, in display order. Callers map a cursor back
// through these, so they must be the filtered set.
func (m Model) Rows() []Row { return m.visible }

// AllRows returns every row, filtered or not.
func (m Model) AllRows() []Row { return m.rows }

// Cursor is the index of the row under the cursor, or -1 when there are none.
func (m Model) Cursor() int {
	if len(m.visible) == 0 {
		return -1
	}
	return m.cursor
}

// SetOverlay draws lines immediately after the cursor's row.
//
// They come out of the page budget rather than being added to it, so the table
// occupies the same space and the rows below simply move down -- which is what
// makes an editor feel like it opened in the list rather than replacing part
// of it.
func (m Model) SetOverlay(lines []string) Model {
	m.overlay = lines
	m.clampScroll()
	return m
}

// SetCursor puts the cursor on a row by index, scrolling if it has to.
//
// It exists for callers whose rows can be rebuilt under the cursor -- a tree
// folding a subtree away -- and who therefore have to restore the cursor by
// IDENTITY rather than let it keep an index that now means a different row.
func (m Model) SetCursor(i int) Model {
	if len(m.visible) == 0 {
		return m
	}
	m.cursor = clamp(i, 0, len(m.visible)-1)
	m.clampScroll()
	return m
}

// Current is the row under the cursor.
func (m Model) Current() (Row, bool) {
	if len(m.visible) == 0 {
		return Row{}, false
	}
	return m.visible[m.cursor], true
}

// Selected returns the keys of the selected rows in display order. With nothing
// selected it returns the row under the cursor, because "act on the selection"
// and "act on this row" are the same gesture with and without a prior space.
func (m Model) Selected() []int64 {
	var out []int64
	for _, r := range m.visible {
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
	if m.fixed || m.sortCol >= len(m.cols) {
		return ""
	}
	direction := "a-z"
	if m.sortDesc {
		direction = "z-a"
	}
	return "by " + strings.ToLower(m.cols[m.sortCol].Title) + " " + direction
}

// FocusedColumn is the column C-f and C-b move between.
func (m Model) FocusedColumn() int { return m.col }

// Update handles a keystroke. It returns the model and whether the key was
// consumed, so a parent can fall through to its own bindings.
//
// Two of the returns are deliberately NOT consumed, and both are how a
// keystroke reaches past the table to whatever contains it. See Cancel and
// SearchForward below.
func (m Model) Update(msg tea.KeyMsg) (Model, bool) {
	switch keys.Lookup(keys.Table, msg) {
	case keys.MoveDown:
		m.move(1)
	case keys.MoveUp:
		m.move(-1)
	case keys.Top:
		m.cursor = 0
		m.clampScroll()
	case keys.Bottom:
		m.cursor = max(0, len(m.visible)-1)
		m.clampScroll()
	case keys.Recenter:
		m.centre()

	case keys.PageDown:
		m.move(m.page() / 2)
	case keys.PageUp:
		m.move(-m.page() / 2)

	// M-n and M-p step through what a filter left, wrapping. Wrapping is what
	// makes them different from plain motion rather than a second name for it:
	// at the end of three matches, the useful next match is the first one.
	//
	// They are not C-s. C-s OPENS the search line, and it has to keep doing
	// that even once a filter is applied, because the line comes back prefilled
	// and that is the only way to refine a filter rather than retype it.
	case keys.NextMatch:
		m.step(1)
	case keys.PrevMatch:
		m.step(-1)

	case keys.MoveLeft:
		if m.col > 0 {
			m.col--
		}
	case keys.MoveRight:
		if m.col < len(m.cols)-1 {
			m.col++
		}

	case keys.ToggleSelect:
		if r, ok := m.Current(); ok {
			if m.selected[r.Key] {
				delete(m.selected, r.Key)
			} else {
				m.selected[r.Key] = true
			}
			m.move(1)
		}
	// Selecting everything picks what is SHOWN. Selecting rows a filter is
	// hiding would mean acting on a set nobody has looked at.
	case keys.SelectVisible:
		for _, r := range m.visible {
			m.selected[r.Key] = true
		}
	case keys.Cancel:
		if len(m.selected) == 0 {
			return m, false // nothing to clear; let the parent leave a mode
		}
		m.selected = map[int64]bool{}

	case keys.Sort:
		if m.fixed {
			return m, false
		}
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
	m.visible = m.matching()
	for i, r := range m.visible {
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

// path is the fuller form of a cell, or the cell itself when it has none.
func path(r Row, i int) string {
	if i < len(r.Paths) && r.Paths[i] != "" {
		return r.Paths[i]
	}
	return cell(r, i)
}

// step moves with wrap-around.
func (m *Model) step(n int) {
	if len(m.visible) == 0 {
		return
	}
	m.cursor = (m.cursor + n + len(m.visible)) % len(m.visible)
	m.clampScroll()
}

func (m *Model) move(n int) {
	if len(m.visible) == 0 {
		return
	}
	m.cursor = clamp(m.cursor+n, 0, len(m.visible)-1)
	m.clampScroll()
}

// page is how many rows are visible, which is what C-d and C-u move by.
//
// The overlay eats into it, because it occupies lines the rows would otherwise
// have had.
func (m Model) page() int { return max(1, m.height-2-len(m.overlay)) }

func (m *Model) centre() {
	m.top = clamp(m.cursor-m.page()/2, 0, max(0, len(m.visible)-m.page()))
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
	m.top = clamp(m.top, 0, max(0, len(m.visible)-page))
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
