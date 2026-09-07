package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"errors"
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
	sel, ok := m.current.Current()
	if !ok {
		return app.HoldingRow{}, false
	}
	return m.holding(sel.Key)
}

// selectedHoldings is what an action should act on: the explicit selection, or
// the row under the cursor when nothing is picked.
func (m Model) selectedHoldings() []app.HoldingRow {
	if m.view != viewHoldings {
		return nil
	}
	var out []app.HoldingRow
	for _, sel := range m.current.Selected() {
		if row, ok := m.holding(sel.Key); ok {
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
func (m Model) handleAction(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	action := keys.Lookup(keys.Browse, msg)

	// Paste puts wherever the cursor is, which is usually somewhere else.
	if action == keys.Paste {
		next, cmd := m.put()
		return next, cmd, true
	}
	// Copy picks up from wherever the cursor is too. The two halves of one
	// gesture have to have the same reach, and a tree is where the second half
	// usually lands.
	if action == keys.Copy {
		return m.copy(), nil, true
	}
	// Moving a thing the tree is SHOWING is the other half of the same idea:
	// the trees display holdings and items, and the one thing a person wants to
	// do to something they can see somewhere wrong is put it somewhere right.
	if forest(m.view) {
		if action == keys.MoveTo {
			next, cmd := m.promptForNode()
			return next, cmd, true
		}
		return m, nil, false
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
		return m.promptFor("move", "where to", "").carrySubject(), m.loadCandidates(), true
	case keys.ToggleCustody:
		next, cmd := m.toggleCustody()
		return next, cmd, true
	case keys.Kill:
		// One key, where retiring used to take two.
		//
		// The doubled key was the guard against retiring on a slip, and it was
		// never the real one: a retirement is a permanent change, so its plan
		// carries a Permanent fact and the confirmation panel opens on it
		// regardless. The guard is the panel. Asking twice before the thing
		// that asks was two answers to one question.
		next, cmd := m.retire()
		return next, cmd, true
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

// promptForNode opens the destination prompt for a thing a tree is showing.
//
// The purpose is the thing's own, not the tree's: a Holding MOVES to a place, an
// Item is RECLASSIFIED under a classification. Two commands, and the difference
// is not a detail of wording -- moving stock and re-filing a kind of thing are
// different events in the ledger, and calling both "move" here would leave the
// interface with one word for two answers.
func (m Model) promptForNode() (Model, tea.Cmd) {
	sel, ok := m.current.Current()
	if !ok || !sel.Contained {
		return m.refuse("move one of the things inside -- a place is moved with %s",
			keys.Show(keys.Browse, keys.CommandLine)+" reparent location"), nil
	}
	purpose, label := "move", "where to"
	if sel.Kind == "Item" {
		purpose, label = "reclassify", "file it under"
	}
	m.problem = nil
	m.editor = m.editor.OpenFor(purpose, sel.Kind, sel.ID, label, "").SetWidth(m.width)
	// Picked up as well as prompted for. The two are the same act -- "this
	// goes somewhere else" -- and which way you finish it is a preference
	// about the destination: name it, or go and point at it. esc closes the
	// prompt and leaves the thing in hand, which is what the banner says.
	m = m.carry(carried{Kind: sel.Kind, ID: sel.ID, Name: sel.Name})
	// The vocabulary is loaded alongside the prompt, so the first keystroke
	// into it already has something to complete against.
	return m, m.loadCandidates()
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
func (m Model) actOnPrompt() (Model, tea.Cmd) {
	answer := strings.TrimSpace(m.editor.Value())
	purpose := m.editor.Purpose()
	rows := m.selectedHoldings()
	subject, kind := m.editor.Subject(), m.editor.Kind()
	inTree := forest(m.view)
	m.editor = m.editor.Close()
	// Answering the prompt finishes the move, so whatever `m` picked up is put
	// down with it. Without this, naming the destination would relocate the
	// thing and leave the banner insisting it was still in hand.
	m.copied = nil

	if answer == "" {
		m.status = "nothing entered"
		return m, nil
	}

	// A prompt opened over a TREE has one subject and it is the node, not a
	// selection: selectedHoldings is the Holdings table's idea of "what am I
	// acting on", and it is empty here -- which would have made this report
	// "nothing to do" for a perfectly complete answer.
	if inTree {
		built, err := m.buildForNode(purpose, kind, subject, answer)
		if err != nil {
			return m.refuse("%v", err), nil
		}
		return m, m.runCommands([]command.Command{built})
	}

	var commands []command.Command
	for _, row := range rows {
		built, err := m.build(purpose, row, answer)
		if err != nil {
			return m.refuse("%v", err), nil
		}
		commands = append(commands, built)
	}
	return m, m.runCommands(commands)
}

// buildForNode makes the Command a prompt over a tree row means.
//
// The kind decides, not the view: the Locations tree shows Holdings and the
// Categories tree shows Items, so reading the view would be reading it twice
// and getting the answer from the wrong one the day a tree shows something
// else.
func (m Model) buildForNode(purpose, kind string, subject int64, answer string) (command.Command, error) {
	switch {
	case purpose == "move" && kind == "Holding":
		to, err := m.locationNamed(answer)
		if err != nil {
			return nil, err
		}
		return command.Move{Holding: domain.HoldingID(subject), To: to}, nil
	case purpose == "reclassify" && kind == "Item":
		to, err := m.categoryNamed(answer)
		if err != nil {
			return nil, err
		}
		return command.Reclassify{Item: domain.ItemID(subject), Category: to}, nil
	}
	return nil, fmt.Errorf("nothing called %q applies to %s", purpose, strings.ToLower(kind))
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
func (m Model) locationNamed(name string) (domain.LocationID, error) {
	id, err := m.named(name, resolve.KindLocation)
	return domain.LocationID(id), err
}

// categoryNamed resolves a classification the person typed.
func (m Model) categoryNamed(name string) (domain.CategoryID, error) {
	id, err := m.named(name, resolve.KindCategory)
	return domain.CategoryID(id), err
}

// named resolves a destination the person typed.
//
// The DESTINATION is a name because a person typed it; the SUBJECT is an
// identifier because the cursor was already on it. That asymmetry is the whole
// design: resolution happens where trust changes, and nowhere else.
//
// Through resolve.Resolve rather than the completion matcher, because those two
// answer different questions: completion offers what you might mean and this
// decides what you did. A near miss here is reported as a near miss -- "did you
// mean" -- rather than silently taken, which is the resolver deciding, and the
// one thing it must never do.
func (m Model) named(name string, kind resolve.Kind) (int64, error) {
	if m.index == nil {
		return 0, errors.New("the vocabulary has not loaded yet")
	}
	outcome := m.index.Resolve(name, kind)
	if exact, ok := outcome.(resolve.Exact); ok {
		return exact.Candidate.ID, nil
	}
	// Worded by resolve, so a near miss on a keystroke reads exactly as the
	// same near miss typed on the command line. It used to read differently,
	// and worse: the basis that says WHY a suggestion was offered was dropped.
	return 0, errors.New(resolve.Describe(outcome))
}

// toggleCustody is one key rather than two.
//
// Two keys would mean remembering which state a thing is in before you can act,
// which is what looking at the screen was supposed to be for.
func (m Model) toggleCustody() (Model, tea.Cmd) {
	rows := m.selectedHoldings()
	if len(rows) == 0 {
		return m, nil
	}
	var commands []command.Command
	for _, row := range rows {
		switch row.Custody {
		case "":
			return m.refuse("%q is measured, so there is no custody to change", row.Item), nil
		case "Out", "Lost":
			commands = append(commands, command.Return{Holding: row.ID})
		default:
			commands = append(commands, command.CheckOut{Holding: row.ID})
		}
	}
	return m, m.runCommands(commands)
}

// retire ends a Holding's life. The record persists in history.
func (m Model) retire() (Model, tea.Cmd) {
	rows := m.selectedHoldings()
	if len(rows) == 0 {
		return m, nil
	}
	var commands []command.Command
	for _, row := range rows {
		commands = append(commands, command.Retire{Holding: row.ID})
	}
	return m, m.runCommands(commands)
}

// carried is a thing picked up by copy, waiting for a put.
//
// Kinded, because there are two things worth relocating and they go to
// different sorts of place: a Holding moves to a Location, an Item is filed
// under a Category. An untyped identifier would let the second land in the
// first, and both are int64.
type carried struct {
	Kind string // "Holding" or "Item"
	ID   int64
	Name string
}

// copy picks up whatever the cursor is on, so a put can relocate it.
//
// M-w and C-y, which is emacs's copy and paste -- and note that the words swap
// sides coming from vim, where yank is the COPY. It is how you relocate
// something when you would rather look for the destination than name it, which
// in a tree is nearly always: the destination is on screen, and naming it would
// be the long way round.
//
// The interface says CARRYING rather than "copied", because nothing is copied:
// there is one thing and it ends up somewhere else. "Copied" belongs to the
// keys, which are emacs's, and it was the wrong word for what happens.
func (m Model) copy() Model {
	if forest(m.view) {
		sel, ok := m.current.Current()
		if !ok || !sel.Contained {
			return m.refuse("copy one of the things inside -- a place is moved with %s",
				keys.Show(keys.Browse, keys.CommandLine)+" reparent location")
		}
		return m.carry(carried{Kind: sel.Kind, ID: sel.ID, Name: sel.Name})
	}
	row, ok := m.currentHolding()
	if !ok {
		return m.refuse("nothing to copy here")
	}
	return m.carry(carried{Kind: "Holding", ID: int64(row.ID), Name: row.Item})
}

// carry picks a thing up. What is being held is said by carryLine, which is a
// BANNER rather than a status: a status is wiped by the next view switch, and a
// view switch is the middle step of this gesture.
func (m Model) carry(what carried) Model {
	m.copied = &what
	m.problem, m.status = nil, ""
	return m
}

// carrySubject picks up what a prompt was just opened over, so the same gesture
// can be finished by pointing instead of by typing.
//
// Only one thing, and only if the prompt actually opened. `m` on the Holdings
// table acts on the whole selection, and a carry holds exactly one thing -- so
// with several rows picked, `m` stays the typed prompt it has always been, and
// what it writes is unchanged.
func (m Model) carrySubject() Model {
	if !m.editor.IsOpen() {
		return m
	}
	rows := m.selectedHoldings()
	if len(rows) != 1 {
		return m
	}
	return m.carry(carried{Kind: "Holding", ID: int64(rows[0].ID), Name: rows[0].Item})
}

// put relocates what was copied to whatever the cursor is on now.
//
// The pairing is the whole of it: a Holding goes to a Location, an Item goes to
// a Category, and the two mismatches are refused in terms of the things rather
// than as a type error. Putting a holding into a classification is not a
// near-miss to be coerced -- a classification is not anywhere.
func (m Model) put() (Model, tea.Cmd) {
	if m.copied == nil {
		return m.refuse("nothing in hand -- %s picks up the row you are on",
			keys.Show(keys.Browse, keys.Copy)), nil
	}
	kind, id, ok := m.destination()
	if !ok {
		return m.refuse("put it on a place or a classification, not on a thing"), nil
	}
	copied := *m.copied

	switch {
	case copied.Kind == "Holding" && kind == "Location":
		m.copied = nil
		return m, m.runCommands([]command.Command{
			command.Move{Holding: domain.HoldingID(copied.ID), To: domain.LocationID(id)},
		})
	case copied.Kind == "Item" && kind == "Category":
		m.copied = nil
		return m, m.runCommands([]command.Command{
			command.Reclassify{Item: domain.ItemID(copied.ID), Category: domain.CategoryID(id)},
		})
	case copied.Kind == "Holding":
		return m.refuse("%q is stock -- it goes in a place, and this is a classification",
			copied.Name), nil
	default:
		return m.refuse("%q is a kind of thing -- it is filed under a classification, not kept in a place",
			copied.Name), nil
	}
}

// destination is the container the cursor is on, and what sort of container it
// is, which is what a put needs to know before it can mean anything.
func (m Model) destination() (kind string, id int64, ok bool) {
	switch {
	case forest(m.view):
		sel, found := m.current.Current()
		if !found {
			return "", 0, false
		}
		// A CONTAINED row is not a container. Without this the cursor sitting
		// on a holding would hand its identifier over as a LocationID and the
		// put would land in whatever location happens to share that number --
		// a silent write to the wrong shelf, which is the worst kind of wrong
		// this interface can be.
		if sel.Contained {
			return "", 0, false
		}
		return sel.Kind, sel.ID, true
	case m.view == viewHoldings:
		row, found := m.currentHolding()
		if !found {
			return "", 0, false
		}
		return "Location", int64(row.LocationID), true
	}
	return "", 0, false
}
