package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"home-management-system/internal/domain"
	"home-management-system/internal/tui/keys"
	"home-management-system/internal/tui/style"
)

// The top line and the bottom two: where you are, what you are on, and what
// you can do to it. A breadcrumb answers "where am I" at any depth, which the
// tab bar it replaced could not answer at all.

const pathSeparator = " > "

func (m Model) header() string {
	if m.view != viewShell {
		return style.Strong.Render(spec(m.view).name) + "  " +
			style.Dim.Render("esc goes back") + "\n" + m.rule()
	}

	// The lens is the mode, so it takes the accent: one colour, one meaning,
	// and it is the only thing on this line that is about the interface
	// rather than about the house.
	left := style.Focus.Render(m.lens.spec().name) + "  " + m.breadcrumb()
	right := style.Dim.Render(m.headerCount())

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		// The path wins: the count is on the facts line anyway, and a wrapped
		// header shifts every row below it.
		return lipgloss.NewStyle().MaxWidth(max(10, m.width)).Render(left) + "\n" + m.rule()
	}
	return left + strings.Repeat(" ", gap) + right + "\n" + m.rule()
}

// breadcrumb is on screen at every width, including the ones where the rail
// is hidden, so where-am-I never depends on a pane that may not be there.
// It sheds ancestors from the front: the leaf is the half that locates you.
func (m Model) breadcrumb() string {
	sh, ok := m.shell()
	if !ok {
		return ""
	}
	node, ok := sh.railNode()
	if !ok {
		return m.lens.spec().root
	}
	path := sh.pathTo(node.Key())
	if len(path) == 0 {
		return style.Dim.Render(m.lens.spec().root)
	}
	for len(path) > 1 && lipgloss.Width(strings.Join(path, pathSeparator)) > m.width/2 {
		path = path[1:]
		path[0] = "..."
	}
	// The leaf is where you are; the rest is how you got here. Drawing them
	// at one weight makes a five-deep path read as one long string.
	leaf := path[len(path)-1]
	above := path[:len(path)-1]
	if len(above) == 0 {
		return style.Strong.Render(leaf)
	}
	return style.Dim.Render(strings.Join(above, pathSeparator)+pathSeparator) +
		style.Strong.Render(leaf)
}

func (m Model) headerCount() string {
	sh, ok := m.shell()
	if !ok {
		return ""
	}
	node, ok := sh.railNode()
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d %s", node.Count, plural(m.lens.spec().unit, node.Count))
}

func plural(word string, n int64) string {
	if n == 1 {
		return strings.TrimSuffix(word, "s")
	}
	return word
}

// ---------------------------------------------------------------------------
// The inspector
// ---------------------------------------------------------------------------

// inspect describes what the cursor is on. It took the facts line's place
// because a count describes the list, which you can see, and this describes
// the row, which mostly you cannot. Quantities are given both ways, per
// domain-model.md 4.1.
func (m Model) inspect() string {
	sel, ok := m.current.Current()
	if !ok {
		return ""
	}

	var parts []string
	switch sel.Kind {
	case kindHolding:
		parts = m.inspectHolding(sel)
	case kindItem:
		parts = m.inspectItem(sel)
	case kindLocation, kindCategory:
		parts = m.inspectNode()
	}
	if len(parts) == 0 {
		return ""
	}
	return joinQuietly(m.width, parts)
}

// quiet dims a part that is not already saying something for itself.
func quiet(s string) string {
	if strings.Contains(s, "\x1b") {
		return s
	}
	return style.Dim.Render(s)
}

func (m Model) inspectHolding(sel selection) []string {
	row, ok := m.holding(sel.Key)
	if !ok {
		return nil
	}
	parts := []string{style.Strong.Render(row.Item)}

	if item, ok := m.item(row.ItemID); ok {
		parts = append(parts, item.Category)
		// The house total only where there is more of it elsewhere.
		if item.OnHand != "" && item.OnHand != row.State {
			parts = append(parts, fmt.Sprintf("%s here, %s in the house", row.State, item.OnHand))
		} else {
			parts = append(parts, row.State)
		}
		// The measure only where it adds to the quantity: the bare unit is
		// one fact twice, a package size is what makes both framings
		// computable.
		if strings.Contains(item.Measure, "per package") || item.Measure == "one of a kind" {
			parts = append(parts, item.Measure)
		}
	} else {
		parts = append(parts, row.State)
	}

	if row.LocationPath != "" {
		parts = append(parts, row.LocationPath)
	}
	if row.Note != "" {
		parts = append(parts, style.Warn.Render(row.Note))
	}
	return parts
}

func (m Model) inspectItem(sel selection) []string {
	item, ok := m.item(domain.ItemID(sel.ID))
	if !ok {
		return nil
	}
	// The measure reads as a column heading on its own -- "Ancho Chile -
	// Spices - g" -- so in prose it gets the verb that makes it a sentence.
	measure := item.Measure
	if measure != "" && measure != "one of a kind" {
		measure = "measured in " + measure
	}
	parts := []string{style.Strong.Render(item.Name), item.Category, measure}
	if item.OnHand != "" {
		parts = append(parts, item.OnHand+" on hand")
	}
	return parts
}

func (m Model) inspectNode() []string {
	sh, ok := m.shell()
	if !ok {
		return nil
	}
	node, ok := sh.railNode()
	if !ok {
		return nil
	}
	spec := m.lens.spec()
	parts := []string{
		style.Strong.Render(strings.Join(sh.pathTo(node.Key()), pathSeparator)),
		fmt.Sprintf("%d %s below here", node.Count, plural(spec.unit, node.Count)),
	}
	if n := sh.childCount(node.Key()); n > 0 {
		word := spec.kids
		if n == 1 {
			word = spec.node
		}
		parts = append(parts, fmt.Sprintf("%d %s inside", n, word))
	}
	return parts
}

// ---------------------------------------------------------------------------
// The verb bar
// ---------------------------------------------------------------------------

// verbs is what the thing under the cursor can take, replacing a fixed footer
// that said the same on every screen. A key is offered only where pressing it
// does something: the line is permanent, so it has to be believable.
func (m Model) verbs() string {
	if m.view != viewShell {
		return keys.Hint(keys.Browse,
			[]keys.Action{keys.Search},
			[]keys.Action{keys.Jump},
			[]keys.Action{keys.CommandLine})
	}

	var lead string
	var groups [][]keys.Action
	switch {
	case m.onRail():
		lead = keys.Hint(keys.Tree, []keys.Action{keys.FoldToggle}) + " - "
		groups = [][]keys.Action{{keys.Create}, {keys.EditInPlace}, {keys.MoveTo}}
	case m.onHoldings():
		groups = [][]keys.Action{{keys.Consume}, {keys.Count}, {keys.MoveTo}, {keys.ToggleCustody}}
	default:
		groups = [][]keys.Action{{keys.Create}, {keys.EditInPlace}}
	}
	groups = append(groups, []keys.Action{keys.LensFlip}, []keys.Action{keys.Jump})
	return lead + keys.Hint(keys.Browse, groups...)
}
