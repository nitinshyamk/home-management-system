package domain

import (
	"errors"
	"math"
	"testing"
)

func TestQuantityArithmeticIsExact(t *testing.T) {
	// The motivating case: 2 kg of rice, 100 g consumed twenty times, must land
	// exactly on zero. A float accumulator does not reliably do this, and H10
	// compares replayed state to stored state for equality.
	q := FromMilli(2_000_000)
	for i := 0; i < 20; i++ {
		var err error
		if q, err = q.Sub(FromMilli(100_000)); err != nil {
			t.Fatalf("subtract %d: %v", i, err)
		}
	}
	if !q.IsZero() {
		t.Errorf("after 20 x 100g from 2kg: %s, want exactly 0", q)
	}
}

func TestQuantityDetectsOverflow(t *testing.T) {
	max := FromMilli(math.MaxInt64)
	if _, err := max.Add(FromMilli(1)); !errors.Is(err, ErrOverflow) {
		t.Errorf("Add past MaxInt64: err = %v, want ErrOverflow", err)
	}

	min := FromMilli(math.MinInt64)
	if _, err := min.Sub(FromMilli(1)); !errors.Is(err, ErrOverflow) {
		t.Errorf("Sub past MinInt64: err = %v, want ErrOverflow", err)
	}
	if _, err := min.Neg(); !errors.Is(err, ErrOverflow) {
		t.Errorf("Neg(MinInt64): err = %v, want ErrOverflow", err)
	}
	if _, err := FromWhole(math.MaxInt64); !errors.Is(err, ErrOverflow) {
		t.Errorf("FromWhole(MaxInt64): err = %v, want ErrOverflow", err)
	}
	if _, err := FromMilli(math.MaxInt64 / 2).Mul(3); !errors.Is(err, ErrOverflow) {
		t.Errorf("Mul overflow: err = %v, want ErrOverflow", err)
	}
}

func TestQuantityStringAndParseRoundTrip(t *testing.T) {
	cases := []struct {
		milli int64
		text  string
	}{
		{0, "0"},
		{1, "0.001"},
		{100, "0.1"},
		{2_000, "2"},
		{2_500, "2.5"},
		{2_000_000, "2000"},
		{-100_000, "-100"},
		{-1, "-0.001"},
		{1_234, "1.234"},
	}
	for _, tc := range cases {
		q := FromMilli(tc.milli)
		if got := q.String(); got != tc.text {
			t.Errorf("FromMilli(%d).String() = %q, want %q", tc.milli, got, tc.text)
		}
		parsed, err := ParseQuantity(tc.text)
		if err != nil {
			t.Errorf("ParseQuantity(%q): %v", tc.text, err)
			continue
		}
		if parsed.Milli() != tc.milli {
			t.Errorf("ParseQuantity(%q) = %d milli, want %d", tc.text, parsed.Milli(), tc.milli)
		}
	}
}

// TestParseRejectsExcessPrecision: silently rounding is how exactness is lost,
// and the loss would surface much later as an unexplained integrity failure.
func TestParseRejectsExcessPrecision(t *testing.T) {
	if _, err := ParseQuantity("1.2345"); !errors.Is(err, ErrPrecision) {
		t.Errorf("err = %v, want ErrPrecision", err)
	}
	if _, err := ParseQuantity("1.234"); err != nil {
		t.Errorf("three decimals should parse: %v", err)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "  ", "abc", "1.2.3", "1a", "-", "1,000"} {
		if _, err := ParseQuantity(s); err == nil {
			t.Errorf("ParseQuantity(%q) accepted, want an error", s)
		}
	}
}

// ---------------------------------------------------------------------------
// Units
// ---------------------------------------------------------------------------

var (
	gram       = Unit{Code: "g", Dimension: DimensionMass, ToBaseFactor: 1}
	kilogram   = Unit{Code: "kg", Dimension: DimensionMass, ToBaseFactor: 1000}
	millilitre = Unit{Code: "ml", Dimension: DimensionVolume, ToBaseFactor: 1}
)

func TestConvertWithinDimension(t *testing.T) {
	// 2 kg expressed in grams.
	got, err := Convert(FromWholeOrFail(t, 2), kilogram, gram)
	if err != nil {
		t.Fatalf("kg to g: %v", err)
	}
	if want := FromWholeOrFail(t, 2000); got.Cmp(want) != 0 {
		t.Errorf("2 kg = %s g, want %s", got, want)
	}

	// And back.
	back, err := Convert(got, gram, kilogram)
	if err != nil {
		t.Fatalf("g to kg: %v", err)
	}
	if want := FromWholeOrFail(t, 2); back.Cmp(want) != 0 {
		t.Errorf("round trip = %s kg, want %s", back, want)
	}
}

// TestConvertRejectsCrossDimension is U1. Consuming 100 ml from a mass-tracked
// holding is a question with no answer, not a rounding problem.
func TestConvertRejectsCrossDimension(t *testing.T) {
	if _, err := Convert(FromMilli(1000), gram, millilitre); !errors.Is(err, ErrDimensionMismatch) {
		t.Errorf("err = %v, want ErrDimensionMismatch", err)
	}
}

// TestConvertRejectsInexactResults: rejecting beats truncating, because a lost
// thousandth reappears later as an integrity failure with no obvious cause.
func TestConvertRejectsInexactResults(t *testing.T) {
	// One milligram cannot be stated exactly in kilograms at three decimals.
	if _, err := Convert(FromMilli(1), gram, kilogram); !errors.Is(err, ErrInexactConversion) {
		t.Errorf("err = %v, want ErrInexactConversion", err)
	}
}

func FromWholeOrFail(t *testing.T, n int64) Quantity {
	t.Helper()
	q, err := FromWhole(n)
	if err != nil {
		t.Fatalf("FromWhole(%d): %v", n, err)
	}
	return q
}
