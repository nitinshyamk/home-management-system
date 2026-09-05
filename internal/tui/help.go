package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"home-management-system/internal/command"
	"home-management-system/internal/tui/complete"
	"home-management-system/internal/tui/keys"
)

// Help, generated rather than written.
//
// Every line below comes from a declaration somewhere else -- the keymap in
// internal/tui/keys, the command vocabulary in internal/command/spec.go. Help
// text written by hand is help text that is wrong within a release: it is the
// one kind of documentation nobody re-reads, because the people who would spot
// the drift are the people who already know the answer.
//
// The same argument is why `hms schema` emits the command spec rather than a
// description of it, and this is that idea pointed at a person instead of an
// agent.

// helpSection is a heading and its rows, each a key or name against what it
// does.
type helpSection struct {
	title string
	rows  [][2]string
	// prose is free text under the heading, wrapped to the terminal. The
	// two-column layout is for keys against meanings; a whole command written
	// out is one long thing and forcing it into a column truncates it.
	prose []string
}

// helpLines renders the help for a topic. An empty topic is the general help.
func (m Model) helpLines(topic string) []string {
	width := max(40, m.width-6)
	if topic == "" {
		return renderHelp(m.generalHelp(), width)
	}
	sections, ok := m.commandHelp(topic)
	if !ok {
		return renderHelp(m.noSuchCommand(topic), width)
	}
	return renderHelp(sections, width)
}

// noSuchCommand says so, and offers the nearest names.
//
// Through the same ranked matcher the completion dropdowns use, so "did you
// mean" here and "did you mean" on the import plan screen are one idea rather
// than two that resemble each other.
func (m Model) noSuchCommand(topic string) []helpSection {
	sections := []helpSection{{
		title: "NOT A COMMAND",
		prose: []string{"There is nothing called " + strconv.Quote(topic) + "."},
	}}
	var vocabulary []complete.Match
	for _, name := range helpTopics() {
		vocabulary = append(vocabulary, complete.Match{Path: name, Leaf: name})
	}
	if near := complete.Near(vocabulary, topic); len(near) > 0 {
		var rows [][2]string
		for _, name := range near {
			spec, _ := command.SpecOf(command.Op(name))
			rows = append(rows, [2]string{name, spec.What})
		}
		sections = append(sections, helpSection{title: "DID YOU MEAN", rows: rows})
	}
	return append(sections, helpSection{
		title: "EVERYTHING THERE IS",
		prose: []string{keys.Show(keys.Browse, keys.CommandLine) + " help"},
	})
}

