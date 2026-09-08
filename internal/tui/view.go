package tui

import (
	"fmt"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/omnibox"
	"home-management-system/internal/tui/style"
	"strings"
)

// Drawing: the screen, and the chrome around whatever is on it.

func (m Model) View() string {
	if !m.ready {
		return "loading..."
	}
	// The editor's lines go INTO the surface, spliced after the row they belong
	// to, rather than under the whole list. Under the list is not inline; it is
	// a second place to look.
	overlay := m.field().Lines()
	if m.creator.IsOpen() {
		overlay = m.creator.SetWidth(m.width).Lines()
	}

	// Every surface scrolls itself, so each renders straight rather than
	// through a viewport somebody else owns. Two things scrolling one list is
	// how a cursor ends up off screen with nothing obviously wrong.
	body := m.current.SetOverlay(overlay).View()
	if m.confirm != nil {
		body = m.confirmView()
	}
	if m.box.Mode() == omnibox.Jump {
		// The palette REPLACES the list rather than floating over it. A jump
		// searches everything, so leaving the current view visible underneath
		// would suggest it is being searched, which is the confusion between
		// the two modes that this substage exists to avoid.
		body = m.jump.View()
	}

	if m.flow.reviewing() && m.confirm == nil {
		// The plan REPLACES the house. A file proposing a batch of changes is
		// not something to look at alongside what you own; it is the only thing
		// worth looking at until it is settled.
		//
		// And a panel opened for one of its rows sits ON the plan, not on the
		// house behind it -- otherwise settling a row shows you a screen that
		// has nothing to do with what you are settling.
		// The field goes INTO the plan, spliced after the row it belongs to --
		// the same overlay the browse views use, for the same reason.
		//
		// The creation panel does not: it is about a thing that does not exist
		// yet rather than about the row's text, and it is tall enough that
		// splicing it would push the plan off the screen it is confirming.
		height := m.height - 2 - len(m.problemLines()) - m.creator.Height()
		plan := m.flow.plan.SetSize(m.width, height).SetOverlay(m.editor.SetWidth(m.width).Lines())
		parts := []string{plan.View()}
		if m.creator.IsOpen() {
			parts = append(parts, m.creator.SetWidth(m.width).Lines()...)
		}
		for _, line := range m.problemLines() {
			parts = append(parts, style.Error.Render(line))
		}
		return strings.Join(parts, "\n")
	}

	parts := []string{m.header()}
	// Above the body, with the view tabs, because that is where a MODE belongs:
	// it is true of the whole screen rather than of the row under the cursor,
	// and it has to be readable in whichever view you have navigated to.
	for _, line := range m.carryLine() {
		parts = append(parts, style.Strong.Render(line))
	}
	parts = append(parts, body)
	if line := m.box.View(); line != "" {
		parts = append(parts, line)
	}
	for _, line := range m.problemLines() {
		parts = append(parts, style.Error.Render(line))
	}
	return strings.Join(append(parts, m.footer()), "\n")
}

// field is the inline editor, told the one thing it cannot know: whether esc
// will throw the answer away or leave the thing it was opened over in hand.
func (m Model) field() editor.Model {
	f := m.editor.SetWidth(m.width)
	if m.copied != nil {
		return f.WithCancel("go and point at it instead")
	}
	return f
}

// problemLines is the refusal, humanised and wrapped to the terminal.
func (m Model) problemLines() []string {
	if len(m.problem) == 0 {
		return nil
	}
	var out []string
	for _, issue := range m.problem {
		out = append(out, wrap(humanise(issue), max(20, m.width-2))...)
	}
	for i := range out {
		out[i] = "  " + out[i]
	}
	return out
}

// bodyHeight is the room left after the header, the footer, and the input line
// when there is one.
func (m Model) bodyHeight() int {
	// The editor is NOT subtracted: it takes lines from inside the surface
	// rather than from around it, so the screen keeps its shape.
	//
	// The carry banner IS, along with the refusal: both sit outside the surface
	// rather than inside it, so the rows have to give up the lines they take.
	if h := m.height - 4 - m.box.Height() - len(m.problemLines()) - len(m.carryLine()); h > 3 {
		return h
	}
	return 3
}

// countPhrase says how many, and how many of how many when a filter is on.
func (m Model) countPhrase() string {
	noun := spec(m.view).noun
	if noun == "" {
		return ""
	}
	if shown, total, filtered := m.counts(); filtered {
		return fmt.Sprintf("%d of %d %s", shown, total, noun)
	}
	_, total := m.current.Counts()
	return fmt.Sprintf("%d %s", total, noun)
}

// selectionCount is how many rows were explicitly picked on whichever surface
// is showing.
func (m Model) selectionCount() int { return m.current.SelectionCount() }

// counts reports the filtered and total row counts of whichever surface is
// showing, and whether a filter is in force at all.
func (m Model) counts() (shown, total int, filtered bool) {
	if m.current.Filtered() {
		shown, total = m.current.Counts()
		return shown, total, true
	}
	return 0, 0, false
}

