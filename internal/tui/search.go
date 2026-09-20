package tui

import (
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/omnibox"
	"home-management-system/internal/tui/table"

	tea "github.com/charmbracelet/bubbletea"
)

// The input line: filtering the list, jumping across the house, and the
// palette that shows where a jump would land.

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
				m.say = m.say.Clear()
				m.helpTopic = topic
				if m.view != viewHelp {
					m.fromView = m.view
				}
				return m, m.load(viewHelp)
			}
			m.say = m.say.Working(workingOn(line))
			return m, m.runLine(line)
		}
		m.box = m.box.Accept()
		m = m.applyFilter()
		return m, nil
	}
	if next, handled := m.box.Update(msg); handled {
		m.box = next
		if m.box.Mode() == omnibox.Filter {
			// Narrow as you type: the filter that would apply if accepted now.
			m = m.applyLive()
		}
		return m, nil
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

// acceptJump goes to the chosen thing: the view it lives in, cursor on its row.
//
// WHERE a jump lands is the application's: it owns the views. What the palette
// is showing, and which row of it is under the cursor, is the widget's.
func (m Model) acceptJump() (Model, tea.Cmd) {
	target, ok := m.box.Chosen()
	m.box = m.box.Cancel()
	if !ok {
		return m, nil
	}
	m.pending = &target
	return m, m.load(viewFor(target.Kind))
}

// refreshJump sizes the palette to the space the list it replaces had, and
// hands it whatever vocabulary has arrived.
func (m Model) refreshJump() Model {
	m.box = m.box.SetResultsSize(m.width, m.bodyHeight()).SetCandidates(m.candidates)
	return m
}
