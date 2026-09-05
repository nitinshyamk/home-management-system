package complete_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/complete"
	"home-management-system/internal/tui/keys"
)

func key(name string) tea.KeyMsg {
	msg, ok := keys.Named(name)
	if !ok {
		panic(name + " is not a key")
	}
	return msg
}

// press sends a script of keys, returning the model and the last option taken.
func press(m complete.Model, names ...string) (complete.Model, string) {
	took := ""
	for _, name := range names {
		next, taken, _ := m.Update(key(name))
		m = next
		if taken != "" {
			took = taken
		}
	}
	return m, took
}

func offered() complete.Model {
	return complete.Model{}.Offer([]string{"Garage", "Garden", "Gate"}, "ga")
}

// Tab takes what is HIGHLIGHTED, not what is first.
//
// This is the whole reason the component exists. Both widgets it replaces took
// suggestions[0] and had no selection at all, so every option below the first
// was decoration.
func TestTabTakesTheHighlightedOption(t *testing.T) {
	if _, took := press(offered(), "tab"); took != "Garage" {
		t.Errorf("tab took %q, want the first option", took)
	}
	if _, took := press(offered(), "ctrl+n", "ctrl+n", "tab"); took != "Gate" {
		t.Errorf("two C-n then tab took %q, want Gate", took)
	}
	if _, took := press(offered(), "ctrl+p", "tab"); took != "Gate" {
		t.Errorf("C-p from the top took %q, want the list to wrap to Gate", took)
	}
}

// Taking one closes the list, so the next Tab is the owner's again.
func TestTakingClosesTheList(t *testing.T) {
	m, _ := press(offered(), "tab")
	if m.IsOpen() {
		t.Error("the list stayed open after an option was taken")
	}
	if _, _, handled := m.Update(key("tab")); handled {
		t.Error("a closed list ate the next tab; the field can never move on")
	}
}

// esc and S-Tab are the same key by two names while a list is open.
func TestBothWaysOutDismissIt(t *testing.T) {
	for _, name := range []string{"esc", "ctrl+g", "shift+tab"} {
		m, _ := press(offered(), name)
		if m.IsOpen() {
			t.Errorf("%s did not put the list away", name)
		}
	}
}

// Dismissal survives the recompute that every keystroke triggers.
//
// Without this, esc read as a flicker: the list came straight back, because the
// keystroke after it is what recomputes the options.
func TestDismissalSurvivesTyping(t *testing.T) {
	m, _ := press(offered(), "esc")
	m = m.Offer([]string{"Garage", "Garden"}, "ga")
	if m.IsOpen() {
		t.Error("the list came back on the next keystroke; esc means nothing")
	}
}

// And does not survive arriving at the field again.
func TestDismissalDoesNotOutliveTheVisit(t *testing.T) {
	m, _ := press(offered(), "esc")
	m = m.Arrive().Offer([]string{"Garage", "Garden"}, "ga")
	if !m.IsOpen() {
		t.Error("a field dismissed once is dismissed forever")
	}
}

// A closed list owns nothing, which is what lets the owner bind the same keys.
func TestAClosedListHandlesNothing(t *testing.T) {
	var m complete.Model
	for _, name := range []string{"tab", "ctrl+n", "ctrl+p", "shift+tab", "esc"} {
		if _, _, handled := m.Update(key(name)); handled {
			t.Errorf("a closed list consumed %s", name)
		}
	}
}

// Typing refines: keys the list does not own fall through to the field.
func TestTypingFallsThrough(t *testing.T) {
	m := offered()
	for _, name := range []string{"a", "g", "space", "backspace", "ctrl+a", "ctrl+k"} {
		if _, _, handled := m.Update(key(name)); handled {
			t.Errorf("the list swallowed %s; typing would not reach the field", name)
		}
	}
}

