package creator_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/command"
	"home-management-system/internal/tui/creator"
	"home-management-system/internal/tui/keys"
)

func press(m creator.Model, keys ...tea.KeyMsg) creator.Model {
	for _, k := range keys {
		m, _ = m.Update(k)
	}
	return m
}

func typed(s string) []tea.KeyMsg {
	var out []tea.KeyMsg
	for _, r := range s {
		if r == ' ' {
			out = append(out, tea.KeyMsg{Type: tea.KeySpace})
			continue
		}
		out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return out
}

var (
	tab    = named("tab")
	escape = named("esc")
	left   = named("ctrl+b")
	right  = named("ctrl+f")
)

func named(name string) tea.KeyMsg {
	msg, ok := keys.Named(name)
	if !ok {
		panic(name + " is not a key")
	}
	return msg
}

// The panel builds a COMMAND LINE, not a Command. Everything the interface writes
// goes through Parse and Bind, so the panel cannot validate differently from
// the typed line or from a CSV row.
func TestThePanelBuildsACommandLine(t *testing.T) {
	m := creator.New().Open(creator.KindItem, "")
	m = press(m, typed("Turmeric")...)
	m = press(m, tab, tab)      // past counting, onto unit
	m = press(m, typed("g")...) //
	m = press(m, tab)           // per package
	m = press(m, typed("2000")...)
	m = press(m, tab) // category
	m = press(m, typed("Spices")...)

	want := `new item Turmeric counting measured unit g package 2000 category Spices`
	if got := m.Line(); got != want {
		t.Errorf("line =\n %q\nwant\n %q", got, want)
	}
}

// A name with a space has to survive as one value, or half of every house is
// unnameable.
func TestANameWithASpaceIsQuoted(t *testing.T) {
	m := creator.New().Open(creator.KindLocation, "Kitchen")
	m = press(m, typed("Left Pantry")...)
	if got := m.Line(); !strings.Contains(got, `new location "Left Pantry"`) {
		t.Errorf("line = %q", got)
	}
	// And the parent came from what the cursor was on.
	if !strings.Contains(m.Line(), "under Kitchen") {
		t.Errorf("line = %q, want the cursor's node as the parent", m.Line())
	}
}

// The measured fields appear BECAUSE of the choice. A unit box that is always
// visible and sometimes meaningless is the nullable-column muddle this project
// spent months eliminating, drawn on a screen.
func TestTheMeasuredFieldsFollowTheChoice(t *testing.T) {
	m := creator.New().Open(creator.KindItem, "")
	m = press(m, typed("Hammer")...)
	m = press(m, tab) // onto counting

	measured := strip(m.View())
	if !strings.Contains(measured, "unit") || !strings.Contains(measured, "per package") {
		t.Errorf("measured is chosen and the fields are missing:\n%s", measured)
	}

	m = press(m, left, left) // one of a kind
	unique := strip(m.View())
	if strings.Contains(unique, "unit") || strings.Contains(unique, "per package") {
		t.Errorf("one of a kind still shows measured fields:\n%s", unique)
	}
	if !strings.Contains(m.Line(), "counting unique") {
		t.Errorf("line = %q", m.Line())
	}
	// And nothing measured leaks into the line.
	if strings.Contains(m.Line(), "unit") {
		t.Errorf("a one-of-a-kind line carries a unit: %q", m.Line())
	}
}

// All three presets are always shown. A choice you can see only one option of
// is a field with legal values, and people type into those.
func TestAllThreePresetsAreVisible(t *testing.T) {
	m := creator.New().Open(creator.KindItem, "")
	view := strip(press(m, tab).View())
	for _, label := range []string{"one of a kind", "a pile I count", "something I measure"} {
		if !strings.Contains(view, label) {
			t.Errorf("the panel does not offer %q:\n%s", label, view)
		}
	}
	// Exactly one is chosen.
	if got := strings.Count(view, "(o)"); got != 1 {
		t.Errorf("%d presets are marked chosen", got)
	}
}

// The counting field is a choice, not a field, so the motion keys move between
// the three answers -- and a letter, which has nowhere to go, goes nowhere.
//
// It used to be h and l that chose, and they had to, because h and l were the
// motion keys. Now that motion is C-b and C-f, a letter here is just a letter:
// what matters is that it is SWALLOWED rather than typed into a value nobody
// can see, which is how it behaved before either binding existed.
func TestTheChoiceMovesAndSwallowsWhatItCannotUse(t *testing.T) {
	m := creator.New().Open(creator.KindItem, "")
	m = press(m, tab)
	before := m.Counting()
	m = press(m, left)
	if m.Counting() == before {
		t.Error("C-b did not move the choice")
	}
	m = press(m, right)
	if m.Counting() != before {
		t.Error("C-f did not move it back")
	}
	// And a letter did not land in a value.
	m = press(m, typed("hl")...)
	if strings.Contains(m.Line(), "hl") {
		t.Errorf("letters landed in a field: %q", m.Line())
	}
	if m.Counting() != before {
		t.Error("a letter moved the choice")
	}
}

// Tab wraps rather than sticking, so there is no dead end at the last field.
func TestTabWrapsThroughTheVisibleFields(t *testing.T) {
	m := creator.New().Open(creator.KindCategory, "")
	first := strip(m.View())
	m = press(m, tab, tab, tab)
	if got := strip(m.View()); got != first {
		t.Errorf("three tabs through three fields did not return to the start:\n%s\n%s", first, got)
	}
}

// Fewer fields for simpler things. A Location has a name, a parent, and a
// description -- if the panel showed counting greyed out it would be one panel
// pretending rather than one panel adapting.
func TestSimplerThingsGetSimplerPanels(t *testing.T) {
	for _, kind := range []creator.Kind{creator.KindCategory, creator.KindLocation} {
		view := strip(creator.New().Open(kind, "").View())
		if strings.Contains(view, "counting") {
			t.Errorf("a %s panel offers a counting choice:\n%s", kind, view)
		}
		if !strings.Contains(view, "under") {
			t.Errorf("a %s panel has no parent field:\n%s", kind, view)
		}
	}
}

// A panel that remembered an abandoned attempt will eventually create it.
func TestOpeningStartsClean(t *testing.T) {
	m := creator.New().Open(creator.KindItem, "")
	m = press(m, typed("Abandoned")...)
	m = press(m, tab, left) // and a different preset
	m = m.Close()

	fresh := m.Open(creator.KindItem, "")
	if got := fresh.Value("name"); got != "" {
		t.Errorf("a fresh panel remembers %q", got)
	}
	if got := fresh.Counting(); got != "measured" {
		t.Errorf("a fresh panel remembers the preset %q", got)
	}
}

func strip(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var _ = right

// ---------------------------------------------------------------------------
// Autocomplete
// ---------------------------------------------------------------------------

// The panel says what it WANTS and the caller answers from the resolve index.
// It does not match names itself, so the panel and the command line cannot
// disagree about what a name nearly is.
func TestThePanelSaysWhichFieldNeedsAName(t *testing.T) {
	m := creator.New().Open(creator.KindItem, "")
	if kind, _ := m.Resolving(); kind != "" {
		t.Errorf("the name field asked to resolve a %q", kind)
	}

	m = press(m, tab, tab, tab, tab) // counting, unit, package, category
	kind, typed := m.Resolving()
	if kind != "Category" {
		t.Errorf("the category field resolves %q, want Category", kind)
	}
	if typed != "" {
		t.Errorf("typed = %q", typed)
	}
	m = press(m, typed2("spic")...)
	if _, got := m.Resolving(); got != "spic" {
		t.Errorf("typed = %q", got)
	}
}

// A Location's parent resolves Locations, a Category's resolves Categories.
// Completing a place against classifications would offer names that cannot
// possibly be right.
func TestEachParentFieldResolvesItsOwnKind(t *testing.T) {
	for _, tc := range []struct {
		kind creator.Kind
		want string
	}{
		{creator.KindCategory, "Category"},
		{creator.KindLocation, "Location"},
	} {
		m := press(creator.New().Open(tc.kind, ""), tab)
		if got, _ := m.Resolving(); got != tc.want {
			t.Errorf("a %s parent resolves %q, want %q", tc.kind, got, tc.want)
		}
	}
}

// Offered, never applied. A completion that filled itself in would be the
// resolver deciding, which is the one thing it must never do.
func TestASuggestionIsOfferedNotApplied(t *testing.T) {
	m := press(creator.New().Open(creator.KindLocation, ""), tab)
	m = press(m, typed2("gar")...)
	m = m.SetSuggestions([]string{"Garage", "Garage > Tool Bench"})

	if got := m.Value("under"); got != "gar" {
		t.Errorf("the suggestion applied itself: %q", got)
	}
	view := strip(m.View())
	if !strings.Contains(view, "Garage > Tool Bench") {
		t.Errorf("the alternatives are not shown:\n%s", view)
	}
	if !strings.Contains(view, "TAB to take it") {
		t.Errorf("nothing says how to take it:\n%s", view)
	}
}

// Tab completes before it moves, which is what a terminal has always done.
// Moving first would mean the only way to take a suggestion is a key nobody
// would guess.
func TestTabTakesTheSuggestionThenMovesOn(t *testing.T) {
	m := press(creator.New().Open(creator.KindLocation, ""), tab)
	m = press(m, typed2("gar")...)
	m = m.SetSuggestions([]string{"Garage"})

	m = press(m, tab)
	if got := m.Value("under"); got != "Garage" {
		t.Errorf("tab did not take the suggestion: %q", got)
	}
	// Focus did not move, because tab was spent taking it.
	if kind, _ := m.Resolving(); kind != "Location" {
		t.Error("tab both took the suggestion and moved on")
	}
	// A second tab moves, because there is nothing left to take.
	m = press(m, tab)
	if kind, _ := m.Resolving(); kind != "" {
		t.Errorf("the second tab did not move on; still on a %q field", kind)
	}
}

// With nothing on offer, tab is just tab.
func TestTabMovesWhenThereIsNothingToTake(t *testing.T) {
	m := press(creator.New().Open(creator.KindLocation, ""), tab)
	m = press(m, typed2("gar")...)
	before, _ := m.Resolving()
	m = press(m, tab)
	if after, _ := m.Resolving(); after == before {
		t.Error("tab did not move with no suggestions")
	}
}

// Moving fields starts the completions over: a list put away on one field is not
// put away on the next.
//
// This used to press the down arrow with a list open and expect a field change.
// It does not any more -- the arrows navigate a dropdown wherever one is open,
// which is the whole reason a dropdown has a highlight -- so the move here is
// made with the list already dismissed, which is the only state in which any key
// still means "next field".
func TestSuggestionsDoNotOutliveTheirField(t *testing.T) {
	m := press(creator.New().Open(creator.KindLocation, ""), tab)
	m = m.SetSuggestions([]string{"Garage"})
	if got := m.Suggestions(); len(got) != 1 {
		t.Fatalf("nothing was on offer to begin with: %v", got)
	}

	m = press(m, escape)
	if got := m.SetSuggestions([]string{"Garage"}).Suggestions(); len(got) != 0 {
		t.Errorf("a dismissed list came back on the same field: %v", got)
	}

	m = press(m, tea.KeyMsg{Type: tea.KeyDown})
	if got := m.SetSuggestions([]string{"Kitchen"}).Suggestions(); len(got) != 1 {
		t.Errorf("the next field inherited the last one's dismissal: %v", got)
	}
}

func typed2(s string) []tea.KeyMsg { return typed(s) }

// TestANameThatSpellsAFieldKeySurvivesTheParser is the case the panel's own
// quoting rule missed.
//
// A value that spells one of its command's field keys has to be quoted, or
// Parse reads it as the start of a pair rather than as the name -- so the panel
// showed one command and produced another. These are not contrived: "notes",
// "package" and "unit" are field keys of `new item` and ordinary things to call
// a thing you keep, and "name" and "under" are field keys of `new category`.
func TestANameThatSpellsAFieldKeySurvivesTheParser(t *testing.T) {
	cases := []struct {
		kind  creator.Kind
		names []string
	}{
		{creator.KindItem, []string{"notes", "package", "unit", "category", "Notes"}},
		{creator.KindCategory, []string{"under", "name", "describe"}},
		{creator.KindLocation, []string{"under", "name", "describe"}},
	}
	for _, c := range cases {
		for _, name := range c.names {
			m := press(creator.New().Open(c.kind, ""), typed(name)...)
			line := m.Line()

			raw, err := command.Parse(line)
			if err != nil {
				t.Fatalf("%s %q: the panel produced a line that does not parse: %v\n%s",
					c.kind, name, err, line)
			}
			if got := raw.Fields["name"]; got != name {
				t.Errorf("%s %q came back as %q from: %s", c.kind, name, got, line)
			}
		}
	}
}
