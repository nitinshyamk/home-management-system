package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/table"
)

// fakeController stands in for the real one. The Controller being a plain Go
// interface is what makes this three lines instead of a mocking framework —
// the practical payoff of not using the previous system's CQRS message layer.
type fakeController struct {
	historyFor domain.HoldingID
	calls      []string

	// What the write surface was asked to do, and what it should answer.
	boundLine    string
	boundSubject command.Subject
	bindResult   command.BindResult
	bindErr      error
	plan         app.Plan
	planErr      error
	applyErr     error
}

func (f *fakeController) CategoryTree(_ context.Context, withContents bool) ([]app.TreeRow, error) {
	f.calls = append(f.calls, "CategoryTree")
	rows := []app.TreeRow{
		{ID: 1, Name: "Spices", Depth: 0, Count: 3, Kind: "Category"},
		{ID: 2, Name: "Dried Peppers", Depth: 1, Count: 1, Kind: "Category"},
	}
	if withContents {
		// Item 1 shares a number with Category 1 on purpose: they are numbered
		// from different tables, and the tree has to tell them apart.
		rows = append(rows, app.TreeRow{
			ID: 1, Name: "Ancho Chile", Depth: 2, Kind: "Item", Measure: "280 g",
		})
	}
	return rows, nil
}

func (f *fakeController) LocationTree(_ context.Context, withContents bool) ([]app.TreeRow, error) {
	f.calls = append(f.calls, "LocationTree")
	rows := []app.TreeRow{{ID: 1, Name: "Kitchen", Depth: 0, Count: 4, Kind: "Location"}}
	if withContents {
		rows = append(rows, app.TreeRow{
			ID: 9, Name: "Basmati Rice", Depth: 1, Kind: "Holding", Measure: "800 g",
		})
	}
	return rows, nil
}

func (f *fakeController) Items(context.Context) ([]app.ItemRow, error) {
	f.calls = append(f.calls, "Items")
	return []app.ItemRow{
		{ID: 1, Name: "Basmati Rice", Kind: "Bulk", Category: "Pantry", CategoryID: 1, Measure: "g", OnHand: "4800 g"},
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

// Units is the reference table, which is the same seven codes everywhere.
func (f *fakeController) Units(context.Context) ([]string, error) {
	return []string{"count", "g", "kg", "ml", "l", "cm", "m"}, nil
}

// The write surface. The fake records what it was ASKED to do and does none of
// it, which is what lets TestTheUIMakesNoWrites assert on the calls a browse
// keystroke did not make. Anything that actually writes uses the Simulator.
func (f *fakeController) Vocabulary(context.Context) (*command.Vocabulary, error) {
	f.calls = append(f.calls, "Vocabulary")
	return command.NewVocabulary(resolve.NewIndex(nil), nil, nil, nil), nil
}

func (f *fakeController) BindLine(_ context.Context, line string, subject command.Subject) (command.BindResult, error) {
	f.calls = append(f.calls, "BindLine")
	f.boundLine, f.boundSubject = line, subject
	if f.bindResult.Command != nil || len(f.bindResult.Issues) > 0 {
		return f.bindResult, f.bindErr
	}
	return command.BindResult{}, f.bindErr
}

func (f *fakeController) PlanCommand(context.Context, command.Command) (app.Plan, error) {
	f.calls = append(f.calls, "PlanCommand")
	return f.plan, f.planErr
}

func (f *fakeController) PlanAll(_ context.Context, commands []command.Command) (app.Plan, int, error) {
	f.calls = append(f.calls, "PlanAll")
	return f.plan, -1, f.planErr
}

func (f *fakeController) ApplyPlan(context.Context, app.Plan) error {
	f.calls = append(f.calls, "ApplyPlan")
	return f.applyErr
}

func (f *fakeController) Describe(_ context.Context, cmd command.Command) string {
	return command.Summary(cmd, nil)
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

// keyMsg names keys the way the keymap names them, so a test cannot press a
// key the interface does not bind.
func keyMsg(key string) tea.KeyMsg {
	msg, ok := keys.Named(key)
	if !ok {
		panic(key + " is not a key")
	}
	return msg
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
	// Move to the second holding, then open it. The table widget is where
	// motion lives, so C-n reaches it before the application sees it.
	m, view := drive(t, fake, "ctrl+n", "enter")

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
	m, _ := drive(t, &fakeController{}, "enter", "esc")
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
	// Asserted by the row it ENDS ON rather than by an index, because the row
	// is what a person sees and what the next keystroke will act on. There are
	// two holdings; far more downs than that must stop on the second, and far
	// more ups on the first.
	onRow := func(m Model) string {
		sel, ok := m.current.Current()
		if !ok {
			t.Fatal("cursor is on no row at all")
		}
		return sel.Name
	}

	m, _ := drive(t, &fakeController{}, "ctrl+n", "ctrl+n", "ctrl+n", "ctrl+n")
	if got := onRow(m); got != "USB-C Cable" {
		t.Errorf("cursor on %q, want the last row", got)
	}
	m, _ = drive(t, &fakeController{}, "ctrl+p", "ctrl+p")
	if got := onRow(m); got != "Basmati Rice" {
		t.Errorf("cursor on %q, want the first row", got)
	}
}

// TestTheUIMakesNoWrites is the v01 scope stated as a test.
func TestTheUIMakesNoWrites(t *testing.T) {
	fake := &fakeController{}
	drive(t, fake, "1", "2", "3", "4", "5", "enter", "esc", "ctrl+n", "s", "ctrl+space", "g")

	for _, call := range fake.calls {
		switch call {
		case "CreateCategory", "RenameCategory", "ReparentCategory", "ArchiveCategory":
			t.Errorf("the read-only UI called %s", call)
		}
	}
}

// TestThePathSeparatorsAgree pins the one constant this interface declares
// twice.
//
// The table draws breadcrumbs and the resolver accepts them, and they have to
// be written the same way or a path the screen shows is a path nothing matches.
// They cannot share a declaration: resolve reaches the database through query,
// and a table widget that imported it would drag the storage layer into a
// drawing routine. This package imports both, so it is where they can be
// compared.
func TestThePathSeparatorsAgree(t *testing.T) {
	if table.PathSeparator != resolve.PathSeparator {
		t.Errorf("table draws paths with %q and the resolver reads them with %q",
			table.PathSeparator, resolve.PathSeparator)
	}
}
