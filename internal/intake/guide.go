package intake

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"home-management-system/internal/importer"
)

// The walk through an import.
//
// It is a plain terminal conversation rather than a screen, and deliberately.
// The middle of this workflow is a pause while somebody drags a photograph into
// a folder with another program -- a full-screen interface would be covering up
// the file manager they need. So hms prints where the folder is, waits, and
// only takes the terminal over once there is a plan to review, which is the
// point at which the interface has something to show.

// ErrStopped is a person ending the workflow at a prompt, as distinct from
// anything going wrong. It is reported without an error message, because "you
// stopped" is not news to the person who stopped.
var ErrStopped = errors.New("intake: stopped")

// Contract is the schema hms hands out: the command vocabulary and this house.
//
// An interface, not command.Schema, so this package does not have to know how a
// contract is built -- only that one can be written into a directory.
type Contract interface {
	Export(dir, format string) (string, error)
}

// Planner turns an import's input into its plan file.
//
// Optional, and usually absent. Reading a photograph of a receipt is a job for
// an agent, and hms has no business assuming which one is installed; with no
// planner the workflow says what needs to happen and waits for the file, which
// is the same workflow with a person doing that step.
type Planner interface {
	// Describe is what the workflow says it is about to run.
	Describe() string
	// Plan is given the workspace and the instructions, and is expected to
	// write the plan file.
	Plan(ctx context.Context, w Workspace, instructions string) error
}

// Guide is the workflow.
type Guide struct {
	// Root is the imports directory, which is config.ImportsDir().
	Root string
	// Contract is written into schema/ on every run, so a plan is always
	// written against the house as it is now rather than as it was when the
	// folder was made.
	Contract Contract
	// Planner is what generates the plan, or nil to wait for one.
	Planner Planner
	// PlannerHint is the configuration file to name when there is no planner,
	// so "hms could do this step for you" comes with the one path you would
	// edit to arrange it. Empty says nothing, which is what a test wants.
	PlannerHint string

	In  io.Reader
	Out io.Writer

	in *bufio.Reader
}

// Run walks the workflow and returns the plan file to review.
//
// name may be empty, in which case it is asked for. Everything else is either
// derived or asked at the point it is needed: an import is a folder, and the
// only thing hms needs from a person before it can make one is what to call it.
func (g *Guide) Run(ctx context.Context, name string) (string, error) {
	g.in = bufio.NewReader(g.In)

	if strings.TrimSpace(name) == "" {
		asked, err := g.askName()
		if err != nil {
			return "", err
		}
		name = asked
	}

	resuming := Existed(g.Root, name)
	w, err := Create(g.Root, name)
	if err != nil {
		return "", err
	}
	if resuming {
		g.printf("\nresuming %s\n", w.Dir)
	} else {
		g.printf("\ncreated %s\n", w.Dir)
	}

	instructions, err := g.writeContract(w)
	if err != nil {
		return "", err
	}
	g.layout(w)

	// A plan that is already here is the whole reason a name can be given
	// twice. Offered rather than opened, because "hms import groceries" the
	// morning after is as likely to mean "the receipt I forgot to add" as it is
	// "show me yesterday's plan".
	if plan, ok, err := w.Plan(); err != nil {
		return "", err
	} else if ok {
		keep, err := g.confirm(fmt.Sprintf("a plan is already here (%s). review it now?", filepath.Base(plan)), true)
		if err != nil {
			return "", err
		}
		if keep {
			return g.check(plan)
		}
	}

	if err := g.waitForInput(w); err != nil {
		return "", err
	}

	plan, err := g.generate(ctx, w, instructions)
	if err != nil {
		return "", err
	}
	return g.check(plan)
}

// askName asks what to call the import, showing what is already there.
func (g *Guide) askName() (string, error) {
	if existing, err := List(g.Root); err == nil && len(existing) > 0 {
		g.printf("\nimports already here: %s\n", strings.Join(existing, ", "))
	}
	for {
		line, err := g.prompt("\nname this import: ")
		if err != nil {
			return "", err
		}
		if quit(line) {
			return "", ErrStopped
		}
		clean, err := CleanName(line)
		if err != nil {
			g.printf("  %v\n", err)
			continue
		}
		return clean, nil
	}
}

