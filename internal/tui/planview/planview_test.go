package planview_test

import (
	"strings"
	"testing"

	"home-management-system/internal/command"
	"home-management-system/internal/importer"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/planview"
	"home-management-system/internal/tui/text"
)

// names is a Namer that says what it was given, so a test can tell a row's
// description apart from its state and its line number.
type names struct{}

func (names) Describe(e importer.Entry) string { return "row " + e.Row.Raw.Op }

func entry(line int, op string, state importer.State) importer.Entry {
	return importer.Entry{
		Row:   importer.Row{Line: line, Raw: command.RawCommand{Op: op}},
		State: state,
	}
}

func plan(entries ...importer.Entry) importer.Plan {
	return importer.Plan{Source: "receipt.csv", Entries: entries}
}

// screen is the plan as the application draws it: the rows, and the facts the
// application renders under them.
func screen(m planview.Model) string {
	return text.StripANSI(m.View() + "\n" + strings.Join(m.Facts(), " - "))
}

func press(m planview.Model, name string) planview.Model {
	msg, ok := keys.Named(name)
	if !ok {
		panic("no key called " + name)
	}
	m, _ = m.Update(msg)
	return m
}

// TestEveryRowIsOnScreenWithItsState: a row that vanished from the plan is a
// row that will surprise someone when the file is applied.
func TestEveryRowIsOnScreenWithItsState(t *testing.T) {
	m := planview.New(plan(
		entry(2, "acquire", importer.Ready),
		entry(3, "consume", importer.Blocked),
		entry(4, "move", importer.Confirmable),
	), names{}).SetSize(120, 20)

	got := screen(m)
	for _, want := range []string{"row acquire", "row consume", "row move"} {
		if !strings.Contains(got, want) {
			t.Errorf("the screen is missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "1 ready") || !strings.Contains(got, "1 blocked") {
		t.Errorf("the counts do not add up to the file:\n%s", got)
	}
}

// TestCurrentIsTheRowUnderTheCursor, which is what every keystroke acts on.
func TestCurrentIsTheRowUnderTheCursor(t *testing.T) {
	m := planview.New(plan(
		entry(2, "acquire", importer.Ready),
		entry(3, "consume", importer.Ready),
	), names{}).SetSize(120, 20)

	e, at, ok := m.Current()
	if !ok || at != 0 || e.Row.Raw.Op != "acquire" {
		t.Fatalf("Current = %q at %d (%v), want acquire at 0", e.Row.Raw.Op, at, ok)
	}

	m = press(m, "ctrl+n")
	if e, at, _ = m.Current(); at != 1 || e.Row.Raw.Op != "consume" {
		t.Errorf("after moving down, Current = %q at %d, want consume at 1", e.Row.Raw.Op, at)
	}
}

// TestDroppingIsReversible right up until the apply, which is why it does not
// ask before doing it.
func TestDroppingIsReversible(t *testing.T) {
	m := planview.New(plan(entry(2, "acquire", importer.Blocked)), names{}).SetSize(120, 20)

	m = press(m, "d")
	if e, _, _ := m.Current(); e.State != importer.Dropped {
		t.Fatalf("d left the row %v, want Dropped", e.State)
	}
	if got := screen(m); !strings.Contains(got, "1 dropped") {
		t.Errorf("the count does not show the drop:\n%s", got)
	}

	m = press(m, "u")
	if e, _, _ := m.Current(); e.State == importer.Dropped {
		t.Error("u did not undrop the row")
	}
}

// TestSettleReplacesOneRowAndLeavesTheRest alone: settling row 1 must not
// disturb what row 0 already decided.
func TestSettleReplacesOneRowAndLeavesTheRest(t *testing.T) {
	m := planview.New(plan(
		entry(2, "acquire", importer.Ready),
		entry(3, "consume", importer.Confirmable),
	), names{}).SetSize(120, 20)

	m = m.Settle(1, entry(3, "settled", importer.Ready))

	got := m.Plan().Entries
	if got[0].Row.Raw.Op != "acquire" || got[0].State != importer.Ready {
		t.Errorf("settling row 1 disturbed row 0: %+v", got[0])
	}
	if got[1].Row.Raw.Op != "settled" || got[1].State != importer.Ready {
		t.Errorf("row 1 = %q/%v, want settled/Ready", got[1].Row.Raw.Op, got[1].State)
	}
	if s := screen(m); !strings.Contains(s, "2 ready") {
		t.Errorf("the counts did not follow the settle:\n%s", s)
	}
}

// TestWithNamesRedescribesTheRows: settling a row can create the thing the
// other rows name, and the screen has to be able to say what they now mean.
func TestWithNamesRedescribesTheRows(t *testing.T) {
	m := planview.New(plan(entry(2, "acquire", importer.Ready)), names{}).SetSize(120, 20)
	if !strings.Contains(screen(m), "row acquire") {
		t.Fatalf("the first namer was not used:\n%s", screen(m))
	}

	m = m.WithNames(shouty{})
	if got := screen(m); !strings.Contains(got, "ACQUIRE") {
		t.Errorf("WithNames did not redescribe the rows:\n%s", got)
	}
}

type shouty struct{}

func (shouty) Describe(e importer.Entry) string { return strings.ToUpper(e.Row.Raw.Op) }

// TestAnEmptyPlanStillDraws. A file with no rows is a thing that happens, and a
// screen that panicked on it would be worse than one that says so.
func TestAnEmptyPlanStillDraws(t *testing.T) {
	m := planview.New(plan(), names{}).SetSize(120, 20)
	if _, _, ok := m.Current(); ok {
		t.Error("an empty plan claims a row under the cursor")
	}
	if screen(m) == "" {
		t.Error("an empty plan drew nothing at all")
	}
}

// A long path does not push the heading past the terminal. What is shown is the
// file's name: the directory it happens to sit in is not what is being
// reviewed, and it was long enough to overflow an 80-column screen.
func TestALongSourcePathDoesNotOverflow(t *testing.T) {
	plan := importer.Plan{
		Source:  "/home/someone/a/very/deeply/nested/directory/that/goes/on/receipt.csv",
		Entries: []importer.Entry{},
	}
	for _, width := range []int{40, 60, 80, 100} {
		m := planview.New(plan, names{}).SetSize(width, 20)
		for _, line := range strings.Split(text.StripANSI(m.View()), "\n") {
			if n := len([]rune(line)); n > width {
				t.Errorf("a plan line is %d columns in a %d-column terminal: %q", n, width, line)
			}
		}
		// And what survives is the file's name.
		if !strings.Contains(text.StripANSI(m.View()), "receipt.csv") {
			t.Errorf("the heading lost the file's name at width %d", width)
		}
	}
}
