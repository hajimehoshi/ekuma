// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale

import (
	"strings"

	"github.com/hajimehoshi/ekuma/internal/intl/cldr"
)

// AddLikelySubtags implements the Add Likely Subtags algorithm of UTS 35 for id, which is in
// canonical form. It reports false when the likely subtags of id are unknown.
func AddLikelySubtags(id LanguageID) (LanguageID, bool) {
	l, s, r := id.Language, id.Script, id.Region
	var keys []string
	if s != "" && r != "" {
		keys = append(keys, l+"-"+s+"-"+r)
	}
	if r != "" {
		keys = append(keys, l+"-"+r)
	}
	if s != "" {
		keys = append(keys, l+"-"+s)
	}
	keys = append(keys, l)
	if l != "und" && s != "" {
		keys = append(keys, "und-"+s)
	}
	for _, k := range keys {
		v, ok := cldr.LikelySubtags.Lookup(k)
		if !ok {
			continue
		}
		m, _, _ := parseLanguageID(strings.Split(lower(v), "-"))
		if l != "und" {
			m.Language = l
		}
		if s != "" {
			m.Script = s
		}
		if r != "" {
			m.Region = r
		}
		m.Variants = id.Variants
		return m, true
	}
	return id, false
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
