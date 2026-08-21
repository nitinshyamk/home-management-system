// Package tree is the Category and Location view.
//
// It is built ON the table widget rather than beside it. A tree row and a table
// row differ in what the first column contains -- indentation and a fold marker
// -- and in nothing else, so cursor, scrolling, banding, selection, and the
// gutter all come across unchanged. That sharing is the point: if the tree
// needed its own answer to any of them, the two surfaces would have started to
// diverge into different products.
//
// What it adds is folding, and an order the table must not touch.
package tree

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/table"
)

// Node is one node of a forest, in pre-order with its depth.
//
// Pre-order plus depth rather than parent pointers, because that is what the
// query path already returns and what folding needs: a node's descendants are
// exactly the nodes that follow it with a greater depth.
type Node struct {
	ID    int64
	Name  string
	Depth int
	// Count is the rollup over the subtree -- items, or holdings.
	Count int64
}

// indent is two spaces per level: enough to see, cheap enough to go five deep
// in sixty columns.
const indent = "  "

const (
	markerExpanded  = "▾"
	markerCollapsed = "▸"
	markerLeaf      = " "
)

// Model is the tree.
type Model struct {
	nodes     []Node
	collapsed map[int64]bool
	filter    string
	unit      string // "items" or "holdings", for the count column's title
	pendingZ  bool
	tbl       table.Model
}

// New builds a tree whose counts are labelled with unit.
func New(unit string) Model {
	m := Model{
		collapsed: map[int64]bool{},
		unit:      unit,
	}
	m.tbl = table.New(m.columns()).Fixed()
	return m
}

func (m Model) columns() []table.Column {
	return []table.Column{
		{Title: "NAME", Min: 12, Grow: true},
		// Right-aligned into a straight column, whatever the depth, so
		// comparing magnitudes down a branch is a single vertical scan -- which
		// is the only thing a rollup is for.
		//
		// Stepping the count leftward as the tree deepens was the alternative,
		// and it was rejected once the banding existed: indentation already
		// says the depth, and the banding already gives the eye something to
		// track along, so a second encoding of depth would be repeating an
		// answer at the cost of the column.
		//
		// Never dropped. A tree without its rollup is a list of names, which
		// the Items view already gives you.
		{Title: strings.ToUpper(m.unit), Min: 5, Align: table.Right},
	}
}

// SetNodes replaces the forest, keeping folds that still refer to a node.
func (m Model) SetNodes(nodes []Node) Model {
	m.nodes = nodes
	present := map[int64]bool{}
	for _, n := range nodes {
		present[n.ID] = true
	}
	for id := range m.collapsed {
		if !present[id] {
			delete(m.collapsed, id)
		}
	}
	return m.refresh()
}

// SetFilter narrows the tree to matching nodes AND their ancestors.
//
// The ancestors are the point. A tree filtered to bare matches is a list, and a
// list is what the Items view already is -- what a tree adds is where the thing
// sits, so a match five levels down has to arrive with its path attached.
//
// Filtering also ignores folds. A node hidden inside a fold that matches what
// you typed is a match you cannot see, which reads as the filter being broken.
func (m Model) SetFilter(text string) Model {
	m.filter = strings.ToLower(strings.TrimSpace(text))
	return m.refresh()
}

// Filtered reports whether a filter is in force.
func (m Model) Filtered() bool { return m.filter != "" }

// Counts returns how many nodes are shown and how many exist.
func (m Model) Counts() (shown, total int) {
	rows, _ := m.tbl.Counts()
	return rows, len(m.nodes)
}

// SetOverlay draws lines after the cursor's row, which is how the editor opens
// inside the tree rather than beneath it.
func (m Model) SetOverlay(lines []string) Model {
	m.tbl = m.tbl.SetOverlay(lines)
	return m
}

// SetSize passes the terminal on to the table.
func (m Model) SetSize(width, height int) Model {
	m.tbl = m.tbl.SetSize(width, height)
	return m
}

// View renders the tree.
func (m Model) View() string { return m.tbl.View() }

// Current is the node under the cursor.
func (m Model) Current() (Node, bool) {
	row, ok := m.tbl.Current()
	if !ok {
		return Node{}, false
	}
	return m.node(row.Key)
}

// Selected returns the keys of the picked nodes.
func (m Model) Selected() []int64 { return m.tbl.Selected() }

// Nodes returns the whole forest, so a caller can map picked keys back to what
// they name.
func (m Model) Nodes() []Node { return m.nodes }

