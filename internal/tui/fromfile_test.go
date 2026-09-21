package tui

import (
	"strings"
	"testing"

	"home-management-system/internal/command"
	"home-management-system/internal/importer"
	"home-management-system/internal/tui/review"
)

// The file half of the review: what a bound row becomes, and what settling
// one does to the rest.
//
// These tests were the plan screen's, and they are here because what they
// test is. Dropping a row, undropping it, replacing it and re-describing it
// all need a bound file and a vocabulary, and the screen has neither -- which
// is the whole of what internal/tui/review being producer-agnostic means.

// The namer is nil throughout: none of these rows bound to a Command, so
// what the screen shows is the text the row was written in, which is what a
// person corrects. Naming is exercised where it matters -- against a real
// vocabulary, in the golden frames.

func boundRow(line int, op string, state importer.State) importer.Entry {
	return importer.Entry{
		Row:   importer.Row{Line: line, Raw: command.RawCommand{Op: op}},
		State: state,
	}
}

func boundFile(entries ...importer.Entry) importer.Plan {
	return importer.Plan{Source: "/tmp/somewhere/deep/receipt.csv", Entries: entries}
}

// openFlow is a review of a bound file, with no stage sequence.
func openFlow(entries ...importer.Entry) flow {
	return newFlow().staged(boundFile(entries...), nil, review.Stage{}, nil)
}

func states(f flow) []review.State {
	var out []review.State
	for _, c := range f.screen.Changes() {
		out = append(out, c.State)
	}
	return out
}

// TestEveryBoundRowReachesTheScreen, in the order the file wrote them, with
// the line number that will let a person find it again.
func TestEveryBoundRowReachesTheScreen(t *testing.T) {
	f := openFlow(
		boundRow(2, "acquire", importer.Ready),
		boundRow(7, "consume", importer.Blocked),
	)
	got := f.screen.Changes()
	if len(got) != 2 {
		t.Fatalf("%d changes reached the screen, want 2", len(got))
	}
	if got[0].At != "2" || got[1].At != "7" {
		t.Errorf("the line numbers are %q and %q, want 2 and 7", got[0].At, got[1].At)
	}
	if got[0].State != review.Ready || got[1].State != review.Blocked {
		t.Errorf("the states did not survive the crossing: %v", states(f))
	}
}

// TestDroppingIsReversible right up until the apply, which is why it does not
// ask before doing it.
func TestDroppingIsReversible(t *testing.T) {
	f := openFlow(boundRow(2, "acquire", importer.Blocked))

	f = f.drop(0)
	if e, _, _ := f.current(); e.State != importer.Dropped {
		t.Fatalf("dropping left the row %v, want Dropped", e.State)
	}
	if got := f.screen.Changes()[0]; got.State != review.Dropped || got.Why != "dropped" {
		t.Errorf("the screen does not show the drop: %+v", got)
	}

	f = f.undrop(0)
	if e, _, _ := f.current(); e.State == importer.Dropped {
		t.Error("undropping did not pick the row back up")
	}
}

// TestSettleReplacesOneRowAndLeavesTheRest alone: settling row 1 must not
// disturb what row 0 already decided.
func TestSettleReplacesOneRowAndLeavesTheRest(t *testing.T) {
	f := openFlow(
		boundRow(2, "acquire", importer.Ready),
		boundRow(3, "consume", importer.Confirmable),
	)

	f = f.settle(1, boundRow(3, "settled", importer.Ready))

	got := f.bound.Entries
	if got[0].Row.Raw.Op != "acquire" || got[0].State != importer.Ready {
		t.Errorf("settling row 1 disturbed row 0: %+v", got[0])
	}
	if got[1].Row.Raw.Op != "settled" || got[1].State != importer.Ready {
		t.Errorf("row 1 = %q/%v, want settled/Ready", got[1].Row.Raw.Op, got[1].State)
	}
	ready, _, _, _ := f.screen.Counts()
	if ready != 2 {
		t.Errorf("the counts did not follow the settle: %d ready, want 2", ready)
	}
}

// TestSettlingOutOfRangeChangesNothing, because an index arriving from a
// keystroke on an empty screen is an ordinary thing to happen.
func TestSettlingOutOfRangeChangesNothing(t *testing.T) {
	f := openFlow(boundRow(2, "acquire", importer.Ready))
	for _, at := range []int{-1, 1, 99} {
		if got := f.settle(at, boundRow(9, "wrong", importer.Blocked)); len(got.bound.Entries) != 1 ||
			got.bound.Entries[0].Row.Raw.Op != "acquire" {
			t.Errorf("settling at %d disturbed the file: %+v", at, got.bound.Entries)
		}
	}
}

// TestTheVerdictFollowsTheFile: the screen is told whether the set can be
// applied, and the file is what decides.
func TestTheVerdictFollowsTheFile(t *testing.T) {
	f := openFlow(boundRow(2, "acquire", importer.Blocked))
	if f.screen.Applicable() {
		t.Error("a blocked file left the screen claiming to be applicable")
	}
	if f.screen.WhyNot() == "" {
		t.Error("a blocked file left the screen with no reason to give")
	}

	// Dropping the one blocked row settles it, which is the way out the
	// refusal names.
	f = f.drop(0)
	if f.screen.WhyNot() == "" {
		t.Error("a file with nothing left to apply claims it can be applied")
	}

	f = openFlow(boundRow(2, "acquire", importer.Ready))
	if !f.screen.Applicable() || f.screen.WhyNot() != "" {
		t.Errorf("a ready file is refused: %q", f.screen.WhyNot())
	}
}

// TestTheHeadingIsTheFilesName, not the path to it. You chose the file a
// moment ago; the directory it happens to sit in is not what you are
// reviewing, and an absolute path ran the heading past the terminal.
func TestTheHeadingIsTheFilesName(t *testing.T) {
	f := openFlow(boundRow(2, "acquire", importer.Ready))
	got := f.screen.SetSize(120, 20).View()
	if !strings.Contains(got, "receipt.csv") {
		t.Errorf("the heading lost the file's name:\n%s", got)
	}
	if strings.Contains(got, "/tmp/somewhere") {
		t.Errorf("the heading shows the path the file sits in:\n%s", got)
	}
}

// TestWaitingIsSaidOnlyWhereItComesToSomething.
//
// A row whose parent is made by another row of the same file is not waiting
// for a person at all -- but only on a stage that applies in PASSES. A stage
// that applies all at once can never make the thing and then bind the row
// that names it, so on one of those the row's only route really is the
// creation panel, and saying "waits for row 2" there would send somebody to
// wait for something that is never coming.
func TestWaitingIsSaidOnlyWhereItComesToSomething(t *testing.T) {
	for _, c := range []struct {
		what     string
		stage    review.Stage
		unwanted string
	}{
		{"all at once", review.Stage{}, "waits for"},
		{"in passes", review.Stage{InPasses: true}, ""},
	} {
		t.Run(c.what, func(t *testing.T) {
			f := newFlow().staged(
				boundFile(boundRow(2, "acquire", importer.Blocked)), nil, c.stage, nil)
			got := f.screen.Changes()[0].Why
			if c.unwanted != "" && strings.Contains(got, c.unwanted) {
				t.Errorf("a %s stage says %q", c.what, got)
			}
		})
	}
}
