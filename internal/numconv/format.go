// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv

import (
	"bytes"
	"math"
	"math/big"
	"math/bits"
	"slices"
	"strconv"
)

// NumberFormatBufSize is the buffer size for the results of the formatting functions in this file.
const NumberFormatBufSize = 128

// AppendFixed appends the result of toFixed for finite |n| < 1e21 and 0 <= digits <= 100.
func AppendFixed(b []byte, n float64, digits int) []byte {
	if n == 0 {
		n = 0
	}
	return appendHalfUp(b, n, 'f', digits)
}

// AppendPrecision appends the result of toPrecision for finite n and 1 <= precision <= 100.
func AppendPrecision(b []byte, n float64, precision int) []byte {
	if n == 0 {
		return appendHalfUp(b, 0, 'f', precision-1)
	}
	if s, ok := appendPrecisionFast(b, n, precision); ok {
		return s
	}
	start := len(b)
	b = appendHalfUp(b, n, 'e', precision-1)
	e := exponent(b[start:])
	if e < -6 || e >= precision {
		return fixExponent(b, start)
	}
	return precisionFixed(b, start, precision, e)
}

// AppendExponentialAuto appends the result of toExponential with no argument for finite n.
func AppendExponentialAuto(b []byte, n float64) []byte {
	if n == 0 {
		n = 0
	}
	return fixExponent(strconv.AppendFloat(b, n, 'e', -1, 64), len(b))
}

// AppendExponential appends the result of toExponential for finite n and 0 <= digits <= 100.
func AppendExponential(b []byte, n float64, digits int) []byte {
	if n == 0 {
		n = 0
	}
	if s, ok := appendExponentialFast(b, n, digits); ok {
		return s
	}
	return fixExponent(appendHalfUp(b, n, 'e', digits), len(b))
}

// appendHalfUp appends n formatted like [strconv.AppendFloat] with the format fmt ('e' or 'f') and
// the precision prec (non-negative), except that an exact tie rounds away from zero.
func appendHalfUp(b []byte, n float64, fmt byte, prec int) []byte {
	if fmt == 'f' {
		if r, ok := appendHalfUpFixed(b, n, prec); ok {
			return r
		}
	}
	if !isTie(n, fmt, prec) {
		return strconv.AppendFloat(b, n, fmt, prec, 64)
	}
	// With one more digit, a tie is exact and ends in 5. Dropping that 5 and incrementing
	// the remaining digits rounds it to the larger magnitude.
	start := len(b)
	b = strconv.AppendFloat(b, n, fmt, prec+1, 64)
	var exp []byte
	if fmt == 'e' {
		i := bytes.IndexByte(b[start:], 'e') + start
		exp = append([]byte(nil), b[i:]...)
		b = b[:i]
	}
	b = b[:len(b)-1]
	if prec == 0 {
		// Drop the decimal point.
		b = b[:len(b)-1]
	}
	carry := true
	for i := len(b) - 1; i >= start && carry; i-- {
		switch c := b[i]; {
		case c == '9':
			b[i] = '0'
		case c >= '0' && c < '9':
			b[i]++
			carry = false
		}
	}
	if carry {
		// All the digits were 9: the result gains a leading 1.
		i := start
		if b[i] == '-' {
			i++
		}
		b = append(b, 0)
		copy(b[i+1:], b[i:])
		b[i] = '1'
		if fmt == 'e' {
			// Keep one digit before the point: 10.0e+05 becomes 1.00e+06.
			if prec > 0 {
				b[i+1], b[i+2] = b[i+2], b[i+1]
			}
			b = b[:len(b)-1]
			e, _ := strconv.Atoi(string(exp[1:]))
			return appendExponent(b, e+1)
		}
	}
	return append(b, exp...)
}

// uint64Pow10 holds 10**n for n from 0 to 19.
var uint64Pow10 = [...]uint64{
	1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9,
	1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19,
}

// appendHalfUpFixed appends what appendHalfUp appends for the finite number n with the format 'f'
// when the integer part of |n| and the bits of its fraction fit in 64 bits each and prec < 20.
// It reports false, appending nothing, otherwise.
func appendHalfUpFixed(b []byte, n float64, prec int) ([]byte, bool) {
	if prec >= len(uint64Pow10) {
		return b, false
	}
	i, f, s, ok := fixedPoint(math.Abs(n))
	if !ok {
		return b, false
	}
	// The digits after the point are those of floor(f×10^prec / 2^s), rounded by the rest.
	d, up := scaleFraction(f, s, prec)
	if up {
		d++
		if d == uint64Pow10[prec] {
			// f is not 0, so |n| < 2^53 and i cannot overflow.
			d = 0
			i++
		}
	}
	if math.Signbit(n) {
		b = append(b, '-')
	}
	b = strconv.AppendUint(b, i, 10)
	if prec > 0 {
		b = append(b, '.')
		b = appendDigits(b, d, prec)
	}
	return b, true
}

