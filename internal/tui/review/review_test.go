package review_test

import (
	"strings"
	"testing"

	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/review"
	"home-management-system/internal/tui/text"
)

func change(at, what string, state review.State) review.Change {
	return review.Change{State: state, At: at, What: what}
}

func newScreen(changes ...review.Change) review.Model {
	return review.New("IMPORT", "receipt.csv", changes).SetSize(120, 20)
}

// screen is the review as the application draws it: the rows, and the facts
// the application renders under them.
func screen(m review.Model) string {
	return text.StripANSI(m.View() + "\n" + strings.Join(m.Facts(), " - "))
}

func press(m review.Model, name string) review.Model {
	msg, ok := keys.Named(name)
	if !ok {
		panic("no key called " + name)
	}
	m, _ = m.Update(msg)
	return m
}

// TestEveryChangeIsOnScreenWithItsState: one that vanished is one that will
// surprise someone when the set is applied.
func TestEveryChangeIsOnScreenWithItsState(t *testing.T) {
	m := newScreen(
		change("2", "add 100 g of Rice", review.Ready),
		change("3", "use 10 of Cumin", review.Blocked),
		change("4", "move the jar", review.Confirmable),
	)

	got := screen(m)
	for _, want := range []string{"add 100 g of Rice", "use 10 of Cumin", "move the jar"} {
		if !strings.Contains(got, want) {
			t.Errorf("the screen is missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "1 ready") || !strings.Contains(got, "1 blocked") {
		t.Errorf("the counts do not add up to the set:\n%s", got)
	}
}

// TestCurrentIsTheChangeUnderTheCursor, which is what every keystroke acts on.
func TestCurrentIsTheChangeUnderTheCursor(t *testing.T) {
	m := newScreen(
		change("2", "acquire", review.Ready),
		change("3", "consume", review.Ready),
	)

	c, at, ok := m.Current()
	if !ok || at != 0 || c.What != "acquire" {
		t.Fatalf("Current = %q at %d (%v), want acquire at 0", c.What, at, ok)
	}

	m = press(m, "ctrl+n")
	if c, at, _ = m.Current(); at != 1 || c.What != "consume" {
		t.Errorf("after moving down, Current = %q at %d, want consume at 1", c.What, at)
	}
}

// TestTheCursorSurvivesAReplacement. The changes are rebuilt from whatever
// produced them after every settle, so a cursor that went back to the top
// each time would make working down a list impossible -- which is the only
// way anybody works down a list.
func TestTheCursorSurvivesAReplacement(t *testing.T) {
	m := newScreen(
		change("2", "acquire", review.Confirmable),
		change("3", "consume", review.Confirmable),
		change("4", "move", review.Confirmable),
	)
	m = press(m, "ctrl+n")
	m = press(m, "ctrl+n")

	m = m.WithChanges([]review.Change{
		change("2", "acquire", review.Ready),
		change("3", "consume", review.Ready),
		change("4", "move", review.Confirmable),
	})

	if _, at, _ := m.Current(); at != 2 {
		t.Errorf("the cursor moved to %d when the set was rebuilt, want 2", at)
	}
}

// TestTheCountsFollowTheStates, because they are derived from the changes
// rather than tracked beside them.
func TestTheCountsFollowTheStates(t *testing.T) {
	m := newScreen(
		change("2", "acquire", review.Ready),
		change("3", "consume", review.Dropped),
	)
	got := screen(m)
	if !strings.Contains(got, "1 ready") || !strings.Contains(got, "1 dropped") {
		t.Errorf("the counts do not follow the states:\n%s", got)
	}
}

// TestTheVerdictDecidesWhetherApplyIsOffered. The screen does not work out
// whether a set can be applied -- that is its producer's rule -- so what it
// must do is report faithfully what it was told.
func TestTheVerdictDecidesWhetherApplyIsOffered(t *testing.T) {
	m := newScreen(change("2", "acquire", review.Blocked))

	if m = m.WithVerdict("1 row is blocked"); m.Applicable() {
		t.Error("a screen with a refusal claims to be applicable")
	}
	if got := strings.Join(m.Facts(), " "); !strings.Contains(text.StripANSI(got), "unavailable") {
		t.Errorf("the facts line does not say the key is unavailable: %q", got)
	}

	if m = m.WithVerdict(""); !m.Applicable() {
		t.Error("a screen with no refusal claims to be inapplicable")
	}
	if got := text.StripANSI(strings.Join(m.Facts(), " ")); !strings.Contains(got, "one transaction") {
		t.Errorf("the facts line does not offer the apply: %q", got)
	}
}

// TestAnEmptySetStillDraws. A file with no rows is a thing that happens, and
// a screen that panicked on it would be worse than one that says so.
func TestAnEmptySetStillDraws(t *testing.T) {
	m := newScreen()
	if _, _, ok := m.Current(); ok {
		t.Error("an empty set claims a change under the cursor")
	}
	if screen(m) == "" {
		t.Error("an empty set drew nothing at all")
	}
}

// A long source does not push the heading past the terminal. A line wider
// than the screen wraps, and one wrapped line shifts every row below it.
func TestALongSourceDoesNotOverflow(t *testing.T) {
	source := "a-very-long-receipt-name-that-goes-on-and-on-and-on-and-on.csv"
	for _, width := range []int{40, 60, 80, 100} {
		m := review.New("IMPORT", source, nil).SetSize(width, 20)
		for _, line := range strings.Split(text.StripANSI(m.View()), "\n") {
			if n := len([]rune(line)); n > width {
				t.Errorf("a line is %d columns in a %d-column terminal: %q", n, width, line)
			}
		}
		// What survives is the END of the name, because that is the part that
		// distinguishes one from another.
		if !strings.Contains(text.StripANSI(m.View()), "on.csv") {
			t.Errorf("the heading lost the end of the name at width %d", width)
		}
	}
}

// TestTheStageIsNamedOnlyWhenThereIsASequence. "stage 1 of 1" describes the
// screen rather than the work.
func TestTheStageIsNamedOnlyWhenThereIsASequence(t *testing.T) {
	one := newScreen(change("2", "acquire", review.Ready)).
		WithStage(review.Stage{Name: "holdings", Number: 1, Of: 1})
	if got := text.StripANSI(one.View()); strings.Contains(got, "STAGE") {
		t.Errorf("a single-stage review announced a stage:\n%s", got)
	}
	if lead := one.Stage().Lead(); lead != "" {
		t.Errorf("a single-stage review leads with %q, want nothing", lead)
	}

	two := newScreen(change("2", "acquire", review.Ready)).
		WithStage(review.Stage{Name: "categories", Number: 1, Of: 2})
	if got := text.StripANSI(two.View()); !strings.Contains(got, "STAGE 1 OF 2") {
		t.Errorf("a staged review did not announce its stage:\n%s", got)
	}
}
