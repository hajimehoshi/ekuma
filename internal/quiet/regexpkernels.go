// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// regExpPattern is the pattern that a RegExp object had.
type regExpPattern struct {
	Source  wtf8.String
	Matcher *regexp.Regexp
}

// UnmodifiedPattern returns the pattern of r when [Context.UnmodifiedRegExpMatcher]
// accepts r. It reports false otherwise.
func UnmodifiedPattern(q Context, r *storage.Object) (regExpPattern, bool) {
	m := q.UnmodifiedRegExpMatcher(r)
	if m == nil {
		return regExpPattern{}, false
	}
	src, _ := r.RegExpOriginal()
	return regExpPattern{
		Source:  src,
		Matcher: m,
	}, true
}

// AdvanceStringIndex implements AdvanceStringIndex.
func AdvanceStringIndex(s storage.Value, index int64, unicode bool) int64 {
	if !unicode {
		return index + 1
	}
	n := int64(s.StringLength())
	if index+1 >= n {
		return index + 1
	}
	if u := s.UnitAt(int(index)); wtf8.IsHighSurrogate(rune(u)) {
		if lo := s.UnitAt(int(index) + 1); wtf8.IsLowSurrogate(rune(lo)) {
			return index + 2
		}
	}
	return index + 1
}

// Capture returns the capture i of caps in s, or undefined.
func Capture(q Context, s storage.Value, caps []int, i int) storage.Value {
	a, b := caps[2*i], caps[2*i+1]
	if a < 0 {
		return storage.Undefined
	}
	if a == 0 && b == s.StringLength() {
		return s
	}
	return q.Substring(s, a, b)
}

// SplitUnmodified returns the array of at most lim strings that splitting s by the matches of re
// makes, when [Context.UnmodifiedRegExpMatcher] accepts r. It reports false otherwise.
func SplitUnmodified(qx Context, r *storage.Object, re *regexp.Regexp, s storage.Value, lim uint32, unicodeMatching bool) (storage.Value, bool) {
	if qx.UnmodifiedRegExpMatcher(r) == nil {
		return storage.Undefined, false
	}
	size := s.StringLength()
	if size == 0 {
		// The splitter is sticky and its lastIndex is 0, so it can only match at 0.
		if caps := qx.FindRegExp(re, s, 0); caps != nil {
			qx.UpdateLegacyRegExpStaticProperties(s, caps)
			return storage.ObjectValue(qx.NewArray(nil)), true
		}
		return storage.ObjectValue(qx.NewArray([]storage.Value{s})), true
	}
	var out []storage.Value
	p, q := 0, 0
	for q < size {
		caps := qx.FindRegExp(re, s, q)
		if caps == nil || caps[0] >= size {
			break
		}
		qx.UpdateLegacyRegExpStaticProperties(s, caps)
		q = caps[0]
		end := caps[1]
		if end == p {
			q = int(AdvanceStringIndex(s, int64(q), unicodeMatching))
			continue
		}
		out = append(out, qx.Substring(s, p, q))
		if uint32(len(out)) == lim {
			return storage.ObjectValue(qx.NewArray(out)), true
		}
		p = end
		for i := 1; i < len(caps)/2; i++ {
			out = append(out, Capture(qx, s, caps, i))
			if uint32(len(out)) == lim {
				return storage.ObjectValue(qx.NewArray(out)), true
			}
		}
		q = p
	}
	out = append(out, qx.Substring(s, p, size))
	return storage.ObjectValue(qx.NewArray(out)), true
}
