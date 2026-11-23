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
	if want.Language != language {
		if want.Script != have.script {
			return cldr.DefaultScriptDistance
		}
		// Cross-language rules have no language-specific regional preference.
		return cldr.RegionDistance("", "", want.Region, have.region)
	}
	if want.Script == have.script {
		return cldr.RegionDistance(language, want.Script, want.Region, have.region)
	}
	distance := cldr.ScriptDistance(language, want.Script, have.script)
	if distance >= cldr.DefaultScriptDistance {
		return distance
	}
	return distance + cldr.RegionDistance(language, "", want.Region, have.region)
}

// For equivalent maximized locales, agreement on an explicit script takes
// precedence over agreement on an explicit region.
type subtagMask uint8

const (
	explicitRegion subtagMask = 1 << iota
	explicitScript
)

func explicitSubtags(id LanguageID) subtagMask {
	var mask subtagMask
	if id.Script != "" {
		mask |= explicitScript
	}
	if id.Region != "" {
		mask |= explicitRegion
	}
	return mask
}

type localeForm struct {
	name     string
	explicit subtagMask
}

type regionalLocale struct {
	name string
	scriptRegion
	paradigm, likelyRegion bool
}

func newRegionalLocale(name string, id LanguageID) regionalLocale {
	likely, ok := AddLikelySubtags(LanguageID{
		Language: id.Language,
		Script:   id.Script,
	})
	return regionalLocale{
		name:         name,
		script:       id.Script,
		region:       id.Region,
		paradigm:     paradigmLocales()[id.String()],
		likelyRegion: ok && id.Region == likely.Region,
	}
}

func (c regionalLocale) preferredTo(other regionalLocale) bool {
	if c.paradigm != other.paradigm {
		return c.paradigm
	}
	return c.script == other.script && c.likelyRegion && !other.likelyRegion
}

type languageLocales struct {
	forms   map[scriptRegion][]localeForm
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
			forms: map[scriptRegion][]localeForm{},
		}
		index[id.Language] = locales
	}
	key := scriptRegion{
		script: id.Script,
		region: id.Region,
	}
	if len(locales.forms[key]) == 0 {
		locales.ordered = append(locales.ordered, newRegionalLocale(name, id))
	}
	locales.forms[key] = append(locales.forms[key], localeForm{
		name:     name,
		explicit: explicitSubtags(t.LanguageID),
	})
}

var likelyLocales = sync.OnceValue(func() likelyIndex {
	index := likelyIndex{}
	for i := range cldr.AvailableLocales.Len() {
		index.add(cldr.AvailableLocales.At(i))
	}
	return index
})

// exactForm selects among supported spellings of the same maximized identifier.
// prefix is an equivalent lookup match when one exists.
func (locales *languageLocales) exactForm(want LanguageID, language string, explicit subtagMask, prefix localeForm) string {
	if locales == nil {
		return prefix.name
	}
	best := prefix
	forms := locales.forms[scriptRegion{
		script: want.Script,
		region: want.Region,
	}]
	for _, form := range forms {
		if want.Language != language {
			return form.name
		}
		if best.name == "" || form.explicit^explicit < best.explicit^explicit {
			best = form
		}
	}
	return best.name
}

// matchLanguage ranks the supported locales of language by script and region distance.
func (index likelyIndex) matchLanguage(want LanguageID, explicit subtagMask, language, prefix string) (string, int) {
	locales := index[language]
	var equivalentPrefix localeForm
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
				equivalentPrefix = localeForm{
					name:     prefix,
					explicit: explicitSubtags(p.LanguageID),
				}
			} else if distance < bestDistance {
				best = newRegionalLocale(prefix, have)
				bestDistance = distance
			}
		}
	}
	if exact := locales.exactForm(want, language, explicit, equivalentPrefix); exact != "" {
		return exact, 0
	}
	if locales == nil {
		return best.name, bestDistance
	}
	for _, candidate := range locales.ordered {
		distance := candidate.distance(want, language)
		if distance < bestDistance || distance == bestDistance && best.name != "" && candidate.preferredTo(best) {
			best = candidate
			bestDistance = distance
		}
	}
	return best.name, bestDistance
}

// matchingLocale returns the closest available locale and its distance, or an empty name if none matches.
func (m Matcher) matchingLocale(t Tag, bestFit bool) (string, int) {
	name := t.BaseName()
	prefix, _ := m.bestAvailableLocale(name)
	if !bestFit || prefix == name {
		return prefix, 0
	}
	if t.Language == "und" {
		if t.Script == "" && t.Region == "" {
			return prefix, 0
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
		return prefix, 0
	}
	index := m.likely
	if index == nil {
		index = likelyLocales()
	}
	explicit := explicitSubtags(t.LanguageID)
	best, bestDistance := index.matchLanguage(want, explicit, want.Language, prefix)
	if bestDistance == 0 {
		return best, bestDistance
	}
	for lang, languageDistance := range cldr.RelatedLanguages(want.Language) {
		if languageDistance >= bestDistance {
			break
		}
		l, distance := index.matchLanguage(want, explicit, lang, "")
		distance += languageDistance
		if l != "" && distance < bestDistance {
			best, bestDistance = l, distance
		}
	}
	return best, bestDistance
}

// ICU uses the distance between US and British English as the cost of moving
// one position down the requested locale list.
var requestDemotion = cldr.RegionDistance("en", "Latn", "US", "GB")

// Match returns the selected locale and corresponding canonical request.
// When no request matches, both results use defaultLocale.
func (m Matcher) Match(requested []string, defaultLocale string, bestFit bool) (string, Tag) {
	var best string
	var request Tag
	remainingDistance := cldr.DefaultScriptDistance
	for _, r := range requested {
		t, ok := Parse(r)
		if !ok {
			continue
		}
		l, distance := m.matchingLocale(t, bestFit)
		if l != "" && distance < remainingDistance {
			best, request = l, t
			remainingDistance = distance
		}
		// Every request remains eligible until the first match. After that,
		// each list position reduces the distance a later match may have.
		if best == "" {
			continue
		}
		remainingDistance -= requestDemotion
		if remainingDistance <= 0 {
			break
		}
	}
	if best != "" {
		return best, request
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
