package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/style"
	"home-management-system/internal/tui/table"
)

// Moving a thing, as one gesture.
//
// It used to be two, and neither was good. Pointing at the destination meant
// M-w here, a tab switch, walking a tree, C-y there -- with the thing you
// were moving off screen for the middle of it. Naming the destination meant
// knowing the name. And a PLACE could not be moved by either: re-parenting a
// shelf was `M-x reparent location`, typed, for the same idea.
//
// Now `m` opens a list of destinations below the house, with what you are
// carrying at the top of it and the rail still visible behind. Typing filters
// by full path, which is the naming half; the arrows are the pointing half.
// One gesture, and the same one whether the thing is a jar, a kind of thing,
// or the shelf itself.

// moving is a move in progress.
type moving struct {
	// what is being carried, and what kind of destination it takes.
	kind  string
	ids   []int64
	what  string
	from  string
	wants resolve.Kind

	all    []resolve.Candidate
	filter string
	tbl    table.Model

	// excluded is the carried node and everything under it, computed when the
	// move starts because it is read off the rail, which the candidates
	// arriving later do not carry.
	excluded map[int64]bool

	width int
	// shown is how many lines the list of destinations takes.
	shown int
}

// startMoving picks up whatever the cursor is on and asks where it goes.
func (m Model) startMoving() (Model, tea.Cmd) {
	sel, ok := m.current.Current()
	if !ok {
		return m.refuse("nothing here to move"), nil
	}

	mv := moving{what: sel.Name, kind: sel.Kind}
	switch sel.Kind {
	case kindHolding:
		mv.wants = resolve.KindLocation
		rows := m.selectedHoldings()
		if len(rows) == 0 {
			return m.refuse("nothing here to move"), nil
		}
		for _, r := range rows {
			mv.ids = append(mv.ids, int64(r.ID))
		}
		if len(rows) > 1 {
			mv.what = fmt.Sprintf("%d holdings", len(rows))
		}
		mv.from = rows[0].LocationPath

	case kindItem:
		mv.wants = resolve.KindCategory
		mv.ids = []int64{sel.ID}
		if item, ok := m.item(domain.ItemID(sel.ID)); ok {
			mv.from = item.Category
		}

	case kindLocation, kindCategory:
		if m.atRailRoot() {
			return m.refuse("the house is not inside anything"), nil
		}
		mv.wants = m.lens.spec().rail
		mv.ids = []int64{sel.ID}
		mv.from = m.parentPath()

	default:
		return m.refuse("nothing here to move"), nil
	}

	// A node cannot go inside itself or anything under it. That is the one
	// rule here about correctness rather than tidiness: the tree would stop
	// being a tree, and the cycle guard would refuse it after the fact.
	// Ruling the rows out beats refusing the answer.
	mv.excluded = map[int64]bool{}
	if mv.kind == kindLocation || mv.kind == kindCategory {
		for _, id := range m.subtreeOf(mv.ids[0]) {
			mv.excluded[id] = true
		}
	}

	// The destinations arrive with the candidates, which are read fresh --
	// they have to be, because the tree they describe is what the move is
	// about, and a stale one would offer a shelf that is no longer there.
	mv.width = m.width
	m = m.open(&mv)
	m.say = m.say.Working(fmt.Sprintf("where does %q go?", mv.what))
	return m, m.loadCandidates()
}

// withDestinations fills a move in progress from the candidate index.
func (mv moving) withDestinations(all []resolve.Candidate) moving {
	mv.all = nil
	for _, c := range all {
		if c.Kind == mv.wants && !c.Archived && !mv.excluded[c.ID] {
			mv.all = append(mv.all, c)
		}
	}
	return mv.refresh()
}

// subtreeOf is a rail node and everything under it, by identifier.
func (m Model) subtreeOf(id int64) []int64 {
	sh, ok := m.shell()
	if !ok {
		return []int64{id}
	}
	nodes := sh.rail.Nodes()
	at := -1
	for i, n := range nodes {
		if n.ID == id {
			at = i
			break
		}
	}
	if at < 0 {
		return []int64{id}
	}
	out := []int64{id}
	for i := at + 1; i < len(nodes) && nodes[i].Depth > nodes[at].Depth; i++ {
		out = append(out, nodes[i].ID)
	}
	return out
}

// parentPath is where the rail's node sits now, for the banner.
func (m Model) parentPath() string {
	sh, ok := m.shell()
	if !ok {
		return ""
	}
	node, ok := sh.railNode()
	if !ok {
		return ""
	}
	path := sh.pathTo(node.Key())
	if len(path) < 2 {
		return ""
	}
	return strings.Join(path[:len(path)-1], pathSeparator)
}

