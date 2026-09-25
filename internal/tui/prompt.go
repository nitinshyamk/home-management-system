package tui

import (
	"errors"
	"fmt"
	"strings"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/keys"

	tea "github.com/charmbracelet/bubbletea"
)

// The field layer: one question, asked in place, answered into a Command.

// openEditor starts renaming whatever the cursor is on.
func (m Model) openEditor() Model {
	sel, ok := m.current.Current()
	if !ok || sel.Name == "" {
		m.say = m.say.Report("nothing selected")
		return m
	}
	if sel.Contained {
		return m.refuseContained(sel.Kind, "rename")
	}
	m.editor = m.editor.Open(sel.Kind, sel.ID, prompt(editor.Rename).label, sel.Name).
		WithVerb(prompt(editor.Rename).verb).SetWidth(m.width)
	return m
}

// handleEditor takes the keystroke while a field is open.
//
// It runs before everything else for the same reason the omnibox does: while a
// field is open a keystroke is a character, and a `j` that moved the cursor
// while someone typed "jar" is how a modal interface betrays the person using
// it.
func (m Model) handleEditor(msg tea.KeyMsg) (Model, tea.Cmd) {
	// The field, INCLUDING the dropdown over it, goes first.
	//
	// It has to: esc puts a list away before it closes the field, which is the
	// same rule as everywhere else here -- one escape leaves exactly one mode.
	// Consulting the field second meant esc closed the whole prompt while a
	// list was showing, so the list could be opened and never dismissed.
	if next, handled := m.editor.Update(msg); handled {
		m.editor = next
		// Recomputed after every keystroke, because a suggestion that lags the
		// input is a suggestion for something else.
		m = m.suggestForPrompt()
		if len(m.candidates) == 0 {
			return m, m.loadCandidates()
		}
		return m, nil
	}
	switch keys.Lookup(keys.Line, msg) {
	case keys.Cancel:
		m.editor = m.editor.Close()
		// Whatever settle this field was opened for is over, cancelled as much
		// as confirmed. Leaving the row index behind meant "row 3 is being
		// settled" with nothing on screen agreeing.
		m = m.settled()
		m.say = m.say.Clear().Report("unchanged")
		return m, nil
	case keys.Confirm:
		// A field opened for an ACTION answers to that action; only a rename
		// goes back through the command line, because only a rename is text.
		if m.editor.Purpose() == editor.Row {
			return m.applyRowEdit()
		}
		// A figure given on a walk goes into the walk, not into the house. It
		// is the same question the count prompt asks and deliberately not the
		// same answer path: everything a walk decides is filed together, in
		// one transaction, at the end.
		if m.editor.Purpose() == editor.WalkCount {
			answer := strings.TrimSpace(m.editor.Value())
			m.editor = m.editor.Close()
			wk, ok := m.top().(*walking)
			if !ok || answer == "" {
				return m, nil
			}
			return m.countedOnWalk(wk, answer)
		}
		if m.editor.Purpose() != editor.Rename {
			return m.actOnPrompt()
		}
		value := strings.TrimSpace(m.editor.Value())
		changed, kind := m.editor.Changed(), m.editor.Kind()
		name := m.subject().Name
		m.editor = m.editor.Close()
		if !changed || value == "" {
			m.say = m.say.Report("unchanged")
			return m, nil
		}
		// Through the command line's own path: the same Parse, the same Bind,
		// the same plan. An inline edit that took a shortcut would be a second
		// way to write, and the two would drift.
		_ = kind
		return m, m.runLine(fmt.Sprintf("rename %q %q", name, value))
	}
	return m, nil
}

// promptFor opens the inline field an action needs.
//
// At the row, like the editor and the creation panel. Third time this answer
// has been given, and it is the same answer because it is the same reason: a
// prompt somewhere else costs you the row you were looking at.
func (m Model) promptFor(purpose editor.Purpose, initial string) Model {
	rows := m.selectedHoldings()
	if len(rows) == 0 {
		return m.refuse("nothing to %s here", purpose)
	}
	// Refused before the field opens rather than after it is filled in, because
	// answering a question that was never going to be used is worse than not
	// being asked.
	if why, ok := refuses(purpose, rows); !ok {
		return m.refuse("%s", why)
	}
	m.say = m.say.Clear()
	spec := prompt(purpose)
	m.editor = m.editor.
		OpenFor(purpose, "Holding", int64(rows[0].ID), spec.label, initial).
		WithVerb(spec.verb).SetWidth(m.width)
	return m
}

