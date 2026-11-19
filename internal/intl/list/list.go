// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package list formats lists as Intl.ListFormat does.
package list

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
)

// Part is a part of a formatted list.
type Part struct {
	// Type is "element" or "literal".
	Type  string
	Value string
}

// Formatter formats lists of a locale, type and style.
type Formatter struct {
	start, middle, end, pair string
	// language is the language subtag of the locale of the patterns.
	language string
}

// NewFormatter returns a formatter for the available locale l, the type ("conjunction",
// "disjunction" or "unit") and the style ("long", "short" or "narrow").
func NewFormatter(l, typ, style string) *Formatter {
	prefix := map[string]string{"conjunction": "c", "disjunction": "d", "unit": "u"}[typ] + style[:1]
	get := func(k string) string {
		v, _ := locale.LookupField(cldr.ListPatterns, l, prefix+k)
		return v
	}
	lang, _, _ := strings.Cut(locale.DataLocale(l), "-")
	return &Formatter{
		start:    get("s"),
		middle:   get("m"),
		end:      get("e"),
		pair:     get("2"),
		language: lang,
	}
}

// Format implements CreatePartsFromList.
func (f *Formatter) Format(list []string) []Part {
	switch len(list) {
	case 0:
		return nil
	case 1:
		return []Part{{Type: "element", Value: list[0]}}
	case 2:
		return deconstruct(f.contextual(f.pair, list[1]), []Part{{Type: "element", Value: list[0]}}, []Part{{Type: "element", Value: list[1]}})
	}
	pattern := func(i int) string {
		switch {
		case i == 0:
			return f.start
		case i < len(list)-2:
			return f.middle
		}
		return f.contextual(f.end, list[i+1])
	}
	// Each pattern nests the parts of the elements after i in its {1}. When {0} comes first in
	// every pattern, the parts are written from the front, and the ends of the patterns after
	// the last element.
	var parts []Part
	var ends []string
	for i := range len(list) - 1 {
		pre, mid, post, ok := splitPattern(pattern(i))
		if !ok {
			return f.formatNested(list, pattern)
		}
		parts = appendLiteral(parts, pre)
		parts = append(parts, Part{Type: "element", Value: list[i]})
		parts = appendLiteral(parts, mid)
		ends = append(ends, post)
	}
	parts = append(parts, Part{Type: "element", Value: list[len(list)-1]})
	for _, post := range slices.Backward(ends) {
		parts = appendLiteral(parts, post)
	}
	return parts
}

// formatNested is Format for any patterns, which the function pattern gives for each element
// but the last.
func (f *Formatter) formatNested(list []string, pattern func(i int) string) []Part {
	parts := []Part{{Type: "element", Value: list[len(list)-1]}}
	for i := len(list) - 2; i >= 0; i-- {
		parts = deconstruct(pattern(i), []Part{{Type: "element", Value: list[i]}}, parts)
	}
	return parts
}

// splitPattern splits the pattern p into the text before {0}, between {0} and {1}, and after
// {1}, and reports whether p has {0} and then {1} as its only placeables.
func splitPattern(p string) (pre, mid, post string, ok bool) {
	pre, rest, ok := strings.Cut(p, "{0}")
	if !ok || strings.Contains(pre, "{") {
		return "", "", "", false
	}
	mid, post, ok = strings.Cut(rest, "{1}")
	if !ok || strings.Contains(mid, "{") || strings.Contains(post, "{") {
		return "", "", "", false
	}
	return pre, mid, post, true
}

// appendLiteral appends a literal part of s to parts unless s is empty.
func appendLiteral(parts []Part, s string) []Part {
	if s == "" {
		return parts
	}
	return append(parts, Part{Type: "literal", Value: s})
}

// contextual returns the pattern p adjusted for next, the element that follows the
// conjunction, as ICU's ListFormatter does for Spanish and Hebrew.
func (f *Formatter) contextual(p, next string) string {
	switch {
	case f.language == "es" && p == "{0} y {1}" && changesToE(next):
		return "{0} e {1}"
	case f.language == "es" && p == "{0} o {1}" && changesToU(next):
		return "{0} u {1}"
	case f.language == "he" && p == "{0} ו{1}" && next != "":
		if r, _ := utf8.DecodeRuneInString(next); !unicode.Is(unicode.Hebrew, r) {
			return "{0} ו-{1}"
		}
	}
	return p
}

// changesToE reports whether the Spanish "y" becomes "e" before s: s starts with "i", or with
// "hi" not followed by "a" or "e".
func changesToE(s string) bool {
	s = strings.ToLower(s)
	if strings.HasPrefix(s, "i") {
		return true
	}
	return strings.HasPrefix(s, "hi") && !strings.HasPrefix(s, "hia") && !strings.HasPrefix(s, "hie")
}

// changesToU reports whether the Spanish "o" becomes "u" before s: s starts with "o", "ho" or
// "8", or is "11" or starts with "11 ".
func changesToU(s string) bool {
	l := strings.ToLower(s)
	return strings.HasPrefix(l, "o") || strings.HasPrefix(l, "ho") || strings.HasPrefix(l, "8") ||
		s == "11" || strings.HasPrefix(s, "11 ")
}

// deconstruct implements DeconstructPattern for a pattern with the placeables {0} and {1}.
func deconstruct(p string, first, second []Part) []Part {
	var out []Part
	for p != "" {
		i := strings.IndexByte(p, '{')
		if i < 0 || i+2 >= len(p) || p[i+2] != '}' || (p[i+1] != '0' && p[i+1] != '1') {
			out = append(out, Part{Type: "literal", Value: p})
			break
		}
		if i > 0 {
			out = append(out, Part{Type: "literal", Value: p[:i]})
		}
		if p[i+1] == '0' {
			out = append(out, first...)
		} else {
			out = append(out, second...)
		}
		p = p[i+3:]
	}
	return out
}
