package intake

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"home-management-system/internal/importer"
)

// The steps of an import, as operations rather than as a conversation.
//
// They were the private methods of a Guide that printed questions to stdout
// and blocked on stdin. That shape made one assumption: that the thing
// driving the workflow had a terminal to itself and could stop dead waiting
// for a person. A screen cannot stop dead -- it has to stay drawn, and stay
// answerable, while the pause happens -- so the steps are functions now and
// the waiting belongs to whatever is doing the drawing.
//
// Nothing about the FOLDER changed. Three subdirectories with three authors,
// no default planner, and a degradation to "you write the plan file" are the
// right design and the right privacy posture. Only the entry and the exit
// moved.

// Contract is the schema hms hands out: the command vocabulary and this
// house.
//
// An interface, not command.Schema, so this package does not have to know how
// a contract is built -- only that one can be written into a directory.
type Contract interface {
	Export(dir, format string) (string, error)
}

// Planner turns an import's input into its plan file.
//
// Optional, and usually absent. Reading a photograph of a receipt is a job
// for an agent, and hms has no business assuming which one is installed; with
// no planner the workflow says what needs to happen and waits for the file,
// which is the same workflow with a person doing that step.
type Planner interface {
	// Describe is what the workflow says it is about to run.
	Describe() string
	// Plan is given the workspace and the instructions, and is expected to
	// write the plan file.
	Plan(ctx context.Context, w Workspace, instructions string) error
}

// State is how far along an import folder is, which is the one thing a list
// of them has to say.
type State int

const (
	// NeedsInput has nothing in input/ and no plan. Nothing can be done with
	// it until somebody puts something in.
	NeedsInput State = iota
	// NeedsPlan has input but nothing has turned it into rows yet. This is
	// the pause -- minutes or days -- that the workflow is shaped around.
	NeedsPlan
	// Ready has a plan file to review.
	Ready
)

func (s State) String() string {
	switch s {
	case NeedsInput:
		return "waiting for input"
	case NeedsPlan:
		return "waiting for a plan"
	}
	return "ready to review"
}

// Status is one import folder and what it needs.
type Status struct {
	Workspace Workspace
	State     State
	// Input is what a person has dropped in, by name.
	Input []string
	// Plan is the file to review, when State is Ready.
	Plan string
	// Rows is how many commands the plan holds, read cheaply so a list can
	// say "12 rows" rather than "a plan". Zero when there is no plan, and
	// zero too when the file is there but does not read -- which is reported
	// as NeedsPlan, because a file that will not parse is not a plan and
	// saying it is ready would be sending somebody to an empty screen.
	Rows int
}

// Survey is every import under root, and what each one needs.
//
// Every folder, not only the ones with plans. The list that showed only the
// ready ones could not answer the question people actually have -- "what was
// I in the middle of" -- and an import halfway through is exactly the thing
// most worth being reminded of.
func Survey(root string) ([]Status, error) {
	names, err := List(root)
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(names))
	for _, name := range names {
		out = append(out, Look(Workspace{Name: name, Dir: filepath.Join(root, name)}))
	}
	return out, nil
}

// Look is what one folder needs.
func Look(w Workspace) Status {
	s := Status{Workspace: w, State: NeedsInput}
	s.Input, _ = w.Input()
	if len(s.Input) > 0 {
		s.State = NeedsPlan
	}
	plan, ok, err := w.Plan()
	if err != nil || !ok {
		return s
	}
	// Read to COUNT, which also decides whether it is a plan at all. A file
	// an agent left half-written parses as nothing, and calling that ready
	// would open a review screen with no rows on it -- the failure the old
	// walk's final check existed to prevent, kept here.
	rows, err := importer.ReadFile(plan)
	if err != nil || len(rows) == 0 {
		return s
	}
	s.Plan, s.Rows, s.State = plan, len(rows), Ready
	return s
}

// WriteContract writes the schema and the handoff into the workspace, and
// returns the handoff -- which is what a planner is given.
//
// Both formats, every run. They are generated -- the same house produces the
// same bytes -- so rewriting them costs nothing, and the alternative is an
// import whose contract describes a house from last month.
func WriteContract(w Workspace, c Contract) (string, error) {
	prompt, err := c.Export(w.SchemaDir(), "prompt")
	if err != nil {
		return "", err
	}
	if _, err := c.Export(w.SchemaDir(), "json"); err != nil {
		return "", err
	}

	schema, err := os.ReadFile(prompt)
	if err != nil {
		return "", fmt.Errorf("intake: %w", err)
	}
	instructions := handoff(w, string(schema))
	path := filepath.Join(w.SchemaDir(), HandoffName)
	if err := os.WriteFile(path, []byte(instructions), 0o644); err != nil {
		return "", fmt.Errorf("intake: writing %s: %w", path, err)
	}
	return instructions, nil
}

// Start opens an import by name, writing the contract into it, and says
// whether it was already there.
//
// Naming one that exists is not an error and is in fact the point: an import
// is resumed by naming it again, which is the only way a workflow with a
// pause in the middle can be picked up the next day.
func Start(root, name string, c Contract) (w Workspace, instructions string, resumed bool, err error) {
	resumed = Existed(root, name)
	w, err = Create(root, name)
	if err != nil {
		return Workspace{}, "", false, err
	}
	instructions, err = WriteContract(w, c)
	if err != nil {
		return Workspace{}, "", false, err
	}
	return w, instructions, resumed, nil
}

// HandoffFile is the covering note to give to whatever writes the plan.
func (w Workspace) HandoffFile() string {
	return filepath.Join(w.SchemaDir(), HandoffName)
}
