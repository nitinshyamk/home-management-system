package app

import (
	"context"

	"home-management-system/internal/domain"
)

// What is inside a node of a tree.
//
// A nil root means the whole house: a forest has no trunk, so "everything" is
// the one node the tree does not have, and nil cannot be confused with the
// location numbered 0 the way a sentinel could.
//
// Retired Holdings are excluded throughout, because the tree's rollups
// already exclude them (query_locations.sql) and a contents pane beside a
// count that disagreed with it would be wrong on screen. Holdings() still
// returns them: it is the raw list, and a lookup by identifier has to find
// one.

// HoldingsUnder is every Holding stowed at a Location or beneath it. Shallow
// asks what was filed HERE, which is how something filed too coarsely becomes
// findable; deep asks what is in the room.
func (c *controller) HoldingsUnder(ctx context.Context, root *domain.LocationID, deep bool) ([]HoldingRow, error) {
	rows, err := c.Holdings(ctx)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return live(rows), nil
	}
	within, err := c.locationScope(ctx, *root, deep)
	if err != nil {
		return nil, err
	}
	out := make([]HoldingRow, 0, len(rows))
	for _, r := range rows {
		if !r.Retired && within[r.LocationID] {
			out = append(out, r)
		}
	}
	return out, nil
}

// ItemsUnder is every Item classified at a Category or beneath it.
func (c *controller) ItemsUnder(ctx context.Context, root *domain.CategoryID, deep bool) ([]ItemRow, error) {
	rows, err := c.Items(ctx)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return rows, nil
	}
	within, err := c.categoryScope(ctx, *root, deep)
	if err != nil {
		return nil, err
	}
	out := make([]ItemRow, 0, len(rows))
	for _, r := range rows {
		if within[r.CategoryID] {
			out = append(out, r)
		}
	}
	return out, nil
}

// HoldingsOfItem is every placement of one Item: the Item/Holding split made
// visible, and what a search lists once it has resolved a name.
func (c *controller) HoldingsOfItem(ctx context.Context, id domain.ItemID) ([]HoldingRow, error) {
	rows, err := c.Holdings(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]HoldingRow, 0, 4)
	for _, r := range rows {
		if !r.Retired && r.ItemID == id {
			out = append(out, r)
		}
	}
	return out, nil
}

func live(rows []HoldingRow) []HoldingRow {
	out := make([]HoldingRow, 0, len(rows))
	for _, r := range rows {
		if !r.Retired {
			out = append(out, r)
		}
	}
	return out
}

// locationScope is the set a rollup covers. Shallow does not read the
// subtree: asking the database to walk a tree whose answer is one identifier
// is a query for nothing.
func (c *controller) locationScope(ctx context.Context, root domain.LocationID, deep bool) (map[domain.LocationID]bool, error) {
	if !deep {
		return map[domain.LocationID]bool{root: true}, nil
	}
	nodes, err := c.read.LocationSubtree(ctx, root)
	if err != nil {
		return nil, err
	}
	within := make(map[domain.LocationID]bool, len(nodes))
	for _, n := range nodes {
		within[n.Location.ID] = true
	}
	return within, nil
}

func (c *controller) categoryScope(ctx context.Context, root domain.CategoryID, deep bool) (map[domain.CategoryID]bool, error) {
	if !deep {
		return map[domain.CategoryID]bool{root: true}, nil
	}
	nodes, err := c.read.CategorySubtree(ctx, root)
	if err != nil {
		return nil, err
	}
	within := make(map[domain.CategoryID]bool, len(nodes))
	for _, n := range nodes {
		within[n.Category.ID] = true
	}
	return within, nil
}
