// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime

import (
	"math"
	"strings"
)

// Parse returns the time value that the string s represents, or NaN. It accepts the
// date time string format of ECMAScript, the formats of [String] and [UTCString], and some
// common legacy formats. A time without an offset is a local time of tz.
func Parse(s string, tz TimeZone) float64 {
	if f, ok := parseDateTime(s, tz); ok {
		return f
	}
	if len(s) >= 29 && (s[3] == ' ' || s[3] == ',') {
		if f, ok := parseFormatted(s, tz); ok {
			return f
		}
	}
	return parseLegacy(s, tz)
}

// fields are the components of a parsed date. month is from 1 to 12.
type fields struct {
	year, month, day                  float64
	hour, minute, second, millisecond float64

	// offset is the offset from UTC in milliseconds when hasOffset is set.
	offset    float64
	hasOffset bool
}

// timeValue returns the time value of f. A time without an offset is a local time of tz.
func (f *fields) timeValue(tz TimeZone) float64 {
	day := MakeDay(f.year, f.month-1, f.day)
	t := MakeDate(day, MakeTime(f.hour, f.minute, f.second, f.millisecond))
	if f.hasOffset {
		t -= f.offset
	} else {
		t = UTC(t, tz)
	}
	return TimeClip(t)
}

func isDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

func isLetter(c byte) bool {
	return 'a' <= c|0x20 && c|0x20 <= 'z'
}

// scanner reads a string byte by byte.
type scanner struct {
	s   string
	pos int
}

func (sc *scanner) done() bool {
	return sc.pos >= len(sc.s)
}

func (sc *scanner) peek() byte {
	if sc.pos < len(sc.s) {
		return sc.s[sc.pos]
	}
	return 0
}

// accept consumes c if it is the next byte.
func (sc *scanner) accept(c byte) bool {
	if sc.peek() == c && !sc.done() {
		sc.pos++
		return true
	}
	return false
}

// fixed reads exactly n digits.
func (sc *scanner) fixed(n int) (float64, bool) {
	if sc.pos+n > len(sc.s) {
		return 0, false
	}
	var v float64
	for i := range n {
		c := sc.s[sc.pos+i]
		if !isDigit(c) {
			return 0, false
		}
		v = v*10 + float64(c-'0')
	}
	sc.pos += n
	return v, true
}

// number reads a run of digits and returns its value and its length.
func (sc *scanner) number() (float64, int) {
	start := sc.pos
	var v float64
	for !sc.done() && isDigit(sc.s[sc.pos]) {
		v = v*10 + float64(sc.s[sc.pos]-'0')
		sc.pos++
	}
	return v, sc.pos - start
}

// timeField reads a two-digit field, allowing a single digit when relaxed.
func (sc *scanner) timeField(relaxed bool) (float64, bool) {
	if !relaxed {
		return sc.fixed(2)
	}
	v, n := sc.number()
	return v, n >= 1 && n <= 2
}

// fraction reads the digits after a decimal point as milliseconds, truncated.
func (sc *scanner) fraction() (float64, bool) {
	var ms float64
	n := 0
	for !sc.done() && isDigit(sc.s[sc.pos]) {
		if n < 3 {
			ms = ms*10 + float64(sc.s[sc.pos]-'0')
		}
		n++
		sc.pos++
	}
	for i := n; i < 3; i++ {
		ms *= 10
	}
	return ms, n > 0
}

// daysInMonth returns the number of days in the month m (1 to 12) of the year y.
func daysInMonth(y, m float64) float64 {
	if m == 2 && DaysInYear(y) == 366 {
		return 29
	}
	return cumulativeDays[int(m)] - cumulativeDays[int(m)-1]
}

// validTime reports whether the time of day in f is in range. 24:00 is the end of the day.
func (f *fields) validTime() bool {
	if f.hour == 24 {
		return f.minute == 0 && f.second == 0 && f.millisecond == 0
	}
	return f.hour <= 23 && f.minute <= 59 && f.second <= 59
}

