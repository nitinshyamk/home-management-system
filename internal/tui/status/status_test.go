package status_test

import (
	"strings"
	"testing"

	"home-management-system/internal/tui/status"
)

func plain(lines []string) string { return strings.Join(lines, "\n") }

// A load replaces the view's hint and nothing else.
//
// This is the whole reason the package exists. One status string used to carry
// the hint, the outcome and the error at once, and every load overwrote it --
// so the feedback for a write was visible for exactly as long as it took the
// list to refresh, which is to say never. reloadKeepingStatus existed to smuggle
// the old string back over the fresh one, and is deleted by this.
func TestALoadDoesNotEraseWhatJustHappened(t *testing.T) {
	m := status.New().Report("used 100 g of Ancho Chile")
	m = m.SetHint("enter for history")

	if got := plain(m.Lines(80)); !strings.Contains(got, "used 100 g") {
		t.Errorf("the load erased the outcome: %q", got)
	}
	if m.Hint() != "enter for history" {
		t.Errorf("hint = %q", m.Hint())
	}
}

// And it does not put down what is in hand.
//
// The carry crosses views by design -- you pick a thing up where you can see it
// is wrong and go looking for where it belongs -- so the view switch in the
// middle is exactly the event that must not clear it. It used to, which is why
// the carry needed a banner of its own.
func TestALoadDoesNotDropWhatIsInHand(t *testing.T) {
	m := status.New().Carrying(`carrying "Ancho Chile"`).SetHint("6 locations")
	if got := plain(m.Lines(80)); !strings.Contains(got, "Ancho Chile") {
		t.Errorf("switching view dropped the carry: %q", got)
	}
	if got := plain(m.Dropped().Lines(80)); strings.Contains(got, "Ancho Chile") {
		t.Errorf("putting it down left it on screen: %q", got)
	}
}

// A success and a failure are never on screen together.
//
// They used to be kept apart by hand: a dozen call sites wrote `m.status = ""`
// or `m.problem = nil` next to the other one, and nothing checked. Missing one
// meant a screen reporting both at once.
func TestAnOutcomeAndARefusalAreMutuallyExclusive(t *testing.T) {
	refused := status.New().Report("used 100 g").Refuse("not enough on hand")
	if got := plain(refused.Lines(80)); strings.Contains(got, "used 100 g") {
		t.Errorf("the refusal left the outcome behind: %q", got)
	}
	if !refused.Refused() {
		t.Error("Refused() is false after refusing")
	}

	reported := status.New().Refuse("not enough on hand").Report("used 100 g")
	if got := plain(reported.Lines(80)); strings.Contains(got, "not enough") {
		t.Errorf("the outcome left the refusal behind: %q", got)
	}
	if reported.Refused() {
		t.Error("Refused() is true after reporting")
	}
}

// An answer replaces the "working" that was waiting for it, whichever answer
// it turns out to be.
func TestAnAnswerEndsTheWait(t *testing.T) {
	for name, m := range map[string]status.Model{
		"reported": status.New().Working("consume 100g ...").Report("used 100 g"),
		"refused":  status.New().Working("consume 100g ...").Refuse("not enough"),
		"cleared":  status.New().Working("consume 100g ...").Clear(),
	} {
		if got := plain(m.Lines(80)); strings.Contains(got, "...") {
			t.Errorf("%s: still says it is working: %q", name, got)
		}
	}
}

// Clearing is for abandoning a gesture, so it takes back the outcome and the
// refusal and leaves alone the two things that are not about this keystroke.
func TestClearingKeepsTheHintAndTheCarry(t *testing.T) {
	m := status.New().
		SetHint("enter for history").
		Carrying(`carrying "Ancho Chile"`).
		Report("used 100 g").
		Clear()

	if m.Hint() != "enter for history" {
		t.Errorf("clearing took the hint: %q", m.Hint())
	}
	got := plain(m.Lines(80))
	if !strings.Contains(got, "Ancho Chile") {
		t.Errorf("clearing put down what was in hand: %q", got)
	}
	if strings.Contains(got, "used 100 g") {
		t.Errorf("clearing kept the outcome: %q", got)
	}
}

// The block wraps to the terminal rather than running off it, and everything in
// it shares one left edge.
//
// Truncating a refusal is the worst thing to truncate: the part that says what
// to do about it is at the END.
func TestTheBlockWrapsAndLinesUp(t *testing.T) {
	m := status.New().
		Carrying(`carrying "Ancho Chile" -- C-y puts it in a place, esc puts it down`).
		Refuse("not enough on hand: only 100 g of \"Ancho Chile\" here, and 5000 g was asked for")

	lines := m.Lines(60)
	if len(lines) < 4 {
		t.Fatalf("nothing wrapped: %q", lines)
	}
	for _, line := range lines {
		if n := len([]rune(line)); n > 60 {
			t.Errorf("a status line is %d columns in a 60-column terminal: %q", n, line)
		}
		if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			t.Errorf("a status line does not sit at the block's left edge: %q", line)
		}
	}
	if m.Height(60) != len(lines) {
		t.Errorf("Height says %d, Lines gives %d", m.Height(60), len(lines))
	}
}

// Nothing to say costs nothing. The block is the one part of the chrome that is
// allowed to vary in height, so an idle interface gives its rows to the list.
func TestSilenceCostsNoLines(t *testing.T) {
	if got := status.New().SetHint("enter for history").Height(80); got != 0 {
		t.Errorf("an idle status block occupies %d lines", got)
	}
}
