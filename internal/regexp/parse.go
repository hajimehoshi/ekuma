// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package regexp

import (
	"cmp"
	"maps"
	"math"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

type nodeOp uint8

const (
	opEmpty     nodeOp = iota
	opChar             // r
	opLit              // the characters of set one after another, not ranges, at least two
	opClass            // set
	opSeq              // subs
	opAlt              // subs
	opCapture          // capture index cap, subs[0]
	opGroup            // subs[0], with the flags of the node (a modifier group or a plain group)
	opRepeat           // subs[0] repeated from min to max (-1 is unbounded), captures from cap to capEnd
	opLineStart        // ^
	opLineEnd          // $
	opWordB            // \b, or \B when neg
	opLook             // subs[0] as a lookaround: behind, neg
	opBackref          // refs
)

// node is a node of the parsed pattern. flags are the flags that apply to the node,
// after the modifiers of the enclosing groups.
type node struct {
	op     nodeOp
	flags  Flags
	neg    bool
	behind bool
	greedy bool
	r      rune
	set    charSet
	subs   []*node
	min    int
	max    int
	cap    int
	capEnd int
	refs   []int
	name   wtf8.String
}

// unbounded is the max of a quantifier without an upper bound.
const unbounded = -1

// countLimit is the largest count of a quantifier. Larger counts are clamped to it, which
// cannot be observed since no string is that long.
const countLimit = math.MaxInt32

type tree struct {
	root       *node
	groupCount int
	groupNames []wtf8.String
	names      []wtf8.String
}

type groupPath []pathStep

type pathStep struct {
	disj int
	alt  int
}

type parser struct {
	src   []rune
	pos   int
	flags Flags

	unicode     bool // u or v: the pattern is a sequence of code points
	unicodeSets bool // v
	named       bool // the NamedCaptureGroups parameter of the grammar

	// validateOnly makes the parser check the pattern without building nodes or character sets.
	validateOnly bool

	groupCount  int // the total, from the prescan
	nextGroup   int
	groupNames  []wtf8.String // by group index, or nil before the first named group
	groupPaths  []groupPath   // by group index, for the named groups
	path        groupPath
	disjCounter int
	namedRefs   []namedRef
	depth       int

	// spare is a node of a character that a literal took over, for the next character.
	spare *node

	poller Poller
	steps  int

	err *Error
}

// namedRef is a backreference by name. node is nil when the parser only validates.
type namedRef struct {
	name wtf8.String
	node *node
}

type syntaxPanic struct{}

func (p *parser) fail(msg string) {
	p.err = &Error{Msg: msg}
	panic(syntaxPanic{})
}

// parse parses the pattern source with the flags. poller, which may be nil,
// is called now and then during a long pattern.
func parse(source wtf8.String, flags Flags, poller Poller) (*tree, error) {
	p := parser{
		poller: poller,
	}
	p.init(source, flags)
	return p.parsePattern()
}

// validate reports the error that parse reports for the pattern source with the flags, if any.
// poller, which may be nil, is called now and then during a long pattern.
func validate(source wtf8.String, flags Flags, poller Poller) error {
	p := parser{
		validateOnly: true,
		poller:       poller,
	}
	p.init(source, flags)
	_, err := p.parsePattern()
	return err
}

func (p *parser) init(source wtf8.String, flags Flags) {
	p.flags = flags
	p.unicode = flags&(FlagUnicode|FlagUnicodeSets) != 0
	p.unicodeSets = flags&FlagUnicodeSets != 0
	// A WTF-8 string has at least as many bytes as code units.
	p.src = make([]rune, 0, len(source))
	for i := 0; i < len(source); {
		p.step()
		r, size := wtf8.DecodeRune(source[i:])
		i += size
		if !p.unicode && r >= 0x10000 {
			r -= 0x10000
			p.src = append(p.src, 0xD800+r>>10, 0xDC00+r&0x3FF)
			continue
		}
		p.src = append(p.src, r)
	}
	count, hasNames := p.prescan()
	p.groupCount = count
	p.named = p.unicode || hasNames
}

// prescan counts the capturing groups and reports whether there is a group name.
func (p *parser) prescan() (int, bool) {
	n := 0
	named := false
	depth := 0
	for i := 0; i < len(p.src); i++ {
		switch p.src[i] {
		case '\\':
			i++
		case '[':
			if depth == 0 || p.unicodeSets {
				depth++
			}
		case ']':
			if depth > 0 {
				depth--
			}
		case '(':
			if depth > 0 {
				continue
			}
			if i+1 < len(p.src) && p.src[i+1] == '?' {
				if i+2 < len(p.src) && p.src[i+2] == '<' && i+3 < len(p.src) && p.src[i+3] != '=' && p.src[i+3] != '!' {
					n++
					named = true
				}
				continue
			}
			n++
		}
	}
	return n, named
}

// step counts a step of the parser, and calls the poller every pollSteps steps.
func (p *parser) step() {
	p.steps++
	if p.steps >= pollSteps {
		p.steps = 0
		if p.poller != nil {
			p.poller.Poll()
		}
	}
}

func (p *parser) eof() bool {
	return p.pos >= len(p.src)
}

func (p *parser) peek() rune {
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return -1
}

func (p *parser) peekAt(i int) rune {
	if p.pos+i < len(p.src) {
		return p.src[p.pos+i]
	}
	return -1
}

func (p *parser) lookingAt(s string) bool {
	i := 0
	for _, c := range s {
		if p.peekAt(i) != c {
			return false
		}
		i++
	}
	return true
}

// parsePattern parses the whole pattern. The tree is nil when the parser only validates.
func (p *parser) parsePattern() (t *tree, err error) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(syntaxPanic); !ok {
				panic(r)
			}
			t, err = nil, p.err
		}
	}()
	root := p.parseDisjunction()
	if !p.eof() {
		if p.peek() == ')' {
			p.fail("unmatched ')'")
		}
		p.fail("unexpected character")
	}
	// Resolve the named backreferences.
	byName := map[wtf8.String][]int{}
	var names []wtf8.String
	for i, n := range p.groupNames {
		if n == "" {
			continue
		}
		if _, ok := byName[n]; !ok {
			names = append(names, n)
		}
		byName[n] = append(byName[n], i)
	}
	for _, r := range p.namedRefs {
		refs, ok := byName[r.name]
		if !ok {
			p.fail("invalid named capture referenced")
		}
		if r.node != nil {
			r.node.refs = refs
		}
	}
	// Two of the paths of a name might both participate exactly when two that are adjacent in
	// the lexicographic order might: a path between two such paths shares their common prefix,
	// and it might participate with one of the two.
	var paths []groupPath
	for _, list := range byName {
		if len(list) < 2 {
			continue
		}
		paths = paths[:0]
		for _, g := range list {
			paths = append(paths, p.groupPaths[g])
		}
		slices.SortFunc(paths, comparePaths)
		for i := 1; i < len(paths); i++ {
			if mightBothParticipate(paths[i-1], paths[i]) {
				p.fail("duplicate capture group name")
			}
		}
	}
	if p.validateOnly {
		return nil, nil
	}
	groupNames := p.groupNames
	if groupNames == nil {
		groupNames = make([]wtf8.String, p.groupCount+1)
	}
	return &tree{
		root:       root,
		groupCount: p.groupCount,
		groupNames: groupNames,
		names:      names,
	}, nil
}

