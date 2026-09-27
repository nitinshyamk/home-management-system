package testing_test

import (
	"os"
	"path/filepath"
	"testing"

	sim "home-management-system/internal/tui/testing"
)

// The import walk, in the house.
//
// It was a conversation on stdin that blocked on `press enter when it is
// there`. The pause it was waiting through is measured in hours and is spent
// in a file manager, so the reasoning went that a screen would be in the way.
// What that missed is that blocking is not the only way to wait: the drawer
// waits by staying drawn, and `r` looks again.

// imports is an HMS_HOME with an imports directory under it.
func imports(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HMS_HOME", home)
	return filepath.Join(home, "imports")
}

// TestStartingAnImportFromInsideTheHouse: the half that could not be reached
// from in here at all.
func TestStartingAnImportFromInsideTheHouse(t *testing.T) {
	root := imports(t)
	s := sim.New(t)
	kitchen(t, s)
	s.ByPlace()

	s.Send(sim.Press("i"))
	s.ShowsText("start a new import")

	s.Send(sim.Enter)
	s.ShowsText("what shall this import be called")
	s.Send(sim.Type("march receipt"))
	s.Send(sim.Enter)

	// Named, made, and the contract written into it -- against the house that
	// is still on screen behind the drawer, which is the whole point of the
	// walk being in here.
	s.ShowsText("march-receipt")
	s.ShowsText("waiting for input")
	for _, dir := range []string{"schema", "input", "plan"} {
		at := filepath.Join(root, "march-receipt", dir)
		if info, err := os.Stat(at); err != nil || !info.IsDir() {
			t.Errorf("%s was not made: %v", at, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "march-receipt", "schema", "handoff.md")); err != nil {
		t.Errorf("the handoff was not written: %v", err)
	}
}

// TestLookingAgainIsHowThePauseEnds.
//
// The old walk blocked here. This one stays drawn and answers `r`, so the
// program is usable for the hours that step takes -- and the house it is
// importing against is visible for all of them.
func TestLookingAgainIsHowThePauseEnds(t *testing.T) {
	root := imports(t)
	s := sim.New(t)
	kitchen(t, s)
	s.ByPlace()

	s.Send(sim.Press("i"))
	s.Send(sim.Enter) // start a new one
	s.Send(sim.Type("groceries"))
	s.Send(sim.Enter)
	s.ShowsText("waiting for input")

	// Somebody drops a receipt in with another program.
	if err := os.WriteFile(
		filepath.Join(root, "groceries", "input", "receipt.txt"), []byte("rice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Send(sim.Press("r"))
	s.ShowsText("1 file in")
	s.ShowsText("receipt.txt")

	// And then something writes the plan.
	if err := os.WriteFile(
		filepath.Join(root, "groceries", "plan", "plan.jsonl"),
		[]byte(`{"op":"acquire","item":"Basmati Rice","qty":"100g","at":"Shelf 1"}`+"\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	s.Send(sim.Press("r"))
	s.ShowsText("1 row to review")

	s.Send(sim.Enter)
	s.ShowsText("add 100 of Spices > Basmati Rice to Shelf 1")
}

// TestAnImportIsResumedByNamingItAgain, which is the only way a workflow with
// a pause in the middle can be picked up the next day.
func TestAnImportIsResumedByNamingItAgain(t *testing.T) {
	root := imports(t)
	if err := os.MkdirAll(filepath.Join(root, "groceries", "input"), 0o755); err != nil {
		t.Fatal(err)
	}

	s := sim.New(t)
	kitchen(t, s)
	s.ByPlace()

	s.Send(sim.Press("i"))
	s.ShowsText("groceries")
	s.Send(sim.Enter) // the folder, which sorts before "start a new import"
	s.ShowsText("resumed")
	s.ShowsText("waiting for input")
}

// TestWithNoPlannerTheWalkSaysWhatToDo.
//
// hms ships no default planner, and that is a privacy position rather than an
// omission: shelling out to whatever agent happened to be installed would
// send a photograph of somebody's kitchen somewhere they never named. So the
// screen names the two paths to hand on, and `p` says where to configure one
// rather than doing something surprising.
func TestWithNoPlannerTheWalkSaysWhatToDo(t *testing.T) {
	imports(t)
	s := sim.New(t)
	kitchen(t, s)
	s.ByPlace()

	s.Send(sim.Press("i"))
	s.Send(sim.Enter)
	s.Send(sim.Type("groceries"))
	s.Send(sim.Enter)

	// The paths to hand on are on screen, and they FIT: an absolute path to
	// an import runs past an 80-column terminal for an ordinary home
	// directory, and a line wider than the screen wraps -- which shifts every
	// row above it.
	s.ShowsText("put what you want imported into")
	s.ShowsText("hand this to whatever writes it")
	s.FitsWidth(sim.Width)
	// And the planner key is not offered, because there is nothing to run.
	s.HidesText("run the planner")

	s.Send(sim.Press("p"))
	s.ShowsText("no planner is configured")
}

// TestEscapeLeavesTheFolderBeforeTheDrawer.
//
// Leaving an import is a different intention from leaving the list you found
// it in, and one esc should not do both.
func TestEscapeLeavesTheFolderBeforeTheDrawer(t *testing.T) {
	imports(t)
	s := sim.New(t)
	kitchen(t, s)
	s.ByPlace()

	s.Send(sim.Press("i"))
	s.Send(sim.Enter)
	s.Send(sim.Type("groceries"))
	s.Send(sim.Enter)
	s.ShowsText("waiting for input")

	s.Send(sim.Esc)
	// Back to the list, which now has the folder in it.
	s.ShowsText("WHAT IT NEEDS")
	s.ShowsText("groceries")

	s.Send(sim.Esc)
	s.HidesText("WHAT IT NEEDS")
}
