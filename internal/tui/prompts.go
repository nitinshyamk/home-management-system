package tui

import (
	"fmt"
	"strings"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/tui/editor"
)

// promptSpec is everything that differs between one prompt and the next.
//
// These facts used to be spread across six switches in three files -- the
// label at the keystroke that opened the prompt, the verb inside the widget,
// the kind of name to complete against in promptResolves, and the Command to
// build in two more switches that had to agree with all of them. Adding a
// prompt meant finding all six; getting one wrong meant a prompt that offered
// completions for the wrong kind of thing, or said the wrong word on screen.
type promptSpec struct {
	// label is the question, shown beside the field.
	label string
	// verb is what enter will do, in the words of the thing being done.
	verb string
	// resolves is the kind of name this prompt completes against, or "" when
	// the answer is not a name. A quantity has nothing to complete against, and
	// offering it a list would be answering a question nobody asked.
	resolves string

	// build turns the answer into the Command it means for one Holding row of
	// the Holdings table. nil where the prompt does not apply to a row.
	build func(Model, app.HoldingRow, string) (command.Command, error)
	// forNode is the same for a tree row, which has one subject and says its
	// own kind. The kind decides, not the view: the Locations tree shows
	// Holdings and the Categories tree shows Items, so reading the view would
	// be reading it twice and getting the answer from the wrong one the day a
	// tree shows something else.
	forNode func(Model, string, int64, string) (command.Command, error)
	// appliesTo is the kind of tree row forNode accepts.
	appliesTo string
}

var prompts = map[editor.Purpose]promptSpec{
	editor.Rename: {label: "rename", verb: "save"},
	editor.Row:    {label: "command", verb: "re-check the row"},

	editor.Consume: {
		label: "how much", verb: "consume",
		build: func(m Model, row app.HoldingRow, answer string) (command.Command, error) {
			amount, err := m.amountFor(row, answer)
			if err != nil {
				return nil, err
			}
			return command.Consume{Item: row.ItemID, Location: row.LocationID, Amount: amount}, nil
		},
	},
	editor.Count: {
		label: "how much is actually there", verb: "count",
		build: func(m Model, row app.HoldingRow, answer string) (command.Command, error) {
			amount, err := m.amountFor(row, answer)
			if err != nil {
				return nil, err
			}
			return command.Count{Holding: row.ID, Observed: amount}, nil
		},
	},
	editor.Move: {
		label: "where to", verb: "move", resolves: "Location",
		build: func(m Model, row app.HoldingRow, answer string) (command.Command, error) {
			to, err := m.locationNamed(answer)
			if err != nil {
				return nil, err
			}
			return command.Move{Holding: row.ID, To: to}, nil
		},
		appliesTo: "Holding",
		forNode: func(m Model, _ string, subject int64, answer string) (command.Command, error) {
			to, err := m.locationNamed(answer)
			if err != nil {
				return nil, err
			}
			return command.Move{Holding: domain.HoldingID(subject), To: to}, nil
		},
	},
	editor.Reclassify: {
		label: "file it under", verb: "reclassify", resolves: "Category",
		appliesTo: "Item",
		forNode: func(m Model, _ string, subject int64, answer string) (command.Command, error) {
			to, err := m.categoryNamed(answer)
			if err != nil {
				return nil, err
			}
			return command.Reclassify{Item: domain.ItemID(subject), Category: to}, nil
		},
	},
}

// prompt describes an open field. An unknown purpose reads as a prompt that
// builds nothing, which is what the refusals below say.
func prompt(p editor.Purpose) promptSpec { return prompts[p] }

// build makes the Command a keystroke means for one row, from identifiers it
// already holds.
func (m Model) build(p editor.Purpose, row app.HoldingRow, answer string) (command.Command, error) {
	spec := prompt(p)
	if spec.build == nil {
		return nil, fmt.Errorf("no action called %q", p)
	}
	return spec.build(m, row, answer)
}

// buildForNode makes the Command a prompt over a tree row means.
func (m Model) buildForNode(p editor.Purpose, kind string, subject int64, answer string) (command.Command, error) {
	spec := prompt(p)
	if spec.forNode == nil || spec.appliesTo != kind {
		return nil, fmt.Errorf("nothing called %q applies to %s", p, strings.ToLower(kind))
	}
	return spec.forNode(m, kind, subject, answer)
}
