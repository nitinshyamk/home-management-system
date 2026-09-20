package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/omnibox"

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

// surface is the thing a screen draws itself on: the shell, or scrolling
// prose. It is the answer to `if tabular(m.view) ... if forest(m.view) ...`,
// which this package wrote thirty-five times before this existed and got
// subtly wrong twice -- restoreCursor and land are the same function, and only
// one of them knew that a tree key is not a row key.
//
// There used to be three implementations and one of them per browse tab. The
// tabs are gone: shellSurface (shell.go) is the tree AND the table, side by
// side, which is what those four views had been between them all along.
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

// targeting is the part of a rail no other surface has: something is being
// carried, and this tree is being read for somewhere to put it down.
//
// Kept off the surface interface and reached by assertion, because a page of
// prose asked to rule rows out would have to invent which.
type targeting interface {
	SetTargeting(on bool) surface
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
	// headings are the rows that start a section, for TAB and S-TAB.
	//
	// Line-at-a-time is the only motion a page of prose has, and on a listing
	// four screens long that is the difference between finding the section you
	// want and scrolling past it. Empty where the rows have no sections, and
	// then the keys do nothing rather than something arbitrary.
	headings []int
}

// newSectionedText is a text surface whose rows have headings worth jumping
// between.
func newSectionedText(rows []string, headings []int, width, height int) surface {
	s := textSurface{rows: rows, headings: headings, port: viewport.New(width, max(1, height))}
	s.port.SetContent(s.content())
	return s
}

// nextHeading is the first heading after the cursor, or the last one when there
// is none -- so TAB at the end stops rather than wrapping round to the top.
// Wrapping in a document reads as having lost your place.
func (s textSurface) nextHeading() int {
	for _, at := range s.headings {
		if at > s.cursor {
			return at
		}
	}
	return s.cursor
}

func (s textSurface) prevHeading() int {
	found := s.cursor
	for _, at := range s.headings {
		if at < s.cursor {
			found = at
		}
	}
	return found
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
	// The tree's fold keys, because on a page of sections they mean the same
	// thing the tree means by them: move by STRUCTURE rather than by line. They
	// are unbound on a text surface otherwise, and a page with sections and no
	// way to step between them is a page you read by scrolling past what you
	// wanted.
	if len(s.headings) > 0 {
		switch keys.Lookup(keys.Tree, msg) {
		case keys.FoldToggle:
			s.cursor = s.nextHeading()
			return s.follow(), true
		case keys.FoldCycleAll:
			s.cursor = s.prevHeading()
			return s.follow(), true
		}
	}
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
