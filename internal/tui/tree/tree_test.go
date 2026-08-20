package tree_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"home-management-system/internal/tui/tree"
)

// The house, five deep, with a leaf at the top so "folded" and "empty" have to
// be told apart.
//
//	Attic                                   (leaf)
//	Garage
//	  Metal Shelving Unit
//	    Bay 3
//	      Blue Crate
//	        Small Parts Tray                (leaf)
//	Kitchen
//	  Spice Cabinet
func house() []tree.Node {
	return []tree.Node{
		{ID: 1, Name: "Attic", Depth: 0, Count: 1},
		{ID: 2, Name: "Garage", Depth: 0, Count: 3},
		{ID: 3, Name: "Metal Shelving Unit", Depth: 1, Count: 2},
		{ID: 4, Name: "Bay 3", Depth: 2, Count: 2},
		{ID: 5, Name: "Blue Crate", Depth: 3, Count: 2},
		{ID: 6, Name: "Small Parts Tray", Depth: 4, Count: 2},
		{ID: 7, Name: "Kitchen", Depth: 0, Count: 4},
		{ID: 8, Name: "Spice Cabinet", Depth: 1, Count: 2},
	}
}

func newTree() tree.Model {
	return tree.New("holdings", tree.RollupRight).SetNodes(house()).SetSize(70, 14)
}

func press(m tree.Model, keys ...string) tree.Model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEscape}
		case " ":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m, _ = m.Update(msg)
	}
	return m
}

