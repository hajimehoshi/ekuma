// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package temporal

import (
	"strconv"
	"strings"
)

// Goal is a goal symbol of the grammar of ISO 8601 / RFC 9557 strings.
type Goal int

const (
	GoalZonedDateTime Goal = iota // TemporalDateTimeString[+Zoned]
	GoalDateTime                  // TemporalDateTimeString[~Zoned]
	GoalInstant                   // TemporalInstantString
	GoalTime                      // TemporalTimeString
	GoalMonthDay                  // TemporalMonthDayString
	GoalYearMonth                 // TemporalYearMonthString
)

// AnyGoal lists all the goals but the duration, in the order that the specification tries
// them when it parses time zones and calendars.
var AnyGoal = []Goal{GoalZonedDateTime, GoalDateTime, GoalInstant, GoalTime, GoalMonthDay, GoalYearMonth}

// ParseResult is an ISO Date-Time Parse Record.
type ParseResult struct {
	// Year is the year, unless YearAbsent is true.
	Year       int
	YearAbsent bool
	Month      int
	Day        int

	// Time is the time of day, unless StartOfDay is true.
	Time       Time
	StartOfDay bool

	// Z, Offset and TimeZoneAnnotation are the ISO String Time Zone Parse Record.
	Z                  bool
	Offset             string
	TimeZoneAnnotation string

	// Calendar is the calendar annotation, or empty when there was none.
	Calendar string
}

// scanner reads a string by the productions of the grammar.
type scanner struct {
	s string
	i int
}

func (p *scanner) eof() bool {
	return p.i >= len(p.s)
}

func (p *scanner) peek() byte {
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *scanner) accept(c byte) bool {
	if p.peek() == c && !p.eof() {
		p.i++
		return true
	}
	return false
}

