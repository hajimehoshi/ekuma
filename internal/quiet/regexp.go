// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// MatchRegExp matches re against the string s at the code unit index start, or after it unless re
// is sticky. It returns the code unit indices of the captures, or nil when there is no match. They
// are valid until the next match.
func (q Context) MatchRegExp(re *regexp.Regexp, s storage.Value, start int) []int {
	m := &q.Realm.RegExpMatcher
	str := s.Str()
	m.SetASCII(str, s.IsASCII())
	return re.Match(m, str, start, q)
}

// FindRegExp is like [Context.MatchRegExp], but it finds the first match at start or after it
// even when re is sticky.
func (q Context) FindRegExp(re *regexp.Regexp, s storage.Value, start int) []int {
	m := &q.Realm.RegExpMatcher
	str := s.Str()
	m.SetASCII(str, s.IsASCII())
	return re.Find(m, str, start, q)
}

// RegExpMatchSpan returns the positions in the subject of the capture i of the last match, which
// must have succeeded.
func (q Context) RegExpMatchSpan(i int) (start, end wtf8.Pos) {
	return q.Realm.RegExpMatcher.Span(i)
}

// RecordRegExpMatch updates the legacy static properties of %RegExp% after a match of the RegExp
// object r against s with the capture indices caps. It does nothing when the [[Realm]] of r is not
// the current realm.
func (q Context) RecordRegExpMatch(r *storage.Object, s storage.Value, caps []int) {
	st := r.ExtState().(*storage.RegExpState)
	if st.Realm != q.Realm {
		return
	}
	if st.Legacy {
		q.UpdateLegacyRegExpStaticProperties(s, caps)
		return
	}
	// This is InvalidateLegacyRegExpStaticProperties.
	l := &q.Realm.LegacyRegExp
	l.Input = storage.Undefined
	l.Str = storage.Undefined
	l.Caps = l.Caps[:0]
}

// UpdateLegacyRegExpStaticProperties implements UpdateLegacyRegExpStaticProperties for %RegExp%
// after a match against s with the capture indices caps.
func (q Context) UpdateLegacyRegExpStaticProperties(s storage.Value, caps []int) {
	l := &q.Realm.LegacyRegExp
	l.Input = s
	l.Str = s
	l.Caps = append(l.Caps[:0], caps...)
}

// RegExpLastIndex returns the value of the own lastIndex property of o, or false when o may not
// have it as a writable data property where ExecutionContext.RegExpAlloc puts it.
func (q Context) RegExpLastIndex(o *storage.Object) (storage.Value, bool) {
	return q.Realm.RegExpLastIndex(o)
}

// SetRegExpLastIndex sets the own lastIndex property of o to v and reports true, or reports
// false when o may not have it as a writable data property where ExecutionContext.RegExpAlloc puts it.
func (q Context) SetRegExpLastIndex(o *storage.Object, v storage.Value) bool {
	return q.Realm.SetRegExpLastIndex(o, v)
}

// UnmodifiedRegExpMatcher returns the compiled pattern of o when o is an initialized RegExp object
// whose prototype is %RegExp.prototype% and whose only own property is a writable lastIndex, while
// the properties recorded by ExecutionContext.RecordRegExpOriginals are unchanged, and nil
// otherwise. When it is not nil, those properties are the only ones that reading the recorded keys
// from o finds, and lastIndex is an ordinary data property.
func (q Context) UnmodifiedRegExpMatcher(o *storage.Object) *regexp.Regexp {
	return q.Realm.UnmodifiedRegExpMatcher(o)
}
