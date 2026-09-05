package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/keys"
)

// The handful of actions worth a key of their own. Everything else is M-x.
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

// refuse reports why something did not happen, and clears whatever the last
// thing that DID happen said.
//
// Leaving the old status underneath a refusal reads as though both were true --
// the screen saying "counted 50" while also saying the action was impossible.
func (m Model) refuse(format string, args ...any) Model {
	m.problem = []string{fmt.Sprintf(format, args...)}
	m.status = ""
	return m
}

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

// handleAction takes the keys that act on whatever the cursor is on.
//
// Each key states its own scope rather than the whole set being gated to one
// view. Paste is why: you copy in the Holdings table and put in the Locations
// tree, so a gate around the lot meant the second half of the gesture never
// fired.
func (m Model) handleAction(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	action := keys.Lookup(keys.Browse, msg)

	// Paste puts wherever the cursor is, which is usually somewhere else.
	if action == keys.Paste {
		return m.put()
	}
	// Everything else acts on a Holding, so it needs the view that has them.
	if m.view != viewHoldings {
		return m, nil, false
	}

	switch action {
	case keys.Consume:
		return m.promptFor("consume", "how much", ""), nil, true
	case keys.Count:
		return m.promptFor("count", "how much is actually there", ""), nil, true
	case keys.MoveTo:
		// The vocabulary is loaded alongside the prompt, so the first
		// keystroke into it already has something to complete against.
		return m.promptFor("move", "where to", ""), m.loadCandidates(), true
	case keys.ToggleCustody:
		return m.toggleCustody()
	case keys.Copy:
		return m.copy(), nil, true
	case keys.Kill:
		// One key, where retiring used to take two.
		//
		// The doubled key was the guard against retiring on a slip, and it was
		// never the real one: a retirement is a permanent change, so its plan
		// carries a Permanent fact and the confirmation panel opens on it
		// regardless. The guard is the panel. Asking twice before the thing
		// that asks was two answers to one question.
		return m.retire()
	}
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
		return m.refuse("nothing to %s here", purpose)
	}
	// Refused before the field opens rather than after it is filled in, because
	// answering a question that was never going to be used is worse than not
	// being asked.
	if why, ok := refuses(purpose, rows); !ok {
		return m.refuse("%s", why)
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
			return m.refuse("%v", err), nil, true
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
		return domain.Zero, fmt.Errorf("counting in packages needs the command line: try %s, then consume %s",
			keys.Show(keys.Browse, keys.CommandLine), text)
	}
	if written.Unit != "" {
		return domain.Zero, fmt.Errorf("a unit needs the command line, which knows how to convert it: try %s, then consume %s",
			keys.Show(keys.Browse, keys.CommandLine), text)
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
			return m.refuse("%q is measured, so there is no custody to change", row.Item), nil, true
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

// copy remembers a row so a put can move it somewhere.
//
// M-w and C-y, which is emacs's copy and paste -- and note that the words swap
// sides coming from vim, where yank is the COPY. It is how you relocate
// something when you would rather look for the destination than name it.
func (m Model) copy() Model {
	row, ok := m.currentHolding()
	if !ok {
		return m.refuse("nothing to copy here")
	}
	m.copied = &row
	m.status = fmt.Sprintf("copied %s -- %s puts it where you are",
		row.Item, keys.Show(keys.Browse, keys.Paste))
	return m
}

// put moves what was copied to wherever the cursor is now.
func (m Model) put() (tea.Model, tea.Cmd, bool) {
	if m.copied == nil {
		return m.refuse("nothing copied"), nil, true
	}
	to, ok := m.destination()
	if !ok {
		return m.refuse("put somewhere that is a place -- try the Locations view"), nil, true
	}
	copied := *m.copied
	m.copied = nil
	return m, m.runCommands([]command.Command{command.Move{Holding: copied.ID, To: to}}), true
}

// destination is the place the cursor is on, which is where a put goes.
func (m Model) destination() (domain.LocationID, bool) {
	switch {
	case m.view == viewLocations:
		node, ok := m.tree.Current()
		if !ok {
			return 0, false
		}
		// A CONTAINED row is not a place. Without this the cursor sitting on a
		// holding would hand its identifier over as a LocationID and the put
		// would land in whatever location happens to share that number -- a
		// silent write to the wrong shelf, which is the worst kind of wrong
		// this interface can be.
		if node.Contained() {
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
