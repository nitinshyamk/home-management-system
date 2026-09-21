package tui

import (
	"strings"

	"home-management-system/internal/command"
	"home-management-system/internal/tui/complete"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/text"

	tea "github.com/charmbracelet/bubbletea"
)

// The panel layer: the creation form, and the completions offered into it.

// openCreator starts a panel, defaulted to create inside what the cursor is on.
func (m Model) openCreator() Model {
	kind := m.creates()
	if kind == "" {
		return m.refuse("stock arrives by acquiring it -- try :acquire")
	}
	parent := ""
	// The synthetic root is the whole house rather than a node of it, so
	// creating there means creating at the top -- no parent, not a parent
	// called "the house".
	if sel, ok := m.current.Current(); ok && m.onRail() && !m.atRailRoot() {
		parent = sel.Name
	}
	m.say = m.say.Clear()
	m.creator = m.creator.Open(kind, parent).SetWidth(m.width)
	return m.suggest()
}

// handleCreator takes the keystroke while the panel is open.
//
// Before the omnibox and the list, for the same reason the editor does: while a
// field is open a keystroke is a character.
func (m Model) handleCreator(msg tea.KeyMsg) (Model, tea.Cmd) {
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
			return m, m.loadCandidates()
		}
		return m, nil
	}
	switch keys.Lookup(keys.Creator, msg) {
	case keys.Cancel:
		m.creator = m.creator.Close()
		// As with the field: a cancelled settle is a finished settle.
		m = m.settled()
		m.say = m.say.Report("nothing was created")
		return m, nil
	case keys.Confirm:
		if name := m.creator.Value("name"); name == "" {
			return m.refuse("a name is required"), nil
		}
		// Through the command line's own path -- the same Parse, the same Bind,
		// the same confirmation. A panel that took a shortcut would be a second
		// way to create things, validating differently from the first.
		return m, m.runLine(m.creator.Line())
	}
	return m, nil
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
