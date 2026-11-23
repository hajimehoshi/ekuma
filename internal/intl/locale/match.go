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
	return allLocales.bestAvailableLocale(s)
}

// Matcher selects from an ordered set of available locales. Its zero value uses all available locales.
type Matcher struct {
	available map[string]bool
	likely    likelyIndex
}

var allLocales Matcher

// NewMatcher returns a matcher for an ordered subset of the available canonical locale identifiers.
func NewMatcher(supported []string) Matcher {
	m := Matcher{
		available: make(map[string]bool, len(supported)),
		likely:    likelyIndex{},
	}
	for _, name := range supported {
		m.available[name] = true
		m.likely.add(name)
	}
	return m
}

func (m Matcher) contains(s string) bool {
	if m.available != nil {
		return m.available[s]
	}
	return len(s) <= maxAvailableLen() && IsAvailable(s)
}

func (m Matcher) bestAvailableLocale(s string) (string, bool) {
	for {
		if m.contains(s) {
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

type scriptRegion struct {
	script, region string
}

func (have scriptRegion) distance(want LanguageID, language string) int {
	if want.Script == have.script {
		return cldr.RegionDistance(language, want.Script, want.Region, have.region)
	}
	// The script rules apply to two writing systems of the same language.
	if want.Language != language {
		return cldr.DefaultScriptDistance
	}
	distance := cldr.ScriptDistance(language, want.Script, have.script)
	if distance >= cldr.DefaultScriptDistance {
		return distance
	}
	return distance + cldr.RegionDistance(language, "", want.Region, have.region)
}

type regionalLocale struct {
	name string
	scriptRegion
	paradigm bool
}

type languageLocales struct {
	exact   map[scriptRegion]string
	ordered []regionalLocale
}

type likelyIndex map[string]*languageLocales

var paradigmLocales = sync.OnceValue(func() map[string]bool {
	locales := map[string]bool{}
	for i := range cldr.ParadigmLocales.Len() {
		t, _ := Parse(cldr.ParadigmLocales.At(i))
		max, _ := AddLikelySubtags(t.LanguageID)
		locales[max.String()] = true
	}
	return locales
})

func (index likelyIndex) add(name string) {
	t, ok := Parse(name)
	if !ok || t.Language == "und" || len(t.Variants) != 0 {
		return
	}
	id, ok := AddLikelySubtags(t.LanguageID)
	if !ok {
		return
	}
	locales := index[id.Language]
	if locales == nil {
		locales = &languageLocales{
			exact: map[scriptRegion]string{},
		}
		index[id.Language] = locales
	}
	key := scriptRegion{
		script: id.Script,
		region: id.Region,
	}
	if locales.exact[key] != "" {
		return
	}
	locales.exact[key] = name
	locales.ordered = append(locales.ordered, regionalLocale{
		name:         name,
		scriptRegion: key,
		paradigm:     paradigmLocales()[id.String()],
	})
}

var likelyLocales = sync.OnceValue(func() likelyIndex {
	index := likelyIndex{}
	for i := range cldr.AvailableLocales.Len() {
		index.add(cldr.AvailableLocales.At(i))
	}
	return index
})

// matchLanguage ranks the supported locales of language by script and region distance.
func (index likelyIndex) matchLanguage(want LanguageID, language, prefix string) string {
	bestDistance := cldr.DefaultScriptDistance
	var best regionalLocale
	if prefix != "" {
		p, _ := Parse(prefix)
		have, ok := AddLikelySubtags(p.LanguageID)
		if ok && have.Language == language {
			candidate := scriptRegion{
				script: have.Script,
				region: have.Region,
			}
			distance := candidate.distance(want, language)
			if distance == 0 {
				return prefix
			}
			if distance < bestDistance {
				best = regionalLocale{
					name:     prefix,
					paradigm: paradigmLocales()[have.String()],
				}
				bestDistance = distance
			}
		}
	}
	locales := index[language]
	if locales == nil {
		return best.name
	}
	if exact := locales.exact[scriptRegion{
		script: want.Script,
		region: want.Region,
	}]; exact != "" {
		return exact
	}
	for _, candidate := range locales.ordered {
		distance := candidate.distance(want, language)
		if distance < bestDistance || distance == bestDistance && best.name != "" && candidate.paradigm && !best.paradigm {
			best = candidate
			bestDistance = distance
		}
	}
	return best.name
}

// matchingLocale returns an available locale for t, or the empty string.
// related reports whether a related-language rule supplied the match.
func (m Matcher) matchingLocale(t Tag, bestFit bool) (matched string, related bool) {
	name := t.BaseName()
	prefix, _ := m.bestAvailableLocale(name)
	if !bestFit || prefix == name {
		return prefix, false
	}
	if t.Language == "und" {
		if t.Script == "" && t.Region == "" {
			return prefix, false
		}
		// Only the explicit script or region identifies a language here.
		prefix = ""
	}
	want, ok := AddLikelySubtags(t.LanguageID)
	if !ok && t.Script != "" {
		// Best-fit matching can use the script's likely region to find a related language.
		want, ok = lookupLikelySubtags(t.LanguageID, "und-"+t.Script)
	}
	if !ok {
		return prefix, false
	}
	index := m.likely
	if index == nil {
		index = likelyLocales()
	}
	if l := index.matchLanguage(want, want.Language, prefix); l != "" {
		return l, false
	}
	languages, _ := cldr.LanguageMatches.Lookup(want.Language)
	for lang := range strings.FieldsSeq(languages) {
		if l := index.matchLanguage(want, lang, ""); l != "" {
			return l, true
		}
	}
	return "", false
}

// Match returns the selected locale and corresponding canonical request.
// When no request matches, both results use defaultLocale.
func (m Matcher) Match(requested []string, defaultLocale string, bestFit bool) (string, Tag) {
	var relatedLocale string
	var relatedRequest Tag
	for _, r := range requested {
		t, ok := Parse(r)
		if !ok {
			continue
		}
		l, related := m.matchingLocale(t, bestFit)
		if l == "" {
			continue
		}
		if !related {
			return l, t
		}
		// Prefer any requested language to a related language.
		if relatedLocale == "" {
			relatedLocale, relatedRequest = l, t
		}
	}
	if relatedLocale != "" {
		return relatedLocale, relatedRequest
	}
	t, _ := Parse(defaultLocale)
	return defaultLocale, t
}

// SupportedLocales returns the canonical requested tags that have an available match.
func SupportedLocales(requested []string, bestFit bool) []string {
	var out []string
	for _, r := range requested {
		t, ok := Parse(r)
		if !ok {
			continue
		}
		if l, _ := allLocales.matchingLocale(t, bestFit); l != "" {
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
