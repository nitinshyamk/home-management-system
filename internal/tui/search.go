package tui

import (
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/omnibox"
	"home-management-system/internal/tui/table"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// The input line: filtering the list, jumping across the house, and the
// palette that shows where a jump would land.

// jumpColumns are the palette's. KIND comes first and is never dropped:
// "Shelf 1" as a Location and "Shelf 1" inside a Holding path are different
// destinations, and a jump that does not say which lands somewhere surprising.
var jumpColumns = []table.Column{
	{Title: "KIND", Min: 8},
	{Title: "NAME", Min: 12, Grow: true, Elide: table.ElideStart},
}

// handleOmnibox takes the keystroke when the input line is open.
//
// It runs before everything else, because while the line is open a keystroke is
// a CHARACTER. A `j` that moved the cursor while someone was typing "jar" would
// make the input line unusable, and it is the classic way a modal interface
// betrays the person using it.
func (m Model) handleOmnibox(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch keys.Lookup(keys.Line, msg) {
	case keys.Cancel:
		m.box = m.box.Cancel()
		// Restoring the ACCEPTED filter, not merely closing the line. Rows
		// narrow as you type, so an abandoned edit leaves the half-typed filter
		// on the table -- which showed up as "0 of 12 holdings" under a jump
		// palette, long after the filter that produced it had been cancelled.
		return m.applyFilter(), nil
	case keys.Confirm:
		if m.box.Mode() == omnibox.Jump {
			return m.acceptJump()
		}
		if m.box.Mode() == omnibox.Command {
			line := m.box.Input()
			m.box = m.box.Accept()
			// help is answered here rather than bound and planned, because it
			// is the one line that acts on the INTERFACE instead of on the
			// house. Sending it through Bind would need a Command that writes
			// nothing, and a write path with a no-op in it is a write path
			// somebody will one day give something to do.
			if topic, ok := helpAsked(line); ok {
				m.problem, m.status = nil, ""
				m.helpTopic = topic
				if m.view != viewHelp {
					m.fromView = m.view
				}
				return m, m.load(viewHelp)
			}
			m.status = "working..."
			return m, m.runLine(line)
		}
		m.box = m.box.Accept()
		m = m.applyFilter()
		return m, nil
	}
	if next, handled := m.box.Update(msg); handled {
		m.box = next
		switch m.box.Mode() {
		case omnibox.Jump:
			m = m.refreshJump()
		case omnibox.Filter:
			// Narrow as you type: the filter that would apply if accepted now.
			m = m.applyLive()
		}
		return m, nil
	}
	// A key the input line does not want -- cursor motion through the results,
	// which the palette owns.
	if m.box.Mode() == omnibox.Jump {
		if next, handled := m.jump.Update(msg); handled {
			m.jump = next
			return m, nil
		}
	}
	return m, nil
}

// applyFilter puts the accepted filter onto whichever surface is showing.
func (m Model) applyFilter() Model { return m.filterWith(m.box.Query()) }

// applyLive puts the half-typed filter on, which is what makes rows narrow as
// the line is typed rather than when it is accepted.
func (m Model) applyLive() Model { return m.filterWith(m.box.Live()) }

func (m Model) filterWith(q omnibox.Query) Model {
	m.current = m.current.SetFilter(q)
	return m
}

// facetTests resolves facet names to columns, dropping the ones this view has
// no field for.
func facetTests(v view, q omnibox.Query) []table.FacetTest {
	columns := spec(v).facets
	var out []table.FacetTest
	for _, f := range q.Facets {
		if column, ok := columns[f.Key]; ok && !f.Any() {
			out = append(out, table.FacetTest{Column: column, Value: f.Value})
		}
	}
	return out
}

// refreshJump re-runs the search across everything.
//
// It re-sizes as well as re-filters, because the palette replaces the body and
// therefore has to fit the same space the list did -- a widget that is only
// sized on a terminal resize is a widget that is the wrong size until one
// happens.
func (m Model) refreshJump() Model {
	m.jump = m.jump.SetSize(m.width, m.bodyHeight())
	rows := make([]table.Row, 0, len(m.candidates))
	for i, c := range m.candidates {
		if c.Archived {
			continue
		}
		if !matchesJump(c, m.box.Input()) {
			continue
		}
		rows = append(rows, table.Row{Key: int64(i), Cells: []string{string(c.Kind), c.Path}})
		if len(rows) >= 12 {
			break
		}
	}
	m.jump = m.jump.SetRows(rows)
	return m
}

func matchesJump(c resolve.Candidate, text string) bool {
	if strings.TrimSpace(text) == "" {
		return true
	}
	return fuzzyContains(strings.ToLower(c.Path), strings.ToLower(strings.TrimSpace(text)))
}

// fuzzyContains is subsequence matching: every character of the query, in
// order. The same rule the resolver uses, so the palette and the `:` line agree
// about what a name nearly is.
func fuzzyContains(haystack, needle string) bool {
	at := 0
	for _, r := range needle {
		if r == ' ' {
			continue
		}
		i := strings.IndexRune(haystack[at:], r)
		if i < 0 {
			return false
		}
		at += i + 1
	}
	return true
}

// acceptJump goes to the chosen thing: the view it lives in, cursor on its row.
func (m Model) acceptJump() (Model, tea.Cmd) {
	row, ok := m.jump.Current()
	if !ok || int(row.Key) >= len(m.candidates) {
		m.box = m.box.Cancel()
		return m, nil
	}
	target := m.candidates[row.Key]
	m.box = m.box.Cancel()
	m.pending = &target
	return m, m.load(viewFor(target.Kind))
}
