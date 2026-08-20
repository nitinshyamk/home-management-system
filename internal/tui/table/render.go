package table

import (
	"strings"
)

// Rendering, and the width arithmetic that decides what a narrow terminal
// loses.
//
// The rule the whole file serves: a line wider than the terminal WRAPS, and one
// wrapped line shifts every row below it. That turns a cramped screen into an
// unreadable one, so overflowing is never the lesser evil -- dropping a column
// always is.

const separator = "  "

// View renders the table at its current size.
func (m Model) View() string {
	widths, visible := m.layout()
	if len(visible) == 0 {
		return emptyStyle.Render("(too narrow)")
	}

	var b strings.Builder
	b.WriteString(m.header(widths, visible))
	b.WriteByte('\n')

	if len(m.visible) == 0 {
		b.WriteString(emptyStyle.Render(m.emptyMessage()))
		return b.String()
	}

	page := m.page()
	for i := m.top; i < len(m.visible) && i < m.top+page; i++ {
		b.WriteString(m.line(m.visible[i], i, i == m.cursor, widths, visible))
		if i < len(m.visible)-1 && i < m.top+page-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// header names the columns, marks the focused one, and states the sort.
//
// Stating it matters: a table that is sorted and does not say so is quietly
// lying about what "first" means.
func (m Model) header(widths []int, visible []int) string {
	cells := make([]string, 0, len(visible))
	for n, i := range visible {
		title := m.cols[i].Title
		if i == m.sortCol && !m.fixed {
			arrow := " ^"
			if m.sortDesc {
				arrow = " v"
			}
			title = fit(title, max(0, widths[n]-len(arrow)), ElideEnd) + arrow
		}
		text := pad(title, widths[n], m.cols[i].Align)
		if i == m.col {
			text = focusedStyle.Render(text)
		} else {
			text = headerStyle.Render(text)
		}
		cells = append(cells, text)
	}
	return strings.Repeat(" ", gutter) + strings.Join(cells, separator)
}

// line renders one row: gutter, cells, and the banding that makes a screenful
// of them scannable.
//
// The whole line is padded to the terminal width before it is styled, so a
// stripe is a BAND across the screen rather than a ragged blob that stops
// wherever the last column happened to end.
func (m Model) line(r Row, index int, isCursor bool, widths []int, visible []int) string {
	cursorMark, selectMark := " ", " "
	if isCursor {
		cursorMark = ">"
	}
	selected := m.selected[r.Key]
	if selected {
		selectMark = "*"
	}

	cells := make([]string, 0, len(visible))
	for n, i := range visible {
		cells = append(cells, pad(fit(cell(r, i), widths[n], m.cols[i].Elide), widths[n], m.cols[i].Align))
	}
	text := cursorMark + selectMark + strings.Join(cells, separator)

	// Striping follows the row's place in the DATA, not its place on screen, so
	// a row keeps its band while the list scrolls under the cursor. Banding by
	// screen position makes every stripe appear to move on every keystroke.
	return rowStyle(index%2 == 1, selected, isCursor).Render(pad(text, m.width, Left))
}

// layout decides column widths, and which columns survive.
//
// Squeeze BEFORE dropping, which is the opposite of the first version and the
// reason it was wrong. Dropping first looks at a column's natural width -- and
// a location column's natural width is the longest path in the house, sixty-odd
// characters -- so LOCATION and FLAGS both vanished from a hundred-column
// terminal that had ample room for them truncated. The three rows of Ancho
// Chile then became indistinguishable, which is the exact question a holdings
// table exists to answer.
//
// So: give every column its natural width, squeeze the widest toward their Min,
// and only when even the Min widths will not fit does a column get dropped --
// in the order Drop declares, so what a narrow terminal loses is a decision.
func (m Model) layout() (widths []int, visible []int) {
	available := m.width - gutter
	if available <= 0 {
		return nil, nil
	}

	visible = make([]int, 0, len(m.cols))
	for i := range m.cols {
		visible = append(visible, i)
	}

	for {
		widths = m.squeeze(m.naturalWidths(visible), visible, available)
		if total(widths) <= available || len(visible) == 1 {
			break
		}
		worst, at := 0, -1
		for n, i := range visible {
			if m.cols[i].Drop > worst {
				worst, at = m.cols[i].Drop, n
			}
		}
		if at < 0 {
			break // everything left is undroppable; show it squeezed and clipped
		}
		visible = append(visible[:at:at], visible[at+1:]...)
	}

	// Hand any leftover to whoever asked to grow.
	if slack := available - total(widths); slack > 0 {
		for n, i := range visible {
			if m.cols[i].Grow {
				widths[n] += slack
				break
			}
		}
	}
	return widths, visible
}

// squeeze narrows the widest column repeatedly until it fits or nothing can
// give.
//
// Widest-first rather than proportional, so one very long column yields before
// several short ones are all made useless -- a path of sixty characters has far
// more to spare than a quantity of five.
func (m Model) squeeze(widths []int, visible []int, available int) []int {
	for total(widths) > available {
		widest, at := -1, -1
		for n, i := range visible {
			if widths[n] > widest && widths[n] > m.cols[i].Min {
				widest, at = widths[n], n
			}
		}
		if at < 0 {
			return widths // every column is at its Min
		}
		widths[at]--
	}
	return widths
}

// emptyMessage says WHY the list is empty, which is the half of "the filter
// says what it did" that is easy to forget. "Nothing here" and "nothing matches
// what you typed" are different facts about the house.
func (m Model) emptyMessage() string {
	if m.Filtered() && len(m.rows) > 0 {
		return "  (nothing matches this filter -- esc to clear it)"
	}
	return "  (nothing here)"
}

func (m Model) naturalWidths(visible []int) []int {
	widths := make([]int, len(visible))
	for n, i := range visible {
		w := len(m.cols[i].Title)
		if i == m.sortCol && !m.fixed {
			w += 2 // the sort arrow lives in the header cell
		}
		for _, r := range m.rows {
			if c := len(cell(r, i)); c > w {
				w = c
			}
		}
		widths[n] = w
	}
	return widths
}

func total(widths []int) int {
	if len(widths) == 0 {
		return 0
	}
	sum := len(separator) * (len(widths) - 1)
	for _, w := range widths {
		sum += w
	}
	return sum
}

// fit truncates to width, marking that it did.
//
// The ellipsis costs a character and earns it: a name cut without one reads as
// a name that is simply short, and "Thunderbolt 4 to Dual DisplayP" looks like
// a real product until you go looking for it.
func fit(s string, width int, elide Elide) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	if elide == ElideStart {
		return "…" + string(runes[len(runes)-width+1:])
	}
	return string(runes[:width-1]) + "…"
}

func pad(s string, width int, align Align) string {
	n := width - len([]rune(stripANSI(s)))
	if n <= 0 {
		return s
	}
	if align == Right {
		return strings.Repeat(" ", n) + s
	}
	return s + strings.Repeat(" ", n)
}

// stripANSI removes escape sequences so padding measures printable columns.
func stripANSI(s string) string {
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
