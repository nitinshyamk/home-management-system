package complete

import "strings"

// Limit is how many matches a dropdown holds.
//
// Not how many it SHOWS -- that is Window, and the difference is the whole
// point. A cap sized to the screen is a cap that decides which matches exist
// based on how tall the list is allowed to be, and in a house with thirty
// places the ninth match was unreachable by any keystroke.
//
// Thirty, which is five windows of scrolling: enough that the answer is in the
// list for any household this program is for, and few enough that C-n is a
// choice rather than a journey. Past it the advice is to type another letter,
// which is what the list says.
const Limit = 30

// Match is one candidate to complete against.
//
// Path is the full hierarchical label ("Kitchen > Spice Cabinet"); Leaf is its
// last segment. Both are matched, and which one matched decides the rank -- a
// person typing "shelf" means the shelf, not the house that contains one.
type Match struct {
	Path string
	Leaf string
}

// tiers, best first. The distinction is the whole reason this is not a filter:
// the old completer had none, so `gar` offered whatever the tree walk reached
// first and the Garage could come third behind two things merely containing
// those letters in order.
const (
	tierExact = iota
	tierLeafPrefix
	tierPathPrefix
	tierLeafInfix
	tierSubsequence
	tierNone
)

// Rank scores one candidate against typed text.
func rank(m Match, typed string) int {
	path, leaf := strings.ToLower(m.Path), strings.ToLower(m.Leaf)
	switch {
	case path == typed || leaf == typed:
		return tierExact
	case strings.HasPrefix(leaf, typed):
		return tierLeafPrefix
	case strings.HasPrefix(path, typed):
		return tierPathPrefix
	case strings.Contains(leaf, typed):
		return tierLeafInfix
	case subsequence(path, typed):
		return tierSubsequence
	}
	return tierNone
}

// Options ranks candidates against what has been typed and returns what to
// offer, best first, capped at Limit.
//
// Empty input offers the top of the hierarchy rather than nothing. The old rule
// was the opposite -- "a list of everything is not a suggestion, it is the tree,
// which is one keystroke away" -- and it was right when the list only appeared
// mid-typing. It is wrong now that arriving at a field is meant to show what the
// field accepts.
func Options(candidates []Match, typed string) []string {
	typed = strings.ToLower(strings.TrimSpace(typed))
	if typed == "" {
		return shallowest(candidates)
	}

	// Bucketed rather than sorted, so candidates keep their incoming order --
	// which is tree order -- within a tier. A sort would need a tie-break, and
	// any tie-break here is a second ranking rule nobody asked for.
	buckets := make([][]string, tierNone)
	for _, c := range candidates {
		if tier := rank(c, typed); tier < tierNone {
			buckets[tier] = append(buckets[tier], c.Path)
		}
	}

	// Exactly one candidate, and it is what is already typed: there is nothing
	// to offer. A list saying "take it" when taking it changes nothing is a
	// list that lies, and the old guard for this sat after the cap -- so three
	// earlier fuzzy matches hid it and the field kept offering completions for
	// a value that was already right.
	if len(buckets[tierExact]) == 1 && total(buckets) == 1 {
		return nil
	}

	out := make([]string, 0, Limit)
	for _, bucket := range buckets {
		for _, path := range bucket {
			if out = append(out, path); len(out) == Limit {
				return out
			}
		}
	}
	return out
}

// shallowest is what an empty field offers: the top of the hierarchy first.
//
// Tree order alone put the first room's whole subtree at the top, so arriving
// at the prompt in a five-room house showed the Basement and its five shelves
// and nothing else -- a list that answers "what is under Basement" to somebody
// who has not said Basement. Depth first answers "what is there", which is the
// question an empty field is asking.
//
// Within a depth the incoming order is kept, which is tree order, for the same
// reason the tiers keep it.
func shallowest(candidates []Match) []string {
	var byDepth [][]string
	for _, c := range candidates {
		d := strings.Count(c.Path, ">")
		for len(byDepth) <= d {
			byDepth = append(byDepth, nil)
		}
		byDepth[d] = append(byDepth[d], c.Path)
	}
	out := make([]string, 0, Limit)
	for _, level := range byDepth {
		for _, path := range level {
			if out = append(out, path); len(out) == Limit {
				return out
			}
		}
	}
	return out
}

// NearLimit is how many existing things a warning names.
//
// Fewer than a dropdown offers, because a warning is read rather than chosen.
const NearLimit = 3

// Near is the strict cousin of Options, for warnings.
//
// Only the tiers where the NAME itself matched -- exact, or the leaf starting
// with or containing what was typed. A subsequence match is fine for a list
// somebody is choosing from and useless as a warning: typing "Cum" would
// otherwise announce that "USB-C to HDMI Cable 2m" already exists, which is
// true, irrelevant, and reads as a bug.
func Near(candidates []Match, typed string) []string {
	typed = strings.ToLower(strings.TrimSpace(typed))
	if typed == "" {
		return nil
	}
	buckets := make([][]string, tierLeafInfix+1)
	for _, c := range candidates {
		if tier := rank(c, typed); tier <= tierLeafInfix && tier != tierPathPrefix {
			buckets[tier] = append(buckets[tier], c.Path)
		}
	}
	out := make([]string, 0, NearLimit)
	for _, bucket := range buckets {
		for _, path := range bucket {
			if out = append(out, path); len(out) == NearLimit {
				return out
			}
		}
	}
	return out
}

func total(buckets [][]string) int {
	n := 0
	for _, b := range buckets {
		n += len(b)
	}
	return n
}

// subsequence reports whether every character of needle appears in haystack, in
// order. Spaces in the needle are skipped so a path separator need not be typed.
func subsequence(haystack, needle string) bool {
	at := 0
	for _, r := range needle {
		if r == ' ' {
			continue
		}
		i := strings.IndexRune(haystack[at:], r)
		if i < 0 {
			return false
		}
		at += i + 1
	}
	return true
}
