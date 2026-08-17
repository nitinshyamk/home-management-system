package domain

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// Scale is the number of milli-units in one whole unit. Quantities are exact
// integers in thousandths.
//
// Float is unusable here. H10 compares stored state against replayed state for
// *equality*, and a sum of floats does not reliably reproduce itself. Integer
// milli-units make replay exact by construction and map directly to SQLite
// INTEGER.
const Scale = 1000

// MaxDecimals is the precision Scale affords.
const MaxDecimals = 3

var (
	// ErrOverflow means an arithmetic result cannot be represented. Reachable in
	// practice only through corrupt data, which is exactly when silence is worst.
	ErrOverflow = errors.New("quantity overflow")

	// ErrPrecision means a value carries more precision than Scale can hold.
	ErrPrecision = errors.New("quantity precision exceeds three decimal places")
)

// Quantity is an exact signed amount in milli-units of some unit. The unit
// itself lives on the Item; a Quantity is a bare magnitude.
//
// The field is unexported so arithmetic must go through methods that check for
// overflow, and so a Quantity cannot be confused with a raw count.
type Quantity struct{ milli int64 }

// Zero is the additive identity and the value a Bulk projection starts from.
var Zero = Quantity{}

// FromMilli builds a Quantity from thousandths.
func FromMilli(m int64) Quantity { return Quantity{milli: m} }

// FromWhole builds a Quantity from whole units.
func FromWhole(n int64) (Quantity, error) {
	if n > math.MaxInt64/Scale || n < math.MinInt64/Scale {
		return Zero, fmt.Errorf("%w: %d whole units", ErrOverflow, n)
	}
	return Quantity{milli: n * Scale}, nil
}

// MustFromWhole is FromWhole for literals known to be in range. It panics
// otherwise, so it belongs in tests and constants, never in a request path.
func MustFromWhole(n int64) Quantity {
	q, err := FromWhole(n)
	if err != nil {
		panic(err)
	}
	return q
}

// Milli returns the underlying thousandths, for storage.
func (q Quantity) Milli() int64 { return q.milli }

// Add returns q+o, or ErrOverflow.
func (q Quantity) Add(o Quantity) (Quantity, error) {
	sum := q.milli + o.milli
	// Overflow occurred if the operands share a sign that the result does not.
	if (q.milli > 0 && o.milli > 0 && sum < 0) || (q.milli < 0 && o.milli < 0 && sum > 0) {
		return Zero, fmt.Errorf("%w: %s + %s", ErrOverflow, q, o)
	}
	return Quantity{milli: sum}, nil
}

// Sub returns q-o, or ErrOverflow.
func (q Quantity) Sub(o Quantity) (Quantity, error) {
	if o.milli == math.MinInt64 {
		return Zero, fmt.Errorf("%w: negating %s", ErrOverflow, o)
	}
	return q.Add(Quantity{milli: -o.milli})
}

// Mul scales q by an integer factor, or returns ErrOverflow.
func (q Quantity) Mul(n int64) (Quantity, error) {
	if q.milli == 0 || n == 0 {
		return Zero, nil
	}
	product := q.milli * n
	if product/n != q.milli {
		return Zero, fmt.Errorf("%w: %s * %d", ErrOverflow, q, n)
	}
	return Quantity{milli: product}, nil
}

// Neg returns -q.
func (q Quantity) Neg() (Quantity, error) {
	if q.milli == math.MinInt64 {
		return Zero, fmt.Errorf("%w: negating %s", ErrOverflow, q)
	}
	return Quantity{milli: -q.milli}, nil
}

func (q Quantity) IsZero() bool     { return q.milli == 0 }
func (q Quantity) IsNegative() bool { return q.milli < 0 }
func (q Quantity) IsPositive() bool { return q.milli > 0 }

// Cmp reports whether q is less than, equal to, or greater than o.
func (q Quantity) Cmp(o Quantity) int {
	switch {
	case q.milli < o.milli:
		return -1
	case q.milli > o.milli:
		return 1
	default:
		return 0
	}
}

// String renders the quantity in decimal, trimming trailing zeros: 2500 milli
// prints as "2.5", 2000 as "2".
func (q Quantity) String() string {
	neg := q.milli < 0
	m := q.milli
	if neg {
		// Guard MinInt64, whose absolute value is not representable.
		if m == math.MinInt64 {
			return "-9223372036854775.808"
		}
		m = -m
	}

	whole, frac := m/Scale, m%Scale
	out := fmt.Sprintf("%d", whole)
	if frac != 0 {
		out += strings.TrimRight(fmt.Sprintf(".%03d", frac), "0")
	}
	if neg {
		out = "-" + out
	}
	return out
}

// ParseQuantity reads a decimal string with at most three decimal places.
// It rejects excess precision rather than rounding: silently discarding
// precision is how exactness is lost.
func ParseQuantity(s string) (Quantity, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Zero, fmt.Errorf("parse quantity: empty")
	}

	neg := false
	switch s[0] {
	case '-':
		neg, s = true, s[1:]
	case '+':
		s = s[1:]
	}

	wholeStr, fracStr, hasFrac := strings.Cut(s, ".")
	if wholeStr == "" && !hasFrac {
		return Zero, fmt.Errorf("parse quantity %q: no digits", s)
	}
	if hasFrac && len(fracStr) > MaxDecimals {
		return Zero, fmt.Errorf("%w: %q", ErrPrecision, s)
	}

	var whole int64
	for _, r := range wholeStr {
		if r < '0' || r > '9' {
			return Zero, fmt.Errorf("parse quantity %q: unexpected %q", s, r)
		}
		if whole > (math.MaxInt64-int64(r-'0'))/10 {
			return Zero, fmt.Errorf("%w: %q", ErrOverflow, s)
		}
		whole = whole*10 + int64(r-'0')
	}

	var frac int64
	for i := 0; i < MaxDecimals; i++ {
		digit := int64(0)
		if i < len(fracStr) {
			r := fracStr[i]
			if r < '0' || r > '9' {
				return Zero, fmt.Errorf("parse quantity %q: unexpected %q", s, rune(r))
			}
			digit = int64(r - '0')
		}
		frac = frac*10 + digit
	}

	q, err := FromWhole(whole)
	if err != nil {
		return Zero, err
	}
	q, err = q.Add(FromMilli(frac))
	if err != nil {
		return Zero, err
	}
	if neg {
		return q.Neg()
	}
	return q, nil
}