// parseDateTime parses ISO and space-separated date-time strings.
func parseDateTime(s string, tz TimeZone) (float64, bool) {
	sc := scanner{
		s: s,
	}
	f := fields{
		month: 1,
		day:   1,
	}
	var ok bool
	switch c := sc.peek(); {
	case c == '+' || c == '-':
		sc.pos++
		if f.year, ok = sc.fixed(6); !ok {
			return 0, false
		}
		if c == '-' {
			if f.year == 0 {
				return 0, false
			}
			f.year = -f.year
		}
	default:
		if f.year, ok = sc.fixed(4); !ok {
			return 0, false
		}
	}
	isoDate := true
	var hasDay bool
	if sc.accept('-') {
		var n int
		f.month, n = sc.number()
		if n < 1 || n > 2 || f.month < 1 || f.month > 12 {
			return 0, false
		}
		isoDate = n == 2
		if sc.accept('-') {
			f.day, n = sc.number()
			if n < 1 || n > 2 || f.day < 1 || f.day > daysInMonth(f.year, f.month) {
				return 0, false
			}
			isoDate = isoDate && n == 2
			hasDay = true
		}
	}
	if sc.done() {
		if !isoDate {
			return 0, false
		}
		// A date-only form is UTC.
		f.hasOffset = true
		return f.timeValue(tz), true
	}
	var relaxed bool
	if sc.accept('T') {
		if !isoDate {
			return 0, false
		}
	} else if sc.accept(' ') && hasDay {
		relaxed = true
	} else {
		return 0, false
	}
	if f.hour, ok = sc.timeField(relaxed); !ok || !sc.accept(':') {
		return 0, false
	}
	if f.minute, ok = sc.timeField(relaxed); !ok {
		return 0, false
	}
	if sc.accept(':') {
		if f.second, ok = sc.timeField(relaxed); !ok {
			return 0, false
		}
		if sc.accept('.') {
			if f.millisecond, ok = sc.fraction(); !ok {
				return 0, false
			}
		}
	}
	if !f.validTime() {
		return 0, false
	}
	switch c := sc.peek(); {
	case sc.done():
	case c == 'Z':
		sc.pos++
		f.hasOffset = true
	case c == '+' || c == '-':
		sc.pos++
		h, ok := sc.fixed(2)
		if !ok || h > 23 {
			return 0, false
		}
		var m float64
		if !relaxed || !sc.done() {
			if !sc.accept(':') && !relaxed {
				return 0, false
			}
			if m, ok = sc.fixed(2); !ok || m > 59 {
				return 0, false
			}
		}
		f.offset = h*msPerHour + m*msPerMinute
		if c == '-' {
			f.offset = -f.offset
		}
		f.hasOffset = true
	}
	if !sc.done() {
		return 0, false
	}
	return f.timeValue(tz), true
}

