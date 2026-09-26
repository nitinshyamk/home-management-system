package tui

import (
	"fmt"
	"strings"

	"home-management-system/internal/tui/organise"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/tui/undo"

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
		commands := make([]command.Command, 0, len(subjects))
		for _, subject := range subjects {
			result, err := m.ctrl.BindLine(m.ctx, line, subject)
			if err != nil {
				return issuesMsg{issues: []string{err.Error()}}
			}
			if !result.Ready() {
				return issuesMsg{issues: app.Issues(result)}
			}
			commands = append(commands, result.Command)
		}
		// Binding is where the two paths meet: a keystroke arrives holding
		// Commands already, and a line arrives here. Everything after -- the
		// diversion into a staged batch, the planning, the confirmation, the
		// transaction -- is one function, which is what makes "a keystroke
		// and the equivalent line do the same thing" a property of the code
		// rather than a promise.
		return m.written(commands)
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

// workingOn names the command in flight, so the screen is not silent between
// the keystroke and the answer.
//
// It quotes the line rather than saying "working...", because what a person
// wants to know while waiting is WHICH of the things they typed is still
// running -- and on a slow write that is the only evidence the keystroke
// landed at all.
func workingOn(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return "working ..."
	}
	return fmt.Sprintf("%s ...", line)
}

// apply commits a plan and reports what happened, with the way back when
// there is one.
func (m Model) apply(plan app.Plan, summary string, back undo.Step) tea.Cmd {
	return func() tea.Msg {
		if err := m.ctrl.ApplyPlan(m.ctx, plan); err != nil {
			return issuesMsg{issues: []string{err.Error()}}
		}
		return appliedMsg{summary: summary, back: back}
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
	return func() tea.Msg { return m.written(commands) }
}

// written is what happens to Commands once they exist, whichever path built
// them: staged if a batch is open, planned if not.
//
// ONE function, and the reason is organise mode. A verb that had to check for
// itself whether the mode was open would be a verb that could forget to, and
// the one that forgot would write in the middle of a batch nobody had
// applied -- which is the mode failing at its only promise, silently, in the
// one place a person is trusting it.
func (m Model) written(commands []command.Command) tea.Msg {
	if _, organising := m.organising(); organising {
		staged, rest := splitArranging(commands)
		switch {
		case len(staged) > 0 && len(rest) > 0:
			// Mixed, which only the command line can produce. Staging half of
			// it and writing the other half would be the worst of both, so
			// neither happens.
			return issuesMsg{issues: []string{"organising stages the arrangement; " +
				"this also changes what is on hand -- do them separately"}}
		case len(staged) > 0:
			return m.describedForStaging(staged)
		}
		// Everything else writes now, in organise mode as everywhere else.
		// See splitArranging.
	}

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
	summary := summarise(summaries)
	// The way back, worked out NOW, while the house still says what it said
	// when the command was built. Asked afterwards it would be asked of a
	// house that had already changed, which is the one moment it cannot
	// answer.
	back := undo.StepFor(summary, commands, m.before(commands))
	return planMsg{plan: combined, summary: summary, back: back}
}

// before is what was true of each command's subject, read off the rows the
// screen already has.
//
// From the CACHE rather than from a query, because these are the same rows
// the person is looking at -- and because a read here would be a read per
// keystroke for something the model was handed a moment ago.
func (m Model) before(commands []command.Command) []undo.Before {
	out := make([]undo.Before, 0, len(commands))
	for _, cmd := range commands {
		var was undo.Before
		switch c := cmd.(type) {
		case command.Move:
			if row, ok := m.holding(int64(c.Holding)); ok {
				was.Location = row.LocationID
			}
		case command.Rehome:
			if row, ok := m.holding(int64(c.Holding)); ok {
				was.Location = row.LocationID
			}
		case command.Reclassify:
			if item, ok := m.item(c.Item); ok {
				was.Category = item.CategoryID
			}
		case command.Rename:
			was.Name = m.subject().Name
		}
		out = append(out, was)
	}
	return out
}

// splitArranging separates what organise mode stages from what it does not.
//
// Quantity and custody are never staged. Arranging is free and reversible --
// the domain model guarantees it -- which is what makes holding a batch of it
// honest; consuming a jar is neither, so a batch that promised to defer it
// would be promising a safety the system cannot provide. See
// internal/tui/organise.Arranges.
func splitArranging(commands []command.Command) (staged, rest []command.Command) {
	for _, cmd := range commands {
		if organise.Arranges(cmd.Op()) {
			staged = append(staged, cmd)
		} else {
			rest = append(rest, cmd)
		}
	}
	return staged, rest
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
