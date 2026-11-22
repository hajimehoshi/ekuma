// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package collate implements the Unicode Collation Algorithm with the CLDR root collation
// and the tailorings of CLDR.
package collate

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"io"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/unicode/collation"
	"github.com/hajimehoshi/ekuma/internal/unorm"
	"github.com/hajimehoshi/ekuma/internal/xerrors"
)

// A CE is a collation element: the primary weight in bits 33 to 63, the secondary weight
// in bits 13 to 32, the tertiary weight in bits 2 to 12, and the case in bits 0 and 1.
type ce uint64

func (c ce) primary() uint32   { return uint32(c >> 33) }
func (c ce) secondary() uint32 { return uint32(c>>13) & 0xFFFFF }
func (c ce) tertiary() uint32  { return uint32(c>>2) & 0x7FF }
func (c ce) caseBits() uint32  { return uint32(c) & 3 }

func makeCE(p, s, t, c uint32) ce {
	return ce(p)<<33 | ce(s)<<13 | ce(t)<<2 | ce(c)
}

// continuation reports whether c is the second CE of an implicit weight, whose primary
// weight is not a reorderable lead weight.
func (c ce) continuation() bool {
	return c.primary() != 0 && c.secondary() == 0 && c.tertiary() == 0
}

func (c ce) variable() bool {
	lead := c.primary() >> 15
	return !c.continuation() && lead >= collation.FirstVariable && lead <= collation.PunctTop
}

const (
	commonSecondary = 0x20 << 11
	commonTertiary  = 0x02 << 6
)

func isUpper(t uint32) bool {
	switch t {
	case 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0E, 0x11, 0x12, 0x1D:
		return true
	}
	return false
}

// rootCE converts a root CE value of tag 0 or 3.
func rootCE(v uint32) ce {
	p := v >> 16
	t := (v >> 2) & 0x1F
	var c uint32
	if p != 0 && isUpper(t) {
		c = 2
	}
	return makeCE(p<<15, ((v>>7)&0x1FF)<<11, t<<6, c)
}

func u16(off int) uint32 {
	return uint32(binary.LittleEndian.Uint16([]byte(collation.Root[off : off+2])))
}

func u32(off int) uint32 {
	return binary.LittleEndian.Uint32([]byte(collation.Root[off : off+4]))
}

// rootValue returns the root value of the code point c.
func rootValue(c rune) uint32 {
	if c < 0 || c > unicode.MaxRune {
		return 0
	}
	mid := u16(int(c>>10) * 2)
	leaf := u16(collation.RootStage2 + int(mid*32+uint32(c>>5)&31)*2)
	return u32(collation.RootLeaves + int(leaf*32+uint32(c)&31)*4)
}

// appendValue appends the CEs of a root value of tag 0, 1 or 3.
func appendValue(ces []ce, v uint32) []ce {
	if v&3 != 1 {
		return append(ces, rootCE(v))
	}
	idx, n := int(v>>10), int(v>>2&0xFF)
	for i := range n {
		ces = append(ces, rootCE(u32(collation.RootExpansions+(idx+i)*4)))
	}
	return ces
}

// appendImplicit appends the implicit CEs of the code point c.
func appendImplicit(ces []ce, c rune) []ce {
	ims := collation.Implicits
	for i := 0; i < len(ims); i += 4 {
		if rune(ims[i]) <= c && c <= rune(ims[i+1]) {
			second := (0x8000 | (ims[i+3] + uint32(c) - ims[i])) & 0xFFFF
			return append(ces, makeCE(ims[i+2]<<15, commonSecondary, commonTertiary, 0), makeCE(second<<15, 0, 0, 0))
		}
	}
	base := uint32(0xFBC0)
	if isUnified(c) {
		if 0x4E00 <= c && c <= 0x9FFF || 0xF900 <= c && c <= 0xFAFF {
			base = 0xFB40
		} else {
			base = 0xFB80
		}
	}
	return append(ces, makeCE((base+uint32(c>>15))<<15, commonSecondary, commonTertiary, 0), makeCE((0x8000|uint32(c&0x7FFF))<<15, 0, 0, 0))
}

func isUnified(c rune) bool {
	u := collation.UnifiedIdeographs
	i, _ := slices.BinarySearch(u, c)
	// u holds pairs of first and last code points.
	if i < len(u) && u[i] == c {
		return true
	}
	return i%2 == 1
}

