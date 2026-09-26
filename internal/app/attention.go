package app

import (
	"context"
	"fmt"
	"sort"
)

// What needs answering, in one list.
//
// It was four lists in three places: the expiry and out-too-long flags on
// every Holding row, the integrity report's discrepancies and orphans, and
// the classification nudges -- each rendered by a different bit of the
// interface, in a different shape, and three of the four reachable only by
// going and looking at a screen nobody had a reason to open.
//
// That is the failure mode the nudge discipline exists to prevent. A system
// that knows something needs doing and waits to be asked has not told you;
// it has made itself available for questioning, which is a different service
// and not the one anybody wanted.
//
// So: one method, one row type, worst first. The interface shows how many
// there are where it cannot be missed, and the list itself one keystroke
// away.

// Nagging is one thing that wants answering.
type Nagging struct {
	// Level is how loudly it reads, and how it sorts.
	Level Attention
	// What it is, as a sentence a person can act on without translating.
	// "cable out 23 days" is a fact; "CUSTODY_STALE" is a log line.
	What string
	// Where it is, so the interface can go there. Empty for the things that
	// are not about one place -- a classification nudge is about an Item,
	// which may be held in five.
	Where string

	// Subject is what to point the cursor at: a Holding or an Item, by
	// identifier. Kind is empty when there is nothing to point at, which is
	// what an orphaned event amounts to -- it is a fact about the ledger
	// rather than about a thing on a shelf.
	Kind string
	ID   int64
}

// Attention is everything that wants answering, worst first.
//
// One read of the house plus the two reports, rather than a query per kind
// of nag. The flags are already computed for every Holding row -- this is
// the same describeFlags the table shows, asked a different question -- so
// the cost of the whole list is the cost of the list the shell draws anyway.
func (c *controller) Attention(ctx context.Context) ([]Nagging, error) {
	var out []Nagging

	holdings, err := c.Holdings(ctx)
	if err != nil {
		return nil, err
	}
	for _, h := range holdings {
		// A retired Holding's "gone" flag is not a nag. It is the record of
		// something finished, and a queue that listed every finished thing
		// would be a queue nobody reads -- which is how a queue stops working
		// for the things that do need answering.
		if h.Retired || h.Attention == AttentionNone {
			continue
		}
		out = append(out, Nagging{
			Level: h.Attention,
			What:  fmt.Sprintf("%s -- %s", h.Item, h.Note),
			Where: h.LocationPath,
			Kind:  "Holding", ID: int64(h.ID),
		})
	}

	report, err := c.Integrity(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range report.Discrepancies {
		// Over, always. The ledger disagreeing with itself is the one thing
		// in this system that invalidates everything else it says, so it can
		// never sort below a jar with a date coming up.
		out = append(out, Nagging{Level: AttentionOver, What: "the ledger disagrees: " + d})
	}
	for _, o := range report.Orphans {
		out = append(out, Nagging{Level: AttentionOver, What: "an event with nothing behind it: " + o})
	}

	nudges, err := c.Nudges(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range nudges {
		out = append(out, Nagging{
			Level: AttentionSoon,
			What: fmt.Sprintf("%q is filed at %q, which has %d subcategories",
				n.Item, n.Category, n.Siblings),
		})
	}

	// Worst first, and stable within a level so the list does not reshuffle
	// under the cursor between one read and the next. A queue that reorders
	// itself while you work down it is a queue you have to start again.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Level > out[j].Level })
	return out, nil
}

// Pressing is how many of them are past it rather than merely coming up,
// which is the number the one-line banner leads with.
func Pressing(all []Nagging) int {
	var n int
	for _, a := range all {
		if a.Level == AttentionOver {
			n++
		}
	}
	return n
}
