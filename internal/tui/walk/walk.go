// Package walk is verifying a subtree: one holding at a time, in front of
// the shelf it is on.
//
// The ledger's whole claim is that it says what is in the house. Nothing in
// it checks that claim against the house itself, and nothing can: the only
// instrument is a person standing in the garage with their eyes open. So the
// walk is the one place the system asks rather than tells, and what it asks
// has to be answerable in one keystroke with a box in your other hand.
//
// # An observation is not a correction
//
// This is the distinction the whole design turns on, and the ledger already
// draws it: counting a shelf records Counted, and only a count that DISAGREES
// also records Adjusted. Looking for something and not finding it records
// Verified{Present: false}, and only then MarkedLost. Two events, because
// they are two claims -- "I looked" and "so the number changed" -- and a
// system that recorded only the second could never tell an unchecked shelf
// from a correct one.
//
// The walk exists to produce the first of each pair. That is why confirming
// a count is worth a keystroke even when nothing is wrong: an answer of
// "still 500 g" is not a no-op, it is the evidence.
//
// # What is held here
//
// Answers, and the holdings they are about. Not a tally, not a projection,
// and nothing about what the house will look like afterwards -- the batch is
// compiled into Commands and planned as one transaction, exactly as an
// import stage is, so the house's own rules get the last word rather than
// arithmetic done in a screen.
package walk

import (
	"fmt"

	"home-management-system/internal/command"
	"home-management-system/internal/domain"
)

// Answer is what a person said about one holding.
type Answer int

const (
	// Unanswered is a holding the walk has not reached, or has skipped past.
	Unanswered Answer = iota
	// Right is "the ledger has it correct". For a Unique holding that is a
	// sighting; for a Bulk one it is a count that happens to agree.
	Right
	// Amount is "there is this much instead", and only a Bulk holding has
	// one. The figure is on the Stop.
	Amount
	// Missing is "I looked and it is not there".
	Missing
)

// Stop is one holding on the walk, and what was said about it.
type Stop struct {
	// Holding, and the words that identify it on the shelf. The walk holds
	// what it needs to ASK -- the name, where it is, what the ledger claims
	// -- and nothing else about the row.
	Holding domain.HoldingID
	Item    string
	Where   string
	// Claim is what the ledger says, in words, for the person to disagree
	// with.
	Claim string
	// Bulk says this holding has an amount rather than a custody, which is
	// what decides whether "this much instead" is even a question.
	Bulk bool
	// OnHand is the ledger's own figure, sent back unchanged when the answer
	// is Right. A count that agrees still records Counted.
	OnHand domain.Quantity
	Unit   domain.UnitCode

	Answer Answer
	// Observed is the figure a person gave, when the answer is Amount.
	Observed domain.Quantity
}

// Said reports this stop having been answered one way or another.
func (s Stop) Said() bool { return s.Answer != Unanswered }

// Says what was decided, in words, for the review that follows.
//
// The PLACE is in every line, and it is not decoration. One item is commonly
// held in three places at once, so three stops of a walk can be about the
// same name; a review that said "counted Ancho Chile: still 100 g" three
// times would be three rows nobody could tell apart, on the screen whose
// only job is to be checked before it is written.
func (s Stop) Says() string {
	at := s.Where
	if at == "" {
		at = "nowhere named"
	}
	switch s.Answer {
	case Right:
		if s.Bulk {
			return fmt.Sprintf("counted %s in %s: still %s", s.Item, at, s.Claim)
		}
		return fmt.Sprintf("saw %s in %s, where it should be", s.Item, at)
	case Amount:
		return fmt.Sprintf("counted %s in %s: %s %s, not %s",
			s.Item, at, s.Observed, s.Unit, s.Claim)
	case Missing:
		if s.Bulk {
			return fmt.Sprintf("counted %s in %s: none left, where the ledger says %s",
				s.Item, at, s.Claim)
		}
		return fmt.Sprintf("could not find %s in %s", s.Item, at)
	}
	return ""
}

