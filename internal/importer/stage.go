package importer

import (
	"strings"

	"home-management-system/internal/command"
	"home-management-system/internal/resolve"
)

// The stages of an import.
//
// A file written from unstructured content routinely proposes a SHAPE -- a
// classification, a set of places, or both -- and the things filed into it at
// the same time, and the two cannot be reviewed as one list. A row that files
// something under a category or into a place the same file is about to create
// has nothing to resolve against: it arrives blocked, for a reason that is not
// the row's fault and that no amount of looking at the row explains.
//
// So the shape goes first, as its own screen and its own transaction, and
// everything else is bound AGAIN afterwards -- against a vocabulary that now
// contains whatever the earlier stages made. That second binding is the whole
// point of splitting: it is what turns "Spices does not exist" into an ordinary
// ready row without anybody retyping anything.
//
// The categories and the places are two stages rather than one, even though
// neither waits on the other. What separates them is not a dependency but a
// DECISION: each can be skipped on its own, and a file whose proposed
// classification is nonsense but whose places are right should cost one
// keystroke, not a choice between fixing the categories row by row and
// throwing the places away with them.
//
// A structural stage can be skipped rather than only applied, because a
// proposed shape is the part a file is most likely to get wrong, and the review
// screen can already file a row into a category or a place that exists.
// Skipping leaves the house exactly as it was.

// Stage is which part of an import a row belongs to.
type Stage int

const (
	// StageCategories is the bulk category upload: the rows whose whole
	// business is the shape of the classification.
	StageCategories Stage = iota
	// StageLocations is the bulk upload of places: the same thing for the
	// tree that says where things are, which a file photographed off a shelf
	// proposes exactly as readily as it proposes categories.
	StageLocations
	// StageReview is everything else -- the items and the holdings, which is
	// the review the interface has always had.
	StageReview
)

// stageOrder is the order the stages are reviewed in, and the only place it is
// written down.
//
// The two structural stages do not depend on each other -- a category is never
// filed under a place -- so the order between them is a choice rather than a
// consequence. It is the order the domain names them in: what a thing IS, then
// where it is kept.
var stageOrder = []Stage{StageCategories, StageLocations, StageReview}

// String names the stage in the words the screen uses for it.
func (s Stage) String() string {
	switch s {
	case StageCategories:
		return "categories"
	case StageLocations:
		return "places"
	}
	return "items and holdings"
}

// Structural reports whether a stage proposes the SHAPE of the house -- a tree
// of classifications or a tree of places -- rather than what is in it.
//
// Two things follow, and between them they are what makes a stage a stage. A
// tree is built from the top down, so the stage applies in PASSES rather than
// all at once: see Plan.ApplicableInPasses. And a proposed shape is the part of
// a file most likely to be wrong, so the stage can be SKIPPED: nothing is
// written, and the review behind it can still file a row into a category or a
// place that already exists.
func (s Stage) Structural() bool { return s != StageReview }

// StageOf says which stage a row belongs to.
//
// Read off the SPEC rather than a list of ops written down here. A list is a
// second statement of what a command is about, and it would be wrong the first
// time somebody added a command without finding it -- silently, by putting a
// category row in front of a person reviewing holdings.
func StageOf(row Row) Stage {
	spec, ok := command.SpecOf(command.Op(strings.ToLower(strings.TrimSpace(row.Raw.Op))))
	if !ok {
		// An op nothing recognises is the review screen's to report, in its own
		// words. It is not a structural row, and a stage that swallowed it
		// would be a stage that hid the one row a person most needs to see.
		return StageReview
	}
	return stageOf(spec)
}

// stageOf is the rule, in terms of the vocabulary.
//
// A command belongs to a structural stage when it brings that stage's kind into
// existence, or when every name it takes is of that one kind and it brings
// nothing into existence at all. The second clause is what catches `reparent
// category`, `archive location` and `restore location` without naming them; the
// exclusion of the creating commands is what keeps `new item` out of it, whose
// only named field IS a category and which is emphatically not a category row.
//
// One kind throughout, which is what puts `rehome` and `check out` in the
// review where they belong: they name a Holding and a Location, and a row that
// moves a thing is about the thing rather than about the shape of the house.
func stageOf(spec command.Spec) Stage {
	if stage, ok := stageFor(spec.Creates); ok {
		return stage
	}
	if spec.Creates != "" {
		return StageReview
	}
	var only resolve.Kind
	for _, field := range spec.Fields {
		if field.Type != command.FieldName {
			continue
		}
		if len(field.Kinds) != 1 {
			// A field that could mean several kinds says nothing about which
			// tree the row is shaping.
			return StageReview
		}
		if only != "" && field.Kinds[0] != only {
			return StageReview
		}
		only = field.Kinds[0]
	}
	// only is empty when the command names nothing -- there are none today --
	// and stageFor turns that away with everything else that has no stage of
	// its own: such a command is about the house rather than about its shape.
	stage, ok := stageFor(only)
	if !ok {
		return StageReview
	}
	return stage
}

// stageFor is the structural stage a kind has, if it has one.
func stageFor(kind resolve.Kind) (Stage, bool) {
	switch kind {
	case resolve.KindCategory:
		return StageCategories, true
	case resolve.KindLocation:
		return StageLocations, true
	}
	return StageReview, false
}

// StagePlan is one stage of an import: the rows, and which stage they are.
type StagePlan struct {
	Stage Stage
	Plan  Plan
}

// Stages divides a bound file into the stages it actually has, in order,
// keeping file order within each.
//
// Only the stages the file HAS. A file of holdings proposes no shape at all and
// is one screen; a file that proposes places and holdings is two; one that
// proposes both trees and the things in them is three. Handing back the empty
// ones would make every caller ask which of them were real, and would make the
// screen say "stage 1 of 3" about a file with one stage in it.
//
// Every stage keeps the Source, because every stage is a screen ABOUT that
// file, and a screen that could not say which file it was reviewing would be
// the one screen in the system that does not.
func (p Plan) Stages() []StagePlan {
	rows := make(map[Stage][]Entry, len(stageOrder))
	for _, entry := range p.Entries {
		stage := StageOf(entry.Row)
		rows[stage] = append(rows[stage], entry)
	}

	var out []StagePlan
	for _, stage := range stageOrder {
		if len(rows[stage]) == 0 {
			continue
		}
		out = append(out, StagePlan{
			Stage: stage,
			Plan:  Plan{Source: p.Source, Entries: rows[stage]},
		})
	}
	if len(out) == 0 {
		// A file with no rows in it. It is still an import, and it is still a
		// review screen saying so -- returning nothing would make "the first
		// stage" a thing every caller had to check for.
		return []StagePlan{{Stage: StageReview, Plan: Plan{Source: p.Source}}}
	}
	return out
}