// tailoring is a decoded tailoring.
type tailoring struct {
	backwards bool
	// caseFirst is "upper", "lower", "off", or empty when the tailoring does not set it.
	caseFirst string
	shifted   bool
	caseLevel bool
	// reorder holds (first, last, new first) triples of root primary weights.
	reorder []uint32

	// singles are the code points tailored alone without a prefix, sorted, and singleEnds
	// the end offsets of their CEs in ces.
	singles    []rune
	singleEnds []uint32
	ces        []ce
	// complex are the strings with a prefix or of more than one code point by their first
	// code point.
	complex map[rune]*complexEntries
	// low tells for the code points below lowLimit whether they are tailored: 0 for no, 1 for
	// a single code point, and 2 for complex strings.
	low [lowLimit]uint8
}

const lowLimit = 0x250

// complexEntries are the tailored strings with a prefix or of more than one code point that
// start with one code point.
type complexEntries struct {
	prefixed []complexEntry
	plain    complexCandidates
}

type complexEntry struct {
	prefix []rune
	// rest are the code points after the first one.
	rest []rune
	ces  []ce
}

type tailoringEntry struct {
	once sync.Once
	t    *tailoring
	err  error
}

var tailorings = make([]tailoringEntry, len(collation.TailoringKeys))

func (e *tailoringEntry) get(compressed string) *tailoring {
	e.once.Do(func() {
		e.t, e.err = loadTailoring(compressed)
	})
	if e.err != nil {
		panic(e.err)
	}
	return e.t
}

// lookupTailoring returns the tailoring of key, or nil.
func lookupTailoring(key string) *tailoring {
	i, ok := slices.BinarySearch(collation.TailoringKeys, key)
	if !ok {
		return nil
	}
	var start uint32
	if i > 0 {
		start = collation.TailoringEnds[i-1]
	}
	return tailorings[i].get(collation.Tailorings[start:collation.TailoringEnds[i]])
}

func loadTailoring(compressed string) (*tailoring, error) {
	r := flate.NewReader(strings.NewReader(compressed))
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, &xerrors.InternalError{
			Op:  "inflate collation tailoring",
			Err: err,
		}
	}
	return decodeTailoring(data)
}

func decodeTailoring(data []byte) (t *tailoring, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*xerrors.InternalError); ok {
				t, err = nil, e
			} else {
				panic(r)
			}
		}
	}()
	r := bytes.NewReader(data)
	u := func() uint64 {
		v, err := binary.ReadUvarint(r)
		if err != nil {
			panic(&xerrors.InternalError{
				Op:  "decode collation tailoring",
				Err: err,
			})
		}
		return v
	}
	t = &tailoring{
		complex: map[rune]*complexEntries{},
	}
	flags := u()
	t.backwards = flags&1 != 0
	switch {
	case flags&2 != 0:
		t.caseFirst = "upper"
	case flags&4 != 0:
		t.caseFirst = "lower"
	case flags&8 != 0:
		t.caseFirst = "off"
	}
	t.shifted = flags&16 != 0
	t.caseLevel = flags&32 != 0
	n := int(u())
	for range n * 3 {
		t.reorder = append(t.reorder, uint32(u()))
	}
	count := int(u())
	runes := func() []rune {
		n := int(u())
		if n == 0 {
			return nil
		}
		rs := make([]rune, n)
		for i := range rs {
			rs[i] = rune(u())
		}
		return rs
	}
	var prev uint32
	for range count {
		prefix := runes()
		str := runes()
		nc := int(u())
		start := len(t.ces)
		for range nc {
			z := u()
			d := int64(z>>1) ^ -int64(z&1)
			p := uint32(int64(prev) + d)
			prev = p
			s := uint32(u())
			tc := u()
			t.ces = append(t.ces, makeCE(p, s, uint32(tc>>2), uint32(tc&3)))
		}
		if len(prefix) == 0 && len(str) == 1 {
			t.singles = append(t.singles, str[0])
			t.singleEnds = append(t.singleEnds, uint32(len(t.ces)))
			continue
		}
		ces := slices.Clone(t.ces[start:])
		t.ces = t.ces[:start]
		es := t.complex[str[0]]
		if es == nil {
			es = &complexEntries{}
			t.complex[str[0]] = es
		}
		e := complexEntry{
			prefix: prefix,
			rest:   str[1:],
			ces:    ces,
		}
		if len(prefix) > 0 {
			es.prefixed = append(es.prefixed, e)
		} else {
			es.plain = append(es.plain, e)
		}
	}
	// The singles are sorted because the entries are sorted by their strings.
	for _, c := range t.singles {
		if c < lowLimit {
			t.low[c] = 1
		}
	}
	for c := range t.complex {
		if c < lowLimit {
			t.low[c] = 2
		}
	}
	return t, nil
}

