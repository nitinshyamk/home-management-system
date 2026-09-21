package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/omnibox"
)

// What can be done to the thing under the cursor.
//
// One table, read by two things: the bar along the bottom, which names the
// few verbs that have a key of their own, and the palette, which lists all of
// them with what they would change. Two tables would be two answers to one
// question, and the screen would eventually offer a key the palette did not
// have or the other way round.
//
// The annotations are the point of the palette. "use some" is a guess about
// what a key does; "use some -- 100 g here" is the state you would be acting
// on, said before you act rather than after.

// offer is one verb, for this row, now.
type offer struct {
	// press is what does it while the palette is open, for the verbs that
	// have no key outside it. Where there IS one, key() uses that instead --
	// see there for why this field is not simply the answer.
	press string
	// label is the palette's wording, which has a line to itself and can be
	// a phrase. short is the bar's, which has to share one line with four
	// others; empty means the keymap's own word for the action will do.
	label string
	short string
	// note is the state this verb would change, in the row's own terms.
	note string
	// global is the key that does this without opening the palette, or None
	// where the palette is the only way to it. A verb nobody reaches for
	// daily does not earn a key on a keyboard that has to hold the ones
	// people do.
	global keys.Action
	// permanent marks what cannot be taken back, so the palette can say so
	// before the confirmation does.
	permanent bool
	// line is the command to prefill the `:` line with, for the verbs this
	// interface has a Command for but no gesture.
	//
	// It is not a shortcut around building them properly -- it is what makes
	// the whole vocabulary reachable from the row it applies to, instead of
	// from a help page listing thirty-five ops. The line then completes and
	// refuses exactly as it does when typed.
	line command.Op
	// run is the gesture, where there is one.
	run func(Model) (Model, tea.Cmd)
}

// key is what to press for this offer, in the terms the screen writes keys.
//
// The global binding wins wherever there is one. Letting the palette pick its
// own letter was the first version, and the bar came out saying "c consume"
// beside a palette saying "u use some" -- one verb, two keys, which is the
// disagreement this table exists to make impossible.
func (o offer) key() string {
	if o.global != keys.None {
		if shown := keys.Show(keys.Browse, o.global); shown != "" {
			return shown
		}
	}
	return o.press
}

// offersFor is every verb legal for the cursor's subject.
func (m Model) offersFor() []offer {
	if m.view != viewShell {
		return nil
	}
	sel, ok := m.current.Current()
	if !ok {
		return nil
	}
	switch sel.Kind {
	case kindHolding:
		row, found := m.holding(sel.Key)
		if !found {
			return nil
		}
		if row.Custody == "" {
			return m.bulkOffers(row)
		}
		return m.uniqueOffers(row)
	case kindItem:
		return m.itemOffers(domain.ItemID(sel.ID))
	case kindLocation, kindCategory:
		return m.nodeOffers()
	}
	return nil
}

func (m Model) bulkOffers(row app.HoldingRow) []offer {
	here := row.State
	return []offer{
		{
			label: "use some", note: here + " here",
			global: keys.Consume, run: asks(editor.Consume),
		},
		{
			label: "count what is there", note: "the ledger says " + here,
			global: keys.Count, run: asks(editor.Count),
		},
		{
			label: "move it", note: "to another place",
			global: keys.MoveTo, run: startMove,
		},
		{
			press: "d", label: "discard some", note: "spoiled, expired, broken",
			line: command.OpDiscard,
		},
		{
			press: "x", label: "set an expiry", note: expiryNote(row),
			line: command.OpSetExpiry,
		},
		{
			label: "rename the item", note: row.Item,
			global: keys.EditInPlace, run: openRename,
		},
		{
			label: "retire this holding", note: "the history stays; the holding does not",
			global: keys.Kill, permanent: true, run: retire,
		},
	}
}

func (m Model) uniqueOffers(row app.HoldingRow) []offer {
	out := row.Custody == "Out" || row.Custody == "Lost"
	take := offer{
		label: "take it out", short: "take out",
		note:   "one keystroke, no questions",
		global: keys.ToggleCustody, run: toggleCustody,
	}
	if out {
		take = offer{
			label: "put it back", short: "put back",
			note:   "home is " + row.LocationPath,
			global: keys.ToggleCustody, run: toggleCustody,
		}
	}
	offers := []offer{
		take,
		{
			label: "move it", note: "to another place",
			global: keys.MoveTo, run: startMove,
		},
	}
	if out {
		offers = append(offers,
			offer{
				press: "h", label: "rehome it here", note: "this lives here now",
				line: command.OpRehome,
			})
	} else {
		offers = append(offers,
			offer{
				press: "v", label: "say it is here", note: "records an observation",
				line: command.OpVerify,
			})
	}
	return append(offers,
		offer{
			press: "l", label: "mark it lost", note: "stays searchable, reversible by finding it",
			line: command.OpMarkLost,
		},
		offer{
			label: "rename the item", note: row.Item,
			global: keys.EditInPlace, run: openRename,
		},
		offer{
			label:  "retire this holding",
			note:   "the history stays; the holding does not",
			global: keys.Kill, permanent: true, run: retire,
		},
	)
}