// writeContract writes the schema and the handoff, and returns the handoff.
//
// Both formats, every run. They are generated -- the same house produces the
// same bytes -- so rewriting them costs nothing, and the alternative is an
// import whose contract describes a house from last month.
func (g *Guide) writeContract(w Workspace) (string, error) {
	prompt, err := g.Contract.Export(w.SchemaDir(), "prompt")
	if err != nil {
		return "", err
	}
	if _, err := g.Contract.Export(w.SchemaDir(), "json"); err != nil {
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

// layout prints the three subdirectories and what each is for, because the
// next step happens in a file manager rather than in here.
func (g *Guide) layout(w Workspace) {
	g.printf("\n  %s/%s\n", w.Name, SchemaDirName)
	g.printf("      the contract, written just now against this house\n")
	g.printf("  %s/%s\n", w.Name, InputDirName)
	g.printf("      put the receipt, the photograph, or the note here\n")
	g.printf("  %s/%s\n", w.Name, PlanDirName)
	g.printf("      the rows that come back, which is what hms reviews\n")
}

// waitForInput is the pause. It ends when there is something in input/.
//
// An empty input/ is asked about rather than refused: a plan written by hand
// into plan/ is a legitimate import with no input at all, and the person who
// did that should not have to make a decoy file to get past this.
func (g *Guide) waitForInput(w Workspace) error {
	for {
		g.printf("\nput what you want imported into:\n  %s\n", w.InputDir())
		line, err := g.prompt("press enter when it is there (q to stop): ")
		if err != nil {
			return err
		}
		if quit(line) {
			return ErrStopped
		}

		files, err := w.Input()
		if err != nil {
			return err
		}
		if len(files) > 0 {
			g.printf("\nfound %s: %s\n", plural(len(files), "file"), strings.Join(files, ", "))
			return nil
		}

		anyway, err := g.confirm("there is nothing in input/. carry on anyway?", false)
		if err != nil {
			return err
		}
		if anyway {
			return nil
		}
	}
}

// generate produces the plan: by running the planner, or by waiting for
// whatever a person is using instead.
func (g *Guide) generate(ctx context.Context, w Workspace, instructions string) (string, error) {
	if g.Planner != nil {
		g.printf("\nreading input/ with %s\n", g.Planner.Describe())
		if err := g.Planner.Plan(ctx, w, instructions); err != nil {
			g.printf("  %v\n", err)
		} else if plan, ok, err := w.Plan(); err != nil {
			return "", err
		} else if ok {
			g.printf("  wrote %s\n", plan)
			return plan, nil
		} else {
			g.printf("  it wrote no plan\n")
		}
		// Falls through to the wait. A planner that failed leaves the workflow
		// exactly where it would have been without one, which is recoverable;
		// ending the import here would throw away the folder and the contract
		// over a step somebody can still do by hand.
	}

	g.printf("\nhand these two to whatever writes the plan:\n")
	g.printf("  %s\n", filepath.Join(w.SchemaDir(), HandoffName))
	g.printf("  %s\n", w.InputDir())
	g.printf("and have it write:\n  %s\n", w.PlanFile())
	if g.Planner == nil && g.PlannerHint != "" {
		g.printf("\nto have hms run that step itself, set %q in %s\n", "import_planner", g.PlannerHint)
	}

	for {
		line, err := g.prompt("\npress enter when the plan is there (q to stop): ")
		if err != nil {
			return "", err
		}
		if quit(line) {
			return "", ErrStopped
		}
		plan, ok, err := w.Plan()
		if err != nil {
			return "", err
		}
		if ok {
			return plan, nil
		}
		g.printf("  nothing in %s yet\n", w.PlanDir())
	}
}

// check reads the plan before the screen does, so a file that is not a plan
// fails here -- naming the file and the reason -- rather than as an empty
// review screen.
func (g *Guide) check(path string) (string, error) {
	rows, err := importer.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("intake: %s has no rows", path)
	}
	g.printf("\n%s to review\n", plural(len(rows), "row"))
	return path, nil
}

// prompt writes a question and reads the answer.
func (g *Guide) prompt(question string) (string, error) {
	g.printf("%s", question)
	line, err := g.in.ReadString('\n')
	if err != nil && line == "" {
		if errors.Is(err, io.EOF) {
			// Nothing more is coming. A workflow with a pause in it cannot be
			// run from a pipe, and spinning on an exhausted reader would be a
			// hang rather than an answer.
			g.printf("\n")
			return "", ErrStopped
		}
		return "", fmt.Errorf("intake: reading input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// confirm asks a yes/no question with a default.
func (g *Guide) confirm(question string, yes bool) (bool, error) {
	suffix := " [y/N] "
	if yes {
		suffix = " [Y/n] "
	}
	line, err := g.prompt("\n" + question + suffix)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(line) {
	case "":
		return yes, nil
	case "y", "yes":
		return true, nil
	case "q", "quit":
		return false, ErrStopped
	default:
		return false, nil
	}
}

func (g *Guide) printf(format string, args ...any) {
	if g.Out == nil {
		return
	}
	fmt.Fprintf(g.Out, format, args...)
}

func quit(line string) bool {
	switch strings.ToLower(line) {
	case "q", "quit", "exit":
		return true
	}
	return false
}

// plural is "1 row" and "4 rows", because "1 rows" on the last line before a
// screen that decides what gets written reads as carelessness about the rest.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
