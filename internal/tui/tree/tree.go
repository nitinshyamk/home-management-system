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

	"home-management-system/internal/tui/keys"

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
	// Kind says what this node IS. A tree used to hold only its own kind; it
	// can now hold what its nodes CONTAIN, and the difference is load-bearing
	// rather than cosmetic -- see Key.
	Kind string
	// Measure is what a contained node says for itself where a container shows
	// its rollup: "120 g", "1 held".
	Measure string
}

// Key identifies a node across kinds.
//
// The bare ID cannot: a Category and an Item are numbered from different
// tables, so Category 7 and Item 7 collide -- and the ID is the fold key, the
// selection key, and the identity the cursor is restored by. Folding one would
// fold the other, and selecting one would select both.
//
// The kind goes in the high bits, reversibly, because landing a jump and
// restoring a cursor both need the ID back out again. SQLite rowids are
// nowhere near 2^56, so nothing here is close to overflowing.
func (n Node) Key() int64 { return kindBit(n.Kind)<<56 | n.ID }

func kindBit(kind string) int64 {
	switch kind {
	case "Location":
		return 1
	case "Item":
		return 2
	case "Holding":
		return 3
	}
	return 0 // Category, and anything that forgot to say
}

// Contained reports a node that is INSIDE its parent rather than part of the
// tree's own hierarchy -- an Item under a Category, a Holding under a Location.
//
// Everything that acts on a tree row asks this first. A contained row is shown,
// folded and filtered like any other, and is a valid target for nothing.
func (n Node) Contained() bool { return n.Kind == "Item" || n.Kind == "Holding" }

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
	// kids is "does this node have anything under it", built once per SetNodes.
	//
	// It is read for every rendered row, and the answer used to be a scan of
	// the whole forest -- so rendering was quadratic before any of this. That
	// was survivable for a dozen locations and is not once every holding in the
	// house is a node.
	kids map[int64]bool
	// targeting says something is being carried and this tree is being read
	// for somewhere to put it down.
	//
	// It lives on the tree rather than being passed to rows(), because it
	// changes what the CURSOR may do as well as how a row is drawn, and the
	// two have to agree: a row drawn as ruled out that the cursor still lands
	// on is worse than either alone -- it says no and then invites you to try.
	targeting bool
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

// Folds is which nodes are collapsed, so a caller rebuilding the tree can hand
// the answer back.
//
// The tree is rebuilt from scratch on every load -- after a write, a refresh, a
// display-mode toggle -- and without this every one of those silently unfolded
// the whole house. It showed up the moment the trees learned to show their
// contents: asking for them reloaded, the reload discarded the folds, and a
// tree somebody had carefully collapsed sprang open with every holding in it.
func (m Model) Folds() map[int64]bool {
	out := make(map[int64]bool, len(m.collapsed))
	for key, folded := range m.collapsed {
		out[key] = folded
	}
	return out
}

// WithFolds restores fold state. SetNodes prunes whatever no longer refers to a
// node, so a stale fold is dropped rather than kept against nothing.
func (m Model) WithFolds(folds map[int64]bool) Model {
	m.collapsed = make(map[int64]bool, len(folds))
	for key, folded := range folds {
		m.collapsed[key] = folded
	}
	return m
}

