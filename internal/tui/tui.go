// Package tui is the read-only browser.
//
// v01's UI exists to prove state is readable, and nothing more: no writes, no
// forms, no confirmation flows. What it must show is the thing the whole model
// was built for -- a Holding's full history, in sequence order, reconstructible
// from the ledger.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"home-management-system/internal/app"
	"home-management-system/internal/domain"
	"home-management-system/internal/tui/table"
)

type view int

const (
	viewCategories view = iota
	viewLocations
	viewItems
	viewHoldings
	viewIntegrity
	viewHistory
)

var viewNames = map[view]string{
	viewCategories: "Categories",
	viewLocations:  "Locations",
	viewItems:      "Items",
	viewHoldings:   "Holdings",
	viewIntegrity:  "Integrity",
	viewHistory:    "History",
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	cursorStyle = lipgloss.NewStyle().Bold(true).Reverse(true)
	alertStyle  = lipgloss.NewStyle().Bold(true)
)

// Model is the Bubbletea model.
type Model struct {
	ctrl app.Controller
	ctx  context.Context

	view   view
	cursor int
	rows   []string

	// table is the working surface for Holdings and Items. The tree and report
	// views stay on plain rows in a viewport until 10b, because folding is a
	// different problem and merging them early would settle it by accident.
	table table.Model
	// holdingIDs parallels rows in the Holdings view, so Enter knows what was
	// selected without the rendering layer carrying domain types.
	holdingIDs []domain.HoldingID
	fromView   view

	status   string
	width    int
	height   int
	viewport viewport.Model
	ready    bool
}

func New(ctx context.Context, ctrl app.Controller) Model {
	return Model{ctx: ctx, ctrl: ctrl, view: viewHoldings, table: table.New(columnsFor(viewHoldings))}
}

// tabular reports whether a view is a table. Holdings and Items are; hierarchy
// is not something a table shows, so Categories and Locations are trees.
func tabular(v view) bool { return v == viewHoldings || v == viewItems }

// columnsFor declares each table's shape, and with it what a narrow terminal
// loses. Drop order is a decision recorded here rather than an accident of
// layout arithmetic.
func columnsFor(v view) []table.Column {
	switch v {
	case viewHoldings:
		return []table.Column{
			{Title: "ITEM", Min: 10, Grow: true},
			// Quantity is never dropped: a holdings table that does not say how
			// much is a list of things you own, which you already knew.
			{Title: "QTY", Min: 5, Align: table.Right},
			{Title: "LOCATION", Min: 12, Drop: 2, Elide: table.ElideStart},
			{Title: "FLAGS", Min: 6, Drop: 3},
		}
	case viewItems:
		return []table.Column{
			{Title: "ITEM", Min: 10, Grow: true},
			{Title: "ON HAND", Min: 6, Align: table.Right},
			{Title: "CATEGORY", Min: 8, Drop: 2},
			{Title: "MEASURE", Min: 8, Drop: 3},
			{Title: "KIND", Min: 6, Drop: 4},
		}
	}
	return nil
}

func (m Model) Init() tea.Cmd { return m.load(m.view) }

// loadedMsg carries a rendered view. Exactly one of rows and cells is set: the
// tree and report views render to lines, the table views to cells.
type loadedMsg struct {
	view       view
	rows       []string
	cells      []table.Row
	holdingIDs []domain.HoldingID
	status     string
}

type errMsg struct{ err error }

func (m Model) load(v view) tea.Cmd {
	return func() tea.Msg {
		if tabular(v) {
			cells, ids, status, err := m.renderTable(v)
			if err != nil {
				return errMsg{err}
			}
			return loadedMsg{view: v, cells: cells, holdingIDs: ids, status: status}
		}
		rows, ids, status, err := m.render(v, 0)
		if err != nil {
			return errMsg{err}
		}
		return loadedMsg{view: v, rows: rows, holdingIDs: ids, status: status}
	}
}

