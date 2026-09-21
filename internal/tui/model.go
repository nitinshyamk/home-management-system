package tui

import (
	"context"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/creator"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/omnibox"
	"home-management-system/internal/tui/status"
	"home-management-system/internal/tui/table"
	"home-management-system/internal/tui/text"
	"home-management-system/internal/tui/tree"

	tea "github.com/charmbracelet/bubbletea"
)

// The model: what the interface is, and how it is started.
//
// `view` and its four values live in views.go, beside the specs that describe
// them.

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
	// candidates is everything there is to name: what the resolver resolves
	// against, what the completions offer, and what the jump palette searches.
	candidates []resolve.Candidate
	// index is the candidates, ready to resolve against. Built once when they
	// arrive rather than per keystroke: named() rebuilt it on every character
	// typed into a prompt, which is a whole-house index per keypress.
	index *resolve.Index
	// units is the unit vocabulary, cached beside the candidates because both
	// are read together and neither changes while a panel is open.
	units []string
	// lens is which tree the rail is showing: the places, or the kinds.
	lens lens
	// railKey is where the rail's cursor is, PER LENS, as a tree key.
	//
	// Per lens because the two lenses are two positions, and a flip is meant
	// to be reversible: going to see what kind a thing is and coming back
	// should put you back on the shelf you left, not at the top of the house.
	// Zero means the synthetic root -- all of it -- which is where a fresh
	// interface starts.
	railKey map[lens]int64
	// deep says whether the contents pane rolls up the whole subtree or shows
	// only what is filed at the node itself.
	//
	// Both are real questions. Deep is "what is in the garage"; shallow is
	// "what did I file at the garage rather than on a shelf in it", which is
	// how something filed too coarsely becomes findable.
	deep bool
	// folds is each tree's collapsed nodes, kept here rather than on the tree
	// because the tree is rebuilt from scratch on every load.
	//
	// Per LENS, and surviving a flip -- unlike the cursor, which is also per
	// lens but for the opposite reason. A fold is a statement about the SHAPE
	// of one tree, and it is still true when you come back to it.
	folds map[lens]map[int64]bool
	// helpTopic is the command `help` was asked about, or "" for all of it.
	helpTopic string
	// pending is where a jump is going, held until the destination view has
	// loaded and there is a row to put the cursor on.
	pending *resolve.Candidate

	// editor is the in-place field. confirm is the one thing that stands
	// between a person and a permanent change.
	editor  editor.Model
	creator creator.Model
	confirm *pendingPlan

	// holdingRows is what is on screen, by identifier. A keystroke reaches the
	// identifiers behind its row through this rather than through the display
	// strings, which is what lets it build a Command without resolving a name.
	holdingRows map[int64]app.HoldingRow
	// itemRows is every Item, by identifier.
	//
	// EVERY one, not only what is on screen, because the inspector says what a
	// holding's item is classified as and how much of it the whole house has,
	// and neither of those is on a HoldingRow. Reading them per row was the
	// alternative and it is the N+1 the controller's own comments warn about.
	itemRows map[domain.ItemID]app.ItemRow
	// total is what the rail's root rolls up, for the header.
	total int64

	// drawer is whatever is open below the house -- the destinations a move
	// offers, the verbs a row takes, the plans waiting, the plan being
	// reviewed. Nil the rest of the time, which is nearly always.
	//
	// ONE field, because the region is one region. It was four, and each new
	// one had to be wired into the model, the layer list, the renderer and
	// the height arithmetic before it worked; the plan never got the fourth
	// and so never shrank the house behind it.
	drawer   drawer
	fromView view

	// say is everything the interface has to say about itself: the view's
	// hint, what just happened or why it did not, what is in hand, and what is
	// in flight. Four lifetimes, which is why it is a value of its own rather
	// than the two fields it replaces.
	say    status.Model
	width  int
	height int
	// ready says the terminal has told us its size. Until it has, there is no
	// width to lay anything out against.
	ready bool
}

