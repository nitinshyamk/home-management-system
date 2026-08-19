package resolve_test

import (
	"strings"
	"testing"

	"home-management-system/internal/resolve"
)

// sample is a small hand-built index. It is written out rather than seeded
// through a database because the resolver is pure over its candidates, and a
// test that has to create twelve rows to check a tie is a test nobody reads.
func sample() *resolve.Index {
	return resolve.NewIndex([]resolve.Candidate{
		loc(1, "Kitchen"),
		loc(2, "Kitchen > Left Pantry"),
		loc(3, "Kitchen > Left Pantry > Shelf 1"),
		loc(4, "Kitchen > Left Pantry > Shelf 2"),
		loc(5, "Garage"),
		loc(6, "Garage > Shelf 1"),
		archived(loc(7, "Garage > Old Shelf")),
		item(10, "Food > Spices > Turmeric"),
		item(11, "Food > Spices > Cumin"),
		item(12, "Food > Grains > Basmati Rice"),
		item(13, "Food > Grains > Brown Rice"),
	})
}

func candidate(k resolve.Kind, id int64, path string) resolve.Candidate {
	segments := strings.Split(path, resolve.PathSeparator)
	return resolve.Candidate{Kind: k, ID: id, Path: path, Leaf: segments[len(segments)-1]}
}

func loc(id int64, path string) resolve.Candidate {
	return candidate(resolve.KindLocation, id, path)
}

func item(id int64, path string) resolve.Candidate {
	return candidate(resolve.KindItem, id, path)
}

func archived(c resolve.Candidate) resolve.Candidate {
	c.Archived = true
	return c
}

// wantExact and friends keep the assertions about the outcome rather than about
// the type switch, since every test here is one of four shapes.
func wantExact(t *testing.T, got resolve.Outcome, path string) {
	t.Helper()
	e, ok := got.(resolve.Exact)
	if !ok {
		t.Fatalf("got %T (%v), want Exact %q", got, got, path)
	}
	if e.Candidate.Path != path {
		t.Errorf("resolved to %q, want %q", e.Candidate.Path, path)
	}
}

func wantSuggested(t *testing.T, got resolve.Outcome, path string) {
	t.Helper()
	s, ok := got.(resolve.Suggested)
	if !ok {
		t.Fatalf("got %T (%v), want Suggested %q", got, got, path)
	}
	if s.Candidate.Path != path {
		t.Errorf("suggested %q, want %q", s.Candidate.Path, path)
	}
}

func wantAmbiguous(t *testing.T, got resolve.Outcome, paths ...string) {
	t.Helper()
	a, ok := got.(resolve.Ambiguous)
	if !ok {
		t.Fatalf("got %T (%v), want Ambiguous over %v", got, got, paths)
	}
	seen := map[string]bool{}
	for _, c := range a.Candidates {
		seen[c.Path] = true
	}
	for _, p := range paths {
		if !seen[p] {
			t.Errorf("Ambiguous is missing %q; it offered %v", p, a.Candidates)
		}
	}
}

func wantMissing(t *testing.T, got resolve.Outcome) {
	t.Helper()
	if _, ok := got.(resolve.Missing); !ok {
		t.Fatalf("got %T (%v), want Missing", got, got)
	}
}

// A literal name is never a guess, even though the fuzzy matcher would happily
// score it against a dozen other rows.
func TestLiteralLeafIsExact(t *testing.T) {
	wantExact(t, sample().Resolve("Turmeric"), "Food > Spices > Turmeric")
}

func TestLiteralPathIsExact(t *testing.T) {
	wantExact(t, sample().Resolve("Kitchen > Left Pantry > Shelf 1"), "Kitchen > Left Pantry > Shelf 1")
}

func TestLiteralMatchIgnoresCase(t *testing.T) {
	wantExact(t, sample().Resolve("  tUrMeRiC "), "Food > Spices > Turmeric")
}

// Two things may legally share a name (schema 3.10), so a literal match that
// hits twice is a real outcome rather than a data error.
func TestSharedLeafNameIsAmbiguous(t *testing.T) {
	wantAmbiguous(t, sample().Resolve("Shelf 1"),
		"Kitchen > Left Pantry > Shelf 1", "Garage > Shelf 1")
}

// The two gates the plan names by example.
func TestAbbreviationFindsThePath(t *testing.T) {
	wantSuggested(t, sample().Resolve("kitshelf1"), "Kitchen > Left Pantry > Shelf 1")
}

func TestReceiptTypoFindsTheItem(t *testing.T) {
	wantSuggested(t, sample().Resolve("Tumeric"), "Food > Spices > Turmeric")
}

// The whole point of the dominance gap: two rice items are a question, not a
// winner.
func TestNoDominantMatchIsAmbiguous(t *testing.T) {
	wantAmbiguous(t, sample().Resolve("rice"),
		"Food > Grains > Basmati Rice", "Food > Grains > Brown Rice")
}