func (m Model) loadHistory(id domain.HoldingID) tea.Cmd {
	return func() tea.Msg {
		rows, _, status, err := m.render(viewHistory, id)
		if err != nil {
			return errMsg{err}
		}
		return loadedMsg{view: viewHistory, rows: rows, status: status}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		body := msg.Height - 4
		if body < 3 {
			body = 3
		}
		if !m.ready {
			m.viewport = viewport.New(msg.Width, body)
			m.ready = true
		} else {
			m.viewport.Width, m.viewport.Height = msg.Width, body
		}
		m.table = m.table.SetSize(msg.Width, m.bodyHeight())
		m.viewport.SetContent(m.body())
		return m, nil

	case loadedMsg:
		m.view, m.rows, m.holdingIDs, m.status = msg.view, msg.rows, msg.holdingIDs, msg.status
		m.cursor = 0
		if tabular(msg.view) {
			// A fresh table per view: the columns differ, and carrying a
			// selection across views would mean acting on rows a person picked
			// while looking at something else.
			m.table = table.New(columnsFor(msg.view)).SetRows(msg.cells).SetSize(m.width, m.bodyHeight())
		}
		m.viewport.SetContent(m.body())
		m.viewport.GotoTop()
		return m, nil

	case errMsg:
		m.status = "error: " + msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey uses Emacs bindings, matching the previous system so muscle memory
// carries over.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The table sees motion, selection, and sorting first. It reports what it
	// did not use, so the keys that belong to the application -- views, quit,
	// enter -- still reach it.
	if tabular(m.view) {
		if next, handled := m.table.Update(msg); handled {
			m.table = next
			m.cursor = max(0, m.table.Cursor())
			return m, nil
		}
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "ctrl+n", "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			m.viewport.SetContent(m.body())
		}
	case "ctrl+p", "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.viewport.SetContent(m.body())
		}
	case "ctrl+a", "home":
		m.cursor = 0
		m.viewport.SetContent(m.body())
		m.viewport.GotoTop()
	case "ctrl+e", "end":
		m.cursor = max(0, len(m.rows)-1)
		m.viewport.SetContent(m.body())
		m.viewport.GotoBottom()

	case "ctrl+f", "enter", "right":
		// In a table, h and l move between columns, so Enter is the only way
		// down into a row -- which is also what it means everywhere else.
		if m.view == viewHoldings {
			if row, ok := m.table.Current(); ok {
				m.fromView = m.view
				return m, m.loadHistory(domain.HoldingID(row.Key))
			}
		}
	case "ctrl+b", "esc", "left", "h":
		if m.view == viewHistory {
			return m, m.load(m.fromView)
		}

	case "1":
		return m, m.load(viewCategories)
	case "2":
		return m, m.load(viewLocations)
	case "3":
		return m, m.load(viewItems)
	case "4":
		return m, m.load(viewHoldings)
	case "5":
		return m, m.load(viewIntegrity)
	case "r":
		return m, m.load(m.view)
	}
	return m, nil
}

func (m Model) View() string {
	if !m.ready {
		return "loading..."
	}
	body := m.viewport.View()
	if tabular(m.view) {
		// The table scrolls itself, so it renders straight rather than through
		// the viewport. Two things scrolling one list is how a cursor ends up
		// off screen with nothing obviously wrong.
		body = m.table.View()
	}
	return strings.Join([]string{m.header(), body, m.footer()}, "\n")
}

// bodyHeight is the room left after the header and footer, both two lines.
func (m Model) bodyHeight() int {
	if h := m.height - 4; h > 3 {
		return h
	}
	return 3
}

