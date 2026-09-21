package tui

import (
	"fmt"
	"strings"

	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/omnibox"
	"home-management-system/internal/tui/style"
	"home-management-system/internal/tui/text"
)

// Drawing: the screen, and the chrome around whatever is on it.

func (m Model) View() string {
	if !m.ready {
		return "loading..."
	}
	// The one-line field goes INTO the surface, spliced after the row it
	// belongs to. Under the list is not inline; it is a second place to look.
	//
	// The creation PANEL does not, any more. It is a form with completions
	// under it, and splicing it into a pane a third of the screen wide cut
	// "Garage > Metal Shelving Unit" off mid-path and the hint off mid-word.
	// It is drawn full width below the house instead, which states the parent
	// in words where position used to imply it.
	body := m.current.SetOverlay(m.field().Lines()).View()
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
		// The plan sits BELOW the house rather than instead of it.
		//
		// A row saying "add 100 g to Shelf 1" is unreadable without the thing
		// it is proposed against -- to what, and how much was there? Taking
		// the screen made that the one question the review screen could not
		// answer, and moving the cursor through the rows now points the house
		// at what each one would touch.
		//
		// The field goes INTO the plan, spliced after its row. The creation
		// panel does not: it is about a thing that does not exist yet rather
		// than about the row's text, and it is tall enough that splicing it
		// would push the plan off the screen it is confirming.
		plan := m.flow.plan.
			SetSize(m.width, m.drawerHeight()).
			SetOverlay(m.editor.SetWidth(m.width).Lines())

		parts := []string{m.header(), body, m.rule(), plan.View()}
		if m.creator.IsOpen() {
			parts = append(parts, m.creator.SetWidth(m.width).Lines()...)
		}
		// The plan's own counts ARE its facts, which is why they moved out of
		// planview and are handed in here.
		return strings.Join(append(parts,
			m.chrome(joinQuietly(m.width, m.flow.plan.Facts()))...), "\n")
	}

	parts := []string{m.header(), body}
	if m.creator.IsOpen() {
		parts = append(parts, m.creator.SetWidth(m.width).Lines()...)
	}
	return strings.Join(append(parts, m.chrome(m.facts())...), "\n")
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

// bodyHeight is the room left once the chrome, and anything in the drawer,
// have taken their lines.
//
// Five are fixed -- the header and its rule, the rule below the body, the
// facts, the line -- and the status block takes what it needs on top.
//
// The one-line editor is NOT subtracted: it takes its lines from inside the
// surface. The creation panel and the review drawer are, because they sit
// below the house rather than in it.
func (m Model) bodyHeight() int {
	room := m.height - 5 - m.say.Height(m.width) - m.creator.Height()
	if m.flow.reviewing() {
		room -= m.drawerHeight() + 1 // and the rule above it
	}
	if room > 3 {
		return room
	}
	return 3
}

// drawerHeight is what the review drawer gets: as much as it needs for its
// rows, and never so much that the house stops being visible behind it.
//
// The house keeps at least a few rows, because a drawer that squeezed it to
// nothing would be the full-screen takeover again with an extra rule drawn
// across it.
func (m Model) drawerHeight() int {
	const (
		leastHouse  = 6
		leastDrawer = 6 // the chrome, and one row to look at
	)
	room := m.height - 5 - m.say.Height(m.width) - m.creator.Height() - leastHouse - 1
	return max(leastDrawer, min(m.flow.plan.Wants(), room))
}

// countPhrase says how many, and how many of how many when a filter is on.
func (m Model) countPhrase() string {
	noun := m.noun()
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

// facts is the inspector: what the cursor is on, said in words.
//
// It used to be what is true of the LIST -- how many, how many picked, how
// sorted -- and that moved: the count to the header, beside the node it
// counts, and the rest into the one line below. What a person cannot see is
// the thing under the cursor, and that is what this says now.
//
// While the palette is up it describes ITSELF, because that is what the
// person is looking at; counting the list underneath would be describing a
// screen nobody is reading.
func (m Model) facts() string {
	if m.box.Mode() == omnibox.Jump {
		return style.Dim.Render(fmt.Sprintf("%d matches across every kind", m.box.Matches()))
	}

	// A filter and a selection are things a person DID, and they outrank a
	// description of where the cursor happens to be: not knowing that three
	// rows are hidden is how a verb ends up acting on the wrong set.
	var parts []string
	if shown, total, filtered := m.counts(); filtered {
		parts = append(parts, fmt.Sprintf("%d of %d %s", shown, total, m.noun()))
	}
	if n := m.selectionCount(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d selected", n))
	}
	if len(parts) > 0 {
		if hint := m.say.Hint(); hint != "" {
			parts = append(parts, hint)
		}
		return joinQuietly(m.width, parts)
	}
	if inspected := m.inspect(); inspected != "" {
		return inspected
	}
	return joinQuietly(m.width, []string{m.countPhrase(), m.say.Hint()})
}

// joinQuietly dims the parts that are not already saying something for
// themselves, then joins. Dimming the JOINED string instead puts a reset in
// the middle of the outer style, and everything after the first styled part
// comes out at full brightness.
func joinQuietly(width int, parts []string) string {
	for i, p := range parts {
		parts[i] = quiet(p)
	}
	return text.JoinWhatFits(width, parts)
}

// noun is what the thing on screen is a list of.
func (m Model) noun() string {
	if m.view == viewShell {
		return m.lens.spec().noun
	}
	return spec(m.view).noun
}
