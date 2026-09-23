// Package keys is the keymap.
//
// Every binding the interface has is in this file, and nowhere else. Before it
// existed the bindings were string literals in nine switch statements, and four
// separate places had to agree on them by hand: the switches themselves, the
// `hmsdev render` script parser, the test harness's key vocabulary, and a test
// holding a written-out list of every key. They drifted, and the drift was
// silent -- a script naming a key nobody had taught the parser about failed by
// producing a frame of something else.
//
// The bindings are emacs. C-n, C-p, C-f and C-b are the four-way motion
// everywhere they can be, esc and C-g both leave exactly one mode, and the
// verbs a row can be acted on with stay single letters the way dired and magit
// bind them. What is gone is the vim half: hjkl, gg, G, dd, zz, za, zR, zM, V,
// y, p, n, N, `/` and `:`.
package keys

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Action is what a key means. It is deliberately named for the INTENT rather
// than for any one widget's response to it, because the same intent lands
// differently depending on what is under the cursor: MoveRight is the next
// column in a table, the child node in a tree, and the next character in a
// field. Three meanings, one key, one action -- and each widget says which of
// the three it is, which is exactly the knowledge a widget has and this file
// does not.
type Action int

const (
	// None is "this file has nothing to say about that key", which for a text
	// field means the key is a character and for everything else means the
	// keystroke should fall through to whatever contains it.
	None Action = iota

	// Motion.
	MoveDown
	MoveUp
	MoveLeft
	MoveRight
	PageDown
	PageUp
	Top
	Bottom
	Recenter

	// Selection.
	ToggleSelect
	SelectVisible

	// The two that every mode answers to.
	Cancel
	Confirm

	// Searching, and stepping through what a search left. Two jobs, two keys:
	// one key doing both meant that once a filter was applied there was no way
	// back into the line to refine it, because the key that would have opened
	// it was busy stepping.
	Search
	NextMatch
	PrevMatch

	// The leaders.
	Jump
	CommandLine
	// Act lists what can be done to the thing under the cursor.
	//
	// It takes the space bar from ToggleSelect, which keeps C-space and gains
	// x. That is the one binding in this keymap most likely to annoy on the
	// first day, and it is worth it: selecting is something you do before a
	// verb, and the verb is the thing that had no key at all.
	Act

	// Acting on what the cursor is on.
	Consume
	Count
	MoveTo
	ToggleCustody
	Sort
	EditInPlace
	Create
	Refresh
	Copy
	Paste
	Kill
	Quit

	// The rail.
	//
	// LensFlip swaps the rail between the places and the kinds. It is one
	// action rather than two view keys because it is one question asked two
	// ways, and because a flip KEEPS what you are looking at -- which two
	// separate destinations could not.
	LensFlip
	// ViewAttention is what needs answering: the ledger's disagreements, and
	// the nudges.
	ViewAttention
	// OpenImport lists the plans waiting to be reviewed, so a review happens
	// against the house rather than after quitting it.
	OpenImport
	// Organise stages the arrangement rather than committing it, so a shape
	// can be tried before it is owned.
	Organise
	// TakeBack removes the last staged edit. It is undo for something that
	// has not happened yet, which is why it does not ask and why there is no
	// redo: nothing was written, so nothing is being recovered.
	TakeBack

	// Folding.
	FoldToggle
	FoldCycleAll
	// ToggleDepth swaps the contents pane between rolling up the whole
	// subtree and showing only what is filed at the node itself.
	ToggleDepth

	// The import plan.
	Drop
	Undrop
	ApplyAll
	// SkipStage sets a whole stage aside without applying any of it. Only a
	// stage that proposes a shape -- the categories, the places -- offers it,
	// and only when something follows.
	SkipStage

	// Editing a line.
	LineStart
	LineEnd
	WordLeft
	WordRight
	DeleteBack
	DeleteForward
	KillToStart
	KillToEnd
	KillWordBack
	KillWordForward
	Complete
	// Dismiss puts away the thing that opened over a field -- the completion
	// dropdown -- without leaving the field.
	//
	// It is a separate action from Cancel only because of what the two do when
	// there is no dropdown: S-Tab steps back a field, and esc closes the panel.
	// While one is open they are the same key by two names, which is the point.
	Dismiss
)

