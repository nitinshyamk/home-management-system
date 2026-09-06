package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/tui/complete"
	"home-management-system/internal/tui/keys"

	"home-management-system/internal/tui/style"

	"home-management-system/internal/tui/text"
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

// asSubject is how a row on screen names itself to a command.
//
// By NAME rather than by identifier, so a line binds through exactly the same
// path a CSV row does. That is the difference between one contract and two that
// resemble each other.
func asSubject(s selection) command.Subject {
	return command.Subject{Kind: s.Kind, Name: s.Name, At: s.At}
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

// ---------------------------------------------------------------------------
// Inline editing
// ---------------------------------------------------------------------------

// openEditor starts renaming whatever the cursor is on.
func (m Model) openEditor() Model {
	sel, ok := m.current.Current()
	if !ok || sel.Name == "" {
		m.status = "nothing selected"
		return m
	}
	if sel.Contained {
		return m.refuseContained(sel.Kind, "rename")
	}
	m.editor = m.editor.Open(sel.Kind, sel.ID, "rename", sel.Name).SetWidth(m.width)
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
			return m, m.loadCandidates(), true
		}
		return m, nil, true
	}
	switch keys.Lookup(keys.Line, msg) {
	case keys.Cancel:
		m.editor = m.editor.Close()
		// "unchanged" is true and, while something is in hand, beside the
		// point: esc there is not abandoning the move, it is choosing the other
		// half of it. The banner says what is happening; a second message
		// alongside it would be a second thing to read.
		m.status = ""
		if m.copied == nil {
			m.status = "unchanged"
		}
		return m, nil, true
	case keys.Confirm:
		// A field opened for an ACTION answers to that action; only a rename
		// goes back through the command line, because only a rename is text.
		if m.editor.Purpose() == "row" {
			return m.applyRowEdit()
		}
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
	switch keys.Lookup(keys.Line, msg) {
	case keys.Cancel:
		m.confirm = nil
		m.status = "nothing was created"
		return m, nil, true
	case keys.Confirm:
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
	plan := m.confirm.plan
	lines := []string{"  " + style.Strong.Render(m.confirm.summary+"?"), ""}

	for _, facts := range plan.Permanent {
		lines = append(lines, "  "+style.Strong.Render("permanent")+"     "+facts)
		lines = append(lines, "  "+strings.Repeat(" ", len("permanent"))+"     "+
			style.Dim.Render("changing these later replaces every holding"))
	}
	for _, ends := range plan.Irreversible {
		lines = append(lines, "  "+style.Strong.Render("no way back")+"   "+ends)
		lines = append(lines, "  "+strings.Repeat(" ", len("no way back"))+"   "+
			style.Dim.Render("the history stays; the holding does not"))
	}
	if len(plan.Permanent) == 0 && len(plan.Irreversible) == 0 {
		lines = append(lines, "  "+style.Dim.Render(
			"nothing here is permanent, but a second one by accident is worse than a question"))
	}

	// The verb is the act's own. "[enter] create" on a retirement would be
	// describing the wrong thing at the worst moment.
	verb := "go ahead"
	if plan.Creates() {
		verb = "create"
	}
	lines = append(lines, "", style.Dim.Render(
		"  ["+keys.Show(keys.Line, keys.Confirm)+"] "+verb+
			"    ["+keys.Show(keys.Line, keys.Cancel)+"] back"))
	return strings.Join(lines, "\n")
}

// ---------------------------------------------------------------------------
// Creation
// ---------------------------------------------------------------------------

// openCreator starts a panel, defaulted to create inside what the cursor is on.
func (m Model) openCreator() Model {
	kind := spec(m.view).creates
	if kind == "" {
		return m.refuse("stock arrives by acquiring it -- try :acquire")
	}
	parent := ""
	if sel, ok := m.current.Current(); ok && forest(m.view) {
		// A contained row is not somewhere to create INSIDE. Offering its name
		// as the parent would file a new category under an item.
		if sel.Contained {
			return m.refuseContained(sel.Kind, "create inside")
		}
		parent = sel.Name
	}
	m.problem = nil
	m.creator = m.creator.Open(kind, parent).SetWidth(m.width)
	return m.suggest()
}

// suggest offers completions for whatever field the creation panel has focused.
// suggest refreshes the creation panel's dropdown and its warnings.
//
// Three sources, because a field wants one of three things: a closed
// vocabulary it was handed (units), an entity kind the resolver knows
// (categories, locations), or nothing at all.
func (m Model) suggest() Model {
	if choices, typed := m.creator.Choices(); len(choices) > 0 {
		m.creator = m.creator.SetSuggestions(complete.Options(asMatches(choices), typed))
		return m.warnInPanel()
	}
	kind, typed := m.creator.Resolving()
	m.creator = m.creator.SetSuggestions(m.completions(kind, typed))
	return m.warnInPanel()
}

// warnInPanel shows what already exists under a field that warns.
//
// Near rather than Options: a warning is read, not chosen, so only the tiers
// where the name itself matched are worth naming.
func (m Model) warnInPanel() Model {
	kind, typed := m.creator.Warning()
	m.creator = m.creator.SetWarnings(complete.Near(m.matchesOf(kind), typed))
	return m
}

// matchesOf is the live vocabulary of one kind.
func (m Model) matchesOf(kind string) []complete.Match {
	if kind == "" {
		return nil
	}
	var out []complete.Match
	for _, candidate := range m.candidates {
		if string(candidate.Kind) != kind || candidate.Archived {
			continue
		}
		out = append(out, complete.Match{Path: candidate.Path, Leaf: candidate.Leaf})
	}
	return out
}

// asMatches wraps a closed vocabulary for the matcher. A unit code has no
// hierarchy, so its leaf is itself.
func asMatches(options []string) []complete.Match {
	out := make([]complete.Match, 0, len(options))
	for _, option := range options {
		out = append(out, complete.Match{Path: option, Leaf: option})
	}
	return out
}

// completions ranks the vocabulary of one kind against what has been typed.
//
// One function for every field that resolves a name -- the creation panel's
// parent and category, and the move prompt's destination -- because a second
// completer would be a second opinion about what a name nearly is. It reads the
// resolve index, which is the same index behind the jump palette, the command
// line, and the bulk importer.
//
// The RANKING lives in the complete package, and it is the whole reason this is
// not a filter: the old version took the first three subsequence matches in
// index order, so `gar` could offer two things merely containing g-a-r ahead of
// the Garage.
func (m Model) completions(kind, typed string) []string {
	if len(m.candidates) == 0 {
		return nil
	}
	return complete.Options(m.matchesOf(kind), typed)
}

// suggestForPrompt offers completions for the inline field a keystroke opened.
//
// Only the prompts that name something get them: a quantity has nothing to
// complete against, and offering it a list would be answering a question nobody
// asked.
func (m Model) suggestForPrompt() Model {
	if m.editor.Purpose() == "row" {
		return m.suggestForLine()
	}
	kind, ok := promptResolves(m.editor.Purpose())
	if !ok {
		return m
	}
	m.editor = m.editor.SetSuggestions(m.completions(kind, strings.TrimSpace(m.editor.Value())))
	return m
}

// suggestForLine completes the token being typed at the end of a command line.
//
// Which field that token belongs to is answered by the PARSER (command.FieldAt)
// rather than guessed here, so the completer cannot offer Locations in a slot
// the parser will read as an Item.
func (m Model) suggestForLine() Model {
	value := m.editor.Value()
	// Only at the end of the line. A completion of "the last token" means
	// nothing when the cursor is in the middle of one, and taking it would
	// overwrite the tail the person moved back to keep.
	if !m.editor.AtEnd() {
		return m.clearLineSuggestions()
	}
	field, partial, ok := command.FieldAt(value)
	if !ok || field.Type != command.FieldName || len(field.Kinds) == 0 || partial == "" {
		return m.clearLineSuggestions()
	}
	var quoted []string
	for _, path := range m.completions(string(field.Kinds[0]), partial) {
		// Quoted, because that is literally what goes into the line: a path
		// holds spaces, and a suggestion shown bare would be a suggestion for
		// a line that means something else.
		quoted = append(quoted, command.Quote(path))
	}
	if len(quoted) == 0 {
		return m.clearLineSuggestions()
	}
	// Everything up to the token being typed is kept when one is taken.
	m.editor = m.editor.SetSuggestionsAfter(value[:len(value)-len(partial)], quoted)
	return m
}

func (m Model) clearLineSuggestions() Model {
	m.editor = m.editor.SetSuggestionsAfter("", nil)
	return m
}

// promptResolves says what kind of thing a prompt is asking for a name of.
func promptResolves(purpose string) (string, bool) {
	switch purpose {
	case "move":
		return "Location", true
	case "reclassify":
		return "Category", true
	}
	return "", false
}

// handleCreator takes the keystroke while the panel is open.
//
// Before the omnibox and the list, for the same reason the editor does: while a
// field is open a keystroke is a character.
func (m Model) handleCreator(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !m.creator.IsOpen() {
		return m, nil, false
	}
	// The panel, INCLUDING the dropdown over its focused field, goes first.
	//
	// It has to: esc puts a list away before it closes the panel, which is the
	// same rule as everywhere else here -- one escape leaves exactly one mode.
	// Consulting the panel second meant esc threw away a half-filled form while
	// a list was showing, so the list could be opened and never dismissed.
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
	switch keys.Lookup(keys.Creator, msg) {
	case keys.Cancel:
		m.creator = m.creator.Close()
		m.status = "nothing was created"
		return m, nil, true
	case keys.Confirm:
		if name := m.creator.Value("name"); name == "" {
			return m.refuse("a name is required"), nil, true
		}
		// Through the command line's own path -- the same Parse, the same Bind,
		// the same confirmation. A panel that took a shortcut would be a second
		// way to create things, validating differently from the first.
		return m, m.runLine(m.creator.Line()), true
	}
	return m, nil, true
}

// refuseContained says why a row that is only being SHOWN here cannot be acted
// on, in terms of the thing rather than the keystroke.
//
// The trees show what their nodes contain so that you can see where things are.
// Acting on them from here would make the Locations tree a second holdings
// screen -- with its own idea of selection, its own verbs, and its own bugs.
func (m Model) refuseContained(kind, verb string) Model {
	where := "the Items view"
	if kind == "Holding" {
		where = "the Holdings view"
	}
	return m.refuse("that is %s the tree is showing you -- %s it from %s",
		text.Article(strings.ToLower(kind)), verb, where)
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