// generalHelp is the whole interface: what every key does, and what every
// command is for.
//
// The KEYS come from the keymap; the descriptions are written here, because
// the keymap's labels are footer-sized ("move", "column") and a listing wants a
// sentence. Pairs share a row -- C-n and C-p are one thing a person learns, not
// two, and a page of rows reading "move / move" teaches nobody anything.
func (m Model) generalHelp() []helpSection {
	sections := []helpSection{
		{title: "MOVING", rows: [][2]string{
			pair(keys.Table, "down and up a row", keys.MoveDown, keys.MoveUp),
			pair(keys.Table, "between columns, or in and out of a tree", keys.MoveRight, keys.MoveLeft),
			pair(keys.Table, "half a screen", keys.PageDown, keys.PageUp),
			pair(keys.Table, "the first row, and the last", keys.Top, keys.Bottom),
			pair(keys.Table, "put the cursor back in the middle", keys.Recenter),
		}},
		{title: "CHOOSING ROWS", rows: [][2]string{
			pair(keys.Table, "pick this row, and move on", keys.ToggleSelect),
			pair(keys.Table, "pick everything a filter left", keys.SelectVisible),
			pair(keys.Table, "sort by the focused column", keys.Sort),
			pair(keys.Browse, "clear the selection, then the filter", keys.Cancel),
		}},
		{title: "FINDING THINGS", rows: [][2]string{
			pair(keys.Browse, "search this view -- comes back with what is applied", keys.Search),
			pair(keys.Table, "step through what the search left", keys.NextMatch, keys.PrevMatch),
			pair(keys.Browse, "jump to anything, of any kind", keys.Jump),
			pair(keys.Browse, "the command line", keys.CommandLine),
		}},
		{title: "VIEWS", rows: [][2]string{
			{"1 - 5", "Categories, Locations, Items, Holdings, Integrity"},
			pair(keys.Browse, "open a holding's history", keys.Confirm),
			pair(keys.Browse, "read it again from the database", keys.Refresh),
			pair(keys.Browse, "quit", keys.Quit),
		}},
		{title: "IN A TREE", rows: [][2]string{
			pair(keys.Tree, "fold the node under the cursor", keys.FoldToggle),
			pair(keys.Tree, "fold everything, or unfold it", keys.FoldCycleAll),
			pair(keys.Browse, "show what the nodes contain -- items, or holdings", keys.ShowContents),
		}},
		{title: "ACTING ON A ROW", rows: [][2]string{
			pair(keys.Browse, "use some of it", keys.Consume),
			pair(keys.Browse, "say how much is actually there", keys.Count),
			pair(keys.Browse, "move it somewhere", keys.MoveTo),
			pair(keys.Browse, "take it out, or bring it back", keys.ToggleCustody),
			pair(keys.Browse, "rename it, in place", keys.EditInPlace),
			pair(keys.Browse, "make a new one inside this", keys.Create),
			pair(keys.Browse, "copy a row, then put it where the cursor is", keys.Copy, keys.Paste),
			pair(keys.Browse, "retire it -- it asks first", keys.Kill),
		}},
		{title: "TYPING IN A FIELD", rows: [][2]string{
			pair(keys.Line, "the start of the line, and the end", keys.LineStart, keys.LineEnd),
			pair(keys.Line, "a word back, and a word forward", keys.WordLeft, keys.WordRight),
			pair(keys.Line, "delete the character under the cursor", keys.DeleteForward),
			pair(keys.Line, "kill to the start, and to the end", keys.KillToStart, keys.KillToEnd),
			pair(keys.Line, "kill the word behind, and the word ahead", keys.KillWordBack, keys.KillWordForward),
		}},
		{title: "WHEN A LIST IS OPEN OVER A FIELD", rows: [][2]string{
			pair(keys.Line, "take what is highlighted", keys.Complete),
			pair(keys.Line, "choose", keys.MoveDown, keys.MoveUp),
			pair(keys.Line, "put the list away and keep typing", keys.Dismiss, keys.Cancel),
		}},
		{title: "ON THE IMPORT PLAN", rows: [][2]string{
			pair(keys.Plan, "settle the row under the cursor", keys.Confirm),
			pair(keys.Plan, "drop a row, or take it back", keys.Drop, keys.Undrop),
			pair(keys.Plan, "apply the whole file, in one transaction", keys.ApplyAll),
		}},
	}

	// help leads, because it is the one line on this list that is not a
	// Command -- it acts on the interface rather than on the house, so nothing
	// in the command vocabulary knows about it.
	commands := [][2]string{{"help", "this, and `help <name>` for one command"}}
	for _, spec := range command.Specs() {
		commands = append(commands, [2]string{string(spec.Op), spec.What})
	}
	sections = append(sections, helpSection{
		title: "COMMANDS  (" + keys.Show(keys.Browse, keys.CommandLine) + " help <name> for one of them)",
		rows:  commands,
	})
	return sections
}

// commandHelp is one command's shape, in the order it is written.
func (m Model) commandHelp(topic string) ([]helpSection, bool) {
	spec, ok := command.SpecOf(command.Op(strings.ToLower(strings.TrimSpace(topic))))
	if !ok {
		return nil, false
	}

	sections := []helpSection{
		{title: "WHAT IT DOES", prose: []string{spec.What}},
		{title: "HOW IT IS WRITTEN", prose: []string{
			keys.Show(keys.Browse, keys.CommandLine) + " " + written(spec),
		}},
	}

	// Positional first, in the order they are written, then the rest. That is
	// the order a person types them, and any other order makes the reader work
	// out the grammar from a list that is not in it.
	var fields [][2]string
	for _, f := range append(spec.Positional(), pairOnly(spec)...) {
		fields = append(fields, [2]string{f.Key, describeField(f)})
	}
	if len(fields) > 0 {
		sections = append(sections, helpSection{title: "ITS FIELDS", rows: fields})
	}
	if spec.Creates != "" {
		sections = append(sections, helpSection{title: "IT CREATES", rows: [][2]string{
			{string(spec.Creates), "so it asks before it does, and cannot be undone after"},
		}})
	}
	return sections, true
}

