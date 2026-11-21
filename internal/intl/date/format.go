// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package date

import (
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/intl/calendar"
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

// localTime is the fields of a date and time in a calendar.
type localTime struct {
	// year is the arithmetic year, where 0 is 1 BC in the Gregorian calendar.
	year int64
	// era is the CLDR key of the era, and eraYear the year in it, which is the year of the
	// sexagenary cycle for the Chinese and the Korean calendar.
	era     string
	eraYear int64
	// gregorianEra reports whether the era is one of the Gregorian calendar.
	gregorianEra bool
	// relatedYear is the Gregorian year in which the year starts.
	relatedYear int64
	// month is the number of the month in the CLDR data, which is the number of its month
	// code except in the Hebrew calendar, and numericMonth the number that a numeric month
	// shows, which is the ordinal month in the Hebrew calendar.
	month        int
	numericMonth int
	// leapMonth reports whether the month is a leap month of the Chinese or the Korean
	// calendar, and leapYear whether the year is a Hebrew leap year.
	leapMonth   bool
	leapYear    bool
	day         int
	weekday     int // 0 for Sunday
	hour        int
	minute      int
	second      int
	millisecond int
}

// cldrEras maps the eras of the calendars to the keys of their names in CLDR.
var cldrEras = map[string]string{
	"buddhist/be":         "0",
	"coptic/am":           "1",
	"ethioaa/aa":          "0",
	"ethiopic/aa":         "0",
	"ethiopic/am":         "1",
	"hebrew/am":           "0",
	"indian/shaka":        "0",
	"islamic-civil/ah":    "0",
	"islamic-civil/bh":    "1",
	"islamic-tbla/ah":     "0",
	"islamic-tbla/bh":     "1",
	"islamic-umalqura/ah": "0",
	"islamic-umalqura/bh": "1",
	"japanese/meiji":      "232",
	"japanese/taisho":     "233",
	"japanese/showa":      "234",
	"japanese/heisei":     "235",
	"japanese/reiwa":      "236",
	"persian/ap":          "0",
	"roc/broc":            "0",
	"roc/roc":             "1",
}

// localTime returns the local time of t milliseconds since the epoch in the calendar of fm.
func (fm *Formatter) localTime(t int64) localTime {
	lt := civil(t)
	lt.numericMonth = lt.month
	lt.relatedYear = lt.year
	lt.eraYear = lt.year
	lt.era = "1"
	if lt.year <= 0 {
		lt.era = "0"
		lt.eraYear = 1 - lt.year
	}
	if fm.cal == nil {
		return lt
	}
	d := fm.cal.Date(floorDiv(t, msPerDay))
	lt.year = int64(d.Year)
	lt.eraYear = int64(d.EraYear)
	lt.day = d.Day
	lt.month = d.MonthCode.Number
	lt.numericMonth = d.MonthCode.Number
	switch id := fm.cal.ID(); id {
	case "chinese", "dangi":
		lt.relatedYear = int64(d.Year)
		lt.eraYear = floorMod(int64(d.Year)-4, 60) + 1
		lt.leapMonth = d.MonthCode.Leap
	case "hebrew":
		// CLDR numbers the Hebrew months as in a leap year, with Adar II as Adar.
		if d.MonthCode.Leap || d.MonthCode.Number > 5 {
			lt.month++
		}
		lt.leapYear = d.InLeapYear
		lt.numericMonth = d.Month
	}
	if e, ok := cldrEras[fm.cal.ID()+"/"+d.Era]; ok {
		lt.era = e
	} else if d.Era == "" {
		lt.era = ""
	} else {
		// The Japanese calendar uses the eras of the Gregorian calendar before Meiji.
		lt.gregorianEra = true
	}
	return lt
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
	f *Format
	// cal is the calendar, or nil for the Gregorian and the ISO 8601 calendar.
	cal    *calendar.Calendar
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
		cal:    calendar.Lookup(f.calendar),
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
	return fm.formatItems(fm.f.items, t, plain, fm.f.numbers)
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

func (fm *Formatter) formatItems(items []item, t int64, plain bool, numbers string) []Part {
	za := fm.zoneAt(t, plain)
	lt := fm.localTime(t + za.offset)
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
		typ, v := fm.field(it, lt, za, hasMinute, hasSecond, numbers)
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

// weekYear returns the year of the locale's week at local time t, with weekday 0 for Sunday.
func (fm *Formatter) weekYear(t int64, weekday int) int64 {
	day := floorDiv(t, msPerDay)
	if fm.f.calendar == "iso8601" {
		_, y := temporal.EpochDaysToISODate(day).WeekOfYear()
		return int64(y)
	}
	tag, _ := locale.Parse(fm.f.locale)
	firstDay, minDays := locale.WeekRules(tag)
	// Count back from the last day of the week so that at least minDays
	// of the first week fall in the new year.
	daysSinceStart := (weekday - firstDay + 7) % 7
	day += int64(7 - minDays - daysSinceStart)
	// Buddhist and ROC week-years use Gregorian year numbers.
	if fm.cal == nil || fm.f.calendar == "buddhist" || fm.f.calendar == "roc" {
		return int64(temporal.EpochDaysToISODate(day).Year)
	}
	return int64(fm.cal.Date(day).Year)
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

func (fm *Formatter) field(it item, lt localTime, za zoneAt, hasMinute, hasSecond bool, numbers string) (string, string) {
	n := it.n
	switch it.ch {
	case 'G':
		w := "a"
		switch n {
		case 4:
			w = "n"
		case 5:
			w = "r"
		}
		if lt.gregorianEra {
			v, ok := fm.data.lookup(cldr.GregorianData, "E"+w+lt.era)
			if !ok {
				v, _ = fm.data.lookup(cldr.GregorianData, "Ea"+lt.era)
			}
			return "era", v
		}
		v, ok := fm.data.calendar("E" + w + lt.era)
		if !ok {
			v, _ = fm.data.calendar("Ea" + lt.era)
		}
		return "era", v
	case 'y':
		y := lt.eraYear
		if numbers == "y=jpanyear" {
			if y == 1 {
				return "year", "元"
			}
			return "year", strconv.FormatInt(y, 10)
		}
		if n == 2 {
			return "year", fm.num(floorMod(y, 100), 2)
		}
		return "year", fm.num(y, n)
	case 'Y', 'u', 'r':
		y := lt.year
		typ := "year"
		switch it.ch {
		case 'Y':
			y = fm.weekYear(za.t+za.offset, lt.weekday)
		case 'r':
			y = lt.relatedYear
			typ = "relatedYear"
		}
		magnitude := abs64(y)
		if it.ch == 'Y' && n == 2 {
			magnitude %= 100
		}
		s := pad(magnitude, n)
		if y < 0 {
			s = "-" + s
		}
		return typ, number.Transliterate(fm.digits, s)
	case 'U':
		if v, ok := fm.data.calendar("U" + strconv.FormatInt(lt.eraYear, 10)); ok {
			return "yearName", v
		}
		return "yearName", fm.num(lt.eraYear, 1)
	case 'M', 'L':
		ctx := "f"
		if it.ch == 'L' {
			ctx = "s"
		}
		if n <= 2 {
			return "month", fm.leapMonth(lt, "n", "l", fm.num(int64(lt.numericMonth), n))
		}
		key := strconv.Itoa(lt.month)
		if lt.leapYear && lt.month == 7 {
			if v, ok := fm.data.calendar("M" + ctx + nameWidth(n) + key + "-yeartype-leap"); ok {
				return "month", v
			}
		}
		return "month", fm.leapMonth(lt, ctx, nameWidth(n), fm.name("M", ctx, nameWidth(n), key))
	case 'd':
		if numbers == "d=hanidays" {
			return "day", hanDays[lt.day-1]
		}
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

// hanDays are the days of a lunar month in CLDR's hanidays numbering system.
var hanDays = [...]string{
	"初一", "初二", "初三", "初四", "初五", "初六", "初七", "初八", "初九", "初十",
	"十一", "十二", "十三", "十四", "十五", "十六", "十七", "十八", "十九", "二十",
	"廿一", "廿二", "廿三", "廿四", "廿五", "廿六", "廿七", "廿八", "廿九", "三十",
}

// leapMonth returns the month name or number v with the pattern of leap months of the context
// and the width when the month of lt is a leap month.
func (fm *Formatter) leapMonth(lt localTime, ctx, width, v string) string {
	if !lt.leapMonth {
		return v
	}
	p, ok := fm.data.calendar("L" + ctx + width + "leap")
	if !ok && ctx != "n" {
		p, ok = fm.data.calendar("L" + ctx + "aleap")
	}
	if !ok {
		return v
	}
	return strings.Replace(p, "{0}", v, 1)
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
