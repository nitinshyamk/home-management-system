package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/tui/creator"
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

// runLine binds a typed line against every selected row and plans them as ONE
// unit of work.
//
// Multi-select plus a command IS a batch. Acting on the cursor row while three
// rows are selected is the worst available outcome: it does something, it does
// not do what was asked, and it says nothing about the difference.
//
// With nothing selected there is exactly one subject -- the row under the
// cursor -- so the two cases are the same code and cannot drift.
func (m Model) runLine(line string) tea.Cmd {
	subjects := m.subjects()
	return func() tea.Msg {
		var combined app.Plan
		var summaries []string

		for _, subject := range subjects {
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
			combined = combined.Merge(plan)
			summaries = append(summaries, m.ctrl.Describe(m.ctx, result.Command))
		}
		return planMsg{plan: combined, summary: summarise(summaries)}
	}
}

// summarise says what happened, and how many times when it was more than once.
//
// The count leads for a batch, because "3 rows" is the fact a person needs to
// check before approving and the fact they most need afterwards.
//
// Identical lines collapse. Three rows of one item read as "3 rows - use 10 of
// Ancho Chile", not as that sentence three times: a summary that repeats itself
// is one nobody finishes reading, and the end is where the differences would
// have been.
func summarise(summaries []string) string {
	switch len(summaries) {
	case 0:
		return "nothing to do"
	case 1:
		return summaries[0]
	}

	var distinct []string
	seen := map[string]bool{}
	for _, s := range summaries {
		if !seen[s] {
			seen[s] = true
			distinct = append(distinct, s)
		}
	}
	if len(distinct) > 3 {
		distinct = append(distinct[:3:3], fmt.Sprintf("and %d more", len(distinct)-3))
	}
	return fmt.Sprintf("%d rows - %s", len(summaries), strings.Join(distinct, "; "))
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

// subjects is everything a command should act on: the explicit selection, or
// the row under the cursor when nothing is picked.
//
// Selected() already falls back to the cursor row, but the fallback is not
// enough on its own -- the subject needs the row's cells, not just its key.
func (m Model) subjects() []command.Subject {
	if m.selectionCount() == 0 {
		if one := m.subject(); one.Name != "" {
			return []command.Subject{one}
		}
		return nil
	}

	picked := map[int64]bool{}
	if tabular(m.view) {
		for _, key := range m.table.Selected() {
			picked[key] = true
		}
		var out []command.Subject
		for _, row := range m.table.Rows() {
			if !picked[row.Key] {
				continue
			}
			subject := command.Subject{Kind: "Item", Name: row.Cells[0]}
			if m.view == viewHoldings && len(row.Cells) > 2 {
				subject.At = row.Cells[2]
			}
			out = append(out, subject)
		}
		return out
	}

	kind := "Category"
	if m.view == viewLocations {
		kind = "Location"
	}
	for _, key := range m.tree.Selected() {
		picked[key] = true
	}
	var out []command.Subject
	for _, node := range m.tree.Nodes() {
		if picked[node.ID] {
			out = append(out, command.Subject{Kind: kind, Name: node.Name})
		}
	}
	return out
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
		// A field opened for an ACTION answers to that action; only a rename
		// goes back through the command line, because only a rename is text.
		if m.editor.Purpose() != "rename" {
			return m.actOnPrompt()
		}
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

// ---------------------------------------------------------------------------
// Creation
// ---------------------------------------------------------------------------

// creatorKind is what a view creates. Holdings are absent on purpose: stock
// arrives by acquiring it, and a Holding is a placement rather than a name.
func creatorKind(v view) (creator.Kind, bool) {
	switch v {
	case viewItems:
		return creator.KindItem, true
	case viewCategories:
		return creator.KindCategory, true
	case viewLocations:
		return creator.KindLocation, true
	}
	return "", false
}

// openCreator starts a panel, defaulted to create inside what the cursor is on.
func (m Model) openCreator() Model {
	kind, ok := creatorKind(m.view)
	if !ok {
		m.problem = []string{"stock arrives by acquiring it -- try :acquire"}
		return m
	}
	parent := ""
	if node, ok := m.tree.Current(); ok && forest(m.view) {
		parent = node.Name
	}
	m.problem = nil
	m.creator = m.creator.Open(kind, parent).SetWidth(m.width)
	return m.suggest()
}

// suggest offers completions for whatever field the panel has focused.
//
// Through the resolve index, which is the same index behind the jump palette,
// the `:` line, and the bulk importer. One index, four consumers: if the panel
// completed names differently from the command line, the two would disagree
// about what a name nearly is -- and the panel would be teaching a vocabulary
// the rest of the application does not speak.
func (m Model) suggest() Model {
	kind, typed := m.creator.Resolving()
	if kind == "" || len(m.candidates) == 0 {
		return m.withSuggestions(nil)
	}

	var out []string
	for _, candidate := range m.candidates {
		if string(candidate.Kind) != kind || candidate.Archived {
			continue
		}
		// Already exactly a name: there is nothing to suggest, and a list that
		// says "tab to take it" when tab will move on is a list that lies.
		if strings.EqualFold(candidate.Path, typed) {
			return m.withSuggestions(nil)
		}
		if typed != "" && !fuzzyContains(strings.ToLower(candidate.Path), strings.ToLower(typed)) {
			continue
		}
		out = append(out, candidate.Path)
		if len(out) == 3 {
			break
		}
	}
	// Everything matches an empty field, and a list of everything is not a
	// suggestion -- it is the tree, which is one keystroke away already.
	if typed == "" {
		return m.withSuggestions(nil)
	}
	return m.withSuggestions(out)
}

func (m Model) withSuggestions(suggestions []string) Model {
	m.creator = m.creator.SetSuggestions(suggestions)
	return m
}

// handleCreator takes the keystroke while the panel is open.
//
// Before the omnibox and the list, for the same reason the editor does: while a
// field is open a keystroke is a character.
func (m Model) handleCreator(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !m.creator.IsOpen() {
		return m, nil, false
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.creator = m.creator.Close()
		m.status = "nothing was created"
		return m, nil, true
	case tea.KeyEnter:
		if name := m.creator.Value("name"); name == "" {
			m.problem = []string{"a name is required"}
			return m, nil, true
		}
		// Through the `:` line's own path -- the same Parse, the same Bind, the
		// same confirmation. A panel that took a shortcut would be a second way
		// to create things, validating differently from the first.
		return m, m.runLine(m.creator.Line()), true
	}
	if next, handled := m.creator.Update(msg); handled {
		m.creator = next
		// Recomputed after every keystroke, because a suggestion that lags the
		// input is a suggestion for something else.
		m = m.suggest()
		if len(m.candidates) == 0 {
			// The vocabulary has not been loaded yet, so load it once and the
			// next keystroke will have it.
			return m, m.loadCandidates(), true
		}
		return m, nil, true
	}
	return m, nil, true
}

// runCommands plans already-built Commands and merges them into one unit of
// work.
//
// The keystroke path's equivalent of runLine, and it joins the same pipeline
// one step later: runLine ends by producing Commands, and this begins with
// them. Everything after -- planning, the confirmation, the transaction, the
// feedback -- is shared, which is what makes "a keystroke and the equivalent
// line do the same thing" a property of the code rather than a promise.
func (m Model) runCommands(commands []command.Command) tea.Cmd {
	return func() tea.Msg {
		var combined app.Plan
		var summaries []string
		for _, cmd := range commands {
			plan, err := m.ctrl.PlanCommand(m.ctx, cmd)
			if err != nil {
				return issuesMsg{issues: []string{err.Error()}}
			}
			combined = combined.Merge(plan)
			summaries = append(summaries, m.ctrl.Describe(m.ctx, cmd))
		}
		return planMsg{plan: combined, summary: summarise(summaries)}
	}
}
