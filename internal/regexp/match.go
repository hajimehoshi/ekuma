// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package regexp

import (
	"strings"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Positions.
//
// The matcher works on the bytes of the WTF-8 subject. A position is a byte offset, except
// that the low surrogate of a four-byte sequence, which is a code unit of its own, is at the
// offset of the sequence plus two. Such a position is called a middle position. Only matching
// without the u and v flags reaches them.

type frameKind uint8

const (
	fChoice   frameKind = iota // resume at pc with pos
	fCap                       // restore caps[n] to pos
	fReg                       // restore regs[n] to pos
	fLook                      // the start of a lookaround: the iLookStart at pc, at pos
	fStar                      // a greedy iStar at pc that can give back characters down to n
	fLazyStar                  // a lazy iStar at pc that can take more characters, n so far
)

type frame struct {
	kind frameKind
	pc   int32
	pos  int
	n    int
}

// Matcher holds the state of matching. It can be reused for any number of matches of any
// regular expressions, but not by several goroutines at once.
type Matcher struct {
	caps  []int
	regs  []int
	stack []frame
	looks []int
	out   []int

	s      string
	ascii  bool
	asciiS string // the subject ascii was computed for
	poller Poller
	steps  int

	// last is a known position of s, which speeds up the conversions of indices.
	lastS string
	last  wtf8.Pos
}

const pollInterval = 1 << 14

// Match matches re against s at the code unit index start, or after it unless re is sticky.
// It returns the start and end code unit indices of the whole match and of each capturing
// group, -1 for the groups that did not participate, or nil when there is no match.
// The result is valid until the next use of m. poller may be nil.
func (re *Regexp) Match(m *Matcher, s string, start int, poller Poller) []int {
	return re.match(m, s, start, re.flags&FlagSticky != 0, poller)
}

// Find is like [Regexp.Match], but it finds the first match at start or after it even when re is
// sticky.
func (re *Regexp) Find(m *Matcher, s string, start int, poller Poller) []int {
	return re.match(m, s, start, false, poller)
}

func (re *Regexp) match(m *Matcher, s string, start int, sticky bool, poller Poller) []int {
	prog := re.prog
	m.s = s
	if s != m.asciiS || len(s) == 0 {
		m.asciiS, m.ascii = s, len(s) == 0 || wtf8.IsASCII(s)
	}
	// The step budget carries over from the previous match, so that a loop of cheap
	// matches still polls.
	m.poller = poller
	if cap(m.caps) < prog.ncaps {
		m.caps = make([]int, prog.ncaps)
	}
	m.caps = m.caps[:prog.ncaps]
	if cap(m.regs) < 2*prog.nregs {
		m.regs = make([]int, 2*prog.nregs)
	}
	m.regs = m.regs[:2*prog.nregs]

	pos := m.bytePos(start)
	if prog.unicode && m.isMiddle(pos) {
		pos -= 2
	}
	// need is the position of the first prog.need at or after an earlier pos+prog.needMin.
	need := -1
	for {
		if prog.anchored && pos != 0 {
			return nil
		}
		if !sticky {
			pos = m.skip(prog, pos)
			if pos < 0 {
				return nil
			}
		}
		if prog.need != "" && need < pos+prog.needMin {
			// A character spans at least one byte.
			from := pos + prog.needMin
			if from > len(s) {
				return nil
			}
			i := strings.Index(s[from:], prog.need)
			if i < 0 {
				return nil
			}
			need = from + i
			// In an ASCII subject, a character spans exactly one byte.
			if !sticky && m.ascii && prog.needMax != unbounded && need-prog.needMax > pos {
				pos = need - prog.needMax
				continue
			}
		}
		if m.run(prog, pos) {
			return m.results()
		}
		if sticky || pos >= len(s) {
			return nil
		}
		pos = m.advance(pos, prog.unicode)
	}
}

// skip returns the first position from pos where a match can start, or -1.
func (m *Matcher) skip(prog *program, pos int) int {
	s := m.s
	if prog.prefix != "" {
		i := strings.Index(s[pos:], prog.prefix)
		if i < 0 {
			return -1
		}
		return pos + i
	}
	t := prog.first
	if t == nil {
		return pos
	}
	if m.ascii {
		for pos < len(s) && !t[s[pos]] {
			pos++
		}
		if pos >= len(s) {
			return -1
		}
		return pos
	}
	for pos < len(s) && !t[s[pos]] {
		pos = m.advance(pos, prog.unicode)
	}
	if pos >= len(s) {
		return -1
	}
	return pos
}

// advance returns the position of the next code unit, or of the next code point in unicode mode.
func (m *Matcher) advance(pos int, unicodeMode bool) int {
	s := m.s
	b := s[pos]
	switch {
	case b < 0x80:
		return pos + 1
	case b < 0xC0:
		return pos + 2
	case b < 0xE0:
		return pos + 2
	case b < 0xF0:
		return pos + 3
	}
	if unicodeMode {
		return pos + 4
	}
	return pos + 2
}

func (m *Matcher) isMiddle(pos int) bool {
	return pos < len(m.s) && m.s[pos]&0xC0 == 0x80
}

// bytePos converts the code unit index u to a position.
func (m *Matcher) bytePos(u int) int {
	if m.ascii {
		return u
	}
	from := wtf8.Pos{}
	if m.lastS == m.s {
		from = m.last
	}
	p := wtf8.Seek(m.s, from, u)
	m.lastS, m.last = m.s, p
	if p.Low {
		return p.Byte + 2
	}
	return p.Byte
}

// unitIndex converts the position pos to a code unit index.
func (m *Matcher) unitIndex(pos int) int {
	if m.ascii {
		return pos
	}
	s := m.s
	p := wtf8.Pos{}
	if m.lastS == s && m.last.Byte <= pos {
		p = m.last
		if p.Low {
			p.Low = false
			p.Unit--
		}
	}
	u, b := p.Unit, p.Byte
	for b < pos {
		c := s[b]
		switch {
		case c < 0x80:
			b++
			u++
		case c < 0xE0:
			b += 2
			u++
		case c < 0xF0:
			b += 3
			u++
		default:
			if b+2 == pos {
				return u + 1
			}
			b += 4
			u += 2
		}
	}
	m.lastS, m.last = s, wtf8.Pos{Unit: u, Byte: b}
	return u
}

func (m *Matcher) results() []int {
	n := len(m.caps)
	if cap(m.out) < n {
		m.out = make([]int, n)
	}
	m.out = m.out[:n]
	// Convert in increasing order of position, so that the conversion moves forward.
	for i, p := range m.caps {
		if p < 0 || i%2 == 1 && m.caps[i-1] < 0 {
			m.out[i] = -1
			continue
		}
		m.out[i] = m.unitIndex(p)
	}
	for i := 0; i < n; i += 2 {
		if m.out[i] < 0 || m.out[i+1] < 0 {
			m.out[i], m.out[i+1] = -1, -1
		}
	}
	return m.out
}

// next reads the character at pos. It returns the character and the position after it,
// or -1 at the end of the input.
func (m *Matcher) next(pos int, unicodeMode bool) (rune, int) {
	s := m.s
	if pos >= len(s) {
		return 0, -1
	}
	b := s[pos]
	switch {
	case b < 0x80:
		return rune(b), pos + 1
	case b < 0xC0:
		r := decode4(s[pos-2:])
		return 0xDC00 + (r-0x10000)&0x3FF, pos + 2
	case b < 0xE0:
		return rune(b&0x1F)<<6 | rune(s[pos+1]&0x3F), pos + 2
	case b < 0xF0:
		return rune(b&0x0F)<<12 | rune(s[pos+1]&0x3F)<<6 | rune(s[pos+2]&0x3F), pos + 3
	}
	r := decode4(s[pos:])
	if unicodeMode {
		return r, pos + 4
	}
	return 0xD800 + (r-0x10000)>>10, pos + 2
}

// prev reads the character before pos. It returns the character and its position,
// or -1 at the start of the input.
func (m *Matcher) prev(pos int, unicodeMode bool) (rune, int) {
	s := m.s
	if pos <= 0 {
		return 0, -1
	}
	if m.isMiddle(pos) {
		r := decode4(s[pos-2:])
		return 0xD800 + (r-0x10000)>>10, pos - 2
	}
	b := s[pos-1]
	if b < 0x80 {
		return rune(b), pos - 1
	}
	q := pos - 1
	for s[q]&0xC0 == 0x80 {
		q--
	}
	if s[q] >= 0xF0 {
		r := decode4(s[q:])
		if unicodeMode {
			return r, q
		}
		return 0xDC00 + (r-0x10000)&0x3FF, q + 2
	}
	r, _ := wtf8.DecodeRune(s[q:])
	return r, q
}

func decode4(s string) rune {
	return rune(s[0]&0x07)<<18 | rune(s[1]&0x3F)<<12 | rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F)
}