func (p *scanner) acceptFold(c byte) bool {
	if b := p.peek(); !p.eof() && (b == c || b == c+'a'-'A') {
		p.i++
		return true
	}
	return false
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isAlpha(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// digits reads exactly n decimal digits.
func (p *scanner) digits(n int) (int, bool) {
	if p.i+n > len(p.s) {
		return 0, false
	}
	v := 0
	for j := range n {
		c := p.s[p.i+j]
		if !isDigit(c) {
			return 0, false
		}
		v = v*10 + int(c-'0')
	}
	p.i += n
	return v, true
}

// twoDigits reads two digits whose value is at most maximum.
func (p *scanner) twoDigits(maximum int) (int, bool) {
	save := p.i
	v, ok := p.digits(2)
	if !ok || v > maximum {
		p.i = save
		return 0, false
	}
	return v, true
}

// dateYear reads DateYear.
func (p *scanner) dateYear() (int, bool) {
	save := p.i
	if c := p.peek(); c == '+' || c == '-' {
		p.i++
		v, ok := p.digits(6)
		if !ok || (c == '-' && v == 0) {
			p.i = save
			return 0, false
		}
		if c == '-' {
			v = -v
		}
		return v, true
	}
	v, ok := p.digits(4)
	if !ok {
		p.i = save
	}
	return v, ok
}

func (p *scanner) dateMonth() (int, bool) {
	save := p.i
	v, ok := p.twoDigits(12)
	if !ok || v == 0 {
		p.i = save
		return 0, false
	}
	return v, true
}

func (p *scanner) dateDay() (int, bool) {
	save := p.i
	v, ok := p.twoDigits(31)
	if !ok || v == 0 {
		p.i = save
		return 0, false
	}
	return v, true
}

// isValidMonthDay implements IsValidMonthDay.
func isValidMonthDay(month, day int) bool {
	switch {
	case day == 31 && (month == 2 || month == 4 || month == 6 || month == 9 || month == 11):
		return false
	case month == 2 && day == 30:
		return false
	}
	return true
}

// date reads Date.
func (p *scanner) date() (y, m, d int, ok bool) {
	save := p.i
	if y, ok = p.dateYear(); !ok {
		return 0, 0, 0, false
	}
	ext := p.accept('-')
	if m, ok = p.dateMonth(); !ok {
		p.i = save
		return 0, 0, 0, false
	}
	if ext && !p.accept('-') {
		p.i = save
		return 0, 0, 0, false
	}
	if d, ok = p.dateDay(); !ok {
		p.i = save
		return 0, 0, 0, false
	}
	if !isValidMonthDay(m, d) || (m == 2 && d == 29 && !IsLeapYear(y)) {
		p.i = save
		return 0, 0, 0, false
	}
	return y, m, d, true
}

// fraction reads TemporalDecimalFraction and returns its digits.
func (p *scanner) fraction() (string, bool) {
	save := p.i
	if !p.accept('.') && !p.accept(',') {
		return "", false
	}
	start := p.i
	for p.i < len(p.s) && isDigit(p.s[p.i]) && p.i-start < 9 {
		p.i++
	}
	if p.i == start || (p.i < len(p.s) && isDigit(p.s[p.i])) {
		p.i = save
		return "", false
	}
	return p.s[start:p.i], true
}

// fractionNanoseconds returns the digits of a fraction as nanoseconds.
func fractionNanoseconds(digits string) int64 {
	digits = (digits + "000000000")[:9]
	v, _ := strconv.ParseInt(digits, 10, 64)
	return v
}

// timeSpec reads Time.
func (p *scanner) timeSpec() (Time, bool) {
	var t Time
	hour, ok := p.twoDigits(23)
	if !ok {
		return t, false
	}
	t.Hour = hour
	save := p.i
	ext := p.accept(':')
	minute, ok := p.twoDigits(59)
	if !ok {
		p.i = save
		return t, true
	}
	t.Minute = minute
	save = p.i
	if ext && !p.accept(':') {
		return t, true
	}
	second, ok := p.twoDigits(60)
	if !ok {
		p.i = save
		return t, true
	}
	if second == 60 {
		second = 59
	}
	t.Second = second
	if f, ok := p.fraction(); ok {
		ns := fractionNanoseconds(f)
		t.Millisecond = int(ns / 1_000_000)
		t.Microsecond = int(ns / 1000 % 1000)
		t.Nanosecond = int(ns % 1000)
	}
	return t, true
}

// utcOffset reads UTCOffset[SubMinutePrecision] and returns its source text.
func (p *scanner) utcOffset(subMinute bool) (string, bool) {
	start := p.i
	if c := p.peek(); c != '+' && c != '-' {
		return "", false
	}
	p.i++
	if _, ok := p.twoDigits(23); !ok {
		p.i = start
		return "", false
	}
	save := p.i
	ext := p.accept(':')
	if _, ok := p.twoDigits(59); !ok {
		p.i = save
		return p.s[start:p.i], true
	}
	if !subMinute {
		return p.s[start:p.i], true
	}
	save = p.i
	if ext && !p.accept(':') {
		return p.s[start:p.i], true
	}
	if _, ok := p.twoDigits(59); !ok {
		p.i = save
		return p.s[start:p.i], true
	}
	p.fraction()
	return p.s[start:p.i], true
}

// dateTimeUTCOffset reads DateTimeUTCOffset[Z] into r.
func (p *scanner) dateTimeUTCOffset(z bool, r *ParseResult) bool {
	if z && (p.accept('Z') || p.accept('z')) {
		r.Z = true
		return true
	}
	if off, ok := p.utcOffset(true); ok {
		r.Offset = off
		return true
	}
	return false
}

func isTZLeadingChar(c byte) bool {
	return isAlpha(c) || c == '.' || c == '_'
}

func isTZChar(c byte) bool {
	return isTZLeadingChar(c) || isDigit(c) || c == '-' || c == '+'
}

// tzIANAName reads TimeZoneIANAName.
func (p *scanner) tzIANAName() bool {
	start := p.i
	for {
		if !isTZLeadingChar(p.peek()) || p.eof() {
			p.i = start
			return false
		}
		p.i++
		for p.i < len(p.s) && isTZChar(p.s[p.i]) {
			p.i++
		}
		if p.peek() != '/' || p.eof() {
			return true
		}
		p.i++
	}
}

// timeZoneIdentifier reads TimeZoneIdentifier.
func (p *scanner) timeZoneIdentifier() bool {
	if _, ok := p.utcOffset(false); ok {
		return true
	}
	return p.tzIANAName()
}

// annotations reads TimeZoneAnnotation (when allowed) and Annotations into r. It reports an
// error for a critical unknown annotation or for an invalid calendar annotation.
func (p *scanner) annotations(r *ParseResult, timeZone bool) (bool, error) {
	if timeZone && p.peek() == '[' && !strings.Contains(p.s[p.i:min(len(p.s), strings.IndexByte(p.s[p.i:]+"]", ']')+p.i)], "=") {
		save := p.i
		p.i++
		p.accept('!')
		start := p.i
		if !p.timeZoneIdentifier() || !p.accept(']') {
			p.i = save
			return false, nil
		}
		r.TimeZoneAnnotation = p.s[start : p.i-1]
	}
	calendarCritical := false
	hasCalendar := false
	for !p.eof() {
		if !p.accept('[') {
			return false, nil
		}
		critical := p.accept('!')
		start := p.i
		if c := p.peek(); !(c >= 'a' && c <= 'z') && c != '_' {
			return false, nil
		}
		p.i++
		for p.i < len(p.s) {
			c := p.s[p.i]
			if (c >= 'a' && c <= 'z') || c == '_' || isDigit(c) || c == '-' {
				p.i++
				continue
			}
			break
		}
		key := p.s[start:p.i]
		if !p.accept('=') {
			return false, nil
		}
		start = p.i
		for {
			n := p.i
			for p.i < len(p.s) && (isAlpha(p.s[p.i]) || isDigit(p.s[p.i])) {
				p.i++
			}
			if p.i == n {
				return false, nil
			}
			if p.peek() != '-' || p.eof() {
				break
			}
			p.i++
		}
		value := p.s[start:p.i]
		if !p.accept(']') {
			return false, nil
		}
		if key == "u-ca" {
			if !hasCalendar {
				hasCalendar = true
				r.Calendar = value
				calendarCritical = critical
			} else if critical || calendarCritical {
				return true, rangeErrorf("more than one calendar annotation with a critical flag")
			}
		} else if critical {
			return true, rangeErrorf("unknown critical annotation %s", key)
		}
	}
	return true, nil
}

// parseAnnotatedDateTime reads AnnotatedDateTime[Zoned, TimeRequired], or TemporalInstantString
// when instant is true.
func parseAnnotatedDateTime(s string, zoned, timeRequired, instant bool) (ParseResult, bool, error) {
	p := scanner{
		s: s,
	}
	var r ParseResult
	y, m, d, ok := p.date()
	if !ok {
		return r, false, nil
	}
	r.Year, r.Month, r.Day = y, m, d
	r.StartOfDay = true
	if c := p.peek(); !p.eof() && (c == ' ' || c == 'T' || c == 't') {
		p.i++
		t, ok := p.timeSpec()
		if !ok {
			return r, false, nil
		}
		r.Time = t
		r.StartOfDay = false
		if !p.dateTimeUTCOffset(zoned || instant, &r) && instant {
			return r, false, nil
		}
	} else if timeRequired || instant {
		return r, false, nil
	}
	ok, err := p.annotations(&r, true)
	if !ok || err != nil {
		return r, ok, err
	}
	if zoned && r.TimeZoneAnnotation == "" {
		return r, false, nil
	}
	return r, true, nil
}

// parseAnnotatedTime reads AnnotatedTime.
func parseAnnotatedTime(s string) (ParseResult, bool, error) {
	p := scanner{
		s: s,
	}
	var r ParseResult
	designator := p.accept('T') || p.accept('t')
	t, ok := p.timeSpec()
	if !ok {
		return r, false, nil
	}
	r.Time = t
	p.dateTimeUTCOffset(false, &r)
	end := p.i
	ok, err := p.annotations(&r, true)
	if !ok || err != nil {
		return r, ok, err
	}
	if !designator {
		// Without the designator, a time that could be a month and a day, or a year and a
		// month, is not a time.
		text := s[:end]
		if _, ok := parseMonthDaySpec(text); ok {
			return r, false, nil
		}
		if _, _, ok := parseYearMonthSpec(text); ok {
			return r, false, nil
		}
	}
	return r, true, nil
}

// parseMonthDaySpec reads DateSpecMonthDay from the whole of s.
func parseMonthDaySpec(s string) ([2]int, bool) {
	p := scanner{
		s: s,
	}
	n, ok := p.monthDaySpec()
	return n, ok && p.eof()
}

func (p *scanner) monthDaySpec() ([2]int, bool) {
	save := p.i
	if p.accept('-') && !p.accept('-') {
		p.i = save
		return [2]int{}, false
	}
	m, ok := p.dateMonth()
	if !ok {
		p.i = save
		return [2]int{}, false
	}
	p.accept('-')
	d, ok := p.dateDay()
	if !ok || !isValidMonthDay(m, d) {
		p.i = save
		return [2]int{}, false
	}
	return [2]int{m, d}, true
}

// parseYearMonthSpec reads DateSpecYearMonth from the whole of s.
func parseYearMonthSpec(s string) (int, int, bool) {
	p := scanner{
		s: s,
	}
	y, m, ok := p.yearMonthSpec()
	return y, m, ok && p.eof()
}

func (p *scanner) yearMonthSpec() (int, int, bool) {
	save := p.i
	y, ok := p.dateYear()
	if !ok {
		return 0, 0, false
	}
	p.accept('-')
	m, ok := p.dateMonth()
	if !ok {
		p.i = save
		return 0, 0, false
	}
	return y, m, true
}

// parseGoal parses s as goal. It reports false when s is not of the goal.
func parseGoal(s string, goal Goal) (ParseResult, bool, error) {
	switch goal {
	case GoalZonedDateTime:
		return parseAnnotatedDateTime(s, true, false, false)
	case GoalDateTime:
		return parseAnnotatedDateTime(s, false, false, false)
	case GoalInstant:
		return parseAnnotatedDateTime(s, false, false, true)
	case GoalTime:
		if r, ok, err := parseAnnotatedTime(s); ok || err != nil {
			return r, ok, err
		}
		return parseAnnotatedDateTime(s, false, true, false)
	case GoalMonthDay:
		p := scanner{
			s: s,
		}
		if md, ok := p.monthDaySpec(); ok {
			var r ParseResult
			ok, err := p.annotations(&r, true)
			if err != nil {
				return r, true, err
			}
			if ok {
				r.YearAbsent = true
				r.Month, r.Day = md[0], md[1]
				r.StartOfDay = true
				if r.Calendar != "" && !strings.EqualFold(r.Calendar, "iso8601") {
					return r, true, rangeErrorf("a month and a day without a year are only for the ISO 8601 calendar")
				}
				return r, true, nil
			}
		}
		return parseAnnotatedDateTime(s, false, false, false)
	case GoalYearMonth:
		p := scanner{
			s: s,
		}
		if y, m, ok := p.yearMonthSpec(); ok {
			var r ParseResult
			ok, err := p.annotations(&r, true)
			if err != nil {
				return r, true, err
			}
			if ok {
				r.Year, r.Month, r.Day = y, m, 1
				r.StartOfDay = true
				if r.Calendar != "" && !strings.EqualFold(r.Calendar, "iso8601") {
					return r, true, rangeErrorf("a year and a month without a day are only for the ISO 8601 calendar")
				}
				return r, true, nil
			}
		}
		return parseAnnotatedDateTime(s, false, false, false)
	}
	return ParseResult{}, false, nil
}

// ParseISODateTime implements ParseISODateTime.
func ParseISODateTime(s string, goals ...Goal) (ParseResult, error) {
	for _, g := range goals {
		r, ok, err := parseGoal(s, g)
		if err != nil {
			return ParseResult{}, err
		}
		if ok {
			return r, nil
		}
	}
	return ParseResult{}, rangeErrorf("invalid ISO 8601 string: %s", s)
}

// ParseTimeZoneIdentifier implements ParseTimeZoneIdentifier.
func ParseTimeZoneIdentifier(s string) (name string, offsetMinutes int, isOffset bool, err error) {
	p := scanner{
		s: s,
	}
	if off, ok := p.utcOffset(false); ok && p.eof() {
		ns, _ := ParseDateTimeUTCOffset(off)
		return "", int(ns / NsPerMinute), true, nil
	}
	p = scanner{
		s: s,
	}
	if p.tzIANAName() && p.eof() {
		return s, 0, false, nil
	}
	return "", 0, false, rangeErrorf("invalid time zone identifier: %s", s)
}

// ParseTemporalTimeZoneString implements ParseTemporalTimeZoneString.
func ParseTemporalTimeZoneString(s string) (name string, offsetMinutes int, isOffset bool, err error) {
	if name, off, isOff, err := ParseTimeZoneIdentifier(s); err == nil {
		return name, off, isOff, nil
	}
	r, err := ParseISODateTime(s, AnyGoal...)
	if err != nil {
		return "", 0, false, err
	}
	switch {
	case r.TimeZoneAnnotation != "":
		return ParseTimeZoneIdentifier(r.TimeZoneAnnotation)
	case r.Z:
		return "UTC", 0, false, nil
	case r.Offset != "":
		return ParseTimeZoneIdentifier(r.Offset)
	}
	return "", 0, false, rangeErrorf("no time zone in %s", s)
}

// ParseDateTimeUTCOffset implements ParseDateTimeUTCOffset.
func ParseDateTimeUTCOffset(s string) (int64, error) {
	p := scanner{
		s: s,
	}
	if _, ok := p.utcOffset(true); !ok || !p.eof() {
		return 0, rangeErrorf("invalid UTC offset: %s", s)
	}
	sign := int64(1)
	if s[0] == '-' {
		sign = -1
	}
	b := strings.ReplaceAll(s[1:], ":", "")
	hours, _ := strconv.ParseInt(b[0:2], 10, 64)
	var minutes, seconds, ns int64
	if len(b) >= 4 {
		minutes, _ = strconv.ParseInt(b[2:4], 10, 64)
	}
	if len(b) >= 6 {
		seconds, _ = strconv.ParseInt(b[4:6], 10, 64)
	}
	if len(b) > 7 {
		ns = fractionNanoseconds(b[7:])
	}
	return sign * (((hours*60+minutes)*60+seconds)*NsPerSecond + ns), nil
}

// IsOffsetWithoutSubMinutes reports whether the UTC offset string s has no seconds, so that
// it parses as UTCOffset[~SubMinutePrecision].
func IsOffsetWithoutSubMinutes(s string) bool {
	p := scanner{
		s: s,
	}
	_, ok := p.utcOffset(false)
	return ok && p.eof()
}

// IsAnnotationValue reports whether s is an AnnotationValue.
func IsAnnotationValue(s string) bool {
	for part := range strings.SplitSeq(s, "-") {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			if !isAlpha(part[i]) && !isDigit(part[i]) {
				return false
			}
		}
	}
	return true
}

// ParseTemporalDurationString implements ParseTemporalDurationString, without the creation of
// the Temporal.Duration. The fields still have to be validated with IsValidDuration.
func ParseTemporalDurationString(s string) (Duration, error) {
	var d Duration
	p := scanner{
		s: s,
	}
	negative := false
	if p.accept('-') {
		negative = true
	} else {
		p.accept('+')
	}
	if !p.acceptFold('P') {
		return d, rangeErrorf("invalid duration: %s", s)
	}
	fail := func() (Duration, error) {
		return Duration{}, rangeErrorf("invalid duration: %s", s)
	}
	// number reads DecimalDigits and an optional fraction.
	number := func(allowFraction bool) (string, string, bool, bool) {
		start := p.i
		for p.i < len(p.s) && isDigit(p.s[p.i]) {
			p.i++
		}
		if p.i == start {
			return "", "", false, false
		}
		digits := p.s[start:p.i]
		if allowFraction {
			if f, ok := p.fraction(); ok {
				return digits, f, true, true
			}
		}
		return digits, "", false, true
	}
	toNumber := func(digits string) float64 {
		v, _ := strconv.ParseFloat(digits, 64)
		return v
	}
	any := false
	dateDesignators := []byte{'Y', 'M', 'W', 'D'}
	dateFields := []*float64{&d.Years, &d.Months, &d.Weeks, &d.Days}
	next := 0
	for !p.eof() && isDigit(p.peek()) {
		save := p.i
		digits, _, _, _ := number(false)
		found := false
		for j := next; j < len(dateDesignators); j++ {
			if p.acceptFold(dateDesignators[j]) {
				*dateFields[j] = toNumber(digits)
				next = j + 1
				found = true
				break
			}
		}
		if !found {
			p.i = save
			return fail()
		}
		any = true
	}
	var excessNs int64
	if p.acceptFold('T') {
		timeDesignators := []byte{'H', 'M', 'S'}
		timeAny := false
		next := 0
		fractional := false
		for !p.eof() && isDigit(p.peek()) {
			if fractional {
				return fail()
			}
			digits, frac, hasFrac, _ := number(true)
			found := false
			for j := next; j < len(timeDesignators); j++ {
				if p.acceptFold(timeDesignators[j]) {
					v := toNumber(digits)
					switch j {
					case 0:
						d.Hours = v
						if hasFrac {
							excessNs = fractionNanoseconds(frac) * 3600
						}
					case 1:
						d.Minutes = v
						if hasFrac {
							excessNs = fractionNanoseconds(frac) * 60
						}
					case 2:
						d.Seconds = v
						if hasFrac {
							excessNs = fractionNanoseconds(frac)
						}
					}
					next = j + 1
					found = true
					break
				}
			}
			if !found {
				return fail()
			}
			fractional = hasFrac
			timeAny = true
		}
		if !timeAny {
			return fail()
		}
		any = true
	}
	if !any || !p.eof() {
		return fail()
	}
	d.Minutes += float64(excessNs / NsPerMinute)
	d.Seconds += float64(excessNs / NsPerSecond % 60)
	d.Milliseconds = float64(excessNs / NsPerMillisecond % 1000)
	d.Microseconds = float64(excessNs / NsPerMicrosecond % 1000)
	d.Nanoseconds = float64(excessNs % 1000)
	if negative {
		d = d.Negated()
	}
	return d, nil
}
