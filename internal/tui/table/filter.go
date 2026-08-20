package table

import (
	"strings"

	"github.com/sahilm/fuzzy"
)

// Filtering.
//
// The table keeps every row and shows the matching subset, rather than being
// handed a narrowed list. That is what lets the count say "12 of 34" -- a
// narrowed list that cannot say what it is narrowed FROM is a list that lies
// about what you own.

// Filter decides which rows are shown.
//
// Facets are keyed by name and resolved to a column by the caller, because only
// the caller knows that `loc:` means the LOCATION column here and something
// else in another view. Text is matched across the whole row.
type Filter struct {
	// Facets maps a facet name to the column it restricts and the value it
	// restricts it to.
	Facets []FacetTest
	Text   string
}

// FacetTest is one field restriction resolved to a column.
type FacetTest struct {
	Column int
	Value  string
}

// Empty reports a filter that restricts nothing.
func (f Filter) Empty() bool { return len(f.Facets) == 0 && f.Text == "" }

// SetFilter narrows what is shown. The cursor is kept on its row when that row
// survives, because a filter that moves the cursor makes you re-find your place
// every keystroke.
func (m Model) SetFilter(f Filter) Model {
	var under int64 = -1
	if r, ok := m.Current(); ok {
		under = r.Key
	}
	m.filter = f
	m.visible = m.matching()
	m.cursor = 0
	for i, r := range m.visible {
		if r.Key == under {
			m.cursor = i
		}
	}
	m.clampScroll()
	return m
}

// Filtered reports whether a filter is in force.
func (m Model) Filtered() bool { return !m.filter.Empty() }

// Counts returns how many rows are shown and how many exist, which is what a
// status line needs to say "12 of 34".
func (m Model) Counts() (shown, total int) { return len(m.visible), len(m.rows) }

// matching applies the filter.
//
// Facets AND together and each is a substring test: a field restriction is a
// statement about a field, and fuzzy-matching it would make `loc:garage` quietly
// also mean the Great Room. Free text is fuzzy across the whole row, because
// that is where a person is guessing rather than stating.
func (m Model) matching() []Row {
	if m.filter.Empty() {
		return m.rows
	}

	kept := make([]Row, 0, len(m.rows))
	for _, r := range m.rows {
		if facetsMatch(r, m.filter.Facets) {
			kept = append(kept, r)
		}
	}
	if m.filter.Text == "" {
		return kept
	}

	source := rowText(kept)
	matches := fuzzy.FindFrom(strings.ToLower(m.filter.Text), source)
	out := make([]Row, 0, len(matches))
	for _, match := range matches {
		out = append(out, kept[match.Index])
	}
	return out
}

func facetsMatch(r Row, tests []FacetTest) bool {
	for _, t := range tests {
		if t.Value == "" || t.Value == "*" {
			continue
		}
		if !strings.Contains(strings.ToLower(cell(r, t.Column)), strings.ToLower(t.Value)) {
			return false
		}
	}
	return true
}

// rowText adapts rows to fuzzy.Source, joining the cells so a query can span
// columns -- "ancho garage" is one thing a person means, across two fields.
type rowText []Row

func (rs rowText) String(i int) string {
	return strings.ToLower(strings.Join(rs[i].Cells, " "))
}
func (rs rowText) Len() int { return len(rs) }
