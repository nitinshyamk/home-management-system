package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/resolve"
)

// The six actions worth a single key. Everything else is `:`.
//
// A keystroke on a selected row ALREADY HOLDS AN IDENTIFIER, so it builds the
// Command directly and never goes through Bind. That is not an optimisation:
// serialising a row to a name and fuzzy-matching it back is lossy in the worst
// way, because an ambiguous name could resolve to a DIFFERENT row than the one
// under the cursor. `c` on the second of three Ancho Chiles has to consume from
// the second one, always.
//
// What a keystroke does not skip is the value parsers. `c` prompts for a
// quantity and that text goes through the same ParseAmount a CSV cell does,
// because `100` and `100g` and `2bag` must mean the same thing wherever they
// are written.

// rowsByKey is the holdings currently on screen, by identifier, so a keystroke
// can reach the identifiers behind the row it is on.
func (m Model) holding(key int64) (app.HoldingRow, bool) {
	row, ok := m.holdingRows[key]
	return row, ok
}

// currentHolding is the row under the cursor, in the Holdings view.
func (m Model) currentHolding() (app.HoldingRow, bool) {
	if m.view != viewHoldings {
		return app.HoldingRow{}, false
	}
	row, ok := m.table.Current()
	if !ok {
		return app.HoldingRow{}, false
	}
	return m.holding(row.Key)
}

// selectedHoldings is what an action should act on: the explicit selection, or
// the row under the cursor when nothing is picked.
func (m Model) selectedHoldings() []app.HoldingRow {
	if m.view != viewHoldings {
		return nil
	}
	var out []app.HoldingRow
	for _, key := range m.table.Selected() {
		if row, ok := m.holding(key); ok {
			out = append(out, row)
		}
	}
	return out
}

// handleAction takes the single-key actions.
//
// Each key states its own scope rather than the whole set being gated to one
// view. `p` is why: you yank in the Holdings table and put in the Locations
// tree, so a gate around the lot meant the second half of the gesture never
// fired.
func (m Model) handleAction(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	key := msg.String()

	// p puts wherever the cursor is, which is usually somewhere else.
	if key == "p" {
		return m.put()
	}
	// Everything else acts on a Holding, so it needs the view that has them.
	if m.view != viewHoldings {
		return m, nil, false
	}

	switch key {
	case "c":
		return m.promptFor("consume", "how much", ""), nil, true
	case "#":
		return m.promptFor("count", "how much is actually there", ""), nil, true
	case "m":
		return m.promptFor("move", "where to", ""), nil, true
	case "t":
		return m.toggleCustody()
	case "y":
		return m.yank(), nil, true
	case "d":
		// dd, because a single d is the start of an operator in every editor
		// that has one, and retiring on a slip is not a thing to allow.
		if m.pendingD {
			m.pendingD = false
			return m.retire()
		}
		m.pendingD = true
		return m, nil, true
	}
	m.pendingD = false
	return m, nil, false
}

// promptFor opens the inline field an action needs.
//
// At the row, like the editor and the creation panel. Third time this answer
// has been given, and it is the same answer because it is the same reason: a
// prompt somewhere else costs you the row you were looking at.
func (m Model) promptFor(purpose, label, initial string) Model {
	rows := m.selectedHoldings()
	if len(rows) == 0 {
		m.problem = []string{"nothing to " + purpose + " here"}
		return m
	}
	// Refused before the field opens rather than after it is filled in, because
	// answering a question that was never going to be used is worse than not
	// being asked.
	if why, ok := refuses(purpose, rows); !ok {
		m.problem = []string{why}
		return m
	}
	m.problem = nil
	m.editor = m.editor.OpenFor(purpose, "Holding", int64(rows[0].ID), label, initial).SetWidth(m.width)
	return m
}

// refuses says why an action does not apply, in terms of the THING rather than
// the keystroke. "you cannot consume a cable" beats "invalid operation".
func refuses(purpose string, rows []app.HoldingRow) (string, bool) {
	for _, row := range rows {
		if row.Retired {
			return fmt.Sprintf("%q is done with, so there is nothing to %s", row.Item, purpose), false
		}
		measured := row.Custody == ""
		switch purpose {
		case "consume", "count":
			if !measured {
				return fmt.Sprintf("%q is one of a kind -- there is no amount to %s", row.Item, purpose), false
			}
		}
	}
	return "", true
}

// actOnPrompt turns a filled-in field into Commands, one per selected row.
func (m Model) actOnPrompt() (tea.Model, tea.Cmd, bool) {
	answer := strings.TrimSpace(m.editor.Value())
	purpose := m.editor.Purpose()
	rows := m.selectedHoldings()
	m.editor = m.editor.Close()

	if answer == "" {
		m.status = "nothing entered"
		return m, nil, true
	}

	var commands []command.Command
	for _, row := range rows {
		built, err := m.build(purpose, row, answer)
		if err != nil {
			m.problem = []string{err.Error()}
			return m, nil, true
		}
		commands = append(commands, built)
	}
	return m, m.runCommands(commands), true
}

