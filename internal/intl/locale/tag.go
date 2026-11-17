// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package locale resolves Unicode BCP 47 locale identifiers against CLDR data.
package locale

import "github.com/hajimehoshi/ekuma/internal/localetag"

// LanguageID is a Unicode language identifier.
type LanguageID = localetag.LanguageID

// Keyword is a Unicode locale extension key and value.
type Keyword = localetag.Keyword

// Tag is a Unicode BCP 47 locale identifier.
type Tag localetag.Tag

// Parse parses a Unicode BCP 47 locale identifier.
func Parse(s string) (Tag, bool) {
	t, ok := localetag.Parse(s)
	return Tag(t), ok
}

// String returns t as a language tag.
func (t Tag) String() string {
	return localetag.Tag(t).String()
}

// Keyword returns the value of the Unicode extension key and whether it is present.
func (t Tag) Keyword(key string) (string, bool) {
	return localetag.Tag(t).Keyword(key)
}

// WithoutUnicode returns t without its Unicode locale extension.
func (t Tag) WithoutUnicode() Tag {
	return Tag(localetag.Tag(t).WithoutUnicode())
}

// BaseName returns t without its extensions or private use part.
func (t Tag) BaseName() string {
	return localetag.Tag(t).BaseName()
}
