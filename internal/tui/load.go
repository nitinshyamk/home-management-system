package tui

import (
	"fmt"
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
		// One switch on the same fact surfaceFor switches on, so a view cannot
		// be loaded as one shape and then drawn as another.
		switch spec(v).kind {
		case surfaceTree:
			nodes, hint, err := m.renderTree(v)
			if err != nil {
				return errMsg{err}
			}
			return loadedMsg{view: v, nodes: nodes, hint: hint}
		case surfaceTable:
			cells, byKey, hint, err := m.renderTable(v)
			if err != nil {
				return errMsg{err}
			}
			return loadedMsg{view: v, cells: cells, holdingRows: byKey, hint: hint}
		default:
			rows, hint, err := m.render(v, 0)
			if err != nil {
				return errMsg{err}
			}
			return loadedMsg{view: v, rows: rows, hint: hint}
		}
	}
}

// surfaceFor builds the surface a loaded view is drawn on.
//
// A fresh one per load: the columns differ between views, and carrying a
// selection across them would mean acting on rows a person picked while
// looking at something else.
func (m Model) surfaceFor(msg loadedMsg) surface {
	height := m.bodyHeight()
	switch spec(msg.view).kind {
	case surfaceTable:
		return tableSurface{
			model: table.New(spec(msg.view).columns).SetRows(msg.cells).SetSize(m.width, height),
			view:  msg.view,
		}
	case surfaceTree:
		return treeSurface{model: tree.New(spec(msg.view).unit).
			WithFolds(m.folds[msg.view]).
			SetNodes(msg.nodes).SetSize(m.width, height)}
	default:
		return newTextSurface(msg.rows, m.width, height)
	}
}

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
//
// Only those three. The other four had cases here once and every one of them
// was unreachable -- load sends a tree to renderTree and a table to
// renderTable long before it gets this far. Categories and Locations were
// removed when they became trees; Items and Holdings outlived them by two
// stages, quietly drifting to column widths no screen ever showed. A second
// answer to one question does not announce that it has stopped being used,
// which is the argument for spec(v).kind deciding this in one place.
func (m Model) render(v view, subject domain.HoldingID) ([]string, string, error) {
	switch v {

	case viewHelp:
		lines := m.helpLines(m.helpTopic)
		hint := "every key and every command"
		if m.helpTopic != "" {
			hint = m.helpTopic
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

// renderTable turns Controller rows into table cells.
//
// The Controller already returns flat, display-ready strings, so this is a
// mapping and nothing more. Anything that had to decide something here would be
// a decision the plan screen (11b) would have to make again, differently.
func (m Model) renderTable(v view) ([]table.Row, map[int64]app.HoldingRow, string, error) {
	switch v {
	case viewHoldings:
		rows, err := m.ctrl.Holdings(m.ctx)
		if err != nil {
			return nil, nil, "", err
		}
		cells := make([]table.Row, 0, len(rows))
		byKey := make(map[int64]app.HoldingRow, len(rows))
		for _, r := range rows {
			cells = append(cells, table.Row{
				Key:   int64(r.ID),
				Cells: []string{r.Item, r.State, r.Location, r.Note},
				// Shown, never matched: the filter and the sort read Cells, so
				// `loc:` keeps meaning the place itself rather than the branch
				// it hangs from.
				Paths: []string{"", "", r.LocationPath, ""},
			})
			byKey[int64(r.ID)] = r
		}
		return cells, byKey, "enter for history", nil

	case viewItems:
		rows, err := m.ctrl.Items(m.ctx)
		if err != nil {
			return nil, nil, "", err
		}
		cells := make([]table.Row, 0, len(rows))
		for _, r := range rows {
			cells = append(cells, table.Row{
				Key:   int64(r.ID),
				Cells: []string{r.Name, r.OnHand, r.Category, r.Measure, r.Kind},
			})
		}
		return cells, nil, "", nil
	}
	return nil, nil, "", fmt.Errorf("view %d is not a table", v)
}

// renderTree turns Controller rows into tree nodes. As with renderTable, this
// is a mapping and nothing more: anything deciding something here would be a
// decision the tree would have to make again, differently.
func (m Model) renderTree(v view) ([]tree.Node, string, error) {
	var rows []app.TreeRow
	var err error
	if v == viewCategories {
		rows, err = m.ctrl.CategoryTree(m.ctx, m.contents[v])
	} else {
		rows, err = m.ctrl.LocationTree(m.ctx, m.contents[v])
	}
	if err != nil {
		return nil, "", err
	}
	nodes := make([]tree.Node, 0, len(rows))
	for _, r := range rows {
		nodes = append(nodes, tree.Node{
			ID: r.ID, Name: r.Name, Depth: r.Depth,
			Count: r.Count, Kind: r.Kind, Measure: r.Measure,
		})
	}
	// Only the fold keys and the contents toggle. Ascend and descend are the
	// same C-b and C-f as everywhere else and the footer already carries them
	// -- and this line has to fit a 60-column terminal, where naming all four
	// wrapped it.
	return nodes, keys.Hint(keys.Tree,
		[]keys.Action{keys.FoldToggle},
		[]keys.Action{keys.FoldCycleAll}) + " - " +
		keys.Hint(keys.Browse, []keys.Action{keys.ShowContents}), nil
}

// focusKey puts the cursor on a row by identity, and does nothing if that row
// is not here -- a reload can drop the row somebody was on.
//
// This was two functions, restoreCursor and land, which differed only in where
// the key came from and agreed on nothing else: one knew that a tree key is not
// a row key and the other did not.
func (m Model) focusKey(key int64) Model {
	m.current = m.current.Focus(key)
	return m
}

// keyOf is the key a jump destination has on the surface it lands on.
//
// A tree keys its rows by kind AND identifier, because Category 7 and Item 7
// come from different tables and would otherwise be the same row. A table keys
// them by identifier alone. The candidate does not know which it is about to
// become, so the view it lands in decides.
func keyOf(target resolve.Candidate) int64 {
	if forest(viewFor(target.Kind)) {
		return tree.Node{ID: target.ID, Kind: string(target.Kind)}.Key()
	}
	return target.ID
}