func shown(m tree.Model) []string {
	var out []string
	for _, line := range strings.Split(strip(m.View()), "\n")[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func contains(m tree.Model, name string) bool {
	for _, line := range shown(m) {
		if strings.Contains(line, name) {
			return true
		}
	}
	return false
}

func TestFoldingHidesTheSubtreeAndNothingElse(t *testing.T) {
	m := press(newTree(), "j", "z", "a") // cursor on Garage, fold it

	for _, gone := range []string{"Metal Shelving Unit", "Bay 3", "Blue Crate", "Small Parts Tray"} {
		if contains(m, gone) {
			t.Errorf("%q is still shown inside a folded Garage", gone)
		}
	}
	for _, kept := range []string{"Attic", "Garage", "Kitchen", "Spice Cabinet"} {
		if !contains(m, kept) {
			t.Errorf("%q disappeared, and it is not inside the folded subtree", kept)
		}
	}
}

// A folded node with four descendants and a leaf with none must not look alike.
// That is the one tree failure that silently hides data.
func TestFoldedIsDistinguishableFromALeaf(t *testing.T) {
	m := press(newTree(), "j", "z", "a")
	lines := shown(m)

	var folded, leaf string
	for _, line := range lines {
		if strings.Contains(line, "Garage") {
			folded = line
		}
		if strings.Contains(line, "Attic") {
			leaf = line
		}
	}
	if folded == "" || leaf == "" {
		t.Fatalf("could not find both rows in %v", lines)
	}
	if !strings.Contains(folded, "▸") {
		t.Errorf("a folded node carries no marker: %q", folded)
	}
	if strings.Contains(leaf, "▸") || strings.Contains(leaf, "▾") {
		t.Errorf("a leaf carries a fold marker, so it looks foldable: %q", leaf)
	}
}

// Folding removes rows beneath the cursor, so a cursor that followed its INDEX
// would slide onto whatever moved up under it.
func TestFoldingKeepsTheCursorOnItsNode(t *testing.T) {
	m := press(newTree(), "j") // Garage
	before, _ := m.Current()

	m = press(m, "z", "a")
	if after, _ := m.Current(); after.ID != before.ID {
		t.Errorf("after folding the cursor is on %q, want %q", after.Name, before.Name)
	}
	m = press(m, "z", "a")
	if after, _ := m.Current(); after.ID != before.ID {
		t.Errorf("after unfolding the cursor is on %q, want %q", after.Name, before.Name)
	}
}

func TestCollapseAllAndExpandAll(t *testing.T) {
	m := press(newTree(), "z", "M")
	if got := len(shown(m)); got != 3 {
		t.Errorf("zM left %d rows, want the 3 roots: %v", got, shown(m))
	}
	m = press(m, "z", "R")
	if got := len(shown(m)); got != len(house()) {
		t.Errorf("zR left %d rows, want all %d", got, len(house()))
	}
}

// h tidies away a subtree far more often than it travels, so it collapses
// first and only then moves up.
func TestAscendCollapsesBeforeItTravels(t *testing.T) {
	m := press(newTree(), "j") // Garage, expanded

	m = press(m, "h")
	if current, _ := m.Current(); current.Name != "Garage" {
		t.Errorf("h moved to %q; it should have collapsed Garage first", current.Name)
	}
	if contains(m, "Bay 3") {
		t.Error("h did not collapse the subtree")
	}
	// Now there is nothing to close, so it travels -- and Garage is a root, so
	// there is nowhere to go.
	m = press(m, "h")
	if current, _ := m.Current(); current.Name != "Garage" {
		t.Errorf("h from a collapsed root moved to %q", current.Name)
	}
	// From a child, it reaches the parent.
	m = press(m, "z", "R", "j", "j") // Garage > Metal Shelving Unit > Bay 3
	deep, _ := m.Current()
	if deep.Name != "Bay 3" {
		t.Fatalf("expected to be on Bay 3, on %q", deep.Name)
	}
	m = press(m, "h", "h") // collapse Bay 3, then ascend
	if current, _ := m.Current(); current.Name != "Metal Shelving Unit" {
		t.Errorf("h from a collapsed Bay 3 moved to %q, want its parent", current.Name)
	}
}

func TestDescendOpensThenSteps(t *testing.T) {
	m := press(newTree(), "j", "z", "a") // Garage, folded
	m = press(m, "l")
	if current, _ := m.Current(); current.Name != "Garage" {
		t.Errorf("l moved to %q; it should have opened Garage first", current.Name)
	}
	if !contains(m, "Metal Shelving Unit") {
		t.Error("l did not open the subtree")
	}
	m = press(m, "l")
	if current, _ := m.Current(); current.Name != "Metal Shelving Unit" {
		t.Errorf("l from an open node moved to %q, want its first child", current.Name)
	}
	// A leaf has nowhere to go.
	m = press(m, "z", "M", "g", "g") // Attic
	if current, _ := press(m, "l").Current(); current.Name != "Attic" {
		t.Errorf("l from a leaf moved somewhere")
	}
}

// Hierarchy order IS the order. Sorting a tree by name would destroy the
// containment the view exists to show.
func TestATreeIsNotSortable(t *testing.T) {
	m := newTree()
	before := shown(m)
	m = press(m, "s")
	if got := shown(m); !equal(got, before) {
		t.Errorf("s reordered the tree:\n before %v\n after  %v", before, got)
	}
	if strings.Contains(strip(m.View()), "^") || strings.Contains(strip(m.View()), " v") {
		t.Errorf("a tree claims a sort order:\n%s", strip(m.View()))
	}
}

// The tree is the table underneath, so the gestures the table owns still work.
func TestTheTableUnderneathStillWorks(t *testing.T) {
	m := press(newTree(), "j", " ")
	if got := m.SelectionCount(); got != 1 {
		t.Errorf("space selected %d rows", got)
	}
	if got := len(m.Selected()); got != 1 {
		t.Errorf("Selected() = %v", m.Selected())
	}
	m = press(m, "G")
	if current, _ := m.Current(); current.Name != "Spice Cabinet" {
		t.Errorf("G landed on %q, want the last row", current.Name)
	}
	// zz centres rather than being eaten by the fold gestures.
	m = press(m, "z", "z")
	if current, _ := m.Current(); current.Name != "Spice Cabinet" {
		t.Errorf("zz moved the cursor to %q", current.Name)
	}
}

// The rollup is the decision 10b's review is about, so both candidates are
// pinned rather than only the one currently chosen.
func TestBothRollupLayoutsPlaceTheCountDifferently(t *testing.T) {
	right := tree.New("holdings", tree.RollupRight).SetNodes(house()).SetSize(70, 14)
	staggered := tree.New("holdings", tree.RollupStaggered).SetNodes(house()).SetSize(70, 14)

	// In COLUMNS, not bytes. The fold marker is one column and three bytes, so
	// a byte index would report two rows as misaligned that line up perfectly
	// on screen -- which is exactly the mistake the widget itself must not make.
	column := func(m tree.Model, name string) int {
		for _, line := range strings.Split(strip(m.View()), "\n") {
			if !strings.Contains(line, name) {
				continue
			}
			runes := []rune(strings.TrimRight(line, " "))
			for i := len(runes) - 1; i >= 0; i-- {
				if runes[i] == '2' {
					return i
				}
			}
		}
		return -1
	}

	// Right-aligned: the counts form a straight column whatever the depth.
	shallow, deep := column(right, "Metal Shelving Unit"), column(right, "Small Parts Tray")
	if shallow != deep {
		t.Errorf("right-aligned counts are not in a column: %d vs %d", shallow, deep)
	}
	// Staggered: the deeper node's count sits further LEFT.
	shallow, deep = column(staggered, "Metal Shelving Unit"), column(staggered, "Small Parts Tray")
	if deep >= shallow {
		t.Errorf("staggered counts do not step left with depth: %d then %d", shallow, deep)
	}
}

// TestAMultiByteMarkerDoesNotShiftTheColumns is the hazard the fold markers
// introduce: ▾ is one column and three bytes, so any width arithmetic done in
// bytes puts every row carrying a marker three columns out.
//
// It shows up as a ragged right edge that looks like a rounding error and is
// not one.
func TestAMultiByteMarkerDoesNotShiftTheColumns(t *testing.T) {
	m := newTree()
	widths := map[int]bool{}
	for _, line := range strings.Split(strip(m.View()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		widths[len([]rune(line))] = true
	}
	if len(widths) != 1 {
		t.Errorf("rows render at %d different widths %v; a marker is being measured in bytes",
			len(widths), widths)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func strip(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestCollapseAllKeepsYouWhereYouWere is the defect the break-the-guard pass
// found: folding at the CURSOR never changes the cursor's own index, because it
// only removes rows below it. zM and zR move rows above it, and neither put the
// cursor back.
//
// zM from five levels down landed on whatever row inherited that index, and zR
// from a root landed on its own grandchild.
func TestCollapseAllKeepsYouWhereYouWere(t *testing.T) {
	m := press(newTree(), "j", "j", "j", "j", "j")
	if current, _ := m.Current(); current.Name != "Small Parts Tray" {
		t.Fatalf("expected to be on Small Parts Tray, on %q", current.Name)
	}

	// The node itself is folded away, so the nearest ancestor still on screen
	// is the useful answer -- the root of the branch being read, not the row
	// that inherited the index.
	m = press(m, "z", "M")
	if current, _ := m.Current(); current.Name != "Garage" {
		t.Errorf("zM from Small Parts Tray landed on %q, want its visible ancestor Garage",
			current.Name)
	}

	// Expanding again, the cursor's own node is visible, so it keeps it.
	m = press(m, "z", "R")
	if current, _ := m.Current(); current.Name != "Garage" {
		t.Errorf("zR moved the cursor from Garage to %q", current.Name)
	}
}

// zR needs its own case, because the obvious one passes by luck: expanding from
// the SECOND root leaves that root's index unchanged. What moves the cursor is
// expanding a subtree ABOVE it -- Kitchen is row 2 collapsed and row 6
// expanded, so an index-following cursor lands four rows short.
func TestExpandAllKeepsYouWhereYouWere(t *testing.T) {
	m := press(newTree(), "z", "M", "j", "j")
	if current, _ := m.Current(); current.Name != "Kitchen" {
		t.Fatalf("expected to be on Kitchen, on %q", current.Name)
	}
	m = press(m, "z", "R")
	if current, _ := m.Current(); current.Name != "Kitchen" {
		t.Errorf("zR moved the cursor from Kitchen to %q; the rows above it expanded under it",
			current.Name)
	}
}