// SetNodes replaces the forest, keeping folds that still refer to a node.
func (m Model) SetNodes(nodes []Node) Model {
	m.nodes = nodes
	present := make(map[int64]bool, len(nodes))
	m.kids = make(map[int64]bool, len(nodes))
	for i, n := range nodes {
		present[n.Key()] = true
		// A node's descendants are exactly the nodes that follow it with a
		// greater depth, so having any is a single look at the next one.
		m.kids[n.Key()] = i+1 < len(nodes) && nodes[i+1].Depth > n.Depth
	}
	for key := range m.collapsed {
		if !present[key] {
			delete(m.collapsed, key)
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

// Targeting rules the contained rows out, or lets them back in.
//
// A carry is looking for a CONTAINER: a Holding goes to a Location, an Item is
// filed under a Category. The items and holdings a tree shows are the things
// those containers hold, so while something is in hand they are not places --
// and until this existed the tree said nothing about that. You walked the
// cursor onto one, pressed the put key, and were told no by a refusal, which
// is a conversation the screen could have had by itself.
//
// So the tree says it in advance: the rows go faint and the cursor steps over
// them to the next place something can actually go.
//
// Contained-ness is the test rather than the kind of thing in hand, and it is
// the same test the put itself makes -- see destination() in the application.
// A Holding offered a Category is a mismatch worth a sentence; a Holding
// offered another Holding is not a near-miss at all.
func (m Model) Targeting(on bool) Model {
	if m.targeting == on {
		return m
	}
	m.targeting = on
	// Through refresh, so the cursor is moved off a row that has just stopped
	// being a candidate by the same code that keeps it off one -- the table's
	// own settling, rather than a second answer here.
	return m.refresh()
}

// Counts returns how many nodes are shown and how many exist.
// Counts is how many nodes are shown and how many exist.
//
// CONTAINERS only, both times. The footer says "13 locations", and a count that
// grew to 40 the moment the holdings were shown would be answering a different
// question with the same words.
func (m Model) Counts() (shown, total int) {
	for _, n := range m.nodes {
		if !n.Contained() {
			total++
		}
	}
	for _, row := range m.tbl.Rows() {
		if node, ok := m.node(row.Key); ok && !node.Contained() {
			shown++
		}
	}
	return shown, total
}

// SetOverlay draws lines after the cursor's row, which is how the editor opens
// inside the tree rather than beneath it.
// Focused says whether this tree has the keyboard, which it hands to the
// table underneath: the cursor mark means "a keystroke lands here", and two
// of them on one screen is two claims about that.
func (m Model) Focused(on bool) Model {
	m.tbl = m.tbl.Focused(on)
	return m
}

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
// The tree takes only the keys it means something different by, and hands the
// rest to the table underneath. C-b and C-f are ascend and descend here rather
// than column motion, because a tree has depth where a table has columns; tab
// folds, the way it cycles an outline in org-mode.
func (m Model) Update(msg tea.KeyMsg) (Model, bool) {
	switch keys.Lookup(keys.Tree, msg) {
	case keys.MoveLeft:
		return m.ascend(), true
	case keys.MoveRight:
		return m.descend(), true
	case keys.FoldToggle:
		return m.toggleFold(), true
	case keys.FoldCycleAll:
		// One key for what used to be two gestures. Everything-collapsed and
		// everything-expanded are the only two states worth reaching from here,
		// and which one you want is always the one you are not in -- so the key
		// can read the tree instead of asking.
		if len(m.collapsed) > 0 {
			return m.expandAll(), true
		}
		return m.collapseAll(), true
	}

	next, handled := m.tbl.Update(msg)
	m.tbl = next
	return m, handled
}

// expandAll and collapseAll both move rows ABOVE the cursor, which is the case
// folding at the cursor never produces -- so both have to put the cursor back
// by identity, and collapseAll has to settle for the nearest ancestor that is
// still on screen.
//
// Without that, collapsing everything from five levels down landed on whatever
// row happened to take that index, and expanding everything from a root landed
// on its own grandchild. Both were
// the cursor sliding onto whatever moved under it, which is the failure folding
// is careful about everywhere else.
func (m Model) expandAll() Model {
	anchor, ok := m.Current()
	m.collapsed = map[int64]bool{}
	m = m.refresh()
	if ok {
		m = m.focusVisible(anchor.Key())
	}
	return m
}

func (m Model) collapseAll() Model {
	anchor, ok := m.Current()
	m.collapsed = map[int64]bool{}
	for _, n := range m.nodes {
		if m.hasChildren(n) {
			m.collapsed[n.Key()] = true
		}
	}
	m = m.refresh()
	if ok {
		m = m.focusVisible(anchor.Key())
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
	if m.collapsed[current.Key()] {
		delete(m.collapsed, current.Key())
	} else {
		m.collapsed[current.Key()] = true
	}
	return m.refresh().focus(current.Key())
}

// ascend collapses an open node, or moves to its parent when there is nothing
// to close.
//
// Collapse-then-move rather than jumping straight up, because C-b is used far
// more often to tidy away a subtree you have finished with than to travel.
func (m Model) ascend() Model {
	current, ok := m.Current()
	if !ok {
		return m
	}
	if m.hasChildren(current) && !m.collapsed[current.Key()] {
		m.collapsed[current.Key()] = true
		return m.refresh().focus(current.Key())
	}
	if parent, ok := m.parentOf(current); ok {
		return m.focus(parent.Key())
	}
	return m
}

// descend opens a closed node, or steps into its first child.
func (m Model) descend() Model {
	current, ok := m.Current()
	if !ok || !m.hasChildren(current) {
		return m
	}
	if m.collapsed[current.Key()] {
		delete(m.collapsed, current.Key())
		return m.refresh().focus(current.Key())
	}
	if child, ok := m.firstChildOf(current); ok {
		return m.focus(child.Key())
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
			Key:    n.Key(),
			Cells:  []string{m.label(n), measure(n)},
			Accent: n.Contained(),
			Inert:  m.targeting && n.Contained(),
		})
		if m.collapsed[n.Key()] {
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
			Key: n.Key(),
			// No fold marker while filtering: what is shown is what matched,
			// not what is open, and a marker would claim otherwise.
			Cells:  []string{strings.Repeat(indent, n.Depth) + "  " + n.Name, measure(n)},
			Accent: n.Contained(),
			Inert:  m.targeting && n.Contained(),
		})
	}
	return out
}

// measure is what the second column says for a node.
//
// A container shows its ROLLUP -- how many things are under it -- and a
// contained node shows its own quantity, which is the only number it has. The
// column carries two meanings, and that is the honest reading of it: the header
// says HOLDINGS, and "120 g" is what that holding is. The alternative was a
// bare 0 on every contained row, which is a number that means nothing.
func measure(n Node) string {
	if n.Contained() {
		return n.Measure
	}
	return fmt.Sprintf("%d", n.Count)
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
		if m.collapsed[n.Key()] {
			marker = markerCollapsed
		}
	}
	return strings.Repeat(indent, n.Depth) + marker + " " + n.Name
}

