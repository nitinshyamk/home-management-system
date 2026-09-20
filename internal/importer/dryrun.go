package importer

import (
	"encoding/json"
	"io"
)

// The dry run: a bound plan as JSON, so an agent can iterate against it.
//
// It reports the same three states the plan screen shows, from the same Bind,
// so what an agent sees when it checks its work is what a person will see when
// they review it. Two renderings that could disagree would mean the agent was
// iterating against a different contract from the one that finally judges it.

// DryRunRow is one row's outcome, for a program to read.
type DryRunRow struct {
	Line int    `json:"line"`
	Op   string `json:"op"`
	// Stage is which half of the review this row lands in. An agent that mixes
	// a classification into a receipt should be able to see that it did: the
	// two are reviewed and applied separately, so a file that is half categories
	// is a file that asks for two decisions rather than one.
	Stage  string   `json:"stage"`
	State  string   `json:"state"`
	Does   string   `json:"does,omitempty"`
	Issues []string `json:"issues,omitempty"`
	// Creates names what the row would bring into existence. An agent that sees
	// this and did not mean it has guessed at something permanent.
	Creates []string `json:"creates,omitempty"`
	// WaitsFor is the line that makes what this row is missing, when another
	// row of the same file makes it. It replaces Creates rather than joining
	// it: a row filed under a category the row above it creates does NOT create
	// that category, and an agent reading that it did would fix the file by
	// deleting the row that does.
	WaitsFor int `json:"waits_for,omitempty"`
}

// DryRun is the whole file's outcome, with the three numbers that matter.
//
// Wrong is not among them and cannot be: a row that bound to the WRONG thing
// looks exactly like one that bound to the right thing. That number comes from
// a corpus a person checks, which is why 11c's gate is measured rather than
// asserted.
type DryRun struct {
	Source      string      `json:"source"`
	Rows        int         `json:"rows"`
	Ready       int         `json:"ready"`
	Confirmable int         `json:"needs_confirmation"`
	Blocked     int         `json:"blocked"`
	Entries     []DryRunRow `json:"entries"`
}

// Describer renders a bound command the way the plan screen does.
type Describer interface {
	Describe(entry Entry) string
}

// NewDryRun reports a bound plan.
func NewDryRun(plan Plan, names Describer) DryRun {
	ready, confirmable, blocked, _ := plan.Counts()
	out := DryRun{
		Source: plan.Source, Rows: len(plan.Entries),
		Ready: ready, Confirmable: confirmable, Blocked: blocked,
	}
	for at, entry := range plan.Entries {
		row := DryRunRow{
			Line: entry.Row.Line, Op: entry.Row.Raw.Op, State: entry.State.String(),
			Stage: StageOf(entry.Row).String(),
		}
		if entry.Command != nil && names != nil {
			row.Does = names.Describe(entry)
		}
		for _, issue := range entry.Issues {
			row.Issues = append(row.Issues, issue.String())
		}
		if other, waiting := plan.WillBeCreatedBy(at); waiting && !entry.IsCreation() {
			row.WaitsFor = plan.Entries[other].Row.Line
		} else {
			for _, creation := range entry.Creates {
				row.Creates = append(row.Creates, string(creation.Kind)+" "+creation.Name)
			}
		}
		out.Entries = append(out.Entries, row)
	}
	return out
}

// WriteJSON emits the dry run.
func (d DryRun) WriteJSON(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(d)
}
