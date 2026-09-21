package tui

import (
	"context"
	"testing"

	"home-management-system/internal/tui/omnibox"
)

// TestBrowsingIsTheAbsenceOfEveryMode. There is no flag for "browsing" -- it is
// what is left when nothing is over the list, which is exactly why the modes
// need one place to be counted.
func TestBrowsingIsTheAbsenceOfEveryMode(t *testing.T) {
	m, _ := drive(t, &fakeController{})

	for _, l := range m.layers() {
		if l.active {
			t.Errorf("nothing has been opened, but %q is active", l.name)
		}
	}
	if got := m.mode(); got != "browsing" {
		t.Errorf("mode = %q, want browsing", got)
	}
}

// TestTheTopLayerIsTheInnermostOpenOne, which is the whole of the precedence
// rule and used to live only in the order of six if statements.
func TestTheTopLayerIsTheInnermostOpenOne(t *testing.T) {
	base := New(context.Background(), &fakeController{})

	cases := []struct {
		name string
		open func(Model) Model
		want string
	}{
		{"input line", func(m Model) Model {
			m.box = m.box.Open(omnibox.Command)
			return m
		}, "input line"},
		{"import plan", func(m Model) Model {
			m = m.withImport()
			return m
		}, "import plan"},
		// The panel opens INSIDE the import, and outranks it: while a field is
		// open a keystroke is a character, not a plan command.
		{"panel over import", func(m Model) Model {
			m = m.withImport()
			m.creator = m.creator.Open("item", "")
			return m
		}, "panel"},
		// And a confirmation outranks everything, including the panel that
		// raised it -- a plan-row creation confirms while its panel is still up.
		{"confirmation over panel", func(m Model) Model {
			m = m.withImport()
			m.creator = m.creator.Open("item", "")
			m.confirm = &pendingPlan{}
			return m
		}, "confirmation"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := c.open(base)
			if got := m.mode(); got != c.want {
				t.Errorf("mode = %q, want %q", got, c.want)
			}
			top, ok := m.topLayer()
			if !ok || top.name != c.want {
				t.Errorf("topLayer = %q (%v), want %q", top.name, ok, c.want)
			}
		})
	}
}

// TestEveryLayerIsNamed, because an unnamed one cannot be reported by mode()
// and would make the report silently wrong rather than absent.
func TestEveryLayerIsNamed(t *testing.T) {
	seen := map[string]bool{}
	for i, l := range New(context.Background(), &fakeController{}).layers() {
		if l.name == "" {
			t.Errorf("layer %d has no name", i)
		}
		if l.handle == nil {
			t.Errorf("layer %q has no handler", l.name)
		}
		if seen[l.name] {
			t.Errorf("two layers are both called %q", l.name)
		}
		seen[l.name] = true
	}
}
