package tui

import (
	"fmt"

	"home-management-system/internal/tui/organise"
	"home-management-system/internal/tui/review"
)

// A staged batch, as a set of changes to review.
//
// The second adapter, and the reason the first one had to stop being the
// review screen itself. It is a dozen lines because there is almost nothing
// to translate: an Edit already holds what it would do and why it cannot, in
// words, for the same reason a file's row does -- both were worked out on
// the producer's side, where the vocabulary is.
//
// What is NOT here is as much the point. There is no tree to walk, no diff to
// compute and no shape to compare, because organise mode stages commands. See
// the note at the top of internal/tui/organise.

// asStagedChanges renders a batch the way the review screen takes it.
func asStagedChanges(edits []organise.Edit) []review.Change {
	out := make([]review.Change, 0, len(edits))
	for i, e := range edits {
		state := review.Ready
		if e.Blocked() {
			state = review.Blocked
		}
		out = append(out, review.Change{
			State: state,
			// The order it was staged in, which is the only handle a staged
			// edit has: it came from the cursor rather than from a line of a
			// file. It is also what "take back the last" counts down from.
			At:   fmt.Sprintf("%d", i+1),
			What: e.What,
			Why:  e.Why,
		})
	}
	return out
}
