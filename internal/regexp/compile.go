// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package regexp

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

type instOp uint8

const (
	iMatch     instOp = iota
	iChar             // the character r
	iSet              // a character of the class cls
	iStr              // the WTF-8 bytes str (forward only)
	iJmp              // goto x
	iSplit            // try x, then y
	iSave             // caps[n] = pos
	iLineStart        // ^; multiline when n is 1
	iLineEnd          // $; multiline when n is 1
	iWordB            // \b, or \B when neg; n is 1 when the word characters include ſ and K
	iBackref          // refs; fold
	iLookStart        // neg, back; x is the instruction after the matching iLookEnd
	iLookEnd
	iRepStart // reg n
	iRepCheck // reg n; min, max, greedy; x is the exit
	iRepBody  // reg n; clears captures from y to z
	iRepEnd   // reg n; min; x is the iRepCheck
	iStar     // the single character of r or cls, from min to max times; greedy
)

type inst struct {
	op     instOp
	back   bool
	neg    bool
	greedy bool
	fold   bool
	n      int
	x, y   int
	z      int
	min    int
	max    int
	r      rune
	cls    *class
	str    wtf8.String
	refs   []int
}

// class is a compiled character class.
type class struct {
	ascii    [2]uint64
	set      charSet
	nonASCII bool
}

func newClass(s charSet) *class {
	c := &class{set: s}
	for i := 0; i < len(s); i += 2 {
		for r := s[i]; r <= s[i+1] && r < 0x80; r++ {
			c.ascii[r>>6] |= 1 << (r & 63)
		}
		if s[i+1] >= 0x80 {
			c.nonASCII = true
		}
	}
	return c
}

func (c *class) has(r rune) bool {
	if r < 0x80 {
		return c.ascii[r>>6]&(1<<(r&63)) != 0
	}
	return c.nonASCII && c.set.has(r)
}

type program struct {
	insts   []inst
	nregs   int
	ncaps   int
	unicode bool

	// anchored is set when every match starts at the beginning of the input.
	anchored bool

	// first, when not nil, tells by the byte at a position whether a match can start there.
	first *[256]bool

	// prefix, when not empty, is a literal that every match starts with.
	prefix wtf8.String

	// need, when not empty, is a literal that every match contains, starting at least
	// needMin and at most needMax characters after the start of the match. needMax is
	// unbounded when there is no upper bound.
	need    wtf8.String
	needMin int
	needMax int

	// prefixKey and needKey are the offsets in prefix and need of the bytes that a search for
	// them looks for first.
	prefixKey int
	needKey   int

	// whole is set when the pattern is prefix alone, without groups.
	whole bool
}

type compiler struct {
	prog    *program
	unicode bool
	poller  Poller
	steps   int

	// foldClasses are the classes of the characters that match their case variants, by the
	// character.
	foldClasses map[rune]*class
}

func compile(t *tree, flags Flags, poller Poller) *program {
	c := &compiler{
		prog: &program{
			ncaps:   2 * (t.groupCount + 1),
			unicode: flags&(FlagUnicode|FlagUnicodeSets) != 0,
		},
		unicode: flags&(FlagUnicode|FlagUnicodeSets) != 0,
		poller:  poller,
	}
	c.emit(inst{op: iSave, n: 0})
	c.node(t.root, false)
	c.emit(inst{op: iSave, n: 1})
	c.emit(inst{op: iMatch})
	c.analyze(t.root)
	return c.prog
}

// step counts n steps of the compiler, and calls the poller every pollSteps steps.
func (c *compiler) step(n int) {
	c.steps += n
	if c.steps >= pollSteps {
		c.steps = 0
		if c.poller != nil {
			c.poller.Poll()
		}
	}
}

func (c *compiler) emit(i inst) int {
	c.prog.insts = append(c.prog.insts, i)
	return len(c.prog.insts) - 1
}

func (c *compiler) pc() int {
	return len(c.prog.insts)
}

func (c *compiler) newReg() int {
	c.prog.nregs++
	return c.prog.nregs - 1
}

// charInst returns the instruction that matches the character r of the node n.
func (c *compiler) charInst(n *node, r rune, back bool) inst {
	if n.flags&FlagIgnoreCase != 0 {
		if cls, ok := c.foldClasses[r]; ok {
			return inst{op: iSet, cls: cls, back: back}
		}
		eq := foldEquivalents(r, c.unicode)
		if len(eq) > 1 {
			cls := newClass(setOf(eq...))
			if c.foldClasses == nil {
				c.foldClasses = map[rune]*class{}
			}
			c.foldClasses[r] = cls
			return inst{op: iSet, cls: cls, back: back}
		}
	}
	return inst{op: iChar, r: r, back: back}
}

