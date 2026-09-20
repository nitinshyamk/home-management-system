package tui

import (
	"maps"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/omnibox"
	"home-management-system/internal/tui/style"
	"home-management-system/internal/tui/table"
	"home-management-system/internal/tui/tree"
)

// The shell: a rail of structure beside the contents of whatever it points at.
// One surface, so the field, the filter and the overlay work here unchanged.

// narrowWidth is where two panes stop fitting and the shell shows one at a
// time. The breadcrumb is in the header at every width, so the hidden pane is
// never the only thing that said where you are.
const narrowWidth = 76

// railWidth: two fifths, bounded. The rail indents two columns per level and
// the house is five deep, so a third of eighty columns is a row of ellipses.
func railWidth(total int) int {
	w := total * 2 / 5
	return min(max(w, 22), 36)
}

// pane says which half has the cursor.
type pane int

const (
	paneRail pane = iota
	paneBody
)

type shellSurface struct {
	lens lens
	rail tree.Model
	body table.Model
	on   pane

	width, height int
	// kinds is what each contents row IS, by key, since the cells are strings.
	kinds map[int64]string
}

func (s shellSurface) with(f func(*shellSurface)) surface {
	f(&s)
	return s
}

// ---------------------------------------------------------------------------
// Keys
// ---------------------------------------------------------------------------

// Update gives the keystroke to the half with the cursor, after the two keys
// that cross between them.
//
// Left and right are the pane switch rather than the tree's ascend and
// descend: beside a contents pane, rightward means into what is in there.
// Depth is tab-to-fold then step, as in any outline.
func (s shellSurface) Update(msg tea.KeyMsg) (surface, bool) {
	if s.on == paneRail {
		// The Tree keymap: Browse binds no left or right at all.
		if keys.Lookup(keys.Tree, msg) == keys.MoveRight && len(s.body.Rows()) > 0 {
			s.on = paneBody
			return s.focus(), true
		}
		next, handled := s.rail.Update(msg)
		s.rail = next
		return s, handled
	}
	// Out of the first column only; before that C-b is column motion, which
	// is how a column is chosen to sort by.
	if keys.Lookup(keys.Table, msg) == keys.MoveLeft && s.body.FocusedColumn() == 0 {
		s.on = paneRail
		return s.focus(), true
	}
	next, handled := s.body.Update(msg)
	s.body = next
	return s, handled
}

// railAt is the key the rail's cursor is on, which the model watches: the rail
// moving is what changes the other half.
func (s shellSurface) railAt() int64 {
	if n, ok := s.rail.Current(); ok {
		return n.Key()
	}
	return 0
}

func (s shellSurface) railNode() (tree.Node, bool) { return s.rail.Current() }

// atRoot reports the rail sitting on the synthetic top. It is not a real
// node: nothing can be created inside it, renamed, or moved, and offering it
// as a parent would file a new category under a name no table has.
func (s shellSurface) atRoot() bool {
	n, ok := s.rail.Current()
	return ok && n.ID == rootID
}

// ---------------------------------------------------------------------------
// Drawing
// ---------------------------------------------------------------------------

func (s shellSurface) View() string {
	if s.narrow() {
		if s.on == paneRail {
			return s.rail.View()
		}
		return s.body.View()
	}
	return sideBySide(lines(s.rail.View()), railWidth(s.width), lines(s.body.View()), s.height)
}

func (s shellSurface) narrow() bool { return s.width < narrowWidth }

func lines(s string) []string { return strings.Split(s, "\n") }