// Context is which keymap is being consulted.
//
// One flat map cannot express this keymap, because the same key is a different
// action in different places and no amount of renaming makes it one: tab
// completes a field and folds a tree, C-k retires a holding and kills to the
// end of a line, and `d` drops an import row while being nothing but the letter
// d inside a field. So the map is per surface, and a surface asks the one it
// belongs to.
type Context int

const (
	// Browse is the application underneath everything -- views, leaders, quit,
	// and the verbs that act on a row.
	Browse Context = iota
	// Table is the grid: motion, selection, sorting, stepping through matches.
	Table
	// Tree is consulted BEFORE Table, and holds only the keys a tree takes for
	// itself. Everything else it delegates.
	Tree
	// Line is a text field: the editor and the input line.
	Line
	// Creator is a Line plus the panel's field-to-field motion.
	Creator
	// Plan is the import review screen.
	Plan
	// Staging is the rail while the arrangement is being staged. Consulted
	// BEFORE Browse, and holding only what acts on the batch.
	Staging
)

// binding is one action and the keys that mean it. The first key is the one
// help text shows; the rest are aliases, and an alias is not decoration -- the
// arrow keys and Home/End/PgUp/PgDn exist so that every Meta binding has a
// path that does not go through Meta at all.
type binding struct {
	action Action
	keys   []string
	label  string
}

// motion is shared by every surface that has a cursor, so the four-way motion
// is written once and cannot disagree with itself.
var motion = []binding{
	{MoveDown, []string{"ctrl+n", "down"}, "move"},
	{MoveUp, []string{"ctrl+p", "up"}, "move"},
	{PageDown, []string{"ctrl+v", "pgdown"}, "page"},
	{PageUp, []string{"alt+v", "pgup"}, "page"},
	{Top, []string{"alt+<", "home"}, "top"},
	{Bottom, []string{"alt+>", "end"}, "bottom"},
	{Recenter, []string{"ctrl+l"}, "recentre"},
}

// cancel is esc and C-g, together, everywhere. Two names for one escape, and
// the pair is defined once so no mode can end up answering to only one of them.
var cancel = binding{Cancel, []string{"esc", "ctrl+g"}, "cancel"}

var contexts = map[Context][]binding{
	Table: append(append([]binding{}, motion...), []binding{
		{MoveLeft, []string{"ctrl+b", "left"}, "column"},
		{MoveRight, []string{"ctrl+f", "right"}, "column"},
		{ToggleSelect, []string{"ctrl+@", "x"}, "select"},
		{SelectVisible, []string{"alt+h"}, "select all"},
		{NextMatch, []string{"alt+n"}, "next match"},
		{PrevMatch, []string{"alt+p"}, "previous match"},
		{Sort, []string{"s"}, "sort"},
		cancel,
	}...),

	// Only what a tree takes before the table sees it. C-b and C-f are ascend
	// and descend here rather than column motion, and tab folds.
	Tree: {
		{MoveLeft, []string{"ctrl+b", "left"}, "ascend"},
		{MoveRight, []string{"ctrl+f", "right"}, "descend"},
		{FoldToggle, []string{"tab"}, "fold"},
		{FoldCycleAll, []string{"shift+tab"}, "fold all"},
	},

	Browse: append(append([]binding{}, motion...), []binding{
		{Confirm, []string{"enter"}, "history"},
		cancel,
		{Quit, []string{"q", "ctrl+c"}, "quit"},
		{Search, []string{"ctrl+s"}, "search"},
		{Jump, []string{"alt+g"}, "jump"},
		{CommandLine, []string{"alt+x"}, "command"},
		{Act, []string{" "}, "act"},
		{EditInPlace, []string{"e"}, "rename"},
		{Create, []string{"o"}, "new"},
		{Refresh, []string{"g"}, "refresh"},
		{ToggleDepth, []string{"v"}, "here only"},
		{Consume, []string{"c"}, "consume"},
		{Count, []string{"#"}, "count"},
		{MoveTo, []string{"m"}, "move"},
		{ToggleCustody, []string{"t"}, "custody"},
		{Copy, []string{"alt+w"}, "copy"},
		{Paste, []string{"ctrl+y"}, "put"},
		{Kill, []string{"ctrl+k"}, "retire"},
		// The backslash, because it is unshifted, unused by readline, and not
		// a character anything in this house is called.
		{LensFlip, []string{"\\"}, "lens"},
		{ViewAttention, []string{"!"}, "attention"},
		{OpenImport, []string{"i"}, "import"},
		{Organise, []string{"O"}, "organise"},
	}...),

	// Staging is the rail while the arrangement is being staged. It is
	// consulted BEFORE Browse and holds only the keys that act on the BATCH;
	// everything else -- the motion, the folds, and the verbs that stage --
	// is the browse keymap unchanged, because staging a rename is the same
	// gesture as renaming and the whole point is that it is.
	//
	// A context rather than a handful of special cases inside the drawer,
	// for the reason Context exists: `A` applies here and means nothing while
	// browsing, and `q` has to leave the batch rather than the program.
	Staging: {
		{ApplyAll, []string{"A"}, "apply the lot"},
		{TakeBack, []string{"u"}, "take back"},
		{Quit, []string{"q"}, "abandon"},
		cancel,
	},

	Line: lineBindings(),

	// A field, plus the panel's own motion. C-n and C-p are already line keys --
	// here they step between FIELDS, or through a dropdown open over one, which
	// is the same intent landing on what this surface actually has.
	//
	// S-Tab is Dismiss rather than an alias of MoveUp, and the difference only
	// shows when a dropdown is open: then it puts the dropdown away, where C-p
	// moves the highlight inside it. With nothing open the panel reads Dismiss
	// as "back a field", which is what S-Tab has always done here.
	Creator: alias(alias(lineBindings(),
		MoveDown, "next field"),
		MoveUp, "previous field"),

	Plan: {
		{Confirm, []string{"enter"}, "settle"},
		{Drop, []string{"d"}, "drop"},
		{Undrop, []string{"u"}, "undrop"},
		{ApplyAll, []string{"A"}, "apply"},
		// Shifted, like apply, because both act on the WHOLE screen rather than
		// on the row under the cursor -- and because `s` is the table's sort.
		{SkipStage, []string{"S"}, "skip the stage"},
		{EditInPlace, []string{"e"}, "edit"},
		{Quit, []string{"q", "ctrl+c"}, "cancel"},
	},
}