func (m Model) header() string {
	// The tab bar drops to numbers alone when the names will not fit.
	//
	// Not cosmetic: a line wider than the terminal WRAPS, which shifts every
	// row below it and makes the whole screen unreadable rather than merely
	// cramped. Found by the Simulator's width check at 60 columns, where the
	// full names come to 65.
	names := len(m.tabLabels(true)) <= m.width
	var rendered []string
	for _, t := range m.tabs() {
		label := t.label(names)
		if t.view == m.view {
			rendered = append(rendered, style.Strong.Render("["+label+"]"))
		} else {
			rendered = append(rendered, style.Dim.Render(" "+label+" "))
		}
	}
	if m.view == viewHistory || m.view == viewHelp {
		suffix := spec(m.view).name
		if !names {
			suffix = suffix[:1]
		}
		rendered = append(rendered, style.Strong.Render("["+suffix+"]"))
	}
	return strings.Join(rendered, " ") + "\n" + style.Dim.Render(strings.Repeat("-", max(10, m.width)))
}

type tab struct {
	view view
	name string
	key  int
}

func (t tab) label(withName bool) string {
	if withName {
		return fmt.Sprintf("%d %s", t.key, t.name)
	}
	return fmt.Sprintf("%d", t.key)
}

func (m Model) tabs() []tab {
	var out []tab
	for _, v := range []view{viewCategories, viewLocations, viewItems, viewHoldings, viewIntegrity} {
		out = append(out, tab{view: v, name: spec(v).name, key: int(v) + 1})
	}
	return out
}

// tabLabels renders the bar as plain text, so its width can be measured before
// any styling is applied. Styling adds escape sequences that occupy no columns,
// which is exactly why measuring the rendered string would be wrong.
func (m Model) tabLabels(withName bool) string {
	var parts []string
	for _, t := range m.tabs() {
		parts = append(parts, " "+t.label(withName)+" ")
	}
	s := strings.Join(parts, " ")
	if m.view == viewHistory {
		if withName {
			s += " [History]"
		} else {
			s += " [H]"
		}
	}
	return s
}

// joinWhatFits joins as many hints as the width allows, dropping from the end.
//
// Named apart from table.fit, which cuts ONE string to a width. Two functions
// called fit that do different things is a name that has to be read twice.
func joinWhatFits(width int, parts []string) string {
	line := ""
	for _, part := range parts {
		next := part
		if line != "" {
			next = line + " - " + part
		}
		if len([]rune(next)) > width {
			break
		}
		line = next
	}
	return line
}

func (m Model) footer() string {
	// Named in the order they would be given up, most useful first, and cut to
	// the terminal rather than allowed to wrap. A line wider than the screen
	// wraps, and one wrapped line shifts every row below it -- which is the
	// same reason the table drops columns instead of overflowing.
	help := joinWhatFits(m.width, []string{
		keys.Hint(keys.Table,
			[]keys.Action{keys.MoveDown, keys.MoveUp},
			[]keys.Action{keys.MoveLeft, keys.MoveRight}),
		"1-5 views",
		keys.Hint(keys.Browse, []keys.Action{keys.Confirm}),
		keys.Hint(keys.Table, []keys.Action{keys.ToggleSelect}, []keys.Action{keys.Sort}),
		keys.Hint(keys.Browse, []keys.Action{keys.Quit}),
	})
	rule := style.Dim.Render(strings.Repeat("-", max(10, m.width)))

	// Assembled as PARTS and joined once, rather than concatenated piece by
	// piece. The first version glued them together with separators baked into
	// each piece and produced "2 items - - sorted by item a-z" the moment one
	// view had nothing to say -- which the golden frames caught.
	//
	// The count leads, and it is built in one place rather than by each view: a
	// count assembled per view is a count that says "3 of 12" in only some of
	// them.
	// While the palette is up it is what the person is looking at, so the
	// footer describes IT. Counting the list underneath would be describing a
	// screen nobody is reading.
	if m.box.Mode() == omnibox.Jump {
		shown, _ := m.jump.Counts()
		return rule + "\n" + style.Dim.Render(fmt.Sprintf(
			"%d matches across every kind - %s go - %s cancel",
			shown, keys.Show(keys.Line, keys.Confirm), keys.Show(keys.Line, keys.Cancel)))
	}

	var parts []string
	if phrase := m.countPhrase(); phrase != "" {
		parts = append(parts, phrase)
	}
	if m.status != "" {
		parts = append(parts, m.status)
	}
	if sorted := m.current.SortDescription(); sorted != "" {
		parts = append(parts, "sorted "+sorted)
	}
	if n := m.selectionCount(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d selected", n))
	}
	status := strings.Join(parts, " - ")

	if status != "" {
		return rule + "\n" + status
	}
	return rule + "\n" + style.Dim.Render(help)
}