func (m Model) header() string {
	// The tab bar drops to numbers alone when the names will not fit.
	//
	// Not cosmetic: a line wider than the terminal WRAPS, which shifts every
	// row below it and makes the whole screen unreadable rather than merely
	// cramped. Found by the Simulator's width check at 60 columns, where the
	// full names come to 65.
	names := len(m.tabLabels(true)) <= m.width
	var rendered []string
	for _, t := range m.tabs() {
		label := t.label(names)
		if t.view == m.view {
			rendered = append(rendered, titleStyle.Render("["+label+"]"))
		} else {
			rendered = append(rendered, dimStyle.Render(" "+label+" "))
		}
	}
	if m.view == viewHistory {
		suffix := "History"
		if !names {
			suffix = "H"
		}
		rendered = append(rendered, titleStyle.Render("["+suffix+"]"))
	}
	return strings.Join(rendered, " ") + "\n" + dimStyle.Render(strings.Repeat("-", max(10, m.width)))
}

type tab struct {
	view view
	name string
	key  int
}

func (t tab) label(withName bool) string {
	if withName {
		return fmt.Sprintf("%d %s", t.key, t.name)
	}
	return fmt.Sprintf("%d", t.key)
}

func (m Model) tabs() []tab {
	var out []tab
	for _, v := range []view{viewCategories, viewLocations, viewItems, viewHoldings, viewIntegrity} {
		out = append(out, tab{view: v, name: viewNames[v], key: int(v) + 1})
	}
	return out
}

// tabLabels renders the bar as plain text, so its width can be measured before
// any styling is applied. Styling adds escape sequences that occupy no columns,
// which is exactly why measuring the rendered string would be wrong.
func (m Model) tabLabels(withName bool) string {
	var parts []string
	for _, t := range m.tabs() {
		parts = append(parts, " "+t.label(withName)+" ")
	}
	s := strings.Join(parts, " ")
	if m.view == viewHistory {
		if withName {
			s += " [History]"
		} else {
			s += " [H]"
		}
	}
	return s
}

func (m Model) footer() string {
	help := "j/k move - h/l column - s sort - space select - enter history - 1-5 views - q quit"
	if m.status != "" {
		return dimStyle.Render(strings.Repeat("-", max(10, m.width))) + "\n" + m.status
	}
	return dimStyle.Render(strings.Repeat("-", max(10, m.width))) + "\n" + dimStyle.Render(help)
}

