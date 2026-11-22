// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package number

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/intl/plural"
)

// RangePart is a part of a formatted range of numbers.
type RangePart struct {
	Type  string
	Value string
	// Source is "startRange", "endRange" or "shared".
	Source string
}

func withSource(parts []Part, source string) []RangePart {
	out := make([]RangePart, 0, len(parts))
	for _, p := range parts {
		out = append(out, RangePart{
			Type:   p.Type,
			Value:  p.Value,
			Source: source,
		})
	}
	return out
}

func codePoints(parts ...[]Part) int {
	n := 0
	for _, ps := range parts {
		for _, p := range ps {
			n += utf8.RuneCountInString(p.Value)
		}
	}
	return n
}

// FormatRange implements PartitionNumberRangePattern for x and y, which are not
// not-a-number. poll, which may be nil, is called now and then during a long number, and may
// stop the formatting by panicking.
func (f *Formatter) FormatRange(x, y Decimal, poll func()) []RangePart {
	xf := f.format(x, false, false, poll)
	yf := f.format(y, false, false, poll)
	if String(f.parts(xf)) == String(f.parts(yf)) {
		return withSource(f.parts(f.format(x, true, false, poll)), "shared")
	}

	// The unit or currency name is the same on both sides, so it is shown once.
	collapseOuter := true
	// A compact literal that supplies the entire number belongs to its endpoint.
	collapseMiddle := len(xf.number) > 0 && len(yf.number) > 0 && xf.middleKey == yf.middleKey &&
		String(xf.middlePrefix)+String(xf.middleSuffix) != "" &&
		codePoints(xf.middlePrefix, xf.middleSuffix) > 1
	if xf.compactMag < 0 && collapseMiddle {
		collapseMiddle = String(xf.middlePrefix) == String(yf.middlePrefix) && String(xf.middleSuffix) == String(yf.middleSuffix)
	}

	rangePattern := f.d.number("R")
	if !strings.Contains(rangePattern, "{0}") {
		rangePattern = "{0}–{1}"
	}
	_, rest, _ := strings.Cut(rangePattern, "{0}")
	sep, _, _ := strings.Cut(rest, "{1}")
	repeat := len(xf.exponent) > 0 ||
		!collapseMiddle && codePoints(xf.middlePrefix, xf.middleSuffix) > 0 ||
		!collapseOuter && codePoints(xf.outerPrefix, xf.outerSuffix) > 0
	if repeat && sep != "" {
		if r, _ := utf8.DecodeRuneInString(sep); !unicode.IsSpace(r) {
			sep = " " + sep
		}
		if r, _ := utf8.DecodeLastRuneInString(sep); !unicode.IsSpace(r) {
			sep += " "
		}
	}

	side := func(r *formatted) []Part {
		body := append(r.number[:len(r.number):len(r.number)], r.exponent...)
		if !collapseMiddle {
			body = f.wrapMiddle(r.middlePrefix, body, r.middleSuffix)
		}
		if !collapseOuter {
			body = append(append(r.outerPrefix[:len(r.outerPrefix):len(r.outerPrefix)], body...), r.outerSuffix...)
		}
		return body
	}
	start := side(xf)
	end := side(yf)

	var prefix, suffix []Part
	if collapseMiddle {
		m := xf
		if xf.compactMag >= 0 {
			// The compact symbol takes the plural form of the range.
			m = &formatted{
				compactMag: xf.compactMag,
				sign:       xf.sign,
			}
			c := plural.SelectRange(f.pluralLocale, f.pluralOf(xf.compactString, 0), f.pluralOf(yf.compactString, 0))
			p, _ := f.compact.pattern(xf.compactMag, c)
			f.setMiddle(m, false, p)
		}
		// The affixes surround the whole range, so the currency spacing depends on the
		// outermost digits.
		before, after := f.currencySpacing(m.middlePrefix, append(start[:len(start):len(start)], end...), m.middleSuffix)
		prefix = append(m.middlePrefix[:len(m.middlePrefix):len(m.middlePrefix)], before...)
		suffix = append(after, m.middleSuffix...)
	}
	if collapseOuter && xf.hasOuter {
		o := &formatted{}
		c := plural.SelectRange(f.pluralLocale, f.pluralOf(xf.pluralString, xf.pluralExponent), f.pluralOf(yf.pluralString, yf.pluralExponent))
		f.setOuter(o, c)
		prefix = append(o.outerPrefix[:len(o.outerPrefix):len(o.outerPrefix)], prefix...)
		suffix = append(suffix, o.outerSuffix...)
	}

	var out []RangePart
	out = append(out, withSource(prefix, "shared")...)
	out = append(out, withSource(start, "startRange")...)
	out = append(out, RangePart{
		Type:   "literal",
		Value:  sep,
		Source: "shared",
	})
	out = append(out, withSource(end, "endRange")...)
	out = append(out, withSource(suffix, "shared")...)
	return mergeRangeLiterals(out)
}

// mergeRangeLiterals merges adjacent literal parts of the same source.
func mergeRangeLiterals(parts []RangePart) []RangePart {
	var out []RangePart
	for _, p := range parts {
		if p.Value == "" {
			continue
		}
		if n := len(out); n > 0 && p.Type == "literal" && out[n-1].Type == "literal" && out[n-1].Source == p.Source {
			out[n-1].Value += p.Value
			continue
		}
		out = append(out, p)
	}
	return out
}
