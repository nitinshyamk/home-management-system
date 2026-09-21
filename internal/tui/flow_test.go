package tui

import (
	"context"
	"testing"

	"home-management-system/internal/tui/editor"
)

// TestASettleEndsWhetherOrNotItSucceeded is the invariant the flow type exists
// to make unrepresentable.
//
// settling used to be a bare int cleared only on the two success paths, so
// escaping a field or panel opened for row 3 left the index behind: the model
// said "row 3 is being settled" while nothing on screen agreed, and the next
// applied change anywhere would have been routed into rebinding that row.
func TestASettleEndsWhetherOrNotItSucceeded(t *testing.T) {
	f := newFlow()
	f.active = true

	if _, settling := f.settlingRow(); settling {
		t.Fatal("a fresh import claims to be settling a row")
	}

	f = f.opened(3)
	at, settling := f.settlingRow()
	if !settling || at != 3 {
		t.Fatalf("settlingRow() = %d, %v; want 3, true", at, settling)
	}

	// The one that used to be missing: the cancel path.
	if _, settling := f.settled().settlingRow(); settling {
		t.Error("a settle that ended still claims a row")
	}
}

// TestNoImportIsNotSettlingAnything: the zero value is "no import", and it must
// not read as "settling row 0".
func TestNoImportIsNotSettlingAnything(t *testing.T) {
	var f flow
	if f.reviewing() {
		t.Error("the zero flow claims to be reviewing an import")
	}
	if _, settling := f.settlingRow(); settling {
		t.Error("the zero flow claims to be settling a row")
	}
	if _, settling := newFlow().done().settlingRow(); settling {
		t.Error("a finished import still claims to be settling a row")
	}
}

// TestCancellingAPromptEndsItsSettle pins the CALL SITE, which is where the bug
// was -- flow.settled() existing is not the same as the cancel paths calling it.
//
// There is no behavioural test for this, and there cannot be a useful one: the
// only way to reach an applied change while an import is on screen is to open a
// panel, and opening one resets the row anyway. That is why it survived. What
// is checkable is the state the model is left in, so that is what is checked.
func TestCancellingAPromptEndsItsSettle(t *testing.T) {
	for _, c := range []struct {
		what string
		open func(Model) Model
	}{
		{"field", func(m Model) Model {
			m.editor = m.editor.OpenFor(editor.Row, "", 3, "command", "acquire Cardamom 50")
			return m
		}},
		{"panel", func(m Model) Model {
			m.creator = m.creator.Open("item", "Cardamom")
			return m
		}},
	} {
		t.Run(c.what, func(t *testing.T) {
			m := New(context.Background(), &fakeController{})
			m = m.withImport()
			m = c.open(m).withSettling(3)

			if _, settling := m.flow().settlingRow(); !settling {
				t.Fatal("opening for a row did not record the row")
			}

			next, _ := m.Update(keyMsg("esc"))
			m = next.(Model)

			if at, settling := m.flow().settlingRow(); settling {
				t.Errorf("escaping the %s left row %d claiming to be settling", c.what, at)
			}
		})
	}
}

// withImport is the test's stand-in for a plan on screen, which otherwise
// needs a file on disk to reach.
func (m Model) withImport() Model { return m.open(flow{active: true, settling: noRow}) }

// withSettling is the test's stand-in for the keystroke that opens a panel FOR
// a plan row, which needs a real import to reach.
func (m Model) withSettling(at int) Model { return m.withFlow(m.flow().opened(at)) }