// lineBindings is readline as emacs has it, and it is a function rather than a
// variable because Creator appends to it -- a shared slice appended to twice is
// the kind of aliasing bug that shows up as one panel's keys leaking into
// another's.
func lineBindings() []binding {
	return []binding{
		{LineStart, []string{"ctrl+a", "home"}, "start"},
		{LineEnd, []string{"ctrl+e", "end"}, "end"},
		{MoveLeft, []string{"ctrl+b", "left"}, "back"},
		{MoveRight, []string{"ctrl+f", "right"}, "forward"},
		{WordLeft, []string{"alt+b"}, "word back"},
		{WordRight, []string{"alt+f"}, "word forward"},
		{DeleteBack, []string{"backspace"}, "rub out"},
		{DeleteForward, []string{"ctrl+d", "delete"}, "delete"},
		{KillToEnd, []string{"ctrl+k"}, "kill to end"},
		{KillToStart, []string{"ctrl+u"}, "clear"},
		{KillWordBack, []string{"ctrl+w"}, "kill word"},
		{KillWordForward, []string{"alt+d"}, "kill word forward"},
		{Complete, []string{"tab"}, "take it"},
		{Dismiss, []string{"shift+tab"}, "dismiss"},
		// C-n and C-p reach a LINE only to move through the completions open
		// over it. A single-line field has nothing else for them to do, and a
		// dropdown that could not be walked would be a list with one usable
		// entry -- which is what the first version was.
		//
		// The arrows are here for the same reason they are on Creator: a list
		// of options is the one place in a terminal where everybody reaches for
		// the down arrow first, and this context had them bound on the panel's
		// dropdown and NOT on the prompt's -- so the same list answered the
		// same key in two places and refused it in a third.
		{MoveDown, []string{"ctrl+n", "down"}, "next"},
		{MoveUp, []string{"ctrl+p", "up"}, "previous"},
		{Confirm, []string{"enter"}, "accept"},
		cancel,
	}
}