// hasChildren reads a table built once per SetNodes.
//
// It used to scan the whole forest for the node, and it is called for every
// rendered row -- so the render was already quadratic. That was survivable
// while a forest was a dozen locations; it is not once every holding in the
// house is a node too.
func (m Model) hasChildren(n Node) bool { return m.kids[n.Key()] }

func (m Model) parentOf(n Node) (Node, bool) {
	at := m.indexOf(n.Key())
	for i := at - 1; i >= 0; i-- {
		if m.nodes[i].Depth < n.Depth {
			return m.nodes[i], true
		}
	}
	return Node{}, false
}

// firstChildOf is the first child the cursor may step down onto.
//
// A scan rather than a look at the next node, because a container's own
// contents are spliced in AHEAD of its child containers -- so a category with
// items in it has an item as its first child, and while a carry is in hand
// that is a row C-f must step past rather than land on. Without this, descend
// aimed at a row the table then refused to put the cursor on, and the key did
// nothing at all.
func (m Model) firstChildOf(n Node) (Node, bool) {
	at := m.indexOf(n.Key())
	if at < 0 {
		return Node{}, false
	}
	for i := at + 1; i < len(m.nodes) && m.nodes[i].Depth > n.Depth; i++ {
		if m.nodes[i].Depth != n.Depth+1 {
			continue // a grandchild, reached only through its own parent
		}
		if m.targeting && m.nodes[i].Contained() {
			continue
		}
		return m.nodes[i], true
	}
	return Node{}, false
}

func (m Model) indexOf(key int64) int {
	for i, n := range m.nodes {
		if n.Key() == key {
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
			if row.Key == node.Key() {
				m.tbl = m.tbl.SetCursor(i)
				return m
			}
		}
		node, ok = m.parentOf(node)
	}
	return m
}