// A highlight the person MOVED follows its option through a recompute.
//
// The options are recomputed on every keystroke. A highlight that reset to the
// top each time would make C-n unusable, because typing would undo it.
func TestAChosenHighlightSurvivesARecompute(t *testing.T) {
	m, _ := press(offered(), "ctrl+n")
	if got, _ := m.Selected(); got != "Garden" {
		t.Fatalf("C-n highlighted %q", got)
	}
	m = m.Offer([]string{"Gate", "Garden", "Garage"}, "ga")
	if got, _ := m.Selected(); got != "Garden" {
		t.Errorf("after the list was recomputed the highlight moved to %q", got)
	}
}

// A highlight nobody moved goes back to the best match.
//
// Without this it kept whatever ranked first for the first letter typed: `s`
// highlighted a shelf, and by `shel` that shelf had sunk to the bottom with the
// highlight riding down, so the best match was never what Tab would take.
func TestAnUntouchedHighlightFollowsTheRanking(t *testing.T) {
	m := complete.Model{}.Offer([]string{"Shelf 2", "Small Parts Tray"}, "s")
	m = m.Offer([]string{"Small Parts Tray", "Shelf 2"}, "sm")
	if got, _ := m.Selected(); got != "Small Parts Tray" {
		t.Errorf("highlight = %q, want the new best match", got)
	}
}

// And falls back to the top when what was highlighted is gone.
func TestTheHighlightFallsBackWhenItsOptionGoes(t *testing.T) {
	m, _ := press(offered(), "ctrl+n")
	m = m.Offer([]string{"Gate", "Gateway"}, "ga")
	if got, _ := m.Selected(); got != "Gate" {
		t.Errorf("highlight = %q, want the top of the new list", got)
	}
}

// The hint sits on the highlight, because that is what tab would take.
func TestTheHintFollowsTheHighlight(t *testing.T) {
	m, _ := press(offered(), "ctrl+n")
	lines := m.Lines("", 60)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if !strings.Contains(lines[1], "to take it") {
		t.Errorf("the hint is not on the highlighted row:\n%s", strings.Join(lines, "\n"))
	}
	if strings.Contains(lines[0], "to take it") {
		t.Errorf("the hint is still pinned to the first row:\n%s", strings.Join(lines, "\n"))
	}
}

// An empty list is not a dropdown.
func TestNothingToOfferIsNotOpen(t *testing.T) {
	var empty complete.Model
	if empty.Offer(nil, "").IsOpen() {
		t.Error("an empty list opened")
	}
}

// Tab is not ours when taking would change nothing.
//
// Typing `g` into the unit field offers `g` and `kg`; Tab there should reach
// the next field, not spend a keystroke re-taking the g. This is what makes Tab
// one key rather than two behaviours to keep track of, and it is the rule the
// creation panel had before the dropdown existed.
func TestTabFallsThroughWhenItWouldChangeNothing(t *testing.T) {
	m := complete.Model{}.Offer([]string{"g", "kg"}, "g")
	next, taken, handled := m.Update(key("tab"))
	if handled || taken != "" {
		t.Errorf("tab took %q on a field that already held it", taken)
	}
	// But moving off it and taking the other one works.
	moved, _ := press(next, "ctrl+n")
	_, taken, handled = moved.Update(key("tab"))
	if !handled || taken != "kg" {
		t.Errorf("after C-n, tab took %q, want kg", taken)
	}
}

// Taking an option settles the field for the rest of the visit.
//
// The list is recomputed on every keystroke, so without this it re-offered the
// option just taken: the field kept saying "take it" about the value it was
// already holding, and the next Tab had to fight past it.
func TestTakingSettlesTheField(t *testing.T) {
	m, took := press(offered(), "tab")
	if took != "Garage" {
		t.Fatalf("tab took %q", took)
	}
	m = m.Offer([]string{"Garage", "Garden"}, "Garage")
	if m.IsOpen() {
		t.Error("the list re-offered the option that was just taken")
	}
	// Leaving and coming back offers again.
	if !m.Arrive().Offer([]string{"Garage"}, "").IsOpen() {
		t.Error("a field is offered nothing ever again after one take")
	}
}
