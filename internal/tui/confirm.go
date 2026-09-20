package tui

import (
	"strings"

	"home-management-system/internal/app"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/style"

	tea "github.com/charmbracelet/bubbletea"
)

// The confirmation layer: the one thing between a person and a permanent
// change.

// pendingPlan is a plan waiting for a person to say yes.
//
// Only ever set when the plan originates something, because that is the one
// error this system cannot undo: an annotation is a typo you fix, a recording
// is reversible by a compensating event, and a wrong kind or content unit is
// remedied only by making a new Item and archiving the old one.
type pendingPlan struct {
	plan    app.Plan
	summary string
}

// handleConfirm takes the keystroke while a permanent change is waiting.
func (m Model) handleConfirm(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch keys.Lookup(keys.Line, msg) {
	case keys.Cancel:
		m.confirm = nil
		m.say = m.say.Report("nothing was created")
		return m, nil
	case keys.Confirm:
		pending := *m.confirm
		m.confirm = nil
		return m, m.apply(pending.plan, pending.summary)
	}
	// Every other key is ignored. A confirmation that could be dismissed by a
	// stray keystroke is not a confirmation.
	return m, nil
}

// confirmView is the panel that names what cannot be changed later.
//
// The facts alone are not the point: "kind = Bulk, unit = g" is a fact, and
// "changing these later replaces every holding" is the reason to care. Friction
// is proportional to permanence, and this is the only place in the application
// that has any.
func (m Model) confirmView() string {
	plan := m.confirm.plan
	lines := []string{"  " + style.Strong.Render(m.confirm.summary+"?"), ""}

	for _, facts := range plan.Permanent {
		lines = append(lines, "  "+style.Strong.Render("permanent")+"     "+facts)
		lines = append(lines, "  "+strings.Repeat(" ", len("permanent"))+"     "+
			style.Dim.Render("changing these later replaces every holding"))
	}
	for _, ends := range plan.Irreversible {
		lines = append(lines, "  "+style.Strong.Render("no way back")+"   "+ends)
		lines = append(lines, "  "+strings.Repeat(" ", len("no way back"))+"   "+
			style.Dim.Render("the history stays; the holding does not"))
	}
	if len(plan.Permanent) == 0 && len(plan.Irreversible) == 0 {
		lines = append(lines, "  "+style.Dim.Render(
			"nothing here is permanent, but a second one by accident is worse than a question"))
	}

	// The verb is the act's own. "[enter] create" on a retirement would be
	// describing the wrong thing at the worst moment.
	verb := "go ahead"
	if plan.Creates() {
		verb = "create"
	}
	lines = append(lines, "", style.Dim.Render(
		"  ["+keys.Show(keys.Line, keys.Confirm)+"] "+verb+
			"    ["+keys.Show(keys.Line, keys.Cancel)+"] back"))
	return strings.Join(lines, "\n")
}
