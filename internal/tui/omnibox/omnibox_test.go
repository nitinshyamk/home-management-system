package omnibox_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/omnibox"
)

func typeInto(m omnibox.Model, s string) omnibox.Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// The two modes have to look different BEFORE a word is read. They feel
// identical for exactly one keystroke and then diverge completely, so a person
// who has to work out which they are in has been failed by the widget whatever
// else it does well.
func TestTheTwoModesLookDifferent(t *testing.T) {
	filter := typeInto(omnibox.New().Open(omnibox.Filter), "rice")
	jump := typeInto(omnibox.New().Open(omnibox.Jump), "rice")

	if strip(filter.View()) == strip(jump.View()) {
		t.Fatalf("filter and jump render identically: %q", strip(filter.View()))
	}
	// Different in their PROMPT, not merely in some styling a monochrome
	// terminal would drop.
	if !strings.Contains(strip(filter.View()), "I-search") {
		t.Errorf("the filter has no distinguishing prompt: %q", strip(filter.View()))
	}
	if !strings.Contains(strip(jump.View()), "JUMP") {
		t.Errorf("the jump does not say what it is: %q", strip(jump.View()))
	}
}

// Escape is never destructive. Cancelling must not clear a filter that was
// already applied.
func TestCancelIsNeverDestructive(t *testing.T) {
	m := typeInto(omnibox.New().Open(omnibox.Filter), "ancho").Accept()
	if m.Applied() != "ancho" {
		t.Fatalf("applied = %q", m.Applied())
	}

	m = typeInto(m.Open(omnibox.Jump), "shelf").Cancel()
	if m.Applied() != "ancho" {
		t.Errorf("cancelling a jump cleared the filter: %q", m.Applied())
	}
	m = typeInto(m.Open(omnibox.Filter), "xyz").Cancel()
	if m.Applied() != "ancho" {
		t.Errorf("cancelling an edit changed the filter to %q", m.Applied())
	}
	// Clearing is its own act, from the list rather than the line.
	if got := m.Clear().Applied(); got != "" {
		t.Errorf("clear left %q", got)
	}
}

// Refining a filter starts from what is already applied, so narrowing further
// does not mean retyping it.
func TestOpeningTheFilterResumesIt(t *testing.T) {
	m := typeInto(omnibox.New().Open(omnibox.Filter), "loc:garage").Accept()
	m = m.Open(omnibox.Filter)
	if m.Input() != "loc:garage" {
		t.Errorf("reopening the filter gave %q, want the applied filter back", m.Input())
	}
	// A jump starts blank -- it searches everything, so the filter would be a
	// restriction nobody asked for.
	if got := m.Open(omnibox.Jump).Input(); got != "" {
		t.Errorf("opening a jump inherited %q", got)
	}
}

// The line is always there, and always the same shape.
//
// This reverses an earlier decision -- a closed omnibox used to render nothing
// and report a height of 0, on the argument that "a line that is always there
// is a row of the table that never is". Density won, and three things lost:
// nothing said the line existed or which keys reached it, the body resized
// whenever it opened, and each state began its text at its own column.
//
// A row of the table is a fair price for the one line where all typing happens
// being findable. What is NOT negotiable is that the price stops changing,
// which is what this asserts.
func TestTheLineIsAlwaysOneLine(t *testing.T) {
	filtered := typeInto(omnibox.New().Open(omnibox.Filter), "rice").Accept()
	for name, m := range map[string]omnibox.Model{
		"resting":            omnibox.New(),
		"resting, filtered":  filtered,
		"filtering":          typeInto(omnibox.New().Open(omnibox.Filter), "rice"),
		"jumping":            typeInto(omnibox.New().Open(omnibox.Jump), "rice"),
		"running a command":  typeInto(omnibox.New().Open(omnibox.Command), "consume 100g"),
		"filtering, empty":   omnibox.New().Open(omnibox.Filter),
		"a very long filter": typeInto(omnibox.New().Open(omnibox.Filter), strings.Repeat("x", 200)),
	} {
		if got := m.Height(); got != 1 {
			t.Errorf("%s: Height() = %d, want 1", name, got)
		}
		if got := strings.Count(m.View(), "\n"); got != 0 {
			t.Errorf("%s: the view is %d lines, want 1: %q", name, got+1, strip(m.View()))
		}
	}

	// And a resting filter still says it is on, since a narrowed list that does
	// not say so is a list that lies about what you own.
	if !strings.Contains(strip(filtered.View()), "rice") {
		t.Errorf("a resting filter does not show itself: %q", strip(filtered.View()))
	}
}