// parseFormatted parses the formats of String and UTCString.
func parseFormatted(s string, tz TimeZone) (float64, bool) {
	if len(s) < 29 {
		return 0, false
	}
	// Only exact abbreviations enter this path.
	if matchName(s[:3], weekdayNames[:]) < 0 {
		return 0, false
	}
	var f fields
	f.hasOffset = true
	sc := scanner{
		s:   s,
		pos: 4,
	}
	var month string
	var ok bool
	utc := s[3] == ','
	if utc {
		if s[4] != ' ' {
			return 0, false
		}
		sc.pos = 5
		if f.day, ok = sc.fixed(2); !ok || !sc.accept(' ') {
			return 0, false
		}
		month = s[8:11]
		sc.pos = 11
	} else {
		if s[3] != ' ' || s[7] != ' ' {
			return 0, false
		}
		month = s[4:7]
		sc.pos = 8
		if f.day, ok = sc.fixed(2); !ok {
			return 0, false
		}
	}
	m := matchName(month, monthNames[:])
	// A leading number above 31 can be a year in the legacy grammar.
	if m < 0 || f.day > 31 || !sc.accept(' ') {
		return 0, false
	}
	f.month = float64(m + 1)
	neg := sc.accept('-')
	y, n := sc.number()
	if n < 4 || n > 6 || !sc.accept(' ') {
		return 0, false
	}
	f.year = y
	if neg {
		f.year = -y
	}
	if f.hour, ok = sc.fixed(2); !ok || !sc.accept(':') {
		return 0, false
	}
	if f.minute, ok = sc.fixed(2); !ok || !sc.accept(':') {
		return 0, false
	}
	if f.second, ok = sc.fixed(2); !ok {
		return 0, false
	}
	if !strings.HasPrefix(s[sc.pos:], " GMT") {
		return 0, false
	}
	sc.pos += 4
	if !utc {
		sign := sc.peek()
		if sign != '+' && sign != '-' {
			return 0, false
		}
		sc.pos++
		h, ok := sc.fixed(2)
		if !ok {
			return 0, false
		}
		m, ok := sc.fixed(2)
		if !ok {
			return 0, false
		}
		f.offset = h*msPerHour + m*msPerMinute
		if sign == '-' {
			f.offset = -f.offset
		}
		if !sc.done() {
			tail := s[sc.pos:]
			if len(tail) < 3 || !strings.HasPrefix(tail, " (") || tail[len(tail)-1] != ')' || strings.ContainsAny(tail[2:len(tail)-1], "()") {
				return 0, false
			}
			sc.pos = len(s)
		}
	}
	if !sc.done() {
		return 0, false
	}
	if f.day < 1 || !f.validTime() {
		return math.NaN(), true
	}
	return f.timeValue(tz), true
}

var fullMonthNames = [12]string{"january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"}

var fullWeekdayNames = [7]string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

// matchName reports the index of the name in names that word is, either in full or
// abbreviated to its first three letters, or -1.
func matchName(word string, names []string) int {
	for i, n := range names {
		if word == n || len(word) == 3 && word == n[:3] {
			return i
		}
	}
	return -1
}

// legacyYear interprets a year of a legacy format with n digits.
func legacyYear(y float64, n int) float64 {
	if n <= 2 {
		if y < 50 {
			return 2000 + y
		}
		return 1900 + y
	}
	return y
}

