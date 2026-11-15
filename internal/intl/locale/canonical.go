// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale

import (
	"cmp"
	"slices"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/intl/cldr"
)

// maxAliasSteps bounds the alias replacements of a unicode_language_id. The CLDR aliases
// settle in a few steps; the bound guards against a cycle in the data.
const maxAliasSteps = 16

// parseReplacement parses the replacement of an alias rule, ignoring a private use part.
func parseReplacement(s string) LanguageID {
	s, _, _ = strings.Cut(s, "-x-")
	id, _, _ := parseLanguageID(strings.Split(lower(s), "-"))
	return id
}

// apply replaces the fields of id that the alias rule with the type rule matches by the
// fields of repl. The other fields of repl fill the fields of id that are empty.
func (id *LanguageID) apply(rule, repl LanguageID) {
	if rule.Language != "und" || id.Language == "und" {
		id.Language = repl.Language
	}
	if rule.Script != "" || id.Script == "" {
		id.Script = repl.Script
	}
	if rule.Region != "" || id.Region == "" {
		id.Region = repl.Region
	}
	vs := slices.DeleteFunc(slices.Clone(id.Variants), func(v string) bool {
		return slices.Contains(rule.Variants, v)
	})
	for _, v := range repl.Variants {
		if !slices.Contains(vs, v) {
			vs = append(vs, v)
		}
	}
	slices.Sort(vs)
	id.Variants = vs
}

// replaceLanguageAlias applies the first language alias rule that matches id, and reports
// whether one did. Rules with more variants come first, then rules with a region, then the
// rule for the language alone.
func (id *LanguageID) replaceLanguageAlias() bool {
	try := func(key string) bool {
		r, ok := cldr.LanguageAliases.Lookup(key)
		if !ok {
			return false
		}
		rule := parseReplacement(key)
		id.apply(rule, parseReplacement(r))
		return true
	}
	langs := []string{id.Language}
	if id.Language != "und" {
		langs = append(langs, "und")
	}
	for i, v := range id.Variants {
		for _, w := range id.Variants[i+1:] {
			for _, l := range langs {
				if try(l + "-" + v + "-" + w) {
					return true
				}
			}
		}
	}
	for _, v := range id.Variants {
		for _, l := range langs {
			if try(l + "-" + v) {
				return true
			}
		}
	}
	if id.Region != "" && try(id.Language+"-"+id.Region) {
		return true
	}
	return id.Language != "und" && try(id.Language)
}

// replaceAliases replaces the aliases in id as the canonicalization of UTS 35 does.
func (id *LanguageID) replaceAliases() {
	for range maxAliasSteps {
		if id.replaceLanguageAlias() {
			continue
		}
		if id.Script != "" {
			if r, ok := cldr.ScriptAliases.Lookup(id.Script); ok {
				id.Script = r
				continue
			}
		}
		if id.Region != "" {
			if r, ok := cldr.TerritoryAliases.Lookup(id.Region); ok {
				id.Region = id.replacementRegion(strings.Fields(r))
				continue
			}
		}
		changed := false
		for i, v := range id.Variants {
			if r, ok := cldr.VariantAliases.Lookup(v); ok {
				vs := slices.Delete(slices.Clone(id.Variants), i, i+1)
				if !slices.Contains(vs, r) {
					vs = append(vs, r)
				}
				slices.Sort(vs)
				id.Variants = vs
				changed = true
				break
			}
		}
		if !changed {
			return
		}
	}
}

// replacementRegion returns the region among regions, the replacements of a region alias,
// that the language and script of id are most likely used in, or the first one.
func (id LanguageID) replacementRegion(regions []string) string {
	if len(regions) > 1 {
		max, ok := AddLikelySubtags(LanguageID{
			Language: id.Language,
			Script:   id.Script,
		})
		if ok && slices.Contains(regions, max.Region) {
			return max.Region
		}
	}
	return regions[0]
}

// canonicalize returns id in canonical form, with sorted variants and the aliases replaced.
func (id LanguageID) canonicalize() LanguageID {
	id.Variants = slices.Clone(id.Variants)
	slices.Sort(id.Variants)
	id.replaceAliases()
	return id
}

// canonicalValue returns the canonical value of the keyword key with the value v.
func canonicalValue(key, v string) string {
	if r, ok := cldr.KeywordAliases.Lookup(key + "-" + v); ok {
		v = r
	}
	switch key {
	case "rg", "sd":
		if r, ok := cldr.SubdivisionAliases.Lookup(v); ok {
			v, _, _ = strings.Cut(r, " ")
		}
	}
	return v
}

// Canonicalize returns t in the canonical form of ECMA-402's CanonicalizeUnicodeLocaleId.
func (t Tag) Canonicalize() Tag {
	t.LanguageID = t.LanguageID.canonicalize()

	attrs := slices.Clone(t.Attributes)
	slices.Sort(attrs)
	t.Attributes = slices.Compact(attrs)
	var kws []Keyword
	for _, k := range t.Keywords {
		if slices.ContainsFunc(kws, func(e Keyword) bool { return e.Key == k.Key }) {
			continue
		}
		k.Value = canonicalValue(k.Key, k.Value)
		if k.Value == "true" {
			k.Value = ""
		}
		kws = append(kws, k)
	}
	slices.SortFunc(kws, func(a, b Keyword) int { return cmp.Compare(a.Key, b.Key) })
	t.Keywords = kws

	if t.TLang.Language != "" {
		t.TLang = t.TLang.canonicalize()
	}
	var tfs []Keyword
	for _, k := range t.TFields {
		if slices.ContainsFunc(tfs, func(e Keyword) bool { return e.Key == k.Key }) {
			continue
		}
		k.Value = canonicalValue(k.Key, k.Value)
		tfs = append(tfs, k)
	}
	slices.SortFunc(tfs, func(a, b Keyword) int { return cmp.Compare(a.Key, b.Key) })
	t.TFields = tfs

	t.Others = slices.Clone(t.Others)
	slices.Sort(t.Others)
	return t
}

// Canonicalize returns the canonical form of the language tag s, and reports whether s is a
// structurally valid language tag.
func Canonicalize(s string) (string, bool) {
	t, ok := Parse(s)
	if !ok {
		return "", false
	}
	return t.Canonicalize().String(), true
}
