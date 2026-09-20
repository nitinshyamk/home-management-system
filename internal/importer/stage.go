package importer

import (
	"strings"

	"home-management-system/internal/command"
	"home-management-system/internal/resolve"
)

// The two halves of an import.
//
// A file written from unstructured content routinely proposes a
// CLASSIFICATION and the things filed under it at the same time, and the two
// cannot be reviewed as one list. A row that files something under a category
// the same file is about to create has nothing to resolve against: it arrives
// blocked, for a reason that is not the row's fault and that no amount of
// looking at the row explains.
//
// So the categories go first, as their own screen and their own transaction,
// and everything else is bound AGAIN afterwards -- against a vocabulary that
// now contains whatever the first stage made. That second binding is the whole
// point of splitting: it is what turns "Spices does not exist" into an ordinary
// ready row without anybody retyping anything.
//
// The first stage can also be SKIPPED rather than only applied, because a
// proposed classification is the part an agent is most likely to get wrong, and
// the review screen can already file a row into an existing category in place.
// Skipping leaves the house exactly as it was.

// Stage is which half of an import a row belongs to.
type Stage int

const (
	// StageCategories is the bulk category upload: the rows whose whole
	// business is the shape of the classification.
	StageCategories Stage = iota
	// StageReview is everything else -- the items and the holdings, which is
	// the review the interface has always had.
	StageReview
)

// String names the stage in the words the screen uses for it.
func (s Stage) String() string {
	if s == StageCategories {
		return "categories"
	}
	return "items and holdings"
}

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
		// words. It is not a category row, and a stage that swallowed it would
		// be a stage that hid the one row a person most needs to see.
		return StageReview
	}
	return stageOf(spec)
}

// stageOf is the rule, in terms of the vocabulary.
//
// A command belongs to the category stage when it brings a Category into
// existence, or when every name it takes is a Category and it brings nothing
// into existence at all. The second clause is what catches `reparent category`,
// `archive category` and `restore category` without naming them; the exclusion
// of the creating commands is what keeps `new item` out of it, whose only named
// field IS a category and which is emphatically not a category row.
func stageOf(spec command.Spec) Stage {
	if spec.Creates == resolve.KindCategory {
		return StageCategories
	}
	if spec.Creates != "" {
		return StageReview
	}
	named := 0
	for _, field := range spec.Fields {
		if field.Type != command.FieldName {
			continue
		}
		named++
		if len(field.Kinds) != 1 || field.Kinds[0] != resolve.KindCategory {
			return StageReview
		}
	}
	if named == 0 {
		// A command that names nothing -- there are none today -- is about the
		// house rather than about the classification.
		return StageReview
	}
	return StageCategories
}

// Split divides a bound file into its two stages, keeping file order in each.
//
// Both halves keep the Source, because both are screens ABOUT that file and a
// screen that could not say which file it was reviewing would be the one screen
// in the system that does not.
func (p Plan) Split() (categories, review Plan) {
	categories, review = Plan{Source: p.Source}, Plan{Source: p.Source}
	for _, entry := range p.Entries {
		if StageOf(entry.Row) == StageCategories {
			categories.Entries = append(categories.Entries, entry)
			continue
		}
		review.Entries = append(review.Entries, entry)
	}
	return categories, review
}