// nameGroup records the name of the capturing group idx and the path to it.
func (p *parser) nameGroup(idx int, name wtf8.String) {
	if p.groupNames == nil {
		p.groupNames = make([]wtf8.String, p.groupCount+1)
		p.groupPaths = make([]groupPath, p.groupCount+1)
	}
	p.groupNames[idx] = name
	p.groupPaths[idx] = slices.Clone(p.path)
}

// mightBothParticipate reports whether the groups at the paths a and b are not in different
// alternatives of one disjunction.
func mightBothParticipate(a, b groupPath) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			continue
		}
		return a[i].disj != b[i].disj
	}
	return true
}

// comparePaths compares the paths a and b lexicographically, by the disjunctions first.
func comparePaths(a, b groupPath) int {
	return slices.CompareFunc(a, b, func(x, y pathStep) int {
		return cmp.Or(cmp.Compare(x.disj, y.disj), cmp.Compare(x.alt, y.alt))
	})
}

func (p *parser) newNode(op nodeOp) *node {
	return &node{op: op, flags: p.flags}
}

// enter counts a level of nesting, which leave ends.
func (p *parser) enter() {
	p.depth++
	if p.depth > maxDepth {
		p.fail("regular expression is nested too deeply")
	}
}

func (p *parser) leave() {
	p.depth--
}

func (p *parser) parseDisjunction() *node {
	p.enter()
	defer p.leave()
	id := p.disjCounter
	p.disjCounter++
	p.path = append(p.path, pathStep{disj: id})
	var alts []*node
	for i := 0; ; i++ {
		p.path[len(p.path)-1].alt = i
		alt := p.parseAlternative()
		if !p.validateOnly {
			alts = append(alts, alt)
		}
		if p.peek() != '|' {
			break
		}
		p.pos++
	}
	p.path = p.path[:len(p.path)-1]
	if p.validateOnly {
		return nil
	}
	if len(alts) == 1 {
		return alts[0]
	}
	n := p.newNode(opAlt)
	n.subs = alts
	return n
}

func (p *parser) parseAlternative() *node {
	var terms []*node
	for !p.eof() && p.peek() != '|' && p.peek() != ')' {
		p.step()
		term := p.parseTerm()
		if p.validateOnly {
			continue
		}
		// Characters one after another make one literal.
		if n := len(terms); n > 0 && term.op == opChar {
			if last := terms[n-1]; (last.op == opChar || last.op == opLit) && last.flags == term.flags {
				if last.op == opChar {
					last.op = opLit
					last.set = charSet{last.r, term.r}
				} else {
					last.set = append(last.set, term.r)
				}
				p.spare = term
				continue
			}
		}
		terms = append(terms, term)
	}
	if p.validateOnly {
		return nil
	}
	switch len(terms) {
	case 0:
		return p.newNode(opEmpty)
	case 1:
		return terms[0]
	}
	n := p.newNode(opSeq)
	n.subs = terms
	return n
}

func (p *parser) parseTerm() *node {
	c := p.peek()
	var atom *node
	quantifiable := true
	groups := p.nextGroup
	switch c {
	case '^':
		p.pos++
		if p.validateOnly {
			return nil
		}
		return p.newNode(opLineStart)
	case '$':
		p.pos++
		if p.validateOnly {
			return nil
		}
		return p.newNode(opLineEnd)
	case '\\':
		if p.peekAt(1) == 'b' || p.peekAt(1) == 'B' {
			p.pos += 2
			if p.validateOnly {
				return nil
			}
			n := p.newNode(opWordB)
			n.neg = p.src[p.pos-1] == 'B'
			return n
		}
		atom = p.parseAtomEscape()
	case '(':
		atom, quantifiable = p.parseGroup()
	case '.':
		p.pos++
		atom = p.dotNode()
	case '[':
		atom = p.parseClass()
	case '*', '+', '?':
		p.fail("nothing to repeat")
	case '{':
		if p.unicode {
			p.fail("nothing to repeat")
		}
		if _, _, _, _, ok := p.braceQuantifier(p.pos); ok {
			p.fail("nothing to repeat")
		}
		p.pos++
		atom = p.charNode(c)
	case '}', ']':
		if p.unicode {
			p.fail("lone quantifier brackets")
		}
		p.pos++
		atom = p.charNode(c)
	default:
		p.pos++
		atom = p.charNode(c)
	}
	return p.parseQuantifier(atom, quantifiable, groups)
}

