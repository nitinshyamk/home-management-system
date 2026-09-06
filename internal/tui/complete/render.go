package complete

import (
	"strconv"
	"strings"

	"home-management-system/internal/tui/keys"

	"home-management-system/internal/tui/style"
)

// Window is how many options are drawn at once.
//
// The list is scrolled rather than cut. Limit used to be eight and doubled as
// the window, so "how many matches there are" and "how many rows fit" were the
// same number and neither was right: eight rows of Basement shelves pushed the
// table off the bottom, while the Kitchen shelf somebody was actually looking
// for was match number nine and no keystroke could reach it.
//
// Six, because these lines are spliced INTO a list of rows and every one of
// them displaces a row of the thing being worked on.
const Window = 6

var (
// The highlight carries a mark in the gutter as well as its weight.
//
// Bold-against-faint is the whole signal only until the list is read on a
// terminal that renders neither, or by somebody scanning a column of paths
// that are bold-ish anyway. The table marks its cursor with a character in
// the margin for the same reason, and this is the same cursor.
)

const (
	mark   = "\u203a "
	unmark = "  "
)

// Lines renders the list under the field it belongs to.
//
// The HIGHLIGHT carries the hint, not the first row. Putting "TAB to take it"
// on the top option was honest while the top option was the only one Tab could
// take; now that C-n moves, a hint pinned to the top would be pointing at the
// wrong row the moment anybody used it.
func (m Model) Lines(gutter string, width int) []string {
	if !m.IsOpen() {
		return nil
	}
	room := width - len([]rune(gutter)) - len([]rune(unmark))
	take := "   " + keys.Show(keys.Line, keys.Complete) + " to take it"
	first, last := m.window()

	out := make([]string, 0, Window+2)
	if first > 0 {
		out = append(out, gutter+style.Dim.Render(more(first, "above")))
	}
	for i := first; i < last; i++ {
		option := m.options[i]
		if i != m.selected {
			out = append(out, gutter+unmark+style.Dim.Render(clip(option, room)))
			continue
		}
		line := gutter + style.Strong.Render(mark) + style.Strong.Render(clip(option, room-len([]rune(take))))
		// The hint names what Tab would DO, so it is absent on the option the
		// field is already holding -- there Tab falls through to the next
		// field, and a line saying "take it" would be describing a keystroke
		// that does something else.
		if !m.settled() {
			line += style.Dim.Render(take)
		}
		out = append(out, line)
	}
	if rest := len(m.options) - last; rest > 0 {
		out = append(out, gutter+style.Dim.Render(more(rest, "below")))
	}
	return out
}

// window is the slice of options currently drawn.
//
// One over the window is drawn whole. A "1 more below" line costs the row it is
// reporting on, so at Window+1 options the marker buys nothing and hides the
// thing it is pointing at -- which for the seven unit codes would mean a closed
// vocabulary that never shows all of itself.
func (m Model) window() (first, last int) {
	if len(m.options) <= Window+1 {
		return 0, len(m.options)
	}
	first = m.offset
	if first > len(m.options) {
		first = len(m.options)
	}
	last = first + Window
	if last > len(m.options) {
		last = len(m.options)
	}
	return first, last
}

// more names what is off the end of the window, and says to keep typing.
//
// Scrolling to it is one thing; narrowing to it is nearly always faster, and a
// count with no advice reads as a wall rather than as a prompt.
func more(n int, where string) string {
	if n == 1 {
		return "  1 more " + where
	}
	return "  " + strconv.Itoa(n) + " more " + where + " -- keep typing to narrow"
}

// Warnings renders matches that are NOT on offer.
//
// A warning is not a suggestion and must not look like one: these are the
// things that already exist, shown while naming something new so that a second
// Turmeric is a decision rather than an accident. Tab cannot take one, so
// nothing here says it can.
//
// Cut to the width, because these are PATHS of existing things and a household
// has some long ones -- and a line wider than the panel wraps, which shifts
// every row of the list underneath it.
func Warnings(gutter string, width int, matches []string) []string {
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for i, match := range matches {
		text := "! " + match
		if i == 0 {
			text += " already exists"
		}
		out = append(out, gutter+style.Aside.Render(clip(text, width-len([]rune(gutter)))))
	}
	return out
}

// Lines is cut the same way, for the same reason.
func clip(s string, width int) string {
	if width <= 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "…"
}

// Hint is the one-line summary of what the keys do, for a footer.
//
// Enter is named beside Tab because it is the key somebody presses when they
// have finished choosing, and it now does what that means: it takes the
// highlight rather than submitting the half-typed text underneath it.
func Hint() string {
	return strings.Join([]string{
		keys.Show(keys.Line, keys.Complete) + "/" +
			keys.Show(keys.Line, keys.Confirm) + " take",
		keys.Show(keys.Line, keys.MoveDown) + "/" + keys.Show(keys.Line, keys.MoveUp) + " choose",
		keys.Show(keys.Line, keys.Dismiss) + " dismiss",
	}, "   ")
}