// single returns the CEs of the code point c tailored alone.
func (t *tailoring) single(c rune) ([]ce, bool) {
	i, ok := slices.BinarySearch(t.singles, c)
	if !ok {
		return nil, false
	}
	start := uint32(0)
	if i > 0 {
		start = t.singleEnds[i-1]
	}
	return t.ces[start:t.singleEnds[i]], true
}

// Options are the options of a collator.
type Options struct {
	// Strength is 1 for primary, 2 for secondary and 3 for tertiary differences.
	Strength int
	// Backwards compares secondary weights in reverse order.
	Backwards bool
	// CaseLevel compares the case after the secondary weights.
	CaseLevel bool
	// CaseFirst is "upper", "lower", or "false" for the order of the tailoring.
	CaseFirst string
	Numeric   bool
	// IgnorePunctuation ignores spaces and punctuation.
	IgnorePunctuation bool
}

// Collator compares strings.
type Collator struct {
	t         *tailoring
	strength  int
	caseLevel bool
	// caseFirst is 0 for none, 1 for upper first and 2 for lower first.
	caseFirst int
	numeric   bool
	shifted   bool
	backwards bool
}

// find returns the tailoring of the type typ for the locale l or its parents.
func find(l, typ string) *tailoring {
	for {
		if t := lookupTailoring(l + "/" + typ); t != nil {
			return t
		}
		p, ok := locale.CollationParent(l)
		if !ok {
			return nil
		}
		l = p
	}
}

// tailoringOf returns the tailoring of the type typ for the CLDR locale l: "standard",
// "search", or another type of collation.
func tailoringOf(l, typ string) *tailoring {
	switch typ {
	case "standard":
		if d := defaultType(l); d != "" {
			return find(l, d)
		}
		return find(l, "standard")
	case "search":
		if t := find(l, "search"); t != nil {
			return t
		}
	}
	if t := find(l, typ); t != nil {
		return t
	}
	return tailoringOf(l, "standard")
}

// defaultType returns the default collation type of the locale l, or an empty string for
// "standard".
func defaultType(l string) string {
	for {
		if d, ok := collation.DefaultTypes[l]; ok {
			return d
		}
		if lookupTailoring(l+"/standard") != nil {
			return ""
		}
		p, ok := locale.CollationParent(l)
		if !ok {
			return ""
		}
		l = p
	}
}

// Defaults returns the case first setting ("upper", "lower" or "false") and whether
// punctuation is ignored by default for the type typ of the CLDR locale l.
func Defaults(l, typ string) (caseFirst string, ignorePunctuation bool) {
	t := tailoringOf(l, typ)
	if t == nil {
		return "false", false
	}
	switch t.caseFirst {
	case "upper", "lower":
		caseFirst = t.caseFirst
	default:
		caseFirst = "false"
	}
	return caseFirst, t.shifted
}

// New returns a collator for the type typ of the CLDR locale l.
func New(l, typ string, o Options) *Collator {
	c := &Collator{
		t:         tailoringOf(l, typ),
		strength:  o.Strength,
		backwards: o.Backwards,
		caseLevel: o.CaseLevel,
		numeric:   o.Numeric,
		shifted:   o.IgnorePunctuation,
	}
	switch o.CaseFirst {
	case "upper":
		c.caseFirst = 1
	case "lower":
		c.caseFirst = 2
	}
	if c.t != nil {
		c.backwards = c.backwards || c.t.backwards
		if c.t.caseLevel {
			c.caseLevel = true
		}
	}
	return c
}

// buffer holds the scratch data of one string.
type buffer struct {
	runes []rune
	ces   []ce
}

var buffers = sync.Pool{
	New: func() any {
		return &buffer{}
	},
}

// Compare returns -1, 0 or 1 as the well-formed string a sorts before, the same as, or after the
// well-formed string b.
func (c *Collator) Compare(a, b string) int {
	return c.ComparePolled(a, b, nil)
}

