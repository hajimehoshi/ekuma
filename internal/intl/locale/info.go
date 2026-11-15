// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale

import (
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/intl/cldr"
)

// Calendars are the calendars that Intl supports, sorted.
var Calendars = []string{
	"buddhist",
	"chinese",
	"coptic",
	"dangi",
	"ethioaa",
	"ethiopic",
	"gregory",
	"hebrew",
	"indian",
	"islamic-civil",
	"islamic-tbla",
	"islamic-umalqura",
	"iso8601",
	"japanese",
	"persian",
	"roc",
}

// regionOfSubdivision returns the region of the Unicode subdivision value v of the keyword
// "rg" or "sd", and reports whether v is one.
func regionOfSubdivision(v string) (string, bool) {
	if len(v) < 3 || len(v) > 7 {
		return "", false
	}
	if isAlpha(v[:2]) {
		return strings.ToUpper(v[:2]), true
	}
	if len(v) >= 4 && isDigit(v[:3]) {
		return v[:3], true
	}
	return "", false
}

// regionPreference implements RegionPreference of ECMA-402 for t, which is in canonical form.
// It returns the region of t and the region override of the keyword "rg", if any.
func regionPreference(t Tag) (region, override string) {
	region = t.Region
	if region == "" {
		if v, ok := t.Keyword("sd"); ok {
			region, _ = regionOfSubdivision(v)
		}
	}
	if region == "" {
		if max, ok := AddLikelySubtags(t.LanguageID); ok {
			region = max.Region
		}
		if region == "" {
			region = "001"
		}
	}
	if v, ok := t.Keyword("rg"); ok {
		override, _ = regionOfSubdivision(v)
	}
	return region, override
}

// lookupRegion returns the region whose data in m applies to t: the region override of t if
// m has data for it, or else the region of t.
func lookupRegion(t Tag, m cldr.Map) string {
	region, override := regionPreference(t)
	if override != "" {
		if _, ok := m.Lookup(override); ok {
			return override
		}
	}
	return region
}

// regionData returns the value of m for region, or for the world when m has none.
func regionData(m cldr.Map, region string) string {
	if v, ok := m.Lookup(region); ok {
		return v
	}
	v, _ := m.Lookup("001")
	return v
}

// CalendarsOf implements CalendarsOfLocale of ECMA-402 for t, which is in canonical form.
func CalendarsOf(t Tag) []string {
	if v, ok := t.Keyword("ca"); ok {
		return []string{v}
	}
	var cals []string
	for c := range strings.FieldsSeq(regionData(cldr.CalendarPreferences, lookupRegion(t, cldr.CalendarPreferences))) {
		if _, ok := slices.BinarySearch(Calendars, c); ok {
			cals = append(cals, c)
		}
	}
	return cals
}

// collationsOf returns the collation types of the canonical locale s and its parents, other
// than "standard" and "search".
func collationsOf(s string) []string {
	var types []string
	for {
		if v, ok := cldr.Collations.Lookup(s); ok {
			for c := range strings.FieldsSeq(v) {
				if !slices.Contains(types, c) {
					types = append(types, c)
				}
			}
		}
		p, ok := parent(s, true)
		if !ok {
			break
		}
		s = p
	}
	slices.Sort(types)
	return types
}

// CollationsOf implements CollationsOfLocale of ECMA-402 for t, which is in canonical form.
func CollationsOf(t Tag) []string {
	if v, ok := t.Keyword("co"); ok {
		return []string{v}
	}
	return collationsOf(t.BaseName())
}

// Collations returns the collation types of all the locales, sorted.
func Collations() []string {
	var types []string
	for i := range cldr.Collations.Len() {
		for c := range strings.FieldsSeq(cldr.Collations.Value(i)) {
			if !slices.Contains(types, c) {
				types = append(types, c)
			}
		}
	}
	slices.Sort(types)
	return types
}

