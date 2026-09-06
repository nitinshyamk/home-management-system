// Package creator is the one creation panel.
//
// One panel for Item, Category, and Location, and the SAME panel the import
// plan screen opens for a receipt row that would create something. That sharing
// is not an optimisation: it is what keeps the interactive and bulk flows from
// drifting into different products. If the plan screen ever needs this
// reworked, the two have already diverged.
//
// It builds a `:` line rather than a Command. Everything the interface writes
// goes through Parse and Bind, so a panel cannot become a second way to create
// things that validates differently from the first.
package creator

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/complete"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/line"

	"home-management-system/internal/tui/style"
)

// Kind is what the panel is making.
type Kind string

const (
	KindItem     Kind = "item"
	KindCategory Kind = "category"
	KindLocation Kind = "location"
)

// Counting is the Item variant choice: three presets, never two booleans.
// Nobody reads the word "fungible".
type Counting struct {
	Value string
	Label string
}

var countings = []Counting{
	{"unique", "one of a kind"},
	{"pile", "a pile I count"},
	{"measured", "something I measure"},
}

// field is one input in the panel.
type field struct {
	key   string
	label string
	value string
	// cursor is where the next character goes, as a rune index into value.
	// The panel's fields had none until the bindings became emacs, and could
	// only be appended to and backspaced over.
	cursor int
	// hint is what the field is for, shown while it is focused. A label alone
	// says what to type; the hint says why.
	hint string
	// measured marks the fields that exist only for a measured or counted Item.
	// They appear BECAUSE of the counting choice -- a unit box that is always
	// visible and sometimes meaningless is the nullable-column muddle this
	// project spent months eliminating, drawn on a screen.
	measured bool

	// resolves names the kind of thing this field refers to, or "" for free
	// text. The panel does not do the matching itself -- it says what it wants
	// and the caller answers from the resolve index, so the panel and the
	// command line agree about what a name nearly is.
	resolves string

	// choices is a CLOSED vocabulary, offered instead of asking the caller.
	//
	// Units are the case: seven codes in a reference table, not a household's
	// growing list of shelves. A closed set is worth telling the field about
	// directly, because there is nothing to resolve and no index to consult.
	choices []string

	// warns names a kind whose existing members are shown but NOT offered.
	//
	// The item name uses it. Naming a new item something that already exists is
	// legal -- the schema permits two items sharing a name, told apart by their
	// category -- so this is not a refusal. It is the fact, put where the
	// decision is being made, and deliberately not takeable: a completion here
	// would make recreating what you already have the fastest path through the
	// form.
	warns string
}

// Model is the panel.
type Model struct {
	open     bool
	kind     Kind
	fields   []field
	focus    int
	counting int
	width    int
	// list is the dropdown over the focused field. Offered and never applied:
	// a completion that filled itself in would be the resolver deciding, which
	// is the one thing it must never do.
	list complete.Model
	// warnings are existing things matching the focused field, shown and not
	// takeable. See field.warns.
	warnings []string
	// parent is the thing the cursor was on when the panel opened, offered as
	// the default parent or category. Creating inside what you are looking at
	// is what o means.
	parent string
	// units is the unit vocabulary, handed in by the caller because it comes
	// from the database. Seven codes in a reference table -- a closed set, so
	// the field carries it directly rather than asking the resolver.
	units []string
}

var ()

func New() Model { return Model{width: 80} }

// WithUnits hands the panel the unit vocabulary. It survives Open, because the
// units are reference data and do not change while a panel is being filled in.
func (m Model) WithUnits(units []string) Model {
	m.units = units
	for i := range m.fields {
		if m.fields[i].key == "unit" {
			m.fields[i].choices = units
		}
	}
	return m
}

func (m Model) SetWidth(w int) Model { m.width = w; return m }

// Open starts a fresh panel.
//
// Fresh every time: a panel that remembered an abandoned attempt will
// eventually create it.
func (m Model) Open(kind Kind, parent string) Model {
	units := m.units
	m = Model{open: true, kind: kind, width: m.width, parent: parent, counting: 2, units: units}
	switch kind {
	case KindItem:
		m.fields = []field{
			// The name WARNS rather than completes. Two items may share a name
			// -- the schema permits it, told apart by category -- so an
			// existing Turmeric is a fact worth putting in front of somebody
			// naming a new one, and not a thing to offer them.
			{key: "name", label: "name", hint: "what it is called", warns: "Item"},
			{key: "counting", label: "counting", hint: "how it is counted -- PERMANENT"},
			{key: "unit", label: "unit", hint: "what it is measured in -- PERMANENT",
				measured: true, choices: units},
			{key: "package", label: "per package", hint: "how much is in one package", measured: true},
			{key: "category", label: "category", value: parent, hint: "where to file it", resolves: "Category"},
		}
	case KindCategory:
		m.fields = []field{
			{key: "name", label: "name", hint: "what it is called"},
			{key: "under", label: "under", value: parent, hint: "the classification it belongs to", resolves: "Category"},
			{key: "describe", label: "describe", hint: "what belongs in it"},
		}
	case KindLocation:
		m.fields = []field{
			{key: "name", label: "name", hint: "what it is called"},
			{key: "under", label: "under", value: parent, hint: "the place it is inside", resolves: "Location"},
			{key: "describe", label: "describe", hint: "what it is"},
		}
	}
	return m.cursorsToEnd()
}

