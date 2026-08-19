package resolve

import (
	"strings"

	"github.com/sahilm/fuzzy"
)

// The four outcomes, which ARE the plan screen's row states.
//
// The resolver never decides. It reports what it found and how confident that
// is, and the caller renders it: Exact is ready, Suggested needs one keystroke,
// Ambiguous and Missing block. Nothing downstream re-derives this, so the UI
// and the bulk importer cannot disagree about what a name meant.
type Outcome interface{ isOutcome() }

// Exact is a literal match on a name or a full path. No judgement involved.
type Exact struct{ Candidate Candidate }

// Suggested is one clear winner. It is offered, never applied.
type Suggested struct {
	Candidate Candidate
	// Basis is why it was offered. It is deliberately not a score: see Basis.
	Basis Basis
}

// Ambiguous is several plausible matches with no dominant one. Deliberately
// easy to reach: a wrong Suggested is far worse than an Ambiguous, because
// Ambiguous stops and asks while a wrong Suggested quietly files the receipt
// against the wrong thing.
type Ambiguous struct{ Candidates []Candidate }

// Missing is nothing close enough to offer.
type Missing struct{ Query string }

func (Exact) isOutcome()     {}
func (Suggested) isOutcome() {}
func (Ambiguous) isOutcome() {}
func (Missing) isOutcome()   {}

// Basis says how a suggestion was arrived at, and it is what the UI should show
// beside one.
//
// There is deliberately no confidence percentage. The underlying scores are
// unnormalised and are not comparable between candidates -- measured, not
// assumed: matching "kitshelf1" scores "Kitchen > Left Pantry > Shelf 1" at
// 1848 and the equally plausible "Kitchen > Spice Cabinet > Shelf 1" at 631,
// purely because an adjacency bonus compounds over a longer run. Rendering
// either as a percentage would fabricate a precision that is not there. Which
// tier matched is a real signal; the number is not.
type Basis string

const (
	// ByName means the query is literally inside the thing's own name.
	ByName Basis = "name contains"
	// ByPath means it is inside the fuller label -- a category or an ancestor.
	ByPath Basis = "path contains"
	// ByLetters means only that the query's letters appear in order. This is
	// the weakest basis and the one that catches abbreviations and typos.
	ByLetters Basis = "letters match"
)

// Tuning holds the judgements the resolver cannot make for itself.
type Tuning struct {
	// LetterMatchGap is how far the best letter-match score must exceed the
	// runner-up before the best is offered rather than asked about.
	//
	// It is an absolute difference in an unnormalised score, which is a weak
	// instrument -- see Basis. It is used only at the last tier, where nothing
	// better is available, and the corpus picks a value deliberately far above
	// what a "close call" looks like, because at that tier asking is cheap and
	// being wrong is not.
	LetterMatchGap int

	// MaxCandidates caps how many an Ambiguous carries. Beyond a handful a
	// person is not choosing, they are re-typing.
	MaxCandidates int
}

// DefaultTuning is the setting chosen from the corpus in corpus_test.go.
var DefaultTuning = Tuning{LetterMatchGap: 2000, MaxCandidates: 5}

// Resolve maps a written name to an outcome, considering only the given kinds.
//
// Archived candidates are excluded: they remain in the index so search can find
// them, but nothing should be filed into a place that has been put away.
func (ix *Index) Resolve(text string, kinds ...Kind) Outcome {
	return ix.ResolveWith(DefaultTuning, text, kinds...)
}

