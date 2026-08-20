package omnibox_test

import (
	"reflect"
	"testing"

	"home-management-system/internal/tui/omnibox"
)

// One rule is the whole grammar: a token containing ':' is a facet, everything
// else is fuzzy text. A search line a person has to think about is a search line
// they stop using.
func TestFacetsAndTextComposeInOneLine(t *testing.T) {
	for _, tc := range []struct {
		line   string
		facets []omnibox.Facet
		text   string
	}{
		{"rice", nil, "rice"},
		{"loc:kitchen", []omnibox.Facet{{Key: "loc", Value: "kitchen"}}, ""},
		{"loc:kitchen rice", []omnibox.Facet{{Key: "loc", Value: "kitchen"}}, "rice"},
		{"rice loc:kitchen", []omnibox.Facet{{Key: "loc", Value: "kitchen"}}, "rice"},
		{"loc:kitchen state:out rice", []omnibox.Facet{
			{Key: "loc", Value: "kitchen"}, {Key: "state", Value: "out"},
		}, "rice"},
		// Half the locations in a house have two words in them.
		{`loc:"left pantry" rice`, []omnibox.Facet{{Key: "loc", Value: "left pantry"}}, "rice"},
		// Quoted text is text even when it contains a colon.
		{`"12:30" rice`, nil, "12:30 rice"},
		// A bare colon is not a facet -- there is no field to restrict.
		{":rice", nil, ":rice"},
		{"", nil, ""},
	} {
		got := omnibox.ParseQuery(tc.line)
		if !reflect.DeepEqual(got.Facets, tc.facets) {
			t.Errorf("%q facets = %v, want %v", tc.line, got.Facets, tc.facets)
		}
		if got.Text != tc.text {
			t.Errorf("%q text = %q, want %q", tc.line, got.Text, tc.text)
		}
	}
}

// Retyping a field replaces it rather than intersecting with itself, which is
// what makes refining a filter feel like editing rather than accumulating.
func TestTheLastValueForAFieldWins(t *testing.T) {
	q := omnibox.ParseQuery("loc:kitchen loc:garage")
	if got, _ := q.Facet("loc"); got != "garage" {
		t.Errorf("loc = %q, want the last one given", got)
	}
	if _, found := q.Facet("nope"); found {
		t.Error("found a facet that was never given")
	}
}

// A bare * takes a field back off without retyping the line.
func TestAStarClearsAField(t *testing.T) {
	q := omnibox.ParseQuery("loc:* rice")
	if len(q.Facets) != 1 || !q.Facets[0].Any() {
		t.Errorf("facets = %v, want one that clears", q.Facets)
	}
	if q.Text != "rice" {
		t.Errorf("text = %q", q.Text)
	}
}

func TestEmpty(t *testing.T) {
	if !omnibox.ParseQuery("   ").Empty() {
		t.Error("whitespace is not an empty query")
	}
	if omnibox.ParseQuery("loc:*").Empty() {
		t.Error("a query with a facet is empty")
	}
}
