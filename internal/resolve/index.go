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
//
// An alias rather than a distinct type: an outcome's Kind is handed straight to
// an operation as the thing to act on, and converting between two identical
// enums at that boundary would be ceremony that could also be got wrong.
type Kind = domain.EntityKind

const (
	KindCategory = domain.EntityCategory
	KindLocation = domain.EntityLocation
	KindItem     = domain.EntityItem
	KindHolding  = domain.EntityHolding
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

// Label renders an identifier as the name it resolved from, which is what makes
// a review screen readable: a Command holds only identifiers, and the index is
// the honest place for the names to come back from.
//
// It returns "" for something not indexed, so the caller decides what to show
// rather than being handed a plausible-looking wrong answer. Archived
// candidates DO get a label -- a summary of what happened to something put away
// still has to say what it was.
func (ix *Index) Label(kind domain.EntityKind, id int64) string {
	for _, c := range ix.candidates {
		if c.Kind == kind && c.ID == id {
			return c.Path
		}
	}
	return ""
}

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
			Path: h.ItemName + PathSeparator + h.LocationName + distinguish(h.Holding),
			Leaf: h.ItemName, Archived: base.RetiredAt != nil,
		})
	}

	return NewIndex(out), nil
}

// distinguish appends what tells one Holding from its sibling in the same
// place, because "Basmati Rice > Left Pantry" is routinely TWO Holdings -- the
// sealed bags and the loose contents -- and a label that names both names
// neither.
//
// H8's key is exactly what makes two Holdings of one Item in one place
// distinct, so it is exactly what the label has to carry: the unit basis, and
// the expiry when there is one.
func distinguish(h domain.Holding) string {
	switch v := h.(type) {
	case domain.BulkHolding:
		s := " (sealed)"
		if v.UnitBasis == domain.BasisContent {
			s = " (loose)"
		}
		if v.ExpiresOn != nil {
			s += " expiring " + v.ExpiresOn.Format("2006-01-02")
		}
		return s
	case domain.UniqueHolding:
		// A Unique Holding is a specific object and two of them in one place
		// are two things, so its own label is the only thing that separates
		// them -- and it may not have one.
		if v.Label != "" {
			return " (" + v.Label + ")"
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
