// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package number

import (
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
)

// localeData looks up the number data of a locale, inheriting it from its ancestors.
type localeData struct {
	// chain is the locale of CLDR whose data applies, followed by its ancestors.
	chain []string
	// nu is the numbering system.
	nu string
}

func newLocaleData(available, nu string) *localeData {
	var chain []string
	for l, ok := locale.DataLocale(available), true; ok; l, ok = locale.Parent(l) {
		chain = append(chain, l)
	}
	return &localeData{
		chain: chain,
		nu:    nu,
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

// number returns the number data field of the numbering system, falling back to that of
// "latn".
func (d *localeData) number(field string) string {
	if v, ok := d.lookup(cldr.NumberData, d.nu+"."+field); ok {
		return v
	}
	v, _ := d.lookup(cldr.NumberData, "latn."+field)
	return v
}

// currencySymbol returns the number symbol of the field "d" or "g" for formatting the
// currency c. The locale's separator of the currency comes first, then its currency symbol of
// the field, then the plain symbol. It falls back to "latn" like [localeData.number].
func (d *localeData) currencySymbol(c, field string) string {
	key := c + "."
	if field == "g" {
		key = c + ","
	}
	if v, ok := d.lookup(cldr.CurrencyData, key); ok {
		return v
	}
	for _, nu := range []string{d.nu, "latn"} {
		if v, ok := d.lookup(cldr.NumberData, nu+".$"+field); ok {
			return v
		}
		if v, ok := d.lookup(cldr.NumberData, nu+"."+field); ok {
			return v
		}
	}
	return ""
}

// minimumGroupingDigits returns the minimum grouping digits of the locale.
func (d *localeData) minimumGroupingDigits() int {
	v, _ := d.lookup(cldr.NumberData, "g")
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// pluralLetters maps plural categories to the letters of the records of the locale data.
var pluralLetters = map[string]string{
	"zero":  "z",
	"one":   "o",
	"two":   "t",
	"few":   "f",
	"many":  "m",
	"other": "x",
}

// plural returns the field prefix followed by the letter of the plural category, falling
// back to "other".
func (d *localeData) plural(m cldr.Map, prefix, category string) (string, bool) {
	if v, ok := d.lookup(m, prefix+pluralLetters[category]); ok {
		return v, true
	}
	return d.lookup(m, prefix+"x")
}

// compact holds the patterns of compact decimal notation of one kind.
type compact struct {
	d *localeData
	// kind is the prefix of the fields of the patterns, such as "latn.s".
	kind string
}

// pattern returns the pattern for the magnitude mag and the plural category.
func (c compact) pattern(mag int, category string) (string, bool) {
	return c.d.plural(cldr.NumberData, c.kind+strconv.Itoa(mag)+".", category)
}

// maxCompactMagnitude is the largest magnitude that CLDR has compact patterns for.
const maxCompactMagnitude = 19

// exponentForMagnitude implements ComputeExponentForMagnitude for compact notation. It also
// returns the magnitude of the pattern to use, or -1 when no pattern applies.
func (c compact) exponentForMagnitude(mag int) (exponent, patternMag int) {
	for k := min(mag, maxCompactMagnitude); k >= 3; k-- {
		p, ok := c.pattern(k, "other")
		if !ok {
			continue
		}
		zeros := strings.Count(positiveSubpattern(p), "0")
		if zeros == 0 || isOnlyZeros(p) {
			return 0, -1
		}
		return k - (zeros - 1), k
	}
	return 0, -1
}

// isOnlyZeros reports whether the positive subpattern of the pattern p has nothing but
// zeros, which means that numbers are not compacted.
func isOnlyZeros(p string) bool {
	return strings.Trim(positiveSubpattern(p), "0") == ""
}

// positiveSubpattern returns the positive subpattern of the pattern p.
func positiveSubpattern(p string) string {
	return splitUnquoted(p, ';')[0]
}

// CompactExponent returns the exponent of compact decimal notation for the absolute value
// of the finite x, in the available locale l with its default numbering system. long selects
// the long form.
func CompactExponent(l string, long bool, x Decimal) int {
	if x.IsZero() {
		return 0
	}
	d := newLocaleData(l, locale.NumberingSystemOf(locale.DataLocale(l)))
	kind := "s"
	if long {
		kind = "l"
	}
	e, _ := d.compactOf(kind).exponentForMagnitude(x.magnitude())
	return e
}

// compactOf returns the compact patterns of the kind "s", "l" or "c" for the numbering
// system, falling back to those of "latn".
func (d *localeData) compactOf(kind string) compact {
	c := compact{
		d:    d,
		kind: d.nu + "." + kind,
	}
	if _, ok := c.pattern(3, "other"); !ok {
		c.kind = "latn." + kind
	}
	return c
}

// CurrencyDigits implements CurrencyDigits for the well-formed currency code c in upper
// case.
func CurrencyDigits(c string) int {
	v, ok := cldr.CurrencyFractions.Lookup(c)
	if !ok {
		v, _ = cldr.CurrencyFractions.Lookup("DEFAULT")
	}
	d, _, _ := strings.Cut(v, " ")
	n, err := strconv.Atoi(d)
	if err != nil {
		return 2
	}
	return n
}
