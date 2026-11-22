// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package numconv implements the conversions between JavaScript numbers and strings,
// and the integer conversions of numbers.
package numconv

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

// MaxSafeInteger is 2**53 - 1.
const MaxSafeInteger = 1<<53 - 1

// NumberToString implements Number::toString for radix 10.
func NumberToString(n float64) wtf8.String {
	if i := int64(n); float64(i) == n && i > -1e15 && i < 1e15 {
		// This also turns -0 into "0".
		return wtf8.String(strconv.FormatInt(i, 10))
	}
	var buf [32]byte
	return wtf8.String(AppendNumber(buf[:0], n))
}

// AppendNumber appends the result of Number::toString for radix 10 to dst.
func AppendNumber(dst []byte, n float64) []byte {
	if i := int64(n); float64(i) == n && i > -1e15 && i < 1e15 {
		return strconv.AppendInt(dst, i, 10)
	}
	switch {
	case n != n:
		return append(dst, "NaN"...)
	case math.IsInf(n, 1):
		return append(dst, "Infinity"...)
	case math.IsInf(n, -1):
		return append(dst, "-Infinity"...)
	}
	if a := math.Abs(n); a >= 1e-6 && a < 1e21 {
		// In this range the result is the shortest decimal digits without an exponent.
		return strconv.AppendFloat(dst, n, 'f', -1, 64)
	}

	var buf [32]byte
	b := strconv.AppendFloat(buf[:0], n, 'e', -1, 64)
	neg := b[0] == '-'
	if neg {
		b = b[1:]
	}

	// b is d[.ddd]e±xx. Collect the digits and the decimal exponent.
	e := len(b) - 1
	for b[e] != 'e' {
		e--
	}
	exp := 0
	for _, c := range b[e+2:] {
		exp = exp*10 + int(c-'0')
	}
	if b[e+1] == '-' {
		exp = -exp
	}
	var digits [24]byte
	k := 0
	for _, c := range b[:e] {
		if c != '.' {
			digits[k] = c
			k++
		}
	}
	d := digits[:k]

	// The value is 0.d1d2...dk * 10**pos.
	pos := exp + 1

	out := dst
	if neg {
		out = append(out, '-')
	}
	switch {
	case k <= pos && pos <= 21:
		out = append(out, d...)
		for i := k; i < pos; i++ {
			out = append(out, '0')
		}
	case 0 < pos && pos <= 21:
		out = append(out, d[:pos]...)
		out = append(out, '.')
		out = append(out, d[pos:]...)
	case -6 < pos && pos <= 0:
		out = append(out, '0', '.')
		for i := pos; i < 0; i++ {
			out = append(out, '0')
		}
		out = append(out, d...)
	default:
		out = append(out, d[0])
		if k > 1 {
			out = append(out, '.')
			out = append(out, d[1:]...)
		}
		out = append(out, 'e')
		if pos-1 >= 0 {
			out = append(out, '+')
		}
		out = strconv.AppendInt(out, int64(pos-1), 10)
	}
	return out
}

// DigitValue returns the base-36 value of an ASCII digit (0-9) or letter (A-Z or a-z).
// It returns 36 for any other byte.
func DigitValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'z':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 10
	}
	return 36
}

// ParseRadixDigits returns the value of s, which consists of digits of the given radix,
// rounded to the nearest number. ok is false when s is empty or has another character. poll is
// as for [StringToNumber].
func ParseRadixDigits(s string, radix int, poll func()) (f float64, ok bool) {
	if s == "" {
		return 0, false
	}
	if len(s) > scanStep {
		if skipRadixDigits(s, 0, radix, poll) < len(s) {
			return 0, false
		}
		// The leading zeros add nothing to the value.
		if s = s[skipZeros(s, 0, poll):]; s == "" {
			return 0, true
		}
	}
	var n uint64
	exact := true
	for i := 0; i < len(s); i++ {
		d := DigitValue(s[i])
		if d >= radix {
			return 0, false
		}
		if exact {
			if n > (1<<53)/uint64(radix) {
				exact = false
				continue
			}
			n = n*uint64(radix) + uint64(d)
		}
	}
	if exact {
		return float64(n), true
	}
	// A value of at least 2^1025 is beyond the numbers.
	s = strings.TrimLeft(s, "0")
	if float64(len(s)-1)*math.Log2(float64(radix)) > 1025 {
		return math.Inf(1), true
	}
	i, ok := ParseBigInt(s, radix, nil)
	if !ok {
		return 0, false
	}
	f, _ = new(big.Float).SetInt(i).Float64()
	return f, true
}

