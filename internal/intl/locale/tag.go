// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package locale implements Unicode BCP 47 locale identifiers as ECMA-402 uses them: parsing,
// canonicalization, likely subtags, matching against the available locales, and the locale
// data that Intl.Locale reports.
package locale

import (
	"slices"
	"strings"
)

// LanguageID is a unicode_language_id in canonical case: the language in lower case, the
// script in title case, the region in upper case and the variants in lower case.
type LanguageID struct {
	// Language is the language subtag, "und" for an undetermined language.
	Language string
	// Script is the script subtag, or empty.
	Script string
	// Region is the region subtag, or empty.
	Region string
	// Variants are the variant subtags.
	Variants []string
}

// Keyword is a key of a Unicode locale extension with its value.
type Keyword struct {
	Key string
	// Value is the subtags of the value joined by "-", empty when there are none.
	Value string
}

// Tag is a Unicode BCP 47 locale identifier.
type Tag struct {
	LanguageID

	// Attributes are the attributes of the Unicode locale extension.
	Attributes []string
	// Keywords are the keywords of the Unicode locale extension.
	Keywords []Keyword
	// HasUnicode reports whether the tag has a Unicode locale extension.
	HasUnicode bool

	// TLang is the tlang of the transformed extension, with an empty Language when there
	// is none.
	TLang LanguageID
	// TFields are the tfields of the transformed extension.
	TFields []Keyword
	// HasTransformed reports whether the tag has a transformed extension.
	HasTransformed bool

	// Others are the other extensions, each with its singleton, in lower case.
	Others []string
	// PrivateUse is the private use extension with its singleton in lower case, or empty.
	PrivateUse string
}

func isAlpha(s string) bool {
	for i := range len(s) {
		c := s[i]
		if !('a' <= c && c <= 'z') {
			return false
		}
	}
	return true
}

func isDigit(s string) bool {
	for i := range len(s) {
		c := s[i]
		if !('0' <= c && c <= '9') {
			return false
		}
	}
	return true
}

func isAlphanum(s string) bool {
	for i := range len(s) {
		c := s[i]
		if !('a' <= c && c <= 'z' || '0' <= c && c <= '9') {
			return false
		}
	}
	return true
}

// lower returns s in ASCII lower case.
func lower(s string) string {
	for i := range len(s) {
		if c := s[i]; 'A' <= c && c <= 'Z' {
			return strings.ToLower(s)
		}
	}
	return s
}

// IsLanguageSubtag reports whether s, in lower case, is a unicode_language_subtag.
func IsLanguageSubtag(s string) bool {
	return (len(s) >= 2 && len(s) <= 3 || len(s) >= 5 && len(s) <= 8) && isAlpha(s)
}

// IsScriptSubtag reports whether s, in lower case, is a unicode_script_subtag.
func IsScriptSubtag(s string) bool {
	return len(s) == 4 && isAlpha(s)
}

// IsRegionSubtag reports whether s, in lower case, is a unicode_region_subtag.
func IsRegionSubtag(s string) bool {
	return len(s) == 2 && isAlpha(s) || len(s) == 3 && isDigit(s)
}

// IsVariantSubtag reports whether s, in lower case, is a unicode_variant_subtag.
func IsVariantSubtag(s string) bool {
	return len(s) >= 5 && len(s) <= 8 && isAlphanum(s) || len(s) == 4 && isDigit(s[:1]) && isAlphanum(s)
}

// IsType reports whether s, in lower case, matches the type production of a Unicode locale
// identifier: one or more subtags of three to eight alphanumeric characters.
func IsType(s string) bool {
	for t := range strings.SplitSeq(s, "-") {
		if len(t) < 3 || len(t) > 8 || !isAlphanum(t) {
			return false
		}
	}
	return true
}

func isKey(s string) bool {
	return len(s) == 2 && isAlphanum(s[:1]) && isAlpha(s[1:])
}

func isTKey(s string) bool {
	return len(s) == 2 && isAlpha(s[:1]) && isDigit(s[1:])
}

func title(s string) string {
	return strings.ToUpper(s[:1]) + s[1:]
}

// parseLanguageID parses the unicode_language_id at the start of subtags, which are in lower
// case, and returns it with the number of subtags it takes. It reports false when the
// variants repeat.
func parseLanguageID(subtags []string) (LanguageID, int, bool) {
	var id LanguageID
	i := 0
	if i >= len(subtags) || !IsLanguageSubtag(subtags[i]) {
		return id, 0, false
	}
	id.Language = subtags[i]
	i++
	if i < len(subtags) && IsScriptSubtag(subtags[i]) {
		id.Script = title(subtags[i])
		i++
	}
	if i < len(subtags) && IsRegionSubtag(subtags[i]) {
		id.Region = strings.ToUpper(subtags[i])
		i++
	}
	for i < len(subtags) && IsVariantSubtag(subtags[i]) {
		if slices.Contains(id.Variants, subtags[i]) {
			return id, 0, false
		}
		id.Variants = append(id.Variants, subtags[i])
		i++
	}
	return id, i, true
}

