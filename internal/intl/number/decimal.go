// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package number implements the locale-independent and locale-dependent number formatting of
// ECMA-402: the Intl mathematical values, their rounding, and the parts that Intl.NumberFormat
// formats them into.
package number

import (
	"math"
	"math/big"
	"math/bits"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

// Decimal is an Intl mathematical value: a decimal number, negative zero, an infinity or
// not-a-number.
type Decimal struct {
	nan bool
	inf bool
	neg bool
	// digits are the decimal digits of the coefficient without leading or trailing zeros. They
	// are empty for zero.
	digits string
	// exp is the power of ten that the coefficient is multiplied by.
	exp int
}

// NaN returns not-a-number.
func NaN() Decimal {
	return Decimal{
		nan: true,
	}
}

// Inf returns positive infinity, or negative infinity when neg is true.
func Inf(neg bool) Decimal {
	return Decimal{
		inf: true,
		neg: neg,
	}
}

// IsNaN reports whether d is not-a-number.
func (d Decimal) IsNaN() bool {
	return d.nan
}

// IsInf reports whether d is an infinity.
func (d Decimal) IsInf() bool {
	return d.inf
}

// IsZero reports whether d is zero or negative zero.
func (d Decimal) IsZero() bool {
	return !d.nan && !d.inf && d.digits == ""
}

// Neg reports whether d is negative, negative zero or negative infinity.
func (d Decimal) Neg() bool {
	return d.neg
}

// newDecimal returns the value of the digits s, which may have leading and trailing zeros,
// times ten to the power of exp.
func newDecimal(neg bool, s string, exp int) Decimal {
	s = strings.TrimLeft(s, "0")
	n := len(s)
	s = strings.TrimRight(s, "0")
	exp += n - len(s)
	if s == "" {
		exp = 0
	}
	return Decimal{
		neg:    neg,
		digits: s,
		exp:    exp,
	}
}

// FromFloat returns the Intl mathematical value of the Number f.
func FromFloat(f float64) Decimal {
	switch {
	case math.IsNaN(f):
		return NaN()
	case math.IsInf(f, 0):
		return Inf(f < 0)
	case f == 0:
		return Decimal{
			neg: math.Signbit(f),
		}
	}
	s := strconv.FormatFloat(math.Abs(f), 'e', -1, 64)
	mant, e, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(e)
	whole, frac, _ := strings.Cut(mant, ".")
	return newDecimal(f < 0, whole+frac, exp-len(frac))
}

// FromInteger returns the Intl mathematical value of the integer whose decimal digits are s,
// which may start with a minus sign.
func FromInteger(s string) Decimal {
	neg := strings.HasPrefix(s, "-")
	return newDecimal(neg, strings.TrimPrefix(s, "-"), 0)
}

// Parse returns the Intl mathematical value of the String s as ToIntlMathematicalValue
// converts it: the exact value of a StringNumericLiteral, not-a-number for any other string,
// and an infinity or zero for a value that rounds to one as a Number.
func Parse(s string) Decimal {
	t := xstrings.TrimWhiteSpace(s, true, true)
	if t == "" {
		return Decimal{}
	}
	if len(t) > 2 && t[0] == '0' {
		radix := 0
		switch t[1] {
		case 'x', 'X':
			radix = 16
		case 'o', 'O':
			radix = 8
		case 'b', 'B':
			radix = 2
		}
		if radix != 0 {
			for i := 2; i < len(t); i++ {
				if numconv.DigitValue(t[i]) >= radix {
					return NaN()
				}
			}
			// A value of more bits than this is beyond the largest finite Number.
			const maxBits = 1100
			digits := strings.TrimLeft(t[2:], "0")
			if (len(digits)-1)*bits.Len(uint(radix-1)) > maxBits {
				return Inf(false)
			}
			i, ok := new(big.Int).SetString("0"+digits, radix)
			if !ok {
				return NaN()
			}
			return roundedToNumber(FromInteger(i.String()), s)
		}
	}
	neg := false
	u := t
	switch u[0] {
	case '+':
		u = u[1:]
	case '-':
		neg = true
		u = u[1:]
	}
	if u == "Infinity" {
		return Inf(neg)
	}
	d, ok := parseDecimal(u)
	if !ok {
		return NaN()
	}
	d.neg = neg
	return roundedToNumber(d, s)
}

// parseDecimal parses s as StrUnsignedDecimalLiteral other than Infinity.
func parseDecimal(s string) (Decimal, bool) {
	i := 0
	for i < len(s) && '0' <= s[i] && s[i] <= '9' {
		i++
	}
	whole := s[:i]
	var frac string
	if i < len(s) && s[i] == '.' {
		i++
		start := i
		for i < len(s) && '0' <= s[i] && s[i] <= '9' {
			i++
		}
		frac = s[start:i]
	}
	if whole == "" && frac == "" {
		return Decimal{}, false
	}
	exp := 0
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		expNeg := false
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			expNeg = s[i] == '-'
			i++
		}
		start := i
		for i < len(s) && '0' <= s[i] && s[i] <= '9' {
			i++
		}
		if i == start {
			return Decimal{}, false
		}
		// An exponent this large makes the value round to zero or an infinity as a Number,
		// which roundedToNumber detects.
		e := strings.TrimLeft(s[start:i], "0")
		if len(e) > 9 {
			e = "999999999"
		}
		exp, _ = strconv.Atoi("0" + e)
		if expNeg {
			exp = -exp
		}
	}
	if i != len(s) {
		return Decimal{}, false
	}
	return newDecimal(false, whole+frac, exp-len(frac)), true
}