// ComparePolled is [Collator.Compare] that calls poll, which may be nil, now and then during a
// long comparison. poll may stop the comparison by panicking.
func (c *Collator) ComparePolled(a, b string, poll func()) int {
	if a == b {
		return 0
	}
	p := &pacer{
		poll: poll,
	}
	ba := buffers.Get().(*buffer)
	bb := buffers.Get().(*buffer)
	defer func() {
		ba.runes, ba.ces = ba.runes[:0], ba.ces[:0]
		bb.runes, bb.ces = bb.runes[:0], bb.ces[:0]
		buffers.Put(ba)
		buffers.Put(bb)
	}()
	ca := c.appendCEs(ba, a, p)
	cb := c.appendCEs(bb, b, p)
	return c.compareCEs(ca, cb, p)
}

// pollSteps is the number of steps, such as converting a code point or comparing a weight, between
// two calls of the poll function of a comparison.
const pollSteps = 1 << 14

// pacer calls the poll function of a comparison every pollSteps steps.
type pacer struct {
	poll  func()
	steps int
}

// step counts n steps.
func (p *pacer) step(n int) {
	p.steps += n
	if p.steps >= pollSteps {
		p.steps = 0
		if p.poll != nil {
			p.poll()
		}
	}
}

// appendCEs returns the CEs of s with the settings of c applied.
func (c *Collator) appendCEs(buf *buffer, s string, p *pacer) []ce {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if !ascii {
		s = unorm.String(norm.NFD, s, p.poll)
	}
	for _, r := range s {
		p.step(1)
		buf.runes = append(buf.runes, r)
	}
	ces := c.generate(buf.ces, buf.runes, p)
	if c.t != nil && len(c.t.reorder) > 0 {
		for i, e := range ces {
			if e.primary() != 0 && !e.continuation() {
				ces[i] = c.t.reorderCE(e)
			}
		}
	}
	buf.ces = ces
	return ces
}

func (t *tailoring) reorderCE(e ce) ce {
	p := e.primary()
	lead := p >> 15
	r := t.reorder
	for i := 0; i < len(r); i += 3 {
		if r[i] <= lead && lead <= r[i+1] {
			lead = r[i+2] + lead - r[i]
			np := lead<<15 | p&0x7FFF
			return e&(1<<33-1) | ce(np)<<33
		}
	}
	return e
}

// generate appends the CEs of the NFD code points rs to ces, before variable weighting.
func (c *Collator) generate(ces []ce, rs []rune, p *pacer) []ce {
	t := c.t
	afterVariable := false
	for i := 0; i < len(rs); {
		p.step(1)
		start := len(ces)
		r := rs[i]
		if c.numeric {
			if n, ok := c.appendNumber(&ces, rs[i:]); ok {
				i += n
				afterVariable = false
				continue
			}
		}
		consumed := 1
		handled := false
		if t != nil && (r >= lowLimit || t.low[r] != 0) {
			if es, ok := t.complex[r]; ok {
				var cs []ce
				cs, consumed, rs = matchComplex(es, rs, i)
				if cs != nil {
					ces = append(ces, cs...)
					handled = true
				}
			}
			if !handled {
				if cs, ok := t.single(r); ok {
					ces = append(ces, cs...)
					handled = true
					consumed = 1
				} else if _, ok := t.complex[r]; ok {
					// The tailoring changes the contractions of r: the root value of r
					// alone applies.
					v := rootValue(r)
					if v&3 == 2 {
						v = u32(collation.RootContractions + int(v>>2)*4)
					}
					ces = appendRootValue(ces, v, r)
					handled = true
					consumed = 1
				}
			}
		}
		if !handled {
			v := rootValue(r)
			if v&3 == 2 {
				var nv uint32
				nv, consumed, rs = matchRootContraction(v, rs, i)
				v = nv
			}
			ces = appendRootValue(ces, v, r)
		}
		i += consumed
		if c.shifted {
			for j := start; j < len(ces); j++ {
				e := ces[j]
				p := e.primary()
				switch {
				case p == 0:
					if afterVariable {
						ces[j] = 0
					}
				case e.variable():
					ces[j] = 0
					afterVariable = true
				default:
					afterVariable = false
				}
			}
		}
	}
	return ces
}

func appendRootValue(ces []ce, v uint32, r rune) []ce {
	if v == 0 {
		return appendImplicit(ces, r)
	}
	return appendValue(ces, v)
}

