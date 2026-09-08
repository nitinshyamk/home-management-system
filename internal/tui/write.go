package tui

import (
	"fmt"
	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The write pipeline: text or Commands in, a plan out, applied on request.

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
	if len(subjects) == 0 {
		// A line that names everything it needs takes no subject, and there is
		// nothing under the cursor on an import screen anyway. One empty
		// subject rather than none: zero subjects would mean zero commands, and
		// the line would report "nothing to do" for a command that was
		// perfectly complete.
		subjects = []command.Subject{{}}
	}
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
			if combined, err = combined.Merge(plan); err != nil {
				return issuesMsg{issues: []string{err.Error()}}
			}
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
			if combined, err = combined.Merge(plan); err != nil {
				return issuesMsg{issues: []string{err.Error()}}
			}
			summaries = append(summaries, m.ctrl.Describe(m.ctx, cmd))
		}
		return planMsg{plan: combined, summary: summarise(summaries)}
	}
}

// asSubject is how a row on screen names itself to a command.
//
// By NAME rather than by identifier, so a line binds through exactly the same
// path a CSV row does. That is the difference between one contract and two that
// resemble each other.
func asSubject(s selection) command.Subject {
	return command.Subject{Kind: s.Subject, Name: s.Name, At: s.At}
}

// subjects is everything a command should act on: the explicit selection, or
// the row under the cursor when nothing is picked.
func (m Model) subjects() []command.Subject {
	if m.selectionCount() == 0 {
		if one := m.subject(); one.Name != "" {
			return []command.Subject{one}
		}
		return nil
	}
	var out []command.Subject
	for _, sel := range m.current.Selected() {
		out = append(out, asSubject(sel))
	}
	return out
}

// subject is what the cursor is on, so a contextual line can leave it out.
func (m Model) subject() command.Subject {
	sel, ok := m.current.Current()
	if !ok {
		return command.Subject{}
	}
	return asSubject(sel)
}
