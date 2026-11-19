// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale

import (
	"cmp"
	"slices"
	"strings"
	"sync"

	"github.com/hajimehoshi/ekuma/internal/localetag"
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
)

// maxAliasSteps bounds the alias replacements of a unicode_language_id. The CLDR aliases
// settle in a few steps; the bound guards against a cycle in the data.
const maxAliasSteps = 16

// parseReplacement parses the replacement of an alias rule, ignoring a private use part.
func parseReplacement(s string) LanguageID {
	s, _, _ = strings.Cut(s, "-x-")
	t, _ := Parse(s)
	return t.LanguageID
}

// applyAlias replaces the fields of id that the alias rule with the type rule matches by the
// fields of repl. The other fields of repl fill the fields of id that are empty.
func applyAlias(id *LanguageID, rule, repl LanguageID) {
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

// aliasSubtags returns the set of the subtags after the language of the keys of the
// language alias rules.
var aliasSubtags = sync.OnceValue(func() map[string]struct{} {
	m := map[string]struct{}{}
	for i := range cldr.LanguageAliases.Len() {
		_, rest, _ := strings.Cut(cldr.LanguageAliases.Key(i), "-")
		for t := range strings.SplitSeq(rest, "-") {
			if t != "" {
				m[t] = struct{}{}
			}
		}
	}
	return m
})

// replaceLanguageAlias applies the first language alias rule that matches id, and reports
// whether one did. Rules with more variants come first, then rules with a region, then the
// rule for the language alone.
func replaceLanguageAlias(id *LanguageID) bool {
	try := func(key string) bool {
		r, ok := cldr.LanguageAliases.Lookup(key)
		if !ok {
			return false
		}
		rule := parseReplacement(key)
		applyAlias(id, rule, parseReplacement(r))
		return true
	}
	langs := []string{id.Language}
	if id.Language != "und" {
		langs = append(langs, "und")
	}
	// Only the variants of the rules can match, which keeps the pairs few.
	aliased := aliasSubtags()
	var variants []string
	for _, v := range id.Variants {
		if _, ok := aliased[v]; ok {
			variants = append(variants, v)
		}
	}
	for i, v := range variants {
		for _, w := range variants[i+1:] {
			for _, l := range langs {
				if try(l + "-" + v + "-" + w) {
					return true
				}
			}
		}
	}
	for _, v := range variants {
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
func replaceAliases(id *LanguageID) {
	for range maxAliasSteps {
		if replaceLanguageAlias(id) {
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
				id.Region = replacementRegion(*id, strings.Fields(r))
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
func replacementRegion(id LanguageID, regions []string) string {
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

// canonicalizeLanguageID returns id in canonical form, with sorted variants and the aliases replaced.
func canonicalizeLanguageID(id LanguageID) LanguageID {
	id.Variants = slices.Clone(id.Variants)
	slices.Sort(id.Variants)
	replaceAliases(&id)
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
	return t.CanonicalizePolled(nil)
}

// CanonicalizePolled is [Tag.Canonicalize] that calls poll, which may be nil, now and then
// for a tag of many subtags. poll may stop the canonicalization by panicking.
func (t Tag) CanonicalizePolled(poll func()) Tag {
	t.LanguageID = canonicalizeLanguageID(t.LanguageID)

	attrs := slices.Clone(t.Attributes)
	sortPolled(attrs, poll)
	t.Attributes = slices.Compact(attrs)
	var kws []Keyword
	var keys []string
	var seen map[string]struct{}
	for _, k := range t.Keywords {
		if localetag.HasDuplicate(&seen, keys, k.Key) {
			continue
		}
		keys = append(keys, k.Key)
		k.Value = canonicalValue(k.Key, k.Value)
		if k.Value == "true" {
			k.Value = ""
		}
		kws = append(kws, k)
	}
	slices.SortFunc(kws, func(a, b Keyword) int { return cmp.Compare(a.Key, b.Key) })
	t.Keywords = kws

	if t.TLang.Language != "" {
		t.TLang = canonicalizeLanguageID(t.TLang)
	}
	var tfs []Keyword
	keys, seen = keys[:0], nil
	for _, k := range t.TFields {
		if localetag.HasDuplicate(&seen, keys, k.Key) {
			continue
		}
		keys = append(keys, k.Key)
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
// structurally valid language tag. poll, which may be nil, is called now and then for a tag
// of many subtags, and may stop the canonicalization by panicking.
func Canonicalize(s string, poll func()) (string, bool) {
	t, ok := Parse(s)
	if !ok {
		return "", false
	}
	return t.CanonicalizePolled(poll).String(), true
}

// sortChunk is the number of strings that sortPolled sorts or merges between two calls of its
// poll function.
const sortChunk = 1 << 14

// sortPolled sorts s, calling poll, which may be nil, now and then for a long s.
func sortPolled(s []string, poll func()) {
	if poll == nil || len(s) <= sortChunk {
		slices.Sort(s)
		return
	}
	for i := 0; i < len(s); i += sortChunk {
		poll()
		slices.Sort(s[i:min(i+sortChunk, len(s))])
	}
	// The sorted runs of width w are merged in pairs from src into dst.
	src, dst := s, make([]string, len(s))
	var steps int
	for w := sortChunk; w < len(s); w *= 2 {
		for lo := 0; lo < len(s); lo += 2 * w {
			mid, hi := min(lo+w, len(s)), min(lo+2*w, len(s))
			i, j := lo, mid
			for k := lo; k < hi; k++ {
				if steps++; steps == sortChunk {
					steps = 0
					poll()
				}
				if j >= hi || i < mid && src[i] <= src[j] {
					dst[k] = src[i]
					i++
				} else {
					dst[k] = src[j]
					j++
				}
			}
		}
		src, dst = dst, src
	}
	copy(s, src)
}