func TestNothingCloseIsMissing(t *testing.T) {
	wantMissing(t, sample().Resolve("zzzzqqqq"))
}

func TestEmptyQueryIsMissing(t *testing.T) {
	wantMissing(t, sample().Resolve("   "))
}

// Archived candidates stay in the index so search can find them, and are
// excluded from resolution so nothing is filed into a place that is gone.
func TestArchivedIsNotResolvable(t *testing.T) {
	ix := sample()
	if got := ix.Resolve("Old Shelf"); !isMissingOrElsewhere(got, "Garage > Old Shelf") {
		t.Errorf("archived candidate resolved: %v", got)
	}
	var found bool
	for _, c := range ix.All() {
		if c.Path == "Garage > Old Shelf" {
			found = true
		}
	}
	if !found {
		t.Error("archived candidate was dropped from the index; search can no longer find it")
	}
}

func isMissingOrElsewhere(o resolve.Outcome, path string) bool {
	switch v := o.(type) {
	case resolve.Missing:
		return true
	case resolve.Exact:
		return v.Candidate.Path != path
	case resolve.Suggested:
		return v.Candidate.Path != path
	case resolve.Ambiguous:
		for _, c := range v.Candidates {
			if c.Path == path {
				return false
			}
		}
		return true
	}
	return false
}

// Kind filtering is what lets `at` mean a Location even when an Item is named
// the same thing.
func TestKindFilterExcludesOtherKinds(t *testing.T) {
	ix := sample()
	wantExact(t, ix.Resolve("Kitchen", resolve.KindLocation), "Kitchen")
	wantMissing(t, ix.Resolve("Kitchen", resolve.KindItem))
}

func TestKindFilterBreaksATie(t *testing.T) {
	ix := resolve.NewIndex([]resolve.Candidate{
		loc(1, "Pantry"),
		item(2, "Food > Pantry"),
	})
	wantAmbiguous(t, ix.Resolve("Pantry"), "Pantry", "Food > Pantry")
	wantExact(t, ix.Resolve("Pantry", resolve.KindLocation), "Pantry")
}

// Beyond a handful a person is not choosing, they are re-typing.
func TestAmbiguousIsCapped(t *testing.T) {
	var cs []resolve.Candidate
	for i := int64(0); i < 20; i++ {
		cs = append(cs, loc(i, "Shelf"))
	}
	got, ok := resolve.NewIndex(cs).Resolve("Shelf").(resolve.Ambiguous)
	if !ok {
		t.Fatalf("got %T, want Ambiguous", got)
	}
	if len(got.Candidates) != resolve.DefaultTuning.MaxCandidates {
		t.Errorf("offered %d candidates, want the cap of %d",
			len(got.Candidates), resolve.DefaultTuning.MaxCandidates)
	}
}

// The last tier's gap turns confident letter-matches into questions. This is
// the knob the corpus sweeps, so it is worth pinning that it does something
// monotone -- and pinning it on a query that actually reaches that tier, since
// the tiers above it ignore the gap entirely.
func TestAWiderGapAsksMoreOften(t *testing.T) {
	ix := resolve.NewIndex([]resolve.Candidate{
		loc(1, "Kitchen > Left Pantry > Shelf 1"),
		loc(2, "Kitchen > Spice Cabinet > Shelf 1"),
	})
	narrow := ix.ResolveWith(resolve.Tuning{LetterMatchGap: 0, MaxCandidates: 5}, "kitshelf1")
	if _, ok := narrow.(resolve.Suggested); !ok {
		t.Fatalf("with no dominance requirement, got %T, want Suggested", narrow)
	}
	wide := ix.ResolveWith(resolve.Tuning{LetterMatchGap: 100000, MaxCandidates: 5}, "kitshelf1")
	if _, ok := wide.(resolve.Ambiguous); !ok {
		t.Fatalf("with an unreachable dominance requirement, got %T, want Ambiguous", wide)
	}
}

// The tiers are ordered by how much evidence they carry, and a stronger tier
// must shut out a weaker one entirely. Without that, "rice" ranks "Ancho Chile"
// above "Brown Rice", which is what a single fuzzy pass over the whole index
// actually does.
func TestAStrongerTierShutsOutAWeakerOne(t *testing.T) {
	ix := resolve.NewIndex([]resolve.Candidate{
		item(1, "Food > Grains > Brown Rice"),
		item(2, "Food > Spices > Dried Peppers > Ancho Chile"),
	})
	got, ok := ix.Resolve("rice").(resolve.Suggested)
	if !ok {
		t.Fatalf("got %T, want Suggested", ix.Resolve("rice"))
	}
	if got.Candidate.Path != "Food > Grains > Brown Rice" {
		t.Errorf("suggested %q, want the item whose name contains it", got.Candidate.Path)
	}
	if got.Basis != resolve.ByName {
		t.Errorf("offered on basis %q, want %q", got.Basis, resolve.ByName)
	}
}
