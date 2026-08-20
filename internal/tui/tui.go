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
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/omnibox"
	"home-management-system/internal/tui/table"
	"home-management-system/internal/tui/tree"
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

	// tree is the working surface for Categories and Locations. It is built on
	// the same table, so density, cursor, banding, and selection are the same
	// decisions rather than two answers to one question.
	tree tree.Model

	// box is the one input line. Its mode says whether a keystroke is a
	// character or a command, which is why every key handler consults it first.
	box omnibox.Model
	// jump holds the palette's results, rendered through the same table so the
	// density decisions are not made twice.
	jump       table.Model
	candidates []resolve.Candidate
	// pending is where a jump is going, held until the destination view has
	// loaded and there is a row to put the cursor on.
	pending *resolve.Candidate

	// editor is the in-place field. confirm is the one thing that stands
	// between a person and a permanent change.
	editor  editor.Model
	confirm *pendingPlan
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
	return Model{
		ctx: ctx, ctrl: ctrl, view: viewHoldings,
		table:  table.New(columnsFor(viewHoldings)),
		box:    omnibox.New(),
		editor: editor.New(),
		jump:   table.New(jumpColumns).Fixed(),
	}
}

// tabular reports whether a view is a table. Holdings and Items are; hierarchy
// is not something a flat table shows, so Categories and Locations are trees.
func tabular(v view) bool { return v == viewHoldings || v == viewItems }

// jumpColumns are the palette's. KIND comes first and is never dropped:
// "Shelf 1" as a Location and "Shelf 1" inside a Holding path are different
// destinations, and a jump that does not say which lands somewhere surprising.
var jumpColumns = []table.Column{
	{Title: "KIND", Min: 8},
	{Title: "NAME", Min: 12, Grow: true, Elide: table.ElideStart},
}

// facetColumns says which column each facet name restricts, per view. Only the
// view knows that `loc:` means the LOCATION column here and nothing at all in
// the Items table.
func facetColumns(v view) map[string]int {
	switch v {
	case viewHoldings:
		return map[string]int{"item": 0, "qty": 1, "state": 1, "loc": 2, "at": 2, "flag": 3}
	case viewItems:
		return map[string]int{"item": 0, "name": 0, "cat": 2, "unit": 3, "kind": 4}
	}
	return nil
}

// forest reports whether a view is a tree.
func forest(v view) bool { return v == viewCategories || v == viewLocations }

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
	nodes      []tree.Node
	holdingIDs []domain.HoldingID
	status     string
}

type errMsg struct{ err error }

// candidatesMsg carries the flat index the jump palette searches.
type candidatesMsg struct{ candidates []resolve.Candidate }

func (m Model) loadCandidates() tea.Cmd {
	return func() tea.Msg {
		index, err := m.ctrl.SearchIndex(m.ctx)
		if err != nil {
			return errMsg{err}
		}
		return candidatesMsg{candidates: index.All()}
	}
}