// CompareRootShiftedQuaternary breaks a tertiary tie at the quaternary level
// of the shifted root collation.
func CompareRootShiftedQuaternary(a, b string) int {
	return slices.Compare(rootShiftedQuaternaries(a), rootShiftedQuaternaries(b))
}

func rootShiftedQuaternaries(s string) []uint32 {
	var root Collator
	var buf buffer
	var p pacer
	var weights []uint32
	var afterVariable bool
	for _, e := range root.appendCEs(&buf, s, &p) {
		if e.primary() != 0 {
			afterVariable = e.variable()
		}
		switch {
		case afterVariable && e.primary() != 0:
			weights = append(weights, e.primary())
		case !afterVariable && e.tertiary() != 0:
			// The common quaternary weight sorts after every variable primary.
			weights = append(weights, 0xffffffff)
		}
	}
	return weights
}

// appendNumber appends the CEs of the digits at the start of rs as one number, and returns
// the number of the code points of the digits.
func (c *Collator) appendNumber(ces *[]ce, rs []rune) (int, bool) {
	n := 0
	var digits []byte
	for n < len(rs) {
		d, ok := digitValue(rs[n])
		if !ok {
			break
		}
		if len(digits) > 0 || d != 0 {
			digits = append(digits, d)
		}
		n++
	}
	if n == 0 {
		return 0, false
	}
	if len(digits) == 0 {
		digits = []byte{0}
	}
	const base = collation.DigitZero << 15
	l := min(len(digits), 0x3FFF)
	*ces = append(*ces, makeCE(base|0x4000|uint32(l), commonSecondary, commonTertiary, 0))
	for _, d := range digits {
		*ces = append(*ces, makeCE(base|0x4000|uint32(d), commonSecondary, commonTertiary, 0))
	}
	return n, true
}

// digitValue returns the value of a decimal digit.
func digitValue(r rune) (byte, bool) {
	if r < '0' || r > '9' && !unicode.Is(unicode.Nd, r) {
		return 0, false
	}
	v := rootValue(r)
	if v&3 != 0 || v == 0 {
		return 0, false
	}
	d := v>>16 - collation.DigitZero
	if d > 9 {
		return 0, false
	}
	return byte(d), true
}

func ccc(r rune) uint8 {
	if r < 0x300 {
		return 0
	}
	var b [utf8.UTFMax]byte
	n := utf8.EncodeRune(b[:], r)
	return norm.NFD.Properties(b[:n]).CCC()
}

// candidates are the strings of contractions that start with one code point, by the code
// points after the first one.
type candidates interface {
	len() int
	rest(i int) []rune
}

// matchContraction finds the longest contraction at rs[i], first contiguous and then
// discontiguous as UTS #10 does. It returns the index of the candidate (or -1 for the
// starter alone), the number of code points from rs[i] that the match consumes, and rs
// with the code points of a discontiguous match removed.
func matchContraction(cs candidates, rs []rune, i int) (int, int, []rune) {
	best, bestLen := -1, 0
	for k := range cs.len() {
		rest := cs.rest(k)
		if len(rest) > bestLen && i+1+len(rest) <= len(rs) && slices.Equal(rs[i+1:i+1+len(rest)], rest) {
			best, bestLen = k, len(rest)
		}
	}
	// Discontiguous matches: unblocked non-starters after the match.
	if j := i + 1 + bestLen; j >= len(rs) || ccc(rs[j]) == 0 {
		return best, 1 + bestLen, rs
	}
	matched := slices.Clone(rs[i+1 : i+1+bestLen])
	var skippedMax uint8
	skipped := false
	j := i + 1 + bestLen
	for j < len(rs) {
		cc := ccc(rs[j])
		if cc == 0 {
			break
		}
		if !skipped || cc > skippedMax {
			want := append(slices.Clone(matched), rs[j])
			found := -1
			for k := range cs.len() {
				if slices.Equal(cs.rest(k), want) {
					found = k
					break
				}
			}
			if found >= 0 && skipped {
				best = found
				matched = want
				rs = slices.Delete(slices.Clone(rs), j, j+1)
				continue
			}
			if found >= 0 {
				best = found
				matched = want
				bestLen++
				j++
				continue
			}
		}
		skipped = true
		skippedMax = max(skippedMax, cc)
		j++
	}
	return best, 1 + bestLen, rs
}

