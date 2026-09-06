package query_test

import (
	"context"
	"testing"

	"home-management-system/internal/domain"
	"home-management-system/internal/ledger"
	"home-management-system/internal/origin"
	"home-management-system/internal/query"
	"home-management-system/internal/testsupport"
)

// The two trees are read by code that is the same shape for both, so they are
// tested by one set of cases run against each. A behaviour that holds for
// Categories and not for Locations is exactly what these are here to catch.
//
// The shape built, for both:
//
//	Root
//	  Middle
//	    Leaf
//	  Sibling
//	Second
type forest struct {
	root, middle, leaf, sibling, second int64
}

func categories(t *testing.T, o *origin.Originator, ctx context.Context) forest {
	t.Helper()
	mk := func(name string, parent *int64) int64 {
		var p *domain.CategoryID
		if parent != nil {
			id := domain.CategoryID(*parent)
			p = &id
		}
		id, err := o.CreateCategory(ctx, origin.CreateCategoryInput{Name: name, Parent: p})
		if err != nil {
			t.Fatalf("create category %q: %v", name, err)
		}
		return int64(id)
	}
	f := forest{}
	f.root = mk("Root", nil)
	f.middle = mk("Middle", &f.root)
	f.leaf = mk("Leaf", &f.middle)
	f.sibling = mk("Sibling", &f.root)
	f.second = mk("Second", nil)
	return f
}

func locations(t *testing.T, l *ledger.Processor, ctx context.Context) forest {
	t.Helper()
	mk := func(name string, parent *int64) int64 {
		var p *domain.LocationID
		if parent != nil {
			id := domain.LocationID(*parent)
			p = &id
		}
		id, err := l.CreateLocation(ctx, name, p, "")
		if err != nil {
			t.Fatalf("create location %q: %v", name, err)
		}
		return int64(id)
	}
	f := forest{}
	f.root = mk("Root", nil)
	f.middle = mk("Middle", &f.root)
	f.leaf = mk("Leaf", &f.middle)
	f.sibling = mk("Sibling", &f.root)
	f.second = mk("Second", nil)
	return f
}

// tree is one entity's reads, named so a case can be written once.
type tree struct {
	kind    string
	build   func(*testing.T, *origin.Originator, *ledger.Processor, context.Context) forest
	path    func(*query.Reader, context.Context, int64) ([]named, error)
	subtree func(*query.Reader, context.Context, int64) ([]named, error)
	forest  func(*query.Reader, context.Context) ([]named, error)
}

// named is a node flattened to the two things every case asserts on.
type named struct {
	Name  string
	Depth int
}

func trees() []tree {
	return []tree{
		{
			kind: "category",
			build: func(t *testing.T, o *origin.Originator, _ *ledger.Processor, ctx context.Context) forest {
				return categories(t, o, ctx)
			},
			path: func(r *query.Reader, ctx context.Context, id int64) ([]named, error) {
				got, err := r.CategoryPath(ctx, domain.CategoryID(id))
				out := make([]named, 0, len(got))
				for _, c := range got {
					out = append(out, named{Name: c.Name})
				}
				return out, err
			},
			subtree: func(r *query.Reader, ctx context.Context, id int64) ([]named, error) {
				got, err := r.CategorySubtree(ctx, domain.CategoryID(id))
				out := make([]named, 0, len(got))
				for _, n := range got {
					out = append(out, named{n.Category.Name, n.Depth})
				}
				return out, err
			},
			forest: func(r *query.Reader, ctx context.Context) ([]named, error) {
				got, err := r.CategoryForest(ctx)
				out := make([]named, 0, len(got))
				for _, n := range got {
					out = append(out, named{n.Category.Name, n.Depth})
				}
				return out, err
			},
		},
		{
			kind: "location",
			build: func(t *testing.T, _ *origin.Originator, l *ledger.Processor, ctx context.Context) forest {
				return locations(t, l, ctx)
			},
			path: func(r *query.Reader, ctx context.Context, id int64) ([]named, error) {
				got, err := r.LocationPath(ctx, domain.LocationID(id))
				out := make([]named, 0, len(got))
				for _, l := range got {
					out = append(out, named{Name: l.Name})
				}
				return out, err
			},
			subtree: func(r *query.Reader, ctx context.Context, id int64) ([]named, error) {
				got, err := r.LocationSubtree(ctx, domain.LocationID(id))
				out := make([]named, 0, len(got))
				for _, n := range got {
					out = append(out, named{n.Location.Name, n.Depth})
				}
				return out, err
			},
			forest: func(r *query.Reader, ctx context.Context) ([]named, error) {
				got, err := r.LocationForest(ctx)
				out := make([]named, 0, len(got))
				for _, n := range got {
					out = append(out, named{n.Location.Name, n.Depth})
				}
				return out, err
			},
		},
	}
}

