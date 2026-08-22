package app

import (
	"context"

	"home-management-system/internal/command"
	"home-management-system/internal/ops"
)

// The write surface the interface is allowed to see.
//
// archlint asserts that internal/tui imports none of origin, ledger, or
// annotate. This is how it gets work done anyway: it hands over a Command and
// receives a Plan it can render, and the Plan's Batch is UNEXPORTED so there is
// no way for the interface to assemble a write of its own. The boundary is a
// property of the types rather than a convention about who calls what.

// Plan is a Command worked out, ready to be shown and then applied.
type Plan struct {
	// Summary is what will happen, one line per step, in the terms of the
	// receipt rather than the ledger.
	Summary []string

	// Confirm lists what will be created. Non-empty means the plan originates
	// something, and the interface cannot fail to notice because the emptiness
	// IS the signal.
	//
	// Everything named is confirmed, and the reason is duplication rather than
	// permanence: a receipt that quietly creates a second "Turmeric" is worse
	// than one that stops and asks.
	Confirm []string

	// Permanent is the subset that can never be changed afterwards, as facts.
	// A Category is confirmed and has none.
	Permanent []string

	// Irreversible is what the plan puts beyond recovery. It is a separate
	// question from Confirm: creating asks because a second Turmeric by
	// accident is worse than a question, while ENDING something asks because
	// there is no way back. Retiring is not a write path's idea of permanent --
	// it is a recording -- but Gone is the single lifecycle terminal and
	// nothing clears RetiredAt, so it is permanent in the only sense a person
	// cares about.
	Irreversible []string

	batch ops.Batch
}

// NeedsConfirmation reports whether anything permanent is about to happen.
func (p Plan) NeedsConfirmation() bool {
	return len(p.Confirm) > 0 || len(p.Irreversible) > 0
}

// Creates reports whether anything is being brought into existence, which is a
// different question from whether to ask.
func (p Plan) Creates() bool { return len(p.Confirm) > 0 }

// Empty reports a plan that would do nothing.
func (p Plan) Empty() bool { return len(p.batch.Steps) == 0 }

// PlanCommand works out what a Command implies without applying any of it.
func (c *controller) PlanCommand(ctx context.Context, cmd command.Command) (Plan, error) {
	batch, err := PlanFor(ctx, c.planner, cmd)
	if err != nil {
		return Plan{}, err
	}
	out := Plan{batch: batch}
	for _, step := range batch.Steps {
		out.Summary = append(out.Summary, step.Summary)
		if step.Ends != "" {
			out.Irreversible = append(out.Irreversible, step.Ends)
		}
		for _, origination := range step.Originates {
			if !origination.NeedsConfirmation() {
				continue
			}
			// Confirmed either way; the facts are what a panel lays out, and
			// some originations have none -- a Category is confirmed because
			// creating a second one by accident is the hazard, not because
			// anything about it is permanent.
			out.Confirm = append(out.Confirm, origination.Describe())
			if facts := origination.Permanent(); facts != "" {
				out.Permanent = append(out.Permanent, facts)
			}
		}
	}
	return out, nil
}

// Merge folds another plan into this one, so several commands commit together.
//
// One transaction across the whole selection, which is what "multi-select plus
// a command is a batch" has to mean: three rows either all move or none do.
// Applying them one at a time would leave a half-done batch on any refusal, and
// the half would be silent.
func (p Plan) Merge(other Plan) Plan {
	p.Summary = append(p.Summary, other.Summary...)
	p.Confirm = append(p.Confirm, other.Confirm...)
	p.Permanent = append(p.Permanent, other.Permanent...)
	p.Irreversible = append(p.Irreversible, other.Irreversible...)
	p.batch.Steps = append(p.batch.Steps, other.batch.Steps...)
	return p
}

// ApplyPlan commits it, as one unit of work.
func (c *controller) ApplyPlan(ctx context.Context, p Plan) error {
	if p.Empty() {
		return nil
	}
	_, err := c.executor.Execute(ctx, p.batch)
	return err
}

// BindLine turns a typed line into a Command, filling the subject from the
// selection when the line leaves it out.
//
// The work is in internal/command so that RawCommand never leaves it -- see
// command.BindLine. This is the trip to the database for the vocabulary, and
// nothing else.
func (c *controller) BindLine(ctx context.Context, line string, subject command.Subject) (command.BindResult, error) {
	vocabulary, err := command.LoadVocabulary(ctx, c.read)
	if err != nil {
		return command.BindResult{}, err
	}
	return command.BindLine(vocabulary, line, subject)
}

// Describe renders a Command for review, through the same index that resolved
// it -- a Command holds only identifiers, so the names have to come back from
// where they went.
func (c *controller) Describe(ctx context.Context, cmd command.Command) string {
	index, err := c.SearchIndex(ctx)
	if err != nil {
		return command.Summary(cmd, nil)
	}
	return command.Summary(cmd, index)
}

// Issues renders a BindResult's problems as sentences.
func Issues(result command.BindResult) []string {
	out := make([]string, 0, len(result.Issues))
	for _, issue := range result.Issues {
		out = append(out, issue.String())
	}
	return out
}
