// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package dates

import (
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
)

// localeData looks up the CLDR data of a locale, inheriting it from its ancestors.
type localeData struct {
	// chain is the CLDR locale whose data applies, followed by its ancestors.
	chain []string
	// calendars are the tables of the calendar, the first one that has a field winning.
	calendars []cldr.Map
}

// calendarTables returns the tables of the data of the calendar cal.
func calendarTables(cal string) []cldr.Map {
	switch cal {
	case "gregory", "iso8601":
		return []cldr.Map{cldr.GregorianData}
	}
	return []cldr.Map{cldr.GregorianData}
}

// newLocaleData returns the data of the available locale l for the calendar cal.
func newLocaleData(l, cal string) *localeData {
	var chain []string
	for c, ok := locale.DataLocale(l), true; ok; c, ok = locale.Parent(c) {
		chain = append(chain, c)
	}
	return &localeData{
		chain:     chain,
		calendars: calendarTables(cal),
	}
}

// lookup returns the field key of the records in m for the locale.
func (d *localeData) lookup(m cldr.Map, key string) (string, bool) {
	for _, l := range d.chain {
		r, ok := m.Lookup(l)
		if !ok {
			continue
		}
		if v, ok := cldr.Field(r, key); ok {
			return v, true
		}
	}
	return "", false
}

// calendar returns the field key of the calendar data.
func (d *localeData) calendar(key string) (string, bool) {
	for _, m := range d.calendars {
		if v, ok := d.lookup(m, key); ok {
			return v, true
		}
	}
	return "", false
}

// calendarFields returns the fields of the calendar data whose keys start with prefix, by
// their keys without the prefix.
func (d *localeData) calendarFields(prefix string) map[string]string {
	out := map[string]string{}
	for _, m := range d.calendars {
		for _, l := range d.chain {
			r, ok := m.Lookup(l)
			if !ok {
				continue
			}
			for k, v := range cldr.Fields(r) {
				k, ok := strings.CutPrefix(k, prefix)
				if !ok {
					continue
				}
				if _, ok := out[k]; !ok {
					out[k] = v
				}
			}
		}
	}
	return out
}

// sortedKeys returns the keys of m sorted.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// numberSymbol returns the number symbol of the locale for the numbering system nu, with the
// key of the records of cldr.NumberData.
func (d *localeData) numberSymbol(nu, key string) string {
	if v, ok := d.lookup(cldr.NumberData, nu+"."+key); ok {
		return v
	}
	v, _ := d.lookup(cldr.NumberData, "latn."+key)
	return v
}

// region returns the region of the locale tag t, or the region of its likely subtags.
func region(t locale.Tag) string {
	if r, ok := t.Keyword("rg"); ok && len(r) >= 2 {
		return strings.ToUpper(r[:2])
	}
	if t.Region != "" {
		return t.Region
	}
	if max, ok := locale.AddLikelySubtags(t.LanguageID); ok && max.Region != "" {
		return max.Region
	}
	return "001"
}

// allowedHours returns the hour formats that the locale tag t allows, most preferred first,
// and its preferred hour letter.
func allowedHours(t locale.Tag) (allowed []string, preferred byte) {
	lang := t.Language
	r := region(t)
	if t.Language == "" || t.Region == "" {
		if max, ok := locale.AddLikelySubtags(t.LanguageID); ok {
			lang = max.Language
		}
	}
	a, ok := cldr.AllowedHours.Lookup(lang + "-" + r)
	p, _ := cldr.PreferredHours.Lookup(lang + "-" + r)
	if !ok {
		a, ok = cldr.AllowedHours.Lookup(r)
		p, _ = cldr.PreferredHours.Lookup(r)
	}
	if !ok {
		return []string{"H"}, 'H'
	}
	if p == "" {
		p = "H"
	}
	return strings.Fields(a), p[0]
}