// literal reports whether the character r of a node with the flags matches only r.
func (c *compiler) literal(flags Flags, r rune) bool {
	return flags&FlagIgnoreCase == 0 || len(foldEquivalents(r, c.unicode)) <= 1
}

// chars returns the characters that n matches one after another, when n is a character or a
// literal.
func chars(n *node) ([]rune, bool) {
	switch n.op {
	case opChar:
		return []rune{n.r}, true
	case opLit:
		return n.set, true
	}
	return nil, false
}

func (c *compiler) node(n *node, back bool) {
	c.step(1)
	switch n.op {
	case opEmpty:
	case opChar:
		c.emit(c.charInst(n, n.r, back))
	case opLit:
		if back {
			for _, r := range slices.Backward(n.set) {
				c.step(1)
				c.emit(c.charInst(n, r, true))
			}
			return
		}
		c.seq([]*node{n})
	case opClass:
		c.emit(inst{op: iSet, cls: newClass(n.set), back: back})
	case opSeq:
		if back {
			for _, v := range slices.Backward(n.subs) {
				c.node(v, true)
			}
			return
		}
		c.seq(n.subs)
	case opAlt:
		var jumps []int
		for i, s := range n.subs {
			if i == len(n.subs)-1 {
				c.node(s, back)
				break
			}
			split := c.emit(inst{op: iSplit})
			c.prog.insts[split].x = c.pc()
			c.node(s, back)
			jumps = append(jumps, c.emit(inst{op: iJmp}))
			c.prog.insts[split].y = c.pc()
		}
		for _, j := range jumps {
			c.prog.insts[j].x = c.pc()
		}
	case opCapture:
		open, close := 2*n.cap, 2*n.cap+1
		if back {
			open, close = close, open
		}
		c.emit(inst{op: iSave, n: open})
		c.node(n.subs[0], back)
		c.emit(inst{op: iSave, n: close})
	case opGroup:
		c.node(n.subs[0], back)
	case opRepeat:
		c.repeat(n, back)
	case opLineStart:
		c.emit(inst{op: iLineStart, n: boolInt(n.flags&FlagMultiline != 0)})
	case opLineEnd:
		c.emit(inst{op: iLineEnd, n: boolInt(n.flags&FlagMultiline != 0)})
	case opWordB:
		c.emit(inst{op: iWordB, neg: n.neg, n: boolInt(c.unicode && n.flags&FlagIgnoreCase != 0)})
	case opLook:
		start := c.emit(inst{op: iLookStart, neg: n.neg, back: n.behind})
		c.node(n.subs[0], n.behind)
		c.emit(inst{op: iLookEnd, x: start})
		c.prog.insts[start].x = c.pc()
	case opBackref:
		c.emit(inst{op: iBackref, refs: n.refs, fold: n.flags&FlagIgnoreCase != 0, back: back})
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// seq compiles a sequence forward, joining runs of literal characters into strings.
func (c *compiler) seq(subs []*node) {
	var run []rune
	for _, n := range subs {
		rs, ok := chars(n)
		if !ok {
			c.literalRun(run)
			run = run[:0]
			c.node(n, false)
			continue
		}
		for _, r := range rs {
			c.step(1)
			if c.literal(n.flags, r) {
				run = append(run, r)
				continue
			}
			c.literalRun(run)
			run = run[:0]
			c.emit(c.charInst(n, r, false))
		}
	}
	c.literalRun(run)
}

// literalRun compiles a run of literal characters (code units without the u flag).
// Lone surrogates cannot be compared as bytes, so they get instructions of their own.
func (c *compiler) literalRun(run []rune) {
	var b []byte
	nchars := 0
	var first rune
	flush := func() {
		switch nchars {
		case 0:
		case 1:
			c.emit(inst{op: iChar, r: first})
		default:
			c.emit(inst{op: iStr, str: wtf8.String(b)})
		}
		b = b[:0]
		nchars = 0
	}
	for k := 0; k < len(run); k++ {
		r := run[k]
		if !c.unicode && isHigh(r) && k+1 < len(run) && isLow(run[k+1]) {
			// Without the u flag, a surrogate pair of the pattern is two characters that
			// match the two code units of a supplementary character of the input.
			if nchars == 0 {
				first = r
			}
			b = wtf8.AppendRune(b, combine(r, run[k+1]))
			k++
			nchars += 2
			continue
		}
		if r >= 0xD800 && r <= 0xDFFF {
			flush()
			c.emit(inst{op: iChar, r: r})
			continue
		}
		if nchars == 0 {
			first = r
		}
		nchars++
		b = wtf8.AppendRune(b, r)
	}
	flush()
}

// singleChar reports whether n matches exactly one character and has no captures,
// and returns the instruction that matches it.
func (c *compiler) singleChar(n *node) (inst, bool) {
	switch n.op {
	case opChar:
		return c.charInst(n, n.r, false), true
	case opClass:
		return inst{op: iSet, cls: newClass(n.set)}, true
	case opGroup:
		return c.singleChar(n.subs[0])
	}
	return inst{}, false
}

func (c *compiler) repeat(n *node, back bool) {
	sub := n.subs[0]
	if n.max == 0 {
		return
	}
	if n.min == 1 && n.max == 1 {
		c.node(sub, back)
		return
	}
	if !back {
		if ch, ok := c.singleChar(sub); ok {
			i := inst{op: iStar, min: n.min, max: n.max, greedy: n.greedy}
			if ch.op == iChar {
				i.r = ch.r
			} else {
				i.cls = ch.cls
				i.n = 1
			}
			c.emit(i)
			return
		}
	}
	if lo, _ := width(sub); lo > 0 {
		// An iteration cannot match the empty string, so the loop needs neither a count nor the
		// check for an empty iteration. Each iteration starts where the one before ended.
		switch {
		case n.min == 0 && n.max == 1:
			// A single iteration leaves no captures of an earlier one to clear.
			split := c.emit(inst{op: iSplit})
			c.node(sub, back)
			c.choose(split, split+1, c.pc(), n.greedy)
			return
		case n.min <= 1 && n.max == unbounded && (n.cap == n.capEnd || sub.op == opCapture && n.capEnd-n.cap == 1):
			// The captures of an earlier iteration need no clearing either: there are none, or
			// the iteration is a group without groups inside, which each iteration sets again.
			if n.min == 0 {
				split := c.emit(inst{op: iSplit})
				c.node(sub, back)
				c.emit(inst{op: iJmp, x: split})
				c.choose(split, split+1, c.pc(), n.greedy)
				return
			}
			body := c.pc()
			c.node(sub, back)
			split := c.emit(inst{op: iSplit})
			c.choose(split, body, split+1, n.greedy)
			return
		}
	}
	r := c.newReg()
	c.emit(inst{op: iRepStart, n: r})
	check := c.emit(inst{op: iRepCheck, n: r, min: n.min, max: n.max, greedy: n.greedy})
	c.emit(inst{op: iRepBody, n: r, y: 2 * n.cap, z: 2 * n.capEnd})
	c.node(sub, back)
	c.emit(inst{op: iRepEnd, n: r, min: n.min, x: check})
	c.prog.insts[check].x = c.pc()
}

// choose makes the iSplit at pc try more, the next iteration of a loop, before less when greedy,
// and the other way round otherwise.
func (c *compiler) choose(pc, more, less int, greedy bool) {
	in := &c.prog.insts[pc]
	if greedy {
		in.x, in.y = more, less
	} else {
		in.x, in.y = less, more
	}
}

// Analysis of where a match can start.

// firstInfo is what the start of a node can match: the characters in set, or anything when
// any is set. empty is set when the node can match the empty string.
type firstInfo struct {
	set   charSet
	any   bool
	empty bool
}

func (c *compiler) first(n *node) firstInfo {
	switch n.op {
	case opEmpty, opLineStart, opLineEnd, opWordB:
		return firstInfo{empty: true}
	case opChar, opLit:
		r := n.r
		if n.op == opLit {
			r = n.set[0]
		}
		if n.flags&FlagIgnoreCase != 0 {
			return firstInfo{set: setOf(foldEquivalents(r, c.unicode)...)}
		}
		return firstInfo{set: charSet{r, r}}
	case opClass:
		return firstInfo{set: n.set}
	case opSeq:
		// The ranges are collected first and normalized once.
		var acc firstInfo
		acc.empty = true
		for _, s := range n.subs {
			c.step(1)
			f := c.first(s)
			acc.set = append(acc.set, f.set...)
			acc.any = acc.any || f.any
			if !f.empty {
				acc.empty = false
				break
			}
		}
		acc.set = acc.set.normalize()
		return acc
	case opAlt:
		var acc firstInfo
		for _, s := range n.subs {
			c.step(1)
			f := c.first(s)
			acc.set = append(acc.set, f.set...)
			acc.any = acc.any || f.any
			acc.empty = acc.empty || f.empty
		}
		acc.set = acc.set.normalize()
		return acc
	case opCapture, opGroup:
		return c.first(n.subs[0])
	case opRepeat:
		if n.max == 0 {
			return firstInfo{empty: true}
		}
		f := c.first(n.subs[0])
		if n.min == 0 {
			f.empty = true
		}
		return f
	}
	// Lookarounds and backreferences.
	return firstInfo{any: true, empty: true}
}

// anchoredAtStart reports whether every match of n must start at the beginning of the input.
func anchoredAtStart(n *node) bool {
	switch n.op {
	case opLineStart:
		return n.flags&FlagMultiline == 0
	case opSeq:
		return len(n.subs) > 0 && anchoredAtStart(n.subs[0])
	case opAlt:
		for _, s := range n.subs {
			if !anchoredAtStart(s) {
				return false
			}
		}
		return true
	case opCapture, opGroup:
		return anchoredAtStart(n.subs[0])
	case opLook:
		return !n.neg && !n.behind && anchoredAtStart(n.subs[0])
	}
	return false
}

func (c *compiler) analyze(root *node) {
	c.prog.anchored = anchoredAtStart(root)
	if f := c.first(root); !f.any && !f.empty {
		t := new([256]bool)
		set := f.set
		for i := 0; i < len(set); i += 2 {
			lo, hi := set[i], set[i+1]
			markLeads(t, lo, hi, c.unicode)
		}
		c.prog.first = t
		// A literal prefix.
		if i := c.prog.insts[1]; i.op == iStr {
			c.prog.prefix = i.str
		}
	}
	c.analyzeNeed(root)
	if c.prog.need != "" && c.prog.needMax == 0 {
		// Every match starts with the literal.
		if len(c.prog.need) > len(c.prog.prefix) {
			c.prog.prefix = c.prog.need
		}
		c.prog.need = ""
	}
	c.prog.prefixKey = rareByte(c.prog.prefix)
	c.prog.needKey = rareByte(c.prog.need)
	// A group in a repeat of no iterations has no instructions, so ncaps tells whether there are
	// any groups.
	if insts := c.prog.insts; c.prog.prefix != "" && len(insts) == 4 && c.prog.ncaps == 2 {
		switch i := insts[1]; i.op {
		case iStr:
			c.prog.whole = i.str == c.prog.prefix
		case iChar:
			c.prog.whole = wtf8.String(wtf8.AppendRune(nil, i.r)) == c.prog.prefix
		}
	}
}

// rareByte returns the offset of the byte of lit that is the least frequent in text, the first
// one of them on a tie, or 0 for an empty lit.
func rareByte(lit wtf8.String) int {
	k := 0
	for i := 1; i < len(lit); i++ {
		if byteRarity[lit[i]] > byteRarity[lit[k]] {
			k = i
		}
	}
	return k
}

// byteRarity ranks the bytes by how rarely they occur in text, markup and code: a byte ranks
// above the bytes that are more frequent.
var byteRarity = func() [256]uint8 {
	// The ASCII characters from the most frequent on. The others are rarer than all of them.
	const common = " etaoinsrhldcu\"m=/.,<>pfg-wy:b_\\v0k1'2;()\n\t&%#?x3456789jqzETAOINSRHLDCUMPFGWYBVKXJQZ[]{}+*!@$^`|~\r"
	var r [256]uint8
	for b := range r {
		switch {
		case b >= 0xC0:
			// Lead bytes of non-ASCII characters: a text in one script repeats a few of them.
			r[b] = uint8(len(common))
		case b >= 0x80:
			r[b] = uint8(len(common) + 1)
		default:
			r[b] = uint8(len(common) + 2)
		}
	}
	for i := range len(common) {
		r[common[i]] = uint8(i)
	}
	return r
}()

// analyzeNeed finds the longest literal that every match contains in its top-level sequence.
func (c *compiler) analyzeNeed(root *node) {
	nodes := appendSeq(nil, root)
	// lo and hi are the bounds of the offset of the current character from the start of a
	// match, and runLo and runHi those of the start of the literal b.
	var lo, hi, runLo, runHi int
	var b []byte
	end := func() {
		if len(b) > len(c.prog.need) {
			c.prog.need, c.prog.needMin, c.prog.needMax = wtf8.String(b), runLo, runHi
		}
		b = b[:0]
	}
	for _, n := range nodes {
		rs, ok := chars(n)
		if !ok {
			end()
			l, h := width(n)
			lo, hi = addMin(lo, l), addMax(hi, h)
			continue
		}
		for _, r := range rs {
			c.step(1)
			// A lone surrogate cannot be found as bytes, since it can be half of a pair.
			if !c.literal(n.flags, r) || r >= 0xD800 && r <= 0xDFFF {
				end()
			} else {
				if len(b) == 0 {
					runLo, runHi = lo, hi
				}
				b = wtf8.AppendRune(b, r)
			}
			lo, hi = addMin(lo, 1), addMax(hi, 1)
		}
	}
	end()
}

// appendSeq appends the nodes that every match of n matches one after another.
func appendSeq(nodes []*node, n *node) []*node {
	switch n.op {
	case opSeq:
		for _, s := range n.subs {
			nodes = appendSeq(nodes, s)
		}
		return nodes
	case opCapture, opGroup:
		return appendSeq(nodes, n.subs[0])
	}
	return append(nodes, n)
}

// widthLimit bounds the widths that width computes. A lower bound beyond it is clamped to it,
// and an upper bound beyond it is unbounded.
const widthLimit = 1 << 16

// width returns the least and the greatest number of characters that a match of n spans.
// The greatest is unbounded when there is no upper bound.
func width(n *node) (lo, hi int) {
	switch n.op {
	case opChar, opClass:
		return 1, 1
	case opLit:
		return mulMin(1, len(n.set)), mulMax(1, len(n.set))
	case opSeq:
		for _, s := range n.subs {
			l, h := width(s)
			lo, hi = addMin(lo, l), addMax(hi, h)
		}
		return lo, hi
	case opAlt:
		for i, s := range n.subs {
			l, h := width(s)
			if i == 0 {
				lo, hi = l, h
				continue
			}
			lo = min(lo, l)
			if hi != unbounded && (h == unbounded || h > hi) {
				hi = h
			}
		}
		return lo, hi
	case opCapture, opGroup:
		return width(n.subs[0])
	case opRepeat:
		if n.max == 0 {
			return 0, 0
		}
		l, h := width(n.subs[0])
		return mulMin(l, n.min), mulMax(h, n.max)
	case opBackref:
		return 0, unbounded
	}
	// Empty nodes, assertions and lookarounds.
	return 0, 0
}

func addMin(a, b int) int {
	return min(a+b, widthLimit)
}

func addMax(a, b int) int {
	if a == unbounded || b == unbounded || a+b > widthLimit {
		return unbounded
	}
	return a + b
}

func mulMin(a, n int) int {
	if a != 0 && n > widthLimit/a {
		return widthLimit
	}
	return a * n
}

func mulMax(a, n int) int {
	if a == 0 {
		return 0
	}
	if a == unbounded || n == unbounded || n > widthLimit/a {
		return unbounded
	}
	return a * n
}

// markLeads marks the bytes that a position holding a character from lo to hi can start with.
func markLeads(t *[256]bool, lo, hi rune, unicodeMode bool) {
	segs := [][2]rune{{0, 0x7F}, {0x80, 0x7FF}, {0x800, 0xFFFF}, {0x10000, maxRune}}
	for _, sg := range segs {
		a, b := max(lo, sg[0]), min(hi, sg[1])
		if a > b {
			continue
		}
		for x := lead(a); x <= lead(b); x++ {
			t[x] = true
		}
	}
	if !unicodeMode {
		// A surrogate pair is two code units: the high one is at the lead byte of the
		// pair, and the low one at its third byte.
		if lo <= 0xDBFF && hi >= 0xD800 {
			for x := 0xF0; x <= 0xF4; x++ {
				t[x] = true
			}
		}
		if lo <= 0xDFFF && hi >= 0xDC00 {
			for x := 0x80; x <= 0xBF; x++ {
				t[x] = true
			}
		}
	}
}

func lead(r rune) byte {
	switch {
	case r < 0x80:
		return byte(r)
	case r < 0x800:
		return byte(0xC0 | r>>6)
	case r < 0x10000:
		return byte(0xE0 | r>>12)
	}
	return byte(0xF0 | r>>18)
}
