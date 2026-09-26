// Package undo is the way back from the last thing you did, where there is
// one.
//
// # Undo does not mean undo
//
// The ledger is append-only and that is load-bearing: it is the reason the
// house can be reconstructed, the reason a count can be believed, and the
// reason a discrepancy is detectable at all. Nothing here removes an event.
//
// So `z` can only ever mean "apply the inverse command" -- a NEW entry that
// happens to put things back where they were. The history keeps both, which
// is honest: you did move the crate to the garage, and then you moved it
// back, and a system that hid the first would be lying about the afternoon.
//
// # And it is not always possible
//
// An inverse has to be a command that genuinely restores the prior state,
// not one that merely arrives at a similar number. That rules out more than
// it sounds like:
//
//   - Retiring is terminal. Nothing clears RetiredAt, by design.
//   - Consuming has no inverse. Receiving is not one: it would record an
//     acquisition, with a source, that never happened -- and the ledger's
//     value is precisely that it does not contain events nobody caused.
//     Counting it back is no better, because Counted means somebody looked.
//     A clean inverse would need an Adjust command carrying a reason, which
//     the domain does not have and which is not this package's to invent.
//   - A count cannot be un-observed. The observation happened.
//   - Creating something has no inverse short of deletion, which does not
//     exist here.
//
// What is left is the arrangement -- where a thing is, what it is called,
// what it is filed under, whether it is out -- and that is enough, because
// it is exactly the set of things a person does by accident and notices
// immediately.
//
// Offered per action and absent where it cannot be honoured. An undo that
// silently did nothing on the scariest operation would be worse than no
// undo at all, so the line says `z undo` only when it is true.
package undo

import (
	"home-management-system/internal/command"
	"home-management-system/internal/domain"
)

// Before is what was true of the subject before the command was built.
//
// Supplied by the caller, because the caller is the screen and the screen
// has the row in hand. Reading it back afterwards would be reading it after
// the write, which is the one moment it no longer says what it said.
type Before struct {
	// Location is where a Holding was stowed.
	Location domain.LocationID
	// Category is what an Item was filed under.
	Category domain.CategoryID
	// Name is what the subject was called.
	Name string
	// Custody is whether a Unique Holding was out: "AtRest", "Out", "Lost".
	Custody string
}

// Step is one applied action and the way back from it.
type Step struct {
	// Did is what happened, in the words it was reported in.
	Did string
	// Back is what would put it back, in order.
	Back []command.Command
}

// Possible reports there being a way back.
func (s Step) Possible() bool { return len(s.Back) > 0 }

// Inverse is the command that would put one command back, if there is one.
//
// The false return is the whole point of the signature. Every caller has to
// handle "there is no way back from this", which is the case the interface
// must never quietly get wrong.
func Inverse(cmd command.Command, was Before) (command.Command, bool) {
	switch c := cmd.(type) {
	case command.Move:
		// Only a WHOLE move. A partial one splits a Holding, and moving the
		// same amount back merges it with whatever is at the other end --
		// which lands on the right total by a route that is not the reverse
		// of what happened.
		if c.Amount != nil {
			return nil, false
		}
		return command.Move{Holding: c.Holding, To: was.Location}, true

	case command.Reclassify:
		return command.Reclassify{Item: c.Item, Category: was.Category}, true

	case command.Rename:
		if was.Name == "" {
			return nil, false
		}
		out := c
		out.Name = was.Name
		return out, true

	case command.CheckOut:
		return command.Return{Holding: c.Holding}, true

	case command.Return:
		return command.CheckOut{Holding: c.Holding}, true

	case command.Rehome:
		return command.Rehome{Holding: c.Holding, To: was.Location}, true

	case command.ReparentLocation:
		// The old parent, which may be nothing: a place moved out of the root
		// goes back to the root, and a nil parent is how that is said.
		return command.ReparentLocation{Location: c.Location, Parent: parentLocation(was)}, true

	case command.ReparentCategory:
		return command.ReparentCategory{Category: c.Category, Parent: parentCategory(was)}, true
	}
	return nil, false
}

// parentLocation is the place something used to sit inside, or nothing when
// it sat at the root.
func parentLocation(was Before) *domain.LocationID {
	if was.Location == 0 {
		return nil
	}
	at := was.Location
	return &at
}

// parentCategory is the same question for the classification tree.
func parentCategory(was Before) *domain.CategoryID {
	if was.Category == 0 {
		return nil
	}
	at := was.Category
	return &at
}

// StepFor builds the way back from a batch.
//
// It ALWAYS names what was done, and leaves Back empty when there is no way
// back from it. The distinction matters at the keyboard: "nothing to undo"
// and "there is no way back from retiring that" are different sentences, and
// a caller handed only the first would say the wrong one at the moment
// somebody is asking whether they can take it back.
//
// All or nothing within a batch. One that moved three crates and retired a
// fourth has no way back: undoing three quarters of an action and saying
// nothing about the rest is the failure mode this package exists to avoid,
// and a person pressing one key is not agreeing to a partial reversal they
// were never shown.
func StepFor(did string, commands []command.Command, were []Before) Step {
	if len(commands) == 0 || len(commands) != len(were) {
		return Step{Did: did}
	}
	back := make([]command.Command, 0, len(commands))
	// In reverse, because undoing is retracing: the last thing done is the
	// first thing to put back, and a batch whose steps depended on each other
	// in order depends on them in the other order coming back.
	for i := len(commands) - 1; i >= 0; i-- {
		inverse, ok := Inverse(commands[i], were[i])
		if !ok {
			return Step{Did: did}
		}
		back = append(back, inverse)
	}
	return Step{Did: did, Back: back}
}