// appendPrecisionFast appends the finite nonzero number n as Number.prototype.toPrecision formats
// it with p significant digits. It reports false, appending nothing, when significand does.
func appendPrecisionFast(b []byte, n float64, p int) ([]byte, bool) {
	d, e, ok := significand(n, p)
	if !ok {
		return b, false
	}
	if n < 0 {
		b = append(b, '-')
	}
	if e < -6 || e >= p {
		return appendScientific(b, d, p, e), true
	}
	if e < 0 {
		// "0." and -e-1 zeros.
		b = append(b, "0.00000"[:1-e]...)
		return appendDigits(b, d, p), true
	}
	b = appendDigits(b, d, p)
	if e+1 < p {
		b = insertPoint(b, len(b)-p+e+1)
	}
	return b, true
}

// appendExponentialFast appends the finite nonzero number n as Number.prototype.toExponential formats
// it with f digits after the point. It reports false, appending nothing, when significand does.
func appendExponentialFast(b []byte, n float64, f int) ([]byte, bool) {
	d, e, ok := significand(n, f+1)
	if !ok {
		return b, false
	}
	if n < 0 {
		b = append(b, '-')
	}
	return appendScientific(b, d, f+1, e), true
}

// significand returns |n| rounded to p significant digits with ties away from zero, as the digits d
// (10^(p-1) <= d < 10^p) and the decimal exponent e of the first digit. It reports false when n is
// 0, when its integer part or the bits of its fraction do not fit in 64 bits, or when p > 19.
func significand(n float64, p int) (d uint64, e int, ok bool) {
	i, f, s, ok := fixedPoint(math.Abs(n))
	if !ok {
		return 0, 0, false
	}
	return roundSignificant(i, f, s, p)
}

// appendScientific appends the p digits of d with a point after the first one, then e as the
// exponent, in the form of Number.prototype.toExponential.
func appendScientific(b []byte, d uint64, p, e int) []byte {
	b = appendDigits(b, d, p)
	if p > 1 {
		b = insertPoint(b, len(b)-p+1)
	}
	if e < 0 {
		b = append(b, 'e', '-')
		e = -e
	} else {
		b = append(b, 'e', '+')
	}
	return strconv.AppendInt(b, int64(e), 10)
}

// insertPoint inserts a decimal point into b at the index i.
func insertPoint(b []byte, i int) []byte {
	b = append(b, 0)
	copy(b[i+1:], b[i:])
	b[i] = '.'
	return b
}

// fixedPoint returns the finite non-negative number x as its integer part i and its fraction
// f/2^s, where f < 2^s and s <= 64. It reports false when i or the fraction does not fit.
func fixedPoint(x float64) (i, f uint64, s uint, ok bool) {
	u := math.Float64bits(x)
	m := u & (1<<52 - 1)
	e2 := int(u >> 52 & 0x7ff)
	if e2 == 0 {
		e2 = -1074
	} else {
		m |= 1 << 52
		e2 -= 1075
	}
	if m == 0 {
		return 0, 0, 0, true
	}
	tz := bits.TrailingZeros64(m)
	m >>= tz
	e2 += tz
	if e2 >= 0 {
		if bits.Len64(m)+e2 > 64 {
			return 0, 0, 0, false
		}
		return m << e2, 0, 0, true
	}
	if e2 < -64 {
		return 0, 0, 0, false
	}
	s = uint(-e2)
	return m >> s, m & (1<<s - 1), s, true
}

// scaleFraction returns floor(f×10^k / 2^s) for f < 2^s, s <= 64 and k <= 19, and reports
// whether the rest is at least half of 2^s.
func scaleFraction(f uint64, s uint, k int) (d uint64, up bool) {
	if f == 0 {
		return 0, false
	}
	hi, lo := bits.Mul64(f, uint64Pow10[k])
	if s == 64 {
		return hi, lo >= 1<<63
	}
	return hi<<(64-s) | lo>>s, lo&(1<<s-1) >= 1<<(s-1)
}

// roundSignificant returns the positive number i + f/2^s (as fixedPoint returns it) rounded to p
// significant digits with ties away from zero, as the digits d (10^(p-1) <= d < 10^p) and the
// decimal exponent e of the first digit. It reports false when the number is 0 or does not fit
// the arithmetic.
func roundSignificant(i, f uint64, s uint, p int) (d uint64, e int, ok bool) {
	if p >= len(uint64Pow10) {
		return 0, 0, false
	}
	var up bool
	switch {
	case i > 0:
		// i has n digits.
		n := bits.Len64(i) * 1233 >> 12
		if i >= uint64Pow10[n] {
			n++
		}
		e = n - 1
		switch {
		case n == p:
			d = i
			up = f != 0 && f >= 1<<(s-1)
		case n > p:
			// The dropped digits of i decide alone: the fraction cannot lift a rest below half
			// to half.
			pw := uint64Pow10[n-p]
			d = i / pw
			up = i-d*pw >= pw/2
		default:
			k := p - n
			var fd uint64
			fd, up = scaleFraction(f, s, k)
			d = i*uint64Pow10[k] + fd
		}
	case f > 0:
		// floor(log10(x)) is e or e+1 for e = floor(log10(2^floor(log2(x)))).
		e = (bits.Len64(f) - 1 - int(s)) * 78913 >> 18
		k := p - 1 - e
		if k >= len(uint64Pow10) {
			return 0, 0, false
		}
		d, up = scaleFraction(f, s, k)
		if d >= uint64Pow10[p] {
			e++
			d, up = scaleFraction(f, s, k-1)
		}
	default:
		return 0, 0, false
	}
	if up {
		d++
		if d == uint64Pow10[p] {
			d = uint64Pow10[p-1]
			e++
		}
	}
	return d, e, true
}

