package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/omnibox"
)

// A layer is a mode that takes the keystroke before anything under it.
//
// The interface is modal, and the modes nest: a dropdown sits inside a field,
// a field sits inside the panel that opened it, a confirmation sits over all of
// them. Which one a keystroke belongs to is decided by depth -- the innermost
// active layer gets it, and esc leaves exactly one, never two.
type layer struct {
	// name is what the mode is called, for mode() and for tests. Nothing could
	// say this before: "what is the interface doing right now" was a question
	// you answered by reading seven fields.
	name   string
	active bool
	handle func(Model, tea.KeyMsg) (Model, tea.Cmd)
}

// layers are the modes, innermost first.
//
// This order IS the nesting, and this is the only place it is stated. It used
// to be stated twice -- once as the order of six calls in Update, and again
// inside handleImport as a guard listing the modes that outrank it. The second
// copy could never fire (every layer above it consumes unconditionally once
// active, so handleImport was only ever reached with both of them closed), but
// it had to be kept in step by hand with an order written somewhere else.
//
// Every layer consumes the keystroke once it is active, including keys it has
// no use for. That is what modal means, and it is why this is a search for the
// topmost active layer rather than a chain that each layer might pass along:
// a confirmation that could be dismissed by a stray keystroke is not a
// confirmation, and a field that let ctrl+u reach the plan screen underneath
// was the bug that put this order in the code to begin with.
func (m Model) layers() []layer {
	return []layer{
		{"confirmation", m.confirm != nil, Model.handleConfirm},
		{"field", m.editor.IsOpen(), Model.handleEditor},
		{"panel", m.creator.IsOpen(), Model.handleCreator},
		// After the field and the panel, because they open INSIDE it. Putting
		// the plan first meant its table ate ctrl+u while someone was clearing
		// a field, and enter settled the row instead of saving what they had
		// typed into it.
		{"import plan", m.importing, Model.handleImport},
		{"input line", m.box.Mode() != omnibox.Closed, Model.handleOmnibox},
	}
}

// mode names what has the keystroke, or "browsing" when nothing is over the
// list.
func (m Model) mode() string {
	if top, ok := m.topLayer(); ok {
		return top.name
	}
	return "browsing"
}

// topLayer is the innermost active mode, if any.
func (m Model) topLayer() (layer, bool) {
	for _, l := range m.layers() {
		if l.active {
			return l, true
		}
	}
	return layer{}, false
}