// ResolveWith is Resolve against explicit tuning, which is what the corpus
// harness sweeps.
//
// Matching runs in tiers, strongest first, and stops at the first tier that
// matches anything. The tiers exist because a single fuzzy ranking over the
// whole index is not good enough to trust: matching "rice" that way ranks
// "Ancho Chile" (-29) above "Brown Rice" (-32), since subsequence matching will
// happily scatter four letters across a path. Asking "is the query actually
// inside this name?" first is both stronger evidence and cheaper.
func (ix *Index) ResolveWith(t Tuning, text string, kinds ...Kind) Outcome {
	query := fold(text)
	if query == "" {
		return Missing{Query: text}
	}
	v := ix.subset(kinds)

	// 1. Written out in full, as a name or as a whole path. Never a guess.
	if cs := v.where(func(i int) bool {
		return ix.paths[i] == query || ix.leaves[i] == query
	}); len(cs) > 0 {
		if len(cs) == 1 {
			return Exact{Candidate: cs[0]}
		}
		// The same name in two places is legal (schema 3.10), so this is a real
		// outcome rather than a data error.
		return Ambiguous{Candidates: limit(cs, t.MaxCandidates)}
	}

	// 2. Inside the thing's own name: "basmati", "detergent", "crisper".
	if o, ok := offer(v.where(func(i int) bool {
		return strings.Contains(ix.leaves[i], query)
	}), ByName, t); ok {
		return o
	}

	// 3. Inside the fuller label, which is how a category or an ancestor
	// location gets named: "oils" for "Oils and Vinegars".
	if o, ok := offer(v.where(func(i int) bool {
		return strings.Contains(ix.paths[i], query)
	}), ByPath, t); ok {
		return o
	}

	// 4. The letters, in order. Abbreviations and receipt typos land here, and
	// so does a great deal of noise, which is why this tier demands dominance
	// before it will offer anything.
	matches := fuzzy.FindFrom(query, v)
	if len(matches) == 0 {
		return Missing{Query: text}
	}
	best := matches[0]
	if len(matches) > 1 && best.Score-matches[1].Score < t.LetterMatchGap {
		var tied []Candidate
		for _, m := range matches {
			if best.Score-m.Score >= t.LetterMatchGap {
				break
			}
			tied = append(tied, ix.candidates[v.index[m.Index]])
		}
		return Ambiguous{Candidates: limit(tied, t.MaxCandidates)}
	}
	return Suggested{Candidate: ix.candidates[v.index[best.Index]], Basis: ByLetters}
}

// offer turns a tier's matches into an outcome, or reports that the tier found
// nothing and the next one should run.
func offer(cs []Candidate, b Basis, t Tuning) (Outcome, bool) {
	switch len(cs) {
	case 0:
		return nil, false
	case 1:
		return Suggested{Candidate: cs[0], Basis: b}, true
	default:
		return Ambiguous{Candidates: limit(cs, t.MaxCandidates)}, true
	}
}

func fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func limit(cs []Candidate, n int) []Candidate {
	if len(cs) > n {
		return cs[:n]
	}
	return cs
}

func wanted(c Candidate, kinds []Kind) bool {
	if c.Archived {
		return false
	}
	if len(kinds) == 0 {
		return true
	}
	for _, k := range kinds {
		if c.Kind == k {
			return true
		}
	}
	return false
}

// view is the index restricted to the kinds a caller asked for. It doubles as
// the fuzzy.Source for tier 4, keeping a back-index so a match can name the
// candidate it came from.
type view struct {
	ix    *Index
	index []int    // positions in ix.candidates
	rows  []string // ix.paths[index[j]], for fuzzy
}

func (ix *Index) subset(kinds []Kind) view {
	v := view{ix: ix}
	for i, c := range ix.candidates {
		if !wanted(c, kinds) {
			continue
		}
		v.index = append(v.index, i)
		v.rows = append(v.rows, ix.paths[i])
	}
	return v
}

// where collects the candidates in the view satisfying pred, which is given the
// position in the whole index so it can read the folded forms.
func (v view) where(pred func(i int) bool) []Candidate {
	var out []Candidate
	for _, i := range v.index {
		if pred(i) {
			out = append(out, v.ix.candidates[i])
		}
	}
	return out
}

func (v view) String(i int) string { return v.rows[i] }
func (v view) Len() int            { return len(v.rows) }
