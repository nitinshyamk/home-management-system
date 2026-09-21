package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/style"
	"home-management-system/internal/tui/table"
)

// The act palette: what can be done to the thing under the cursor, listed,
// with what each would change.
//
// Thirty-five commands behind six keymap contexts is a vocabulary, not an
// interface. The palette is how the whole of it stays reachable without
// thirty-five keys: the four or five verbs a person uses hourly keep a key,
// and the rest are one keystroke plus a readable list away -- from the row
// they apply to, rather than from a help page.
//
// What is NOT on it is as much the point. A cable has no amount to consume,
// so "use some" is absent rather than present and refused. Illegal actions
// are the ones you find by trying them, and a list you have to try is a list
// that taught you nothing.

type palette struct {
	offers []offer
	tbl    table.Model
}

func newPalette(offers []offer, width, height int) palette {
	rows := make([]table.Row, 0, len(offers))
	for i, o := range offers {
		tone := table.ToneNormal
		if o.permanent {
			tone = table.ToneStop
		}
		rows = append(rows, table.Row{
			Key:   int64(i),
			Cells: []string{o.key(), o.label, o.note},
			Tone:  tone,
		})
	}
	return palette{
		offers: offers,
		tbl: table.New([]table.Column{
			{Title: "", Min: 1},
			{Title: "", Min: 14, Grow: true},
			{Title: "", Min: 10, Drop: 2},
		}).Fixed().Headerless().SetRows(rows).SetSize(width, height),
	}
}

func (p palette) chosen() (offer, bool) {
	row, ok := p.tbl.Current()
	if !ok || int(row.Key) >= len(p.offers) {
		return offer{}, false
	}
	return p.offers[row.Key], true
}

// byPress is the offer a keystroke names, which is what makes the palette a
// keymap you can read rather than a list you have to walk.
//
// Matched on the key as the SCREEN writes it, so the letter beside a row is
// literally the one to press -- including C-k, which is a keystroke and not a
// letter.
func (p palette) byPress(pressed string) (offer, bool) {
	for _, o := range p.offers {
		if o.key() == pressed {
			return o, true
		}
	}
	return offer{}, false
}

// handlePalette takes the keystroke while the palette is open.
func (m Model) handlePalette(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch keys.Lookup(keys.Browse, msg) {
	case keys.Cancel, keys.Act:
		m.acting = nil
		return m, nil
	case keys.Confirm:
		chosen, ok := m.acting.chosen()
		m.acting = nil
		if !ok {
			return m, nil
		}
		return m.take(chosen)
	}
	// A letter takes its offer outright. The list is sorted by how often a
	// verb is wanted rather than alphabetically, so walking to the one you
	// meant would be slower than the key it is labelled with.
	if chosen, ok := m.acting.byPress(keys.Display(msg.String())); ok {
		m.acting = nil
		return m.take(chosen)
	}
	next, _ := m.acting.tbl.Update(msg)
	m.acting.tbl = next
	return m, nil
}

// paletteView draws the palette in the drawer, headed by what it is about.
func (m Model) paletteView() []string {
	sel, _ := m.current.Current()
	head := style.Dim.Render("ACT ON  ") + style.Strong.Render(sel.Name) +
		style.Dim.Render(fmt.Sprintf("   %s", strings.ToLower(sel.Kind)))
	return append([]string{head}, lines(m.acting.tbl.View())...)
}

// paletteHeight is the palette's share of the screen: a line per offer, the
// heading, and the line the table keeps. Small enough that it never scrolls,
// which is what makes a letter beside every row worth having.
func (m Model) paletteHeight() int {
	return len(m.acting.offers) + 2
}