// roundedToNumber returns d, or the infinity or zero that d rounds to as a Number. s is the
// string that d was parsed from.
func roundedToNumber(d Decimal, s string) Decimal {
	if d.digits == "" {
		return d
	}
	// The magnitude decides the cases far from the range of Numbers without parsing s.
	switch mag := d.magnitude(); {
	case mag > 400:
		return Inf(d.neg)
	case mag < -400:
		return Decimal{
			neg: d.neg,
		}
	}
	f := numconv.StringToNumber(s)
	switch {
	case math.IsInf(f, 0):
		return Inf(d.neg)
	case f == 0:
		return Decimal{
			neg: d.neg,
		}
	}
	return d
}

// magnitude returns the power of ten of the most significant digit of d, which is finite
// and not zero.
func (d Decimal) magnitude() int {
	return len(d.digits) - 1 + d.exp
}

// abs returns the absolute value of d.
func (d Decimal) abs() Decimal {
	d.neg = false
	return d
}

// scale returns d times ten to the power of n, for d that is finite.
func (d Decimal) scale(n int) Decimal {
	if d.digits != "" {
		d.exp += n
	}
	return d
}

// String returns d as a decimal string without an exponent, for d that is finite.
func (d Decimal) String() string {
	var b strings.Builder
	if d.neg {
		b.WriteByte('-')
	}
	b.WriteString(d.plain())
	return b.String()
}

// plain returns the absolute value of the finite d as a decimal string without an exponent.
func (d Decimal) plain() string {
	if d.digits == "" {
		return "0"
	}
	if d.exp >= 0 {
		return d.digits + strings.Repeat("0", d.exp)
	}
	n := len(d.digits) + d.exp
	if n > 0 {
		return d.digits[:n] + "." + d.digits[n:]
	}
	return "0." + strings.Repeat("0", -n) + d.digits
}

// unsignedRoundingMode is an unsigned rounding mode of ECMA-402.
type unsignedRoundingMode int

const (
	roundZero unsignedRoundingMode = iota
	roundInfinity
	roundHalfZero
	roundHalfInfinity
	roundHalfEven
)

// unsignedRoundingModeOf implements GetUnsignedRoundingMode.
func unsignedRoundingModeOf(mode string, neg bool) unsignedRoundingMode {
	switch mode {
	case "ceil":
		if neg {
			return roundZero
		}
		return roundInfinity
	case "floor":
		if neg {
			return roundInfinity
		}
		return roundZero
	case "expand":
		return roundInfinity
	case "trunc":
		return roundZero
	case "halfCeil":
		if neg {
			return roundHalfZero
		}
		return roundHalfInfinity
	case "halfFloor":
		if neg {
			return roundHalfInfinity
		}
		return roundHalfZero
	case "halfTrunc":
		return roundHalfZero
	case "halfEven":
		return roundHalfEven
	}
	return roundHalfInfinity
}

