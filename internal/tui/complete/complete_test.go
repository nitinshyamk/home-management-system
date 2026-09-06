package complete_test

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/complete"
	"home-management-system/internal/tui/keys"
)

// Shorthands, so a test that needs the real ranking reads as one thought.
type Match = complete.Match

var Options = complete.Options

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

// Taking an option does NOT put the list away for the visit -- it closes it and
// lets the next recompute decide whether there is still a choice to make.
//
// The first version did settle the field, and that made a hierarchy impossible
// to walk. Taking "Garage" ended completion for the visit, so the way to reach
// "Garage > Metal Shelving Unit" was to type the rest of it by hand, and
// backspacing to correct a take left a field with no completions at all.
//
// What settling was really guarding against is handled where it belongs: the
// matcher offers nothing when the only match IS the value, and Tab falls through
// rather than re-taking one. Both are checked below through the real matcher,
// because that pairing is the whole reason this can be left open.
func TestTakingOpensTheNextChoice(t *testing.T) {
	house := []Match{
		{Path: "Garage", Leaf: "Garage"},
		{Path: "Garage > Metal Shelving Unit", Leaf: "Metal Shelving Unit"},
		{Path: "Garden", Leaf: "Garden"},
	}
	m, took := press(complete.Model{}.Offer(Options(house, "gar"), "gar"), "tab")
	if took != "Garage" {
		t.Fatalf("tab took %q, want the Garage", took)
	}
	// What the owner does next: put the taken value in the field and recompute.
	m = m.Offer(Options(house, took), took)
	if !m.IsOpen() {
		t.Fatal("taking a parent ended completion, so its children are unreachable")
	}
	if option, _ := m.Selected(); option != "Garage" {
		t.Errorf("the list came back highlighting %q, want the exact match", option)
	}
	// And Tab is the owner's again, because taking it would change nothing.
	if _, _, handled := m.Update(key("tab")); handled {
		t.Error("tab re-took the value the field already held")
	}

	// A take with nothing else near it closes and stays closed.
	only := []Match{{Path: "Garden", Leaf: "Garden"}}
	m, took = press(complete.Model{}.Offer(Options(only, "gard"), "gard"), "tab")
	if m.Offer(Options(only, took), took).IsOpen() {
		t.Error("the list re-offered the one value the field already held")
	}
}

// The arrows walk the list, wherever the list is.
//
// They were bound on the creation panel's dropdown and not on the prompt's, so
// the same list answered the same key in one place and swallowed it in the
// other. A dropdown is the one thing in a terminal everybody reaches for the
// down arrow at.
func TestTheArrowsWalkTheList(t *testing.T) {
	if _, took := press(offered(), "down", "down", "tab"); took != "Gate" {
		t.Errorf("two downs then tab took %q, want Gate", took)
	}
	if _, took := press(offered(), "up", "tab"); took != "Gate" {
		t.Errorf("up from the top took %q, want the list to wrap to Gate", took)
	}
}

// Enter takes the highlight, which is what a person means by it once they have
// pointed at something.
func TestEnterTakesWhatWasChosen(t *testing.T) {
	if _, took := press(offered(), "ctrl+n", "enter"); took != "Garden" {
		t.Errorf("enter took %q, want the highlighted Garden", took)
	}
	// Typed towards is enough; the highlight need not have been moved.
	if _, took := press(offered(), "enter"); took != "Garage" {
		t.Errorf("enter on a typed-towards list took %q, want Garage", took)
	}
}

// Enter on an untouched empty field is NOT a choice.
//
// Something has to be highlighted for the list to have a highlight at all, and
// on arrival that something is whichever option happened to rank first. Taking
// it would be the interface answering the question on the person's behalf --
// so enter falls through, and the owner refuses the empty field as it always
// did.
func TestEnterDoesNotChooseForYou(t *testing.T) {
	arrived := complete.Model{}.Offer([]string{"Garage", "Garden"}, "")
	next, took, handled := arrived.Update(key("enter"))
	if handled || took != "" {
		t.Errorf("enter took %q from an untouched empty field", took)
	}
	// But Tab is explicit, so Tab still takes.
	if _, took := press(arrived, "tab"); took != "Garage" {
		t.Errorf("tab took %q, want Garage", took)
	}
	// And once the highlight has been moved, enter is a choice.
	if _, took := press(next, "ctrl+n", "enter"); took != "Garden" {
		t.Errorf("enter after a move took %q, want Garden", took)
	}
}

