package complete_test

import (
	"strings"
	"testing"

	"home-management-system/internal/tui/complete"
)

func matches(paths ...string) []complete.Match {
	out := make([]complete.Match, 0, len(paths))
	for _, p := range paths {
		segments := strings.Split(p, " > ")
		out = append(out, complete.Match{Path: p, Leaf: segments[len(segments)-1]})
	}
	return out
}

// The tiers, in order. The old completer had none: it took the first three
// subsequence matches in tree order, so `gar` could offer two things merely
// containing g-a-r before the Garage.
func TestBetterMatchesComeFirst(t *testing.T) {
	house := matches(
		"Great Aunt's Room",   // subsequence only: g...a...r
		"Garage > Blue Crate", // the PATH starts with gar; the leaf does not
		"Garage",              // the leaf does
		"Garden",              // so does this
	)
	got := complete.Options(house, "gar")
	want := []string{"Garage", "Garden", "Garage > Blue Crate", "Great Aunt's Room"}
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Fatalf("Options(gar) = %v, want %v", got, want)
		}
	}
}

// The LEAF outranks the path. Somebody typing "shelf" means the shelf, not the
// house that happens to contain one.
func TestTheLeafOutranksThePath(t *testing.T) {
	got := complete.Options(matches("Shelf Room > Cupboard", "Kitchen > Shelf 1"), "shelf")
	if len(got) == 0 || got[0] != "Kitchen > Shelf 1" {
		t.Errorf("Options(shelf) = %v, want the Shelf first", got)
	}
}

// Arriving at an empty field offers what the field accepts.
//
// The old rule returned nothing for empty input -- right when the list only
// appeared mid-typing, wrong now that arriving at a field is meant to show one.
func TestAnEmptyFieldOffersTheVocabulary(t *testing.T) {
	got := complete.Options(matches("g", "kg", "ml"), "")
	if len(got) != 3 {
		t.Errorf("an empty field offered %v, want all three units", got)
	}
	if got := complete.Options(matches("  "), " "); len(got) != 1 {
		t.Errorf("whitespace was not treated as empty: %v", got)
	}
}

// A value that is already exactly right has nothing to offer.
//
// The old guard sat AFTER the cap, so three earlier fuzzy matches hid it and
// the field kept saying "take it" about a value that was already taken.
func TestNothingToOfferWhenItIsAlreadyExact(t *testing.T) {
	if got := complete.Options(matches("Garage"), "garage"); got != nil {
		t.Errorf("an exact and only match still offered %v", got)
	}
	// But an exact match with company still lists, because there is a choice.
	got := complete.Options(matches("Garage", "Garage Shelf"), "garage")
	if len(got) != 2 || got[0] != "Garage" {
		t.Errorf("Options = %v, want the exact match first and the other kept", got)
	}
}

// The cap is the cap.
func TestTheListIsCapped(t *testing.T) {
	var many []string
	for i := 0; i < complete.Limit*3; i++ {
		many = append(many, "Shelf")
	}
	if got := complete.Options(matches(many...), "sh"); len(got) != complete.Limit {
		t.Errorf("offered %d options, want %d", len(got), complete.Limit)
	}
	if got := complete.Options(matches(many...), ""); len(got) != complete.Limit {
		t.Errorf("an empty field offered %d, want %d", len(got), complete.Limit)
	}
}

// A space in what is typed is not a character to find: it stands in for the
// path separator, so "kit sh" reaches "Kitchen > Shelf 1".
func TestSpacesSkipTheSeparator(t *testing.T) {
	if got := complete.Options(matches("Kitchen > Shelf 1"), "kit sh"); len(got) != 1 {
		t.Errorf("Options(kit sh) = %v, want the shelf", got)
	}
}

// Within one tier the incoming order is kept, which is tree order. Sorting
// inside a tier would need a tie-break, and any tie-break is a second ranking
// rule nobody asked for.
func TestEqualMatchesKeepTreeOrder(t *testing.T) {
	got := complete.Options(matches("Garden", "Garage", "Gate"), "ga")
	want := []string{"Garden", "Garage", "Gate"}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("Options(ga) = %v, want %v", got, want)
		}
	}
}

func TestNoMatchOffersNothing(t *testing.T) {
	if got := complete.Options(matches("Garage", "Kitchen"), "zzz"); len(got) != 0 {
		t.Errorf("Options(zzz) = %v", got)
	}
}

// A warning is read, not chosen, so it only names things whose NAME matched.
//
// Options would offer "USB-C to HDMI Cable 2m" for "cum" -- c...u...m is in
// there, in order -- which is true, irrelevant, and reads as a bug when the
// screen says it already exists.
func TestAWarningOnlyNamesRealNearMisses(t *testing.T) {
	items := matches(
		"Electronics > Cables > USB-C to HDMI Cable 2m", // subsequence only
		"Spices > Cumin",
		"Spices > Ground Cumin",
	)
	got := complete.Near(items, "cum")
	want := []string{"Spices > Cumin", "Spices > Ground Cumin"}
	if len(got) != len(want) {
		t.Fatalf("Near(cum) = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("Near(cum) = %v, want %v", got, want)
		}
	}
}

// A path match is not a name match either: everything in the Spices category
// would otherwise warn about everything else in it.
func TestAWarningIgnoresTheCategoryPath(t *testing.T) {
	if got := complete.Near(matches("Spices > Cumin"), "spices"); len(got) != 0 {
		t.Errorf("Near(spices) = %v, want nothing -- no item is called that", got)
	}
}

// An empty field warns about nothing. Everything exists; that is not news.
func TestAnEmptyFieldWarnsAboutNothing(t *testing.T) {
	if got := complete.Near(matches("Cumin", "Turmeric"), ""); len(got) != 0 {
		t.Errorf("Near(\"\") = %v", got)
	}
}

func TestWarningsAreCapped(t *testing.T) {
	var many []string
	for i := 0; i < complete.NearLimit*3; i++ {
		many = append(many, "Cumin")
	}
	if got := complete.Near(matches(many...), "cum"); len(got) != complete.NearLimit {
		t.Errorf("named %d existing things, want %d", len(got), complete.NearLimit)
	}
}
