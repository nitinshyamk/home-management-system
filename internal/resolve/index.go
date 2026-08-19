// Package resolve turns the names a person or an agent writes into the
// identifiers the domain uses.
//
// One flat index serves four consumers: the omnibox jump, inline autocomplete,
// the `:` command line, and the bulk importer. That is deliberate. If bulk
// import matched names differently from the UI, a CSV row and the equivalent
// typed command would resolve to different things, and the claim that they are
// one contract would be false.
package resolve

import (
	"context"
	"fmt"
	"strings"

	"home-management-system/internal/domain"
	"home-management-system/internal/query"
)

// Kind is what a candidate refers to.
type Kind string

const (
	KindCategory Kind = "Category"
	KindLocation Kind = "Location"
	KindItem     Kind = "Item"
	KindHolding  Kind = "Holding"
)

// PathSeparator joins the segments of a hierarchical label. Spaces around it
// matter: the fuzzy matcher treats a space as a word separator and rewards a
// character that follows one, so "kitshelf1" scores well against
// "Kitchen > Left Pantry > Shelf 1".
const PathSeparator = " > "

// Candidate is one thing a name might refer to.
type Candidate struct {
	Kind Kind
	ID   int64
	// Path is the full hierarchical label. For an Item it is the category path
	// plus the name; for a Holding, the item name plus where it is kept.
	Path string
	// Leaf is the last segment -- the name someone would say out loud.
	Leaf string
	// Archived candidates stay in the index so search can find them, and are
	// excluded from resolution so nothing is filed into a place that is gone.
	Archived bool
}

func (c Candidate) String() string { return fmt.Sprintf("%s %d (%s)", c.Kind, c.ID, c.Path) }

// Index is a flat searchable list. It is rebuilt rather than mutated: the data
// it indexes changes only through operations, and an operation already knows to
// invalidate it.
type Index struct {
	candidates []Candidate
	// paths and leaves parallel candidates, lowercased once so matching does
	// not re-fold the same strings on every keystroke.
	paths  []string
	leaves []string
}

// NewIndex builds an index over candidates given directly, for tests and for
// callers that assemble their own.
func NewIndex(candidates []Candidate) *Index {
	ix := &Index{
		candidates: candidates,
		paths:      make([]string, len(candidates)),
		leaves:     make([]string, len(candidates)),
	}
	for i, c := range candidates {
		ix.paths[i] = fold(c.Path)
		ix.leaves[i] = fold(c.Leaf)
	}
	return ix
}

// All returns every candidate, in index order.
func (ix *Index) All() []Candidate { return ix.candidates }

// Len reports how many things are indexed.
func (ix *Index) Len() int { return len(ix.candidates) }

// Build reads the whole vocabulary through the query path.
//
// Everything, in one pass, because the alternative is a query per keystroke.
// The vocabulary of a household is small enough that this is the cheap option
// and stays cheap.
func Build(ctx context.Context, r *query.Reader) (*Index, error) {
	var out []Candidate

	categories, err := r.CategoryForest(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve: read categories: %w", err)
	}
	categoryPath := map[domain.CategoryID]string{}
	var stack []string
	for _, n := range categories {
		stack = append(stack[:min(n.Depth, len(stack))], n.Category.Name)
		path := strings.Join(stack, PathSeparator)
		categoryPath[n.Category.ID] = path
		out = append(out, Candidate{
			Kind: KindCategory, ID: int64(n.Category.ID), Path: path,
			Leaf: n.Category.Name, Archived: n.Category.IsArchived(),
		})
	}

	locations, err := r.LocationForest(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve: read locations: %w", err)
	}
	stack = stack[:0]
	for _, n := range locations {
		stack = append(stack[:min(n.Depth, len(stack))], n.Location.Name)
		out = append(out, Candidate{
			Kind: KindLocation, ID: int64(n.Location.ID), Path: strings.Join(stack, PathSeparator),
			Leaf: n.Location.Name, Archived: n.Location.IsArchived(),
		})
	}

	items, err := r.Items(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve: read items: %w", err)
	}
	for _, it := range items {
		base := it.Base()
		// The category prefix is what disambiguates two items sharing a name,
		// which the schema deliberately permits (3.10).
		path := base.Name
		if prefix, ok := categoryPath[base.Category]; ok {
			path = prefix + PathSeparator + base.Name
		}
		out = append(out, Candidate{
			Kind: KindItem, ID: int64(base.ID), Path: path,
			Leaf: base.Name, Archived: base.ArchivedAt != nil,
		})
	}

	holdings, err := r.Holdings(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve: read holdings: %w", err)
	}
	for _, h := range holdings {
		base := h.Holding.Base()
		out = append(out, Candidate{
			Kind: KindHolding, ID: int64(base.ID),
			Path: h.ItemName + PathSeparator + h.LocationName,
			Leaf: h.ItemName, Archived: base.RetiredAt != nil,
		})
	}

	return NewIndex(out), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
