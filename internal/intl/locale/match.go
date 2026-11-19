// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale

import (
	"strings"
	"sync"

	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
)

// defaultRegionLocales maps the language-region form of each available locale with a
// language, a script and a region whose script is the likely script of the language and
// region, as ECMA-402 asks of the available locales, to that locale.
var defaultRegionLocales = sync.OnceValue(func() map[string]string {
	m := map[string]string{}
	for i := range cldr.AvailableLocales.Len() {
		t, ok := Parse(cldr.AvailableLocales.At(i))
		if !ok || t.Script == "" || t.Region == "" || len(t.Variants) > 0 {
			continue
		}
		short := LanguageID{
			Language: t.Language,
			Region:   t.Region,
		}
		if max, ok := AddLikelySubtags(short); ok && max.Script == t.Script {
			m[short.String()] = t.String()
		}
	}
	return m
})

// maxAvailableLen is the length of the longest available locale.
var maxAvailableLen = sync.OnceValue(func() int {
	n := 0
	for i := range cldr.AvailableLocales.Len() {
		n = max(n, len(cldr.AvailableLocales.At(i)))
	}
	for s := range defaultRegionLocales() {
		n = max(n, len(s))
	}
	return n
})

// IsAvailable reports whether the canonical language tag s is an available locale.
func IsAvailable(s string) bool {
	if cldr.AvailableLocales.Contains(s) {
		return true
	}
	_, ok := defaultRegionLocales()[s]
	return ok
}

// DataLocale returns the locale of CLDR whose data applies to the available locale s.
func DataLocale(s string) string {
	if l, ok := defaultRegionLocales()[s]; ok && !cldr.AvailableLocales.Contains(s) {
		return l
	}
	return s
}

// Parent returns the parent locale of the locale s of CLDR in the inheritance chain of CLDR,
// with "und" for the root locale, and reports false when s is the root locale.
func Parent(s string) (string, bool) {
	return parent(s, false)
}

// CollationParent returns the parent locale of the locale s of CLDR in the inheritance chain
// of the collation data of CLDR, with "und" for the root locale, and reports false when s is
// the root locale.
func CollationParent(s string) (string, bool) {
	return parent(s, true)
}

// BestAvailableLocale implements BestAvailableLocale of ECMA-402 for the canonical language
// tag s and the available locales. It reports false when no available locale matches.
func BestAvailableLocale(s string) (string, bool) {
	for {
		if len(s) <= maxAvailableLen() && IsAvailable(s) {
			return s, true
		}
		pos := strings.LastIndexByte(s, '-')
		if pos < 0 {
			return "", false
		}
		if pos >= 2 && s[pos-2] == '-' {
			pos -= 2
		}
		s = s[:pos]
	}
}

// Match is the result of matching requested locales against the available locales.
type Match struct {
	// Locale is the available locale that matched.
	Locale string
	// Requested is the requested locale that matched, or the default locale when none did.
	Requested Tag
}

// LookupMatcher implements LookupMatcher of ECMA-402 for the canonical language tags
// requested, falling back to the available locale defaultLocale.
func LookupMatcher(requested []string, defaultLocale string) Match {
	for _, r := range requested {
		t, ok := Parse(r)
		if !ok {
			continue
		}
		if l, ok := BestAvailableLocale(t.WithoutUnicode().String()); ok {
			return Match{
				Locale:    l,
				Requested: t,
			}
		}
	}
	t, _ := Parse(defaultLocale)
	return Match{
		Locale:    defaultLocale,
		Requested: t,
	}
}

// SupportedLocales implements LookupSupportedLocales of ECMA-402 for the canonical language
// tags requested: it returns those that an available locale matches.
func SupportedLocales(requested []string) []string {
	var out []string
	for _, r := range requested {
		t, ok := Parse(r)
		if !ok {
			continue
		}
		if _, ok := BestAvailableLocale(t.WithoutUnicode().String()); ok {
			out = append(out, r)
		}
	}
	return out
}

// Default returns the default locale: the available locale that best matches the locale of
// the host environment, or "en-US".
var Default = sync.OnceValue(func() string {
	if s, ok := fromHost(hostLocale()); ok {
		return s
	}
	return "en-US"
})

// fromHost returns the available locale that best matches the host locale s, which may be a
// POSIX locale name such as "ja_JP.UTF-8".
func fromHost(s string) (string, bool) {
	s, _, _ = strings.Cut(s, ".")
	s, _, _ = strings.Cut(s, "@")
	s = strings.ReplaceAll(s, "_", "-")
	if s == "" || s == "C" || s == "POSIX" {
		return "", false
	}
	t, ok := Parse(s)
	if !ok {
		return "", false
	}
	return BestAvailableLocale(t.Canonicalize().WithoutUnicode().String())
}

// parent returns the parent locale of the canonical language tag s in the inheritance chain
// of CLDR, with "und" for the root locale, and reports false when s is the root locale.
// collation selects the parents of collation data, where, as in ICU, a locale whose script
// is not the likely one inherits from its language.
func parent(s string, collation bool) (string, bool) {
	if s == "und" {
		return "", false
	}
	if collation {
		if p, ok := cldr.CollationParentLocales.Lookup(s); ok {
			return p, true
		}
	}
	if p, ok := cldr.ParentLocales.Lookup(s); ok && (!collation || p != "und") {
		return p, true
	}
	t, ok := Parse(s)
	if !ok {
		return "und", true
	}
	id := t.LanguageID
	switch {
	case len(id.Variants) > 0:
		id.Variants = id.Variants[:len(id.Variants)-1]
	case id.Region != "":
		id.Region = ""
	case id.Script != "" && collation:
		id.Script = ""
	case id.Script != "":
		// A script that is not the likely script of the language starts its own tree.
		if max, ok := AddLikelySubtags(LanguageID{
			Language: id.Language,
		}); !ok || max.Script != id.Script {
			return "und", true
		}
		id.Script = ""
	default:
		return "und", true
	}
	return id.String(), true
}

// LookupField returns the field key of the records in m for the locale of CLDR whose data
// applies to the available locale l, inheriting the field from its ancestors.
func LookupField(m cldr.Map, l, key string) (string, bool) {
	for l, ok := DataLocale(l), true; ok; l, ok = Parent(l) {
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
