package undo_test

import (
	"testing"

	"home-management-system/internal/command"
	"home-management-system/internal/domain"
	"home-management-system/internal/tui/undo"
)

// What has a way back, and what does not.
//
// The second list is the one that matters. An undo that silently did nothing
// on the scariest operation would be worse than no undo at all, so every
// case here that returns false is a case the interface must not offer.

func TestWhatCanBePutBack(t *testing.T) {
	was := undo.Before{Location: 4, Category: 7, Name: "Old Name", Custody: "AtRest"}

	for _, c := range []struct {
		what string
		cmd  command.Command
		want command.Command
	}{
		{
			"a whole move",
			command.Move{Holding: 1, To: 9},
			command.Move{Holding: 1, To: 4},
		},
		{
			"a re-filing",
			command.Reclassify{Item: 2, Category: 9},
			command.Reclassify{Item: 2, Category: 7},
		},
		{
			"checking out",
			command.CheckOut{Holding: 3},
			command.Return{Holding: 3},
		},
		{
			"bringing back",
			command.Return{Holding: 3},
			command.CheckOut{Holding: 3},
		},
		{
			"a rehome",
			command.Rehome{Holding: 5, To: 9},
			command.Rehome{Holding: 5, To: 4},
		},
	} {
		t.Run(c.what, func(t *testing.T) {
			got, ok := undo.Inverse(c.cmd, was)
			if !ok {
				t.Fatalf("%s has no inverse, and it should", c.what)
			}
			if got != c.want {
				t.Errorf("the way back from %s is %#v, want %#v", c.what, got, c.want)
			}
		})
	}
}

func TestWhatCannotBePutBack(t *testing.T) {
	was := undo.Before{Location: 4, Category: 7, Name: "Old Name"}
	amount := domain.FromMilli(100 * domain.Scale)

	for _, c := range []struct {
		what string
		cmd  command.Command
	}{
		// Receiving is not the inverse: it would record an acquisition, with
		// a source, that never happened.
		{"consuming", command.Consume{Item: 1, Location: 2, Amount: amount}},
		// Nothing clears RetiredAt. Gone is the single lifecycle terminal.
		{"retiring", command.Retire{Holding: 1, Reason: "worn out"}},
		// The observation happened. It cannot be un-observed.
		{"counting", command.Count{Holding: 1, Observed: amount}},
		// Nothing deletes.
		{"creating a place", command.NewLocation{Name: "Shed"}},
		// A partial move splits a Holding; moving the same amount back merges
		// it with whatever is at the other end, which reaches the right total
		// by a route that is not the reverse of what happened.
		{"part of a move", command.Move{Holding: 1, To: 9, Amount: &amount}},
	} {
		t.Run(c.what, func(t *testing.T) {
			if got, ok := undo.Inverse(c.cmd, was); ok {
				t.Errorf("%s was given an inverse (%#v), and it must not have one", c.what, got)
			}
		})
	}
}

// TestABatchIsAllOrNothing: undoing three quarters of an action and saying
// nothing about the rest is the failure this package exists to avoid.
func TestABatchIsAllOrNothing(t *testing.T) {
	were := []undo.Before{{Location: 4}, {Location: 5}}

	both := undo.StepFor("moved two", []command.Command{
		command.Move{Holding: 1, To: 9},
		command.Move{Holding: 2, To: 9},
	}, were)
	if !both.Possible() || len(both.Back) != 2 {
		t.Errorf("a batch of two moves gave %d ways back, want 2", len(both.Back))
	}
	// And in reverse: undoing is retracing.
	if first, ok := both.Back[0].(command.Move); !ok || first.Holding != 2 {
		t.Errorf("the way back starts with %#v, want the LAST thing done", both.Back[0])
	}

	mixed := undo.StepFor("moved one and retired one", []command.Command{
		command.Move{Holding: 1, To: 9},
		command.Retire{Holding: 2},
	}, were)
	if mixed.Possible() {
		t.Error("a batch containing a retirement was given a way back")
	}
	// And it still says what it was, so the refusal can name it.
	if mixed.Did != "moved one and retired one" {
		t.Errorf("Did = %q, want what was done -- the refusal has to name it", mixed.Did)
	}
}