// cursorsToEnd puts every cursor after the text already in its field.
//
// The parent fields arrive pre-filled with whatever the cursor was on. A cursor
// left at zero would put the next character typed in FRONT of that -- "Kitchen"
// becoming "sKitchen" -- which is not what a field that has always appended
// does, and not what anybody means.
func (m Model) cursorsToEnd() Model {
	for i := range m.fields {
		m.fields[i].cursor = len([]rune(m.fields[i].value))
	}
	return m
}

// WithName pre-fills the name, for a panel opened because a row named
// something that is not there yet. Retyping a name the file already carries is
// how a receipt ends up with a second spelling of one thing.
func (m Model) WithName(name string) Model {
	for i := range m.fields {
		if m.fields[i].key == "name" {
			m.fields[i].value = name
			m.fields[i].cursor = len([]rune(name))
		}
	}
	return m
}

func (m Model) IsOpen() bool { return m.open }
func (m Model) Kind() Kind   { return m.kind }
func (m Model) Close() Model { m.open = false; return m }

// Value returns a field's contents.
func (m Model) Value(key string) string {
	for _, f := range m.fields {
		if f.key == key {
			return strings.TrimSpace(f.value)
		}
	}
	return ""
}

// Resolving reports what kind of thing the focused field refers to, and what
// has been typed into it, so the caller can offer completions. Empty when the
// field is free text or has a closed vocabulary of its own.
func (m Model) Resolving() (kind, typed string) {
	f, ok := m.focused()
	if !ok {
		return "", ""
	}
	return f.resolves, strings.TrimSpace(f.value)
}

// Warning reports the kind whose existing members the focused field warns
// about, and what has been typed. Empty for every field but the item name.
func (m Model) Warning() (kind, typed string) {
	f, ok := m.focused()
	if !ok {
		return "", ""
	}
	return f.warns, strings.TrimSpace(f.value)
}

// Choices is the focused field's own closed vocabulary, if it has one.
func (m Model) Choices() ([]string, string) {
	f, ok := m.focused()
	if !ok {
		return nil, ""
	}
	return f.choices, strings.TrimSpace(f.value)
}

func (m Model) focused() (field, bool) {
	if !m.open {
		return field{}, false
	}
	visible := m.visible()
	if len(visible) == 0 {
		return field{}, false
	}
	return m.fields[visible[m.focus]], true
}

// SetSuggestions offers completions for the focused field.
//
// Recomputed on every keystroke, so it goes through Offer rather than replacing
// the list: Offer keeps the highlight on the option it was on, and honours a
// list the person has already dismissed.
func (m Model) SetSuggestions(suggestions []string) Model {
	_, typed := m.Resolving()
	if choices, value := m.Choices(); len(choices) > 0 {
		typed = value
	}
	m.list = m.list.Offer(suggestions, typed)
	return m
}

// SetWarnings shows what already exists under the focused field.
func (m Model) SetWarnings(warnings []string) Model {
	m.warnings = warnings
	return m
}

// Suggestions are what is currently on offer.
func (m Model) Suggestions() []string { return m.list.Options() }

// Counting is the chosen preset.
func (m Model) Counting() string { return countings[m.counting].Value }

// measures reports whether the chosen preset needs a unit at all.
func (m Model) measures() bool { return m.Counting() != "unique" }

// visible is the fields the current choice actually needs.
func (m Model) visible() []int {
	var out []int
	for i, f := range m.fields {
		if f.measured && !m.measures() {
			continue
		}
		out = append(out, i)
	}
	return out
}

// Line renders the panel as the `:` command it stands for.
//
// A command rather than a Command: everything the interface writes goes through
// Parse and Bind, so the panel cannot validate differently from the typed line
// or from a CSV row. It is the same contract with a nicer way to fill it in.
func (m Model) Line() string {
	var b strings.Builder
	fmt.Fprintf(&b, "new %s %s", m.kind, quote(m.Value("name")))
	if m.kind == KindItem {
		fmt.Fprintf(&b, " counting %s", m.Counting())
	}
	for _, i := range m.visible() {
		f := m.fields[i]
		if f.key == "name" || f.key == "counting" {
			continue
		}
		if value := strings.TrimSpace(f.value); value != "" {
			fmt.Fprintf(&b, " %s %s", f.key, quote(value))
		}
	}
	return b.String()
}

func quote(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\"") {
		return `"` + strings.ReplaceAll(s, `"`, "") + `"`
	}
	return s
}