// The text begins at the same column in every state.
//
// Before the gutter was a fixed width the four prompts were 10, 6, 5 and 9
// columns wide, so switching modes slid what you were typing sideways. The
// gutter is sized from the widest label, which is why this can be asserted
// without naming a number: whatever the column IS, every state has to agree
// about it.
func TestTheTextBeginsAtTheSameColumnInEveryState(t *testing.T) {
	columns := map[string]int{}
	for name, m := range map[string]omnibox.Model{
		"filtering":         typeInto(omnibox.New().Open(omnibox.Filter), "rice"),
		"jumping":           typeInto(omnibox.New().Open(omnibox.Jump), "rice"),
		"running a command": typeInto(omnibox.New().Open(omnibox.Command), "rice"),
		"resting, filtered": typeInto(omnibox.New().Open(omnibox.Filter), "rice").Accept(),
	} {
		at := strings.Index(strip(m.View()), "rice")
		if at < 0 {
			t.Fatalf("%s: the view does not contain what was typed: %q", name, strip(m.View()))
		}
		columns[name] = at
	}
	var first string
	for name, at := range columns {
		if first == "" {
			first = name
			continue
		}
		if at != columns[first] {
			t.Errorf("the text starts at column %d when %s but column %d when %s",
				at, name, columns[first], first)
		}
	}
}

// Resting, the line says how to reach it. It is the only place on the screen
// that can: a key nobody is told about is a key nobody presses.
func TestTheRestingLineNamesTheKeysThatOpenIt(t *testing.T) {
	got := strip(omnibox.New().SetWidth(100).View())
	for _, want := range []string{"C-s", "M-g", "M-x"} {
		if !strings.Contains(got, want) {
			t.Errorf("the resting line does not name %s: %q", want, got)
		}
	}
}

// Nothing runs past the terminal. A line wider than the screen wraps, and one
// wrapped line shifts every row above it -- which for the bottom line of the
// screen means the whole screen.
func TestTheLineFitsTheTerminal(t *testing.T) {
	for _, width := range []int{40, 60, 80, 100} {
		for name, m := range map[string]omnibox.Model{
			"resting":           omnibox.New(),
			"resting, filtered": typeInto(omnibox.New().Open(omnibox.Filter), "loc:tray").Accept(),
			"filtering":         typeInto(omnibox.New().Open(omnibox.Filter), "rice"),
			"jumping":           typeInto(omnibox.New().Open(omnibox.Jump), "shelf"),
			"running a command": typeInto(omnibox.New().Open(omnibox.Command), "consume 100g of rice"),
		} {
			line := strip(m.SetWidth(width).View())
			if n := len([]rune(line)); n > width {
				t.Errorf("%s at width %d is %d columns: %q", name, width, n, line)
			}
		}
	}
}

func TestBackspace(t *testing.T) {
	m := typeInto(omnibox.New().Open(omnibox.Filter), "rice")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.Input() != "ric" {
		t.Errorf("input = %q", m.Input())
	}
	// Backspacing an empty line is not an error.
	for i := 0; i < 10; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if m.Input() != "" {
		t.Errorf("input = %q", m.Input())
	}
}