func isLineTerminator(r rune) bool {
	return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029
}

func (m *Matcher) isWordAt(r rune, extended bool) bool {
	if r < 0x80 {
		return wordASCII[r>>6]&(1<<(r&63)) != 0
	}
	return extended && (r == 0x17F || r == 0x212A)
}

var wordASCII = newClass(wordSet).ascii

func (m *Matcher) push(f frame) {
	m.stack = append(m.stack, f)
}

func (m *Matcher) setCap(i, pos int) {
	m.stack = append(m.stack, frame{kind: fCap, n: i, pos: m.caps[i]})
	m.caps[i] = pos
}

func (m *Matcher) setReg(i, v int) {
	m.stack = append(m.stack, frame{kind: fReg, n: i, pos: m.regs[i]})
	m.regs[i] = v
}

func (m *Matcher) poll() {
	m.steps = pollInterval
	if m.poller != nil {
		m.poller.Poll()
	}
}

// run tries to match prog at pos.
func (m *Matcher) run(prog *program, pos int) bool {
	for i := range m.caps {
		m.caps[i] = -1
	}
	m.stack = m.stack[:0]
	m.looks = m.looks[:0]
	insts := prog.insts
	uni := prog.unicode
	s := m.s
	pc := 0
	for {
		if m.steps--; m.steps <= 0 {
			m.poll()
		}
		in := &insts[pc]
		switch in.op {
		case iMatch:
			return true
		case iChar:
			var r rune
			var p int
			if in.back {
				r, p = m.prev(pos, uni)
			} else if pos < len(s) && s[pos] < 0x80 {
				r, p = rune(s[pos]), pos+1
			} else {
				r, p = m.next(pos, uni)
			}
			if p < 0 || r != in.r {
				goto fail
			}
			pos = p
			pc++
		case iSet:
			var r rune
			var p int
			if in.back {
				r, p = m.prev(pos, uni)
			} else if pos < len(s) && s[pos] < 0x80 {
				r, p = rune(s[pos]), pos+1
			} else {
				r, p = m.next(pos, uni)
			}
			if p < 0 || !in.cls.has(r) {
				goto fail
			}
			pos = p
			pc++
		case iStr:
			if !strings.HasPrefix(s[pos:], in.str) {
				goto fail
			}
			pos += len(in.str)
			pc++
		case iJmp:
			pc = in.x
		case iSplit:
			m.push(frame{kind: fChoice, pc: int32(in.y), pos: pos})
			pc = in.x
		case iSave:
			m.setCap(in.n, pos)
			pc++
		case iLineStart:
			if pos != 0 {
				if in.n == 0 {
					goto fail
				}
				if r, _ := m.prev(pos, uni); !isLineTerminator(r) {
					goto fail
				}
			}
			pc++
		case iLineEnd:
			if pos != len(s) {
				if in.n == 0 {
					goto fail
				}
				if r, _ := m.next(pos, uni); !isLineTerminator(r) {
					goto fail
				}
			}
			pc++
		case iWordB:
			a, b := false, false
			if r, p := m.prev(pos, uni); p >= 0 {
				a = m.isWordAt(r, in.n == 1)
			}
			if r, p := m.next(pos, uni); p >= 0 {
				b = m.isWordAt(r, in.n == 1)
			}
			if (a != b) == in.neg {
				goto fail
			}
			pc++
		case iBackref:
			p, ok := m.backref(in, pos, uni)
			if !ok {
				goto fail
			}
			pos = p
			pc++
		case iLookStart:
			m.looks = append(m.looks, len(m.stack))
			m.push(frame{kind: fLook, pc: int32(pc), pos: pos})
			pc++
		case iLookEnd:
			mark := m.looks[len(m.looks)-1]
			m.looks = m.looks[:len(m.looks)-1]
			start := &insts[in.x]
			pos = m.stack[mark].pos
			if start.neg {
				// The lookaround matched, so it fails: undo what it did.
				m.unwind(mark)
				goto fail
			}
			// Keep the effects but drop the alternatives inside the lookaround.
			out := mark
			for _, f := range m.stack[mark+1:] {
				if f.kind == fCap || f.kind == fReg {
					m.stack[out] = f
					out++
				}
			}
			m.stack = m.stack[:out]
			pc = start.x
		case iRepStart:
			m.setReg(in.n, 0)
			pc++
		case iRepCheck:
			c := m.regs[in.n]
			switch {
			case in.max != unbounded && c >= in.max:
				pc = in.x
			case c < in.min:
				pc++
			case in.greedy:
				m.push(frame{kind: fChoice, pc: int32(in.x), pos: pos})
				pc++
			default:
				m.push(frame{kind: fChoice, pc: int32(pc + 1), pos: pos})
				pc = in.x
			}
		case iRepBody:
			m.markIteration(in, pos)
			pc++
		case iRepEnd:
			c := m.regs[in.n]
			if c >= in.min && pos == m.iterationStart(in.n) {
				goto fail
			}
			m.setReg(in.n, c+1)
			pc = in.x
		case iStar:
			pos2, ok := m.star(in, pc, pos, uni)
			if !ok {
				goto fail
			}
			pos = pos2
			pc++
		}
		continue

	fail:
		for {
			if len(m.stack) == 0 {
				return false
			}
			f := m.stack[len(m.stack)-1]
			m.stack = m.stack[:len(m.stack)-1]
			switch f.kind {
			case fChoice:
				pc, pos = int(f.pc), f.pos
			case fCap:
				m.caps[f.n] = f.pos
				continue
			case fReg:
				m.regs[f.n] = f.pos
				continue
			case fLook:
				m.looks = m.looks[:len(m.looks)-1]
				start := &insts[f.pc]
				if !start.neg {
					continue
				}
				// The negative lookaround did not match, so it succeeds.
				pc, pos = start.x, f.pos
			case fStar:
				// Give back one character.
				var p int
				if m.ascii {
					p = f.pos - 1
				} else {
					_, p = m.prev(f.pos, uni)
				}
				if p > f.n {
					m.push(frame{kind: fStar, pc: f.pc, pos: p, n: f.n})
				}
				pc, pos = int(f.pc)+1, p
			case fLazyStar:
				// Take one more character.
				in := &insts[f.pc]
				p, ok := m.oneChar(in, f.pos, uni)
				if !ok {
					continue
				}
				if in.max == unbounded || f.n+1 < in.max {
					m.push(frame{kind: fLazyStar, pc: f.pc, pos: p, n: f.n + 1})
				}
				pc, pos = int(f.pc)+1, p
			}
			if m.steps--; m.steps <= 0 {
				m.poll()
			}
			break
		}
	}
}

