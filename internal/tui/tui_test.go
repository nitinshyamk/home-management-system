package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/domain"
	"home-management-system/internal/resolve"
)

// fakeController stands in for the real one. The Controller being a plain Go
// interface is what makes this three lines instead of a mocking framework —
// the practical payoff of not using the previous system's CQRS message layer.
type fakeController struct {
	historyFor domain.HoldingID
	calls      []string
}

func (f *fakeController) CategoryTree(context.Context) ([]app.TreeRow, error) {
	f.calls = append(f.calls, "CategoryTree")
	return []app.TreeRow{
		{ID: 1, Name: "Spices", Depth: 0, Count: 3},
		{ID: 2, Name: "Dried Peppers", Depth: 1, Count: 1},
	}, nil
}

func (f *fakeController) LocationTree(context.Context) ([]app.TreeRow, error) {
	f.calls = append(f.calls, "LocationTree")
	return []app.TreeRow{{ID: 1, Name: "Kitchen", Depth: 0, Count: 4}}, nil
}

func (f *fakeController) Items(context.Context) ([]app.ItemRow, error) {
	f.calls = append(f.calls, "Items")
	return []app.ItemRow{
		{ID: 1, Name: "Basmati Rice", Kind: "Bulk", Category: "Pantry", Measure: "g", OnHand: "4800 g"},
	}, nil
}

func (f *fakeController) Holdings(context.Context) ([]app.HoldingRow, error) {
	f.calls = append(f.calls, "Holdings")
	return []app.HoldingRow{
		{ID: 7, Item: "Basmati Rice", Kind: "Bulk", Location: "Shelf 1", State: "800 g"},
		{ID: 9, Item: "USB-C Cable", Kind: "Unique", Location: "Drawer 2", State: "out at 3", Note: "out 21d"},
	}, nil
}

func (f *fakeController) HoldingHistory(_ context.Context, id domain.HoldingID) ([]app.EventRow, error) {
	f.historyFor = id
	f.calls = append(f.calls, "HoldingHistory")
	return []app.EventRow{
		{Sequence: 1, When: time.Now(), Type: "HoldingCreated", Summary: "put at location 4"},
		{Sequence: 5, When: time.Now(), Type: "Acquired", Summary: "acquired 2000 from shop"},
	}, nil
}

func (f *fakeController) Integrity(context.Context) (app.IntegrityRow, error) {
	f.calls = append(f.calls, "Integrity")
	return app.IntegrityRow{
		HoldingsChecked: 2,
		Discrepancies:   []string{"holding 7: quantity is 999, ledger says 800"},
	}, nil
}

func (f *fakeController) Nudges(context.Context) ([]app.NudgeRow, error) {
	return []app.NudgeRow{{Item: "Cumin", Category: "Spices", Siblings: 1}}, nil
}

// SearchIndex returns a small vocabulary so the jump palette has something to
// find. The widget tests use the fake because they cross no boundary; anything
// that does uses the Simulator.
func (f *fakeController) SearchIndex(context.Context) (*resolve.Index, error) {
	return resolve.NewIndex([]resolve.Candidate{
		{Kind: resolve.KindLocation, ID: 1, Path: "Kitchen > Left Pantry", Leaf: "Left Pantry"},
		{Kind: resolve.KindLocation, ID: 2, Path: "Kitchen > Left Pantry > Shelf 1", Leaf: "Shelf 1"},
		{Kind: resolve.KindItem, ID: 3, Path: "Grains > Basmati Rice", Leaf: "Basmati Rice"},
		{Kind: resolve.KindHolding, ID: 9, Path: "Basmati Rice > Shelf 1 (loose)", Leaf: "Basmati Rice"},
	}), nil
}

func (f *fakeController) CreateCategory(context.Context, string, *domain.CategoryID) (domain.CategoryID, error) {
	f.calls = append(f.calls, "CreateCategory")
	return 0, nil
}
func (f *fakeController) RenameCategory(context.Context, domain.CategoryID, string) error {
	f.calls = append(f.calls, "RenameCategory")
	return nil
}
func (f *fakeController) ReparentCategory(context.Context, domain.CategoryID, *domain.CategoryID) error {
	f.calls = append(f.calls, "ReparentCategory")
	return nil
}
func (f *fakeController) ArchiveCategory(context.Context, domain.CategoryID, domain.Resolution, *domain.CategoryID) error {
	f.calls = append(f.calls, "ArchiveCategory")
	return nil
}

