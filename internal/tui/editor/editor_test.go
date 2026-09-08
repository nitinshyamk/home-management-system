package editor_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/keys"
)

func typed(m editor.Model, s string) editor.Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func press(m editor.Model, name string) editor.Model {
	msg, ok := keys.Named(name)
	if !ok {
		panic("no key called " + name)
	}
	m, _ = m.Update(msg)
	return m
}

// TestOpeningPrefillsAndPutsTheCursorAtTheEnd: a rename is nearly always a
// correction rather than a replacement, so blanking the field would make the
// common case the expensive one.
func TestOpeningPrefillsAndPutsTheCursorAtTheEnd(t *testing.T) {
	m := editor.New().Open("Item", 7, "rename", "Basmati Rice")

	if !m.IsOpen() {
		t.Fatal("Open did not open the field")
	}
	if m.Value() != "Basmati Rice" {
		t.Errorf("Value = %q, want the current name", m.Value())
	}
	if !m.AtEnd() {
		t.Error("the cursor is not at the end, so typing would insert mid-name")
	}
	if m.Changed() {
		t.Error("a freshly opened field reports a change nobody made")
	}
}

// TestChangedIgnoresSurroundingSpace: "unchanged" has to mean unchanged, and a
// trailing space is not an edit anyone made on purpose.
func TestChangedIgnoresSurroundingSpace(t *testing.T) {
	m := typed(editor.New().Open("Item", 7, "rename", "Rice"), "  ")
	if m.Changed() {
		t.Errorf("Value %q reports as changed from %q", m.Value(), "Rice")
	}
	if m = typed(m, "y"); !m.Changed() {
		t.Error("a real edit does not report as changed")
	}
}

// TestCloseKeepsWhatWasTyped: escaping a confirmation returns to the field with
// the answer still in it, which is what makes the confirmation a step rather
// than a dead end.
func TestCloseKeepsWhatWasTyped(t *testing.T) {
	m := typed(editor.New().Open("Item", 7, "rename", ""), "Cardamom").Close()
	if m.IsOpen() {
		t.Error("Close did not close the field")
	}
	if m.Value() != "Cardamom" {
		t.Errorf("Value = %q, want what was typed to survive", m.Value())
	}
}

// TestThePurposeAndSubjectSurviveTheEdit: the caller builds the command from the
// editor rather than from parallel state that could drift, so the editor has to
// still know what it was opened for after a keystroke.
func TestThePurposeAndSubjectSurviveTheEdit(t *testing.T) {
	m := typed(editor.New().OpenFor(editor.Move, "Holding", 42, "where to", ""), "Shelf 1")

	if m.Purpose() != editor.Move {
		t.Errorf("Purpose = %q, want move", m.Purpose())
	}
	if m.Subject() != 42 || m.Kind() != "Holding" {
		t.Errorf("subject = %d/%q, want 42/Holding", m.Subject(), m.Kind())
	}
}

// TestTheVerbIsTheCallersWord, not the purpose. It used to be the purpose: the
// widget's switch fell through and returned the identifier, so the word on
// screen was the constant.
func TestTheVerbIsTheCallersWord(t *testing.T) {
	m := editor.New().OpenFor(editor.Consume, "Holding", 1, "how much", "").WithVerb("consume")
	if got := strings.Join(m.Lines(), "\n"); !strings.Contains(got, "consume") {
		t.Errorf("the field does not offer to consume:\n%s", got)
	}

	// And a field never told otherwise says something neutral rather than "".
	plain := editor.New().OpenFor(editor.Rename, "Item", 1, "rename", "")
	if got := strings.Join(plain.Lines(), "\n"); !strings.Contains(got, "confirm") {
		t.Errorf("a field with no verb says nothing useful:\n%s", got)
	}
}

// TestSuggestionsAreOfferedAndNeverApplied: a completion that filled itself in
// would be the resolver deciding, which is the one thing it must never do.
func TestSuggestionsAreOfferedAndNeverApplied(t *testing.T) {
	m := typed(editor.New().OpenFor(editor.Move, "Holding", 1, "where to", ""), "sh")
	m = m.SetSuggestions([]string{"Shelf 1", "Shelf 2"})

	if got := m.Suggestions(); len(got) != 2 {
		t.Fatalf("Suggestions = %v, want both shelves", got)
	}
	if m.Value() != "sh" {
		t.Errorf("offering a completion changed the value to %q", m.Value())
	}

	// Taking one is a keystroke.
	m = press(m, "tab")
	if m.Value() == "sh" {
		t.Error("tab did not take the highlighted completion")
	}
}

// TestClosedFieldDrawsNothing, which is what lets the caller render it
// unconditionally.
func TestClosedFieldDrawsNothing(t *testing.T) {
	if lines := editor.New().Lines(); len(lines) != 0 {
		t.Errorf("a closed field drew %d lines: %v", len(lines), lines)
	}
	if h := editor.New().Height(); h != 0 {
		t.Errorf("a closed field claims %d lines of height", h)
	}
}
