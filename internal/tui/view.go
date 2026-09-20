package tui

import (
	"fmt"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/omnibox"
	"home-management-system/internal/tui/style"
	"home-management-system/internal/tui/text"
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
		body = m.box.Results()
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
		//
		// The height is the same arithmetic every other screen uses, less the
		// tab bar this screen does not have. It used to be its own formula,
		// kept in step with bodyHeight's by hand.
		height := m.bodyHeight() + 2 - m.creator.Height()
		plan := m.flow.plan.SetSize(m.width, height).SetOverlay(m.editor.SetWidth(m.width).Lines())
		parts := []string{plan.View()}
		if m.creator.IsOpen() {
			parts = append(parts, m.creator.SetWidth(m.width).Lines()...)
		}
		// And it ends the way every screen ends: the rule, the status block,
		// the facts, the line. The plan's own counts ARE its facts, which is
		// why they moved out of planview and are handed in here.
		return strings.Join(append(parts,
			m.chrome(text.JoinWhatFits(m.width, m.flow.plan.Facts()))...), "\n")
	}

	return strings.Join(append([]string{m.header(), body}, m.chrome(m.facts())...), "\n")
}

// chrome is everything below the body: the rule, then the one status block,
// then the facts, then the line.
//
// ONE function, and every screen ends with it. What is being carried used to
// sit at the top of the screen with the tabs, the input line below the body,
// the refusals below that, and what an action did inside the footer -- four
// regions for one subject, which is why a person had to look in four places to
// find out what the interface was doing.
//
// The order is the order of LIFETIMES. What outlives this keystroke -- what is
// in hand, what is in flight -- sits above what does not, and the two rows that
// are always exactly one line each sit at the bottom, where they can be relied
// on to be.
func (m Model) chrome(facts string) []string {
	parts := []string{m.rule()}
	parts = append(parts, m.say.Lines(m.width)...)
	parts = append(parts, facts)
	// The line LAST, and always. The bottom row of the screen is the one place
	// a permanently visible thing can be relied upon to be, which is what makes
	// it findable -- the same reason a minibuffer lives there.
	//
	// Sized here, because SetWidth had never been called anywhere: the line
	// rendered against its default 80 whatever the terminal was, which a
	// flush-right tail turns from a latent bug into a visible one.
	return append(parts, m.box.SetWidth(m.width).Offers(m.offered()).View())
}

// rule is the line between what you are reading and what the screen is saying
// about itself.
func (m Model) rule() string { return style.Dim.Render(strings.Repeat("-", max(10, m.width))) }

// field is the inline editor, told the one thing it cannot know: whether esc
// will throw the answer away or leave the thing it was opened over in hand.
func (m Model) field() editor.Model {
	f := m.editor.SetWidth(m.width)
	if m.copied != nil {
		return f.WithCancel("go and point at it instead")
	}
	return f
}

// bodyHeight is the room left once the chrome has taken its lines.
//
// Five are fixed -- two for the tabs and their rule, one for the rule below the
// body, one for the facts, one for the line -- and the status block takes what
// it needs on top. The fixed five are why opening the input line no longer
// resizes the list: its height stopped being a variable.
//
// The editor is NOT subtracted: it takes its lines from inside the surface
// rather than from around it, so the screen keeps its shape.
func (m Model) bodyHeight() int {
	if h := m.height - 5 - m.say.Height(m.width); h > 3 {
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

// facts is what is true of the list in front of you: how many, how many picked,
// how sorted, and the one hint the view itself supplies.
//
// It no longer carries outcomes or refusals. Those have their own lines in the
// block above, with their own lifetimes, and squeezing them in here is what
// made the key hints below unreachable -- the hints rendered only when this
// line was entirely empty, and a count or a view hint is nearly always present,
// so five hints sat in the code that no screen ever showed.
//
// Assembled as PARTS and joined once, rather than concatenated piece by piece.
// The first version glued them together with separators baked into each piece
// and produced "2 items - - sorted by item a-z" the moment one view had nothing
// to say -- which the golden frames caught.
func (m Model) facts() string {
	// While the palette is up it is what the person is looking at, so this
	// describes IT. Counting the list underneath would be describing a screen
	// nobody is reading.
	if m.box.Mode() == omnibox.Jump {
		return style.Dim.Render(fmt.Sprintf("%d matches across every kind", m.box.Matches()))
	}

	// The count leads, and it is built in one place rather than by each view: a
	// count assembled per view is a count that says "3 of 12" in only some of
	// them.
	var parts []string
	if phrase := m.countPhrase(); phrase != "" {
		parts = append(parts, phrase)
	}
	if n := m.selectionCount(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d selected", n))
	}
	if sorted := m.current.SortDescription(); sorted != "" {
		parts = append(parts, "sorted "+sorted)
	}
	if hint := m.say.Hint(); hint != "" {
		parts = append(parts, hint)
	}
	return style.Dim.Render(text.JoinWhatFits(m.width, parts))
}
