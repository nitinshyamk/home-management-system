package intake

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// The planner: the one step of an import hms cannot do itself.
//
// Turning a photograph of a receipt into rows is a reading problem, and hms is
// an inventory rather than a model. So the step is a command somebody
// configures -- `import_planner` in .hms.json -- run with the paths in the
// environment and the handoff on stdin. hms ships no default: a program that
// silently shelled out to whatever agent happened to be installed would be
// sending a photograph of somebody's kitchen somewhere they never named.

// Environment given to the planner. Paths, so a command can be written as
// `agent "$HMS_HANDOFF_FILE"` without knowing how imports are laid out.
const (
	EnvImportDir  = "HMS_IMPORT_DIR"
	EnvSchemaDir  = "HMS_SCHEMA_DIR"
	EnvHandoff    = "HMS_HANDOFF_FILE"
	EnvInputDir   = "HMS_INPUT_DIR"
	EnvPlanFile   = "HMS_PLAN_FILE"
	EnvImportName = "HMS_IMPORT_NAME"
)

// Shell runs a configured shell command as the planner.
type Shell struct {
	// Command is the shell line to run, from the configuration.
	Command string
	// Out is where the planner's own output goes. It is shown rather than
	// swallowed: an agent that says why it could not read the receipt is the
	// most useful thing on the screen at that moment.
	Out io.Writer
}

// NewShell returns a planner for the configured command, or nil when none is
// configured. nil is the ordinary case and the Guide handles it, so a caller
// never has to ask whether the setting was empty.
func NewShell(command string, out io.Writer) Planner {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	return Shell{Command: command, Out: out}
}

// Describe names the command, so the workflow says what it is about to run
// before it runs it rather than after.
//
// Shortened, because a planner is often a shell line with a prompt in it and
// the point of the line on screen is "hms is about to run something you
// configured", not the configuration. The whole of it is in .hms.json, which is
// where somebody would go to change it anyway.
func (s Shell) Describe() string {
	const most = 60
	command := strings.Join(strings.Fields(s.Command), " ")
	if len(command) <= most {
		return command
	}
	return command[:most-3] + "..."
}

// Plan runs the command in the import directory.
func (s Shell) Plan(ctx context.Context, w Workspace, instructions string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", s.Command)
	cmd.Dir = w.Dir
	cmd.Stdin = strings.NewReader(instructions)
	cmd.Stdout = s.Out
	cmd.Stderr = s.Out
	cmd.Env = append(os.Environ(),
		EnvImportName+"="+w.Name,
		EnvImportDir+"="+w.Dir,
		EnvSchemaDir+"="+w.SchemaDir(),
		EnvHandoff+"="+w.SchemaDir()+"/"+HandoffName,
		EnvInputDir+"="+w.InputDir(),
		EnvPlanFile+"="+w.PlanFile(),
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("intake: planner %q: %w", s.Command, err)
	}
	return nil
}