func (m Model) load(v view) tea.Cmd {
	return func() tea.Msg {
		if forest(v) {
			nodes, status, err := m.renderTree(v)
			if err != nil {
				return errMsg{err}
			}
			return loadedMsg{view: v, nodes: nodes, status: status}
		}
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
		m.tree = m.tree.SetSize(msg.Width, m.bodyHeight())
		m.jump = m.jump.SetSize(msg.Width, m.bodyHeight())
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
		if forest(msg.view) {
			m.tree = tree.New(unitFor(msg.view)).
				SetNodes(msg.nodes).SetSize(m.width, m.bodyHeight())
		}
		m.viewport.SetContent(m.body())
		m.viewport.GotoTop()
		if m.pending != nil {
			m = m.land(*m.pending)
			m.pending = nil
		}
		return m, nil

	case planMsg:
		if msg.plan.Empty() {
			m.status = "nothing to do"
			return m, nil
		}
		if msg.plan.NeedsConfirmation() {
			// Creation is never silent and never one keystroke. The plan says
			// so itself -- a non-empty Permanent IS the signal -- so the
			// interface cannot fail to notice.
			m.confirm = &pendingPlan{plan: msg.plan, summary: msg.summary}
			m.status = ""
			return m, nil
		}
		return m, m.apply(msg.plan, msg.summary)

	case issuesMsg:
		m.status = alertStyle.Render(strings.Join(msg.issues, " - "))
		return m, nil

	case appliedMsg:
		// Reload, because something changed. The list a person is looking at
		// must not disagree with the house.
		m.status = msg.summary
		return m, m.reloadKeepingStatus()

	case candidatesMsg:
		m.candidates = msg.candidates
		m = m.refreshJump()
		return m, nil

	case errMsg:
		m.status = "error: " + msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		// Innermost mode first, and this ORDER is the whole answer to the old
		// interface's defect. Every mode gets the keystroke before the one that
		// contains it, so esc leaves exactly one -- never two, and never
		// depending on how you got there.
		if next, cmd, handled := m.handleConfirm(msg); handled {
			return next, cmd
		}
		if next, cmd, handled := m.handleEditor(msg); handled {
			return next, cmd
		}
		if next, cmd, handled := m.handleOmnibox(msg); handled {
			return next, cmd
		}
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
	if forest(m.view) {
		if next, handled := m.tree.Update(msg); handled {
			m.tree = next
			return m, nil
		}
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	// The two leaders. They feel identical for exactly one keystroke and then
	// diverge completely, which is why the omnibox renders them differently
	// before a word has been read.
	case "/":
		m.box = m.box.Open(omnibox.Filter)
		m = m.applyLive()
		return m, nil
	case "ctrl+p":
		m.box = m.box.Open(omnibox.Jump)
		m = m.refreshJump()
		return m, m.loadCandidates()
	case ":":
		m.box = m.box.Open(omnibox.Command)
		return m, nil

	// e renames whatever the cursor is on, in place.
	case "e":
		return m.openEditor(), nil

	// The Emacs aliases v01 carried are gone: C-p is the jump leader now, and
	// two motion idioms in one application is one too many.
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			m.viewport.SetContent(m.body())
		}
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.viewport.SetContent(m.body())
		}
	case "home", "g":
		m.cursor = 0
		m.viewport.SetContent(m.body())
		m.viewport.GotoTop()
	case "end", "G":
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
		// esc in the LIST clears an applied filter -- a different escape from
		// the one that closes the input line.
		if msg.String() == "esc" && m.box.Applied() != "" {
			m.box = m.box.Clear()
			return m.filterWith(omnibox.Query{}), nil
		}
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
	if forest(m.view) {
		body = m.tree.View()
	}
	if tabular(m.view) {
		// The table scrolls itself, so it renders straight rather than through
		// the viewport. Two things scrolling one list is how a cursor ends up
		// off screen with nothing obviously wrong.
		body = m.table.View()
	}
	if m.editor.IsOpen() {
		// Inside the list, under the row it belongs to, so the rows around it
		// stay where they are. Losing your place is the thing that made the old
		// interface unusable for its actual job.
		body = body + "\n" + m.editor.SetWidth(m.width).View()
	}
	if m.confirm != nil {
		body = m.confirmView()
	}
	if m.box.Mode() == omnibox.Jump {
		// The palette REPLACES the list rather than floating over it. A jump
		// searches everything, so leaving the current view visible underneath
		// would suggest it is being searched, which is the confusion between
		// the two modes that this substage exists to avoid.
		body = m.jump.View()
	}

	parts := []string{m.header(), body}
	if line := m.box.View(); line != "" {
		parts = append(parts, line)
	}
	return strings.Join(append(parts, m.footer()), "\n")
}

// bodyHeight is the room left after the header, the footer, and the input line
// when there is one.
func (m Model) bodyHeight() int {
	if h := m.height - 4 - m.box.Height() - m.editor.Height(); h > 3 {
		return h
	}
	return 3
}

// countPhrase says how many, and how many of how many when a filter is on.
func (m Model) countPhrase() string {
	noun := map[view]string{
		viewHoldings: "holdings", viewItems: "items",
		viewCategories: "categories", viewLocations: "locations",
	}[m.view]
	if noun == "" {
		return ""
	}
	if shown, total, filtered := m.counts(); filtered {
		return fmt.Sprintf("%d of %d %s", shown, total, noun)
	}
	total := len(m.rows)
	if tabular(m.view) {
		_, total = m.table.Counts()
	}
	if forest(m.view) {
		_, total = m.tree.Counts()
	}
	return fmt.Sprintf("%d %s", total, noun)
}