// written is the command as a person would type it: the op, then its positional
// fields in order, then the pair-only ones as key/value.
func written(spec command.Spec) string {
	parts := []string{string(spec.Op)}
	for _, f := range spec.Positional() {
		if f.Required {
			parts = append(parts, "<"+f.Key+">")
			continue
		}
		parts = append(parts, "["+f.Key+"]")
	}
	for _, f := range pairOnly(spec) {
		parts = append(parts, "["+f.Key+" <"+string(f.Type)+">]")
	}
	return strings.Join(parts, " ")
}

func pairOnly(spec command.Spec) []command.Field {
	var out []command.Field
	for _, f := range spec.Fields {
		if !f.Positional {
			out = append(out, f)
		}
	}
	return out
}

// describeField says what a field is for and what it will accept.
func describeField(f command.Field) string {
	what := f.What
	if f.Required {
		what += " (required)"
	}
	switch {
	case len(f.Choices) > 0:
		what += " -- one of " + strings.Join(f.Choices, ", ")
	case len(f.Kinds) > 0:
		var kinds []string
		for _, k := range f.Kinds {
			kinds = append(kinds, article(string(k)))
		}
		what += " -- names " + strings.Join(kinds, " or ")
	case f.Type != command.FieldText:
		what += " -- " + string(f.Type)
	}
	return what
}

// pair is one row: the keys for these actions on this surface, and what they
// are for.
//
// An action the surface does not bind contributes nothing, so the listing
// cannot name a key that does not exist -- which is the whole reason the keys
// are read from the keymap rather than written out here beside the words.
func pair(ctx keys.Context, what string, actions ...keys.Action) [2]string {
	var shown []string
	for _, action := range actions {
		if s := keys.Show(ctx, action); s != "" {
			shown = append(shown, s)
		}
	}
	return [2]string{strings.Join(shown, " / "), what}
}

// renderHelp lays the sections out in two columns, the left one wide enough for
// the widest key in the whole listing rather than per section -- so the
// descriptions line up down the page and the eye reads one column, not nine.
func renderHelp(sections []helpSection, width int) []string {
	// The key column is sized across the WHOLE listing rather than per section,
	// so the meanings line up down the page and the eye reads one column
	// instead of nine.
	keyWidth := 0
	for _, s := range sections {
		for _, row := range s.rows {
			if n := len([]rune(row[0])); n > keyWidth {
				keyWidth = n
			}
		}
	}

	var out []string
	for _, s := range sections {
		if len(s.rows) == 0 && len(s.prose) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, "  "+titleStyle.Render(s.title))
		for _, line := range s.prose {
			for _, wrapped := range wrap(line, width-4) {
				out = append(out, "    "+wrapped)
			}
		}
		// The description wraps under itself rather than past the edge, hanging
		// at the column it started in -- so a long explanation still reads as
		// one entry and the key column stays a column.
		hang := strings.Repeat(" ", 4+keyWidth+2)
		for _, row := range s.rows {
			if row[0] == "" {
				continue // nothing on this surface binds it
			}
			lines := wrap(row[1], width-len(hang))
			out = append(out, fmt.Sprintf("    %-*s  %s", keyWidth, row[0], dimStyle.Render(lines[0])))
			for _, rest := range lines[1:] {
				out = append(out, hang+dimStyle.Render(rest))
			}
		}
	}
	return out
}

// helpAsked reports whether a command line is asking for help, and about what.
//
// `help` alone is all of it; `help acquire` is one command. Anything after the
// first word is the topic, so a typo reaches the help rather than the parser --
// which is the right place for somebody who is asking what the commands are.
func helpAsked(line string) (topic string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) == 0 || !strings.EqualFold(fields[0], "help") {
		return "", false
	}
	return strings.Join(fields[1:], " "), true
}

// helpTopics is every name `help` will answer to, for the completion the
// command line offers.
func helpTopics() []string {
	out := make([]string, 0, len(command.Specs()))
	for _, spec := range command.Specs() {
		out = append(out, string(spec.Op))
	}
	sort.Strings(out)
	return out
}