// refuses says why an action does not apply, in terms of the THING rather than
// the keystroke. "you cannot consume a cable" beats "invalid operation".
func refuses(purpose editor.Purpose, rows []app.HoldingRow) (string, bool) {
	for _, row := range rows {
		if row.Retired {
			return fmt.Sprintf("%q is done with, so there is nothing to %s", row.Item, purpose), false
		}
		measured := row.Custody == ""
		switch purpose {
		case editor.Consume, editor.Count:
			if !measured {
				return fmt.Sprintf("%q is one of a kind -- there is no amount to %s", row.Item, purpose), false
			}
		}
	}
	return "", true
}

// actOnPrompt turns a filled-in field into Commands, one per selected row.
func (m Model) actOnPrompt() (Model, tea.Cmd) {
	answer := strings.TrimSpace(m.editor.Value())
	purpose := m.editor.Purpose()
	rows := m.selectedHoldings()
	subject, kind := m.editor.Subject(), m.editor.Kind()
	// A prompt opened for ONE node has no selection behind it, and an empty
	// selection is exactly how the two paths differ: promptFor refuses to
	// open without rows, promptForNode never has any. Asking which pane is
	// showing stopped answering this once both prompts opened over the same
	// one.
	forNode := len(rows) == 0
	m.editor = m.editor.Close()

	if answer == "" {
		m.say = m.say.Report("nothing entered")
		return m, nil
	}

	if forNode {
		built, err := m.buildForNode(purpose, kind, subject, answer)
		if err != nil {
			return m.refuse("%v", err), nil
		}
		return m, m.runCommands([]command.Command{built})
	}

	var commands []command.Command
	for _, row := range rows {
		built, err := m.build(purpose, row, answer)
		if err != nil {
			return m.refuse("%v", err), nil
		}
		commands = append(commands, built)
	}
	return m, m.runCommands(commands)
}

// amountFor parses a written quantity and states it in the item's own unit.
func (m Model) amountFor(row app.HoldingRow, text string) (domain.Quantity, error) {
	written, err := command.ParseAmount(text)
	if err != nil {
		return domain.Zero, err
	}
	if written.Packages {
		return domain.Zero, fmt.Errorf("counting in packages needs the command line: try %s, then consume %s",
			keys.Show(keys.Browse, keys.CommandLine), text)
	}
	if written.Unit != "" {
		return domain.Zero, fmt.Errorf("a unit needs the command line, which knows how to convert it: try %s, then consume %s",
			keys.Show(keys.Browse, keys.CommandLine), text)
	}
	return written.Value, nil
}

// locationNamed resolves a destination the person typed.
func (m Model) locationNamed(name string) (domain.LocationID, error) {
	id, err := m.named(name, resolve.KindLocation)
	return domain.LocationID(id), err
}

// categoryNamed resolves a classification the person typed.
func (m Model) categoryNamed(name string) (domain.CategoryID, error) {
	id, err := m.named(name, resolve.KindCategory)
	return domain.CategoryID(id), err
}

// named resolves a destination the person typed.
//
// The DESTINATION is a name because a person typed it; the SUBJECT is an
// identifier because the cursor was already on it. That asymmetry is the whole
// design: resolution happens where trust changes, and nowhere else.
//
// Through resolve.Resolve rather than the completion matcher, because those two
// answer different questions: completion offers what you might mean and this
// decides what you did. A near miss here is reported as a near miss -- "did you
// mean" -- rather than silently taken, which is the resolver deciding, and the
// one thing it must never do.
func (m Model) named(name string, kind resolve.Kind) (int64, error) {
	if m.index == nil {
		return 0, errors.New("the vocabulary has not loaded yet")
	}
	outcome := m.index.Resolve(name, kind)
	if exact, ok := outcome.(resolve.Exact); ok {
		return exact.Candidate.ID, nil
	}
	// Worded by resolve, so a near miss on a keystroke reads exactly as the
	// same near miss typed on the command line. It used to read differently,
	// and worse: the basis that says WHY a suggestion was offered was dropped.
	return 0, errors.New(resolve.Describe(outcome))
}

// suggestForPrompt offers completions for the inline field a keystroke opened.
//
// Only the prompts that name something get them: a quantity has nothing to
// complete against, and offering it a list would be answering a question nobody
// asked.
func (m Model) suggestForPrompt() Model {
	if m.editor.Purpose() == editor.Row {
		return m.suggestForLine()
	}
	kind := prompt(m.editor.Purpose()).resolves
	if kind == "" {
		return m
	}
	m.editor = m.editor.SetSuggestions(m.completions(kind, strings.TrimSpace(m.editor.Value())))
	return m
}