func (m Model) body() string {
	if len(m.rows) == 0 {
		return dimStyle.Render("  (nothing here)")
	}
	var b strings.Builder
	for i, row := range m.rows {
		if i == m.cursor {
			b.WriteString(cursorStyle.Render("> " + row))
		} else {
			b.WriteString("  " + row)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// render turns controller data into display rows. The Controller returns flat,
// display-ready rows, so this stays formatting rather than logic.
func (m Model) render(v view, subject domain.HoldingID) ([]string, []domain.HoldingID, string, error) {
	switch v {

	case viewCategories:
		rows, err := m.ctrl.CategoryTree(m.ctx)
		if err != nil {
			return nil, nil, "", err
		}
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s%-30s %s",
				strings.Repeat("  ", r.Depth), r.Name, dimStyle.Render(fmt.Sprintf("%d items", r.Count))))
		}
		return out, nil, fmt.Sprintf("%d categories", len(rows)), nil

	case viewLocations:
		rows, err := m.ctrl.LocationTree(m.ctx)
		if err != nil {
			return nil, nil, "", err
		}
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%s%-30s %s",
				strings.Repeat("  ", r.Depth), r.Name, dimStyle.Render(fmt.Sprintf("%d holdings", r.Count))))
		}
		return out, nil, fmt.Sprintf("%d locations", len(rows)), nil

	case viewItems:
		rows, err := m.ctrl.Items(m.ctx)
		if err != nil {
			return nil, nil, "", err
		}
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%-26s %-7s %-16s %-22s %s",
				truncate(r.Name, 26), r.Kind, truncate(r.Category, 16),
				truncate(r.Measure, 22), r.OnHand))
		}
		return out, nil, fmt.Sprintf("%d items", len(rows)), nil

	case viewHoldings:
		rows, err := m.ctrl.Holdings(m.ctx)
		if err != nil {
			return nil, nil, "", err
		}
		out := make([]string, 0, len(rows))
		ids := make([]domain.HoldingID, 0, len(rows))
		for _, r := range rows {
			line := fmt.Sprintf("%-26s %-16s %-24s %s",
				truncate(r.Item, 26), truncate(r.Location, 16), truncate(r.State, 24), r.Note)
			out = append(out, strings.TrimRight(line, " "))
			ids = append(ids, r.ID)
		}
		return out, ids, fmt.Sprintf("%d holdings - enter for history", len(rows)), nil

	case viewIntegrity:
		report, err := m.ctrl.Integrity(m.ctx)
		if err != nil {
			return nil, nil, "", err
		}
		var out []string
		out = append(out, fmt.Sprintf("%d holdings checked against the ledger", report.HoldingsChecked))
		if report.Clean() {
			out = append(out, "", "no discrepancies")
		} else {
			out = append(out, "")
			for _, d := range report.Discrepancies {
				out = append(out, alertStyle.Render("DISCREPANCY ")+d)
			}
			for _, o := range report.Orphans {
				out = append(out, alertStyle.Render("ORPHAN      ")+o)
			}
			// Reporting, never repairing: silently correcting would destroy the
			// only signal that a write skipped its event.
			out = append(out, "", dimStyle.Render("reported, not repaired"))
		}

		nudges, err := m.ctrl.Nudges(m.ctx)
		if err != nil {
			return nil, nil, "", err
		}
		if len(nudges) > 0 {
			out = append(out, "", titleStyle.Render("Classification"))
			for _, n := range nudges {
				out = append(out, fmt.Sprintf("  %s sits at %q, which has %d subcategories",
					n.Item, n.Category, n.Siblings))
			}
		}
		status := "clean"
		if !report.Clean() {
			status = alertStyle.Render(fmt.Sprintf("%d discrepancies, %d orphans",
				len(report.Discrepancies), len(report.Orphans)))
		}
		return out, nil, status, nil

	case viewHistory:
		rows, err := m.ctrl.HoldingHistory(m.ctx, subject)
		if err != nil {
			return nil, nil, "", err
		}
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%6d  %s  %-18s %s",
				r.Sequence, r.When.Format("2006-01-02 15:04"), r.Type, r.Summary))
		}
		return out, nil, fmt.Sprintf("holding %d - %d events in sequence order", subject, len(rows)), nil
	}
	return nil, nil, "", fmt.Errorf("unknown view %d", v)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Run starts the program.
func Run(ctx context.Context, ctrl app.Controller) error {
	program := tea.NewProgram(New(ctx, ctrl), tea.WithAltScreen())
	_, err := program.Run()
	return err
}

// renderTable turns Controller rows into table cells.
//
// The Controller already returns flat, display-ready strings, so this is a
// mapping and nothing more. Anything that had to decide something here would be
// a decision the plan screen (11b) would have to make again, differently.
func (m Model) renderTable(v view) ([]table.Row, []domain.HoldingID, string, error) {
	switch v {
	case viewHoldings:
		rows, err := m.ctrl.Holdings(m.ctx)
		if err != nil {
			return nil, nil, "", err
		}
		cells := make([]table.Row, 0, len(rows))
		ids := make([]domain.HoldingID, 0, len(rows))
		for _, r := range rows {
			cells = append(cells, table.Row{
				Key:   int64(r.ID),
				Cells: []string{r.Item, r.State, r.Location, r.Note},
			})
			ids = append(ids, r.ID)
		}
		return cells, ids, fmt.Sprintf("%d holdings - enter for history", len(rows)), nil

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
		return cells, nil, fmt.Sprintf("%d items", len(rows)), nil
	}
	return nil, nil, "", fmt.Errorf("view %d is not a table", v)
}
