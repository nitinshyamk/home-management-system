// Package organise stages edits to the arrangement, so a shape can be tried
// before it is owned.
//
// Reorganising is free. The domain model says so outright -- a category is a
// classification, not a container, and re-parenting one migrates nothing --
// but the interface did not act like it. Every structural edit was its own
// transaction the instant you pressed the key, so the only way to find out
// whether a shape was right was to build it and then take it apart again,
// leaving a ledger full of edits nobody meant to keep.
//
// So: a batch. Each edit is staged, the drawer shows what the lot of them
// would come to, and one keystroke applies them in a single transaction.
//
// # What is staged is COMMANDS
//
// Not a proposed tree. This is the one rule here, and archlint holds it,
// because the obvious implementation is the wrong one: copy the tree, let a
// person rearrange the copy, diff it at the end. That copy would be a second
// answer to "what shape is the house", living in the interface, going stale
// the moment anything else wrote -- and the diff would have to invent the
// edits back out of two shapes, guessing which rename was which.
//
// A list of Commands has none of those problems. It is already what will be
// applied, in the order it was meant, and it has no opinion about the tree at
// all -- so the rail underneath goes on being freshly loaded rows, which is
// exactly what it was before.
package organise

import (
	"fmt"

	"home-management-system/internal/command"
)

// Edit is one staged change to the arrangement.
type Edit struct {
	// Command is what will be applied. It holds identifiers, like every other
	// Command in the system: staging does not go back through names.
	Command command.Command
	// What it would do, in the words the house uses -- worked out when it was
	// staged, by the same describer every other write path uses.
	What string
	// Why it cannot, if the house already refused it when it was staged.
	//
	// A courtesy rather than a verdict. The batch is planned again as a whole
	// when it is applied, and that is the answer that counts: an edit staged
	// against a category that a later edit re-parents was never going to be
	// judged correctly one at a time.
	Why string
}

// Blocked reports the house having already refused this edit.
func (e Edit) Blocked() bool { return e.Why != "" }

// Model is the batch.
type Model struct {
	edits []Edit
}

// Stage adds edits, in the order they were made.
func (m Model) Stage(edits ...Edit) Model {
	m.edits = append(append([]Edit{}, m.edits...), edits...)
	return m
}

// TakeBack removes the last edit, reporting what it was so the interface can
// say what it undid.
//
// The LAST rather than a chosen one, because the cursor is in the house while
// a batch is being built -- that is the whole point of the mode -- so there is
// no cursor in here to choose with. Undoing in order is also how a person
// thinks about it: the edit you want back is almost always the one you just
// made.
func (m Model) TakeBack() (Model, Edit, bool) {
	if len(m.edits) == 0 {
		return m, Edit{}, false
	}
	last := m.edits[len(m.edits)-1]
	m.edits = append([]Edit{}, m.edits[:len(m.edits)-1]...)
	return m, last, true
}

// Edits is the batch, in order.
func (m Model) Edits() []Edit { return m.edits }

// Len is how many are staged.
func (m Model) Len() int { return len(m.edits) }

// Commands is what applying the batch would run, in the order it was made.
//
// The order matters and is not a detail: creating a shelf and then moving a
// crate into it works, and the reverse does not.
func (m Model) Commands() []command.Command {
	out := make([]command.Command, 0, len(m.edits))
	for _, e := range m.edits {
		out = append(out, e.Command)
	}
	return out
}

// WhyNot says what is stopping the batch from being applied, or nothing.
//
// The same shape as the importer's rule, and for the same reason: the batch
// is one transaction, so an edit the house has already refused stops all of
// it. The way out is the same too -- take it back -- and saying so is the
// difference between "you are stuck" and "undo the last one".
func (m Model) WhyNot() string {
	if len(m.edits) == 0 {
		return "nothing is staged yet"
	}
	var blocked int
	for _, e := range m.edits {
		if e.Blocked() {
			blocked++
		}
	}
	if blocked == 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d %s refused; take them back to apply the rest",
		blocked, len(m.edits), were(blocked))
}

func were(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}

// Arranges reports an operation being a change to the ARRANGEMENT: where
// something sits, or what it is called.
//
// This is what organise mode stages, and the boundary is not arbitrary.
// Arranging is free and reversible -- the domain model guarantees it -- which
// is what makes staging a batch of it honest. Consuming a jar is not free and
// not reversible, so batching it up to be applied later would be offering a
// safety the system cannot actually provide.
//
// A row's QUANTITY and its CUSTODY are the other two axes, and neither is
// here: acquiring, consuming, counting, checking out and retiring all happen
// the moment you press the key, in organise mode exactly as everywhere else.
func Arranges(op command.Op) bool {
	switch op {
	case command.OpNewCategory, command.OpNewLocation, command.OpNewItem,
		command.OpReparentCategory, command.OpReparentLocation,
		command.OpReclassify, command.OpMove,
		command.OpRename,
		command.OpArchiveCategory, command.OpArchiveLocation, command.OpArchiveItem,
		command.OpRestoreCategory, command.OpRestoreLocation:
		return true
	}
	return false
}
