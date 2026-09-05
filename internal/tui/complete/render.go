package complete

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"home-management-system/internal/tui/keys"
)

var (
	chosenStyle   = lipgloss.NewStyle().Bold(true)
	unchosenStyle = lipgloss.NewStyle().Faint(true)
	hintStyle     = lipgloss.NewStyle().Faint(true)
	warnStyle     = lipgloss.NewStyle().Faint(true).Italic(true)
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
	room := width - len([]rune(gutter))
	take := "   " + keys.Show(keys.Line, keys.Complete) + " to take it"
	out := make([]string, 0, len(m.options))
	for i, option := range m.options {
		if i == m.selected {
			out = append(out, gutter+chosenStyle.Render(clip(option, room-len([]rune(take))))+
				hintStyle.Render(take))
			continue
		}
		out = append(out, gutter+unchosenStyle.Render(clip(option, room)))
	}
	return out
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
		out = append(out, gutter+warnStyle.Render(clip(text, width-len([]rune(gutter)))))
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
func Hint() string {
	return strings.Join([]string{
		keys.Show(keys.Line, keys.Complete) + " take",
		keys.Show(keys.Line, keys.MoveDown) + "/" + keys.Show(keys.Line, keys.MoveUp) + " choose",
		keys.Show(keys.Line, keys.Dismiss) + " dismiss",
	}, "   ")
}
