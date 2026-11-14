// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package numconv implements the conversions between JavaScript numbers and strings,
// and the integer conversions of numbers.
package numconv

import (
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxSafeInteger is 2**53 - 1.
const MaxSafeInteger = 1<<53 - 1

// NumberToString implements Number::toString for radix 10.
func NumberToString(n float64) string {
	if i := int64(n); float64(i) == n && i > -1e15 && i < 1e15 {
		// This also turns -0 into "0".
		return strconv.FormatInt(i, 10)
	}
	var buf [32]byte
	return string(AppendNumber(buf[:0], n))
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

// isWhiteSpaceOrLineTerminator reports whether r is WhiteSpace or LineTerminator.
func isWhiteSpaceOrLineTerminator(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// TrimWhiteSpace implements TrimString. start and end select the sides to trim.
func TrimWhiteSpace(s string, start, end bool) string {
	if start {
		for len(s) > 0 {
			if c := s[0]; c < utf8.RuneSelf {
				if !isWhiteSpaceOrLineTerminator(rune(c)) {
					break
				}
				s = s[1:]
				continue
			}
			r, size := utf8.DecodeRuneInString(s)
			if !isWhiteSpaceOrLineTerminator(r) {
				break
			}
			s = s[size:]
		}
	}
	if end {
		for len(s) > 0 {
			if c := s[len(s)-1]; c < utf8.RuneSelf {
				if !isWhiteSpaceOrLineTerminator(rune(c)) {
					break
				}
				s = s[:len(s)-1]
				continue
			}
			r, size := utf8.DecodeLastRuneInString(s)
			if !isWhiteSpaceOrLineTerminator(r) {
				break
			}
			s = s[:len(s)-size]
		}
	}
	return s
}

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
// rounded to the nearest number. ok is false when s is empty or has another character.
func ParseRadixDigits(s string, radix int) (f float64, ok bool) {
	if s == "" {
		return 0, false
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
	i, ok := new(big.Int).SetString(s, radix)
	if !ok {
		return 0, false
	}
	f, _ = new(big.Float).SetInt(i).Float64()
	return f, true
}

// isDecimalLiteral reports whether s matches StrUnsignedDecimalLiteral, other than Infinity.
func isDecimalLiteral(s string) bool {
	i := 0
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits++
		}
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
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return false
		}
	}
	return i == len(s)
}

// StringToNumber implements StringToNumber.
func StringToNumber(s string) float64 {
	// The common case is a short run of digits.
	if len(s) > 0 && len(s) <= 15 {
		var n int64
		i := 0
		for ; i < len(s); i++ {
			c := s[i]
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int64(c-'0')
		}
		if i == len(s) {
			return float64(n)
		}
	}

	s = TrimWhiteSpace(s, true, true)
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
			f, ok := ParseRadixDigits(s[2:], radix)
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
	if !isDecimalLiteral(u) {
		return math.NaN()
	}
	// ParseFloat rounds correctly and returns an infinity when the value is out of range.
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// NumberToRadixString implements Number::toString for a radix other than 10.
func NumberToRadixString(n float64, radix int) string {
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
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	neg := n < 0
	if neg {
		n = -n
	}
	ip := math.Floor(n)
	fp := n - ip

	var sb strings.Builder
	if neg {
		sb.WriteByte('-')
	}
	if ip < 1<<63 {
		sb.WriteString(strconv.FormatUint(uint64(ip), radix))
	} else {
		i, _ := new(big.Float).SetFloat64(ip).Int(nil)
		sb.WriteString(i.Text(radix))
	}
	if fp > 0 {
		sb.WriteByte('.')
		// Emit digits until the remaining fraction is below half of the distance to the next number.
		delta := 0.5 * (math.Nextafter(n, math.Inf(1)) - n)
		delta = math.Max(math.Nextafter(0, 1), delta)
		r := float64(radix)
		for {
			fp *= r
			delta *= r
			d := int(fp)
			sb.WriteByte(digits[d])
			fp -= float64(d)
			if fp > 0.5 || (fp == 0.5 && d&1 != 0) {
				if fp+delta > 1 {
					// Round up, carrying into the digits already written.
					return roundUpRadix(sb.String(), radix)
				}
			}
			if fp < delta {
				break
			}
		}
	}
	return sb.String()
}

// roundUpRadix adds one unit in the last place to the radix string s.
func roundUpRadix(s string, radix int) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := []byte(s)
	for i := len(b) - 1; i >= 0; i-- {
		c := b[i]
		if c == '.' || c == '-' {
			continue
		}
		d := DigitValue(c) + 1
		if d < radix {
			b[i] = digits[d]
			return strings.TrimRight(trimFraction(string(b)), ".")
		}
		b[i] = '0'
	}
	// Every digit carried.
	neg := len(b) > 0 && b[0] == '-'
	if neg {
		b = b[1:]
	}
	out := "1" + string(b)
	if neg {
		out = "-" + out
	}
	return strings.TrimRight(trimFraction(out), ".")
}

// trimFraction removes trailing zeros after the radix point of s.
func trimFraction(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	return strings.TrimRight(s, "0")
}

// ToInt32 implements ToInt32 for a number.
func ToInt32(f float64) int32 {
	if f >= -2147483648 && f <= 2147483647 {
		return int32(f)
	}
	return int32(toUint32Slow(f))
}

// ToUint32 implements ToUint32 for a number.
func ToUint32(f float64) uint32 {
	if f >= 0 && f <= 4294967295 {
		return uint32(f)
	}
	return toUint32Slow(f)
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

// StringToBigInt implements StringToBigInt. It returns nil when s is not a BigInt literal.
func StringToBigInt(s string) *big.Int {
	s = TrimWhiteSpace(s, true, true)
	if s == "" {
		return new(big.Int)
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
	digits := s
	if radix == 10 && (s[0] == '+' || s[0] == '-') {
		digits = s[1:]
	}
	if digits == "" {
		return nil
	}
	for i := 0; i < len(digits); i++ {
		if DigitValue(digits[i]) >= radix {
			return nil
		}
	}
	i, ok := new(big.Int).SetString(s, radix)
	if !ok {
		return nil
	}
	return i
}

// ParseFloat implements parseFloat: the longest prefix of s that is a decimal literal.
func ParseFloat(s string) float64 {
	s = TrimWhiteSpace(s, true, false)
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
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		j := i + 1
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
			digits++
		}
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
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j {
			i = k
		}
	}
	return StringToNumber(s[:i])
}

// ParseInt implements parseInt.
func ParseInt(s string, radix int) float64 {
	s = TrimWhiteSpace(s, true, false)
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
	end := 0
	for end < len(s) && DigitValue(s[end]) < radix {
		end++
	}
	if end == 0 {
		return math.NaN()
	}
	var n float64
	if radix == 10 {
		// The decimal parser rounds correctly for any number of digits.
		n = StringToNumber(s[:end])
	} else {
		n, _ = ParseRadixDigits(s[:end], radix)
	}
	if neg {
		return -n
	}
	return n
}
