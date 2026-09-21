package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"home-management-system/internal/command"
	"home-management-system/internal/importer"
	"home-management-system/internal/tui/review"
	"home-management-system/internal/tui/text"
)

// A bound file, as a set of changes to review.
//
// The adapter, and the only place that knows those are the same thing. It
// lives HERE rather than in either package it joins: internal/importer knows
// nothing about screens, and internal/tui/review must not know that a file is
// one of the things that can fill it -- a rule archlint holds, because the
// second producer is what the whole seam is for.
//
// Everything that needs a vocabulary, or needs to look at the other rows of
// the same file, happens on this side of it. What crosses is words.

// asChanges renders a bound file the way the review screen takes it.
//
// The namer and the stage both matter to the wording, so both are arguments:
// a row's summary is written in names rather than identifiers, and "waits for
// row 2" is true only on a stage that applies in passes.
func asChanges(plan importer.Plan, names command.Namer, stage review.Stage) []review.Change {
	out := make([]review.Change, 0, len(plan.Entries))
	for i, entry := range plan.Entries {
		out = append(out, review.Change{
			State: asState(entry.State),
			At:    fmt.Sprintf("%d", entry.Row.Line),
			What:  describe(entry, names),
			Why:   whyNot(plan, i, entry, stage),
		})
	}
	return out
}

// asState maps the binder's four states onto the screen's.
//
// They are the same four, deliberately: the states are what a person has to
// do next, and that does not change with who proposed the change. Written out
// rather than assumed equal, because two enumerations that happen to line up
// today are a silent renumbering away from not.
func asState(s importer.State) review.State {
	switch s {
	case importer.Ready:
		return review.Ready
	case importer.Confirmable:
		return review.Confirmable
	case importer.Blocked:
		return review.Blocked
	}
	return review.Dropped
}

// describe is what a row would do, in words.
func describe(entry importer.Entry, names command.Namer) string {
	if entry.Command != nil {
		return command.Summary(entry.Command, names)
	}
	// Not bound yet, so there are no identifiers to render names from. The
	// raw text is what the person wrote, which is the next best thing to show
	// and is in fact what they will be correcting.
	return entry.AsWritten()
}

// whyNot is the first thing standing in a row's way.
func whyNot(plan importer.Plan, at int, entry importer.Entry, stage review.Stage) string {
	switch entry.State {
	case importer.Ready:
		return ""
	case importer.Dropped:
		return "dropped"
	}
	// A row whose parent is made by another row of the same file is not
	// waiting for a person at all. Saying "would create a category" or "would
	// create a location" there invited somebody to make a second one, which
	// is precisely what it must not do.
	//
	// Only on a stage that applies in PASSES, because only there does waiting
	// come to anything: a stage that applies all at once can never make the
	// thing and then bind the row that names it, so on one of those the row's
	// only route really is the creation panel.
	if stage.InPasses && !entry.IsCreation() {
		if other, ok := plan.WillBeCreatedBy(at); ok {
			return fmt.Sprintf("waits for row %d", plan.Entries[other].Row.Line)
		}
	}
	if len(entry.Creates) > 0 {
		return "would create " + text.Article(strings.ToLower(string(entry.Creates[0].Kind)))
	}
	if len(entry.Issues) > 0 {
		return entry.Issues[0].String()
	}
	return "needs confirming"
}

// fileHeading is what the review screen calls a file: its NAME, not the path
// to it.
//
// You chose the file a moment ago; the directory it happens to sit in is not
// what you are reviewing, and an absolute path ran the heading past the
// terminal -- 84 columns on an 80-column screen for an ordinary temporary
// directory. A line wider than the screen wraps, and one wrapped line shifts
// every row below it.
func fileHeading(source string) string { return filepath.Base(source) }
