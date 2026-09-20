package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/keys"
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
	// offers is the keys this mode takes, for the one permanently visible line
	// at the bottom of the screen. Empty where the mode draws its own -- a
	// field and the creation panel put their hints beside the field, and the
	// confirmation states its two keys in the middle of the screen on purpose,
	// because friction there is proportional to permanence.
	//
	// Declared HERE, beside the mode it belongs to, because this file is
	// already the one place that says which modes exist and which one owns the
	// keyboard. A switch somewhere else mapping mode names to hints would be
	// that list written a second time.
	offers string
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
		{name: "confirmation", active: m.confirm != nil, handle: Model.handleConfirm},
		{name: "field", active: m.editor.IsOpen(), handle: Model.handleEditor},
		{name: "panel", active: m.creator.IsOpen(), handle: Model.handleCreator},
		// After the field and the panel, because they open INSIDE it. Putting
		// the plan first meant its table ate ctrl+u while someone was clearing
		// a field, and enter settled the row instead of saving what they had
		// typed into it.
		{
			name: "import plan", active: m.flow.reviewing(), handle: Model.handleImport,
			offers: m.planKeys(),
		},
		{
			name: "input line", active: m.box.Mode() != omnibox.Closed, handle: Model.handleOmnibox,
			// The line is the mode, and it draws its own accept and cancel.
			offers: "",
		},
	}
}

// planKeys is what the plan screen takes, which depends on which stage it is.
//
// Skipping is offered only where it is possible. A key named under a screen
// that ignores it is worse than a key named nowhere: the line at the bottom is
// permanent, so the only thing that makes it worth the row is that it can be
// believed.
func (m Model) planKeys() string {
	groups := [][]keys.Action{
		{keys.Confirm}, {keys.Drop}, {keys.Undrop}, {keys.ApplyAll},
	}
	if m.flow.plan.Stage().Skippable {
		groups = append(groups, []keys.Action{keys.SkipStage})
	}
	return keys.Hint(keys.Plan, append(groups, []keys.Action{keys.Quit})...)
}

// mode names what has the keystroke, or "browsing" when nothing is over the
// list.
func (m Model) mode() string {
	if top, ok := m.topLayer(); ok {
		return top.name
	}
	return "browsing"
}

// offered is what the mode that owns the keyboard takes, for the one
// permanently visible line at the bottom of the screen.
//
// The line is drawn under every screen, so what it advertises has to be true of
// whichever screen that is. A line naming C-s under a confirmation that ignores
// every key but two would be worse than a blank one: the only reason to put an
// affordance on screen permanently is that it can be believed.
//
// Browsing is the case with no layer at all, and it offers the three leaders --
// which is where the footer's five hints went. They rendered only when the
// status line was entirely empty, which a row count made impossible.
func (m Model) offered() string {
	// Browsing is the case with no layer at all. The open line is the other
	// case that offers these: it IS one of them, and the two it does not have
	// open are still a keystroke away once it closes.
	top, ok := m.topLayer()
	if !ok || top.name == "input line" {
		// What the cursor is on decides, not which screen is up. A fixed
		// three-item reminder said the same thing everywhere and therefore
		// told you nothing about the row you were standing on -- see
		// Model.verbs.
		return m.verbs()
	}
	return top.offers
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