// alias extends an action a surface already binds, so a context built on the
// shared line bindings adds its own keys rather than restating the list -- two
// statements of one binding are two things to keep in step.
func alias(bindings []binding, action Action, label string, extra ...string) []binding {
	for i := range bindings {
		if bindings[i].action != action {
			continue
		}
		bindings[i].keys = append(append([]string{}, bindings[i].keys...), extra...)
		bindings[i].label = label
		return bindings
	}
	return append(bindings, binding{action, extra, label})
}

// lookups is contexts inverted, built once, so a keystroke costs a map read
// rather than a scan of every binding on the surface.
var lookups = func() map[Context]map[string]Action {
	out := make(map[Context]map[string]Action, len(contexts))
	for ctx, bindings := range contexts {
		m := make(map[string]Action, len(bindings)*2)
		for _, b := range bindings {
			for _, k := range b.keys {
				m[k] = b.action
			}
		}
		out[ctx] = m
	}
	return out
}()

// Lookup is what a key means on a surface, or None.
func Lookup(ctx Context, msg tea.KeyMsg) Action {
	return lookups[ctx][msg.String()]
}

// IsText reports whether a keystroke should be inserted into a field as
// characters.
//
// The Alt test is the whole reason this is a function. Bubbletea reports M-w as
// KeyRunes with Alt set, so a field that switched on msg.Type alone typed a
// literal "w" every time somebody reached for a Meta binding while a field
// happened to be open -- a key that does nothing here silently becoming a key
// that writes.
func IsText(msg tea.KeyMsg) bool {
	if msg.Alt {
		return false
	}
	return msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace
}

// Show is how help text names a key: the first binding for the action, written
// the way emacs writes it.
func Show(ctx Context, action Action) string {
	for _, b := range contexts[ctx] {
		if b.action == action && len(b.keys) > 0 {
			return Display(b.keys[0])
		}
	}
	return ""
}

// ShowAll is every key bound to an action on a surface, for help text that has
// room to name the alternatives.
//
// Show names one key because a footer has room for one. Help does not have that
// excuse, and an alternative nobody is told about is one nobody uses: the
// arrows walk a dropdown, and the only reason to know that was to try it.
func ShowAll(ctx Context, action Action) string {
	for _, b := range contexts[ctx] {
		if b.action != action {
			continue
		}
		shown := make([]string, 0, len(b.keys))
		for _, k := range b.keys {
			shown = append(shown, Display(k))
		}
		return strings.Join(shown, " / ")
	}
	return ""
}

// Label is the word help text uses for an action on this surface. It belongs to
// the surface, not to the action: MoveLeft is "column" in a table and "ascend"
// in a tree, and a single word for both would be wrong in one of them.
func Label(ctx Context, action Action) string {
	for _, b := range contexts[ctx] {
		if b.action == action {
			return b.label
		}
	}
	return ""
}

// Hint renders the actions as a help line -- "C-n/C-p move   s sort".
//
// Actions given together share one entry, because "C-n move  C-p move" says
// twice what "C-n/C-p move" says once, and the footer has one line.
func Hint(ctx Context, groups ...[]Action) string {
	var parts []string
	for _, group := range groups {
		var shown []string
		for _, a := range group {
			if s := Show(ctx, a); s != "" {
				shown = append(shown, s)
			}
		}
		if len(shown) == 0 {
			continue
		}
		parts = append(parts, strings.Join(shown, "/")+" "+Label(ctx, group[0]))
	}
	return strings.Join(parts, " - ")
}

// Display writes a key the way emacs writes it. C-n, not ctrl+n.
func Display(key string) string {
	switch key {
	case " ":
		return "space"
	case "ctrl+@":
		return "C-space"
	case "shift+tab":
		return "S-TAB"
	case "tab":
		return "TAB"
	case "esc":
		return "esc"
	}
	if rest, ok := strings.CutPrefix(key, "ctrl+"); ok {
		return "C-" + rest
	}
	if rest, ok := strings.CutPrefix(key, "alt+"); ok {
		return "M-" + rest
	}
	return key
}

// Bindings is every binding on a surface, for the tests that check this file
// against the rest of the system.
func Bindings(ctx Context) [][]string {
	var out [][]string
	for _, b := range contexts[ctx] {
		out = append(out, b.keys)
	}
	return out
}

// Contexts is every surface, so a test can walk the whole keymap.
func Contexts() []Context {
	return []Context{Browse, Table, Tree, Line, Creator, Plan, Staging}
}
