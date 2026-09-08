package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/omnibox"
	"home-management-system/internal/tui/table"
	"home-management-system/internal/tui/tree"

	"home-management-system/internal/tui/style"
)

// selection is what the cursor is on, said the same way whatever is showing.
//
// Every verb in this interface asks the same four questions of a row -- what is
// it, what is it called, where is it, and what identifier is behind it -- and
// they used to be answered twice: once by reading a table.Row's cells by index,
// once by reading a tree.Node's fields. Two answers to one question is how `e`
// on an item row came to ask to rename a Category that does not exist.
type selection struct {
	// Key identifies the row on its own surface, and is what Focus takes. For a
	// tree it is Node.Key(), which carries the kind in its high bits so that
	// Category 7 and Item 7 are not the same row; for a table it is the
	// identifier itself.
	Key int64
	// ID is the identifier in the row's OWN table, which is what a command
	// naming this thing has to be given. On a table it equals Key; on a tree it
	// is Key with the kind bits taken back off.
	ID int64
	// Kind says what this row IS -- "Item", "Holding", "Category", "Location".
	// Taken from the row rather than from the view where the row knows, because
	// a tree can show what its nodes contain and the view no longer knows.
	Kind string
	// Subject is the kind a COMMAND naming this row uses, which is not always
	// what the row is: both table views name an Item in their first column, and
	// the stock commands name an Item, which is what lets `:consume 100g` on a
	// holdings row mean the same thing the `c` keystroke will.
	//
	// Kind and Subject used to be one field, and it held the Subject answer --
	// so every table row claimed to be an Item while its ID was a Holding's.
	// Nothing read it wrongly, because the readers that wanted the row's real
	// kind were tree-only and the one that wanted the subject got what it
	// needed. It was a field whose doc invited the next reader to trust it for
	// the other thing.
	Subject string
	Name    string
	// At is the place, for a holdings row. A command typed while pointing at
	// one of three shelves needs it or it has to ask which shelf you meant.
	At string
	// Contained marks a row that is INSIDE a node rather than being one -- an
	// Item under a Category. It cannot be renamed or created into from here.
	Contained bool
}

// surface is the thing a view draws itself on: a table, a tree, or scrolling
// prose. It is the answer to `if tabular(m.view) ... if forest(m.view) ...`,
// which this package wrote thirty-five times before this existed and got
// subtly wrong twice -- restoreCursor and land are the same function, and only
// one of them knew that a tree key is not a row key.
//
// Every method that changes anything returns a surface rather than mutating,
// matching the widgets underneath, which are values.
type surface interface {
	// Update reports whether it used the keystroke, so keys the surface does
	// not want -- views, quit, the verbs -- still reach the application.
	Update(tea.KeyMsg) (surface, bool)
	View() string
	SetSize(width, height int) surface
	SetOverlay(lines []string) surface
	SetFilter(q omnibox.Query) surface

	Current() (selection, bool)
	Selected() []selection
	SelectionCount() int
	Filtered() bool
	Counts() (shown, total int)
	// SortDescription is "" where sorting is not a thing the surface does.
	SortDescription() string
	// Focus puts the cursor on a row by Key, and does nothing if it is not
	// here -- a reload can drop the row somebody was on.
	Focus(key int64) surface
}

// folding is the part of a tree no other surface has. Kept off the surface
// interface and reached by assertion, because a table asked for its folds would
// have to invent an answer, and an invented answer is the thing that goes stale.
type folding interface {
	Folds() map[int64]bool
}

// ---------------------------------------------------------------------------
// The table surface: Holdings and Items.
// ---------------------------------------------------------------------------

// tableSurface adapts table.Model. It carries the view because only the view
// knows which column a facet restricts and which cell holds the place.
type tableSurface struct {
	model table.Model
	view  view
}

func (s tableSurface) with(m table.Model) surface { s.model = m; return s }

func (s tableSurface) Update(msg tea.KeyMsg) (surface, bool) {
	next, handled := s.model.Update(msg)
	return s.with(next), handled
}

func (s tableSurface) View() string                  { return s.model.View() }
func (s tableSurface) SetSize(w, h int) surface      { return s.with(s.model.SetSize(w, h)) }
func (s tableSurface) SetOverlay(l []string) surface { return s.with(s.model.SetOverlay(l)) }
func (s tableSurface) SelectionCount() int           { return s.model.SelectionCount() }
func (s tableSurface) Filtered() bool                { return s.model.Filtered() }
func (s tableSurface) Counts() (int, int)            { return s.model.Counts() }
func (s tableSurface) SortDescription() string       { return s.model.SortDescription() }

func (s tableSurface) SetFilter(q omnibox.Query) surface {
	return s.with(s.model.SetFilter(table.Filter{Text: q.Text, Facets: facetTests(s.view, q)}))
}

// rowSelection reads a row the one way, so the cursor and the selection cannot
// disagree about what they are pointing at.
//
// Both table views name an Item in their first column, which is what lets
// `:consume 100g` on a row mean the same thing the `c` keystroke will.
func (s tableSurface) rowSelection(r table.Row) selection {
	sel := selection{
		Key: r.Key, ID: r.Key, Name: r.Cells[0],
		Kind:    string(spec(s.view).rowKind()),
		Subject: "Item",
	}
	if s.view == viewHoldings && len(r.Cells) > 2 {
		sel.At = r.Cells[2]
	}
	return sel
}

func (s tableSurface) Current() (selection, bool) {
	row, ok := s.model.Current()
	if !ok {
		return selection{}, false
	}
	return s.rowSelection(row), true
}

func (s tableSurface) Selected() []selection {
	picked := map[int64]bool{}
	for _, key := range s.model.Selected() {
		picked[key] = true
	}
	var out []selection
	for _, row := range s.model.Rows() {
		if picked[row.Key] {
			out = append(out, s.rowSelection(row))
		}
	}
	return out
}

