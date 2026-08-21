package creator_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/creator"
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
	tab   = tea.KeyMsg{Type: tea.KeyTab}
	left  = tea.KeyMsg{Type: tea.KeyLeft}
	right = tea.KeyMsg{Type: tea.KeyRight}
)

// The panel builds a `:` LINE, not a Command. Everything the interface writes
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

// Typing a letter on the counting field must not vanish. h and l choose,
// matching the motion keys everywhere else.
func TestLettersOnTheChoiceMoveRatherThanDisappear(t *testing.T) {
	m := creator.New().Open(creator.KindItem, "")
	m = press(m, tab)
	before := m.Counting()
	m = press(m, typed("h")...)
	if m.Counting() == before {
		t.Error("h did not move the choice")
	}
	m = press(m, typed("l")...)
	if m.Counting() != before {
		t.Error("l did not move it back")
	}
	// And nothing was typed into a value.
	if strings.Contains(m.Line(), "hl") {
		t.Errorf("letters landed in a field: %q", m.Line())
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