// A closed line consumes nothing, so every key still reaches the application.
func TestAClosedLineSwallowsNothing(t *testing.T) {
	if _, handled := omnibox.New().Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}); handled {
		t.Error("a closed omnibox swallowed a keystroke")
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

// Three modes now, and they have to differ in KIND rather than by degree of
// emphasis: a slash, a reversed badge, a tinted badge. The old interface's
// defect was modes you could not tell apart from the screen.
func TestAllThreeModesLookDifferent(t *testing.T) {
	seen := map[string]omnibox.Mode{}
	for _, mode := range []omnibox.Mode{omnibox.Filter, omnibox.Jump, omnibox.Command} {
		view := strip(typeInto(omnibox.New().Open(mode), "rice").View())
		if prior, dup := seen[view]; dup {
			t.Errorf("modes %v and %v render identically: %q", prior, mode, view)
		}
		seen[view] = mode
	}
	// The command line spells its own key, the way emacs does.
	if got := strip(typeInto(omnibox.New().Open(omnibox.Command), "consume 100g").View()); !strings.Contains(got, "M-x") {
		t.Errorf("the command line has no leader: %q", got)
	}
}

// A command line is not a filter, so accepting one must not leave a filter
// behind.
func TestAcceptingACommandDoesNotBecomeAFilter(t *testing.T) {
	m := typeInto(omnibox.New().Open(omnibox.Filter), "ancho").Accept()
	m = typeInto(m.Open(omnibox.Command), "consume 100g").Accept()
	if m.Applied() != "ancho" {
		t.Errorf("applied filter = %q after running a command", m.Applied())
	}
}

// A real terminal sends KeySpace, never a space rune. The first version of
// Update handled both in one case, appended msg.Runes AND a literal space, then
// trimmed a double -- which trimmed both, so typing a space did nothing.
//
// Every test was green, because the harness's Type() was sending runes. So this
// sends the message a terminal actually sends, which is the only kind of test
// that could have caught it.
func TestASpaceFromARealTerminalIsASpace(t *testing.T) {
	m := omnibox.New().Open(omnibox.Command)
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("consume")},
		{Type: tea.KeySpace, Runes: []rune{' '}},
		{Type: tea.KeyRunes, Runes: []rune("100g")},
	} {
		m, _ = m.Update(msg)
	}
	if m.Input() != "consume 100g" {
		t.Errorf("input = %q, want %q", m.Input(), "consume 100g")
	}
}

// Out of reach, the line names no keys.
//
// It is drawn under the modal screens too -- a confirmation, an open field, the
// import plan -- and under those every keystroke belongs to the layer on top,
// so the three leaders would do nothing. A permanently visible affordance that
// is sometimes false is worse than none at all: the only reason to put it on
// the screen is that it can be believed.
func TestAnUnreachableLineOffersNothing(t *testing.T) {
	out := strip(omnibox.New().SetWidth(100).Reachable(false).View())
	for _, key := range []string{"C-s", "M-g", "M-x", "esc"} {
		if strings.Contains(out, key) {
			t.Errorf("an unreachable line still offers %s: %q", key, out)
		}
	}
	// It goes quiet, not away. The row it occupies is the same row, or the
	// screen would jump every time a confirmation opened -- which is the thing
	// the fixed height exists to prevent.
	if omnibox.New().Reachable(false).Height() != 1 {
		t.Error("an unreachable line gave its row back")
	}

	// An applied filter is still SAID, because it is a fact about the rows on
	// screen rather than an invitation to press anything.
	filtered := typeInto(omnibox.New().Open(omnibox.Filter), "loc:tray").Accept()
	out = strip(filtered.SetWidth(100).Reachable(false).View())
	if !strings.Contains(out, "loc:tray") {
		t.Errorf("an unreachable line stopped saying the list was filtered: %q", out)
	}
	if strings.Contains(out, "clear") {
		t.Errorf("an unreachable line still offers to clear the filter: %q", out)
	}
}
