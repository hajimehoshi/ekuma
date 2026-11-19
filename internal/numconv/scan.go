// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv

import (
	"math"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

// scanStep is the number of bytes that a conversion of a long string scans between two calls of
// its poll function.
const scanStep = 1 << 16

// callPoll calls poll unless it is nil.
func callPoll(poll func()) {
	if poll != nil {
		poll()
	}
}

// skipRadixDigits returns the end of the run of digits of the radix in s from i, calling poll
// between the steps of a long run.
func skipRadixDigits(s string, i, radix int, poll func()) int {
	for {
		end := min(len(s), i+scanStep)
		for i < end && DigitValue(s[i]) < radix {
			i++
		}
		if i < end || i == len(s) {
			return i
		}
		callPoll(poll)
	}
}

// skipZeros returns the end of the run of zeros in s from i, calling poll between the steps of a
// long run.
func skipZeros(s string, i int, poll func()) int {
	for {
		end := min(len(s), i+scanStep)
		for i < end && s[i] == '0' {
			i++
		}
		if i < end || i == len(s) {
			return i
		}
		callPoll(poll)
	}
}

// trimWhiteSpace is [xstrings.TrimWhiteSpace], calling poll between the steps over long runs of
// white space.
func trimWhiteSpace(s string, start, end bool, poll func()) string {
	if start {
		for len(s) > scanStep {
			w := s[:scanStep]
			n := len(w) - len(xstrings.TrimWhiteSpace(w, true, false))
			if n == 0 {
				break
			}
			s = s[n:]
			callPoll(poll)
		}
	}
	if end {
		for len(s) > scanStep {
			w := s[len(s)-scanStep:]
			n := len(w) - len(xstrings.TrimWhiteSpace(w, false, true))
			if n == 0 {
				break
			}
			s = s[:len(s)-n]
			callPoll(poll)
		}
	}
	return xstrings.TrimWhiteSpace(s, start, end)
}

// maxDecimalDigits is the number of significant digits that ParseDecimal keeps. Together with a
// sticky digit for the rest, they decide the rounding of any value to a number, and they stay
// below the length from which strconv.ParseFloat misplaces the decimal point before Go 1.28
// (go.dev/issue/82118).
const maxDecimalDigits = 780

// ParseDecimal returns the value of s, a StrDecimalLiteral other than Infinity, rounded to the
// nearest number. poll is as for [StringToNumber].
func ParseDecimal(s string, poll func()) float64 {
	if len(s) <= maxDecimalDigits {
		// ParseFloat rounds correctly and returns an infinity when the value is out of range.
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	// The value is 0.d × 10^(point-first) × 10^exp for the digits d from the first
	// significant one, of which the first maxDecimalDigits are kept.
	b := make([]byte, 0, maxDecimalDigits+32)
	i := 0
	if s[0] == '+' || s[0] == '-' {
		if s[0] == '-' {
			b = append(b, '-')
		}
		i++
	}
	mantissa := len(b)
	var point int
	first := -1
	sticky := false
	// count is the number of digits read.
	count := 0
	// digits reads the run of digits from j and returns its end.
	digits := func(j int) int {
		if first < 0 {
			z := skipZeros(s, j, poll)
			count += z - j
			j = z
			if j < len(s) && s[j] >= '1' && s[j] <= '9' {
				first = count
			}
		}
		start := j
		for j < len(s) && s[j] >= '0' && s[j] <= '9' && len(b)-mantissa < maxDecimalDigits {
			b = append(b, s[j])
			j++
		}
		count += j - start
		end := skipRadixDigits(s, j, 10, poll)
		if !sticky && skipZeros(s, j, poll) < end {
			sticky = true
		}
		count += end - j
		return end
	}
	i = digits(i)
	point = count
	if i < len(s) && s[i] == '.' {
		i = digits(i + 1)
	}
	if first < 0 {
		// Every digit is zero.
		if mantissa > 0 {
			return math.Copysign(0, -1)
		}
		return 0
	}
	if sticky {
		b = append(b, '1')
	}
	exp := 0
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		neg := false
		if s[i] == '+' || s[i] == '-' {
			neg = s[i] == '-'
			i++
		}
		i = skipZeros(s, i, poll)
		// An exponent beyond 10^9 gives an infinity or a zero, as 10^9 does.
		for ; i < len(s) && exp < 1e9; i++ {
			exp = exp*10 + int(s[i]-'0')
		}
		if neg {
			exp = -exp
		}
	}
	exp += point - first - (len(b) - mantissa)
	b = append(b, 'e')
	b = strconv.AppendInt(b, int64(exp), 10)
	f, _ := strconv.ParseFloat(string(b), 64)
	return f
}