// parseLegacy parses the formats of [String] and [UTCString] and some common legacy formats.
func parseLegacy(s string, tz TimeZone) float64 {
	nan := math.NaN()
	sc := scanner{s: s}
	f := fields{}

	// nums are the numbers that are not part of a time or a slash date, with their digit counts.
	var nums [2]float64
	var numLens [2]int
	var numNeg [2]bool
	numCount := 0

	var hasMonth, hasSlashDate, hasTime, hasZone, hasNumericOffset bool
	ampm := 0 // 1 for AM and 2 for PM.

	for !sc.done() {
		c := sc.peek()
		switch {
		case c == ' ' || c == ',' || c == '\t' || c == '\n' || c == '\r':
			sc.pos++

		case c == '(':
			// A comment, such as the name of the time zone.
			depth := 0
			for {
				if sc.done() {
					return nan
				}
				switch sc.s[sc.pos] {
				case '(':
					depth++
				case ')':
					depth--
				}
				sc.pos++
				if depth == 0 {
					break
				}
			}

		case isLetter(c):
			start := sc.pos
			for !sc.done() && isLetter(sc.s[sc.pos]) {
				sc.pos++
			}
			word := make([]byte, sc.pos-start)
			for i := range word {
				word[i] = s[start+i] | 0x20
			}
			w := string(word)
			switch {
			case w == "am" || w == "pm":
				if !hasTime || ampm != 0 {
					return nan
				}
				ampm = 1
				if w == "pm" {
					ampm = 2
				}
			case w == "gmt" || w == "utc" || w == "ut" || w == "z":
				if hasZone {
					return nan
				}
				hasZone = true
				f.hasOffset = true
			default:
				month := matchName(w, fullMonthNames[:])
				switch {
				case month >= 0:
					if hasMonth || hasSlashDate {
						return nan
					}
					hasMonth = true
					f.month = float64(month + 1)
					sc.accept('.')
				case matchName(w, fullWeekdayNames[:]) >= 0:
					if hasMonth || hasSlashDate || numCount > 0 {
						return nan
					}
					sc.accept('.')
				default:
					return nan
				}
			}

		case (c == '+' || c == '-') && hasTime:
			// An offset: +hh, +hhmm or +hh:mm.
			if hasNumericOffset {
				return nan
			}
			hasNumericOffset = true
			sc.pos++
			v, n := sc.number()
			var off float64
			switch {
			case n == 4:
				off = math.Floor(v/100)*msPerHour + modulo(v, 100)*msPerMinute
			case n == 1 || n == 2:
				off = v * msPerHour
				if sc.accept(':') {
					m, ok := sc.fixed(2)
					if !ok {
						return nan
					}
					off += m * msPerMinute
				}
			default:
				return nan
			}
			if c == '-' {
				off = -off
			}
			f.offset = off
			f.hasOffset = true
			hasZone = true

		case c == '-' || isDigit(c):
			neg := sc.accept('-')
			if !isDigit(sc.peek()) {
				return nan
			}
			v, n := sc.number()
			switch {
			case !neg && sc.peek() == ':':
				if hasTime {
					return nan
				}
				hasTime = true
				f.hour = v
				sc.pos++
				var ok bool
				if f.minute, ok = sc.fixed(2); !ok {
					return nan
				}
				if sc.accept(':') {
					if f.second, ok = sc.fixed(2); !ok {
						return nan
					}
					if sc.accept('.') {
						if f.millisecond, ok = sc.fraction(); !ok {
							return nan
						}
					}
				}
			case !neg && sc.peek() == '/':
				if hasSlashDate || hasMonth || numCount > 0 {
					return nan
				}
				hasSlashDate = true
				var parts [3]float64
				var lens [3]int
				parts[0], lens[0] = v, n
				for i := 1; i < 3; i++ {
					if !sc.accept('/') || !isDigit(sc.peek()) {
						return nan
					}
					parts[i], lens[i] = sc.number()
				}
				if lens[0] >= 3 || parts[0] > 31 {
					f.year, f.month, f.day = legacyYear(parts[0], lens[0]), parts[1], parts[2]
				} else {
					f.month, f.day, f.year = parts[0], parts[1], legacyYear(parts[2], lens[2])
				}
			default:
				if numCount == len(nums) {
					return nan
				}
				nums[numCount], numLens[numCount], numNeg[numCount] = v, n, neg
				numCount++
			}

		default:
			return nan
		}
	}

	switch {
	case hasSlashDate:
		if numCount != 0 {
			return nan
		}
	case hasMonth:
		if numCount != 2 {
			return nan
		}
		d, y := 0, 1
		if numLens[0] >= 3 || nums[0] > 31 || numNeg[0] {
			d, y = 1, 0
		}
		if numNeg[d] {
			return nan
		}
		f.day = nums[d]
		f.year = legacyYear(nums[y], numLens[y])
		if numNeg[y] {
			f.year = -nums[y]
		}
	default:
		return nan
	}

	if f.month < 1 || f.month > 12 || f.day < 1 || f.day > 31 {
		return nan
	}
	switch ampm {
	case 0:
		if !f.validTime() {
			return nan
		}
	default:
		if f.hour < 1 || f.hour > 12 || f.minute > 59 || f.second > 59 {
			return nan
		}
		f.hour = modulo(f.hour, 12)
		if ampm == 2 {
			f.hour += 12
		}
	}
	return f.timeValue(tz)
}