// Parse parses s as a structurally valid language tag in the sense of ECMA-402: a Unicode
// BCP 47 locale identifier without duplicate variants or singletons. It keeps the order of
// the subtags.
func Parse(s string) (Tag, bool) {
	var t Tag
	for i := range len(s) {
		if s[i] >= 0x80 {
			return t, false
		}
	}
	subtags := strings.Split(lower(s), "-")
	for _, st := range subtags {
		if st == "" || len(st) > 8 {
			return t, false
		}
	}
	id, i, ok := parseLanguageID(subtags)
	if !ok {
		return t, false
	}
	t.LanguageID = id
	var singletons []byte
	for i < len(subtags) {
		st := subtags[i]
		if len(st) != 1 || !isAlphanum(st) {
			return t, false
		}
		if slices.Contains(singletons, st[0]) {
			return t, false
		}
		singletons = append(singletons, st[0])
		i++
		start := i
		switch st[0] {
		case 'x':
			for i < len(subtags) && isAlphanum(subtags[i]) {
				i++
			}
			if i == start || i != len(subtags) {
				return t, false
			}
			t.PrivateUse = "x-" + strings.Join(subtags[start:], "-")
		case 'u':
			t.HasUnicode = true
			for i < len(subtags) && len(subtags[i]) >= 3 && isAlphanum(subtags[i]) {
				t.Attributes = append(t.Attributes, subtags[i])
				i++
			}
			for i < len(subtags) && len(subtags[i]) == 2 {
				if !isKey(subtags[i]) {
					return t, false
				}
				k := Keyword{
					Key: subtags[i],
				}
				i++
				vstart := i
				for i < len(subtags) && len(subtags[i]) >= 3 && isAlphanum(subtags[i]) {
					i++
				}
				k.Value = strings.Join(subtags[vstart:i], "-")
				t.Keywords = append(t.Keywords, k)
			}
			if i == start {
				return t, false
			}
		case 't':
			t.HasTransformed = true
			if i < len(subtags) && IsLanguageSubtag(subtags[i]) {
				tlang, n, ok := parseLanguageID(subtags[i:])
				if !ok {
					return t, false
				}
				t.TLang = tlang
				i += n
			}
			for i < len(subtags) && isTKey(subtags[i]) {
				k := Keyword{
					Key: subtags[i],
				}
				i++
				vstart := i
				for i < len(subtags) && len(subtags[i]) >= 3 && isAlphanum(subtags[i]) {
					i++
				}
				if i == vstart {
					return t, false
				}
				k.Value = strings.Join(subtags[vstart:i], "-")
				t.TFields = append(t.TFields, k)
			}
			if i == start {
				return t, false
			}
		default:
			for i < len(subtags) && len(subtags[i]) >= 2 && isAlphanum(subtags[i]) {
				i++
			}
			if i == start {
				return t, false
			}
			t.Others = append(t.Others, st+"-"+strings.Join(subtags[start:i], "-"))
		}
	}
	return t, true
}

// String returns id as a language tag.
func (id LanguageID) String() string {
	var b strings.Builder
	id.write(&b)
	return b.String()
}

func (id LanguageID) write(b *strings.Builder) {
	b.WriteString(id.Language)
	if id.Script != "" {
		b.WriteByte('-')
		b.WriteString(id.Script)
	}
	if id.Region != "" {
		b.WriteByte('-')
		b.WriteString(id.Region)
	}
	for _, v := range id.Variants {
		b.WriteByte('-')
		b.WriteString(v)
	}
}

// String returns t as a language tag, with the extensions in alphabetical order of their
// singletons.
func (t Tag) String() string {
	var b strings.Builder
	t.LanguageID.write(&b)
	exts := slices.Clone(t.Others)
	if t.HasTransformed {
		exts = append(exts, t.transformed())
	}
	if t.HasUnicode {
		exts = append(exts, t.unicode())
	}
	slices.Sort(exts)
	for _, e := range exts {
		b.WriteByte('-')
		b.WriteString(e)
	}
	if t.PrivateUse != "" {
		b.WriteByte('-')
		b.WriteString(t.PrivateUse)
	}
	return b.String()
}

// unicode returns the Unicode locale extension of t with its singleton.
func (t Tag) unicode() string {
	var b strings.Builder
	b.WriteByte('u')
	for _, a := range t.Attributes {
		b.WriteByte('-')
		b.WriteString(a)
	}
	for _, k := range t.Keywords {
		b.WriteByte('-')
		b.WriteString(k.Key)
		if k.Value != "" {
			b.WriteByte('-')
			b.WriteString(k.Value)
		}
	}
	return b.String()
}

// transformed returns the transformed extension of t with its singleton.
func (t Tag) transformed() string {
	var b strings.Builder
	b.WriteByte('t')
	if t.TLang.Language != "" {
		b.WriteByte('-')
		b.WriteString(lower(t.TLang.String()))
	}
	for _, k := range t.TFields {
		b.WriteByte('-')
		b.WriteString(k.Key)
		b.WriteByte('-')
		b.WriteString(k.Value)
	}
	return b.String()
}

// Keyword returns the value of the keyword key of the Unicode locale extension of t, and
// reports whether t has the keyword.
func (t Tag) Keyword(key string) (string, bool) {
	for _, k := range t.Keywords {
		if k.Key == key {
			return k.Value, true
		}
	}
	return "", false
}

// WithoutUnicode returns t without its Unicode locale extension.
func (t Tag) WithoutUnicode() Tag {
	t.HasUnicode = false
	t.Attributes = nil
	t.Keywords = nil
	return t
}

// BaseName returns the unicode_language_id of t as a language tag.
func (t Tag) BaseName() string {
	return t.LanguageID.String()
}