// The registers of a loop: regs[n] is the count. The start of the current iteration is kept
// in the frames: markIteration records it in a register allocated after all the counts.

func (m *Matcher) markIteration(in *inst, pos int) {
	m.setReg(len(m.regs)/2+in.n, pos)
	for i := in.y; i < in.z; i++ {
		if m.caps[i] >= 0 {
			m.setCap(i, -1)
		}
	}
}

func (m *Matcher) iterationStart(n int) int {
	return m.regs[len(m.regs)/2+n]
}

// unwind pops the frames down to and including the frame at mark, undoing their effects.
func (m *Matcher) unwind(mark int) {
	for len(m.stack) > mark {
		f := m.stack[len(m.stack)-1]
		m.stack = m.stack[:len(m.stack)-1]
		switch f.kind {
		case fCap:
			m.caps[f.n] = f.pos
		case fReg:
			m.regs[f.n] = f.pos
		case fLook:
			if len(m.stack) > mark {
				m.looks = m.looks[:len(m.looks)-1]
			}
		}
	}
}

// oneChar matches the single character of the iStar in at pos.
func (m *Matcher) oneChar(in *inst, pos int, uni bool) (int, bool) {
	s := m.s
	var r rune
	var p int
	if pos < len(s) && s[pos] < 0x80 {
		r, p = rune(s[pos]), pos+1
	} else {
		r, p = m.next(pos, uni)
		if p < 0 {
			return 0, false
		}
	}
	if in.n == 1 {
		if !in.cls.has(r) {
			return 0, false
		}
	} else if r != in.r {
		return 0, false
	}
	return p, true
}

