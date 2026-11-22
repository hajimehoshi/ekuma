// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale

import (
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
)

// AddLikelySubtags implements the Add Likely Subtags algorithm of UTS 35 for id, which is in
// canonical form. It reports false when the likely subtags of id are unknown.
func AddLikelySubtags(id LanguageID) (LanguageID, bool) {
	l, s, r := id.Language, id.Script, id.Region
	var keys []string
	if s != "" && r != "" {
		// A fully specified language ID needs no entry in the likely-subtags data.
		if l != "und" && s != "Zzzz" && r != "ZZ" {
			return id, true
		}
		keys = append(keys, l+"-"+s+"-"+r)
	}
	if s != "" {
		keys = append(keys, l+"-"+s)
	}
	if r != "" {
		keys = append(keys, l+"-"+r)
	}
	keys = append(keys, l)
	for _, k := range keys {
		if max, ok := lookupLikelySubtags(id, k); ok {
			return max, true
		}
	}
	return id, false
}

// lookupLikelySubtags fills the missing fields of id from the entry for key.
func lookupLikelySubtags(id LanguageID, key string) (LanguageID, bool) {
	v, ok := cldr.LikelySubtags.Lookup(key)
	if !ok {
		return id, false
	}
	t, _ := Parse(v)
	if id.Language == "und" {
		id.Language = t.Language
	}
	if id.Script == "" {
		id.Script = t.Script
	}
	if id.Region == "" {
		id.Region = t.Region
	}
	return id, true
}

// RemoveLikelySubtags implements the Remove Likely Subtags algorithm of UTS 35 for id, which
// is in canonical form, favoring the region over the script. It reports false when the
// likely subtags of id are unknown.
func RemoveLikelySubtags(id LanguageID) (LanguageID, bool) {
	max, ok := AddLikelySubtags(LanguageID{
		Language: id.Language,
		Script:   id.Script,
		Region:   id.Region,
	})
	if !ok {
		return id, false
	}
	for _, trial := range []LanguageID{
		{
			Language: max.Language,
		},
		{
			Language: max.Language,
			Region:   max.Region,
		},
		{
			Language: max.Language,
			Script:   max.Script,
		},
	} {
		m, ok := AddLikelySubtags(trial)
		if ok && m.Language == max.Language && m.Script == max.Script && m.Region == max.Region {
			trial.Variants = id.Variants
			return trial, true
		}
	}
	max.Variants = id.Variants
	return max, true
}