// digitPairs holds the two decimal digits of each number from 0 to 99.
const digitPairs = "00010203040506070809" +
	"10111213141516171819" +
	"20212223242526272829" +
	"30313233343536373839" +
	"40414243444546474849" +
	"50515253545556575859" +
	"60616263646566676869" +
	"70717273747576777879" +
	"80818283848586878889" +
	"90919293949596979899"

// appendDigits appends d as exactly n decimal digits, with leading zeros, for n <= 20 and
// d < 10^n.
func appendDigits(b []byte, d uint64, n int) []byte {
	b = append(b, "00000000000000000000"[:n]...)
	a := b[len(b)-n:]
	for n >= 2 && d > 0 {
		q := d / 100
		r := (d - q*100) * 2
		a[n-1], a[n-2] = digitPairs[r+1], digitPairs[r]
		d = q
		n -= 2
	}
	if d > 0 {
		a[0] = byte('0' + d)
	}
	return b
}

// precisionFixed rewrites p rounded exponential digits in fixed notation for -6 <= e < p.
func precisionFixed(s []byte, start, p, e int) []byte {
	if s[start] == '-' {
		start++
	}
	// Remove the point and exponent, leaving only the sign and significant digits.
	if p > 1 {
		copy(s[start+1:], s[start+2:start+p+1])
	}
	s = s[:start+p]
	if e < 0 {
		s = slices.Grow(s, 1-e)[:start+p+1-e]
		copy(s[start+1-e:], s[start:start+p])
		s[start], s[start+1] = '0', '.'
		for i := start + 2; i < start+1-e; i++ {
			s[i] = '0'
		}
	} else if e+1 < p {
		s = s[:len(s)+1]
		point := start + e + 1
		copy(s[point+1:], s[point:len(s)-1])
		s[point] = '.'
	}
	return s
}

// appendExponent appends the exponent e in the form of [strconv.AppendFloat] with the format 'e'.
func appendExponent(b []byte, e int) []byte {
	b = append(b, 'e')
	if e < 0 {
		b = append(b, '-')
		e = -e
	} else {
		b = append(b, '+')
	}
	if e < 10 {
		b = append(b, '0')
	}
	return strconv.AppendInt(b, int64(e), 10)
}

// isTie reports whether the finite number n lies exactly halfway between the two nearest numbers
// that the format fmt ('e' or 'f') shows with the precision prec.
func isTie(n float64, fmt byte, prec int) bool {
	if n == 0 {
		return false
	}
	u := math.Float64bits(n)
	m := u & (1<<52 - 1)
	e2 := int(u >> 52 & 0x7ff)
	if e2 == 0 {
		e2 = -1074
	} else {
		m |= 1 << 52
		e2 -= 1075
	}
	tz := bits.TrailingZeros64(m)
	m >>= tz
	e2 += tz
	// |n| is m×2^e2 with m odd, so its exact decimal expansion has k fraction digits
	// and, when k is positive, ends in 5.
	k := max(0, -e2)
	if fmt == 'f' {
		return k == prec+1
	}
	if k > 0 {
		// The expansion has e10+1+k significant digits, where e10 is the decimal exponent of n,
		// which lies within one of the bounds from the binary exponent.
		e := e2 + bits.Len64(m) - 1
		lo := int(math.Floor(float64(e)*math.Log10(2))) - 1
		hi := int(math.Floor(float64(e+1)*math.Log10(2))) + 1
		if prec+2 < lo+1+k || prec+2 > hi+1+k {
			return false
		}
	}
	// A tie shows as a trailing 5 with one more digit, and that string is then exactly n.
	var buf [NumberFormatBufSize]byte
	t := strconv.AppendFloat(buf[:0], n, 'e', prec+1, 64)
	if t[bytes.IndexByte(t, 'e')-1] != '5' {
		return false
	}
	r, ok := new(big.Rat).SetString(string(t))
	return ok && r.Cmp(new(big.Rat).SetFloat64(n)) == 0
}

// exponent returns the exponent of the number s that [strconv.AppendFloat] formats with the format 'e'.
func exponent(s []byte) int {
	i := bytes.IndexByte(s, 'e')
	e := 0
	for _, c := range s[i+2:] {
		e = e*10 + int(c-'0')
	}
	if s[i+1] == '-' {
		return -e
	}
	return e
}

// fixExponent rewrites in place the exponent that Go formats with at least two digits to the JavaScript form.
func fixExponent(s []byte, start int) []byte {
	i := start + bytes.IndexByte(s[start:], 'e')
	digits := s[i+2:]
	j := 0
	for j < len(digits)-1 && digits[j] == '0' {
		j++
	}
	return append(s[:i+2], digits[j:]...)
}