// build makes the Command a keystroke means, from identifiers it already holds.
func (m Model) build(purpose string, row app.HoldingRow, answer string) (command.Command, error) {
	switch purpose {
	case "consume", "count":
		// The same parser a CSV cell goes through, then resolved against the
		// item exactly as Bind would resolve it.
		amount, err := m.amountFor(row, answer)
		if err != nil {
			return nil, err
		}
		if purpose == "count" {
			return command.Count{Holding: row.ID, Observed: amount}, nil
		}
		return command.Consume{Item: row.ItemID, Location: row.LocationID, Amount: amount}, nil

	case "move":
		to, err := m.locationNamed(answer)
		if err != nil {
			return nil, err
		}
		return command.Move{Holding: row.ID, To: to}, nil
	}
	return nil, fmt.Errorf("no action called %q", purpose)
}

// amountFor parses a written quantity and states it in the item's own unit.
func (m Model) amountFor(row app.HoldingRow, text string) (domain.Quantity, error) {
	written, err := command.ParseAmount(text)
	if err != nil {
		return domain.Zero, err
	}
	if written.Packages {
		return domain.Zero, fmt.Errorf("counting in packages needs the command line: try :consume %s", text)
	}
	if written.Unit != "" {
		return domain.Zero, fmt.Errorf("a unit needs the command line, which knows how to convert it: try :consume %s", text)
	}
	return written.Value, nil
}

// locationNamed resolves a destination the person typed.
//
// The DESTINATION is a name because a person typed it; the SUBJECT is an
// identifier because the cursor was already on it. That asymmetry is the whole
// design: resolution happens where trust changes, and nowhere else.
func (m Model) locationNamed(name string) (domain.LocationID, error) {
	index := resolve.NewIndex(m.candidates)
	switch outcome := index.Resolve(name, resolve.KindLocation).(type) {
	case resolve.Exact:
		return domain.LocationID(outcome.Candidate.ID), nil
	case resolve.Suggested:
		return 0, fmt.Errorf("did you mean %s? nothing called %q", outcome.Candidate.Path, name)
	case resolve.Ambiguous:
		var names []string
		for _, c := range outcome.Candidates {
			names = append(names, c.Path)
		}
		return 0, fmt.Errorf("%q could be %s", name, strings.Join(names, ", "))
	}
	return 0, fmt.Errorf("nowhere called %q", name)
}

// toggleCustody is one key rather than two.
//
// Two keys would mean remembering which state a thing is in before you can act,
// which is what looking at the screen was supposed to be for.
func (m Model) toggleCustody() (tea.Model, tea.Cmd, bool) {
	rows := m.selectedHoldings()
	if len(rows) == 0 {
		return m, nil, true
	}
	var commands []command.Command
	for _, row := range rows {
		switch row.Custody {
		case "":
			m.problem = []string{fmt.Sprintf("%q is measured, so there is no custody to change", row.Item)}
			return m, nil, true
		case "Out", "Lost":
			commands = append(commands, command.Return{Holding: row.ID})
		default:
			commands = append(commands, command.CheckOut{Holding: row.ID})
		}
	}
	return m, m.runCommands(commands), true
}

// retire ends a Holding's life. The record persists in history.
func (m Model) retire() (tea.Model, tea.Cmd, bool) {
	rows := m.selectedHoldings()
	if len(rows) == 0 {
		return m, nil, true
	}
	var commands []command.Command
	for _, row := range rows {
		commands = append(commands, command.Retire{Holding: row.ID})
	}
	return m, m.runCommands(commands), true
}

// yank remembers a row so p can put it somewhere.
//
// The fastest idiom here, and vim muscle memory doing real work: it is how you
// relocate something when you would rather look for the destination than name
// it.
func (m Model) yank() Model {
	row, ok := m.currentHolding()
	if !ok {
		m.problem = []string{"nothing to yank here"}
		return m
	}
	m.yanked = &row
	m.status = fmt.Sprintf("yanked %s -- p puts it where you are", row.Item)
	return m
}

// put moves what was yanked to wherever the cursor is now.
func (m Model) put() (tea.Model, tea.Cmd, bool) {
	if m.yanked == nil {
		m.problem = []string{"nothing yanked"}
		return m, nil, true
	}
	to, ok := m.destination()
	if !ok {
		m.problem = []string{"put somewhere that is a place -- try the Locations view"}
		return m, nil, true
	}
	yanked := *m.yanked
	m.yanked = nil
	return m, m.runCommands([]command.Command{command.Move{Holding: yanked.ID, To: to}}), true
}

// destination is the place the cursor is on, which is where a put goes.
func (m Model) destination() (domain.LocationID, bool) {
	switch {
	case m.view == viewLocations:
		node, ok := m.tree.Current()
		if !ok {
			return 0, false
		}
		return domain.LocationID(node.ID), true
	case m.view == viewHoldings:
		row, ok := m.currentHolding()
		if !ok {
			return 0, false
		}
		return row.LocationID, true
	}
	return 0, false
}