// A list longer than the window scrolls to what is highlighted, rather than
// cutting the matches off at whatever fits.
//
// The cap used to BE the window, so in a house with more places than rows the
// later matches were not merely off-screen -- they were not in the list, and no
// keystroke could reach them.
func TestALongListScrollsToTheHighlight(t *testing.T) {
	var many []string
	for i := 0; i < complete.Window*3; i++ {
		many = append(many, fmt.Sprintf("Shelf %d", i))
	}
	m := complete.Model{}.Offer(many, "shelf")

	shows := func(m complete.Model, option string) bool {
		for _, line := range m.Lines("", 60) {
			if strings.Contains(line, option) {
				return true
			}
		}
		return false
	}
	if shows(m, many[len(many)-1]) {
		t.Fatal("the whole list is drawn, so there is no window to test")
	}
	if n := len(m.Lines("", 60)); n > complete.Window+2 {
		t.Errorf("the list drew %d lines over a field, want at most %d",
			n, complete.Window+2)
	}

	// C-n all the way to the last option, which must be both reachable and
	// visible once reached.
	for i := 0; i < len(many)-1; i++ {
		m, _ = press(m, "ctrl+n")
	}
	if option, _ := m.Selected(); option != many[len(many)-1] {
		t.Fatalf("walking the list stopped at %q", option)
	}
	if !shows(m, many[len(many)-1]) {
		t.Errorf("the highlight is off the window:\n%s", strings.Join(m.Lines("", 60), "\n"))
	}
	// And it says what is off the top, so the list does not look complete.
	if !shows(m, "more above") {
		t.Error("a scrolled list does not say there is more above it")
	}
}

// C-n brings back a list that was put away.
//
// It was a dead key: a dismissed list is closed, so Update declined the
// keystroke, the line editor has no motion of its own to give it, and the field
// swallowed it. Having pressed esc to see the row underneath, there was no way
// back to the list short of retyping the field -- so esc was a one-way door in
// the middle of a field somebody was still filling in.
func TestTheListCanBeBroughtBack(t *testing.T) {
	m, _ := press(offered(), "esc")
	if m.IsOpen() {
		t.Fatal("esc did not put the list away")
	}
	// What the owner does on every keystroke: recompute and re-offer.
	if m.Offer([]string{"Garage", "Garden", "Gate"}, "ga").IsOpen() {
		t.Fatal("a dismissed list came back on its own")
	}

	next, _, handled := m.Update(key("ctrl+n"))
	if !handled {
		t.Error("C-n on a dismissed list was declined, so nothing will happen at all")
	}
	back := next.Offer([]string{"Garage", "Garden", "Gate"}, "ga")
	if !back.IsOpen() {
		t.Fatal("C-n did not bring the list back")
	}
	// At the best match, not at whatever was highlighted when it was dismissed.
	if option, _ := back.Selected(); option != "Garage" {
		t.Errorf("the list came back highlighting %q, want the best match", option)
	}
}

// A field that has simply not been offered anything is a different thing from a
// dismissed one, and C-n has to fall through there -- in the creation panel it
// is how you reach the next field.
func TestCNFallsThroughOnAFieldWithNoList(t *testing.T) {
	if _, _, handled := (complete.Model{}).Update(key("ctrl+n")); handled {
		t.Error("C-n was taken by a list that was never offered")
	}
	fresh := complete.Model{}.Offer([]string{"Garage"}, "ga").Arrive()
	if _, _, handled := fresh.Update(key("ctrl+n")); handled {
		t.Error("C-n was taken after arriving at a new field")
	}
}
