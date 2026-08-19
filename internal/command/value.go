package command

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"home-management-system/internal/domain"
)

// The value parsers, shared by every surface.
//
// This is where "one contract" is either true or false. A `c` keystroke prompts
// for a quantity and that text comes here; so does a CSV cell and so does a `:`
// line. What the keystroke path skips is looking up something it already has --
// never the parsing, because `100` versus `100g` versus `2bag` must mean the
// same thing wherever it is written.

// ErrValue reports text that cannot be a value of the kind asked for.
var ErrValue = errors.New("command: cannot read value")

// Amount is a quantity as WRITTEN, before it is known what it means.
//
// It is deliberately not a domain.Quantity: `100` and `100g` and `2bag` are
// three different claims, and only two of them can be resolved without knowing
// the Item. Collapsing them at parse time is how a receipt ends up meaning
// something it did not say.
type Amount struct {
	// Value is the magnitude, exactly as written.
	Value domain.Quantity
	// Unit is the named unit, or "" for "the item's own content unit". Empty is
	// NOT a default to be guessed at later -- it is a distinct claim, and the
	// grammar is pinned so that it stays one.
	Unit domain.UnitCode
	// Packages means the bag/pkg/package keyword was used, selecting whole
	// packages as the basis. Those are keywords, not units: they choose how the
	// Holding counts rather than what it measures.
	Packages bool
}

func (a Amount) String() string {
	switch {
	case a.Packages:
		return a.Value.String() + " packages"
	case a.Unit != "":
		return a.Value.String() + " " + string(a.Unit)
	}
	return a.Value.String()
}

// packageKeywords select the Package basis. They are not units and never appear
// in the units table, which is why H7 -- an item with no package size cannot be
// counted in packages -- surfaces at this boundary rather than at the schema.
var packageKeywords = map[string]bool{
	"bag": true, "bags": true,
	"pkg": true, "pkgs": true,
	"package": true, "packages": true,
	"box": true, "boxes": true,
	"jar": true, "jars": true,
	"tin": true, "tins": true,
	"can": true, "cans": true,
	"bottle": true, "bottles": true,
}

// ParseAmount reads the quantity grammar.
//
//	100g, 100 g, 1.5kg   that amount in a named unit
//	100                  100 of the item's own content unit -- no guessing
//	2bag, 2 pkg          2 WHOLE packages
func ParseAmount(text string) (Amount, error) {
	s := strings.TrimSpace(text)
	if s == "" {
		return Amount{}, fmt.Errorf("%w: a quantity is required", ErrValue)
	}

	// Split at the first character that cannot be part of a number. A space is
	// optional, so "100g" and "100 g" are the same thing written twice.
	cut := len(s)
	for i, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '+' {
			continue
		}
		cut = i
		break
	}
	number, suffix := strings.TrimSpace(s[:cut]), strings.ToLower(strings.TrimSpace(s[cut:]))
	if number == "" {
		return Amount{}, fmt.Errorf("%w: %q has no number in it", ErrValue, text)
	}

	value, err := domain.ParseQuantity(number)
	if err != nil {
		return Amount{}, fmt.Errorf("%w: %q: %v", ErrValue, text, err)
	}

	switch {
	case suffix == "":
		return Amount{Value: value}, nil
	case packageKeywords[suffix]:
		// Half a package is not a package. Whole ones or nothing, because the
		// basis means "how many of these sealed things", and the alternative is
		// a Holding whose quantity cannot be acted on.
		if value.Milli()%domain.Scale != 0 {
			return Amount{}, fmt.Errorf("%w: %q is not a whole number of packages", ErrValue, text)
		}
		return Amount{Value: value, Packages: true}, nil
	}
	return Amount{Value: value, Unit: domain.UnitCode(suffix)}, nil
}

// ParseDate reads a calendar date.
//
// Dates only, and several spellings of one, because a receipt writes them all
// three ways. There is no time of day: an expiry is what is printed on the
// packet, and expires_on is part of H8's key, so two spellings of one date
// would read as two Holdings.
func ParseDate(text string) (time.Time, error) {
	s := strings.TrimSpace(text)
	if s == "" {
		return time.Time{}, fmt.Errorf("%w: a date is required", ErrValue)
	}
	switch strings.ToLower(s) {
	case "today":
		now := time.Now().UTC()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	for _, layout := range []string{"2006-01-02", "2006-01", "2006/01/02", "02/01/2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: %q is not a date I recognise (try 2027-03-01)", ErrValue, text)
}

// ParseBool reads a yes/no answer, in the words a person actually writes.
func ParseBool(text string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "yes", "y", "true", "t", "1":
		return true, nil
	case "no", "n", "false", "f", "0":
		return false, nil
	}
	return false, fmt.Errorf("%w: %q is not yes or no", ErrValue, text)
}

// ParseMoney reads a price into minor units, so arithmetic on it is exact for
// the same reason quantities are integers.
func ParseMoney(text string) (int64, error) {
	s := strings.TrimSpace(text)
	s = strings.TrimLeft(s, "$£€")
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return 0, fmt.Errorf("%w: a price is required", ErrValue)
	}
	whole, frac, split := strings.Cut(s, ".")
	major, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q is not a price", ErrValue, text)
	}
	if !split {
		return major * 100, nil
	}
	if len(frac) != 2 {
		return 0, fmt.Errorf("%w: %q needs exactly two decimal places", ErrValue, text)
	}
	minor, err := strconv.ParseInt(frac, 10, 64)
	if err != nil || minor < 0 {
		return 0, fmt.Errorf("%w: %q is not a price", ErrValue, text)
	}
	if major < 0 {
		return major*100 - minor, nil
	}
	return major*100 + minor, nil
}

// ParseResolution reads what to do with a tree node's contents when it is
// archived. Required rather than defaulted: what happens to the contents is the
// caller's decision, and silently picking one is how things end up somewhere
// nobody chose.
func ParseResolution(text string) (domain.Resolution, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "lift":
		return domain.ResolutionLift, nil
	case "move":
		return domain.ResolutionMove, nil
	case "block":
		return domain.ResolutionBlock, nil
	}
	return "", fmt.Errorf("%w: %q is not lift, move, or block", ErrValue, text)
}

// ParseCounting reads the three presets. Never a kind: nobody reads the word
// "fungible".
func ParseCounting(text string) (Counting, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "unique", "one of a kind", "individual":
		return CountingUnique, nil
	case "pile", "count", "counted":
		return CountingPile, nil
	case "measured", "measure":
		return CountingMeasured, nil
	}
	return "", fmt.Errorf("%w: %q is not unique, pile, or measured", ErrValue, text)
}
