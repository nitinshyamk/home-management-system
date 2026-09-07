// Package tui is the interface.
//
// It shows the house on one of three surfaces -- a table, a tree, or scrolling
// prose -- and writes to it through exactly one path: compose a command, ask
// app to PLAN it, show what the plan would do, and apply the plan the person
// agreed to. It cannot write any other way, and that is enforced rather than
// intended: archlint asserts this package imports none of origin, ledger or
// annotate, so the only reachable write is one that could be shown first.
//
// Nothing here decides anything about the domain. The Controller returns flat,
// display-ready rows; a keystroke turns into a Command; a name becomes an
// identifier only in resolve. What is left is the arrangement of it, which is
// what the rest of this package is.
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/creator"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/omnibox"
	"home-management-system/internal/tui/table"
	"home-management-system/internal/tui/tree"

	"home-management-system/internal/tui/style"
)

type view int

const (
	viewCategories view = iota
	viewLocations
	viewItems
	viewHoldings
	viewIntegrity
	viewHistory
	// viewHelp is reached by `help` on the command line rather than by a
	// number, and left the way History is. It has no tab: a view you cannot
	// get to by pressing a digit should not claim one of the digits.
	viewHelp
)

// wrap breaks text onto as many lines as it needs.
//
// Truncating an error is the worst thing to truncate: the part that says what
// to do about it is at the END, so a cut message is a message that reports a
// problem and withholds the answer.
func wrap(text string, width int) []string {
	if width < 20 {
		width = 20
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= width:
			line += " " + word
		default:
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// humanise defers to command.Humanise, which is where the list lives: an
// importer, a plan screen, and this all render the same errors, and three
// copies of the list would drift.
func humanise(text string) string { return command.Humanise(text) }

// Model is the Bubbletea model.
type Model struct {
	ctrl app.Controller
	ctx  context.Context

	view view

	// current is what the view is drawn on: a table, a tree, or scrolling
	// prose. One field rather than three, so every question about "the surface
	// showing right now" has one place to be answered instead of a pair of
	// predicates at each of thirty-five call sites.
	current surface

	// box is the one input line. Its mode says whether a keystroke is a
	// character or a command, which is why every key handler consults it first.
	box omnibox.Model
	// jump holds the palette's results, rendered through the same table so the
	// density decisions are not made twice.
	jump       table.Model
	candidates []resolve.Candidate
	// index is the candidates, ready to resolve against. Built once when they
	// arrive rather than per keystroke: named() rebuilt it on every character
	// typed into a prompt, which is a whole-house index per keypress.
	index *resolve.Index
	// units is the unit vocabulary, cached beside the candidates because both
	// are read together and neither changes while a panel is open.
	units []string
	// folds is each tree's collapsed nodes, kept here rather than on the tree
	// because the tree is rebuilt from scratch on every load.
	//
	// Per view, and surviving a view switch -- unlike the cursor, which does
	// not. A cursor carried across views restores a position nobody was in; a
	// fold is a statement about the SHAPE of one tree, and it is still true
	// when you come back to it.
	folds map[view]map[int64]bool
	// helpTopic is the command `help` was asked about, or "" for all of it.
	helpTopic string
	// contents is which trees are showing what their nodes contain.
	//
	// It lives here rather than on the tree because the tree is rebuilt from
	// scratch on every load, and a display mode that forgot itself whenever
	// anything was written would not be a mode. Per view, because the two
	// trees are asking different questions -- one shows Items, one Holdings --
	// and wanting one is no reason to want the other.
	contents map[view]bool
	// pending is where a jump is going, held until the destination view has
	// loaded and there is a row to put the cursor on.
	pending *resolve.Candidate

	// editor is the in-place field. confirm is the one thing that stands
	// between a person and a permanent change.
	editor  editor.Model
	creator creator.Model
	confirm *pendingPlan
	// problem is what went wrong, held apart from status so it can be rendered
	// loudly and wrapped rather than squeezed into a one-line summary.
	problem []string

	// holdingRows is what is on screen, by identifier. A keystroke reaches the
	// identifiers behind its row through this rather than through the display
	// strings, which is what lets it build a Command without resolving a name.
	holdingRows map[int64]app.HoldingRow
	// copied is a thing waiting for a put.
	copied *carried

	// The import flow. importing swaps the whole screen for the plan review,
	// because a file proposing a batch of changes is not something to look at
	// alongside the house -- it is the only thing worth looking at until it is
	// settled. settling is the row a creation panel was opened for, or -1.
	flow     flow
	fromView view

	status string
	width  int
	height int
	// ready says the terminal has told us its size. Until it has, there is no
	// width to lay anything out against.
	ready bool
}

func New(ctx context.Context, ctrl app.Controller) Model {
	return Model{
		ctx: ctx, ctrl: ctrl, view: viewHoldings,
		current:  tableSurface{model: table.New(spec(viewHoldings).columns), view: viewHoldings},
		box:      omnibox.New(),
		editor:   editor.New(),
		creator:  creator.New(),
		flow:     newFlow(),
		jump:     table.New(jumpColumns).Fixed(),
		contents: map[view]bool{},
		folds:    map[view]map[int64]bool{},
	}
}

// jumpColumns are the palette's. KIND comes first and is never dropped:
// "Shelf 1" as a Location and "Shelf 1" inside a Holding path are different
// destinations, and a jump that does not say which lands somewhere surprising.
var jumpColumns = []table.Column{
	{Title: "KIND", Min: 8},
	{Title: "NAME", Min: 12, Grow: true, Elide: table.ElideStart},
}

func (m Model) Init() tea.Cmd { return m.load(m.view) }

// loadedMsg carries a rendered view. Exactly one of rows and cells is set: the
// tree and report views render to lines, the table views to cells.
type loadedMsg struct {
	view        view
	rows        []string
	cells       []table.Row
	nodes       []tree.Node
	holdingRows map[int64]app.HoldingRow
	status      string
}

type errMsg struct{ err error }

// candidatesMsg carries the flat index the jump palette searches, and the unit
// vocabulary the creation panel completes against.
//
// Both in one message because both are read when a panel opens and neither is
// worth a round trip of its own -- the units are seven rows of reference data.
type candidatesMsg struct {
	candidates []resolve.Candidate
	units      []string
}

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
			nodes, status, err := m.renderTree(v)
			if err != nil {
				return errMsg{err}
			}
			return loadedMsg{view: v, nodes: nodes, status: status}
		case surfaceTable:
			cells, byKey, status, err := m.renderTable(v)
			if err != nil {
				return errMsg{err}
			}
			return loadedMsg{view: v, cells: cells, holdingRows: byKey, status: status}
		default:
			rows, status, err := m.render(v, 0)
			if err != nil {
				return errMsg{err}
			}
			return loadedMsg{view: v, rows: rows, status: status}
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
		rows, status, err := m.render(viewHistory, id)
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
		m.ready = true
		m.current = m.current.SetSize(msg.Width, m.bodyHeight())
		m.jump = m.jump.SetSize(msg.Width, m.bodyHeight())
		return m, nil

	case loadedMsg:
		was := m.view
		m.view, m.status = msg.view, msg.status
		m.holdingRows = msg.holdingRows

		// A filter belongs to the VIEW it narrowed. `/rice` means nothing in
		// the Locations tree, and carrying it there filtered the whole house
		// down to nothing while the line still claimed to be showing rice.
		if was != msg.view {
			m.box = m.box.Clear()
		}

		// Where the cursor was, so a RELOAD can put it back. Reloading happens
		// after every write, and a cursor that jumped to the top each time
		// would make acting twice on one row impossible -- the second t of a
		// checkout-and-return would land on whatever had sorted first.
		//
		// Only within the same view: carrying a cursor across views would
		// restore a position nobody was in.
		var wasOn int64 = -1
		if was == msg.view {
			if sel, ok := m.current.Current(); ok {
				wasOn = sel.Key
			}
		}

		// The folds come back, because the tree is rebuilt after every write
		// and every toggle and one that sprang open each time would make
		// collapsing it pointless. Saved under the view being LEFT, which is
		// the case that loses them: switching away and back is the reload that
		// does not mention the tree it is replacing.
		if folds, ok := m.current.(folding); ok && forest(was) {
			m.folds[was] = folds.Folds()
		}
		m.current = m.surfaceFor(msg)

		// And the filter, which lives on the omnibox rather than on the
		// surface -- so a fresh surface arrives unfiltered while the line still
		// says it is filtered. Re-applying is what keeps the two agreeing.
		m = m.applyFilter()
		if wasOn >= 0 {
			m = m.focusKey(wasOn)
		}
		if m.pending != nil {
			m = m.focusKey(keyOf(*m.pending))
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
		m.problem = msg.issues
		m.status = ""
		return m, nil

	case importedMsg:
		m.problem = nil
		m.flow = m.flow.done()
		// Said through the load rather than before it, because a loadedMsg
		// carries the view's own status and would otherwise overwrite the only
		// report a whole import ever makes.
		m.status = fmt.Sprintf("applied %d rows in one transaction", msg.rows)
		m.view = viewHoldings
		return m, m.reloadKeepingStatus()

	case appliedMsg:
		m.problem = nil
		// The panel closes only once something was actually created. Escaping
		// the confirmation returns to the panel with what was typed still in
		// it, which is what makes the confirmation a step rather than a
		// dead end.
		m.creator = m.creator.Close()
		// And when it was opened FOR a row of an import, that row is re-bound
		// now that the thing it named exists.
		if at, ok := m.flow.settlingRow(); ok {
			m.flow = m.flow.settled()
			entry, _, _ := m.flow.plan.Current()
			m = m.rebindRow(at, entry)
			return m, nil
		}
		// Reload, because something changed. The list a person is looking at
		// must not disagree with the house.
		m.status = msg.summary
		return m, m.reloadKeepingStatus()

	case candidatesMsg:
		m.candidates, m.units = msg.candidates, msg.units
		m.index = resolve.NewIndex(msg.candidates)
		m.creator = m.creator.WithUnits(msg.units)
		m = m.refreshJump()
		if m.creator.IsOpen() {
			m = m.suggest()
		}
		if m.editor.IsOpen() {
			m = m.suggestForPrompt()
		}
		return m, nil

	case errMsg:
		m.status = "error: " + msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		// The innermost open mode takes it; the list underneath takes what
		// nothing else wanted. See layers.go for the order and why it is that.
		if top, ok := m.topLayer(); ok {
			return top.handle(m, msg)
		}
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey is the application underneath every mode: views, leaders, quit, and
// the verbs that act on a row.
//
// It runs last, after every mode that could be open, and it is the only place
// that sees a keystroke nothing else wanted.
func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	// The surface sees motion, selection, and sorting first. It reports what it
	// did not use, so the keys that belong to the application -- views, quit,
	// enter -- still reach it.
	if next, handled := m.current.Update(msg); handled {
		m.current = next
		return m, nil
	}

	// The verbs come before the generic keys, so that consume, count, move and
	// custody mean what the plan says rather than falling through to motion.
	// Each one decides for itself which views it applies to.
	if next, cmd, handled := m.handleAction(msg); handled {
		return next, cmd
	}

	switch keys.Lookup(keys.Browse, msg) {
	case keys.Quit:
		return m, tea.Quit

	// The three leaders. C-s and M-g feel identical for exactly one keystroke
	// and then diverge completely, which is why the omnibox renders them
	// differently before a word has been read.
	//
	// C-s opens the line whether or not a filter is already on, because Open
	// prefills it with the applied filter -- so refining a filter is the same
	// gesture as making one. Stepping through the matches is M-n and M-p, in
	// the table.
	case keys.Search:
		m.problem = nil
		m.box = m.box.Open(omnibox.Filter)
		m = m.applyLive()
		return m, nil
	case keys.Jump:
		m.box = m.box.Open(omnibox.Jump)
		m = m.refreshJump()
		return m, m.loadCandidates()
	case keys.CommandLine:
		m.problem = nil
		m.box = m.box.Open(omnibox.Command)
		return m, nil

	// Renames whatever the cursor is on, in place.
	case keys.EditInPlace:
		return m.openEditor(), nil

	// Creates a new one of whatever this view holds, inside what the cursor is
	// on. Creating inside what you are looking at is what it means.
	case keys.Create:
		return m.openCreator(), m.loadCandidates()

	case keys.Confirm:
		// In a table C-f and C-b move between columns, so enter is the only way
		// down into a row -- which is also what it means everywhere else.
		if m.view == viewHoldings {
			if sel, ok := m.current.Current(); ok {
				m.fromView = m.view
				return m, m.loadHistory(domain.HoldingID(sel.Key))
			}
		}
	case keys.Cancel:
		// Escaping in the LIST clears an applied filter -- a different escape
		// from the one that closes the input line.
		if m.box.Applied() != "" {
			m.box = m.box.Clear()
			return m.filterWith(omnibox.Query{}), nil
		}
		// History and Help are both entered from somewhere and left back to
		// it. Neither has a number, so escaping is the only way out.
		if m.view == viewHistory || m.view == viewHelp {
			return m, m.load(m.fromView)
		}
		// Putting down what is being carried, LAST in this chain.
		//
		// Filtering and selecting are things you do while hunting for the
		// destination, so esc has to undo the most recent of those first --
		// otherwise looking for somewhere to put a thing would make you drop
		// it. Dropping a carry with a filter on takes two escapes, which is
		// the same shape as every other nested mode here.
		if m.copied != nil {
			m.status = fmt.Sprintf("put %q down", m.copied.Name)
			m.copied = nil
			return m, nil
		}

	case keys.ViewCategories:
		return m, m.load(viewCategories)
	case keys.ViewLocations:
		return m, m.load(viewLocations)
	case keys.ViewItems:
		return m, m.load(viewItems)
	case keys.ViewHoldings:
		return m, m.load(viewHoldings)
	case keys.ViewIntegrity:
		return m, m.load(viewIntegrity)
	case keys.Refresh:
		return m, m.load(m.view)
	case keys.ShowContents:
		// A tree only. The tables already show what they hold -- that is what
		// a table is -- so the key has nothing to say there.
		if !forest(m.view) {
			return m, nil
		}
		m.contents[m.view] = !m.contents[m.view]
		return m, m.load(m.view)
	}
	return m, nil
}

func (m Model) View() string {
	if !m.ready {
		return "loading..."
	}
	// The editor's lines go INTO the surface, spliced after the row they belong
	// to, rather than under the whole list. Under the list is not inline; it is
	// a second place to look.
	overlay := m.field().Lines()
	if m.creator.IsOpen() {
		overlay = m.creator.SetWidth(m.width).Lines()
	}

	// Every surface scrolls itself, so each renders straight rather than
	// through a viewport somebody else owns. Two things scrolling one list is
	// how a cursor ends up off screen with nothing obviously wrong.
	body := m.current.SetOverlay(overlay).View()
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

	if m.flow.reviewing() && m.confirm == nil {
		// The plan REPLACES the house. A file proposing a batch of changes is
		// not something to look at alongside what you own; it is the only thing
		// worth looking at until it is settled.
		//
		// And a panel opened for one of its rows sits ON the plan, not on the
		// house behind it -- otherwise settling a row shows you a screen that
		// has nothing to do with what you are settling.
		// The field goes INTO the plan, spliced after the row it belongs to --
		// the same overlay the browse views use, for the same reason.
		//
		// The creation panel does not: it is about a thing that does not exist
		// yet rather than about the row's text, and it is tall enough that
		// splicing it would push the plan off the screen it is confirming.
		height := m.height - 2 - len(m.problemLines()) - m.creator.Height()
		plan := m.flow.plan.SetSize(m.width, height).SetOverlay(m.editor.SetWidth(m.width).Lines())
		parts := []string{plan.View()}
		if m.creator.IsOpen() {
			parts = append(parts, m.creator.SetWidth(m.width).Lines()...)
		}
		for _, line := range m.problemLines() {
			parts = append(parts, style.Error.Render(line))
		}
		return strings.Join(parts, "\n")
	}

	parts := []string{m.header()}
	// Above the body, with the view tabs, because that is where a MODE belongs:
	// it is true of the whole screen rather than of the row under the cursor,
	// and it has to be readable in whichever view you have navigated to.
	for _, line := range m.carryLine() {
		parts = append(parts, style.Strong.Render(line))
	}
	parts = append(parts, body)
	if line := m.box.View(); line != "" {
		parts = append(parts, line)
	}
	for _, line := range m.problemLines() {
		parts = append(parts, style.Error.Render(line))
	}
	return strings.Join(append(parts, m.footer()), "\n")
}

// field is the inline editor, told the one thing it cannot know: whether esc
// will throw the answer away or leave the thing it was opened over in hand.
func (m Model) field() editor.Model {
	f := m.editor.SetWidth(m.width)
	if m.copied != nil {
		return f.WithCancel("go and point at it instead")
	}
	return f
}

// problemLines is the refusal, humanised and wrapped to the terminal.
func (m Model) problemLines() []string {
	if len(m.problem) == 0 {
		return nil
	}
	var out []string
	for _, issue := range m.problem {
		out = append(out, wrap(humanise(issue), max(20, m.width-2))...)
	}
	for i := range out {
		out[i] = "  " + out[i]
	}
	return out
}

// carryLine says what is being held, and where it can be put down.
//
// It exists because the carry was INVISIBLE. carry() announced itself through
// m.status, and every loadedMsg overwrites m.status -- so the one keystroke the
// gesture requires in the middle, the view switch, destroyed the only evidence
// that anything was being carried. You picked a thing up, went to the tree, and
// the screen said nothing at all.
//
// Worded per kind, because where a thing can go is the useful half: a Holding
// goes in a place, an Item is filed under a classification, and a banner that
// said only "carrying X" would leave the person to find that out by being
// refused.
func (m Model) carryLine() []string {
	if m.copied == nil {
		return nil
	}
	goes := "files it under a classification"
	if m.copied.Kind == "Holding" {
		goes = "puts it in a place"
	}
	text := fmt.Sprintf("carrying %q -- %s %s, %s puts it down",
		m.copied.Name,
		keys.Show(keys.Browse, keys.Paste), goes,
		keys.Show(keys.Browse, keys.Cancel))
	out := wrap(text, max(20, m.width-2))
	for i := range out {
		out[i] = "  " + out[i]
	}
	return out
}

// bodyHeight is the room left after the header, the footer, and the input line
// when there is one.
func (m Model) bodyHeight() int {
	// The editor is NOT subtracted: it takes lines from inside the surface
	// rather than from around it, so the screen keeps its shape.
	//
	// The carry banner IS, along with the refusal: both sit outside the surface
	// rather than inside it, so the rows have to give up the lines they take.
	if h := m.height - 4 - m.box.Height() - len(m.problemLines()) - len(m.carryLine()); h > 3 {
		return h
	}
	return 3
}

// countPhrase says how many, and how many of how many when a filter is on.
func (m Model) countPhrase() string {
	noun := spec(m.view).noun
	if noun == "" {
		return ""
	}
	if shown, total, filtered := m.counts(); filtered {
		return fmt.Sprintf("%d of %d %s", shown, total, noun)
	}
	_, total := m.current.Counts()
	return fmt.Sprintf("%d %s", total, noun)
}

// selectionCount is how many rows were explicitly picked on whichever surface
// is showing.
func (m Model) selectionCount() int { return m.current.SelectionCount() }

// counts reports the filtered and total row counts of whichever surface is
// showing, and whether a filter is in force at all.
func (m Model) counts() (shown, total int, filtered bool) {
	if m.current.Filtered() {
		shown, total = m.current.Counts()
		return shown, total, true
	}
	return 0, 0, false
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
			rendered = append(rendered, style.Strong.Render("["+label+"]"))
		} else {
			rendered = append(rendered, style.Dim.Render(" "+label+" "))
		}
	}
	if m.view == viewHistory || m.view == viewHelp {
		suffix := spec(m.view).name
		if !names {
			suffix = suffix[:1]
		}
		rendered = append(rendered, style.Strong.Render("["+suffix+"]"))
	}
	return strings.Join(rendered, " ") + "\n" + style.Dim.Render(strings.Repeat("-", max(10, m.width)))
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
		out = append(out, tab{view: v, name: spec(v).name, key: int(v) + 1})
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

// joinWhatFits joins as many hints as the width allows, dropping from the end.
//
// Named apart from table.fit, which cuts ONE string to a width. Two functions
// called fit that do different things is a name that has to be read twice.
func joinWhatFits(width int, parts []string) string {
	line := ""
	for _, part := range parts {
		next := part
		if line != "" {
			next = line + " - " + part
		}
		if len([]rune(next)) > width {
			break
		}
		line = next
	}
	return line
}

func (m Model) footer() string {
	// Named in the order they would be given up, most useful first, and cut to
	// the terminal rather than allowed to wrap. A line wider than the screen
	// wraps, and one wrapped line shifts every row below it -- which is the
	// same reason the table drops columns instead of overflowing.
	help := joinWhatFits(m.width, []string{
		keys.Hint(keys.Table,
			[]keys.Action{keys.MoveDown, keys.MoveUp},
			[]keys.Action{keys.MoveLeft, keys.MoveRight}),
		"1-5 views",
		keys.Hint(keys.Browse, []keys.Action{keys.Confirm}),
		keys.Hint(keys.Table, []keys.Action{keys.ToggleSelect}, []keys.Action{keys.Sort}),
		keys.Hint(keys.Browse, []keys.Action{keys.Quit}),
	})
	rule := style.Dim.Render(strings.Repeat("-", max(10, m.width)))

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
		return rule + "\n" + style.Dim.Render(fmt.Sprintf(
			"%d matches across every kind - %s go - %s cancel",
			shown, keys.Show(keys.Line, keys.Confirm), keys.Show(keys.Line, keys.Cancel)))
	}

	var parts []string
	if phrase := m.countPhrase(); phrase != "" {
		parts = append(parts, phrase)
	}
	if m.status != "" {
		parts = append(parts, m.status)
	}
	if sorted := m.current.SortDescription(); sorted != "" {
		parts = append(parts, "sorted "+sorted)
	}
	if n := m.selectionCount(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d selected", n))
	}
	status := strings.Join(parts, " - ")

	if status != "" {
		return rule + "\n" + status
	}
	return rule + "\n" + style.Dim.Render(help)
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
		status := "every key and every command"
		if m.helpTopic != "" {
			status = m.helpTopic
		}
		return lines, status, nil

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
		status := "clean"
		if !report.Clean() {
			status = style.Strong.Render(fmt.Sprintf("%d discrepancies, %d orphans",
				len(report.Discrepancies), len(report.Orphans)))
		}
		return out, status, nil

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

// Run starts the program.
// RunModel starts the interface on a model that is already set up, which is
// how an import arrives: the file is read and bound before the terminal is
// touched, so a file that cannot be read fails as a command-line error rather
// than as a blank screen.
func RunModel(m Model) error {
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

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

// ---------------------------------------------------------------------------
// The omnibox: filter, and jump
// ---------------------------------------------------------------------------

// handleOmnibox takes the keystroke when the input line is open.
//
// It runs before everything else, because while the line is open a keystroke is
// a CHARACTER. A `j` that moved the cursor while someone was typing "jar" would
// make the input line unusable, and it is the classic way a modal interface
// betrays the person using it.
func (m Model) handleOmnibox(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch keys.Lookup(keys.Line, msg) {
	case keys.Cancel:
		m.box = m.box.Cancel()
		// Restoring the ACCEPTED filter, not merely closing the line. Rows
		// narrow as you type, so an abandoned edit leaves the half-typed filter
		// on the table -- which showed up as "0 of 12 holdings" under a jump
		// palette, long after the filter that produced it had been cancelled.
		return m.applyFilter(), nil
	case keys.Confirm:
		if m.box.Mode() == omnibox.Jump {
			return m.acceptJump()
		}
		if m.box.Mode() == omnibox.Command {
			line := m.box.Input()
			m.box = m.box.Accept()
			// help is answered here rather than bound and planned, because it
			// is the one line that acts on the INTERFACE instead of on the
			// house. Sending it through Bind would need a Command that writes
			// nothing, and a write path with a no-op in it is a write path
			// somebody will one day give something to do.
			if topic, ok := helpAsked(line); ok {
				m.problem, m.status = nil, ""
				m.helpTopic = topic
				if m.view != viewHelp {
					m.fromView = m.view
				}
				return m, m.load(viewHelp)
			}
			m.status = "working..."
			return m, m.runLine(line)
		}
		m.box = m.box.Accept()
		m = m.applyFilter()
		return m, nil
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
		return m, nil
	}
	// A key the input line does not want -- cursor motion through the results,
	// which the palette owns.
	if m.box.Mode() == omnibox.Jump {
		if next, handled := m.jump.Update(msg); handled {
			m.jump = next
			return m, nil
		}
	}
	return m, nil
}

// applyFilter puts the accepted filter onto whichever surface is showing.
func (m Model) applyFilter() Model { return m.filterWith(m.box.Query()) }

// applyLive puts the half-typed filter on, which is what makes rows narrow as
// the line is typed rather than when it is accepted.
func (m Model) applyLive() Model { return m.filterWith(m.box.Live()) }

func (m Model) filterWith(q omnibox.Query) Model {
	m.current = m.current.SetFilter(q)
	return m
}

// facetTests resolves facet names to columns, dropping the ones this view has
// no field for.
func facetTests(v view, q omnibox.Query) []table.FacetTest {
	columns := spec(v).facets
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
func (m Model) acceptJump() (Model, tea.Cmd) {
	row, ok := m.jump.Current()
	if !ok || int(row.Key) >= len(m.candidates) {
		m.box = m.box.Cancel()
		return m, nil
	}
	target := m.candidates[row.Key]
	m.box = m.box.Cancel()
	m.pending = &target
	return m, m.load(viewFor(target.Kind))
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
