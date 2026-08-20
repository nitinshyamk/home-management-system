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
	if !strings.HasPrefix(strip(filter.View()), "/") {
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

// A closed omnibox with no filter costs nothing. Density matters, and a line
// that is always there is a row of the table that never is.
func TestAClosedOmniboxCostsNoLine(t *testing.T) {
	m := omnibox.New()
	if m.Height() != 0 || m.View() != "" {
		t.Errorf("a resting omnibox occupies %d lines: %q", m.Height(), m.View())
	}
	applied := typeInto(m.Open(omnibox.Filter), "rice").Accept()
	if applied.Height() != 1 {
		t.Errorf("an applied filter occupies %d lines", applied.Height())
	}
	// And it says the filter is still on, since a narrowed list that does not
	// say so is a list that lies about what you own.
	if !strings.Contains(strip(applied.View()), "rice") {
		t.Errorf("a resting filter does not show itself: %q", strip(applied.View()))
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
	if got := strip(typeInto(omnibox.New().Open(omnibox.Command), "consume 100g").View()); !strings.Contains(got, ":") {
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