// HourCyclesOf implements HourCyclesOfLocale of ECMA-402 for t, which is in canonical form.
func HourCyclesOf(t Tag) []string {
	if v, ok := t.Keyword("hc"); ok {
		return []string{v}
	}
	region, override := regionPreference(t)
	var h string
	for _, r := range []string{override, region} {
		if r == "" {
			continue
		}
		if v, ok := cldr.PreferredHours.Lookup(t.Language + "-" + r); ok {
			h = v
			break
		}
		if v, ok := cldr.PreferredHours.Lookup(r); ok {
			h = v
			break
		}
	}
	if h == "" {
		h, _ = cldr.PreferredHours.Lookup("001")
	}
	switch h {
	case "K":
		return []string{"h11"}
	case "h":
		return []string{"h12"}
	case "k":
		return []string{"h24"}
	}
	return []string{"h23"}
}

// NumberingSystemOf returns the default numbering system of the available locale s.
func NumberingSystemOf(s string) string {
	if v, ok := cldr.DefaultNumberingSystems.Lookup(s); ok {
		return v
	}
	return "latn"
}

// NumberingSystemsOf implements NumberingSystemsOfLocale of ECMA-402 for t, which is in
// canonical form.
func NumberingSystemsOf(t Tag) []string {
	if v, ok := t.Keyword("nu"); ok {
		return []string{v}
	}
	if l, ok := BestAvailableLocale(t.BaseName()); ok {
		return []string{NumberingSystemOf(l)}
	}
	return []string{"latn"}
}

// NumberingSystems returns the numbering systems that have digits, sorted.
func NumberingSystems() []string {
	out := make([]string, cldr.NumberingSystemDigits.Len())
	for i := range out {
		out[i] = cldr.NumberingSystemDigits.Key(i)
	}
	return out
}

// TimeZonesOf implements TimeZonesOfLocale of ECMA-402 for t, which is in canonical form. It
// reports false when t has no region.
func TimeZonesOf(t Tag) ([]string, bool) {
	if t.Region == "" {
		return nil, false
	}
	return datetime.ZonesOfCountry(t.Region), true
}

// TextDirectionOf implements TextDirectionOfLocale of ECMA-402 for t, which is in canonical
// form. It returns "ltr", "rtl", or an empty string when the direction is unknown.
func TextDirectionOf(t Tag) string {
	script := t.Script
	if script == "" {
		if max, ok := AddLikelySubtags(t.LanguageID); ok {
			script = max.Script
		}
	}
	d, _ := cldr.ScriptDirections.Lookup(script)
	return d
}

// weekdays are the identifiers of the days of the week from Monday.
var weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// Weekday returns the number of the day of the week with the identifier s, from 1 for Monday
// to 7 for Sunday, or 0 when s is not one.
func Weekday(s string) int {
	return slices.Index(weekdays, s) + 1
}

// WeekInfoOf implements WeekInfoOfLocale of ECMA-402 for t, which is in canonical form. It
// returns the first day of the week and the days of the weekend, sorted, numbered from 1 for
// Monday to 7 for Sunday.
func WeekInfoOf(t Tag) (firstDay int, weekend []int) {
	region := lookupRegion(t, cldr.FirstDays)
	firstDay = Weekday(regionData(cldr.FirstDays, region))
	if v, ok := t.Keyword("fw"); ok {
		if d := Weekday(v); d > 0 {
			firstDay = d
		}
	}
	start := Weekday(regionData(cldr.WeekendStarts, region))
	end := Weekday(regionData(cldr.WeekendEnds, region))
	for d := start; ; d = d%7 + 1 {
		weekend = append(weekend, d)
		if d == end || len(weekend) == 7 {
			break
		}
	}
	slices.Sort(weekend)
	return firstDay, weekend
}

// TimeZones returns the primary identifiers of the available time zones, sorted.
func TimeZones() []string {
	var zones []string
	for _, z := range datetime.PrimaryZones() {
		if _, primary, _, ok := datetime.LoadZone(z); ok {
			zones = append(zones, primary)
		}
	}
	slices.Sort(zones)
	return slices.Compact(zones)
}