func (s tableSurface) Focus(key int64) surface {
	for i, row := range s.model.Rows() {
		if row.Key == key {
			return s.with(s.model.SetCursor(i))
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// The tree surface: Categories and Locations.
// ---------------------------------------------------------------------------

type treeSurface struct{ model tree.Model }

func (s treeSurface) with(m tree.Model) surface { s.model = m; return s }

func (s treeSurface) Update(msg tea.KeyMsg) (surface, bool) {
	next, handled := s.model.Update(msg)
	return s.with(next), handled
}

func (s treeSurface) View() string                  { return s.model.View() }
func (s treeSurface) SetSize(w, h int) surface      { return s.with(s.model.SetSize(w, h)) }
func (s treeSurface) SetOverlay(l []string) surface { return s.with(s.model.SetOverlay(l)) }
func (s treeSurface) SelectionCount() int           { return s.model.SelectionCount() }
func (s treeSurface) Filtered() bool                { return s.model.Filtered() }
func (s treeSurface) Counts() (int, int)            { return s.model.Counts() }
func (s treeSurface) SortDescription() string       { return "" }
func (s treeSurface) Folds() map[int64]bool         { return s.model.Folds() }
func (s treeSurface) Focus(key int64) surface       { return s.with(s.model.Focus(key)) }

// A tree filters on text alone: its facets would name columns it does not have.
func (s treeSurface) SetFilter(q omnibox.Query) surface {
	return s.with(s.model.SetFilter(q.Text))
}

// nodeSelection reads the NODE, not the view. Reading the view was safe while a
// tree held only its own kind and stopped being safe the moment a Category
// could show its Items.
func nodeSelection(n tree.Node) selection {
	// A tree node names itself: what it is and what a command calls it are the
	// same thing.
	return selection{
		Key: n.Key(), ID: n.ID, Name: n.Name,
		Kind: n.Kind, Subject: n.Kind, Contained: n.Contained(),
	}
}

func (s treeSurface) Current() (selection, bool) {
	node, ok := s.model.Current()
	if !ok {
		return selection{}, false
	}
	return nodeSelection(node), true
}

func (s treeSurface) Selected() []selection {
	picked := map[int64]bool{}
	for _, key := range s.model.Selected() {
		picked[key] = true
	}
	var out []selection
	for _, node := range s.model.Nodes() {
		if picked[node.Key()] {
			out = append(out, nodeSelection(node))
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// The text surface: Integrity, History, and Help.
// ---------------------------------------------------------------------------

// textSurface is prose that scrolls, with a cursor on a line.
//
// It exists so that "which surface is showing" has three answers rather than
// two-and-a-special-case. These three views used to live as loose rows, cursor
// and viewport fields on the Model, which meant the application handled their
// motion keys itself -- the only surface whose scrolling was somebody else's
// job, and the reason C-n past the last visible line moved a cursor nobody
// could see while the screen sat still.
type textSurface struct {
	rows   []string
	cursor int
	port   viewport.Model
}

func newTextSurface(rows []string, width, height int) surface {
	s := textSurface{rows: rows, port: viewport.New(width, max(1, height))}
	s.port.SetContent(s.content())
	return s
}

// content is the rows with the cursor drawn on one of them.
func (s textSurface) content() string {
	if len(s.rows) == 0 {
		return style.Dim.Render("  (nothing here)")
	}
	var b strings.Builder
	for i, row := range s.rows {
		if i == s.cursor {
			b.WriteString(style.Highlight.Render("> " + row))
		} else {
			b.WriteString("  " + row)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// follow redraws and scrolls the least amount that puts the cursor back on
// screen, which is what the table and the tree already do for themselves.
func (s textSurface) follow() surface {
	s.port.SetContent(s.content())
	if s.cursor < s.port.YOffset {
		s.port.SetYOffset(s.cursor)
	}
	if bottom := s.port.YOffset + s.port.Height; s.cursor >= bottom {
		s.port.SetYOffset(s.cursor - s.port.Height + 1)
	}
	return s
}

func (s textSurface) Update(msg tea.KeyMsg) (surface, bool) {
	switch keys.Lookup(keys.Browse, msg) {
	case keys.MoveDown:
		if s.cursor < len(s.rows)-1 {
			s.cursor++
			return s.follow(), true
		}
		return s, true
	case keys.MoveUp:
		if s.cursor > 0 {
			s.cursor--
			return s.follow(), true
		}
		return s, true
	case keys.Top:
		s.cursor = 0
		s.port.SetContent(s.content())
		s.port.GotoTop()
		return s, true
	case keys.Bottom:
		s.cursor = max(0, len(s.rows)-1)
		s.port.SetContent(s.content())
		s.port.GotoBottom()
		return s, true
	}
	return s, false
}

func (s textSurface) View() string { return s.port.View() }

func (s textSurface) SetSize(width, height int) surface {
	s.port.Width, s.port.Height = width, max(1, height)
	s.port.SetContent(s.content())
	return s
}

// An overlay belongs to a surface that has rows to put it under. These do not.
func (s textSurface) SetOverlay([]string) surface     { return s }
func (s textSurface) SetFilter(omnibox.Query) surface { return s }
func (s textSurface) Current() (selection, bool)      { return selection{}, false }
func (s textSurface) Selected() []selection           { return nil }
func (s textSurface) SelectionCount() int             { return 0 }
func (s textSurface) Filtered() bool                  { return false }
func (s textSurface) Counts() (int, int)              { return len(s.rows), len(s.rows) }
func (s textSurface) SortDescription() string         { return "" }
func (s textSurface) Focus(int64) surface             { return s }
