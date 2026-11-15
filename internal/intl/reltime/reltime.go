// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package reltime provides the patterns of relative times for Intl.RelativeTimeFormat.
package reltime

import (
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
)

// Patterns holds the patterns of a unit in a width for a locale.
type Patterns struct {
	locale string
	// prefix is the prefix of the keys of the patterns in the records.
	prefix string
}

// PatternsOf returns the patterns of the available locale l for the singular unit and the
// style ("long", "short" or "narrow").
func PatternsOf(l, unit, style string) Patterns {
	return Patterns{
		locale: l,
		prefix: unit + style[:1] + "|",
	}
}

// Named returns the name of the relative time at offset, such as "-1" for "yesterday" with
// the unit day, and reports whether there is one.
func (p Patterns) Named(offset string) (string, bool) {
	return locale.LookupField(cldr.RelativeTimeData, p.locale, p.prefix+offset)
}

// pluralLetters maps the plural categories to the letters of the keys of the records.
var pluralLetters = map[string]string{
	"zero":  "z",
	"one":   "o",
	"two":   "t",
	"few":   "f",
	"many":  "m",
	"other": "x",
}

// Pattern returns the pattern with the placeholder {0} for the future, or the past when past
// is true, and the plural category, falling back to that of "other".
func (p Patterns) Pattern(past bool, category string) string {
	tense := "f"
	if past {
		tense = "p"
	}
	if v, ok := locale.LookupField(cldr.RelativeTimeData, p.locale, p.prefix+tense+pluralLetters[category]); ok {
		return v
	}
	v, _ := locale.LookupField(cldr.RelativeTimeData, p.locale, p.prefix+tense+"x")
	return v
}
