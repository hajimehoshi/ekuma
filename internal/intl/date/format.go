// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package date

import (
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/intl/number"
	"github.com/hajimehoshi/ekuma/internal/temporal"
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
)

// Part is a part of a formatted date, with the type of the parts of formatToParts.
type Part struct {
	Type  string
	Value string
}

// localTime is the fields of a date and time in the proleptic Gregorian calendar.
type localTime struct {
	// year is the astronomical year, where 0 is 1 BC.
	year        int64
	month       int
	day         int
	weekday     int // 0 for Sunday
	hour        int
	minute      int
	second      int
	millisecond int
}

// civil returns the local time of t milliseconds since the epoch.
func civil(t int64) localTime {
	days := floorDiv(t, msPerDay)
	ms := t - days*msPerDay
	// The algorithm of civil_from_days by Howard Hinnant.
	z := days + 719468
	era := floorDiv(z, 146097)
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	m := mp + 3
	if m > 12 {
		m -= 12
	}
	if m <= 2 {
		y++
	}
	wd := int((days%7 + 11) % 7)
	return localTime{
		year:        y,
		month:       int(m),
		day:         int(d),
		weekday:     wd,
		hour:        int(ms / 3600000),
		minute:      int(ms / 60000 % 60),
		second:      int(ms / 1000 % 60),
		millisecond: int(ms % 1000),
	}
}

// weekdayKeys are the CLDR keys of the days of the week from Sunday.
var weekdayKeys = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// Formatter formats dates with a format.
type Formatter struct {
	f      *Format
	data   *localeData
	digits []string
	zones  *zoneFormatter
	tz     temporal.TimeZone
	// dayPeriodRules are the rules of the flexible day periods of the locale, or nil.
	dayPeriodRules []dayPeriodRule
	// ranges is the range format, created when it is first needed.
	ranges *rangeFormat
}

// NewFormatter returns a formatter of f with the numbering system nu in the time zone tz.
func NewFormatter(f *Format, nu string, tz temporal.TimeZone) *Formatter {
	t, _ := locale.Parse(f.locale)
	d := newLocaleData(t.BaseName(), f.calendar)
	digits := number.DigitsOf(nu)
	return &Formatter{
		f:      f,
		data:   d,
		digits: digits,
		zones: &zoneFormatter{
			data:   d,
			region: region(t),
			digits: digits,
		},
		tz:             tz,
		dayPeriodRules: dayPeriodRulesOf(t),
	}
}

// Format returns the parts of the date t milliseconds since the epoch. A plain date is
// formatted as wall-clock time, without a time zone.
func (fm *Formatter) Format(t int64, plain bool) []Part {
	return fm.formatItems(fm.f.items, t, plain)
}

// zoneAt returns the time zone at t, which is UTC for a plain date.
func (fm *Formatter) zoneAt(t int64, plain bool) zoneAt {
	tz := fm.tz
	if plain {
		tz = temporal.UTC
	}
	return zoneAt{
		tz:     tz,
		t:      t,
		offset: offsetAt(tz, t),
	}
}

func (fm *Formatter) formatItems(items []item, t int64, plain bool) []Part {
	za := fm.zoneAt(t, plain)
	lt := civil(t + za.offset)
	hasMinute, hasSecond := false, false
	for _, it := range items {
		switch it.ch {
		case 'm':
			hasMinute = true
		case 's':
			hasSecond = true
		}
	}
	var parts []Part
	lit := func(s string) {
		if n := len(parts); n > 0 && parts[n-1].Type == "literal" {
			parts[n-1].Value += s
			return
		}
		parts = append(parts, Part{Type: "literal", Value: s})
	}
	for _, it := range items {
		if it.ch == 0 {
			lit(it.lit)
			continue
		}
		typ, v := fm.field(it, lt, za, hasMinute, hasSecond)
		if typ == "literal" {
			lit(v)
			continue
		}
		parts = append(parts, Part{Type: typ, Value: v})
	}
	for i := range parts {
		parts[i].Value = webCompatible(parts[i].Value)
	}
	return parts
}

// webCompatible returns s with the narrow no-break spaces that CLDR puts between times and
// day periods replaced with spaces, as browsers format dates for compatibility with the web.
func webCompatible(s string) string {
	return strings.ReplaceAll(s, "\u202f", " ")
}