type rootCandidates []struct {
	rest  []rune
	value uint32
}

func (r rootCandidates) len() int          { return len(r) }
func (r rootCandidates) rest(i int) []rune { return r[i].rest }

// matchRootContraction matches a root contraction of the value v at rs[i].
func matchRootContraction(v uint32, rs []rune, i int) (uint32, int, []rune) {
	off := collation.RootContractions + int(v>>2)*4
	def := u32(off)
	n := int(u32(off + 4))
	off += 8
	cands := make(rootCandidates, n)
	for k := range n {
		l := int(u32(off))
		off += 4
		rest := make([]rune, l)
		for x := range l {
			rest[x] = rune(u32(off))
			off += 4
		}
		cands[k].rest = rest
		cands[k].value = u32(off)
		off += 4
	}
	k, consumed, rs := matchContraction(cands, rs, i)
	if k < 0 {
		return def, 1, rs
	}
	return cands[k].value, consumed, rs
}

type complexCandidates []complexEntry

func (c complexCandidates) len() int          { return len(c) }
func (c complexCandidates) rest(i int) []rune { return c[i].rest }

// matchComplex matches the tailored strings es at rs[i]. Prefixed strings come first, the
// one with the longest prefix winning.
func matchComplex(es *complexEntries, rs []rune, i int) ([]ce, int, []rune) {
	bestPrefix := -1
	var best *complexEntry
	for k := range es.prefixed {
		e := &es.prefixed[k]
		if len(e.prefix) <= bestPrefix || len(e.prefix) > i {
			continue
		}
		if !slices.Equal(rs[i-len(e.prefix):i], e.prefix) || i+1+len(e.rest) > len(rs) || !slices.Equal(rs[i+1:i+1+len(e.rest)], e.rest) {
			continue
		}
		bestPrefix = len(e.prefix)
		best = e
	}
	if best != nil {
		return best.ces, 1 + len(best.rest), rs
	}
	if len(es.plain) == 0 {
		return nil, 1, rs
	}
	k, consumed, rs := matchContraction(es.plain, rs, i)
	if k < 0 {
		return nil, 1, rs
	}
	return es.plain[k].ces, consumed, rs
}

// compareCEs compares the CEs of two strings level by level.
func (c *Collator) compareCEs(a, b []ce, p *pacer) int {
	if r := compareLevel(a, b, func(e ce) uint32 { return e.primary() }, p); r != 0 {
		return r
	}
	if c.strength >= 2 {
		sa, sb := a, b
		if c.backwards {
			sa, sb = reversed(a), reversed(b)
		}
		if r := compareLevel(sa, sb, func(e ce) uint32 { return e.secondary() }, p); r != 0 {
			return r
		}
	}
	if c.caseLevel {
		key := func(e ce) uint32 {
			if e.primary() == 0 || e.continuation() {
				return 0
			}
			cb := e.caseBits()
			if c.caseFirst == 1 {
				cb = 2 - cb
			}
			return cb + 1
		}
		if r := compareLevel(a, b, key, p); r != 0 {
			return r
		}
	}
	if c.strength >= 3 {
		key := func(e ce) uint32 {
			t := e.tertiary()
			if t == 0 {
				return 0
			}
			switch c.caseFirst {
			case 1:
				return (2-e.caseBits())<<11 | t
			case 2:
				return e.caseBits()<<11 | t
			}
			return t
		}
		if r := compareLevel(a, b, key, p); r != 0 {
			return r
		}
	}
	return 0
}

func reversed(ces []ce) []ce {
	r := slices.Clone(ces)
	slices.Reverse(r)
	return r
}

// compareLevel compares the non-zero weights that key returns.
func compareLevel(a, b []ce, key func(ce) uint32, p *pacer) int {
	i, j := 0, 0
	for {
		p.step(1)
		var wa, wb uint32
		for i < len(a) {
			if wa = key(a[i]); wa != 0 {
				break
			}
			i++
		}
		for j < len(b) {
			if wb = key(b[j]); wb != 0 {
				break
			}
			j++
		}
		if i >= len(a) || j >= len(b) {
			switch {
			case i < len(a):
				return 1
			case j < len(b):
				return -1
			}
			return 0
		}
		if wa != wb {
			if wa < wb {
				return -1
			}
			return 1
		}
		i++
		j++
	}
}