// drive runs a model through a size message, an initial load, and the given
// keys, returning the rendered view.
func drive(t *testing.T, ctrl app.Controller, keys ...string) (Model, string) {
	t.Helper()
	m := New(context.Background(), ctrl)

	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = next.(Model)
	m = run(t, m, m.Init())

	for _, key := range keys {
		next, cmd := m.Update(keyMsg(key))
		m = next.(Model)
		m = run(t, m, cmd)
	}
	return m, m.View()
}

// run executes a command and folds the resulting message back in.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	if err, ok := msg.(errMsg); ok {
		t.Fatalf("controller error: %v", err.err)
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+n":
		return tea.KeyMsg{Type: tea.KeyCtrlN}
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "ctrl+b":
		return tea.KeyMsg{Type: tea.KeyCtrlB}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

func TestOpensOnHoldings(t *testing.T) {
	_, view := drive(t, &fakeController{})

	for _, want := range []string{"Basmati Rice", "800 g", "USB-C Cable", "out 21d"} {
		if !strings.Contains(view, want) {
			t.Errorf("view is missing %q:\n%s", want, view)
		}
	}
}

func TestNumberKeysSwitchViews(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"1", "Dried Peppers"},
		{"2", "Kitchen"},
		{"3", "Basmati Rice"},
		{"4", "USB-C Cable"},
		{"5", "holdings checked"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			_, view := drive(t, &fakeController{}, tc.key)
			if !strings.Contains(view, tc.want) {
				t.Errorf("view %s is missing %q:\n%s", tc.key, tc.want, view)
			}
		})
	}
}

// TestEnterOpensTheSelectedHoldingsHistory is what the whole model was built
// for: a Holding's events, in sequence order, reconstructed from the ledger.
func TestEnterOpensTheSelectedHoldingsHistory(t *testing.T) {
	fake := &fakeController{}
	// Move to the second holding, then open it.
	// j rather than the Emacs C-n v01 used: 10a adopts the plan's vim-like
	// bindings, and the table widget is where motion now lives.
	m, view := drive(t, fake, "j", "enter")

	if m.view != viewHistory {
		t.Fatalf("view = %v, want history", m.view)
	}
	if fake.historyFor != 9 {
		t.Errorf("history requested for holding %d, want 9 — the cursor and the id list disagree",
			fake.historyFor)
	}
	for _, want := range []string{"HoldingCreated", "acquired 2000 from shop"} {
		if !strings.Contains(view, want) {
			t.Errorf("history view is missing %q:\n%s", want, view)
		}
	}
}

func TestBackReturnsFromHistory(t *testing.T) {
	m, _ := drive(t, &fakeController{}, "enter", "ctrl+b")
	if m.view != viewHoldings {
		t.Errorf("view = %v, want holdings", m.view)
	}
}

// TestIntegrityViewSaysItDidNotRepair: the report is a defect report, and the
// UI must not imply anything was fixed.
func TestIntegrityViewReportsWithoutRepairing(t *testing.T) {
	_, view := drive(t, &fakeController{}, "5")

	if !strings.Contains(view, "DISCREPANCY") {
		t.Errorf("integrity view does not surface the discrepancy:\n%s", view)
	}
	if !strings.Contains(view, "reported, not repaired") {
		t.Errorf("integrity view does not say it left the state alone:\n%s", view)
	}
	if !strings.Contains(view, "Cumin") {
		t.Errorf("integrity view is missing the classification nudge:\n%s", view)
	}
}

func TestCursorStaysInBounds(t *testing.T) {
	// Far more downs than rows, then far more ups.
	m, _ := drive(t, &fakeController{}, "j", "j", "j", "j")
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (two rows)", m.cursor)
	}
	m, _ = drive(t, &fakeController{}, "k", "k")
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
}

// TestTheUIMakesNoWrites is the v01 scope stated as a test.
func TestTheUIMakesNoWrites(t *testing.T) {
	fake := &fakeController{}
	drive(t, fake, "1", "2", "3", "4", "5", "enter", "ctrl+b", "j", "s", " ", "r")

	for _, call := range fake.calls {
		switch call {
		case "CreateCategory", "RenameCategory", "ReparentCategory", "ArchiveCategory":
			t.Errorf("the read-only UI called %s", call)
		}
	}
}
