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
	// Sized HERE rather than when a drawer opens. bodyHeight depends on what
	// is in the drawer, so every open and close would otherwise have to
	// remember to resize the house -- and the one that forgot would draw a
	// screen taller than the terminal, which wraps and shifts every row.
	house := m.current.SetSize(m.width, m.bodyHeight()).SetOverlay(m.field().Lines())
	if blurrable, ok := house.(interface{ Blur() surface }); ok && m.top() != nil && !m.drawerShares() {
		// A drawer with a cursor of its own is the only cursor on screen. The
		// same argument the two panes settled between themselves, one level
		// up. The exception is a drawer that shares the keyboard: organise
		// mode edits the tree in the rail, so blurring it would hide the one
		// cursor the mode is about.
		house = blurrable.Blur()
	}
	body := house.View()
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

	parts := append([]string{m.header(), body}, m.drawerLines()...)
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
	// What wants answering, above what just happened, because it outlives
	// it: a jar that expired last week is still expired after the next
	// keystroke, and the order of this block is the order of lifetimes.
	if line := m.attentionLine(); line != "" {
		parts = append(parts, line)
	}
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

// field is the inline editor, sized to the screen.
func (m Model) field() editor.Model { return m.editor.SetWidth(m.width) }

// room is the terminal less the chrome: five fixed lines -- the header and
// its rule, the rule below the body, the facts, the line -- with the status
// block and the creation panel taking what they need on top of those.
//
// ONE place, because the drawer sizes itself against the same number the
// house is sized against. Written out twice, the two copies drift, and what
// that produces is a screen a line too tall -- which wraps, and shifts every
// row on it.
//
// The one-line editor is NOT subtracted: it takes its lines from inside the
// surface. The creation panel is, because it sits below the house.
func (m Model) room() int {
	return m.height - 5 - m.say.Height(m.width) - m.creator.Height() - m.bannerHeight()
}

// bannerHeight is the row the attention line takes when it has something to
// say, and nothing when it does not.
//
// Counted HERE, with the rest of the chrome, which is the whole reason room
// is one function. A line added to the bottom of the screen without being
// subtracted from the top is a screen one row too tall -- it wraps, and one
// wrapped line shifts every row above it.
func (m Model) bannerHeight() int {
	if m.attentionLine() == "" {
		return 0
	}
	return 1
}

// bodyHeight is what is left for the house once the drawer has taken its
// share, and never less than a few rows.
func (m Model) bodyHeight() int {
	return max(3, m.room()-m.drawerRoom())
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
	// A drawer that describes itself outranks everything below it. What a
	// person is working on is in the region, not in the house behind it.
	if said, ok := m.drawerFacts(); ok {
		return joinQuietly(m.width, said)
	}
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
