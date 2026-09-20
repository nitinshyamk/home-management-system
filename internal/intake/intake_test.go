package intake

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// The whole walk, with the answers a person would type.
func TestGuideRunsTheWholeWalk(t *testing.T) {
	root := t.TempDir()
	var out strings.Builder

	g := &Guide{
		Root:     root,
		Contract: fakeContract{},
		Planner: plannerFunc(func(w Workspace) error {
			// What a real planner does: read input/, write the plan file.
			if files, _ := w.Input(); len(files) == 0 {
				t.Error("the planner ran before there was any input")
			}
			return os.WriteFile(w.PlanFile(),
				[]byte(`{"op":"acquire","item":"Basmati Rice","qty":"2bag","at":"Left Pantry"}`+"\n"), 0o644)
		}),
		// name, then "the input is there"
		In:  strings.NewReader("kitchen receipt\n\n"),
		Out: &out,
	}

	// The input has to be there by the time the pause is answered, and the
	// pause is answered from a string. Put it there first: the test is about
	// the order of the STEPS, not about the timing of a person's hands.
	w, _ := Create(root, "kitchen-receipt")
	write(t, filepath.Join(w.InputDir(), "receipt.txt"), "2 bags basmati")

	plan, err := g.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	if plan != w.PlanFile() {
		t.Errorf("plan = %s, want %s", plan, w.PlanFile())
	}

	// The contract is rewritten on every run, against the house as it is now.
	for _, name := range []string{"import-schema.txt", HandoffName} {
		if _, err := os.Stat(filepath.Join(w.SchemaDir(), name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	// The handoff carries the paths, which is the half of the instruction the
	// schema cannot give.
	note, _ := os.ReadFile(filepath.Join(w.SchemaDir(), HandoffName))
	for _, want := range []string{w.InputDir(), w.PlanFile(), "SCHEMA BODY"} {
		if !strings.Contains(string(note), want) {
			t.Errorf("handoff does not mention %q", want)
		}
	}
	if !strings.Contains(out.String(), "1 row to review") {
		t.Errorf("the walk never said what it found:\n%s", out.String())
	}
}

// A name given on the command line skips the question and nothing else.
func TestGuideTakesAGivenName(t *testing.T) {
	root := t.TempDir()
	w, _ := Create(root, "groceries")
	write(t, filepath.Join(w.InputDir(), "list.txt"), "rice")
	write(t, w.PlanFile(), `{"op":"acquire","item":"Rice","qty":"1bag","at":"Left Pantry"}`+"\n")

	var out strings.Builder
	// The plan is already there, so the first question is whether to review it.
	g := &Guide{Root: root, Contract: fakeContract{}, In: strings.NewReader("y\n"), Out: &out}

	plan, err := g.Run(context.Background(), "groceries")
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	if plan != w.PlanFile() {
		t.Errorf("plan = %s, want %s", plan, w.PlanFile())
	}
	if !strings.Contains(out.String(), "resuming") {
		t.Errorf("a second run did not say it was resuming:\n%s", out.String())
	}
}

// Nothing more is coming: a pause cannot be answered from an exhausted reader,
// and spinning on one would be a hang rather than an answer.
func TestGuideStopsAtEndOfInput(t *testing.T) {
	g := &Guide{Root: t.TempDir(), Contract: fakeContract{}, In: strings.NewReader(""), Out: &strings.Builder{}}
	if _, err := g.Run(context.Background(), ""); !errors.Is(err, ErrStopped) {
		t.Errorf("Run = %v, want ErrStopped", err)
	}
}

// A planner that writes nothing leaves the workflow where it would have been
// without one. Ending the import there would throw the folder away over a step
// somebody can still do by hand.
func TestGuideFallsBackWhenThePlannerWritesNothing(t *testing.T) {
	root := t.TempDir()
	w, _ := Create(root, "groceries")
	write(t, filepath.Join(w.InputDir(), "list.txt"), "rice")

	var out strings.Builder
	g := &Guide{
		Root:     root,
		Contract: fakeContract{},
		Planner:  plannerFunc(func(Workspace) error { return nil }),
		Out:      &out,
	}
	// The plan lands WHILE the workflow is paused, which is the thing being
	// tested: the second prompt has to re-read the directory rather than
	// remember what was in it when it first looked.
	g.In = &answers{lines: []answer{
		{text: ""}, // the input is there
		{text: "", do: func() {
			write(t, w.PlanFile(), `{"op":"acquire","item":"Rice","qty":"1bag","at":"Left Pantry"}`+"\n")
		}},
	}}

	plan, err := g.Run(context.Background(), "groceries")
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	if plan != w.PlanFile() {
		t.Errorf("plan = %s", plan)
	}
	if !strings.Contains(out.String(), "it wrote no plan") {
		t.Errorf("the failure was not reported:\n%s", out.String())
	}
}

// A file that is not a plan fails here, naming the file, rather than as an
// empty review screen.
func TestGuideRefusesAPlanWithNoRows(t *testing.T) {
	root := t.TempDir()
	w, _ := Create(root, "groceries")
	write(t, filepath.Join(w.InputDir(), "list.txt"), "rice")
	write(t, w.PlanFile(), "not a command\n")

	g := &Guide{Root: root, Contract: fakeContract{}, In: strings.NewReader("y\n"), Out: &strings.Builder{}}
	if _, err := g.Run(context.Background(), "groceries"); err == nil {
		t.Fatal("a file of prose was accepted as a plan")
	}
}

// answers is a scripted person: each line can put a file in place before it is
// typed, which is how a test stands in for somebody dragging a receipt into a
// folder while the workflow waits.
type answer struct {
	text string
	do   func()
}

type answers struct {
	lines []answer
	cur   *strings.Reader
}

func (a *answers) Read(p []byte) (int, error) {
	for a.cur == nil || a.cur.Len() == 0 {
		if len(a.lines) == 0 {
			return 0, io.EOF
		}
		next := a.lines[0]
		a.lines = a.lines[1:]
		if next.do != nil {
			next.do()
		}
		a.cur = strings.NewReader(next.text + "\n")
	}
	return a.cur.Read(p)
}

type fakeContract struct{}

func (fakeContract) Export(dir, format string) (string, error) {
	name := "import-schema.txt"
	if format == "json" {
		name = "import-schema.json"
	}
	path := filepath.Join(dir, name)
	return path, os.WriteFile(path, []byte("SCHEMA BODY\n"), 0o644)
}

type plannerFunc func(Workspace) error

func (plannerFunc) Describe() string { return "a test planner" }

func (f plannerFunc) Plan(_ context.Context, w Workspace, _ string) error { return f(w) }

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
