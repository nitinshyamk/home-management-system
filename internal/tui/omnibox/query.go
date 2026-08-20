// Package omnibox is the one input line: search, filter, and later commands.
//
// One line rather than three, because they are the same gesture with different
// leaders -- and because the facet grammar, the fuzzy matching, and the resolve
// index behind them are shared. Three inputs would be three places for those to
// drift apart.
package omnibox

import (
	"strings"
)

// Query is a search line taken apart: the fields, and the rest.
//
// A token containing ':' is a facet; everything else is fuzzy text. That rule is
// the whole grammar, and it is deliberately one rule -- a search line a person
// has to think about is a search line they stop using.
type Query struct {
	Facets []Facet
	Text   string
}

// Facet is one field restriction.
type Facet struct {
	Key   string
	Value string
}

// Any reports whether the facet clears rather than restricts. A bare `*` is how
// you take one back off without retyping the line.
func (f Facet) Any() bool { return f.Value == "*" }

// Empty reports a query that restricts nothing.
func (q Query) Empty() bool { return len(q.Facets) == 0 && q.Text == "" }

// Facet returns the last value given for a key, since retyping a field should
// replace it rather than intersect with itself.
func (q Query) Facet(key string) (string, bool) {
	value, found := "", false
	for _, f := range q.Facets {
		if strings.EqualFold(f.Key, key) {
			value, found = f.Value, true
		}
	}
	return value, found
}

// ParseQuery splits a search line into facets and text.
//
// Quoting is supported for values with spaces -- loc:"left pantry" -- because
// half the locations in a house have two words in them and a grammar that
// cannot express them is a grammar for a different house.
func ParseQuery(line string) Query {
	var q Query
	var text []string

	for _, token := range tokenise(line) {
		key, value, hasColon := strings.Cut(token.text, ":")
		// A facet is a colon in an UNQUOTED key. Where the quote starts is what
		// separates loc:"left pantry" -- a field whose value has a space -- from
		// "12:30", which is a time somebody typed and not a field called 12.
		if hasColon && key != "" && (token.quotedFrom < 0 || len([]rune(key)) < token.quotedFrom) {
			q.Facets = append(q.Facets, Facet{Key: strings.ToLower(key), Value: value})
			continue
		}
		text = append(text, token.text)
	}
	q.Text = strings.Join(text, " ")
	return q
}

type token struct {
	text string
	// quotedFrom is the rune offset where this token's first quote opened, or
	// -1 when it has none. Whether a token is quoted is not enough: what
	// matters is whether the quote came before or after the colon.
	quotedFrom int
}

// tokenise splits on spaces, keeping quoted runs together. A quote may open
// after a facet key, so loc:"left pantry" is one token whose colon still counts.
func tokenise(line string) []token {
	var (
		out        []token
		current    strings.Builder
		inWord     bool
		inQuote    bool
		quotedFrom = -1
		width      int
	)
	flush := func() {
		out = append(out, token{text: current.String(), quotedFrom: quotedFrom})
		current.Reset()
		inWord, quotedFrom, width = false, -1, 0
	}
	for _, r := range line {
		switch {
		case inQuote && r == '"':
			inQuote = false
		case inQuote:
			current.WriteRune(r)
			width++
		case r == '"':
			inQuote = true
			inWord = true
			if quotedFrom < 0 {
				quotedFrom = width
			}
		case r == ' ' || r == '\t':
			if inWord {
				flush()
			}
		default:
			inWord = true
			current.WriteRune(r)
			width++
		}
	}
	if inWord {
		flush()
	}
	return out
}
