package command_test

import (
	"errors"
	"testing"
	"time"

	"home-management-system/internal/command"
	"home-management-system/internal/domain"
)

// The quantity grammar is pinned rather than merely tested, because these three
// spellings mean three different things and only one of them is on the receipt.
func TestTheThreeQuantitySpellingsAreDistinct(t *testing.T) {
	for _, tc := range []struct {
		text string
		want command.Amount
	}{
		{"100", command.Amount{Value: domain.FromMilli(100 * domain.Scale)}},
		{"100g", command.Amount{Value: domain.FromMilli(100 * domain.Scale), Unit: "g"}},
		{"100 g", command.Amount{Value: domain.FromMilli(100 * domain.Scale), Unit: "g"}},
		{"1.5kg", command.Amount{Value: domain.FromMilli(1500), Unit: "kg"}},
		{"2bag", command.Amount{Value: domain.FromMilli(2 * domain.Scale), Packages: true}},
		{"2 pkg", command.Amount{Value: domain.FromMilli(2 * domain.Scale), Packages: true}},
		{"2packages", command.Amount{Value: domain.FromMilli(2 * domain.Scale), Packages: true}},
		{"  3 JARS ", command.Amount{Value: domain.FromMilli(3 * domain.Scale), Packages: true}},
	} {
		got, err := command.ParseAmount(tc.text)
		if err != nil {
			t.Errorf("%q: %v", tc.text, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q = %+v, want %+v", tc.text, got, tc.want)
		}
	}

	// The distinction that matters most: a bare number is not a number of grams,
	// and a number of grams is not a number of packages.
	bare, _ := command.ParseAmount("100")
	grams, _ := command.ParseAmount("100g")
	if bare == grams {
		t.Error("`100` and `100g` parsed to the same thing")
	}
	bags, _ := command.ParseAmount("2bag")
	two, _ := command.ParseAmount("2")
	if bags == two {
		t.Error("`2bag` and `2` parsed to the same thing")
	}
}

func TestAmountRejectsNonsense(t *testing.T) {
	for _, text := range []string{
		"",
		"   ",
		"g",       // a unit with no number
		"lots",    // no number at all
		"1.5bag",  // half a package is not a package
		"1.2345g", // more precision than Scale can hold
	} {
		if got, err := command.ParseAmount(text); !errors.Is(err, command.ErrValue) {
			t.Errorf("%q parsed to %+v (err %v), want ErrValue", text, got, err)
		}
	}
}

func TestParseDate(t *testing.T) {
	want := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	for _, text := range []string{"2027-03-01", "2027/03/01", "01/03/2027"} {
		got, err := command.ParseDate(text)
		if err != nil {
			t.Errorf("%q: %v", text, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("%q = %v, want %v", text, got, want)
		}
	}
	// A month alone is what a packet usually prints, and it means the first.
	if got, err := command.ParseDate("2027-03"); err != nil || !got.Equal(want) {
		t.Errorf("2027-03 = %v (%v), want %v", got, err, want)
	}
	// No time of day survives, because expires_on is part of H8's key.
	got, err := command.ParseDate("2027-03-01")
	if err != nil {
		t.Fatal(err)
	}
	if got.Hour() != 0 || got.Minute() != 0 || got.Location() != time.UTC {
		t.Errorf("%v carries a time of day", got)
	}
	if _, err := command.ParseDate("next tuesday"); !errors.Is(err, command.ErrValue) {
		t.Errorf("parsed a date I cannot actually read: %v", err)
	}
}

func TestParseMoney(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int64
	}{
		{"3", 300}, {"3.50", 350}, {"$3.50", 350}, {"£12.05", 1205}, {"1,234.00", 123400},
	} {
		got, err := command.ParseMoney(tc.text)
		if err != nil {
			t.Errorf("%q: %v", tc.text, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q = %d, want %d", tc.text, got, tc.want)
		}
	}
	// One decimal place is almost always a typo for two, and guessing which
	// costs a factor of ten.
	for _, text := range []string{"3.5", "3.500", "free", ""} {
		if _, err := command.ParseMoney(text); !errors.Is(err, command.ErrValue) {
			t.Errorf("%q parsed, want ErrValue", text)
		}
	}
}

func TestParseTheSmallVocabularies(t *testing.T) {
	if got, _ := command.ParseBool("yes"); !got {
		t.Error("yes is not true")
	}
	if got, _ := command.ParseBool("N"); got {
		t.Error("N is not false")
	}
	if _, err := command.ParseBool("maybe"); !errors.Is(err, command.ErrValue) {
		t.Error("maybe parsed as an answer")
	}
	if got, _ := command.ParseResolution("Lift"); got != domain.ResolutionLift {
		t.Errorf("Lift = %q", got)
	}
	if _, err := command.ParseResolution("delete"); !errors.Is(err, command.ErrValue) {
		t.Error("delete parsed as a resolution")
	}
	if got, _ := command.ParseCounting("measured"); got != command.CountingMeasured {
		t.Errorf("measured = %q", got)
	}
	// The word nobody reads is not the word anybody types.
	if _, err := command.ParseCounting("fungible"); err == nil {
		t.Error("fungible parsed as a preset")
	}
}