// roundTo returns the non-negative finite x rounded to a multiple of inc times ten to the
// power of mag with the mode m, as the decimal digits of the coefficient of that multiple
// without leading zeros. inc is from 1 to 5000.
func roundTo(x Decimal, mag int, inc int64, m unsignedRoundingMode) string {
	if x.digits == "" {
		return "0"
	}
	// x / 10^mag = n + f with the digits n of an integer and the digits f of a fraction below 1
	// without trailing zeros. lead is the number of the zeros between the point and f.
	var n, f string
	var lead int
	switch e := x.exp - mag; {
	case e >= 0:
		n = x.digits + strings.Repeat("0", e)
	case len(x.digits)+e > 0:
		k := len(x.digits) + e
		n, f = x.digits[:k], x.digits[k:]
	default:
		n, f = "0", x.digits
		lead = -(len(x.digits) + e)
	}
	q, rem := divSmall(n, inc)
	if rem == 0 && f == "" {
		return q
	}
	up := false
	switch m {
	case roundZero:
	case roundInfinity:
		up = true
	default:
		// c is the sign of 2 × (rem + f) - inc, where f is below 1.
		var c int
		switch k := inc - 2*rem; {
		case f == "":
			c = -cmpInt(k, 0)
		case k <= 0:
			c = 1
		case k >= 2:
			c = -1
		case lead > 0 || f < "5":
			c = -1
		case f == "5":
			c = 0
		default:
			c = 1
		}
		switch {
		case c > 0:
			up = true
		case c == 0:
			switch m {
			case roundHalfInfinity:
				up = true
			case roundHalfEven:
				up = (q[len(q)-1]-'0')%2 == 1
			}
		}
	}
	if up {
		q = addOne(q)
	}
	return q
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// divSmall returns the quotient and the remainder of the non-negative integer of the decimal
// digits n divided by d, which is from 1 to 5000. The quotient has no leading zeros.
func divSmall(n string, d int64) (string, int64) {
	if d == 1 {
		return n, 0
	}
	q := make([]byte, 0, len(n))
	var r int64
	for i := 0; i < len(n); i++ {
		r = r*10 + int64(n[i]-'0')
		if len(q) > 0 || r >= d {
			q = append(q, byte('0'+r/d))
		}
		r %= d
	}
	if len(q) == 0 {
		return "0", r
	}
	return string(q), r
}

// mulSmall returns the decimal digits of the non-negative integer of the decimal digits n
// without leading zeros times m, which is from 1 to 5000.
func mulSmall(n string, m int64) string {
	if m == 1 || n == "0" {
		return n
	}
	out := make([]byte, len(n)+4)
	var carry int64
	for i := len(n) - 1; i >= 0; i-- {
		p := int64(n[i]-'0')*m + carry
		out[i+4] = byte('0' + p%10)
		carry = p / 10
	}
	i := 4
	for carry > 0 {
		i--
		out[i] = byte('0' + carry%10)
		carry /= 10
	}
	return string(out[i:])
}

// addOne returns the decimal digits of the non-negative integer of the decimal digits n plus
// one.
func addOne(n string) string {
	b := []byte(n)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] != '9' {
			b[i]++
			return string(b)
		}
		b[i] = '0'
	}
	return "1" + string(b)
}

// rawResult is the result of ToRawPrecision or ToRawFixed.
type rawResult struct {
	formattedString   string
	roundedNumber     Decimal
	integerDigits     int
	roundingMagnitude int
}

// toRawPrecision implements ToRawPrecision for the non-negative finite x.
func toRawPrecision(x Decimal, minP, maxP int, m unsignedRoundingMode) rawResult {
	p := maxP
	var digits string
	var e int
	var final Decimal
	if x.digits == "" {
		digits = strings.Repeat("0", p)
	} else {
		e = x.magnitude()
		digits = roundTo(x, e-p+1, 1, m)
		if len(digits) > p {
			// The value rounded up to the next power of ten.
			e++
			digits = digits[:p]
		}
		final = newDecimal(false, digits, e-p+1)
	}
	var s string
	var intDigits int
	switch {
	case e >= p-1:
		s = digits + strings.Repeat("0", e-p+1)
		intDigits = e + 1
	case e >= 0:
		s = digits[:e+1] + "." + digits[e+1:]
		intDigits = e + 1
	default:
		s = "0." + strings.Repeat("0", -(e+1)) + digits
		intDigits = 1
	}
	if strings.Contains(s, ".") && maxP > minP {
		cut := maxP - minP
		for cut > 0 && s[len(s)-1] == '0' {
			s = s[:len(s)-1]
			cut--
		}
		s = strings.TrimSuffix(s, ".")
	}
	return rawResult{
		formattedString:   s,
		roundedNumber:     final,
		integerDigits:     intDigits,
		roundingMagnitude: e - p + 1,
	}
}

// toRawFixed implements ToRawFixed for the non-negative finite x.
func toRawFixed(x Decimal, minF, maxF, inc int, m unsignedRoundingMode) rawResult {
	f := maxF
	s := mulSmall(roundTo(x, -f, int64(inc), m), int64(inc))
	final := newDecimal(false, s, -f)
	var intDigits int
	if f != 0 {
		k := len(s)
		if k <= f {
			s = strings.Repeat("0", f+1-k) + s
			k = f + 1
		}
		a, b := s[:k-f], s[k-f:]
		s = a + "." + b
		intDigits = len(a)
	} else {
		intDigits = len(s)
	}
	cut := maxF - minF
	for cut > 0 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
		cut--
	}
	s = strings.TrimSuffix(s, ".")
	return rawResult{
		formattedString:   s,
		roundedNumber:     final,
		integerDigits:     intDigits,
		roundingMagnitude: -f,
	}
}