// SelectionCount is how many nodes were explicitly picked.
func (m Model) SelectionCount() int { return m.tbl.SelectionCount() }

// Update handles a keystroke, reporting what it did not use so the application
// still sees its own keys.
//
// The fold gestures are taken BEFORE the table sees them, because the table
// reads z as the start of zz and h and l as column motion -- both of which mean
// something else in a tree.
func (m Model) Update(msg tea.KeyMsg) (Model, bool) {
	switch msg.String() {
	case "h", "left":
		return m.ascend(), true
	case "l", "right":
		return m.descend(), true
	}

	// za, zR, zM are z-prefixed and so is the table's zz. The table is asked
	// first only for the keys it alone owns.
	if handled, next, ok := m.foldGesture(msg); ok {
		return next, handled
	}

	next, handled := m.tbl.Update(msg)
	m.tbl = next
	return m, handled
}

// foldGesture recognises z followed by a, R, or M, and lets zz through to the
// table's centring.
func (m Model) foldGesture(msg tea.KeyMsg) (bool, Model, bool) {
	if m.pendingZ {
		key := msg.String()
		m.pendingZ = false
		switch key {
		case "a":
			return true, m.toggleFold(), true
		case "R":
			return true, m.expandAll(), true
		case "M":
			return true, m.collapseAll(), true
		}
		// Not a fold gesture. Hand both keys to the table so zz still centres.
		m.tbl, _ = m.tbl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
		next, handled := m.tbl.Update(msg)
		m.tbl = next
		return handled, m, true
	}
	if msg.String() == "z" {
		m.pendingZ = true
		return true, m, true
	}
	return false, m, false
}

// expandAll and collapseAll both move rows ABOVE the cursor, which is the case
// folding at the cursor never produces -- so both have to put the cursor back
// by identity, and collapseAll has to settle for the nearest ancestor that is
// still on screen.
//
// Without that, zM from five levels down landed on whatever row happened to
// take that index, and zR from a root landed on its own grandchild. Both were
// the cursor sliding onto whatever moved under it, which is the failure folding
// is careful about everywhere else.
func (m Model) expandAll() Model {
	anchor, ok := m.Current()
	m.collapsed = map[int64]bool{}
	m = m.refresh()
	if ok {
		m = m.focusVisible(anchor.ID)
	}
	return m
}

func (m Model) collapseAll() Model {
	anchor, ok := m.Current()
	m.collapsed = map[int64]bool{}
	for _, n := range m.nodes {
		if m.hasChildren(n) {
			m.collapsed[n.ID] = true
		}
	}
	m = m.refresh()
	if ok {
		m = m.focusVisible(anchor.ID)
	}
	return m
}

// toggleFold folds or unfolds the node under the cursor.
//
// The cursor stays on that node rather than on its index: folding a subtree
// removes rows beneath the cursor, and a cursor that follows the index would
// slide onto whatever moved up under it.
func (m Model) toggleFold() Model {
	current, ok := m.Current()
	if !ok || !m.hasChildren(current) {
		return m
	}
	if m.collapsed[current.ID] {
		delete(m.collapsed, current.ID)
	} else {
		m.collapsed[current.ID] = true
	}
	return m.refresh().focus(current.ID)
}

// ascend collapses an open node, or moves to its parent when there is nothing
// to close.
//
// Collapse-then-move rather than jumping straight up, because h is used far
// more often to tidy away a subtree you have finished with than to travel.
func (m Model) ascend() Model {
	current, ok := m.Current()
	if !ok {
		return m
	}
	if m.hasChildren(current) && !m.collapsed[current.ID] {
		m.collapsed[current.ID] = true
		return m.refresh().focus(current.ID)
	}
	if parent, ok := m.parentOf(current); ok {
		return m.focus(parent.ID)
	}
	return m
}

// descend opens a closed node, or steps into its first child.
func (m Model) descend() Model {
	current, ok := m.Current()
	if !ok || !m.hasChildren(current) {
		return m
	}
	if m.collapsed[current.ID] {
		delete(m.collapsed, current.ID)
		return m.refresh().focus(current.ID)
	}
	if child, ok := m.firstChildOf(current); ok {
		return m.focus(child.ID)
	}
	return m
}

// refresh rebuilds the visible rows from the fold state.
func (m Model) refresh() Model {
	m.tbl = m.tbl.SetRows(m.rows())
	return m
}

