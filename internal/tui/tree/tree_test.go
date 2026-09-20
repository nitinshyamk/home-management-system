package tree_test

import (
	"strings"
	"testing"

	"home-management-system/internal/tui/keys"
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
//
// The counts deliberately differ in WIDTH -- 1, 4, 137 -- because single-digit
// counts land in the same place whether the column is left- or right-aligned,
// so a fixture of them cannot tell the two apart. A tray of small parts is also
// exactly where a household ends up with a three-digit rollup.
func house() []tree.Node {
	return []tree.Node{
		{ID: 1, Name: "Attic", Depth: 0, Count: 1},
		{ID: 2, Name: "Garage", Depth: 0, Count: 137},
		{ID: 3, Name: "Metal Shelving Unit", Depth: 1, Count: 128},
		{ID: 4, Name: "Bay 3", Depth: 2, Count: 128},
		{ID: 5, Name: "Blue Crate", Depth: 3, Count: 128},
		{ID: 6, Name: "Small Parts Tray", Depth: 4, Count: 128},
		{ID: 7, Name: "Kitchen", Depth: 0, Count: 4},
		{ID: 8, Name: "Spice Cabinet", Depth: 1, Count: 2},
	}
}

func newTree() tree.Model {
	return tree.New("holdings").SetNodes(house()).SetSize(70, 14)
}

// press names keys the way the keymap names them, so a test cannot press a key
// the interface does not bind.
func press(m tree.Model, names ...string) tree.Model {
	for _, name := range names {
		msg, ok := keys.Named(name)
		if !ok {
			panic(name + " is not a key")
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
	m := press(newTree(), "ctrl+n", "tab") // cursor on Garage, fold it

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
	m := press(newTree(), "ctrl+n", "tab")
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
	m := press(newTree(), "ctrl+n") // Garage
	before, _ := m.Current()

	m = press(m, "tab")
	if after, _ := m.Current(); after.ID != before.ID {
		t.Errorf("after folding the cursor is on %q, want %q", after.Name, before.Name)
	}
	m = press(m, "tab")
	if after, _ := m.Current(); after.ID != before.ID {
		t.Errorf("after unfolding the cursor is on %q, want %q", after.Name, before.Name)
	}
}

func TestCollapseAllAndExpandAll(t *testing.T) {
	m := press(newTree(), "shift+tab")
	if got := len(shown(m)); got != 3 {
		t.Errorf("collapsing everything left %d rows, want the 3 roots: %v", got, shown(m))
	}
	m = press(m, "shift+tab")
	if got := len(shown(m)); got != len(house()) {
		t.Errorf("expanding everything left %d rows, want all %d", got, len(house()))
	}
}

// C-b tidies away a subtree far more often than it travels, so it collapses
// first and only then moves up.
func TestAscendCollapsesBeforeItTravels(t *testing.T) {
	m := press(newTree(), "ctrl+n") // Garage, expanded

	m = press(m, "ctrl+b")
	if current, _ := m.Current(); current.Name != "Garage" {
		t.Errorf("h moved to %q; it should have collapsed Garage first", current.Name)
	}
	if contains(m, "Bay 3") {
		t.Error("h did not collapse the subtree")
	}
	// Now there is nothing to close, so it travels -- and Garage is a root, so
	// there is nowhere to go.
	m = press(m, "ctrl+b")
	if current, _ := m.Current(); current.Name != "Garage" {
		t.Errorf("h from a collapsed root moved to %q", current.Name)
	}
	// From a child, it reaches the parent.
	m = press(m, "shift+tab", "ctrl+n", "ctrl+n") // Garage > Metal Shelving Unit > Bay 3
	deep, _ := m.Current()
	if deep.Name != "Bay 3" {
		t.Fatalf("expected to be on Bay 3, on %q", deep.Name)
	}
	m = press(m, "ctrl+b", "ctrl+b") // collapse Bay 3, then ascend
	if current, _ := m.Current(); current.Name != "Metal Shelving Unit" {
		t.Errorf("h from a collapsed Bay 3 moved to %q, want its parent", current.Name)
	}
}

func TestDescendOpensThenSteps(t *testing.T) {
	m := press(newTree(), "ctrl+n", "tab") // Garage, folded
	m = press(m, "ctrl+f")
	if current, _ := m.Current(); current.Name != "Garage" {
		t.Errorf("l moved to %q; it should have opened Garage first", current.Name)
	}
	if !contains(m, "Metal Shelving Unit") {
		t.Error("l did not open the subtree")
	}
	m = press(m, "ctrl+f")
	if current, _ := m.Current(); current.Name != "Metal Shelving Unit" {
		t.Errorf("l from an open node moved to %q, want its first child", current.Name)
	}
	// A leaf has nowhere to go.
	m = press(m, "shift+tab", "alt+<") // Attic
	if current, _ := press(m, "ctrl+f").Current(); current.Name != "Attic" {
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
	m := press(newTree(), "ctrl+n", "ctrl+space")
	if got := m.SelectionCount(); got != 1 {
		t.Errorf("space selected %d rows", got)
	}
	if got := len(m.Selected()); got != 1 {
		t.Errorf("Selected() = %v", m.Selected())
	}
	m = press(m, "alt+>")
	if current, _ := m.Current(); current.Name != "Spice Cabinet" {
		t.Errorf("M-> landed on %q, want the last row", current.Name)
	}
	// C-l centres. It used to be zz, which the fold gestures nearly ate.
	m = press(m, "ctrl+l")
	if current, _ := m.Current(); current.Name != "Spice Cabinet" {
		t.Errorf("recentring moved the cursor to %q", current.Name)
	}
}

// The counts form a straight column whatever the depth, which is the whole
// claim of the layout that was chosen: comparing magnitudes down a branch is a
// single vertical scan.
func TestTheRollupIsAStraightColumn(t *testing.T) {
	m := newTree()

	// In COLUMNS, not bytes. The fold marker is one column and three bytes, so
	// a byte index would report two rows as misaligned that line up perfectly
	// on screen -- which is exactly the mistake the widget itself must not make.
	column := func(name string) int {
		for _, line := range strings.Split(strip(m.View()), "\n") {
			if !strings.Contains(line, name) {
				continue
			}
			runes := []rune(strings.TrimRight(line, " "))
			for i := len(runes) - 1; i >= 0; i-- {
				if runes[i] >= '0' && runes[i] <= '9' {
					return i
				}
			}
		}
		return -1
	}

	// Attic first, because its count is ONE digit against the others' three:
	// a left-aligned column would put its last digit two places short, and a
	// right-aligned one lines them all up.
	shallow := column("Attic")
	if shallow < 0 {
		t.Fatal("no count found on the Attic row")
	}
	for _, deeper := range []string{"Garage", "Metal Shelving Unit", "Bay 3", "Blue Crate", "Small Parts Tray"} {
		if got := column(deeper); got != shallow {
			t.Errorf("%q ends its count at column %d and Attic at %d; the rollup is not a column",
				deeper, got, shallow)
		}
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
// only removes rows below it. Folding everything at once moves rows above it, and neither put the
// cursor back.
//
// Collapsing from five levels down landed on whatever row inherited that index, and expanding
// from a root landed on its own grandchild.
func TestCollapseAllKeepsYouWhereYouWere(t *testing.T) {
	m := press(newTree(), "ctrl+n", "ctrl+n", "ctrl+n", "ctrl+n", "ctrl+n")
	if current, _ := m.Current(); current.Name != "Small Parts Tray" {
		t.Fatalf("expected to be on Small Parts Tray, on %q", current.Name)
	}

	// The node itself is folded away, so the nearest ancestor still on screen
	// is the useful answer -- the root of the branch being read, not the row
	// that inherited the index.
	m = press(m, "shift+tab")
	if current, _ := m.Current(); current.Name != "Garage" {
		t.Errorf("collapsing from Small Parts Tray landed on %q, want its visible ancestor Garage",
			current.Name)
	}

	// Expanding again, the cursor's own node is visible, so it keeps it.
	m = press(m, "shift+tab")
	if current, _ := m.Current(); current.Name != "Garage" {
		t.Errorf("expanding moved the cursor from Garage to %q", current.Name)
	}
}

// Expanding needs its own case, because the obvious one passes by luck: expanding from
// the SECOND root leaves that root's index unchanged. What moves the cursor is
// expanding a subtree ABOVE it -- Kitchen is row 2 collapsed and row 6
// expanded, so an index-following cursor lands four rows short.
func TestExpandAllKeepsYouWhereYouWere(t *testing.T) {
	m := press(newTree(), "shift+tab", "ctrl+n", "ctrl+n")
	if current, _ := m.Current(); current.Name != "Kitchen" {
		t.Fatalf("expected to be on Kitchen, on %q", current.Name)
	}
	m = press(m, "shift+tab")
	if current, _ := m.Current(); current.Name != "Kitchen" {
		t.Errorf("expanding moved the cursor from Kitchen to %q; the rows above it expanded under it",
			current.Name)
	}
}

// ---------------------------------------------------------------------------
// Targeting: the tree while something is being carried
// ---------------------------------------------------------------------------

// The same house with its contents spliced in, which is what the tree shows
// once the contents toggle is on.
//
// A holding sits immediately under the place that holds it and BEFORE that
// place's own sub-places, which is how the query path returns them -- so the
// first child of Garage is a jar rather than the shelving unit. That ordering
// is the whole reason descend needs an opinion here.
//
//	Attic
//	Garage
//	  Ancho Chile                           (holding)
//	  Metal Shelving Unit
//	    Bay 3
//	Kitchen
//	  Basmati Rice                          (holding)
//	  Cumin                                 (holding)
func stockedHouse() []tree.Node {
	return []tree.Node{
		{ID: 1, Name: "Attic", Depth: 0, Count: 0},
		{ID: 2, Name: "Garage", Depth: 0, Count: 1},
		{ID: 10, Name: "Ancho Chile", Depth: 1, Kind: "Holding", Measure: "100 g"},
		{ID: 3, Name: "Metal Shelving Unit", Depth: 1, Count: 0},
		{ID: 4, Name: "Bay 3", Depth: 2, Count: 0},
		{ID: 7, Name: "Kitchen", Depth: 0, Count: 2},
		{ID: 11, Name: "Basmati Rice", Depth: 1, Kind: "Holding", Measure: "500 g"},
		{ID: 12, Name: "Cumin", Depth: 1, Kind: "Holding", Measure: "90 g"},
	}
}

func stocked() tree.Model {
	return tree.New("holdings").SetNodes(stockedHouse()).SetSize(70, 14)
}

// Nothing in hand, nothing ruled out: the contents are rows like any other,
// and this is the contrast the rest of the section is measured against.
func TestWithNothingInHandTheCursorReachesTheContents(t *testing.T) {
	m := press(stocked(), "ctrl+n", "ctrl+n")
	if current, _ := m.Current(); current.Name != "Ancho Chile" {
		t.Errorf("two C-n landed on %q, want the Ancho Chile inside the Garage", current.Name)
	}
}

// With something in hand the cursor goes to the next PLACE instead.
//
// The things inside a place are not places, so walking the cursor through them
// on the way to a destination is walking it through rows the put is going to
// refuse. Pointing at where a thing goes should cost one keystroke per
// candidate, not one per row.
func TestWhileCarryingTheCursorSkipsToTheNextPlace(t *testing.T) {
	m := press(stocked().Targeting(true), "ctrl+n", "ctrl+n")
	if current, _ := m.Current(); current.Name != "Metal Shelving Unit" {
		t.Errorf("two C-n landed on %q, want Metal Shelving Unit -- the jar in between is not a place",
			current.Name)
	}
	// And a whole run of them at the end of the tree is skipped the same way.
	m = press(m, "alt+>")
	if current, _ := m.Current(); current.Name != "Kitchen" {
		t.Errorf("M-> landed on %q, want Kitchen -- the last row that is a place", current.Name)
	}
}

// Descending steps past them too.
//
// C-f aims at a node's first child, and a place's first child is one of the
// things inside it -- so without an opinion here the key aimed at a row the
// cursor is not allowed to rest on, and did nothing at all.
func TestWhileCarryingDescendingFindsTheFirstChildPlace(t *testing.T) {
	m := press(stocked().Targeting(true), "ctrl+n") // Garage
	m = press(m, "ctrl+f")
	if current, _ := m.Current(); current.Name != "Metal Shelving Unit" {
		t.Errorf("C-f into the Garage landed on %q, want Metal Shelving Unit", current.Name)
	}
}

// Picking a thing up moves the cursor off it, because the row it was picked up
// on is the first row ruled out -- and a gesture that begins by pointing at the
// one place the thing cannot go has begun by contradicting itself.
//
// Onto the container it is IN: that is the nearest row still on offer, it is
// where the eye already is, and for the common case -- this jar is on the wrong
// shelf -- it is one row from every sibling shelf.
func TestPickingSomethingUpMovesTheCursorToWhatHoldsIt(t *testing.T) {
	m := press(stocked(), "ctrl+n", "ctrl+n")
	if current, _ := m.Current(); current.Name != "Ancho Chile" {
		t.Fatalf("expected to be on the Ancho Chile, on %q", current.Name)
	}
	m = m.Targeting(true)
	if current, _ := m.Current(); current.Name != "Garage" {
		t.Errorf("the cursor stayed on %q when it was ruled out, want Garage", current.Name)
	}
}

// And putting the thing down gives the tree back.
func TestPuttingItDownLetsTheCursorReachTheContentsAgain(t *testing.T) {
	m := stocked().Targeting(true).Targeting(false)
	m = press(m, "ctrl+n", "ctrl+n")
	if current, _ := m.Current(); current.Name != "Ancho Chile" {
		t.Errorf("two C-n landed on %q after the carry ended, want Ancho Chile", current.Name)
	}
}

// A filtered tree rules them out too. The filter and the carry narrow different
// things -- which rows are shown, and which shown rows are candidates -- and a
// tree that forgot the second while doing the first would offer a jar as a
// destination to somebody who had just searched for it.
func TestAFilteredTreeStillRulesOutTheContents(t *testing.T) {
	m := stocked().Targeting(true).SetFilter("chile")
	if current, _ := m.Current(); current.Name != "Garage" {
		t.Errorf("the cursor is on %q in the filtered tree, want Garage -- the only row left that is a place",
			current.Name)
	}
	if !contains(m, "Ancho Chile") {
		t.Error("the filter hid the row it matched; it should be shown and merely not offered")
	}
}
