package testing

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/ops"
	"home-management-system/internal/query"
	"home-management-system/internal/testsupport"
	"home-management-system/internal/tui"
)

// Width and Height are the terminal the Simulator starts in. Layout tests
// override them; everything else wants one predictable size.
const (
	Width  = 100
	Height = 30
)

// Simulator drives the real Model against a real database.
type Simulator struct {
	t     *testing.T
	ctx   context.Context
	conn  *sql.DB
	model tui.Model

	// pl and ex seed state through the operations layer rather than through
	// keystrokes. That is the legacy system's pragmatic choice and it is the
	// right one: otherwise every test is fifty keystrokes of setup, and the
	// keystrokes under test are lost in them.
	pl   *ops.Planner
	ex   *ops.Executor
	r    *query.Reader
	ctrl app.Controller
}

// New starts a Simulator on an empty, migrated database.
//
// The DSN comes from testsupport rather than being ":memory:" as the legacy
// harness had it. In this codebase that DSN is known-broken: database/sql
// pools, and each new connection to ":memory:" gets a FRESH, EMPTY database --
// so the schema silently disappears the moment a second connection opens.
func New(t *testing.T) *Simulator {
	t.Helper()
	conn := testsupport.NewDB(t)
	ctx := context.Background()

	// app.Open is the one place that knows how the write paths fit together, so
	// the harness reaches a Controller without importing any of them -- which
	// archlint requires of everything under internal/tui, harness included.
	ctrl := app.Open(conn)
	s := &Simulator{
		t: t, ctx: ctx, conn: conn, ctrl: ctrl,
		model: tui.New(ctx, ctrl),
		pl:    ops.NewPlanner(conn), ex: ops.New(conn), r: query.New(conn),
	}

	// H10 as a universal postcondition of every interface test.
	//
	// This is the single largest thing v02 gets that the legacy system had no
	// way to express: VerifyAll replays every Holding from its events and
	// compares against the stored projection, so putting it here makes
	//
	//	no sequence of keystrokes can produce a state the ledger cannot
	//	reproduce
	//
	// true of every test, including the ones written to check something else
	// entirely. No test author has to think of it.
	t.Cleanup(func() {
		report, err := ctrl.Integrity(s.ctx)
		if err != nil {
			t.Errorf("verify after the test: %v", err)
			return
		}
		if !report.Clean() {
			t.Errorf("the interface left a state the ledger cannot reproduce: %+v", report)
		}
	})

	s.resize(Width, Height)
	s.runCmd(s.model.Init())
	return s
}

// Planner and Executor seed state through the operations layer.
func (s *Simulator) Planner() *ops.Planner    { return s.pl }
func (s *Simulator) Executor() *ops.Executor  { return s.ex }
func (s *Simulator) Reader() *query.Reader    { return s.r }
func (s *Simulator) DB() *sql.DB              { return s.conn }
func (s *Simulator) Context() context.Context { return s.ctx }

// Apply runs a planned Batch, so a test can set up state in one line.
func (s *Simulator) Apply(b ops.Batch, err error) {
	s.t.Helper()
	if err != nil {
		s.t.Fatalf("seed: plan: %v", err)
	}
	if _, err := s.ex.Execute(s.ctx, b); err != nil {
		s.t.Fatalf("seed: execute: %v", err)
	}
}

// Send presses keys, one at a time, exactly as a person would.
func (s *Simulator) Send(script ...any) {
	s.t.Helper()
	for _, k := range Keys(script...) {
		s.update(s.model.Update(k.msg))
	}
}

// Resize changes the terminal, which is a real event the interface must handle
// and the only way to test what happens at 60 columns.
func (s *Simulator) Resize(width, height int) {
	s.t.Helper()
	s.resize(width, height)
}

func (s *Simulator) resize(width, height int) {
	s.update(s.model.Update(tea.WindowSizeMsg{Width: width, Height: height}))
}

// View is what a person would be looking at.
func (s *Simulator) View() string { return s.model.View() }

// Model exposes the model for the few assertions that legitimately need it --
// cursor position, mode depth. Anything about DATA should be asserted against
// the database instead, which is the whole point of the harness.
func (s *Simulator) Model() tui.Model { return s.model }

func (s *Simulator) update(model tea.Model, cmd tea.Cmd) {
	s.model = model.(tui.Model)
	s.runCmd(cmd)
}

// runCmd executes a command and feeds the result back in, expanding tea.BatchMsg
// so every command in a batch runs.
//
// The expansion is the second porting change, and v01's own test helper is
// missing it: internal/tui/tui_test.go ran cmd() once and handed the result
// straight to Update. A tea.BatchMsg would reach a model with no case for it and
// be SILENTLY DROPPED, as would any command the resulting Update returned. v01
// never noticed because it never batches. v02 does immediately -- loading a view
// and rebuilding the resolve index is naturally one batch -- so without this the
// symptom would be "the interface just doesn't refresh sometimes".
//
// Bubbletea's runtime intercepts BatchMsg before Update; this replicates that.
func (s *Simulator) runCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			s.runCmd(c)
		}
		return
	}
	if seq, ok := msg.(tea.QuitMsg); ok {
		_ = seq
		return
	}
	s.update(s.model.Update(msg))
}

// Two helpers from the legacy harness are deliberately NOT ported.
//
// Reload() re-ran Init() with the comment "This simulates what happens when
// user navigates". It was the harness papering over the application not
// refreshing, which means the tests stopped describing real behaviour. If a
// test needs data reloaded, it presses the key that reloads it.
//
// Wait() was a no-op stub. A no-op named after synchronisation is worse than no
// helper at all: it reads like a guarantee and provides none.

// RunCommand feeds a command through the Simulator's OWN loop, which is the
// only way to test that loop's BatchMsg expansion rather than a helper that
// happens to do the same thing.
//
// It exists because the first version of that test called a separate flatten()
// helper, and deleting the expansion from runCmd left the test passing.
func (s *Simulator) RunCommand(cmd tea.Cmd) { s.runCmd(cmd) }

// Contains reports whether the view holds text, which is the loosest useful
// assertion and the one worth having a helper for.
func (s *Simulator) Contains(text string) bool {
	return strings.Contains(s.View(), text)
}
