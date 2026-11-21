// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package date

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
	iso8601   bool
}

// calendarTables returns the tables of the data of the calendar cal, each falling back to the
// ones after it.
func calendarTables(cal string) []cldr.Map {
	common := []cldr.Map{cldr.GenericData, cldr.GregorianData}
	switch cal {
	case "buddhist":
		return append([]cldr.Map{cldr.BuddhistData}, common...)
	case "chinese":
		return append([]cldr.Map{cldr.ChineseData}, common...)
	case "coptic":
		return append([]cldr.Map{cldr.CopticData}, common...)
	case "dangi":
		return append([]cldr.Map{cldr.DangiData, cldr.ChineseData}, common...)
	case "ethioaa":
		return append([]cldr.Map{cldr.EthioaaData, cldr.EthiopicData}, common...)
	case "ethiopic":
		return append([]cldr.Map{cldr.EthiopicData}, common...)
	case "hebrew":
		return append([]cldr.Map{cldr.HebrewData}, common...)
	case "indian":
		return append([]cldr.Map{cldr.IndianData}, common...)
	case "islamic-civil":
		return append([]cldr.Map{cldr.IslamicCivilData, cldr.IslamicData}, common...)
	case "islamic-tbla":
		return append([]cldr.Map{cldr.IslamicTblaData, cldr.IslamicData}, common...)
	case "islamic-umalqura":
		return append([]cldr.Map{cldr.IslamicUmalquraData, cldr.IslamicData}, common...)
	case "japanese":
		return append([]cldr.Map{cldr.JapaneseData}, common...)
	case "persian":
		return append([]cldr.Map{cldr.PersianData}, common...)
	case "roc":
		return append([]cldr.Map{cldr.ROCData}, common...)
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
		iso8601:   cal == "iso8601",
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

// tables returns the calendar tables supplying the field key or prefix.
func (d *localeData) tables(key string) []cldr.Map {
	// ISO 8601 has its own patterns and inherits names and append items from Gregorian.
	if d.iso8601 && strings.ContainsRune("FTCASIN", rune(key[0])) {
		return []cldr.Map{cldr.ISO8601Patterns}
	}
	return d.calendars
}

// calendar returns the field key of the calendar data.
func (d *localeData) calendar(key string) (string, bool) {
	for _, m := range d.tables(key) {
		if v, ok := d.lookup(m, key); ok {
			return v, v != cldr.Absent
		}
	}
	return "", false
}

// calendarFields returns the fields of the calendar data whose keys start with prefix, by
// their keys without the prefix.
func (d *localeData) calendarFields(prefix string) map[string]string {
	out := map[string]string{}
	for _, m := range d.tables(prefix) {
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
	for k, v := range out {
		if v == cldr.Absent {
			delete(out, k)
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