// isDecimalLiteral reports whether s matches StrUnsignedDecimalLiteral, other than Infinity.
// poll is called between the steps of a long s.
func isDecimalLiteral(s string, poll func()) bool {
	i := skipRadixDigits(s, 0, 10, poll)
	digits := i
	if i < len(s) && s[i] == '.' {
		j := skipRadixDigits(s, i+1, 10, poll)
		digits += j - i - 1
		i = j
	}
	if digits == 0 {
		return false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		start := i
		i = skipRadixDigits(s, i, 10, poll)
		if i == start {
			return false
		}
	}
	return i == len(s)
}

// StringToNumber implements StringToNumber. poll, which may be nil, is called now and then during
// the conversion of a long string, and may stop it by panicking.
func StringToNumber(str wtf8.String, poll func()) float64 {
	// The common case is a short run of digits.
	if len(str) > 0 && len(str) <= 15 {
		var n int64
		i := 0
		for ; i < len(str); i++ {
			c := str[i]
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int64(c-'0')
		}
		if i == len(str) {
			return float64(n)
		}
	}

	s := string(trimWhiteSpace(str, true, true, poll))
	if s == "" {
		return 0
	}
	if len(s) > 2 && s[0] == '0' {
		radix := 0
		switch s[1] {
		case 'x', 'X':
			radix = 16
		case 'o', 'O':
			radix = 8
		case 'b', 'B':
			radix = 2
		}
		if radix != 0 {
			f, ok := ParseRadixDigits(s[2:], radix, poll)
			if !ok {
				return math.NaN()
			}
			return f
		}
	}
	u := s
	if u[0] == '+' || u[0] == '-' {
		u = u[1:]
	}
	if u == "Infinity" {
		if s[0] == '-' {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}
	if !isDecimalLiteral(u, poll) {
		return math.NaN()
	}
	return ParseDecimal(s, poll)
}

// NumberToRadixString implements Number::toString for a radix other than 10.
func NumberToRadixString(n float64, radix int) wtf8.String {
	switch {
	case n != n:
		return "NaN"
	case math.IsInf(n, 1):
		return "Infinity"
	case math.IsInf(n, -1):
		return "-Infinity"
	case n == 0:
		return "0"
	}
	if n < 0 {
		return "-" + NumberToRadixString(-n, radix)
	}
	if n <= MaxSafeInteger && math.Trunc(n) == n {
		return wtf8.String(strconv.FormatUint(uint64(n), radix))
	}
	digits, exponent := shortestRadixDigits(n, radix)
	if exponent >= 0 {
		return wtf8.String(digits + strings.Repeat("0", exponent))
	}
	point := len(digits) + exponent
	if point <= 0 {
		return wtf8.String("0." + strings.Repeat("0", -point) + digits)
	}
	return wtf8.String(digits[:point] + "." + digits[point:])
}

// shortestRadixDigits returns the digits and exponent of the closest shortest radix representation of n.
func shortestRadixDigits(n float64, radix int) (string, int) {
	// Start with a grid coarser than the entire rounding interval, including
	// at subnormal values. Each finer grid contains all the preceding points.
	ulpExponent := max(math.Ilogb(n)-52, -1074)
	exponent := int(math.Floor(float64(ulpExponent)/math.Log2(float64(radix)))) + 2
	base := big.NewRat(int64(radix), 1)
	var quantum big.Rat
	if exponent >= 0 {
		quantum.SetInt(new(big.Int).Exp(base.Num(), big.NewInt(int64(exponent)), nil))
	} else {
		quantum.SetFrac(big.NewInt(1), new(big.Int).Exp(base.Num(), big.NewInt(int64(-exponent)), nil))
	}
	var scaled big.Rat
	scaled.Quo(scaled.SetFloat64(n), &quantum)
	one := big.NewInt(1)
	var below, above, remainder big.Int
	var candidate big.Rat
	for {
		below.QuoRem(scaled.Num(), scaled.Denom(), &remainder)
		// A subnormal can round from one digit on either side of a radix
		// power. Compare both at the position of its first nonzero digit.
		if below.Sign() > 0 {
			above.Add(&below, one)
			near, far := &below, &above
			half := remainder.Lsh(&remainder, 1).Cmp(scaled.Denom())
			if half > 0 || half == 0 && below.Bit(0) != 0 {
				near, far = far, near
			}
			// At a power of two the rounding interval can be asymmetric.
			for _, significand := range [...]*big.Int{near, far} {
				candidate.Mul(candidate.SetInt(significand), &quantum)
				if rounded, _ := candidate.Float64(); rounded == n {
					digits := significand.Text(radix)
					significant := strings.TrimRight(digits, "0")
					return significant, exponent + len(digits) - len(significant)
				}
			}
		}
		exponent--
		quantum.Quo(&quantum, base)
		scaled.Mul(&scaled, base)
	}
}

// ToInt32 implements ToInt32 for a number.
func ToInt32(f float64) int32 {
	if InInt64Range(f) {
		return int32(int64(f))
	}
	return int32(toUint32Slow(f))
}

// ToUint32 implements ToUint32 for a number.
func ToUint32(f float64) uint32 {
	if InInt64Range(f) {
		return uint32(int64(f))
	}
	return toUint32Slow(f)
}

// InInt64Range reports whether int64(f) is f rounded toward zero. Then ToInt32(f) is
// int32(int64(f)) and ToUint32(f) is uint32(int64(f)).
func InInt64Range(f float64) bool {
	return math.Abs(f) < 1<<63
}

func toUint32Slow(f float64) uint32 {
	if f != f || math.IsInf(f, 0) {
		return 0
	}
	// The remainder is exact, and fits in an int64 with either sign.
	return uint32(int64(math.Mod(math.Trunc(f), 4294967296)))
}

// ToIntegerOrInfinity implements ToIntegerOrInfinity for a number.
func ToIntegerOrInfinity(f float64) float64 {
	if f != f {
		return 0
	}
	// Adding zero turns -0 into +0.
	return math.Trunc(f) + 0
}

// ToLength implements ToLength for a number.
func ToLength(f float64) int64 {
	f = ToIntegerOrInfinity(f)
	if f <= 0 {
		return 0
	}
	if f >= MaxSafeInteger {
		return MaxSafeInteger
	}
	return int64(f)
}

// Errors of StringToBigInt.
var (
	// ErrNotBigInt is the error of a string that is not a BigInt literal.
	ErrNotBigInt = errors.New("numconv: not a BigInt literal")
	// ErrBigIntTooLarge is the error of a BigInt literal with too many bits.
	ErrBigIntTooLarge = errors.New("numconv: BigInt too large")
)

// StringToBigInt implements StringToBigInt. It returns [ErrNotBigInt] when str is not a BigInt
// literal, and -1 or 1, the sign of the value, with [ErrBigIntTooLarge] when the value has more
// than maxBits bits. poll, which may be nil, is called between the steps of a long conversion
// and may stop it by panicking.
func StringToBigInt(str wtf8.String, maxBits int, poll func()) (*big.Int, error) {
	s := string(xstrings.TrimWhiteSpace(str, true, true))
	if s == "" {
		return new(big.Int), nil
	}
	radix := 10
	if len(s) > 2 && s[0] == '0' {
		switch s[1] {
		case 'x', 'X':
			radix = 16
		case 'o', 'O':
			radix = 8
		case 'b', 'B':
			radix = 2
		}
		if radix != 10 {
			s = s[2:]
		}
	}
	var neg bool
	if radix == 10 && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		s = s[1:]
	}
	if !validDigits(s, radix, poll) {
		return nil, ErrNotBigInt
	}
	// A value of n digits has more than (n-1) log2(radix) bits.
	if d := strings.TrimLeft(s, "0"); float64(len(d)-1)*math.Log2(float64(radix)) > float64(maxBits) {
		if neg {
			return big.NewInt(-1), ErrBigIntTooLarge
		}
		return big.NewInt(1), ErrBigIntTooLarge
	}
	i, _ := ParseBigInt(s, radix, poll)
	if neg {
		i.Neg(i)
	}
	if i.BitLen() > maxBits {
		return big.NewInt(int64(i.Sign())), ErrBigIntTooLarge
	}
	return i, nil
}

