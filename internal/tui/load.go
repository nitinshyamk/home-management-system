package tui

import (
	"fmt"
	"slices"

	"home-management-system/internal/app"
	"home-management-system/internal/domain"
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/style"
	"home-management-system/internal/tui/table"
	"home-management-system/internal/tui/tree"

	tea "github.com/charmbracelet/bubbletea"
)

// Loading: turning what the Controller knows into a surface to draw.

func (m Model) loadCandidates() tea.Cmd {
	return func() tea.Msg {
		index, err := m.ctrl.SearchIndex(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		units, err := m.ctrl.Units(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		return candidatesMsg{candidates: index.All(), units: units}
	}
}

func (m Model) load(v view) tea.Cmd {
	return func() tea.Msg {
		if spec(v).kind == surfaceShell {
			sh, err := m.renderShell()
			if err != nil {
				return errMsg{err}
			}
			sh.view = v
			return sh
		}
		rows, hint, err := m.render(v, 0)
		if err != nil {
			return errMsg{err}
		}
		if v == viewHelp {
			_, headings := m.helpPage(m.helpTopic)
			return loadedMsg{view: v, rows: rows, hint: hint, headings: headings}
		}
		return loadedMsg{view: v, rows: rows, hint: hint}
	}
}

// ---------------------------------------------------------------------------
// The shell
// ---------------------------------------------------------------------------

// rootID is the rail's synthetic top -- the whole house, or the whole
// taxonomy. A forest has no trunk, so "all of it" is the one node the tree
// does not have, and it is where the flat Holdings tab went. No table
// numbers a row 0, so it cannot collide.
const rootID int64 = 0

func rootKey(l lens) int64 {
	return tree.Node{ID: rootID, Kind: string(l.spec().rail)}.Key()
}

// contents is what the pane beside the rail shows.
type contents struct {
	rows     []table.Row
	kinds    map[int64]string
	columns  []table.Column
	holdings map[int64]app.HoldingRow
	items    map[domain.ItemID]app.ItemRow
}

// renderShell reads the rail and the contents of the node it points at, in
// one message: they are one screen, and two loads would be two moments.
func (m Model) renderShell() (shellMsg, error) {
	nodes, total, err := m.railNodes()
	if err != nil {
		return shellMsg{}, err
	}
	at, atRoot := m.railTarget(nodes)

	var inside contents
	if m.lens == lensPlace {
		inside, err = m.holdingsIn(at, atRoot)
	} else {
		inside, err = m.itemsIn(at, atRoot)
	}
	if err != nil {
		return shellMsg{}, err
	}

	return shellMsg{
		nodes: nodes, total: total, contents: inside,
		unit: m.lens.spec().unit,
		hint: keys.Hint(keys.Tree, []keys.Action{keys.FoldToggle}) + " - " +
			keys.Hint(keys.Browse, []keys.Action{keys.LensFlip}, []keys.Action{keys.ToggleDepth}),
	}, nil
}

func (m Model) holdingsIn(at int64, atRoot bool) (contents, error) {
	var root *domain.LocationID
	if !atRoot {
		id := domain.LocationID(at)
		root = &id
	}
	found, err := m.ctrl.HoldingsUnder(m.ctx, root, m.deep)
	if err != nil {
		return contents{}, err
	}

	// Every Item, not only those on screen: the inspector needs the category
	// and the house-wide total, and neither is on a HoldingRow.
	all, err := m.ctrl.Items(m.ctx)
	if err != nil {
		return contents{}, err
	}

	out := contents{
		kinds:    make(map[int64]string, len(found)),
		holdings: make(map[int64]app.HoldingRow, len(found)),
		items:    make(map[domain.ItemID]app.ItemRow, len(all)),
		columns:  fitPlaceColumns(m.lens.spec().columns, found, at, atRoot),
	}
	for _, r := range found {
		out.rows = append(out.rows, table.Row{
			Key:   int64(r.ID),
			Cells: []string{r.Item, r.State, r.Location, r.Note},
			// Shown, never matched, so `loc:` keeps meaning the place itself
			// rather than the branch it hangs from.
			Paths: []string{"", "", r.LocationPath, ""},
		})
		out.kinds[int64(r.ID)] = kindHolding
		out.holdings[int64(r.ID)] = r
	}
	for _, r := range all {
		out.items[r.ID] = r
	}
	return out, nil
}

func (m Model) itemsIn(at int64, atRoot bool) (contents, error) {
	var root *domain.CategoryID
	if !atRoot {
		id := domain.CategoryID(at)
		root = &id
	}
	found, err := m.ctrl.ItemsUnder(m.ctx, root, m.deep)
	if err != nil {
		return contents{}, err
	}

	out := contents{
		kinds:   make(map[int64]string, len(found)),
		items:   make(map[domain.ItemID]app.ItemRow, len(found)),
		columns: fitKindColumns(m.lens.spec().columns, found),
	}
	for _, r := range found {
		where, err := m.whereIs(r.ID)
		if err != nil {
			return contents{}, err
		}
		out.rows = append(out.rows, table.Row{
			Key:   int64(r.ID),
			Cells: []string{r.Name, r.OnHand, where, r.Measure},
		})
		out.kinds[int64(r.ID)] = kindItem
		out.items[r.ID] = r
	}
	return out, nil
}

// fitPlaceColumns drops what this node has nothing to say about. WHERE earns
// its width only when the rows are spread over more than one place -- the
// three-identical-chiles question -- and nothing when they are all filed here.
func fitPlaceColumns(cols []table.Column, rows []app.HoldingRow, at int64, atRoot bool) []table.Column {
	var drop []int

	allHere := !atRoot
	for _, r := range rows {
		if int64(r.LocationID) != at {
			allHere = false
			break
		}
	}
	if allHere {
		drop = append(drop, colWhere)
	}

	if !slices.ContainsFunc(rows, func(r app.HoldingRow) bool { return r.Note != "" }) {
		drop = append(drop, colTail)
	}
	return without(cols, drop...)
}

// fitKindColumns: WHERE always says something in this lens, so only the
// measure can be empty -- and it is, exactly when nothing here is measured.
func fitKindColumns(cols []table.Column, rows []app.ItemRow) []table.Column {
	if slices.ContainsFunc(rows, func(r app.ItemRow) bool { return r.Measure != "" }) {
		return cols
	}
	return without(cols, colTail)
}

// railNodes is the tree with the synthetic root on top of it, and the total
// that root rolls up.
func (m Model) railNodes() ([]tree.Node, int64, error) {
	var rows []app.TreeRow
	var err error
	if m.lens == lensPlace {
		// Never withContents: splicing holdings into the tree is what the
		// contents pane replaced.
		rows, err = m.ctrl.LocationTree(m.ctx, false)
	} else {
		rows, err = m.ctrl.CategoryTree(m.ctx, false)
	}
	if err != nil {
		return nil, 0, err
	}

	spec := m.lens.spec()
	var total int64
	nodes := make([]tree.Node, 0, len(rows)+1)
	nodes = append(nodes, tree.Node{ID: rootID, Name: spec.root, Kind: string(spec.rail)})
	for _, r := range rows {
		// Everything shifts down one, inside the root. Real roots had depth
		// zero and their counts are disjoint, so the total is their sum.
		if r.Depth == 0 {
			total += r.Count
		}
		nodes = append(nodes, tree.Node{
			ID: r.ID, Name: r.Name, Depth: r.Depth + 1,
			Count: r.Count, Kind: r.Kind, Measure: r.Measure,
		})
	}
	nodes[0].Count = total
	return nodes, total, nil
}

// railTarget is the node the rail is pointing at, and whether it is the root.
//
// A remembered position that no longer names a node falls back to the root
// rather than to nothing: a shelf can be deleted while you are away from the
// lens that shows it, and coming back to an empty screen would read as the
// house having been emptied.
func (m Model) railTarget(nodes []tree.Node) (id int64, atRoot bool) {
	key, ok := m.railKey[m.lens]
	if !ok || key == rootKey(m.lens) {
		return rootID, true
	}
	for _, n := range nodes {
		if n.Key() == key {
			return n.ID, n.ID == rootID
		}
	}
	return rootID, true
}

// whereIs says, for an Item, where its holdings are -- the one place when
// there is one, and how many places when there are several.
//
// "3 places" rather than three rows, because in the kind lens the row IS the
// item: one Item with three piles is one thing you own in three places, and
// listing it three times is the flat-table answer this screen replaced.
func (m Model) whereIs(id domain.ItemID) (string, error) {
	rows, err := m.ctrl.HoldingsOfItem(m.ctx, id)
	if err != nil {
		return "", err
	}
	switch len(rows) {
	case 0:
		return "nowhere", nil
	case 1:
		return rows[0].Location, nil
	default:
		return fmt.Sprintf("%d places", len(rows)), nil
	}
}

// railCountTitle is what heads the rail's rollup column.
//
// Nothing. The header line already says which lens is showing and the
// inspector already says what the number counts, so a third statement of it
// would cost three columns of a pane that indents two per level and has a
// five-deep house to draw.
const railCountTitle = ""

// shellFor builds the shell surface from a load.
//
// The rail's folds come back from the model, because the tree is rebuilt from
// scratch on every load and one that sprang open each time would make
// collapsing it pointless.
func (m Model) shellFor(msg shellMsg) shellSurface {
	folds := m.folds[m.lens]
	if key, ok := m.railKey[m.lens]; ok {
		// Open the one branch that leads where the cursor is going, and
		// nothing else. Without it a jump or a lens flip into a folded branch
		// lands on the nearest visible ancestor -- the right answer for a
		// fold you just made, the wrong one for a destination you just named.
		folds = reveal(msg.nodes, key, folds)
	}
	sh := shellSurface{
		lens:  m.lens,
		rail:  tree.New(railCountTitle).WithFolds(folds).SetNodes(msg.nodes),
		body:  table.New(msg.columns).SetRows(msg.rows),
		kinds: msg.kinds,
	}
	if was, ok := m.shell(); ok {
		sh.on = was.on
	}
	if key, ok := m.railKey[m.lens]; ok {
		sh.rail = sh.rail.Focus(key)
	}
	// A shell with nothing in the node it points at cannot have the cursor in
	// the contents pane: there is no row for it to be on.
	if len(msg.rows) == 0 {
		sh.on = paneRail
	}
	return sh.SetSize(m.width, m.bodyHeight()).(shellSurface)
}

// ---------------------------------------------------------------------------
// The screens that are prose
// ---------------------------------------------------------------------------

func (m Model) loadHistory(id domain.HoldingID) tea.Cmd {
	return func() tea.Msg {
		rows, hint, err := m.render(viewHistory, id)
		if err != nil {
			return errMsg{err}
		}
		return loadedMsg{view: viewHistory, rows: rows, hint: hint}
	}
}

// render turns controller data into display rows, for the views that are prose
// rather than a list: Help, Integrity, and History. The Controller returns
// flat, display-ready rows, so this stays formatting rather than logic.
func (m Model) render(v view, subject domain.HoldingID) ([]string, string, error) {
	switch v {

	case viewHelp:
		lines := m.helpLines(m.helpTopic)
		hint := keys.Show(keys.Browse, keys.CommandLine) + " help <topic>"
		if m.helpTopic != "" {
			hint = "help " + m.helpTopic
		}
		return lines, hint, nil

	case viewIntegrity:
		report, err := m.ctrl.Integrity(m.ctx)
		if err != nil {
			return nil, "", err
		}
		var out []string
		out = append(out, fmt.Sprintf("%d holdings checked against the ledger", report.HoldingsChecked))
		if report.Clean() {
			out = append(out, "", "no discrepancies")
		} else {
			out = append(out, "")
			for _, d := range report.Discrepancies {
				out = append(out, style.Strong.Render("DISCREPANCY ")+d)
			}
			for _, o := range report.Orphans {
				out = append(out, style.Strong.Render("ORPHAN      ")+o)
			}
			// Reporting, never repairing: silently correcting would destroy the
			// only signal that a write skipped its event.
			out = append(out, "", style.Dim.Render("reported, not repaired"))
		}

		nudges, err := m.ctrl.Nudges(m.ctx)
		if err != nil {
			return nil, "", err
		}
		if len(nudges) > 0 {
			out = append(out, "", style.Strong.Render("Classification"))
			for _, n := range nudges {
				out = append(out, fmt.Sprintf("  %s sits at %q, which has %d subcategories",
					n.Item, n.Category, n.Siblings))
			}
		}
		hint := "clean"
		if !report.Clean() {
			hint = style.Strong.Render(fmt.Sprintf("%d discrepancies, %d orphans",
				len(report.Discrepancies), len(report.Orphans)))
		}
		return out, hint, nil

	case viewHistory:
		rows, err := m.ctrl.HoldingHistory(m.ctx, subject)
		if err != nil {
			return nil, "", err
		}
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%6d  %s  %-18s %s",
				r.Sequence, r.When.Format("2006-01-02 15:04"), r.Type, r.Summary))
		}
		return out, fmt.Sprintf("holding %d - %d events in sequence order", subject, len(rows)), nil
	}
	return nil, "", fmt.Errorf("unknown view %d", v)
}

// focusKey puts the cursor on a row by identity, and does nothing if that row
// is not here -- a reload can drop the row somebody was on.
func (m Model) focusKey(key int64) Model {
	m.current = m.current.Focus(key)
	return m
}

// keyOf is the key a jump destination has on the surface it lands on.
//
// A rail node keys itself by kind AND identifier, because a Category and a
// Location come from different tables; a contents row keys itself by
// identifier alone. Which of the two a candidate becomes depends on whether it
// is structure or something inside it.
func keyOf(target resolve.Candidate) int64 {
	switch target.Kind {
	case resolve.KindLocation, resolve.KindCategory:
		return tree.Node{ID: target.ID, Kind: string(target.Kind)}.Key()
	default:
		return target.ID
	}
}
