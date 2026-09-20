// Package status is everything the interface has to say about itself.
//
// One value, four kinds of thing, four lifetimes. Before this package they were
// two fields on the model -- a `status string` and a `problem []string` -- and
// the string carried three unrelated jobs at once:
//
//   - the per-view hint every loader supplies ("enter for history", "TAB fold
//   - S-TAB fold all - v contents", "clean"),
//   - what the last action did ("unchanged", "applied 4 rows in one
//     transaction", the write summary),
//   - and what went wrong, once errMsg wrote "error: ..." into the same string.
//
// Since every load overwrote it, the three destroyed each other. Two
// workarounds are the evidence: the carry banner became a SECOND status channel
// because the first one could not survive the view switch the gesture requires
// in the middle, and reloadKeepingStatus re-ran a load and then put the old
// string back over the freshly loaded one -- a function that existed only
// because two facts with different lifetimes shared a field.
//
// So they are four fields, and each is replaced only by its own kind of event.
// A load can no longer erase a refusal; a refusal can no longer erase what is
// in your hand.
package status

import (
	"home-management-system/internal/tui/style"
	"home-management-system/internal/tui/text"
)

// Model is what the interface is currently saying.
type Model struct {
	// hint belongs to the VIEW, and every load replaces it. "enter for history"
	// is not news; it is a standing fact about the screen you are on.
	hint string
	// outcome is what the last action did. Transient, and the next action
	// replaces it.
	outcome string
	// problem is why the last action was refused, held as lines rather than a
	// string because it is wrapped and rendered loudly rather than squeezed
	// into a one-line summary.
	problem []string
	// carrying is what is in hand, which survives everything until it is put
	// down -- including the view switch that is the middle step of the gesture.
	carrying string
	// working is a command in flight. It is state rather than an outcome: the
	// outcome is what replaces it when the answer arrives.
	working string
}

func New() Model { return Model{} }

// SetHint replaces the view's standing hint, and touches nothing else. This is
// what a load calls, and the whole point of the package is that it can no
// longer take anything else with it.
func (m Model) SetHint(hint string) Model { m.hint = hint; return m }

// Hint is the view's standing hint, for the facts line.
func (m Model) Hint() string { return m.hint }

// Report says what just happened, and clears the refusal it answers.
//
// Mutually exclusive with Refuse BY CONSTRUCTION. The two used to be kept apart
// by hand -- a dozen sites wrote `m.status = ""` or `m.problem = nil` next to
// the other one -- and nothing enforced it, so missing one meant a screen
// showing a success and a failure at the same time.
func (m Model) Report(outcome string) Model {
	m.outcome, m.problem, m.working = outcome, nil, ""
	return m
}

// Refuse says why not, and clears the outcome it replaces.
func (m Model) Refuse(issues ...string) Model {
	m.problem, m.outcome, m.working = issues, "", ""
	return m
}

// Refused reports whether something was refused, for the callers that used to
// test len(m.problem).
func (m Model) Refused() bool { return len(m.problem) > 0 }

// Working says a command is in flight, so the screen is not silent between the
// keystroke and the answer.
func (m Model) Working(what string) Model {
	m.working, m.outcome, m.problem = what, "", nil
	return m
}

// Carrying holds what has been picked up, worded by the caller: where a thing
// can GO is the useful half, and it differs by kind.
func (m Model) Carrying(what string) Model {
	m.carrying, m.problem, m.outcome = what, nil, ""
	return m
}

// Dropped puts down whatever was in hand.
func (m Model) Dropped() Model { m.carrying = ""; return m }

// Clear takes back the last outcome and the last refusal, for the start of a
// fresh gesture -- opening the command line, say. It leaves the hint and the
// carry alone: neither is about the gesture being abandoned.
func (m Model) Clear() Model {
	m.outcome, m.problem, m.working = "", nil, ""
	return m
}

// Lines is the block: what is in hand, what is in flight, and then what
// happened or why it did not.
//
// In that order, and it is the order of LIFETIMES -- the things that outlive
// this keystroke above the thing that does not. Everything is indented to one
// left edge, so the block reads as one region rather than as four features that
// happen to be adjacent.
func (m Model) Lines(width int) []string {
	var out []string
	say := func(s string, render func(...string) string) {
		for _, line := range text.Wrap(s, max(20, width-2)) {
			out = append(out, "  "+render(line))
		}
	}
	if m.carrying != "" {
		say(m.carrying, style.Strong.Render)
	}
	if m.working != "" {
		say(m.working, style.Dim.Render)
	}
	// One or the other, never both -- which Report and Refuse guarantee, so
	// this does not have to choose.
	for _, issue := range m.problem {
		say(issue, style.Error.Render)
	}
	if m.outcome != "" {
		say(m.outcome, func(s ...string) string { return s[0] })
	}
	return out
}

// Height is what the block costs, which the layout needs before it knows what
// the block will draw.
func (m Model) Height(width int) int { return len(m.Lines(width)) }