// selectionCount is how many rows were explicitly picked on whichever surface
// is showing.
func (m Model) selectionCount() int {
	if tabular(m.view) {
		return m.table.SelectionCount()
	}
	if forest(m.view) {
		return m.tree.SelectionCount()
	}
	return 0
}

// counts reports the filtered and total row counts of whichever surface is
// showing, and whether a filter is in force at all.
func (m Model) counts() (shown, total int, filtered bool) {
	switch {
	case tabular(m.view) && m.table.Filtered():
		shown, total = m.table.Counts()
		return shown, total, true
	case forest(m.view) && m.tree.Filtered():
		shown, total = m.tree.Counts()
		return shown, total, true
	}
	return 0, 0, false
}

// land puts the cursor on what a jump chose, now that its view has loaded.
func (m Model) land(target resolve.Candidate) Model {
	if tabular(viewFor(target.Kind)) {
		for i, row := range m.table.Rows() {
			if row.Key == target.ID {
				m.table = m.table.SetCursor(i)
				return m
			}
		}
	}
	if forest(viewFor(target.Kind)) {
		m.tree = m.tree.Focus(target.ID)
	}
	return m
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
	rule := dimStyle.Render(strings.Repeat("-", max(10, m.width)))

	// Assembled as PARTS and joined once, rather than concatenated piece by
	// piece. The first version glued them together with separators baked into
	// each piece and produced "2 items - - sorted by item a-z" the moment one
	// view had nothing to say -- which the golden frames caught.
	//
	// The count leads, and it is built in one place rather than by each view: a
	// count assembled per view is a count that says "3 of 12" in only some of
	// them.
	// While the palette is up it is what the person is looking at, so the
	// footer describes IT. Counting the list underneath would be describing a
	// screen nobody is reading.
	if m.box.Mode() == omnibox.Jump {
		shown, _ := m.jump.Counts()
		return rule + "\n" + dimStyle.Render(fmt.Sprintf(
			"%d matches across every kind - enter go - esc cancel", shown))
	}

	var parts []string
	if phrase := m.countPhrase(); phrase != "" {
		parts = append(parts, phrase)
	}
	if m.status != "" {
		parts = append(parts, m.status)
	}
	if tabular(m.view) {
		if sorted := m.table.SortDescription(); sorted != "" {
			parts = append(parts, "sorted "+sorted)
		}
	}
	if n := m.selectionCount(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d selected", n))
	}
	status := strings.Join(parts, " - ")

	if status != "" {
		return rule + "\n" + status
	}
	return rule + "\n" + dimStyle.Render(help)
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
		return cells, ids, "enter for history", nil

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

// unitFor names what a tree's rollup counts, which is also the count column's
// title.
func unitFor(v view) string {
	if v == viewCategories {
		return "items"
	}
	return "holdings"
}

// renderTree turns Controller rows into tree nodes. As with renderTable, this
// is a mapping and nothing more: anything deciding something here would be a
// decision the tree would have to make again, differently.
func (m Model) renderTree(v view) ([]tree.Node, string, error) {
	var rows []app.TreeRow
	var err error
	if v == viewCategories {
		rows, err = m.ctrl.CategoryTree(m.ctx)
	} else {
		rows, err = m.ctrl.LocationTree(m.ctx)
	}
	if err != nil {
		return nil, "", err
	}
	nodes := make([]tree.Node, 0, len(rows))
	for _, r := range rows {
		nodes = append(nodes, tree.Node{ID: r.ID, Name: r.Name, Depth: r.Depth, Count: r.Count})
	}
	return nodes, "za fold - zR expand all - zM collapse all", nil
}

// ---------------------------------------------------------------------------
// The omnibox: filter, and jump
// ---------------------------------------------------------------------------

// handleOmnibox takes the keystroke when the input line is open.
//
// It runs before everything else, because while the line is open a keystroke is
// a CHARACTER. A `j` that moved the cursor while someone was typing "jar" would
// make the input line unusable, and it is the classic way a modal interface
// betrays the person using it.
func (m Model) handleOmnibox(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if m.box.Mode() == omnibox.Closed {
		return m, nil, false
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.box = m.box.Cancel()
		// Restoring the ACCEPTED filter, not merely closing the line. Rows
		// narrow as you type, so an abandoned edit leaves the half-typed filter
		// on the table -- which showed up as "0 of 12 holdings" under a jump
		// palette, long after the filter that produced it had been cancelled.
		return m.applyFilter(), nil, true
	case tea.KeyEnter:
		if m.box.Mode() == omnibox.Jump {
			return m.acceptJump()
		}
		if m.box.Mode() == omnibox.Command {
			line := m.box.Input()
			m.box = m.box.Accept()
			m.status = "working..."
			return m, m.runLine(line), true
		}
		m.box = m.box.Accept()
		m = m.applyFilter()
		return m, nil, true
	}
	if next, handled := m.box.Update(msg); handled {
		m.box = next
		switch m.box.Mode() {
		case omnibox.Jump:
			m = m.refreshJump()
		case omnibox.Filter:
			// Narrow as you type: the filter that would apply if accepted now.
			m = m.applyLive()
		}
		return m, nil, true
	}
	// A key the input line does not want -- cursor motion through the results,
	// which the palette owns.
	if m.box.Mode() == omnibox.Jump {
		if next, handled := m.jump.Update(msg); handled {
			m.jump = next
			return m, nil, true
		}
	}
	return m, nil, true
}

// applyFilter puts the accepted filter onto whichever surface is showing.
func (m Model) applyFilter() Model { return m.filterWith(m.box.Query()) }

// applyLive puts the half-typed filter on, which is what makes rows narrow as
// the line is typed rather than when it is accepted.
func (m Model) applyLive() Model { return m.filterWith(m.box.Live()) }

func (m Model) filterWith(q omnibox.Query) Model {
	if tabular(m.view) {
		m.table = m.table.SetFilter(table.Filter{
			Facets: facetTests(m.view, q), Text: q.Text,
		})
	}
	if forest(m.view) {
		// A tree has no columns to restrict, so a facet on one is nothing to
		// act on. Saying so beats silently ignoring it.
		m.tree = m.tree.SetFilter(q.Text)
	}
	return m
}

// facetTests resolves facet names to columns, dropping the ones this view has
// no field for.
func facetTests(v view, q omnibox.Query) []table.FacetTest {
	columns := facetColumns(v)
	var out []table.FacetTest
	for _, f := range q.Facets {
		if column, ok := columns[f.Key]; ok && !f.Any() {
			out = append(out, table.FacetTest{Column: column, Value: f.Value})
		}
	}
	return out
}

// refreshJump re-runs the search across everything.
//
// It re-sizes as well as re-filters, because the palette replaces the body and
// therefore has to fit the same space the list did -- a widget that is only
// sized on a terminal resize is a widget that is the wrong size until one
// happens.
func (m Model) refreshJump() Model {
	m.jump = m.jump.SetSize(m.width, m.bodyHeight())
	rows := make([]table.Row, 0, len(m.candidates))
	for i, c := range m.candidates {
		if c.Archived {
			continue
		}
		if !matchesJump(c, m.box.Input()) {
			continue
		}
		rows = append(rows, table.Row{Key: int64(i), Cells: []string{string(c.Kind), c.Path}})
		if len(rows) >= 12 {
			break
		}
	}
	m.jump = m.jump.SetRows(rows)
	return m
}

func matchesJump(c resolve.Candidate, text string) bool {
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

// acceptJump goes to the chosen thing: the view it lives in, cursor on its row.
func (m Model) acceptJump() (tea.Model, tea.Cmd, bool) {
	row, ok := m.jump.Current()
	if !ok || int(row.Key) >= len(m.candidates) {
		m.box = m.box.Cancel()
		return m, nil, true
	}
	target := m.candidates[row.Key]
	m.box = m.box.Cancel()
	m.pending = &target
	return m, m.load(viewFor(target.Kind)), true
}

// viewFor is where a kind of thing lives.
func viewFor(k resolve.Kind) view {
	switch k {
	case resolve.KindCategory:
		return viewCategories
	case resolve.KindLocation:
		return viewLocations
	case resolve.KindItem:
		return viewItems
	}
	return viewHoldings
}

// reloadKeepingStatus re-reads the current view without discarding what the
// last action said it did.
//
// A status that vanished on reload would mean the feedback for a write was
// visible for exactly as long as it took to refresh, which is to say never.
func (m Model) reloadKeepingStatus() tea.Cmd {
	said := m.status
	reload := m.load(m.view)
	return func() tea.Msg {
		msg := reload()
		if loaded, ok := msg.(loadedMsg); ok {
			loaded.status = said
			return loaded
		}
		return msg
	}
}