func New(ctx context.Context, ctrl app.Controller) Model {
	return Model{
		ctx: ctx, ctrl: ctrl, view: viewShell,
		// By place, at the root, rolled up -- which is the old Holdings tab,
		// arrived at as one node of a tree rather than as a tab of its own.
		lens: lensPlace,
		deep: true,
		current: shellSurface{
			lens: lensPlace,
			rail: tree.New(railCountTitle),
			body: table.New(lensPlace.spec().columns),
			// On the contents, not the rail: opening the house should land
			// where the things are, which is what the Holdings tab did.
			on: paneBody,
		},
		box:     omnibox.New(),
		editor:  editor.New(),
		creator: creator.New(),
		say:     status.New(),
		railKey: map[lens]int64{},
		folds:   map[lens]map[int64]bool{},
	}
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

// holding is a row of the contents pane, by key.
func (m Model) holding(key int64) (app.HoldingRow, bool) {
	row, ok := m.holdingRows[key]
	return row, ok
}

// item is an Item by identifier, from the whole house rather than from what is
// on screen.
func (m Model) item(id domain.ItemID) (app.ItemRow, bool) {
	row, ok := m.itemRows[id]
	return row, ok
}

// railAt is the key of the node the rail is on, or zero where no rail is
// showing.
func (m Model) railAt() int64 {
	sh, ok := m.shell()
	if !ok {
		return 0
	}
	return sh.railAt()
}

// rememberRail records where the rail's cursor ended up, under the lens it
// belongs to, so a flip away and back returns to it.
//
// Called after every load rather than on every keystroke: the rail moves
// inside the surface, which the model does not see, and the load is the one
// moment the model and the surface are known to agree.
func (m Model) rememberRail() Model {
	sh, ok := m.shell()
	if !ok {
		return m
	}
	if node, ok := sh.railNode(); ok {
		m.railKey[sh.lens] = node.Key()
	}
	return m
}

// wrap breaks text onto as many lines as it needs, never narrower than is
// readable. It defers to text.Wrap, which is where the one implementation
// lives now that the status block wraps its own lines.
func wrap(s string, width int) []string { return text.Wrap(s, max(20, width)) }

// humanise defers to command.Humanise, which is where the list lives: an
// importer, a plan screen, and this all render the same errors, and three
// copies of the list would drift.
func humanise(issue string) string { return command.Humanise(issue) }

// humaniseAll is humanise over a refusal's several reasons.
func humaniseAll(issues []string) []string {
	out := make([]string, 0, len(issues))
	for _, issue := range issues {
		out = append(out, humanise(issue))
	}
	return out
}

// The few facts a test legitimately needs about where the interface is.
// Anything about DATA belongs in an assertion against the database.

// OnRail reports whether the cursor is on the structure.
func (m Model) OnRail() bool { return m.onRail() }

// ShowingKinds reports which lens the rail is on.
func (m Model) ShowingKinds() bool { return m.lens == lensKind }

// RailName is the node the rail's cursor is on, or "".
func (m Model) RailName() string {
	sh, ok := m.shell()
	if !ok {
		return ""
	}
	node, ok := sh.railNode()
	if !ok {
		return ""
	}
	return node.Name
}

// ContentRows is the first cell of every row in the contents pane, and
// RailRows the same for the rail.
//
// Tests used to count occurrences in the whole rendered view. That stopped
// working when the inspector started naming the row under the cursor -- three
// piles of chile plus one mention is four. Counting rows is what those
// assertions meant.
func (m Model) ContentRows() []string {
	sh, ok := m.shell()
	if !ok {
		return nil
	}
	return firstCells(sh.body.Rows())
}

func (m Model) RailRows() []string {
	sh, ok := m.shell()
	if !ok {
		return nil
	}
	out := make([]string, 0, len(sh.rail.Nodes()))
	for _, n := range sh.rail.Nodes() {
		out = append(out, n.Name)
	}
	return out
}

func firstCells(rows []table.Row) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if len(r.Cells) > 0 {
			out = append(out, r.Cells[0])
		}
	}
	return out
}

// RailView and ContentsView are each half on its own, for assertions that
// mean one of them. "The tree no longer shows the tray" is not the same claim
// as "the tray is nowhere on screen": it is still in the contents pane's
// WHERE column, correctly.
func (m Model) RailView() string {
	sh, ok := m.shell()
	if !ok {
		return ""
	}
	return sh.rail.View()
}

func (m Model) ContentsView() string {
	sh, ok := m.shell()
	if !ok {
		return ""
	}
	return sh.body.View()
}

// Offers is what the open field is suggesting.
//
// The rail and the inspector legitimately name places and categories, so
// "the screen does not show Garage" stopped being a claim about what a
// destination field offered.
func (m Model) Offers() []string {
	if m.creator.IsOpen() {
		return m.creator.Suggestions()
	}
	return m.editor.Suggestions()
}
