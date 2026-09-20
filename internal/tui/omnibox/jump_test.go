package omnibox_test

import (
	"strings"
	"testing"

	"home-management-system/internal/resolve"
	"home-management-system/internal/tui/omnibox"
)

func house() []resolve.Candidate {
	return []resolve.Candidate{
		{ID: 1, Kind: resolve.KindLocation, Path: "Garage > Metal Shelving Unit > Bay 3"},
		{ID: 2, Kind: resolve.KindItem, Path: "Spices > Ancho Chile"},
		{ID: 3, Kind: resolve.KindLocation, Path: "Left Pantry"},
		{ID: 4, Kind: resolve.KindItem, Path: "Spices > Cumin", Archived: true},
	}
}

func jumping(query string) omnibox.Model {
	m := omnibox.New().SetCandidates(house()).SetResultsSize(80, 12).Open(omnibox.Jump)
	return typeInto(m, query)
}

// The palette narrows as the query is typed, the way the filter narrows a list.
func TestTheJumpNarrowsAsYouType(t *testing.T) {
	if got := jumping("").Matches(); got != 3 {
		t.Errorf("an empty jump offers %d destinations, want every live one", got)
	}
	// Subsequence matching, the same rule the resolver uses -- so the palette
	// and the command line agree about what a name nearly is.
	m := jumping("bay3")
	if m.Matches() != 1 {
		t.Fatalf("%q left %d matches", "bay3", m.Matches())
	}
	if got := strings.Join(strings.Fields(strip(m.Results())), " "); !strings.Contains(got, "Bay 3") {
		t.Errorf("the palette does not show what it matched: %q", got)
	}
}

// What is archived is not a destination. Jumping to a thing you retired is
// going somewhere that is not there any more.
func TestTheJumpSkipsWhatIsArchived(t *testing.T) {
	if got := strip(jumping("cumin").Results()); strings.Contains(got, "Cumin") {
		t.Errorf("the palette offers an archived thing: %q", got)
	}
}

// The palette says what KIND each destination is, and never drops it: the same
// name can be a Location and part of a Holding's path, and a jump that does not
// say which lands somewhere surprising.
func TestTheJumpNamesTheKind(t *testing.T) {
	got := strip(jumping("").Results())
	for _, want := range []string{"KIND", "Location", "Item"} {
		if !strings.Contains(got, want) {
			t.Errorf("the palette does not say %q: %q", want, got)
		}
	}
}

// Chosen is where a jump would land, which is what the application asks for
// when the line is accepted.
func TestChosenIsTheRowUnderTheCursor(t *testing.T) {
	target, ok := jumping("ancho").Chosen()
	if !ok {
		t.Fatal("a jump with one match chose nothing")
	}
	if target.ID != 2 || target.Kind != resolve.KindItem {
		t.Errorf("chose %+v, want the Ancho Chile item", target)
	}

	// And nothing matching chooses nothing, rather than the wrong thing.
	if _, ok := jumping("zzzzz").Chosen(); ok {
		t.Error("a jump with no matches still chose something")
	}
}

// Closing the line does not throw the vocabulary away: the next jump opens
// against the same house without waiting for it to be loaded again.
func TestTheVocabularySurvivesCancelling(t *testing.T) {
	m := jumping("ancho").Cancel()
	if got := m.Open(omnibox.Jump).Matches(); got != 3 {
		t.Errorf("reopening the jump offers %d destinations, want every live one", got)
	}
}
