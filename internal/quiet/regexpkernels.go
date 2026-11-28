// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"strings"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

const maxRegExpKernelBytes = storage.PollInterval

func RegExpExecKernel(q Context, this, s storage.Value) (storage.Value, bool) {
	if !this.IsObject() || !s.IsString() || s.StringByteLength() > maxRegExpKernelBytes {
		return storage.Undefined, false
	}
	r := this.Object()
	re := r.RegExpMatcher()
	if re == nil || !plainMatchResult(re) {
		return storage.Undefined, false
	}
	lastIndex, ok := q.RegExpLastIndex(r)
	if !ok || !lastIndex.IsNumber() {
		return storage.Undefined, false
	}
	flags := re.Flags()
	stateful := flags&(regexp.FlagGlobal|regexp.FlagSticky) != 0
	var from int
	if stateful {
		index := numconv.ToLength(lastIndex.Number())
		if index > 0 && index > int64(s.StringLength()) {
			q.SetRegExpLastIndex(r, storage.Number(0))
			return storage.Null, true
		}
		from = int(index)
	}
	text := s.Str()
	if literal, matched, ok := r.RegExpLiteral(); ok && len(literal) <= maxRegExpKernelBytes {
		start, end := findLiteral(text, literal, s.IsASCII(), from, flags)
		if start < 0 {
			if stateful {
				q.SetRegExpLastIndex(r, storage.Number(0))
			}
			return storage.Null, true
		}
		if stateful {
			q.SetRegExpLastIndex(r, storage.Number(float64(end)))
		}
		q.RecordRegExpMatch(r, s, []int{start, end})
		return storage.ObjectValue(q.Realm.NewLiteralMatchResult(matched, storage.Number(float64(start)), s)), true
	}
	if !re.CanMatchShort(text) {
		return storage.Undefined, false
	}
	m := &q.Realm.RegExpMatcher
	m.SetASCII(text, s.IsASCII())
	caps := re.Match(m, text, from, nil)
	if caps == nil {
		if stateful {
			q.SetRegExpLastIndex(r, storage.Number(0))
		}
		return storage.Null, true
	}
	if stateful {
		q.SetRegExpLastIndex(r, storage.Number(float64(caps[1])))
	}
	q.RecordRegExpMatch(r, s, caps)
	index := storage.Number(float64(caps[0]))
	n := re.GroupCount()
	if n == 0 {
		return storage.ObjectValue(q.Realm.NewLiteralMatchResult(q.MatchCapture(s, caps, 0), index, s)), true
	}
	a := q.Realm.NewArrayWithRoom(n+1, 3)
	for i := 0; i <= n; i++ {
		a.SetElement(int64(i), q.MatchCapture(s, caps, i))
	}
	q.Realm.DefineMatchResultProperties(a, index, s, storage.Undefined)
	return storage.ObjectValue(a), true
}

func plainMatchResult(re *regexp.Regexp) bool {
	return re.GroupCount() <= 5 && len(re.Names()) == 0 && re.Flags()&regexp.FlagHasIndices == 0
}

// findLiteral returns the UTF-16 span of the match or -1, -1 on a miss.
func findLiteral(s, literal wtf8.String, ascii bool, from int, flags regexp.Flags) (int, int) {
	sticky := flags&regexp.FlagSticky != 0
	byteOffset := from
	if !ascii {
		pos := wtf8.Seek(s, wtf8.Pos{}, from)
		byteOffset = pos.Byte
		if pos.Low {
			switch {
			case flags&(regexp.FlagUnicode|regexp.FlagUnicodeSets) != 0:
				from--
			case sticky:
				return -1, -1
			default:
				// A whole literal cannot start with the low half of the code point.
				from++
				byteOffset += 4
			}
		}
	}
	gap := literalByteIndex(s[byteOffset:], literal, sticky)
	if gap < 0 {
		return -1, -1
	}
	if ascii {
		start := from + gap
		return start, start + len(literal)
	}
	start := from + wtf8.Len(s[byteOffset:byteOffset+gap])
	return start, start + wtf8.Len(literal)
}

func literalByteIndex(s, literal wtf8.String, sticky bool) int {
	if sticky {
		if strings.HasPrefix(string(s), string(literal)) {
			return 0
		}
		return -1
	}
	return strings.Index(string(s), string(literal))
}