func (p *parser) top() rune {
	if p.unicode {
		return maxRune
	}
	return 0xFFFF
}

// dotNode returns the node of '.', or nil when the parser only validates.
func (p *parser) dotNode() *node {
	if p.validateOnly {
		return nil
	}
	if p.flags&FlagDotAll != 0 {
		return p.classNode(charSet{0, p.top()})
	}
	return p.classNode(lineTerminators.complement(p.top()))
}

// charNode returns the node of c, or nil when the parser only validates.
func (p *parser) charNode(c rune) *node {
	if p.validateOnly {
		return nil
	}
	n := p.spare
	if n == nil {
		n = p.newNode(opChar)
	} else {
		p.spare = nil
		*n = node{op: opChar, flags: p.flags}
	}
	n.r = c
	return n
}

// braceQuantifier parses {n}, {n,} or {n,m} at i and returns the bounds and the position after it.
// ordered reports whether the upper bound, if any, is at least the lower bound before clamping.
func (p *parser) braceQuantifier(i int) (lo, hi, end int, ordered, ok bool) {
	if i >= len(p.src) || p.src[i] != '{' {
		return 0, 0, 0, false, false
	}
	i++
	loStart := i
	lo, i, ok = p.decimal(i)
	if !ok {
		return 0, 0, 0, false, false
	}
	loDigits := p.src[loStart:i]
	hi = lo
	ordered = true
	if i < len(p.src) && p.src[i] == ',' {
		i++
		hi = unbounded
		if i < len(p.src) && p.src[i] >= '0' && p.src[i] <= '9' {
			hiStart := i
			hi, i, _ = p.decimal(i)
			ordered = compareDigits(p.src[hiStart:i], loDigits) >= 0
		}
	}
	if i >= len(p.src) || p.src[i] != '}' {
		return 0, 0, 0, false, false
	}
	return lo, hi, i + 1, ordered, true
}

// compareDigits compares the numbers that the decimal digits a and b denote, and returns -1, 0
// or +1.
func compareDigits(a, b []rune) int {
	trim := func(d []rune) []rune {
		for len(d) > 1 && d[0] == '0' {
			d = d[1:]
		}
		return d
	}
	a, b = trim(a), trim(b)
	if len(a) != len(b) {
		return cmp.Compare(len(a), len(b))
	}
	return slices.Compare(a, b)
}

// decimal parses decimal digits at i, clamping the value to countLimit.
func (p *parser) decimal(i int) (int, int, bool) {
	start := i
	v := 0
	for i < len(p.src) && p.src[i] >= '0' && p.src[i] <= '9' {
		v = int(min(int64(v)*10+int64(p.src[i]-'0'), countLimit))
		i++
	}
	return v, i, i > start
}

// parseQuantifier parses the quantifier of atom, if any. groups is the number of the capturing
// groups before atom.
func (p *parser) parseQuantifier(atom *node, quantifiable bool, groups int) *node {
	lo, hi := 0, 0
	switch p.peek() {
	case '*':
		lo, hi = 0, unbounded
		p.pos++
	case '+':
		lo, hi = 1, unbounded
		p.pos++
	case '?':
		lo, hi = 0, 1
		p.pos++
	case '{':
		l, h, end, ordered, ok := p.braceQuantifier(p.pos)
		if !ok {
			if p.unicode {
				p.fail("incomplete quantifier")
			}
			return atom
		}
		if !ordered {
			p.fail("numbers out of order in {} quantifier")
		}
		lo, hi = l, h
		p.pos = end
	default:
		return atom
	}
	if !quantifiable {
		p.fail("nothing to repeat")
	}
	greedy := true
	if p.peek() == '?' {
		p.pos++
		greedy = false
	}
	if p.validateOnly {
		return nil
	}
	n := p.newNode(opRepeat)
	n.min, n.max = lo, hi
	n.greedy = greedy
	n.subs = []*node{atom}
	// The groups of atom are numbered from groups+1 up to p.nextGroup.
	if p.nextGroup > groups {
		n.cap, n.capEnd = groups+1, p.nextGroup+1
	}
	return n
}

func (p *parser) parseGroup() (*node, bool) {
	p.pos++ // (
	if p.peek() != '?' {
		p.nextGroup++
		idx := p.nextGroup
		sub := p.parseDisjunction()
		p.expect(')')
		return p.captureNode(idx, "", sub), true
	}
	p.pos++ // ?
	switch c := p.peek(); c {
	case ':':
		p.pos++
		sub := p.parseDisjunction()
		p.expect(')')
		return p.groupNode(sub), true
	case '=', '!':
		p.pos++
		sub := p.parseDisjunction()
		p.expect(')')
		// Annex B allows quantifying a lookahead without the u and v flags.
		return p.lookNode(sub, c == '!', false), !p.unicode
	case '<':
		if p.peekAt(1) == '=' || p.peekAt(1) == '!' {
			neg := p.peekAt(1) == '!'
			p.pos += 2
			sub := p.parseDisjunction()
			p.expect(')')
			return p.lookNode(sub, neg, true), false
		}
		p.pos++
		name := p.parseGroupName()
		p.nextGroup++
		idx := p.nextGroup
		p.nameGroup(idx, name)
		sub := p.parseDisjunction()
		p.expect(')')
		return p.captureNode(idx, name, sub), true
	default:
		return p.parseModifiers(), true
	}
}

