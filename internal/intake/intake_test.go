package intake

import (
	"os"
	"path/filepath"
	"testing"
)

// write puts a file there, making its directory if it is missing.
func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCleanName(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
		bad  bool
	}{
		{"plain", "groceries", "groceries", false},
		{"trimmed", "  groceries  ", "groceries", false},
		// A space is what a person types, and a workflow that refuses it has
		// spent its first interaction telling somebody off.
		{"spaces become dashes", "kitchen receipt march", "kitchen-receipt-march", false},
		{"runs collapse", "kitchen   receipt", "kitchen-receipt", false},
		{"already dashed", "kitchen-receipt", "kitchen-receipt", false},
		{"empty", "", "", true},
		{"only spaces", "   ", "", true},
		{"only dashes", "---", "", true},
		// Not a name with a problem: a different request entirely.
		{"a path", "../../etc", "", true},
		{"a separator", "a/b", "", true},
		{"a backslash", `a\b`, "", true},
		{"hidden", ".secret", "", true},
		{"control character", "a\x00b", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CleanName(tc.in)
			if tc.bad {
				if err == nil {
					t.Fatalf("CleanName(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("CleanName(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("CleanName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCreateMakesThreeSubdirectories(t *testing.T) {
	root := t.TempDir()
	w, err := Create(root, "groceries")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{w.SchemaDir(), w.InputDir(), w.PlanDir()} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Errorf("%s is not a directory: %v", dir, err)
		}
	}
	if w.PlanFile() != filepath.Join(root, "groceries", PlanDirName, PlanFileName) {
		t.Errorf("PlanFile = %s", w.PlanFile())
	}
}

// Naming an import twice resumes it. A workflow with a pause in the middle
// cannot be picked up the next day any other way.
func TestCreateIsIdempotent(t *testing.T) {
	root := t.TempDir()
	if Existed(root, "groceries") {
		t.Fatal("Existed said yes before anything was created")
	}
	if _, err := Create(root, "groceries"); err != nil {
		t.Fatal(err)
	}
	if !Existed(root, "groceries") {
		t.Fatal("Existed said no after Create")
	}
	if _, err := Create(root, "groceries"); err != nil {
		t.Fatalf("second Create: %v", err)
	}
}

// The canonical name wins over anything else in plan/, so regenerating a plan
// replaces what is reviewed instead of adding a second candidate.
func TestPlanPrefersTheCanonicalName(t *testing.T) {
	root := t.TempDir()
	w, err := Create(root, "groceries")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok, err := w.Plan(); err != nil || ok {
		t.Fatalf("Plan on an empty directory = %v, %v", ok, err)
	}

	write(t, filepath.Join(w.PlanDir(), "agent-output.jsonl"), "{}\n")
	path, ok, err := w.Plan()
	if err != nil || !ok {
		t.Fatalf("Plan = %v, %v", ok, err)
	}
	if filepath.Base(path) != "agent-output.jsonl" {
		t.Errorf("Plan = %s, want the only file there", path)
	}

	write(t, w.PlanFile(), "{}\n")
	path, _, _ = w.Plan()
	if path != w.PlanFile() {
		t.Errorf("Plan = %s, want %s", path, w.PlanFile())
	}
}

// An empty file is not a plan. An agent that failed halfway leaves one, and
// preferring it would mean reviewing nothing and calling it a plan.
func TestPlanIgnoresAnEmptyFile(t *testing.T) {
	root := t.TempDir()
	w, _ := Create(root, "groceries")
	write(t, w.PlanFile(), "")
	if _, ok, _ := w.Plan(); ok {
		t.Error("an empty plan file was offered as a plan")
	}
}

func TestInputListsOnlyVisibleFiles(t *testing.T) {
	root := t.TempDir()
	w, _ := Create(root, "groceries")
	write(t, filepath.Join(w.InputDir(), "receipt.txt"), "rice")
	write(t, filepath.Join(w.InputDir(), ".DS_Store"), "junk")
	if err := os.MkdirAll(filepath.Join(w.InputDir(), "photos"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := w.Input()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "receipt.txt" {
		t.Errorf("Input = %v, want [receipt.txt]", files)
	}
}

// What a folder needs, which is the one thing a list of them has to say.
func TestLookSaysWhatAFolderNeeds(t *testing.T) {
	root := t.TempDir()
	w, _ := Create(root, "groceries")

	if got := Look(w); got.State != NeedsInput {
		t.Errorf("an empty folder needs %v, want NeedsInput", got.State)
	}

	write(t, filepath.Join(w.InputDir(), "receipt.txt"), "rice")
	got := Look(w)
	if got.State != NeedsPlan {
		t.Errorf("a folder with input needs %v, want NeedsPlan", got.State)
	}
	if len(got.Input) != 1 {
		t.Errorf("Input = %v, want the one file", got.Input)
	}

	write(t, w.PlanFile(), `{"op":"acquire","item":"Rice","qty":"1kg","at":"Shelf"}`+"\n")
	if got := Look(w); got.State != Ready || got.Rows != 1 {
		t.Errorf("a folder with a plan is %v with %d rows, want Ready with 1", got.State, got.Rows)
	}
}

// A file that will not read is not a plan.
//
// This was the walk's final check, and it has to survive the walk: calling it
// ready would send somebody to a review screen with nothing on it, which
// reads as "the import was empty" rather than "the file is broken". An agent
// that failed halfway leaves exactly this.
func TestAPlanThatWillNotReadIsNotReady(t *testing.T) {
	root := t.TempDir()
	w, _ := Create(root, "groceries")
	write(t, filepath.Join(w.InputDir(), "receipt.txt"), "rice")

	for _, body := range []string{
		"not json at all\n",
		"# a comment an agent added\n",
	} {
		write(t, w.PlanFile(), body)
		if got := Look(w); got.State == Ready {
			t.Errorf("a plan file containing %q was called ready", body)
		}
	}
}

// Survey lists every folder, not only the ones with plans. Showing only those
// could not answer "what was I in the middle of".
func TestSurveyListsEveryFolder(t *testing.T) {
	root := t.TempDir()
	ready, _ := Create(root, "ready")
	write(t, ready.PlanFile(), `{"op":"acquire","item":"Rice","qty":"1kg","at":"Shelf"}`+"\n")
	if _, err := Create(root, "half-done"); err != nil {
		t.Fatal(err)
	}

	found, err := Survey(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("Survey found %d folders, want 2", len(found))
	}
	// By name, so the list does not reorder itself between one look and the
	// next.
	if found[0].Workspace.Name != "half-done" || found[0].State != NeedsInput {
		t.Errorf("first = %q/%v, want half-done/NeedsInput", found[0].Workspace.Name, found[0].State)
	}
	if found[1].Workspace.Name != "ready" || found[1].State != Ready {
		t.Errorf("second = %q/%v, want ready/Ready", found[1].Workspace.Name, found[1].State)
	}
}

// Start makes the folder, writes the contract into it, and says whether it
// was already there -- which is what "resuming" is told from.
func TestStartResumesByName(t *testing.T) {
	root := t.TempDir()

	w, instructions, resumed, err := Start(root, "groceries", fakeContract{})
	if err != nil {
		t.Fatal(err)
	}
	if resumed {
		t.Error("the first Start said it was resuming")
	}
	if instructions == "" {
		t.Error("Start returned no handoff to give a planner")
	}
	if _, err := os.Stat(w.HandoffFile()); err != nil {
		t.Errorf("the handoff was not written: %v", err)
	}

	if _, _, resumed, err = Start(root, "groceries", fakeContract{}); err != nil {
		t.Fatal(err)
	}
	if !resumed {
		t.Error("naming an import twice did not resume it")
	}
}

// fakeContract writes a schema without needing a house.
type fakeContract struct{}

func (fakeContract) Export(dir, format string) (string, error) {
	path := filepath.Join(dir, "schema."+format)
	if err := os.WriteFile(path, []byte("SCHEMA\n"), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