func StringReplaceKernel(q Context, this, pattern, replacement storage.Value) (storage.Value, bool) {
	if !this.IsString() || !replacement.IsString() ||
		this.StringByteLength() > maxRegExpKernelBytes || replacement.StringByteLength() > maxRegExpKernelBytes {
		return storage.Undefined, false
	}
	rep := replacement.Str()
	if strings.IndexByte(string(rep), '$') >= 0 {
		return storage.Undefined, false
	}
	text := this.Str()
	ascii := this.IsASCII() && replacement.IsASCII()
	if !ascii && (!this.IsWellFormed() || !replacement.IsWellFormed()) {
		// Removing a match or inserting replacement text can join lone surrogates.
		return storage.Undefined, false
	}
	if pattern.IsString() {
		if pattern.StringByteLength() > maxRegExpKernelBytes || !pattern.IsWellFormed() {
			return storage.Undefined, false
		}
		literal := pattern.Str()
		if !shortReplacement(len(text), len(literal), len(rep), 1) {
			return storage.Undefined, false
		}
		start := strings.Index(string(text), string(literal))
		if start < 0 {
			return this, true
		}
		var buf [256]byte
		out := append(buf[:0], text[:start]...)
		out = append(out, rep...)
		out = append(out, text[start+len(literal):]...)
		return replacedString(q, out, ascii), true
	}
	if !pattern.IsObject() {
		return storage.Undefined, false
	}
	r := pattern.Object()
	re := q.UnmodifiedRegExpMatcher(r)
	if re == nil || !plainMatchResult(re) {
		return storage.Undefined, false
	}
	if lastIndex, _ := q.RegExpLastIndex(r); !lastIndex.IsNumber() {
		return storage.Undefined, false
	}
	flags := re.Flags()
	global, sticky := flags&regexp.FlagGlobal != 0, flags&regexp.FlagSticky != 0
	if sticky && !global {
		return storage.Undefined, false
	}
	if literal, _, ok := r.RegExpLiteral(); ok && len(literal) <= maxRegExpKernelBytes {
		maxMatches := 1
		if global {
			maxMatches = len(text) / len(literal)
		}
		if !shortReplacement(len(text), len(literal), len(rep), maxMatches) {
			return storage.Undefined, false
		}
		return replaceLiteralRegExp(q, r, this, text, literal, rep, ascii, global, sticky), true
	}
	if !ascii || !re.CanMatchShort(text) {
		return storage.Undefined, false
	}
	maxMatches := 1
	if global {
		maxMatches = len(text) + 1
	}
	if !shortReplacement(len(text), 0, len(rep), maxMatches) {
		return storage.Undefined, false
	}
	return replaceShortRegExp(q, r, re, this, text, rep, global), true
}

// Limit the output before setting lastIndex or recording matches. Larger results and any
// invalid string length error are handled by the generic call.
func shortReplacement(subject, match, replacement, maxMatches int) bool {
	n := int64(subject) + int64(max(replacement-match, 0))*int64(maxMatches)
	return n <= min(storage.MaxStringLength, maxRegExpKernelBytes)
}

func replacedString(q Context, b []byte, ascii bool) storage.Value {
	if ascii {
		return q.NewASCIIStringBytes(b)
	}
	return q.NewString(wtf8.String(b))
}

func replaceLiteralRegExp(q Context, r *storage.Object, s storage.Value, text, literal, rep wtf8.String, ascii, global, sticky bool) storage.Value {
	var buf [512]byte
	out := buf[:0]
	next, last := 0, -1
	for {
		gap := literalByteIndex(text[next:], literal, sticky)
		if gap < 0 {
			break
		}
		last = next + gap
		out = append(out, text[next:last]...)
		out = append(out, rep...)
		next = last + len(literal)
		if !global {
			break
		}
	}
	if global {
		q.SetRegExpLastIndex(r, storage.Number(0))
	}
	if last < 0 {
		return s
	}
	start, end := last, next
	if !s.IsASCII() {
		start = wtf8.Len(text[:last])
		end = start + wtf8.Len(literal)
	}
	q.RecordRegExpMatch(r, s, []int{start, end})
	return replacedString(q, append(out, text[next:]...), ascii)
}

func replaceShortRegExp(q Context, r *storage.Object, re *regexp.Regexp, s storage.Value, text, rep wtf8.String, global bool) storage.Value {
	var buf [512]byte
	out := buf[:0]
	var last [12]int
	var n, next int
	m := &q.Realm.RegExpMatcher
	m.SetASCII(text, true)
	for pos := 0; pos <= len(text); {
		caps := re.Match(m, text, pos, nil)
		if caps == nil {
			break
		}
		n = copy(last[:], caps)
		out = append(out, text[next:caps[0]]...)
		out = append(out, rep...)
		next = caps[1]
		if !global {
			break
		}
		pos = next
		if caps[0] == next {
			pos++
		}
	}
	if global {
		q.SetRegExpLastIndex(r, storage.Number(0))
	}
	if n == 0 {
		return s
	}
	q.RecordRegExpMatch(r, s, last[:n])
	return q.NewASCIIStringBytes(append(out, text[next:]...))
}

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