// ParseFloat implements parseFloat: the longest prefix of str that is a decimal literal. poll is
// as for [StringToNumber].
func ParseFloat(str wtf8.String, poll func()) float64 {
	s := string(trimWhiteSpace(str, true, false, poll))
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	if strings.HasPrefix(s[i:], "Infinity") {
		if s[0] == '-' {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}
	start := i
	i = skipRadixDigits(s, i, 10, poll)
	digits := i - start
	if i < len(s) && s[i] == '.' {
		j := skipRadixDigits(s, i+1, 10, poll)
		digits += j - i - 1
		if digits > 0 {
			i = j
		}
	}
	if digits == 0 {
		return math.NaN()
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if k := skipRadixDigits(s, j, 10, poll); k > j {
			i = k
		}
	}
	return StringToNumber(wtf8.String(s[:i]), poll)
}

// ParseInt implements parseInt. poll is as for [StringToNumber].
func ParseInt(str wtf8.String, radix int, poll func()) float64 {
	s := string(trimWhiteSpace(str, true, false, poll))
	neg := false
	if len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		s = s[1:]
	}
	strip := true
	if radix != 0 {
		if radix < 2 || radix > 36 {
			return math.NaN()
		}
		strip = radix == 16
	} else {
		radix = 10
	}
	if strip && len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
		radix = 16
	}
	end := skipRadixDigits(s, 0, radix, poll)
	if end == 0 {
		return math.NaN()
	}
	var n float64
	if radix == 10 {
		// The decimal parser rounds correctly for any number of digits.
		n = StringToNumber(wtf8.String(s[:end]), poll)
	} else {
		n, _ = ParseRadixDigits(s[:end], radix, poll)
	}
	if neg {
		return -n
	}
	return n
}
