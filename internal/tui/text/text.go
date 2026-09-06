// Package text is the small vocabulary of presentation every surface needs and
// none of them owns.
//
// Each of these existed two or three times before this package. StripANSI was
// byte-for-byte identical in the table and in the golden-frame harness, and the
// same escape scanner a third time as a width count. Article existed twice and
// the two DISAGREED -- one preserved the caller's case and tested "AEIOU", the
// other lowercased and tested "aeiou" -- so the same refusal read differently
// depending on which screen refused you.
package text

import "strings"

// Article prefixes "a" or "an" to a noun, so a sentence about a kind reads as
// one: "an Item", "a holding".
//
// It chooses the article and NOTHING else. The two implementations this
// replaces each also decided the noun's case, and decided it differently --
// which is why the help said "names an Item" while a refusal said "an item",
// and why neither could be used where the other was. Case belongs to the
// caller, who knows whether the word is starting a sentence or sitting in the
// middle of one.
func Article(noun string) string {
	if noun == "" {
		return ""
	}
	if strings.ContainsAny(strings.ToLower(noun[:1]), "aeiou") {
		return "an " + noun
	}
	return "a " + noun
}

// StripANSI removes the escape sequences that carry colour, leaving what a
// reader actually sees. Golden frames are compared after this, so a change of
// styling is not a change of frame.
func StripANSI(s string) string {
	var b strings.Builder
	forEachVisible(s, func(r rune) { b.WriteRune(r) })
	return b.String()
}

// VisibleWidth counts printable columns. An escape sequence carries colour and
// occupies none.
func VisibleWidth(s string) int {
	n := 0
	forEachVisible(s, func(rune) { n++ })
	return n
}

// forEachVisible walks the runes that are actually drawn.
//
// One scanner rather than two, because the escape grammar is the thing worth
// getting right once: an escape runs until its first letter, and everything
// between is invisible.
func forEachVisible(s string, visit func(rune)) {
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
		default:
			visit(r)
		}
	}
}
