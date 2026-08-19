package command

import (
	"context"
	"fmt"

	"home-management-system/internal/domain"
	"home-management-system/internal/query"
	"home-management-system/internal/resolve"
)

// Vocabulary is everything Bind needs: the names, and the facts that decide
// what a quantity means.
//
// It is loaded once and used for a whole batch, for the same reason the resolve
// index is: a two-hundred-row receipt should not be two hundred round trips,
// and every row should be bound against the SAME picture of the world. A row
// that resolved differently from the row above it because something changed
// mid-import would make an all-or-nothing import a lie.
type Vocabulary struct {
	// Names is the flat index of everything that can be referred to.
	Names *resolve.Index

	units    map[domain.UnitCode]domain.Unit
	items    map[domain.ItemID]domain.Item
	holdings map[domain.ItemID][]HoldingFacts
}

// HoldingFacts is what Bind needs to pick a Holding when a command names an
// Item and a place instead.
type HoldingFacts struct {
	ID       domain.HoldingID
	Item     domain.ItemID
	Location domain.LocationID
	Retired  bool
}

// LoadVocabulary reads the whole vocabulary through the query path.
func LoadVocabulary(ctx context.Context, r *query.Reader) (*Vocabulary, error) {
	names, err := resolve.Build(ctx, r)
	if err != nil {
		return nil, err
	}
	v := &Vocabulary{
		Names:    names,
		units:    map[domain.UnitCode]domain.Unit{},
		items:    map[domain.ItemID]domain.Item{},
		holdings: map[domain.ItemID][]HoldingFacts{},
	}

	units, err := r.Units(ctx)
	if err != nil {
		return nil, err
	}
	for _, u := range units {
		v.units[u.Code] = u
	}

	items, err := r.Items(ctx)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		v.items[it.Base().ID] = it
	}

	holdings, err := r.Holdings(ctx)
	if err != nil {
		return nil, err
	}
	for _, h := range holdings {
		base := h.Holding.Base()
		v.holdings[base.Item] = append(v.holdings[base.Item], HoldingFacts{
			ID: base.ID, Item: base.Item, Location: base.StowedLocation,
			Retired: base.RetiredAt != nil,
		})
	}
	return v, nil
}

// NewVocabulary assembles one from parts, for tests and for callers that
// already hold the pieces.
func NewVocabulary(names *resolve.Index, units []domain.Unit, items []domain.Item, holdings []HoldingFacts) *Vocabulary {
	v := &Vocabulary{
		Names:    names,
		units:    map[domain.UnitCode]domain.Unit{},
		items:    map[domain.ItemID]domain.Item{},
		holdings: map[domain.ItemID][]HoldingFacts{},
	}
	for _, u := range units {
		v.units[u.Code] = u
	}
	for _, it := range items {
		v.items[it.Base().ID] = it
	}
	for _, h := range holdings {
		v.holdings[h.Item] = append(v.holdings[h.Item], h)
	}
	return v
}

// unit returns a unit's reference data.
func (v *Vocabulary) unit(code domain.UnitCode) (domain.Unit, error) {
	u, ok := v.units[code]
	if !ok {
		return domain.Unit{}, fmt.Errorf("%w: %q is not a unit I know", ErrValue, code)
	}
	return u, nil
}

// item returns an Item's measurement facts.
func (v *Vocabulary) item(id domain.ItemID) (domain.Item, bool) {
	it, ok := v.items[id]
	return it, ok
}

// liveHoldings returns an Item's active Holdings.
func (v *Vocabulary) liveHoldings(item domain.ItemID) []HoldingFacts {
	var out []HoldingFacts
	for _, h := range v.holdings[item] {
		if !h.Retired {
			out = append(out, h)
		}
	}
	return out
}