// star runs the iStar in at pc from pos.
func (m *Matcher) star(in *inst, pc, pos int, uni bool) (int, bool) {
	for range in.min {
		p, ok := m.oneChar(in, pos, uni)
		if !ok {
			return 0, false
		}
		pos = p
	}
	if in.max != unbounded && in.max == in.min {
		return pos, true
	}
	if !in.greedy {
		m.push(frame{kind: fLazyStar, pc: int32(pc), pos: pos, n: in.min})
		return pos, true
	}
	minPos := pos
	n := in.min
	s := m.s
	if m.ascii && in.n == 1 {
		cls := in.cls
		for pos < len(s) && (in.max == unbounded || n < in.max) && cls.has(rune(s[pos])) {
			pos++
			n++
		}
	} else {
		for in.max == unbounded || n < in.max {
			p, ok := m.oneChar(in, pos, uni)
			if !ok {
				break
			}
			pos = p
			n++
		}
	}
	if pos > minPos {
		m.push(frame{kind: fStar, pc: int32(pc), pos: pos, n: minPos})
	}
	return pos, true
}

// backref matches the backreference in at pos.
func (m *Matcher) backref(in *inst, pos int, uni bool) (int, bool) {
	start, end := -1, -1
	for _, g := range in.refs {
		if a, b := m.caps[2*g], m.caps[2*g+1]; a >= 0 && b >= 0 {
			start, end = a, b
			break
		}
	}
	if start < 0 {
		return pos, true
	}
	s := m.s
	if !in.fold && !in.back && !m.isMiddle(start) && !m.isMiddle(end) && !m.isMiddle(pos) &&
		!endsWithHighSurrogate(s[start:end]) && !startsWithLowSurrogate(s[start:end]) {
		// Whole characters compare equal exactly when their bytes do.
		if strings.HasPrefix(s[pos:], s[start:end]) {
			return pos + end - start, true
		}
		return 0, false
	}
	canon := canonUCS2
	if uni {
		canon = canonUnicode
	}
	if in.back {
		q := end
		for q > start {
			a, qa := m.prev(q, uni)
			b, pb := m.prev(pos, uni)
			if pb < 0 || a != b && (!in.fold || canon(a) != canon(b)) {
				return 0, false
			}
			q, pos = qa, pb
		}
		return pos, true
	}
	q := start
	for q < end {
		a, qa := m.next(q, uni)
		b, pb := m.next(pos, uni)
		if pb < 0 || a != b && (!in.fold || canon(a) != canon(b)) {
			return 0, false
		}
		q, pos = qa, pb
	}
	return pos, true
}

func endsWithHighSurrogate(s string) bool {
	n := len(s)
	return n >= 3 && s[n-3] == 0xED && s[n-2] >= 0xA0 && s[n-2] < 0xB0
}

func startsWithLowSurrogate(s string) bool {
	return len(s) >= 3 && s[0] == 0xED && s[1] >= 0xB0
}
