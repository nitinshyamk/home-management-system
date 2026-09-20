package tui

import (
	"context"

	"home-management-system/internal/app"
	"home-management-system/internal/command"
	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/creator"
	"home-management-system/internal/tui/editor"
	"home-management-system/internal/tui/omnibox"
	"home-management-system/internal/tui/status"
	"home-management-system/internal/tui/table"
	"home-management-system/internal/tui/text"

	tea "github.com/charmbracelet/bubbletea"
)

// The model: what the interface is, and how it is started.

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
		ctx: ctx, ctrl: ctrl, view: viewHoldings,
		current:  tableSurface{model: table.New(spec(viewHoldings).columns), view: viewHoldings},
		box:      omnibox.New(),
		editor:   editor.New(),
		creator:  creator.New(),
		flow:     newFlow(),
		say:      status.New(),
		contents: map[view]bool{},
		folds:    map[view]map[int64]bool{},
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
