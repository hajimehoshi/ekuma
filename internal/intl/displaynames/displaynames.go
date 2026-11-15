// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package displaynames provides the display names of Intl.DisplayNames, as ICU's
// LocaleDisplayNames does.
package displaynames

import (
	"strings"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
)

// Names looks up the display names of a locale.
type Names struct {
	locale string
	// short reports whether the short forms are preferred.
	short bool
}

// New returns the names of the available locale l, preferring the short forms when short is
// true.
func New(l string, short bool) Names {
	return Names{
		locale: l,
		short:  short,
	}
}

func (n Names) field(m cldr.Map, key string) (string, bool) {
	return locale.LookupField(m, n.locale, key)
}

// name returns the name key of LocaleDisplayNames, or its short form "|s" when n prefers
// short forms and there is one.
func (n Names) name(key string) (string, bool) {
	if n.short {
		if v, ok := n.field(cldr.LocaleDisplayNames, key+"|s"); ok {
			return v, true
		}
	}
	return n.field(cldr.LocaleDisplayNames, key)
}

// Region returns the name of the region code in upper case.
func (n Names) Region(code string) (string, bool) {
	if n.short {
		if v, ok := n.field(cldr.LocaleDisplayNames, "r"+code); ok {
			return v, true
		}
	}
	return n.field(cldr.TerritoryNames, code)
}

// Script returns the name of the script code in title case.
func (n Names) Script(code string) (string, bool) {
	return n.name("s" + code)
}

// Calendar returns the name of the Unicode calendar type code.
func (n Names) Calendar(code string) (string, bool) {
	return n.field(cldr.LocaleDisplayNames, "c"+code)
}

// Currency returns the name of the currency code in upper case.
func (n Names) Currency(code string) (string, bool) {
	return n.field(cldr.CurrencyData, code+"d")
}

// dateTimeFields maps the codes of date-time fields of ECMA-402 to the names of the fields of
// CLDR.
var dateTimeFields = map[string]string{
	"era":          "era",
	"year":         "year",
	"quarter":      "quarter",
	"month":        "month",
	"weekOfYear":   "week",
	"weekday":      "weekday",
	"day":          "day",
	"dayPeriod":    "dayperiod",
	"hour":         "hour",
	"minute":       "minute",
	"second":       "second",
	"timeZoneName": "zone",
}

// IsDateTimeField reports whether code is a code of a date-time field of ECMA-402.
func IsDateTimeField(code string) bool {
	_, ok := dateTimeFields[code]
	return ok
}

// DateTimeField returns the name of the date-time field code in the style.
func (n Names) DateTimeField(code, style string) (string, bool) {
	f := dateTimeFields[code]
	if style != "long" {
		if v, ok := n.field(cldr.DateFieldNames, f+"-"+style); ok {
			return v, true
		}
	}
	return n.field(cldr.DateFieldNames, f)
}

// Language returns the name of the canonical unicode_language_id id, with the names of
// dialects such as "American English" when dialect is true. It reports false when a subtag
// has no name.
func (n Names) Language(id locale.LanguageID, dialect bool) (string, bool) {
	hasScript, hasRegion := id.Script != "", id.Region != ""
	var name string
	found := false
	if dialect {
		lang := id.Language
		if hasScript && hasRegion {
			if name, found = n.name("l" + lang + "-" + id.Script + "-" + id.Region); found {
				hasScript, hasRegion = false, false
			}
		}
		// As ICU, the names of the language with only the script or only the region
		// are not used for a locale that has both.
		if hasScript && !hasRegion {
			if name, found = n.name("l" + lang + "-" + id.Script); found {
				hasScript = false
			}
		}
		if hasRegion && !hasScript {
			if name, found = n.name("l" + lang + "-" + id.Region); found {
				hasRegion = false
			}
		}
	}
	if !found {
		v, ok := n.name("l" + id.Language)
		if !ok {
			return "", false
		}
		name = v
	}
	var qualifiers []string
	if hasScript {
		v, ok := n.name("s" + id.Script)
		if !ok {
			return "", false
		}
		qualifiers = append(qualifiers, v)
	}
	if hasRegion {
		v, ok := n.field(cldr.TerritoryNames, id.Region)
		if n.short {
			if s, sok := n.field(cldr.LocaleDisplayNames, "r"+id.Region); sok {
				v, ok = s, true
			}
		}
		if !ok {
			return "", false
		}
		qualifiers = append(qualifiers, v)
	}
	for _, variant := range id.Variants {
		v, ok := n.field(cldr.LocaleDisplayNames, "v"+strings.ToUpper(variant))
		if !ok {
			return "", false
		}
		qualifiers = append(qualifiers, v)
	}
	if len(qualifiers) == 0 {
		return name, true
	}
	pattern, _ := n.field(cldr.LocaleDisplayNames, "p")
	sep, _ := n.field(cldr.LocaleDisplayNames, "q")
	open, close, ropen, rclose := "(", ")", "[", "]"
	if strings.Contains(pattern, "（") {
		open, close, ropen, rclose = "（", "）", "［", "］"
	}
	rest := qualifiers[0]
	for _, q := range qualifiers[1:] {
		rest = strings.NewReplacer("{0}", rest, "{1}", q).Replace(sep)
	}
	rest = strings.NewReplacer(open, ropen, close, rclose).Replace(rest)
	return strings.NewReplacer("{0}", name, "{1}", rest).Replace(pattern), true
}