// Command is what this stop would write, and false when it would write
// nothing.
//
// The two kinds of holding answer different questions, so they produce
// different commands, and neither is a special case of the other. A Bulk
// holding has an amount, so every answer about it is a count -- including
// "none left", which is a count of zero rather than a disappearance. A
// Unique holding is there or it is not, and saying so is exactly what Verify
// is for: the ledger turns a failed sighting into a MarkedLost itself, and a
// successful one on something already given up for lost into a Found.
func (s Stop) Command() (command.Command, bool) {
	switch s.Answer {
	case Right:
		if s.Bulk {
			return command.Count{Holding: s.Holding, Observed: s.OnHand}, true
		}
		return command.Verify{Holding: s.Holding, Present: true}, true
	case Amount:
		return command.Count{Holding: s.Holding, Observed: s.Observed}, true
	case Missing:
		if s.Bulk {
			return command.Count{Holding: s.Holding, Observed: domain.Zero}, true
		}
		return command.Verify{Holding: s.Holding, Present: false}, true
	}
	return nil, false
}

// Model is a walk in progress.
type Model struct {
	// where is what is being walked, for the heading -- "the Garage", or the
	// whole house.
	where string
	stops []Stop
	at    int
}

// New starts a walk over the stops, in the order they were given.
//
// The order is the CALLER's, and that is the whole ergonomic question: a walk
// is useful exactly to the degree that its order matches the order you can
// physically visit things in. Sorted by place is the nearest the system can
// get to that from here.
func New(where string, stops []Stop) Model {
	return Model{where: where, stops: stops}
}

// Where is what is being walked.
func (m Model) Where() string { return m.where }

// Len is how many stops there are.
func (m Model) Len() int { return len(m.stops) }

// At is which stop the walk is on.
func (m Model) At() int { return m.at }

// Stops is every stop, in order.
func (m Model) Stops() []Stop { return m.stops }

// Current is the stop in front of you.
func (m Model) Current() (Stop, bool) {
	if m.at < 0 || m.at >= len(m.stops) {
		return Stop{}, false
	}
	return m.stops[m.at], true
}

// Answered is how many stops have been settled.
func (m Model) Answered() int {
	var n int
	for _, s := range m.stops {
		if s.Said() {
			n++
		}
	}
	return n
}

// Done reports every stop having been answered.
func (m Model) Done() bool { return len(m.stops) > 0 && m.Answered() == len(m.stops) }

// Say records an answer and moves on.
//
// Moving on is part of the same gesture rather than a second keystroke,
// because a walk is done with both hands full. It stops at the END rather
// than wrapping: wrapping would put you back at the top of a list you have
// just finished, with nothing on screen saying you had.
func (m Model) Say(answer Answer, observed domain.Quantity) Model {
	if m.at < 0 || m.at >= len(m.stops) {
		return m
	}
	stops := append([]Stop{}, m.stops...)
	stops[m.at].Answer = answer
	stops[m.at].Observed = observed
	m.stops = stops
	if m.at < len(m.stops)-1 {
		m.at++
	}
	return m
}

// Step moves without answering, which is how you look at one again.
func (m Model) Step(by int) Model {
	at := m.at + by
	if at < 0 {
		at = 0
	}
	if at > len(m.stops)-1 {
		at = len(m.stops) - 1
	}
	m.at = at
	return m
}

// Commands is what the walk would write, in the order it was walked.
//
// Only the stops that were ANSWERED. A walk abandoned halfway writes what it
// checked and says nothing about the rest, which is the honest outcome: the
// unvisited shelves are exactly as unverified as they were before, and a
// walk that silently confirmed them would be the ledger lying with more
// confidence than before somebody tried to check it.
func (m Model) Commands() []command.Command {
	var out []command.Command
	for _, s := range m.stops {
		if cmd, ok := s.Command(); ok {
			out = append(out, cmd)
		}
	}
	return out
}

// WhyNot says what is stopping the walk from being filed, or nothing.
func (m Model) WhyNot() string {
	if m.Answered() == 0 {
		return "nothing has been checked yet"
	}
	return ""
}