// num returns n in decimal with at least w digits in the numbering system.
func (fm *Formatter) num(n int64, w int) string {
	return number.Transliterate(fm.digits, pad(n, w))
}

// name returns the calendar name with the key, falling back to the other context.
func (fm *Formatter) name(kind, ctx, width, key string) string {
	if v, ok := fm.data.calendar(kind + ctx + width + key); ok {
		return v
	}
	other := "f"
	if ctx == "f" {
		other = "s"
	}
	if v, ok := fm.data.calendar(kind + other + width + key); ok {
		return v
	}
	if width == "s" {
		return fm.name(kind, ctx, "a", key)
	}
	return key
}

// nameWidth returns the width letter of the records for a text field of length n.
func nameWidth(n int) string {
	switch n {
	case 4:
		return "w"
	case 5:
		return "n"
	case 6:
		return "s"
	}
	return "a"
}

func (fm *Formatter) field(it item, lt localTime, za zoneAt, hasMinute, hasSecond bool) (string, string) {
	n := it.n
	switch it.ch {
	case 'G':
		era := "1"
		if lt.year <= 0 {
			era = "0"
		}
		w := "a"
		switch n {
		case 4:
			w = "n"
		case 5:
			w = "r"
		}
		v, ok := fm.data.calendar("E" + w + era)
		if !ok {
			v, _ = fm.data.calendar("Ea" + era)
		}
		return "era", v
	case 'y':
		y := lt.year
		if y <= 0 {
			y = 1 - y
		}
		if n == 2 {
			return "year", fm.num(y%100, 2)
		}
		return "year", fm.num(y, n)
	case 'u', 'r':
		y := lt.year
		s := strconv.FormatInt(abs64(y), 10)
		for len(s) < n {
			s = "0" + s
		}
		if y < 0 {
			s = "-" + s
		}
		typ := "year"
		if it.ch == 'r' {
			typ = "relatedYear"
		}
		return typ, number.Transliterate(fm.digits, s)
	case 'M', 'L':
		if n <= 2 {
			return "month", fm.num(int64(lt.month), n)
		}
		ctx := "f"
		if it.ch == 'L' {
			ctx = "s"
		}
		return "month", fm.name("M", ctx, nameWidth(n), strconv.Itoa(lt.month))
	case 'd':
		return "day", fm.num(int64(lt.day), n)
	case 'E', 'c', 'e':
		if it.ch != 'E' && n <= 2 {
			return "weekday", fm.num(int64(lt.weekday+1), n)
		}
		ctx := "f"
		if it.ch == 'c' {
			ctx = "s"
		}
		return "weekday", fm.name("D", ctx, nameWidth(n), weekdayKeys[lt.weekday])
	case 'Q', 'q':
		q := (lt.month-1)/3 + 1
		if n <= 2 {
			return "quarter", fm.num(int64(q), n)
		}
		ctx := "f"
		if it.ch == 'q' {
			ctx = "s"
		}
		return "quarter", fm.name("Q", ctx, nameWidth(n), strconv.Itoa(q))
	case 'a':
		return "dayPeriod", fm.ampm(lt.hour, n)
	case 'b':
		key := ""
		switch {
		case lt.hour == 12 && lt.minute == 0 && lt.second == 0:
			key = "noon"
		case lt.hour == 0 && lt.minute == 0 && lt.second == 0:
			key = "midnight"
		}
		if key != "" {
			if v, ok := fm.data.calendar("Pf" + periodWidth(n) + key); ok {
				return "dayPeriod", v
			}
		}
		return "dayPeriod", fm.ampm(lt.hour, n)
	case 'B':
		return "dayPeriod", fm.flexibleDayPeriod(lt, n, hasMinute, hasSecond)
	case 'h':
		h := lt.hour % 12
		if h == 0 {
			h = 12
		}
		return "hour", fm.num(int64(h), n)
	case 'K':
		return "hour", fm.num(int64(lt.hour%12), n)
	case 'H':
		return "hour", fm.num(int64(lt.hour), n)
	case 'k':
		h := lt.hour
		if h == 0 {
			h = 24
		}
		return "hour", fm.num(int64(h), n)
	case 'm':
		return "minute", fm.num(int64(lt.minute), n)
	case 's':
		return "second", fm.num(int64(lt.second), n)
	case 'S':
		s := pad(int64(lt.millisecond), 3)
		for len(s) < n {
			s += "0"
		}
		return "fractionalSecond", number.Transliterate(fm.digits, s[:n])
	case 'z', 'v', 'V', 'O':
		return "timeZoneName", fm.zones.format(it.ch, n, za)
	}
	return "unknown", strings.Repeat(string(it.ch), n)
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// periodWidth returns the width letter of the records for a day period field of length n.
func periodWidth(n int) string {
	switch n {
	case 4:
		return "w"
	case 5:
		return "n"
	}
	return "a"
}

// ampm returns the name of the half of the day of hour.
func (fm *Formatter) ampm(hour, n int) string {
	key := "am"
	if hour >= 12 {
		key = "pm"
	}
	if v, ok := fm.data.calendar("Pf" + periodWidth(n) + key); ok {
		return v
	}
	v, _ := fm.data.calendar("Pfa" + key)
	return v
}

// dayPeriodRule is a rule of the flexible day periods.
type dayPeriodRule struct {
	name string
	// at is the hour of a period at an hour, or -1.
	at int
	// from and before are the hours of a period of hours.
	from, before int
}

// dayPeriodRulesOf returns the rules of the day periods of the locale tag t, or nil when it
// has none.
func dayPeriodRulesOf(t locale.Tag) []dayPeriodRule {
	l := t.BaseName()
	for {
		if v, ok := cldr.DayPeriodRules.Lookup(l); ok {
			var rules []dayPeriodRule
			for r := range strings.SplitSeq(v, ";") {
				f := strings.Fields(r)
				rule := dayPeriodRule{
					name: f[0],
					at:   -1,
				}
				if len(f) == 2 {
					rule.at, _ = strconv.Atoi(f[1])
				} else {
					rule.from, _ = strconv.Atoi(f[1])
					rule.before, _ = strconv.Atoi(f[2])
				}
				rules = append(rules, rule)
			}
			return rules
		}
		i := strings.LastIndexByte(l, '-')
		if i < 0 {
			return nil
		}
		l = l[:i]
	}
}

// periodOfHour returns the name of the day period of the hour.
func periodOfHour(rules []dayPeriodRule, hour int) string {
	for _, r := range rules {
		if r.at >= 0 {
			continue
		}
		if r.from < r.before {
			if r.from <= hour && hour < r.before {
				return r.name
			}
		} else if hour >= r.from || hour < r.before {
			return r.name
		}
	}
	if hour < 12 {
		return "am"
	}
	return "pm"
}

// hasRule reports whether rules have a period at an hour with the name.
func hasRule(rules []dayPeriodRule, name string) bool {
	for _, r := range rules {
		if r.at >= 0 && r.name == name {
			return true
		}
	}
	return false
}

// flexibleDayPeriod returns the flexible day period of lt.
func (fm *Formatter) flexibleDayPeriod(lt localTime, n int, hasMinute, hasSecond bool) string {
	rules := fm.dayPeriodRules
	if rules == nil {
		return fm.ampm(lt.hour, n)
	}
	minute, second := 0, 0
	if hasMinute {
		minute = lt.minute
	}
	if hasSecond {
		second = lt.second
	}
	var period string
	switch {
	case lt.hour == 12 && minute == 0 && second == 0 && hasRule(rules, "noon"):
		period = "noon"
	case lt.hour == 0 && minute == 0 && second == 0 && hasRule(rules, "midnight"):
		period = "midnight"
	default:
		period = periodOfHour(rules, lt.hour)
	}
	lookup := func(p string) (string, bool) {
		if p == "am" || p == "pm" || p == "midnight" {
			return "", false
		}
		return fm.data.calendar("Pf" + periodWidth(n) + p)
	}
	v, ok := lookup(period)
	if !ok {
		period = periodOfHour(rules, lt.hour)
		v, ok = lookup(period)
	}
	if !ok {
		return fm.ampm(lt.hour, n)
	}
	return v
}
