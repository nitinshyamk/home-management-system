package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"home-management-system/internal/command"
	"home-management-system/internal/tui/complete"
	"home-management-system/internal/tui/keys"

	"home-management-system/internal/tui/style"

	"home-management-system/internal/tui/text"
)

// Help, generated rather than written.
//
// Every line below comes from a declaration somewhere else -- the keymap in
// internal/tui/keys, the command vocabulary in internal/command/spec.go. Help
// text written by hand is help text that is wrong within a release: it is the
// one kind of documentation nobody re-reads, because the people who would spot
// the drift are the people who already know the answer.
//
// The same argument is why `hmsdev schema` emits the command spec rather than a
// description of it, and this is that idea pointed at a person instead of an
// agent.

// helpSection is a heading and its rows, each a key or name against what it
// does.
type helpSection struct {
	// topic is what `help <topic>` answers to, and what the index lists. Empty
	// for the sections that only ever appear inside one command's help, which
	// are reached by naming the command instead.
	topic string
	title string
	// what is the topic in a few lowercase words, for the index. The title is a
	// heading and shouts; an index of eleven shouting headings is the wall of
	// text this exists to break up.
	what string
	rows [][2]string
	// prose is free text under the heading, wrapped to the terminal. The
	// two-column layout is for keys against meanings; a whole command written
	// out is one long thing and forcing it into a column truncates it.
	prose []string
}

// helpLines renders the help for a topic.
//
// An empty topic is the INDEX -- eleven lines naming what there is -- rather
// than the whole listing. The whole listing is 102 lines on a 100-column
// terminal, of which one section is 35 rows, on a surface that scrolls a line
// at a time: four screens of it, with no way to see what sections exist without
// going past all of them. A page nobody reaches the end of teaches whatever is
// on its first screen and nothing else.
//
// `help all` is still the whole thing, for anyone who wants to read or search
// it in one piece.
func (m Model) helpLines(topic string) []string {
	lines, _ := m.helpPage(topic)
	return lines
}

// helpPage is helpLines plus where the headings are, for the surface that steps
// between them.
func (m Model) helpPage(topic string) (lines []string, headings []int) {
	width := max(40, m.width-6)
	switch topic {
	case "":
		return renderHelp(m.helpIndex(), width)
	case "all":
		return renderHelp(m.generalHelp(), width)
	}
	// A topic names a section, a command, or neither.
	for _, section := range m.generalHelp() {
		if section.topic != "" && strings.EqualFold(section.topic, topic) {
			return renderHelp([]helpSection{section}, width)
		}
	}
	sections, ok := m.commandHelp(topic)
	if !ok {
		return renderHelp(m.noSuchCommand(topic), width)
	}
	return renderHelp(sections, width)
}