// rows renders the visible nodes, skipping everything inside a fold.
func (m Model) rows() []table.Row {
	if m.filter != "" {
		return m.filteredRows()
	}
	var out []table.Row
	skipBelow := -1
	for _, n := range m.nodes {
		if skipBelow >= 0 {
			if n.Depth > skipBelow {
				continue
			}
			skipBelow = -1
		}
		out = append(out, table.Row{
			Key:   n.ID,
			Cells: []string{m.label(n), fmt.Sprintf("%d", n.Count)},
		})
		if m.collapsed[n.ID] {
			skipBelow = n.Depth
		}
	}
	return out
}

// filteredRows keeps the nodes that match and every ancestor above them.
func (m Model) filteredRows() []table.Row {
	keep := make([]bool, len(m.nodes))
	for i, n := range m.nodes {
		if !strings.Contains(strings.ToLower(n.Name), m.filter) {
			continue
		}
		keep[i] = true
		// Walk up by depth: the ancestors of node i are the nearest preceding
		// nodes of each shallower depth.
		want := n.Depth - 1
		for j := i - 1; j >= 0 && want >= 0; j-- {
			if m.nodes[j].Depth == want {
				keep[j] = true
				want--
			}
		}
	}
	var out []table.Row
	for i, n := range m.nodes {
		if !keep[i] {
			continue
		}
		out = append(out, table.Row{
			Key: n.ID,
			// No fold marker while filtering: what is shown is what matched,
			// not what is open, and a marker would claim otherwise.
			Cells: []string{strings.Repeat(indent, n.Depth) + "  " + n.Name, fmt.Sprintf("%d", n.Count)},
		})
	}
	return out
}

// label is the fold marker, the indentation, and the name.
//
// The marker comes FIRST, before the indentation, so the markers of siblings
// line up with each other rather than with their names -- which is what lets
// the eye find what is foldable without reading.
func (m Model) label(n Node) string {
	marker := markerLeaf
	if m.hasChildren(n) {
		marker = markerExpanded
		if m.collapsed[n.ID] {
			marker = markerCollapsed
		}
	}
	return strings.Repeat(indent, n.Depth) + marker + " " + n.Name
}

func (m Model) hasChildren(n Node) bool {
	for i, candidate := range m.nodes {
		if candidate.ID != n.ID {
			continue
		}
		return i+1 < len(m.nodes) && m.nodes[i+1].Depth > n.Depth
	}
	return false
}

func (m Model) parentOf(n Node) (Node, bool) {
	at := m.indexOf(n.ID)
	for i := at - 1; i >= 0; i-- {
		if m.nodes[i].Depth < n.Depth {
			return m.nodes[i], true
		}
	}
	return Node{}, false
}

func (m Model) firstChildOf(n Node) (Node, bool) {
	at := m.indexOf(n.ID)
	if at < 0 || at+1 >= len(m.nodes) {
		return Node{}, false
	}
	if m.nodes[at+1].Depth > n.Depth {
		return m.nodes[at+1], true
	}
	return Node{}, false
}

func (m Model) indexOf(id int64) int {
	for i, n := range m.nodes {
		if n.ID == id {
			return i
		}
	}
	return -1
}

func (m Model) node(id int64) (Node, bool) {
	if i := m.indexOf(id); i >= 0 {
		return m.nodes[i], true
	}
	return Node{}, false
}

// Focus puts the cursor on a node by identity, for a caller arriving from
// somewhere else -- a jump that has just landed in this view.
func (m Model) Focus(id int64) Model { return m.focusVisible(id) }

// focus puts the cursor on a node by identity, which is what keeps it in place
// when folding changes how many rows there are.
func (m Model) focus(id int64) Model {
	for i, row := range m.tbl.Rows() {
		if row.Key == id {
			m.tbl = m.tbl.SetCursor(i)
			return m
		}
	}
	return m
}

// focusVisible puts the cursor on a node, or on the nearest ancestor that is
// still on screen when the node itself has been folded away.
//
// Where you WERE is the useful answer when where you were is no longer shown:
// after collapsing everything, the root of the branch you were reading is the
// row you want, not the row that inherited your index.
func (m Model) focusVisible(id int64) Model {
	node, ok := m.node(id)
	for ok {
		for i, row := range m.tbl.Rows() {
			if row.Key == node.ID {
				m.tbl = m.tbl.SetCursor(i)
				return m
			}
		}
		node, ok = m.parentOf(node)
	}
	return m
}