// Update handles a keystroke, reporting whether it was consumed.
//
// The dropdown is consulted FIRST, and that ordering is the whole of the
// interaction: while a list is open, Tab takes what is highlighted and C-n
// moves the highlight; with nothing open the same two keys are field motion.
// The panel does not branch on whether a list is up -- the list reports what it
// did not use, exactly as every other layer in this interface does.
func (m Model) Update(msg tea.KeyMsg) (Model, bool) {
	if !m.open {
		return m, false
	}
	visible := m.visible()
	if len(visible) == 0 {
		return m, false
	}
	at := visible[m.focus]
	onCounting := m.fields[at].key == "counting"

	if next, taken, handled := m.list.Update(msg); handled {
		m.list = next
		if taken != "" {
			m.fields[at].value = taken
			m.fields[at].cursor = len([]rune(taken))
		}
		return m, true
	}

	action := keys.Lookup(keys.Creator, msg)

	// The counting choice is not a text field -- it is one of three answers --
	// so it takes the motion keys and nothing else. Any other keystroke would
	// silently vanish into a field that has nowhere to put it.
	if onCounting {
		switch action {
		case keys.MoveLeft:
			m.counting = (m.counting - 1 + len(countings)) % len(countings)
			m.focus = m.clampFocus()
			return m, true
		case keys.MoveRight:
			m.counting = (m.counting + 1) % len(countings)
			m.focus = m.clampFocus()
			return m, true
		}
	}

	switch action {
	case keys.Complete, keys.MoveDown:
		return m.moveFocus(1), true
	case keys.MoveUp, keys.Dismiss:
		// S-Tab is Dismiss, and with no list to dismiss it steps back a field
		// -- which is what it has always done here.
		return m.moveFocus(-1), true
	}

	if onCounting {
		// Consumed rather than passed on. A stray letter here must not fall
		// through to the application and act on the list behind the panel.
		return m, true
	}

	if value, cursor, ok := line.Edit(m.fields[at].value, m.fields[at].cursor, msg); ok {
		m.fields[at].value, m.fields[at].cursor = value, cursor
		return m, true
	}
	return m, false
}

// moveFocus steps between fields, arriving fresh.
//
// Arriving is what clears a dismissal: esc means "not this field, this visit",
// so coming back later offers the list again.
func (m Model) moveFocus(by int) Model {
	visible := m.visible()
	m.focus = (m.focus + by + len(visible)) % len(visible)
	m.list = m.list.Arrive()
	m.warnings = nil
	return m
}

// clampFocus keeps the focus in range after a choice changes which fields exist.
func (m Model) clampFocus() int {
	if n := len(m.visible()); m.focus >= n {
		return n - 1
	}
	return m.focus
}

// Lines renders the panel for splicing into the list.
func (m Model) Lines() []string {
	if !m.open {
		return nil
	}
	visible := m.visible()
	head := "  new " + string(m.kind) + " "
	if pad := m.width - len([]rune(head)) - 2; pad > 0 {
		head += strings.Repeat("─", pad)
	}
	out := []string{style.Dim.Render(head)}

	// The dropdown, the warnings and the hint all sit UNDER THE FIELD they
	// belong to, spliced between the rows rather than collected at the bottom.
	//
	// They used to be drawn after every field, under a comment claiming they
	// were under the one they belonged to. With one completing field in the
	// panel the difference never showed; with three -- name, unit, category --
	// a list at the bottom is a list you have to work out the owner of.
	gutter := "  " + strings.Repeat(" ", 11) + " "
	for n, i := range visible {
		f := m.fields[i]
		label := style.Dim.Render(fmt.Sprintf("  %-11s ", f.label))
		if f.key == "counting" {
			out = append(out, label+m.countingLine())
		} else if n == m.focus {
			out = append(out, label+line.Render(f.value, f.cursor))
		} else {
			out = append(out, label+style.Strong.Render(f.value))
		}
		if n != m.focus {
			continue
		}
		switch {
		case m.list.IsOpen():
			out = append(out, m.list.Lines(gutter, m.width)...)
		case len(m.warnings) > 0:
			out = append(out, complete.Warnings(gutter, m.width, m.warnings)...)
		case f.hint != "":
			out = append(out, style.Dim.Render(gutter+f.hint))
		}
	}

	out = append(out, style.Dim.Render("  "+m.footerHint()))
	return out
}

// footerHint says what the keys do HERE, which depends on whether a list is up.
//
// Tab means two things by design -- take the highlighted option, or move to the
// next field -- and a footer that named only one of them would be wrong half
// the time.
func (m Model) footerHint() string {
	if m.list.IsOpen() {
		return complete.Hint()
	}
	return keys.Show(keys.Creator, keys.Complete) + " next   " +
		keys.Show(keys.Creator, keys.Confirm) + " continue   " +
		keys.Show(keys.Creator, keys.Cancel) + " discard"
}

// countingLine renders the three presets as a choice rather than a field.
//
// All three are always shown. A choice you can only see one option of is a
// field with legal values, and people type into those.
func (m Model) countingLine() string {
	parts := make([]string, 0, len(countings))
	for i, c := range countings {
		if i == m.counting {
			parts = append(parts, style.Strong.Render("(o) "+c.Label))
			continue
		}
		parts = append(parts, style.Dim.Render("( ) "+c.Label))
	}
	return strings.Join(parts, "  ")
}

func (m Model) View() string { return strings.Join(m.Lines(), "\n") }

// Height is how many lines the panel occupies.
func (m Model) Height() int { return len(m.Lines()) }
