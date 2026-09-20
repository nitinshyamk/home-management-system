package app

import (
	"context"

	"home-management-system/internal/command"
	"home-management-system/internal/resolve"
)

// House is the classification and the places that exist today, as the paths a
// row writes.
//
// Read off the SAME index the importer resolves names against, so a path the
// contract offers is a path that resolves. A second walk of the two trees would
// be a second answer to keep in step, and the one thing worse than telling an
// agent nothing about the house is telling it something the resolver disagrees
// with. It lives here rather than in either command that wants it, because both
// of them wanting it is exactly how two answers start.
//
// Archived candidates are left out. They stay in the index so that search can
// still find them, and nothing may be filed into one -- so offering one would be
// offering a path that is guaranteed to come back blocked.
func House(ctx context.Context, ctrl Controller) (command.House, error) {
	index, err := ctrl.SearchIndex(ctx)
	if err != nil {
		return command.House{}, err
	}
	var house command.House
	for _, candidate := range index.All() {
		if candidate.Archived {
			continue
		}
		switch candidate.Kind {
		case resolve.KindCategory:
			house.Categories = append(house.Categories, candidate.Path)
		case resolve.KindLocation:
			house.Locations = append(house.Locations, candidate.Path)
		}
	}
	return house, nil
}
