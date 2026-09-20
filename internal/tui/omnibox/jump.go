package omnibox

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/table"
)

// The jump palette: what a jump would land on.
//
// It lives here because it is what this widget DISPLAYS. The line owned the
// typing and the application owned the results, so "what is this line doing and
// what would accepting it do" was a question with two owners -- and the screen
// asked the mode twice, once to replace the body and once to rewrite the facts.
//
// The palette REPLACES the list rather than floating over it. A jump searches
// everything, so leaving the current view visible underneath would suggest it
// is being searched, which is the confusion between the two modes this exists
// to avoid.

// resultColumns are the palette's. KIND comes first and is never dropped:
// "Shelf 1" as a Location and "Shelf 1" inside a Holding path are different
// destinations, and a jump that does not say which lands somewhere surprising.
var resultColumns = []table.Column{
	{Title: "KIND", Min: 8},
	{Title: "NAME", Min: 12, Grow: true, Elide: table.ElideStart},
}

// shown is how many destinations the palette offers at once.
//
// A palette longer than a glance is a list you read rather than a shortcut you
// take; if the right thing is not in the first dozen, another character of the
// query is faster than scrolling.
const shown = 12

// SetCandidates gives the palette everything there is to jump to.
func (m Model) SetCandidates(candidates []resolve.Candidate) Model {
	m.candidates = candidates
	return m.refresh()
}

// SetResultsSize sizes the palette, which has to fit the space the list it
// replaces had.
//
// A widget sized only on a terminal resize is a widget that is the wrong size
// until one happens, which is why refresh does this too.
func (m Model) SetResultsSize(width, height int) Model {
	m.resultsWidth, m.resultsHeight = width, height
	m.results = m.results.SetSize(width, height)
	return m
}

// Results is the palette, for the screen to draw in place of the list.
func (m Model) Results() string { return m.results.View() }

// Matches is how many destinations the query left, for the facts line.
func (m Model) Matches() int {
	n, _ := m.results.Counts()
	return n
}

// Chosen is where a jump would go.
func (m Model) Chosen() (resolve.Candidate, bool) {
	row, ok := m.results.Current()
	if !ok || int(row.Key) >= len(m.candidates) {
		return resolve.Candidate{}, false
	}
	return m.candidates[row.Key], true
}

// refresh re-runs the search across everything.
func (m Model) refresh() Model {
	m.results = m.results.SetSize(m.resultsWidth, m.resultsHeight)
	rows := make([]table.Row, 0, len(m.candidates))
	for i, c := range m.candidates {
		if c.Archived {
			continue
		}
		if !matches(c, m.input) {
			continue
		}
		rows = append(rows, table.Row{Key: int64(i), Cells: []string{string(c.Kind), c.Path}})
		if len(rows) >= shown {
			break
		}
	}
	m.results = m.results.SetRows(rows)
	return m
}

// updateResults gives the palette the keys the line did not want -- the cursor
// motion through the results.
func (m Model) updateResults(msg tea.KeyMsg) (Model, bool) {
	next, handled := m.results.Update(msg)
	m.results = next
	return m, handled
}

func matches(c resolve.Candidate, text string) bool {
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