func (m Model) itemOffers(id domain.ItemID) []offer {
	item, ok := m.item(id)
	if !ok {
		return nil
	}
	change := offer{
		press: "p", label: "demote to a counted pile",
		note: "lossy going forward: no future event can name one unit", line: command.OpDemote,
	}
	if item.Kind == "Bulk" {
		change = offer{
			press: "p", label: "promote to one of a kind",
			note: "discards the unit and the package size", line: command.OpPromote,
		}
	}
	return []offer{
		{
			press: "a", label: "acquire some", note: "adds stock somewhere",
			line: command.OpReceive,
		},
		{
			label: "file it elsewhere", note: "now in " + item.Category,
			global: keys.MoveTo, run: promptNode,
		},
		{
			label: "rename it", note: item.Name,
			global: keys.EditInPlace, run: openRename,
		},
		change,
	}
}

func (m Model) nodeOffers() []offer {
	spec := m.lens.spec()
	name := m.RailName()
	offers := []offer{
		{
			label: "new " + spec.node + " inside", note: "child of " + name,
			global: keys.Create, run: openCreate,
		},
		{
			label: "rename it", note: name,
			global: keys.EditInPlace, run: openRename,
		},
	}
	if m.lens == lensPlace {
		offers = append(offers,
			offer{
				press: "m", label: "move it elsewhere", note: "its contents follow",
				line: command.OpReparentLocation,
			})
	} else {
		offers = append(offers,
			offer{
				press: "m", label: "move it elsewhere", note: "its contents follow",
				line: command.OpReparentCategory,
			})
	}
	return append(offers, offer{
		press: "K", label: "retire it", note: "what is inside resolves to the parent",
		permanent: true, line: archiveOp(m.lens),
	})
}

func archiveOp(l lens) command.Op {
	if l == lensPlace {
		return command.OpArchiveLocation
	}
	return command.OpArchiveCategory
}

func expiryNote(row app.HoldingRow) string {
	if row.Note == "" {
		return "none set"
	}
	return row.Note
}

// ---------------------------------------------------------------------------
// The gestures an offer can run
// ---------------------------------------------------------------------------

// asks opens the prompt for a purpose. Named for what it does to the person
// rather than for the widget, because `prompt` is already the spec lookup.
func asks(p editor.Purpose) func(Model) (Model, tea.Cmd) {
	return func(m Model) (Model, tea.Cmd) { return m.promptFor(p, ""), nil }
}

func startMove(m Model) (Model, tea.Cmd) {
	return m.promptFor(editor.Move, "").carrySubject(), m.loadCandidates()
}

func promptNode(m Model) (Model, tea.Cmd)    { return m.promptForNode() }
func toggleCustody(m Model) (Model, tea.Cmd) { return m.toggleCustody() }
func retire(m Model) (Model, tea.Cmd)        { return m.retire() }
func openRename(m Model) (Model, tea.Cmd)    { return m.openEditor(), nil }
func openCreate(m Model) (Model, tea.Cmd)    { return m.openCreator(), m.loadCandidates() }

// take does what an offer offers: its gesture, or the command line prefilled
// with its op for the verbs that have no gesture yet.
func (m Model) take(o offer) (Model, tea.Cmd) {
	if o.run != nil {
		return o.run(m)
	}
	m.say = m.say.Clear()
	m.box = m.box.OpenWith(omnibox.Command, string(o.line)+" ")
	return m, m.loadCandidates()
}

// verbLine is the bar along the bottom: the offers that have a key of their
// own, in the order the table declares them.
func (m Model) verbLine() string {
	var parts []string
	for _, o := range m.offersFor() {
		if o.global == keys.None {
			continue
		}
		shown := keys.Show(keys.Browse, o.global)
		if shown == "" {
			continue
		}
		word := o.short
		if word == "" {
			word = keys.Label(keys.Browse, o.global)
		}
		parts = append(parts, fmt.Sprintf("%s %s", shown, word))
	}
	return strings.Join(parts, " - ")
}
