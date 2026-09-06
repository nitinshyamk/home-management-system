package query

import "home-management-system/internal/domain"

// The shape of a hierarchy, read once rather than twice.
//
// Categories and Locations are the same tree over different types: a node with
// an identifier and an optional parent, read in name order, assembled into a
// path or a depth-first subtree. Both had their own copy of both assemblies --
// the upward walk with its reverse, and the recursive descent with its derived
// depth -- identical but for the type name and the noun in an error.
//
// docs/conceptual-schema.md 3.1 asked for this: "one abstraction implemented
// once, parameterized by entity". These are the read half of it.

// hierarchy says how to get the two things a tree walk needs out of a node.
// Everything else about a Category or a Location is irrelevant here, which is
// why this takes two functions rather than an interface the entities implement.
type hierarchy[ID comparable, T any] struct {
	id     func(T) ID
	parent func(T) *ID
}

// path orders an ancestor chain root-first.
//
// The rows arrive as a set, so the order is derived by walking UP from the
// node -- following each parent until one has none -- and reversing. A
// breadcrumb that did not start at the root would not be one.
func (h hierarchy[ID, T]) path(rows []T, from ID) []T {
	byID := make(map[ID]T, len(rows))
	for _, row := range rows {
		byID[h.id(row)] = row
	}

	var upward []T
	for cur, ok := byID[from], true; ok; {
		upward = append(upward, cur)
		parent := h.parent(cur)
		if parent == nil {
			break
		}
		cur, ok = byID[*parent]
	}

	out := make([]T, 0, len(upward))
	for i := len(upward) - 1; i >= 0; i-- {
		out = append(out, upward[i])
	}
	return out
}

// subtree visits the root and everything beneath it, depth first, handing each
// node its derived depth. It reports false when the root is not among the rows.
//
// Depth first, so the output reads as an indented tree. Names arrive sorted
// from SQL and nothing here reorders them: a map is used for lookup, never for
// iteration.
func (h hierarchy[ID, T]) subtree(rows []T, root ID, visit func(T, int)) bool {
	children := map[ID][]T{}
	var start *T
	for _, row := range rows {
		if h.id(row) == root {
			found := row
			start = &found
			continue
		}
		if parent := h.parent(row); parent != nil {
			children[*parent] = append(children[*parent], row)
		}
	}
	if start == nil {
		return false
	}

	var walk func(node T, depth int)
	walk = func(node T, depth int) {
		visit(node, depth)
		for _, child := range children[h.id(node)] {
			walk(child, depth+1)
		}
	}
	walk(*start, 0)
	return true
}

// The two hierarchies this package reads. Everything that differs between a
// Category tree and a Location tree is here, in four lines.
var (
	categoryTree = hierarchy[domain.CategoryID, domain.Category]{
		id:     func(c domain.Category) domain.CategoryID { return c.ID },
		parent: func(c domain.Category) *domain.CategoryID { return c.Parent },
	}
	locationTree = hierarchy[domain.LocationID, domain.Location]{
		id:     func(l domain.Location) domain.LocationID { return l.ID },
		parent: func(l domain.Location) *domain.LocationID { return l.Parent },
	}
)
