package text_test

import (
	"testing"

	"home-management-system/internal/tui/text"
)

// TestArticleLeavesTheNounAlone is the property that let one implementation
// replace two.
//
// The two it replaced each decided the noun's case as well as the article, and
// decided it differently -- so the help said "names an Item" and a refusal said
// "an item", and neither function could be used where the other was. Choosing
// the article is the whole job; the caller knows whether the word is starting a
// sentence.
func TestArticleLeavesTheNounAlone(t *testing.T) {
	cases := []struct{ noun, want string }{
		{"Item", "an Item"},
		{"item", "an item"},
		{"Holding", "a Holding"},
		{"holding", "a holding"},
		{"Category", "a Category"},
		{"Location", "a Location"},
		{"Unopened box", "an Unopened box"},
		{"", ""},
	}
	for _, c := range cases {
		if got := text.Article(c.noun); got != c.want {
			t.Errorf("Article(%q) = %q, want %q", c.noun, got, c.want)
		}
	}
}

// TestStripANSIAndVisibleWidthAgree: they are the same scanner, and the width
// is what the golden frames are compared on.
func TestStripANSIAndVisibleWidthAgree(t *testing.T) {
	cases := []struct {
		in    string
		plain string
	}{
		{"plain", "plain"},
		{"\x1b[1mbold\x1b[0m", "bold"},
		{"\x1b[38;5;203mred\x1b[0m tail", "red tail"},
		{"\x1b[1m\x1b[7mtwo\x1b[0m", "two"},
		{"", ""},
	}
	for _, c := range cases {
		if got := text.StripANSI(c.in); got != c.plain {
			t.Errorf("StripANSI(%q) = %q, want %q", c.in, got, c.plain)
		}
		if got, want := text.VisibleWidth(c.in), len([]rune(c.plain)); got != want {
			t.Errorf("VisibleWidth(%q) = %d, want %d", c.in, got, want)
		}
	}
}