// captureNode returns the node of the capturing group idx over sub, or nil when the parser only
// validates.
func (p *parser) captureNode(idx int, name wtf8.String, sub *node) *node {
	if p.validateOnly {
		return nil
	}
	n := p.newNode(opCapture)
	n.cap = idx
	n.name = name
	n.subs = []*node{sub}
	return n
}

// groupNode returns the node of a non-capturing group over sub, or nil when the parser only
// validates.
func (p *parser) groupNode(sub *node) *node {
	if p.validateOnly {
		return nil
	}
	n := p.newNode(opGroup)
	n.subs = []*node{sub}
	return n
}

// lookNode returns the node of a lookaround over sub, or nil when the parser only validates.
func (p *parser) lookNode(sub *node, neg, behind bool) *node {
	if p.validateOnly {
		return nil
	}
	n := p.newNode(opLook)
	n.neg = neg
	n.behind = behind
	n.subs = []*node{sub}
	return n
}

// parseModifiers parses the modifiers of (?ims-ims: ...) after the question mark.
func (p *parser) parseModifiers() *node {
	var add, remove Flags
	seen := Flags(0)
	letter := func(c rune) Flags {
		switch c {
		case 'i':
			return FlagIgnoreCase
		case 'm':
			return FlagMultiline
		case 's':
			return FlagDotAll
		}
		return 0
	}
	for {
		f := letter(p.peek())
		if f == 0 {
			break
		}
		if seen&f != 0 {
			p.fail("repeated flag in modifiers")
		}
		seen |= f
		add |= f
		p.pos++
	}
	dash := false
	if p.peek() == '-' {
		dash = true
		p.pos++
		for {
			f := letter(p.peek())
			if f == 0 {
				break
			}
			if seen&f != 0 {
				p.fail("repeated flag in modifiers")
			}
			seen |= f
			remove |= f
			p.pos++
		}
	}
	if p.peek() != ':' {
		p.fail("invalid group")
	}
	if dash && add == 0 && remove == 0 {
		p.fail("invalid group")
	}
	if !dash && add == 0 {
		p.fail("invalid group")
	}
	p.pos++
	saved := p.flags
	p.flags = p.flags&^remove | add
	// The group has the flags of its contents.
	n := p.groupNode(p.parseDisjunction())
	p.flags = saved
	p.expect(')')
	return n
}

func (p *parser) expect(c rune) {
	if p.peek() != c {
		if c == ')' {
			p.fail("unterminated group")
		}
		p.fail("expected " + string(c))
	}
	p.pos++
}

// parseGroupName parses a RegExpIdentifierName followed by '>'.
func (p *parser) parseGroupName() wtf8.String {
	var b wtf8.Builder
	first := true
	for {
		c := p.peek()
		if c == '>' && !first {
			p.pos++
			return b.String()
		}
		if c < 0 {
			p.fail("invalid capture group name")
		}
		p.pos++
		if c == '\\' {
			if p.peek() != 'u' {
				p.fail("invalid capture group name")
			}
			p.pos++
			r, ok := p.unicodeEscape(true)
			if !ok {
				p.fail("invalid Unicode escape in group name")
			}
			c = r
		} else if !p.unicode && isHigh(c) && isLow(p.peek()) {
			c = combine(c, p.peek())
			p.pos++
		}
		if first && !isIDStart(c) || !first && !isIDContinue(c) {
			p.fail("invalid capture group name")
		}
		b.WriteRune(c)
		first = false
	}
}

func isHigh(c rune) bool { return c >= 0xD800 && c <= 0xDBFF }
func isLow(c rune) bool  { return c >= 0xDC00 && c <= 0xDFFF }

func combine(hi, lo rune) rune {
	return 0x10000 + (hi-0xD800)<<10 + (lo - 0xDC00)
}

func isIDStart(c rune) bool {
	return c == '$' || c == '_' || idStart.has(c)
}

func isIDContinue(c rune) bool {
	return c == '$' || c == 0x200C || c == 0x200D || idContinue.has(c)
}