// helpIndex is what there is: one line per section, with how much is in it.
//
// It exists so that the first screen of the help answers "what can I ask
// about", which the listing itself never did -- you had to read all of it to
// find out it had an IN A TREE section, by which point you had read the tree
// keys anyway.
func (m Model) helpIndex() []helpSection {
	var rows [][2]string
	for _, section := range m.generalHelp() {
		if section.topic == "" {
			continue
		}
		unit := "keys"
		if section.topic == "commands" {
			unit = "names"
		}
		rows = append(rows, [2]string{
			fmt.Sprintf("%s  %d %s", section.topic, len(section.rows), unit),
			section.what,
		})
	}
	// Rows rather than prose, because these ARE two columns: a thing to type
	// against what it gets you. Prose is wrapped by words, which collapses the
	// gap that was doing the aligning.
	line := keys.Show(keys.Browse, keys.CommandLine) + " help "
	return []helpSection{
		{title: "WHAT THERE IS", rows: rows},
		{title: "READING MORE OF IT", rows: [][2]string{
			{line + "<topic>", "one of the above"},
			{line + "<command>", "one command, and what it will accept"},
			{line + "all", "the whole listing, as one page"},
			// The keys that move around whatever you open, said here because
			// the page they work on is the page you reach from this one.
			pair(keys.Tree, "step to the next section, and the one before",
				keys.FoldToggle, keys.FoldCycleAll),
			pair(keys.Browse, "back to where you were", keys.Cancel),
		}},
	}
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
	for _, name := range m.helpTopics() {
		vocabulary = append(vocabulary, complete.Match{Path: name, Leaf: name})
	}
	if near := complete.Near(vocabulary, topic); len(near) > 0 {
		var rows [][2]string
		for _, name := range near {
			rows = append(rows, [2]string{name, m.describeTopic(name)})
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
		{topic: "moving", what: "around a list or a tree, a row or a screen at a time", title: "MOVING", rows: [][2]string{
			pair(keys.Table, "down and up a row", keys.MoveDown, keys.MoveUp),
			pair(keys.Table, "between columns, or in and out of a tree", keys.MoveRight, keys.MoveLeft),
			pair(keys.Table, "half a screen", keys.PageDown, keys.PageUp),
			pair(keys.Table, "the first row, and the last", keys.Top, keys.Bottom),
			pair(keys.Table, "put the cursor back in the middle", keys.Recenter),
		}},
		{topic: "rows", what: "picking rows to act on, sorting, clearing", title: "CHOOSING ROWS", rows: [][2]string{
			pair(keys.Table, "pick this row, and move on", keys.ToggleSelect),
			pair(keys.Table, "pick everything a filter left", keys.SelectVisible),
			pair(keys.Table, "sort by the focused column", keys.Sort),
			pair(keys.Browse, "clear the selection, then the filter", keys.Cancel),
		}},
		{topic: "finding", what: "filtering, jumping across the house, the command line", title: "FINDING THINGS", rows: [][2]string{
			pair(keys.Browse, "search this view -- comes back with what is applied", keys.Search),
			pair(keys.Table, "step through what the search left", keys.NextMatch, keys.PrevMatch),
			pair(keys.Browse, "jump to anything, of any kind", keys.Jump),
			pair(keys.Browse, "the command line", keys.CommandLine),
		}},
		{topic: "views", what: "the five views, a holding's history, refresh, quit", title: "VIEWS", rows: [][2]string{
			{"1 - 5", "Categories, Locations, Items, Holdings, Integrity"},
			pair(keys.Browse, "open a holding's history", keys.Confirm),
			pair(keys.Browse, "read it again from the database", keys.Refresh),
			pair(keys.Browse, "quit", keys.Quit),
		}},
		{topic: "trees", what: "folding, and showing what a node contains", title: "IN A TREE", rows: [][2]string{
			pair(keys.Tree, "fold the node under the cursor", keys.FoldToggle),
			pair(keys.Tree, "fold everything, or unfold it", keys.FoldCycleAll),
			pair(keys.Browse, "show what the nodes contain -- items, or holdings", keys.ToggleDepth),
		}},
		// Its own section, because the gesture is not a tree's.
		//
		// It was listed under IN A TREE, where it was added, and that is the
		// half of it that is least true: it starts in the Holdings table as
		// often as in a tree, and CROSSING VIEWS in the middle is the whole
		// point -- you pick a thing up where you can see it is wrong and put it
		// down where it is right.
		{topic: "carrying", what: "picking a thing up here and putting it down there", title: "MOVING A THING BY POINTING AT WHERE IT GOES", rows: [][2]string{
			pair(keys.Browse, "pick up the row under the cursor", keys.Copy),
			pair(keys.Browse, "or pick it up and be asked where it goes", keys.MoveTo),
			pair(keys.Browse, "put it in the place or classification you are on", keys.Paste),
			pair(keys.Browse, "put it down, having changed your mind", keys.Cancel),
		}, prose: []string{
			"What is in hand is named at the bottom of the screen until it is " +
				"put down, and it stays in hand across the views -- so you can " +
				"pick a thing up in one and go looking for where it belongs in " +
				"another.",
			"While something is in hand, a tree showing its contents goes faint " +
				"over the things inside and the cursor steps over them: what you " +
				"are looking for is a place to put the thing, and the things " +
				"already in those places are not places.",
		}},
		{topic: "acting", what: "use, count, move, custody, rename, create, retire", title: "ACTING ON A ROW", rows: [][2]string{
			pair(keys.Browse, "use some of it", keys.Consume),
			pair(keys.Browse, "say how much is actually there", keys.Count),
			pair(keys.Browse, "move it somewhere", keys.MoveTo),
			pair(keys.Browse, "take it out, or bring it back", keys.ToggleCustody),
			pair(keys.Browse, "rename it, in place", keys.EditInPlace),
			pair(keys.Browse, "make a new one inside this", keys.Create),
			pair(keys.Browse, "retire it -- it asks first", keys.Kill),
		}},
		{topic: "organising", what: "trying a shape before you own it", title: "ORGANISE MODE", rows: [][2]string{
			pair(keys.Browse, "start staging instead of writing", keys.Organise),
			pair(keys.Staging, "apply the whole batch, in one transaction", keys.ApplyAll),
			pair(keys.Staging, "take back the last thing you staged", keys.TakeBack),
			pair(keys.Staging, "put the batch down; nothing is written", keys.Quit),
		}, prose: []string{
			"While a batch is open the verbs above stage instead of writing, " +
				"and the region below the house fills up with what they would " +
				"come to. The verbs are the same verbs -- the only difference " +
				"is when they take effect.",
			"Only the ARRANGEMENT is staged: what something is called and " +
				"where it sits. Using, counting and custody write at once, " +
				"because those are not free to undo and a batch that promised " +
				"otherwise would be promising something it cannot do.",
		}},
		{topic: "walking", what: "checking the house against the ledger", title: "THE WALK", rows: [][2]string{
			pair(keys.Browse, "walk everything under this node", keys.Verify),
			pair(keys.Walk, "the ledger has it right", keys.Right),
			pair(keys.Walk, "there is this much instead", keys.Amount),
			pair(keys.Walk, "it is not there", keys.Missing),
			pair(keys.Walk, "file what was checked, in one transaction", keys.ApplyAll),
			pair(keys.Walk, "look at one again", keys.MoveUp, keys.MoveDown),
		}, prose: []string{
			"Saying a count is right is not nothing: it records that somebody " +
				"looked. Only a count that DISAGREES also records a correction, " +
				"which is what lets a shelf nobody has checked be told apart " +
				"from one that was checked and found correct.",
			"Something you cannot find is marked lost, not deleted. You have " +
				"stopped knowing where it is, which is a different claim from " +
				"it having ceased to exist -- and it is what lets the thing be " +
				"found again rather than made again.",
			"Filing a walk part-way writes what was checked and says nothing " +
				"about the rest.",
		}},
		{topic: "fields", what: "readline, as emacs has it", title: "TYPING IN A FIELD", rows: [][2]string{
			pair(keys.Line, "the start of the line, and the end", keys.LineStart, keys.LineEnd),
			pair(keys.Line, "a word back, and a word forward", keys.WordLeft, keys.WordRight),
			pair(keys.Line, "delete the character under the cursor", keys.DeleteForward),
			pair(keys.Line, "kill to the start, and to the end", keys.KillToStart, keys.KillToEnd),
			pair(keys.Line, "kill the word behind, and the word ahead", keys.KillWordBack, keys.KillWordForward),
		}},
		{topic: "completion", what: "taking what is offered, and walking a path", title: "WHEN A LIST IS OPEN OVER A FIELD", rows: [][2]string{
			pair(keys.Line, "take what is highlighted -- and taking a place "+
				"offers what is inside it, so a path is walked rather than "+
				"typed out", keys.Complete, keys.Confirm),
			// Every key, not the first one: the arrows work here, and an
			// alternative nobody is told about is one nobody uses.
			pairAll(keys.Line, "choose", keys.MoveDown, keys.MoveUp),
			pair(keys.Line, "put the list away and keep typing", keys.Dismiss, keys.Cancel),
		}},
		{topic: "plan", what: "settling rows, dropping them, applying the file", title: "ON THE IMPORT PLAN", rows: [][2]string{
			pair(keys.Plan, "settle the row under the cursor -- agree to what it "+
				"would create, take the suggestion it rests on, or make the thing "+
				"it names", keys.Confirm),
			pair(keys.Plan, "drop a row, or take it back", keys.Drop, keys.Undrop),
			pair(keys.Plan, "apply the stage on screen, in one transaction", keys.ApplyAll),
			// A file that proposes categories or places is reviewed in stages,
			// and each of those is optional. Saying so here is the only place
			// somebody who has not met one yet will read it.
			pair(keys.Plan, "skip a bulk upload of categories or places, applying "+
				"none of it, and go on to the next stage", keys.SkipStage),
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
		topic: "commands",
		what:  "everything the command line takes",
		title: "COMMANDS  (" + keys.Show(keys.Browse, keys.CommandLine) + " help <name> for one of them)",
		rows:  commands,
	})
	return sections
}

// describeTopic says what a name will get you, whether it is a command or one
// of the sections.
func (m Model) describeTopic(name string) string {
	if spec, ok := command.SpecOf(command.Op(name)); ok {
		return spec.What
	}
	for _, section := range m.generalHelp() {
		if section.topic == name {
			return section.what
		}
	}
	if name == "all" {
		return "the whole listing, as one page"
	}
	return ""
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
			kinds = append(kinds, text.Article(string(k)))
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

// pairAll is pair, naming every key bound to each action rather than one.
func pairAll(ctx keys.Context, what string, actions ...keys.Action) [2]string {
	var shown []string
	for _, action := range actions {
		if s := keys.ShowAll(ctx, action); s != "" {
			shown = append(shown, s)
		}
	}
	return [2]string{strings.Join(shown, " / "), what}
}

// renderHelp lays the sections out in two columns, the left one wide enough for
// the widest key in the whole listing rather than per section -- so the
// descriptions line up down the page and the eye reads one column, not nine.
func renderHelp(sections []helpSection, width int) (lines []string, headings []int) {
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
		headings = append(headings, len(out))
		out = append(out, "  "+style.Strong.Render(s.title))
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
			wrapped := wrap(row[1], width-len(hang))
			out = append(out, fmt.Sprintf("    %-*s  %s", keyWidth, row[0], style.Dim.Render(wrapped[0])))
			for _, rest := range wrapped[1:] {
				out = append(out, hang+style.Dim.Render(rest))
			}
		}
	}
	return out, headings
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
// command line offers and for the near-miss suggestions.
//
// The sections are in it as well as the commands. A topic you cannot complete
// and are never offered is a topic only somebody who already read the index
// knows about, and the index exists for the people who have not.
func (m Model) helpTopics() []string {
	out := make([]string, 0, len(command.Specs())+12)
	for _, spec := range command.Specs() {
		out = append(out, string(spec.Op))
	}
	for _, section := range m.generalHelp() {
		if section.topic != "" {
			out = append(out, section.topic)
		}
	}
	out = append(out, "all")
	sort.Strings(out)
	return out
}