func setup(t *testing.T, tr tree) (*query.Reader, context.Context, forest) {
	t.Helper()
	conn := testsupport.NewDB(t)
	ctx := context.Background()
	return query.New(conn), ctx, tr.build(t, origin.New(conn), ledger.New(conn), ctx)
}

func names(nodes []named) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
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

// TestPathReadsRootFirst: a path is a breadcrumb, so it has to start at the top
// or it is not one.
func TestPathReadsRootFirst(t *testing.T) {
	for _, tr := range trees() {
		t.Run(tr.kind, func(t *testing.T) {
			r, ctx, f := setup(t, tr)

			got, err := tr.path(r, ctx, f.leaf)
			if err != nil {
				t.Fatalf("path: %v", err)
			}
			if want := []string{"Root", "Middle", "Leaf"}; !equal(names(got), want) {
				t.Errorf("path = %v, want %v", names(got), want)
			}
		})
	}
}

// TestPathOfARootIsJustItself is the boundary the upward walk has to stop at.
func TestPathOfARootIsJustItself(t *testing.T) {
	for _, tr := range trees() {
		t.Run(tr.kind, func(t *testing.T) {
			r, ctx, f := setup(t, tr)

			got, err := tr.path(r, ctx, f.root)
			if err != nil {
				t.Fatalf("path: %v", err)
			}
			if want := []string{"Root"}; !equal(names(got), want) {
				t.Errorf("path = %v, want %v", names(got), want)
			}
		})
	}
}

// TestSubtreeIsDepthFirstWithDerivedDepth: the depth is what indents a tree on
// screen, and depth-first is what makes the indentation mean anything.
func TestSubtreeIsDepthFirstWithDerivedDepth(t *testing.T) {
	for _, tr := range trees() {
		t.Run(tr.kind, func(t *testing.T) {
			r, ctx, f := setup(t, tr)

			got, err := tr.subtree(r, ctx, f.root)
			if err != nil {
				t.Fatalf("subtree: %v", err)
			}
			want := []named{{"Root", 0}, {"Middle", 1}, {"Leaf", 2}, {"Sibling", 1}}
			if len(got) != len(want) {
				t.Fatalf("subtree = %v, want %v", got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("subtree[%d] = %v, want %v", i, got[i], want[i])
				}
			}
		})
	}
}

// TestSubtreeOfALeafIsTheLeaf: a node with no children is still a subtree.
func TestSubtreeOfALeafIsTheLeaf(t *testing.T) {
	for _, tr := range trees() {
		t.Run(tr.kind, func(t *testing.T) {
			r, ctx, f := setup(t, tr)

			got, err := tr.subtree(r, ctx, f.leaf)
			if err != nil {
				t.Fatalf("subtree: %v", err)
			}
			if len(got) != 1 || got[0] != (named{"Leaf", 0}) {
				t.Errorf("subtree = %v, want [{Leaf 0}]", got)
			}
		})
	}
}

// TestSubtreeOfNothingIsNotFound: an identifier that names nothing is an error,
// not an empty tree, because an empty tree renders as a node that exists and
// holds nothing.
func TestSubtreeOfNothingIsNotFound(t *testing.T) {
	for _, tr := range trees() {
		t.Run(tr.kind, func(t *testing.T) {
			r, ctx, _ := setup(t, tr)

			if _, err := tr.subtree(r, ctx, 9999); err == nil {
				t.Error("subtree of a nonexistent node returned no error")
			}
		})
	}
}

// TestForestIsEveryRootInOrder: the forest is what the browse view shows, and
// each root's subtree has to arrive whole rather than interleaved.
func TestForestIsEveryRootInOrder(t *testing.T) {
	for _, tr := range trees() {
		t.Run(tr.kind, func(t *testing.T) {
			r, ctx, _ := setup(t, tr)

			got, err := tr.forest(r, ctx)
			if err != nil {
				t.Fatalf("forest: %v", err)
			}
			want := []string{"Root", "Middle", "Leaf", "Sibling", "Second"}
			if !equal(names(got), want) {
				t.Errorf("forest = %v, want %v", names(got), want)
			}
		})
	}
}