// sideBySide lays two blocks out in columns with a rule between them. Written
// here rather than taken from lipgloss because the padding is what the golden
// frames record.
func sideBySide(left []string, leftWidth int, right []string, height int) string {
	n := max(len(left), len(right))
	if height > 0 && n > height {
		n = height
	}
	rule := style.Dim.Render("│")

	var b strings.Builder
	for i := range n {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		b.WriteString(padTo(l, leftWidth))
		b.WriteString(rule)
		b.WriteString(r)
		if i < n-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// padTo pads or cuts to a PRINTABLE width, which is not len() once styled.
func padTo(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return lipgloss.NewStyle().MaxWidth(width).Render(s)
	}
	return s + strings.Repeat(" ", width-w)
}

// focus draws exactly one cursor mark. Called wherever the pane changes and
// wherever the panes are rebuilt.
func (s shellSurface) focus() shellSurface {
	s.rail = s.rail.Focused(s.on == paneRail)
	s.body = s.body.Focused(s.on == paneBody)
	return s
}

func (s shellSurface) SetSize(width, height int) surface {
	return s.with(func(n *shellSurface) {
		n.width, n.height = width, height
		*n = n.focus()
		if n.narrow() {
			n.rail = n.rail.SetSize(width, height)
			n.body = n.body.SetSize(width, height)
			return
		}
		w := railWidth(width)
		n.rail = n.rail.SetSize(w, height)
		n.body = n.body.SetSize(width-w-1, height) // one column for the rule
	})
}

// SetOverlay splices the field into the half holding the row it was opened on.
func (s shellSurface) SetOverlay(l []string) surface {
	return s.with(func(n *shellSurface) {
		if n.on == paneRail {
			n.rail, n.body = n.rail.SetOverlay(l), n.body.SetOverlay(nil)
			return
		}
		n.body, n.rail = n.body.SetOverlay(l), n.rail.SetOverlay(nil)
	})
}

// SetFilter narrows the contents pane. Finding a PLACE by name is the jump
// palette's job, so the two searches stay distinguishable.
func (s shellSurface) SetFilter(q omnibox.Query) surface {
	return s.with(func(n *shellSurface) {
		n.body = n.body.SetFilter(table.Filter{Text: q.Text, Facets: lensFacets(n.lens, q)})
	})
}

// ---------------------------------------------------------------------------
// What the cursor is on
// ---------------------------------------------------------------------------

func (s shellSurface) railSelection(n tree.Node) selection {
	return selection{Key: n.Key(), ID: n.ID, Name: n.Name, Kind: n.Kind, Subject: n.Kind}
}

// bodySelection: Kind is what the row IS, Subject is what a command at it
// names. Both lenses name an Item in their first column.
func (s shellSurface) bodySelection(r table.Row) selection {
	sel := selection{
		Key: r.Key, ID: r.Key, Name: r.Cells[0],
		Kind: s.kinds[r.Key], Subject: kindItem,
	}
	if s.lens == lensPlace && len(r.Cells) > colWhere {
		sel.At = r.Cells[colWhere]
	}
	return sel
}

func (s shellSurface) Current() (selection, bool) {
	if s.on == paneRail {
		node, ok := s.rail.Current()
		if !ok {
			return selection{}, false
		}
		return s.railSelection(node), true
	}
	row, ok := s.body.Current()
	if !ok {
		return selection{}, false
	}
	return s.bodySelection(row), true
}

// Selected belongs to the contents pane. A rail selection would be a set of
// subtrees, and no verb takes one.
func (s shellSurface) Selected() []selection {
	picked := map[int64]bool{}
	for _, key := range s.body.Selected() {
		picked[key] = true
	}
	var out []selection
	for _, row := range s.body.Rows() {
		if picked[row.Key] {
			out = append(out, s.bodySelection(row))
		}
	}
	return out
}

func (s shellSurface) SelectionCount() int     { return s.body.SelectionCount() }
func (s shellSurface) Filtered() bool          { return s.body.Filtered() }
func (s shellSurface) Counts() (int, int)      { return s.body.Counts() }
func (s shellSurface) SortDescription() string { return s.body.SortDescription() }
func (s shellSurface) Folds() map[int64]bool   { return s.rail.Folds() }

// Focus lands on a row in whichever half owns the key. A rail key carries its
// kind in the high bits and a contents key does not, so they cannot collide.
func (s shellSurface) Focus(key int64) surface {
	if i, ok := indexOfRow(s.body, key); ok {
		return s.with(func(n *shellSurface) {
			n.on = paneBody
			n.body = n.body.SetCursor(i)
			*n = n.focus()
		})
	}
	return s.with(func(n *shellSurface) {
		n.on = paneRail
		n.rail = n.rail.Focus(key)
		*n = n.focus()
	})
}

func indexOfRow(t table.Model, key int64) (int, bool) {
	for i, row := range t.Rows() {
		if row.Key == key {
			return i, true
		}
	}
	return 0, false
}

// lensFacets resolves facet names to columns. Only the lens knows that `loc:`
// means the WHERE column here. An unrecognised facet restricts nothing, as
// everywhere else.
func lensFacets(l lens, q omnibox.Query) []table.FacetTest {
	columns := l.spec().facets
	var out []table.FacetTest
	for _, f := range q.Facets {
		if column, ok := columns[f.Key]; ok && !f.Any() {
			out = append(out, table.FacetTest{Column: column, Value: f.Value})
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Walking the rail
// ---------------------------------------------------------------------------

func indexOfNode(nodes []tree.Node, key int64) int {
	for i, n := range nodes {
		if n.Key() == key {
			return i
		}
	}
	return -1
}

// ancestors visits a node's parents, nearest first.
func ancestors(nodes []tree.Node, at int, visit func(tree.Node)) {
	depth := nodes[at].Depth
	for i := at - 1; i >= 0 && depth > 0; i-- {
		if nodes[i].Depth < depth {
			depth = nodes[i].Depth
			visit(nodes[i])
		}
	}
}

// pathTo is the names from the top of the rail down to a node.
func (s shellSurface) pathTo(key int64) []string {
	nodes := s.rail.Nodes()
	at := indexOfNode(nodes, key)
	if at < 0 {
		return nil
	}
	out := []string{nodes[at].Name}
	ancestors(nodes, at, func(n tree.Node) { out = append([]string{n.Name}, out...) })
	return out
}

// childCount is how many nodes sit directly inside one.
func (s shellSurface) childCount(key int64) int {
	nodes := s.rail.Nodes()
	at := indexOfNode(nodes, key)
	if at < 0 {
		return 0
	}
	n := 0
	for i := at + 1; i < len(nodes) && nodes[i].Depth > nodes[at].Depth; i++ {
		if nodes[i].Depth == nodes[at].Depth+1 {
			n++
		}
	}
	return n
}

// reveal opens the one branch leading to a node, so a jump or a lens flip
// lands on its target rather than the nearest ancestor still visible. Folds
// otherwise survive every reload.
func reveal(nodes []tree.Node, key int64, folds map[int64]bool) map[int64]bool {
	at := indexOfNode(nodes, key)
	if at < 0 || len(folds) == 0 {
		return folds
	}
	out := maps.Clone(folds)
	ancestors(nodes, at, func(n tree.Node) { delete(out, n.Key()) })
	return out
}