func hexVal(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// hex parses n hexadecimal digits at the current position.
func (p *parser) hex(n int) (rune, bool) {
	v := rune(0)
	for i := range n {
		d := hexVal(p.peekAt(i))
		if d < 0 {
			return 0, false
		}
		v = v<<4 | rune(d)
	}
	p.pos += n
	return v, true
}

// unicodeEscape parses the rest of an escape after \u. With unicodeMode, it accepts \u{...}
// and joins an escaped surrogate pair. It consumes nothing when it fails.
func (p *parser) unicodeEscape(unicodeMode bool) (rune, bool) {
	start := p.pos
	if unicodeMode && p.peek() == '{' {
		p.pos++
		v := rune(0)
		digits := 0
		for {
			d := hexVal(p.peek())
			if d < 0 {
				break
			}
			v = v<<4 | rune(d)
			if v > maxRune {
				p.pos = start
				return 0, false
			}
			digits++
			p.pos++
		}
		if digits == 0 || p.peek() != '}' {
			p.pos = start
			return 0, false
		}
		p.pos++
		return v, true
	}
	v, ok := p.hex(4)
	if !ok {
		return 0, false
	}
	if unicodeMode && isHigh(v) && p.peek() == '\\' && p.peekAt(1) == 'u' {
		save := p.pos
		p.pos += 2
		if lo, ok := p.hex(4); ok && isLow(lo) {
			return combine(v, lo), true
		}
		p.pos = save
	}
	return v, true
}

func isSyntaxChar(c rune) bool {
	switch c {
	case '^', '$', '\\', '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|':
		return true
	}
	return false
}

func isASCIILetter(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isOctal(c rune) bool {
	return c >= '0' && c <= '7'
}

// legacyOctal parses a LegacyOctalEscapeSequence at the current position.
func (p *parser) legacyOctal() rune {
	c := p.peek()
	v := c - '0'
	p.pos++
	if !isOctal(p.peek()) {
		return v
	}
	v = v*8 + p.peek() - '0'
	p.pos++
	if c <= '3' && isOctal(p.peek()) {
		v = v*8 + p.peek() - '0'
		p.pos++
	}
	return v
}

// parseAtomEscape parses an escape outside a class, at the backslash.
func (p *parser) parseAtomEscape() *node {
	p.pos++ // backslash
	if p.eof() {
		p.fail("\\ at end of pattern")
	}
	c := p.peek()
	switch {
	case c >= '1' && c <= '9':
		start := p.pos
		v, end, _ := p.decimal(p.pos)
		if v <= p.groupCount {
			p.pos = end
			if p.validateOnly {
				return nil
			}
			n := p.newNode(opBackref)
			n.refs = []int{v}
			return n
		}
		if p.unicode {
			p.fail("invalid escape")
		}
		p.pos = start
		if c >= '8' {
			p.pos++
			return p.charNode(c)
		}
		return p.charNode(p.legacyOctal())
	case c == 'k' && p.named:
		p.pos++
		if p.peek() != '<' {
			p.fail("invalid named reference")
		}
		p.pos++
		name := p.parseGroupName()
		var n *node
		if !p.validateOnly {
			n = p.newNode(opBackref)
			n.name = name
		}
		p.namedRefs = append(p.namedRefs, namedRef{
			name: name,
			node: n,
		})
		return n
	}
	if set, ok := p.classEscape(); ok {
		if p.validateOnly {
			return nil
		}
		n := p.newNode(opClass)
		n.set = set
		if p.flags&FlagIgnoreCase != 0 {
			n.set = foldClosure(n.set, p.unicode)
		}
		return n
	}
	if p.unicodeSets && c == 'p' {
		// A property of strings.
		if strs, ok := p.stringProperty(); ok {
			if p.validateOnly {
				return nil
			}
			return p.classNodeFromStrings(strs)
		}
	}
	if c == 'c' && !p.unicode && !isASCIILetter(p.peekAt(1)) {
		// Annex B: the backslash is a literal character, and so is the c after it.
		return p.charNode('\\')
	}
	return p.charNode(p.characterEscape(false))
}

// classEscape parses \d \D \s \S \w \W \p{...} \P{...} after the backslash.
// It consumes nothing and reports false for any other escape and for a property of strings.
// The set is nil when the parser only validates.
func (p *parser) classEscape() (charSet, bool) {
	c := p.peek()
	var s charSet
	switch c {
	case 'd', 'D':
		s = digitSet
	case 's', 'S':
		s = spaceSet
	case 'w', 'W':
		s = wordSet
	case 'p', 'P':
		if !p.unicode {
			return nil, false
		}
		save := p.pos
		p.pos++
		name, value := p.propertyExpression()
		set, ok := lookupProperty(name, value)
		if !ok {
			if c == 'p' && p.unicodeSets && value == "" && isStringProperty(name) {
				p.pos = save
				return nil, false
			}
			if c == 'P' && p.unicodeSets && value == "" && isStringProperty(name) {
				p.fail("negated property of strings")
			}
			p.fail("invalid property name")
		}
		if p.validateOnly {
			return nil, true
		}
		if c == 'P' {
			if p.unicodeSets && p.flags&FlagIgnoreCase != 0 {
				set = foldClosure(set, true)
			}
			set = set.complement(maxRune)
		}
		return set, true
	default:
		return nil, false
	}
	p.pos++
	if p.validateOnly {
		return nil, true
	}
	if (c == 'w' || c == 'W') && p.unicode && p.flags&FlagIgnoreCase != 0 {
		s = s.union(charSet{0x17F, 0x17F, 0x212A, 0x212A})
	}
	if c >= 'A' && c <= 'Z' {
		if p.flags&FlagIgnoreCase != 0 {
			s = foldClosure(s, p.unicode)
		}
		s = s.complement(p.top())
	}
	return s, true
}

// propertyExpression parses {Name} or {Name=Value} after \p.
func (p *parser) propertyExpression() (string, string) {
	if p.peek() != '{' {
		p.fail("invalid property name")
	}
	p.pos++
	word := func() string {
		start := p.pos
		for c := p.peek(); isASCIILetter(c) || c >= '0' && c <= '9' || c == '_'; c = p.peek() {
			p.pos++
		}
		return string(p.src[start:p.pos])
	}
	name := word()
	value := ""
	if p.peek() == '=' {
		p.pos++
		value = word()
		if value == "" {
			p.fail("invalid property name")
		}
	}
	if name == "" || p.peek() != '}' {
		p.fail("invalid property name")
	}
	p.pos++
	return name, value
}

// stringProperty parses \p{Name} at the p for a property of strings and reports whether there is
// one. It consumes nothing when there is none. The strings are nil when the parser only
// validates.
func (p *parser) stringProperty() ([][]rune, bool) {
	save := p.pos
	p.pos++
	name, value := p.propertyExpression()
	if value == "" && isStringProperty(name) {
		if p.validateOnly {
			return nil, true
		}
		return lookupStringProperty(name)
	}
	p.pos = save
	return nil, false
}

// characterEscape parses a CharacterEscape after the backslash and returns its value.
// inClass selects the class rules for \-.
func (p *parser) characterEscape(inClass bool) rune {
	c := p.peek()
	p.pos++
	switch c {
	case 'f':
		return '\f'
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	case 'v':
		return '\v'
	case 'c':
		if l := p.peek(); isASCIILetter(l) {
			p.pos++
			return l % 32
		}
		if p.unicode {
			p.fail("invalid unicode escape")
		}
		// Callers handle \c without a letter before getting here.
		p.pos--
		return '\\'
	case '0':
		if d := p.peek(); d >= '0' && d <= '9' {
			if p.unicode {
				p.fail("invalid decimal escape")
			}
			p.pos--
			return p.legacyOctal()
		}
		return 0
	case 'x':
		if v, ok := p.hex(2); ok {
			return v
		}
		if p.unicode {
			p.fail("invalid escape")
		}
		return 'x'
	case 'u':
		if v, ok := p.unicodeEscape(p.unicode); ok {
			return v
		}
		if p.unicode {
			p.fail("invalid Unicode escape")
		}
		return 'u'
	}
	if p.unicode {
		if isSyntaxChar(c) || c == '/' || inClass && c == '-' {
			return c
		}
		p.fail("invalid escape")
	}
	if c >= '1' && c <= '9' {
		// In a class, or a decimal escape that is not a backreference.
		if c >= '8' {
			return c
		}
		p.pos--
		return p.legacyOctal()
	}
	if c == 'k' && p.named {
		p.fail("invalid escape")
	}
	return c
}

func (p *parser) classNode(set charSet) *node {
	n := p.newNode(opClass)
	n.set = set
	return n
}

// parseClass parses a character class at the '['.
func (p *parser) parseClass() *node {
	p.pos++
	neg := false
	if p.peek() == '^' {
		neg = true
		p.pos++
	}
	if p.unicodeSets {
		v := p.parseClassSetExpression()
		if neg && v.mayContainStrings {
			p.fail("negated character class may contain strings")
		}
		if p.validateOnly {
			return nil
		}
		if neg {
			v.set = v.set.complement(maxRune)
			v.strs = nil
		}
		return p.classValueNode(v)
	}
	var set charSet
	for {
		if p.eof() {
			p.fail("unterminated character class")
		}
		if p.peek() == ']' {
			p.pos++
			break
		}
		a := p.classAtom()
		if p.peek() == '-' && p.peekAt(1) != ']' && p.peekAt(1) >= 0 {
			p.pos++
			b := p.classAtom()
			if a.isClass || b.isClass {
				if p.unicode {
					p.fail("invalid character class")
				}
				set = p.addClassAtom(set, a)
				set = p.addClassAtom(set, classAtomValue{c: '-'})
				set = p.addClassAtom(set, b)
				continue
			}
			if a.c > b.c {
				p.fail("range out of order in character class")
			}
			set = p.addClassRange(set, a.c, b.c)
			continue
		}
		set = p.addClassAtom(set, a)
	}
	if p.validateOnly {
		return nil
	}
	set = set.normalize()
	if p.flags&FlagIgnoreCase != 0 {
		set = foldClosure(set, p.unicode)
	}
	if neg {
		set = set.complement(p.top())
	}
	return p.classNode(set)
}

// classAtomValue is the value of a ClassAtom of a class without the v flag.
type classAtomValue struct {
	// isClass reports whether the atom is a class escape such as \d.
	isClass bool

	// set is the set of a class escape. It is nil when the parser only validates.
	set charSet

	// c is the character of any other atom.
	c rune
}

// addClassAtom returns set with the characters of a added, or set itself when the parser only
// validates.
func (p *parser) addClassAtom(set charSet, a classAtomValue) charSet {
	if p.validateOnly {
		return set
	}
	if a.isClass {
		return append(set, a.set...)
	}
	return append(set, a.c, a.c)
}

// addClassRange returns set with the characters from lo to hi added, or set itself when the
// parser only validates.
func (p *parser) addClassRange(set charSet, lo, hi rune) charSet {
	if p.validateOnly {
		return set
	}
	return append(set, lo, hi)
}

// classAtom parses a ClassAtom of a class without the v flag.
func (p *parser) classAtom() classAtomValue {
	c := p.peek()
	if c < 0 {
		p.fail("unterminated character class")
	}
	p.pos++
	if c != '\\' {
		return classAtomValue{c: c}
	}
	if p.eof() {
		p.fail("\\ at end of pattern")
	}
	switch e := p.peek(); {
	case e == 'b':
		p.pos++
		return classAtomValue{c: 8}
	case e == '-' && p.unicode:
		p.pos++
		return classAtomValue{c: '-'}
	case e == 'c' && !p.unicode:
		if d := p.peekAt(1); d >= '0' && d <= '9' || d == '_' {
			p.pos += 2
			return classAtomValue{c: d % 32}
		}
		if !isASCIILetter(p.peekAt(1)) {
			return classAtomValue{c: '\\'}
		}
	case e == 'B' && p.unicode:
		p.fail("invalid class escape")
	}
	if s, ok := p.classEscape(); ok {
		return classAtomValue{
			isClass: true,
			set:     s,
		}
	}
	return classAtomValue{c: p.characterEscape(true)}
}

// classValue is the value of a class with the v flag: characters and strings of other lengths.
// The set and the strings are nil when the parser only validates.
type classValue struct {
	set               charSet
	strs              map[string][]rune
	mayContainStrings bool
}

func strKey(s []rune) string {
	b := make([]byte, 0, 3*len(s))
	for _, c := range s {
		b = append(b, byte(c>>16), byte(c>>8), byte(c))
	}
	return string(b)
}

func (v *classValue) addString(s []rune) {
	if len(s) == 1 {
		v.set = append(v.set, s[0], s[0])
		return
	}
	if v.strs == nil {
		v.strs = map[string][]rune{}
	}
	v.strs[strKey(s)] = s
}

func (p *parser) fold() bool {
	return p.flags&FlagIgnoreCase != 0
}

func (p *parser) parseClassSetExpression() classValue {
	p.enter()
	defer p.leave()
	// Read the first operand, then the operator that decides the kind of expression.
	first, isRange := p.classSetOperandOrRange()
	switch {
	case p.lookingAt("&&"):
		if isRange {
			p.fail("invalid set operation in character class")
		}
		acc := first
		for p.lookingAt("&&") {
			p.pos += 2
			if p.peek() == '&' {
				p.fail("invalid set operation in character class")
			}
			b := p.classSetOperand()
			acc = p.intersectValues(acc, b)
		}
		p.expectClassEnd()
		return acc
	case p.lookingAt("--"):
		if isRange {
			p.fail("invalid set operation in character class")
		}
		acc := first
		for p.lookingAt("--") {
			p.pos += 2
			b := p.classSetOperand()
			acc = p.subtractValues(acc, b)
		}
		p.expectClassEnd()
		return acc
	}
	// The union accumulates into its own copy of the first operand, which may be shared.
	acc := first
	if !p.validateOnly {
		acc.set = slices.Clone(acc.set)
		acc.strs = maps.Clone(acc.strs)
	}
	for p.peek() != ']' {
		if p.eof() {
			p.fail("unterminated character class")
		}
		if p.lookingAt("&&") || p.lookingAt("--") {
			p.fail("invalid set operation in character class")
		}
		b, _ := p.classSetOperandOrRange()
		acc.mayContainStrings = acc.mayContainStrings || b.mayContainStrings
		if !p.validateOnly {
			acc.set = append(acc.set, b.set...)
			for _, s := range b.strs {
				acc.addString(s)
			}
		}
	}
	p.pos++
	acc.set = acc.set.normalize()
	return acc
}

func (p *parser) expectClassEnd() {
	if p.peek() != ']' {
		p.fail("invalid set operation in character class")
	}
	p.pos++
}

func (p *parser) intersectValues(a, b classValue) classValue {
	out := classValue{
		mayContainStrings: a.mayContainStrings && b.mayContainStrings,
	}
	if p.validateOnly {
		return out
	}
	out.set = a.set.normalize().intersect(b.set.normalize())
	for k, s := range a.strs {
		if _, ok := b.strs[k]; ok {
			out.addString(s)
		}
	}
	return out
}

func (p *parser) subtractValues(a, b classValue) classValue {
	out := classValue{
		mayContainStrings: a.mayContainStrings,
	}
	if p.validateOnly {
		return out
	}
	out.set = a.set.normalize().subtract(b.set.normalize())
	for k, s := range a.strs {
		if _, ok := b.strs[k]; !ok {
			out.addString(s)
		}
	}
	return out
}

// classSetOperandOrRange parses a ClassSetRange or a ClassSetOperand.
func (p *parser) classSetOperandOrRange() (classValue, bool) {
	if p.eof() {
		p.fail("unterminated character class")
	}
	if c := p.peek(); c == ']' {
		// The empty class.
		return classValue{}, false
	}
	if c, ok := p.tryClassSetCharacter(); ok {
		if p.peek() == '-' && p.peekAt(1) != '-' {
			p.pos++
			d, ok := p.tryClassSetCharacter()
			if !ok {
				p.fail("invalid character class range")
			}
			if c > d {
				p.fail("range out of order in character class")
			}
			return p.rangeValue(c, d), true
		}
		return p.rangeValue(c, c), false
	}
	return p.classSetOperand(), false
}

// rangeValue returns the value of the characters from lo to hi.
func (p *parser) rangeValue(lo, hi rune) classValue {
	if p.validateOnly {
		return classValue{}
	}
	return p.foldValue(classValue{set: charSet{lo, hi}})
}

// foldValue closes v over case equivalence when the i flag applies.
func (p *parser) foldValue(v classValue) classValue {
	if !p.fold() || p.validateOnly {
		return v
	}
	v.set = foldClosure(v.set.normalize(), true)
	if len(v.strs) > 0 {
		strs := v.strs
		v.strs = nil
		for _, s := range strs {
			f := make([]rune, len(s))
			for i, c := range s {
				f[i] = canonUnicode(c)
			}
			v.addString(f)
		}
	}
	return v
}

// classSetOperand parses a NestedClass, a ClassStringDisjunction or a ClassSetCharacter.
func (p *parser) classSetOperand() classValue {
	if p.eof() {
		p.fail("unterminated character class")
	}
	switch p.peek() {
	case '[':
		p.pos++
		neg := false
		if p.peek() == '^' {
			neg = true
			p.pos++
		}
		v := p.parseClassSetExpression()
		if neg {
			if v.mayContainStrings {
				p.fail("negated character class may contain strings")
			}
			if p.validateOnly {
				return classValue{}
			}
			return classValue{set: v.set.normalize().complement(maxRune)}
		}
		return v
	case '\\':
		if p.peekAt(1) == 'q' {
			p.pos += 2
			return p.foldValue(p.classStringDisjunction())
		}
		save := p.pos
		p.pos++
		if p.peek() == 'p' {
			if strs, ok := p.stringProperty(); ok {
				v := classValue{mayContainStrings: true}
				if p.validateOnly {
					return v
				}
				for _, s := range strs {
					v.addString(s)
				}
				v.set = v.set.normalize()
				return p.foldValue(v)
			}
		}
		if s, ok := p.classEscape(); ok {
			if p.fold() && !p.validateOnly {
				s = foldClosure(s, true)
			}
			return classValue{set: s}
		}
		p.pos = save
	}
	c, ok := p.tryClassSetCharacter()
	if !ok {
		p.fail("invalid character in character class")
	}
	return p.rangeValue(c, c)
}

// classStringDisjunction parses the rest of \q{...} after the q.
func (p *parser) classStringDisjunction() classValue {
	if p.peek() != '{' {
		p.fail("invalid escape")
	}
	p.pos++
	var v classValue
	// cur is the current string, which stays nil when the parser only validates, and n is its
	// length.
	var cur []rune
	var n int
	for {
		if p.eof() {
			p.fail("unterminated class string disjunction")
		}
		switch p.peek() {
		case '}':
			p.pos++
			p.addClassString(&v, cur, n)
			v.set = v.set.normalize()
			return v
		case '|':
			p.pos++
			p.addClassString(&v, cur, n)
			cur, n = nil, 0
			continue
		}
		c, ok := p.tryClassSetCharacter()
		if !ok {
			p.fail("invalid character in class string disjunction")
		}
		if !p.validateOnly {
			cur = append(cur, c)
		}
		n++
	}
}

// addClassString adds the string s of length n of a class string disjunction to v.
// s is nil when the parser only validates.
func (p *parser) addClassString(v *classValue, s []rune, n int) {
	if n != 1 {
		v.mayContainStrings = true
	}
	if !p.validateOnly {
		v.addString(s)
	}
}

func isClassSetSyntaxChar(c rune) bool {
	switch c {
	case '(', ')', '[', ']', '{', '}', '/', '-', '\\', '|':
		return true
	}
	return false
}

func isClassSetReservedDouble(c rune) bool {
	switch c {
	case '&', '!', '#', '$', '%', '*', '+', ',', '.', ':', ';', '<', '=', '>', '?', '@', '^', '`', '~':
		return true
	}
	return false
}

func isClassSetReservedPunctuator(c rune) bool {
	switch c {
	case '&', '-', '!', '#', '%', ',', ':', ';', '<', '=', '>', '@', '`', '~':
		return true
	}
	return false
}

// tryClassSetCharacter parses a ClassSetCharacter. It consumes nothing and reports false
// when there is none at the current position, except for errors in escapes.
func (p *parser) tryClassSetCharacter() (rune, bool) {
	c := p.peek()
	if c < 0 {
		return 0, false
	}
	if c == '\\' {
		e := p.peekAt(1)
		switch {
		case e < 0:
			p.fail("\\ at end of pattern")
		case e == 'b':
			p.pos += 2
			return 8, true
		case isClassSetReservedPunctuator(e):
			p.pos += 2
			return e, true
		case e == 'd' || e == 'D' || e == 's' || e == 'S' || e == 'w' || e == 'W' || e == 'p' || e == 'P' || e == 'q':
			return 0, false
		}
		p.pos++
		return p.characterEscape(false), true
	}
	if isClassSetSyntaxChar(c) {
		return 0, false
	}
	if isClassSetReservedDouble(c) && p.peekAt(1) == c {
		p.fail("invalid set operation in character class")
	}
	p.pos++
	return c, true
}

// classValueNode returns the node that matches a class with the v flag.
func (p *parser) classValueNode(v classValue) *node {
	set := v.set.normalize()
	if len(v.strs) == 0 {
		return p.classNode(set)
	}
	var strs [][]rune
	for _, s := range v.strs {
		strs = append(strs, s)
	}
	return p.stringsNode(strs, set)
}

// classNodeFromStrings returns the node for a property of strings outside a class.
func (p *parser) classNodeFromStrings(strs [][]rune) *node {
	v := classValue{}
	for _, s := range strs {
		v.addString(s)
	}
	v = p.foldValue(v)
	return p.classValueNode(v)
}

// stringsNode returns an alternation of the strings, longest first, then of the characters of
// set, then of the empty string when it is one of the strings.
func (p *parser) stringsNode(strs [][]rune, set charSet) *node {
	slices.SortStableFunc(strs, func(a, b []rune) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return slices.Compare(a, b)
	})
	alt := p.newNode(opAlt)
	hasEmpty := false
	for _, s := range strs {
		if len(s) == 0 {
			hasEmpty = true
			continue
		}
		seq := p.newNode(opSeq)
		for _, c := range s {
			seq.subs = append(seq.subs, p.charNode(c))
		}
		alt.subs = append(alt.subs, seq)
	}
	if len(set) > 0 {
		alt.subs = append(alt.subs, p.classNode(set))
	}
	if hasEmpty {
		alt.subs = append(alt.subs, p.newNode(opEmpty))
	}
	n := p.newNode(opGroup)
	n.subs = []*node{alt}
	return n
}
