package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/style"
	"home-management-system/internal/tui/undo"
)

// The way back from the last thing written, where there is one.
//
// `z` applies the INVERSE command -- a new entry that puts things back --
// because the ledger is append-only and that is load-bearing. The history
// keeps both, which is honest: you did move the crate to the garage, and
// then you moved it back.
//
// It is offered only when it exists, and that is most of what makes it worth
// having. See internal/tui/undo for what has an inverse and what does not,
// and why consuming something is on the wrong side of that line.

// undoLast puts back whatever the last write did.
func (m Model) undoLast() (Model, tea.Cmd) {
	if !m.back.Possible() {
		if m.back.Did != "" {
			return m.refuse("there is no way back from %q", m.back.Did), nil
		}
		return m.refuse("nothing to undo"), nil
	}
	step := m.back
	m.back = undo.Step{}
	// And the write this starts leaves no way back of its own. See
	// Model.undoing: the inverse of a move is a move, so without this `z`
	// toggles.
	m.undoing = true
	m.say = m.say.Working("putting back: " + step.Did + " ...")
	return m, m.runCommands(step.Back)
}

// undoOffer is what the permanent line says about going back, or nothing.
//
// It names the key only when pressing it would do something, which is the
// whole discipline: an affordance on screen permanently is worth the room
// only if it can be believed, and the operation people most want undone is
// the one that cannot be.
func (m Model) undoOffer() string {
	if !m.back.Possible() {
		return ""
	}
	return style.Dim.Render(keys.Show(keys.Browse, keys.Undo) + " undo that")
}
