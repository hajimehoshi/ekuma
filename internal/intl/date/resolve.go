// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package date

import (
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
)

// HourCycles returns the hour cycles of the locale tag l: its default, the one for 12-hour
// clocks and the one for 24-hour clocks.
func HourCycles(l string) (def, h12, h24 string) {
	t, _ := locale.Parse(l)
	def = locale.HourCyclesOf(t.WithoutUnicode())[0]
	allowed, _ := allowedHours(t)
	h12, h24 = "h12", "h23"
	found12, found24 := false, false
	for _, a := range allowed {
		switch a[0] {
		case 'K', 'h':
			if !found12 {
				found12 = true
				if a[0] == 'K' {
					h12 = "h11"
				}
			}
		case 'H', 'k':
			if !found24 {
				found24 = true
				if a[0] == 'k' {
					h24 = "h24"
				}
			}
		}
	}
	return def, h12, h24
}

// Required and defaults of TemporalComponents.
const (
	Date            = "date"
	Time            = "time"
	YearMonth       = "year-month"
	MonthDay        = "month-day"
	Any             = "any"
	All             = "all"
	ZonedDateTime   = "zoned-date-time"
	InheritAll      = "all"
	InheritRelevant = "relevant"
)

// get returns the component with the name of Table 16 of ECMA-402, with fractional second
// digits as "1", "2" or "3".
func (c *Components) get(name string) string {
	switch name {
	case "weekday":
		return c.Weekday
	case "era":
		return c.Era
	case "year":
		return c.Year
	case "month":
		return c.Month
	case "day":
		return c.Day
	case "dayPeriod":
		return c.DayPeriod
	case "hour":
		return c.Hour
	case "minute":
		return c.Minute
	case "second":
		return c.Second
	case "fractionalSecondDigits":
		if c.FractionalSecondDigits == 0 {
			return ""
		}
		return string(rune('0' + c.FractionalSecondDigits))
	case "timeZoneName":
		return c.TimeZoneName
	}
	return ""
}

// set sets the component with the name of Table 16 of ECMA-402.
func (c *Components) set(name, v string) {
	switch name {
	case "weekday":
		c.Weekday = v
	case "era":
		c.Era = v
	case "year":
		c.Year = v
	case "month":
		c.Month = v
	case "day":
		c.Day = v
	case "dayPeriod":
		c.DayPeriod = v
	case "hour":
		c.Hour = v
	case "minute":
		c.Minute = v
	case "second":
		c.Second = v
	case "fractionalSecondDigits":
		c.FractionalSecondDigits = 0
		if v != "" {
			c.FractionalSecondDigits = int(v[0] - '0')
		}
	case "timeZoneName":
		c.TimeZoneName = v
	}
}

// componentNames are the names of the components in the order of Table 16 of ECMA-402.
var componentNames = []string{"weekday", "era", "year", "month", "day", "dayPeriod", "hour", "minute", "second", "fractionalSecondDigits", "timeZoneName"}

// TemporalComponents implements the selection of the options of GetDateTimeFormat: it
// returns the components to format for the options c, and reports false for a null format.
func TemporalComponents(c Components, required, defaults, inherit string) (Components, bool) {
	var requiredOptions []string
	switch required {
	case Date:
		requiredOptions = []string{"weekday", "year", "month", "day"}
	case Time:
		requiredOptions = []string{"dayPeriod", "hour", "minute", "second", "fractionalSecondDigits"}
	case YearMonth:
		requiredOptions = []string{"year", "month"}
	case MonthDay:
		requiredOptions = []string{"month", "day"}
	default:
		requiredOptions = []string{"weekday", "year", "month", "day", "dayPeriod", "hour", "minute", "second", "fractionalSecondDigits"}
	}
	var defaultOptions []string
	switch defaults {
	case Date:
		defaultOptions = []string{"year", "month", "day"}
	case Time:
		defaultOptions = []string{"hour", "minute", "second"}
	case YearMonth:
		defaultOptions = []string{"year", "month"}
	case MonthDay:
		defaultOptions = []string{"month", "day"}
	default:
		defaultOptions = []string{"year", "month", "day", "hour", "minute", "second"}
	}
	var out Components
	if inherit == InheritAll {
		out = c
	} else if required == Date || required == YearMonth || required == Any {
		out.Era = c.Era
	}
	anyPresent := false
	for _, n := range []string{"weekday", "year", "month", "day", "dayPeriod", "hour", "minute", "second", "fractionalSecondDigits"} {
		if c.get(n) != "" {
			anyPresent = true
		}
	}
	needDefaults := true
	for _, n := range requiredOptions {
		if v := c.get(n); v != "" {
			out.set(n, v)
			needDefaults = false
		}
	}
	if needDefaults {
		if anyPresent && inherit == InheritRelevant {
			return Components{}, false
		}
		for _, n := range defaultOptions {
			out.set(n, "numeric")
		}
		if defaults == ZonedDateTime && out.TimeZoneName == "" {
			out.TimeZoneName = "short"
		}
	}
	return out, true
}

// Adjust implements AdjustDateTimeStyleFormat: it returns the format nearest to f with only
// the components of allowed, named as in Table 16 of ECMA-402, with the hour cycle hc.
func (f *Format) Adjust(hc string, allowed ...string) *Format {
	c := f.Components()
	conflict := false
	var out Components
	for _, n := range componentNames {
		v := c.get(n)
		if v == "" {
			continue
		}
		allow := false
		for _, a := range allowed {
			if a == n {
				allow = true
			}
		}
		if allow {
			out.set(n, v)
		} else {
			conflict = true
		}
	}
	if !conflict {
		return f
	}
	return NewFormat(f.locale, f.calendar, hc, out)
}

// HasHour reports whether f has an hour.
func (f *Format) HasHour() bool {
	return f.hourCycle != ""
}