// refresh rebuilds the visible destinations from the filter.
func (mv moving) refresh() moving {
	var rows []table.Row
	for _, c := range mv.all {
		if mv.filter != "" && !strings.Contains(strings.ToLower(c.Path), strings.ToLower(mv.filter)) {
			continue
		}
		rows = append(rows, table.Row{Key: c.ID, Cells: []string{c.Path}})
	}
	mv.shown = min(len(rows)+1, 9)
	mv.tbl = table.New([]table.Column{{Title: "", Min: 12, Grow: true}}).
		Fixed().Headerless().SetRows(rows).SetSize(mv.width, mv.shown)
	return mv
}

// chosen is the destination under the cursor.
func (mv moving) chosen() (int64, bool) {
	row, ok := mv.tbl.Current()
	if !ok {
		return 0, false
	}
	return row.Key, true
}

// handleMoving takes the keystroke while the destinations are up.
func (m Model) handleMoving(mv *moving, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch keys.Lookup(keys.Browse, msg) {
	case keys.Cancel:
		m = m.close()
		m.say = m.say.Report("put it down; nothing moved")
		return m, nil
	case keys.Confirm:
		return m.commitMove(mv)
	}
	switch msg.Type {
	case tea.KeyBackspace:
		mv.filter = trimRune(mv.filter)
		*mv = mv.refresh()
		return m, nil
	case tea.KeyRunes, tea.KeySpace:
		// Typing filters by path, which is the naming half of the gesture.
		// It is the same field the old prompt was, without being a different
		// screen from the pointing half.
		if !msg.Alt {
			mv.filter += string(msg.Runes)
			if msg.Type == tea.KeySpace {
				mv.filter += " "
			}
			*mv = mv.refresh()
			return m, nil
		}
	}
	next, _ := mv.tbl.Update(msg)
	mv.tbl = next
	return m, nil
}

func trimRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

// commitMove turns the carried thing and the chosen destination into
// Commands -- one per thing, so a multi-row move is one transaction.
func (m Model) commitMove(mv *moving) (Model, tea.Cmd) {
	to, ok := mv.chosen()
	if !ok {
		return m.refuse("no destination chosen"), nil
	}
	m = m.close()

	var cmds []command.Command
	switch mv.kind {
	case kindHolding:
		for _, id := range mv.ids {
			cmds = append(cmds, command.Move{
				Holding: domain.HoldingID(id), To: domain.LocationID(to),
			})
		}
	case kindItem:
		for _, id := range mv.ids {
			cmds = append(cmds, command.Reclassify{
				Item: domain.ItemID(id), Category: domain.CategoryID(to),
			})
		}
	case kindLocation:
		parent := domain.LocationID(to)
		cmds = append(cmds, command.ReparentLocation{
			Location: domain.LocationID(mv.ids[0]), Parent: &parent,
		})
	case kindCategory:
		parent := domain.CategoryID(to)
		cmds = append(cmds, command.ReparentCategory{
			Category: domain.CategoryID(mv.ids[0]), Parent: &parent,
		})
	}
	return m, m.runCommands(cmds)
}

func (mv *moving) name() string           { return "destinations" }
func (mv *moving) height(Model) int       { return mv.shown + 1 } // and its heading
func (mv *moving) lines(m Model) []string { return mv.view(m.width) }

func (mv *moving) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	return m.handleMoving(mv, msg)
}

func (mv *moving) keys(Model) string {
	// Worded here rather than taken from the keymap: enter is "history"
	// everywhere else, and a line that said so under a list of shelves would
	// be naming the wrong thing entirely.
	return keys.Show(keys.Browse, keys.Confirm) + " put it there - " +
		keys.Show(keys.Browse, keys.Cancel) + " put it down - type to narrow"
}

// view draws the destinations, headed by what is in hand.
func (mv moving) view(width int) []string {
	goes := "goes in a place"
	switch mv.wants {
	case resolve.KindCategory:
		goes = "is filed under a classification"
	}
	head := style.Dim.Render("MOVE  ") + style.Strong.Render(mv.what) +
		style.Dim.Render("   "+goes)
	if mv.from != "" {
		head += style.Dim.Render("   from " + mv.from)
	}
	if mv.filter != "" {
		head += style.Dim.Render("   matching ") + style.Strong.Render(mv.filter)
	}
	return append([]string{head}, lines(mv.tbl.View())...)
}
