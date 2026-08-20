package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
)

// The first place the interface writes anything.
//
// Everything before this was a way of looking. From here a keystroke can change
// the house, and the whole of the design is about making that hard to do by
// accident and obvious once done.
//
// It never touches a write path. app.Plan holds an unexported Batch, so the
// only thing this file can do is ask for a plan, show it, and hand it back --
// which is the boundary archlint asserts, made structural rather than agreed.

// pendingPlan is a plan waiting for a person to say yes.
//
// Only ever set when the plan originates something, because that is the one
// error this system cannot undo: an annotation is a typo you fix, a recording
// is reversible by a compensating event, and a wrong kind or content unit is
// remedied only by making a new Item and archiving the old one.
type pendingPlan struct {
	plan    app.Plan
	summary string
}

// planMsg carries a bound-and-planned command back from the controller.
type planMsg struct {
	plan    app.Plan
	summary string
}

// issuesMsg carries the reasons a line could not become a command.
type issuesMsg struct{ issues []string }

// appliedMsg reports that something happened, in the terms of the receipt.
type appliedMsg struct{ summary string }

// runLine binds a typed line and plans it, without applying anything.
func (m Model) runLine(line string) tea.Cmd {
	subject := m.subject()
	return func() tea.Msg {
		result, err := m.ctrl.BindLine(m.ctx, line, subject)
		if err != nil {
			return issuesMsg{issues: []string{err.Error()}}
		}
		if !result.Ready() {
			return issuesMsg{issues: app.Issues(result)}
		}
		plan, err := m.ctrl.PlanCommand(m.ctx, result.Command)
		if err != nil {
			return issuesMsg{issues: []string{err.Error()}}
		}
		return planMsg{plan: plan, summary: m.ctrl.Describe(m.ctx, result.Command)}
	}
}

// apply commits a plan and reports what happened.
func (m Model) apply(plan app.Plan, summary string) tea.Cmd {
	return func() tea.Msg {
		if err := m.ctrl.ApplyPlan(m.ctx, plan); err != nil {
			return issuesMsg{issues: []string{err.Error()}}
		}
		return appliedMsg{summary: summary}
	}
}

// subject is what the cursor is on, so a contextual line can leave it out.
//
// By NAME rather than by identifier, so the line binds through exactly the same
// path a CSV row does. That is the difference between one contract and two that
// resemble each other.
func (m Model) subject() command.Subject {
	switch {
	case tabular(m.view):
		row, ok := m.table.Current()
		if !ok {
			return command.Subject{}
		}
		// Both views name an Item in their first column, and the stock commands
		// name an Item -- which is what lets `:consume 100g` on a selected row
		// mean the same thing the `c` keystroke will.
		subject := command.Subject{Kind: "Item", Name: row.Cells[0]}
		if m.view == viewHoldings && len(row.Cells) > 2 {
			// And the place, because a holdings row is about an Item IN A
			// PLACE. Without it, a line typed while pointing at one of three
			// shelves asks which shelf you meant.
			subject.At = row.Cells[2]
		}
		return subject
	case forest(m.view):
		node, ok := m.tree.Current()
		if !ok {
			return command.Subject{}
		}
		kind := "Category"
		if m.view == viewLocations {
			kind = "Location"
		}
		return command.Subject{Kind: kind, Name: node.Name}
	}
	return command.Subject{}
}

// ---------------------------------------------------------------------------
// Inline editing
// ---------------------------------------------------------------------------

// openEditor starts renaming whatever the cursor is on.
func (m Model) openEditor() Model {
	subject := m.subject()
	if subject.Name == "" {
		m.status = "nothing selected"
		return m
	}
	var id int64
	if forest(m.view) {
		node, _ := m.tree.Current()
		id = node.ID
	} else {
		row, _ := m.table.Current()
		id = row.Key
	}
	m.editor = m.editor.Open(subject.Kind, id, "rename", subject.Name).SetWidth(m.width)
	return m
}

// handleEditor takes the keystroke while a field is open.
//
// It runs before everything else for the same reason the omnibox does: while a
// field is open a keystroke is a character, and a `j` that moved the cursor
// while someone typed "jar" is how a modal interface betrays the person using
// it.
func (m Model) handleEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !m.editor.IsOpen() {
		return m, nil, false
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.editor = m.editor.Close()
		m.status = "unchanged"
		return m, nil, true
	case tea.KeyEnter:
		value := strings.TrimSpace(m.editor.Value())
		changed, kind := m.editor.Changed(), m.editor.Kind()
		name := m.subject().Name
		m.editor = m.editor.Close()
		if !changed || value == "" {
			m.status = "unchanged"
			return m, nil, true
		}
		// Through the command line's own path: the same Parse, the same Bind,
		// the same plan. An inline edit that took a shortcut would be a second
		// way to write, and the two would drift.
		_ = kind
		return m, m.runLine(fmt.Sprintf("rename %q %q", name, value)), true
	}
	if next, handled := m.editor.Update(msg); handled {
		m.editor = next
		return m, nil, true
	}
	return m, nil, true
}

// ---------------------------------------------------------------------------
// Confirmation
// ---------------------------------------------------------------------------

// handleConfirm takes the keystroke while a permanent change is waiting.
func (m Model) handleConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if m.confirm == nil {
		return m, nil, false
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.confirm = nil
		m.status = "nothing was created"
		return m, nil, true
	case tea.KeyEnter:
		pending := *m.confirm
		m.confirm = nil
		return m, m.apply(pending.plan, pending.summary), true
	}
	// Every other key is ignored. A confirmation that could be dismissed by a
	// stray keystroke is not a confirmation.
	return m, nil, true
}

// confirmView is the panel that names what cannot be changed later.
//
// The facts alone are not the point: "kind = Bulk, unit = g" is a fact, and
// "changing these later replaces every holding" is the reason to care. Friction
// is proportional to permanence, and this is the only place in the application
// that has any.
func (m Model) confirmView() string {
	lines := []string{"  " + alertStyle.Render(m.confirm.summary+"?"), ""}
	for _, facts := range m.confirm.plan.Permanent {
		lines = append(lines, "  "+titleStyle.Render("permanent")+"   "+facts)
		lines = append(lines, "  "+strings.Repeat(" ", len("permanent"))+"   "+
			dimStyle.Render("changing these later replaces every holding"))
	}
	if len(m.confirm.plan.Permanent) == 0 {
		lines = append(lines, "  "+dimStyle.Render("nothing here is permanent, but a second one by accident is worse than a question"))
	}
	lines = append(lines, "", dimStyle.Render("  [enter] create    [esc] back"))
	return strings.Join(lines, "\n")
}
