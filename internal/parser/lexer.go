// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser

import (
	"math/big"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

const (
	charIDStart = 1 << iota
	charIDPart
	charDigit
)

var charClass = func() (t [256]uint8) {
	for c := 'a'; c <= 'z'; c++ {
		t[c] = charIDStart | charIDPart
		t[c-'a'+'A'] = charIDStart | charIDPart
	}
	t['$'] = charIDStart | charIDPart
	t['_'] = charIDStart | charIDPart
	for c := '0'; c <= '9'; c++ {
		t[c] = charIDPart | charDigit
	}
	return
}()

func isIDStartRune(r rune) bool {
	if r < utf8.RuneSelf {
		return charClass[r]&charIDStart != 0
	}
	if r == 0x2e2f {
		return false
	}
	return unicode.IsLetter(r) || unicode.Is(unicode.Nl, r) || unicode.Is(unicode.Other_ID_Start, r)
}

func isIDPartRune(r rune) bool {
	if r < utf8.RuneSelf {
		return charClass[r]&charIDPart != 0
	}
	if r == 0x200c || r == 0x200d {
		return true
	}
	return isIDStartRune(r) || unicode.In(r, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc, unicode.Other_ID_Continue)
}

// IsIdentifierName reports whether s is an IdentifierName without escapes.
func IsIdentifierName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 && !isIDStartRune(r) || i > 0 && !isIDPartRune(r) {
			return false
		}
	}
	return true
}

func isSpaceRune(r rune) bool {
	return r == 0xa0 || r == 0xfeff || unicode.Is(unicode.Zs, r)
}

func hexValue(c byte) int {
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

// appendWTF8 appends the code point or surrogate code unit r to buf in WTF-8,
// combining a low surrogate with a high surrogate that ends buf.
func appendWTF8(buf []byte, r rune) []byte {
	if r < 0xd800 || r > 0xdfff {
		return utf8.AppendRune(buf, r)
	}
	if n := len(buf); r >= 0xdc00 && n >= 3 && buf[n-3] == 0xed && buf[n-2] >= 0xa0 && buf[n-2] <= 0xaf {
		hi := 0xd000 | rune(buf[n-2]&0x3f)<<6 | rune(buf[n-1]&0x3f)
		return utf8.AppendRune(buf[:n-3], 0x10000+(hi-0xd800)<<10+(r-0xdc00))
	}
	return append(buf, 0xed, 0x80|byte(r>>6)&0x3f, 0x80|byte(r)&0x3f)
}

// hasLoneSurrogate reports whether the WTF-8 string s contains a surrogate.
func hasLoneSurrogate(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == 0xed && s[i+1] >= 0xa0 {
			return true
		}
	}
	return false
}

// next advances to the next token.
func (p *parser) next() {
	p.prevEnd = p.t.end
	p.scan()
}

// skipLine returns the offset of the first line terminator at or after pos.
func (p *parser) skipLine(pos int) int {
	src := p.src
	for pos < len(src) {
		c := src[pos]
		if c == '\n' || c == '\r' {
			break
		}
		if c == 0xe2 && pos+2 < len(src) && src[pos+1] == 0x80 && (src[pos+2] == 0xa8 || src[pos+2] == 0xa9) {
			break
		}
		pos++
	}
	return pos
}

func (p *parser) scan() {
	src := p.src
	pos := p.pos
	nl := p.t.end == 0
	t := &p.t

skip:
	for pos < len(src) {
		c := src[pos]
		switch c {
		case ' ', '\t', '\v', '\f':
			pos++
		case '\n', '\r':
			nl = true
			pos++
		case '/':
			if pos+1 >= len(src) {
				break skip
			}
			switch src[pos+1] {
			case '/':
				pos = p.skipLine(pos + 2)
			case '*':
				end := strings.Index(src[pos+2:], "*/")
				if end < 0 {
					p.fail(pos, "unterminated comment")
				}
				body := src[pos+2 : pos+2+end]
				if !nl && (strings.ContainsAny(body, "\n\r") || strings.Contains(body, "\u2028") || strings.Contains(body, "\u2029")) {
					nl = true
				}
				pos += end + 4
			default:
				break skip
			}
		case '<':
			if p.module || !strings.HasPrefix(src[pos:], "<!--") {
				break skip
			}
			pos = p.skipLine(pos + 4)
		case '-':
			if p.module || !nl || !strings.HasPrefix(src[pos:], "-->") {
				break skip
			}
			pos = p.skipLine(pos + 3)
		default:
			if c < utf8.RuneSelf {
				break skip
			}
			r, size := utf8.DecodeRuneInString(src[pos:])
			switch {
			case r == 0x2028 || r == 0x2029:
				nl = true
			case isSpaceRune(r):
			default:
				break skip
			}
			pos += size
		}
	}

	t.nl = nl
	t.esc = false
	t.octal = false
	t.start = int32(pos)
	if pos >= len(src) {
		t.kind = tEOF
		t.end = int32(pos)
		p.pos = pos
		return
	}

	c := src[pos]
	var c1, c2 byte
	if pos+1 < len(src) {
		c1 = src[pos+1]
	}
	if pos+2 < len(src) {
		c2 = src[pos+2]
	}
	kind := tEOF
	n := 1
	switch c {
	case '{':
		kind = tLBrace
	case '}':
		kind = tRBrace
	case '(':
		kind = tLParen
	case ')':
		kind = tRParen
	case '[':
		kind = tLBrack
	case ']':
		kind = tRBrack
	case ';':
		kind = tSemi
	case ',':
		kind = tComma
	case ':':
		kind = tColon
	case '~':
		kind = tTilde
	case '@':
		kind = tAt
	case '.':
		switch {
		case c1 >= '0' && c1 <= '9':
			p.scanNumber(pos)
			return
		case c1 == '.' && c2 == '.':
			kind, n = tEllipsis, 3
		default:
			kind = tDot
		}
	case '?':
		switch {
		case c1 == '?' && c2 == '=':
			kind, n = tCoalesceAssign, 3
		case c1 == '?':
			kind, n = tCoalesce, 2
		case c1 == '.' && !(c2 >= '0' && c2 <= '9'):
			kind, n = tQuestionDot, 2
		default:
			kind = tQuestion
		}
	case '=':
		switch {
		case c1 == '=' && c2 == '=':
			kind, n = tStrictEq, 3
		case c1 == '=':
			kind, n = tEq, 2
		case c1 == '>':
			kind, n = tArrow, 2
		default:
			kind = tAssign
		}
	case '!':
		switch {
		case c1 == '=' && c2 == '=':
			kind, n = tStrictNe, 3
		case c1 == '=':
			kind, n = tNe, 2
		default:
			kind = tNot
		}
	case '+':
		switch c1 {
		case '+':
			kind, n = tInc, 2
		case '=':
			kind, n = tAddAssign, 2
		default:
			kind = tAdd
		}
	case '-':
		switch c1 {
		case '-':
			kind, n = tDec, 2
		case '=':
			kind, n = tSubAssign, 2
		default:
			kind = tSub
		}
	case '*':
		switch {
		case c1 == '*' && c2 == '=':
			kind, n = tExpAssign, 3
		case c1 == '*':
			kind, n = tExp, 2
		case c1 == '=':
			kind, n = tMulAssign, 2
		default:
			kind = tMul
		}
	case '/':
		if c1 == '=' {
			kind, n = tDivAssign, 2
		} else {
			kind = tDiv
		}
	case '%':
		if c1 == '=' {
			kind, n = tModAssign, 2
		} else {
			kind = tMod
		}
	case '<':
		switch {
		case c1 == '<' && c2 == '=':
			kind, n = tShlAssign, 3
		case c1 == '<':
			kind, n = tShl, 2
		case c1 == '=':
			kind, n = tLe, 2
		default:
			kind = tLt
		}
	case '>':
		switch {
		case c1 == '>' && c2 == '>':
			if pos+3 < len(src) && src[pos+3] == '=' {
				kind, n = tUShrAssign, 4
			} else {
				kind, n = tUShr, 3
			}
		case c1 == '>' && c2 == '=':
			kind, n = tShrAssign, 3
		case c1 == '>':
			kind, n = tShr, 2
		case c1 == '=':
			kind, n = tGe, 2
		default:
			kind = tGt
		}
	case '&':
		switch {
		case c1 == '&' && c2 == '=':
			kind, n = tAndAssign, 3
		case c1 == '&':
			kind, n = tAnd, 2
		case c1 == '=':
			kind, n = tBitAndAssign, 2
		default:
			kind = tBitAnd
		}
	case '|':
		switch {
		case c1 == '|' && c2 == '=':
			kind, n = tOrAssign, 3
		case c1 == '|':
			kind, n = tOr, 2
		case c1 == '=':
			kind, n = tBitOrAssign, 2
		default:
			kind = tBitOr
		}
	case '^':
		if c1 == '=' {
			kind, n = tBitXorAssign, 2
		} else {
			kind = tBitXor
		}
	case '"', '\'':
		p.scanString(pos, c)
		return
	case '`':
		p.pos = pos + 1
		p.scanTemplate(pos)
		return
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		p.scanNumber(pos)
		return
	case '#':
		if pos+1 >= len(src) {
			p.fail(pos, "unexpected character '#'")
		}
		if r, _ := utf8.DecodeRuneInString(src[pos+1:]); !isIDStartRune(r) && r != '\\' {
			p.fail(pos, "unexpected character '#'")
		}
		name, _ := p.scanIdentName(pos + 1)
		t.kind = tPrivateName
		t.str = name
		t.end = int32(p.pos)
		return
	default:
		if c >= utf8.RuneSelf {
			if r, _ := utf8.DecodeRuneInString(src[pos:]); !isIDStartRune(r) {
				p.fail(pos, "unexpected character "+strconv.QuoteRune(r))
			}
		} else if charClass[c]&charIDStart == 0 && c != '\\' {
			p.fail(pos, "unexpected character "+strconv.QuoteRune(rune(c)))
		}
		name, esc := p.scanIdentName(pos)
		t.str = name
		t.esc = esc
		t.end = int32(p.pos)
		t.kind = tIdent
		if len(name) >= 2 && len(name) <= 10 {
			if k := keywordKind(name); k != tIdent {
				if esc {
					k = tEscKeyword
				}
				t.kind = k
			}
		}
		return
	}
	t.kind = kind
	p.pos = pos + n
	t.end = int32(p.pos)
}

// scanIdentName scans an IdentifierName starting at start, whose first character is
// known to be an identifier start or a backslash, and sets p.pos to its end.
func (p *parser) scanIdentName(start int) (name string, esc bool) {
	src := p.src
	pos := start
	for pos < len(src) && charClass[src[pos]]&charIDPart != 0 {
		pos++
	}
	if pos >= len(src) || (src[pos] != '\\' && src[pos] < utf8.RuneSelf) {
		p.pos = pos
		return src[start:pos], false
	}

	var buf []byte
	for pos < len(src) {
		c := src[pos]
		if c < utf8.RuneSelf {
			if charClass[c]&charIDPart != 0 {
				if esc {
					buf = append(buf, c)
				}
				pos++
				continue
			}
			if c != '\\' {
				break
			}
			if !esc {
				esc = true
				buf = append(make([]byte, 0, pos-start+16), src[start:pos]...)
			}
			escStart := pos
			if pos+1 >= len(src) || src[pos+1] != 'u' {
				p.fail(pos, "invalid Unicode escape in identifier")
			}
			p.pos = pos + 2
			r, ok := p.scanUnicodeEscape()
			if !ok {
				p.fail(escStart, "invalid Unicode escape in identifier")
			}
			if len(buf) == 0 && !isIDStartRune(r) || len(buf) > 0 && !isIDPartRune(r) {
				p.fail(escStart, "invalid Unicode escape in identifier")
			}
			buf = utf8.AppendRune(buf, r)
			pos = p.pos
			continue
		}
		r, size := utf8.DecodeRuneInString(src[pos:])
		if pos == start {
			if !isIDStartRune(r) {
				break
			}
		} else if !isIDPartRune(r) {
			break
		}
		if esc {
			buf = append(buf, src[pos:pos+size]...)
		}
		pos += size
	}
	p.pos = pos
	if esc {
		return string(buf), true
	}
	return src[start:pos], false
}

// scanUnicodeEscape scans the part of a Unicode escape after "\u" at p.pos.
// On failure p.pos is unchanged.
func (p *parser) scanUnicodeEscape() (rune, bool) {
	src := p.src
	pos := p.pos
	if pos < len(src) && src[pos] == '{' {
		pos++
		var r rune
		digits := 0
		for pos < len(src) {
			v := hexValue(src[pos])
			if v < 0 {
				break
			}
			r = r<<4 | rune(v)
			if r > 0x10ffff {
				return 0, false
			}
			digits++
			pos++
		}
		if digits == 0 || pos >= len(src) || src[pos] != '}' {
			return 0, false
		}
		p.pos = pos + 1
		return r, true
	}
	if pos+4 > len(src) {
		return 0, false
	}
	var r rune
	for i := range 4 {
		v := hexValue(src[pos+i])
		if v < 0 {
			return 0, false
		}
		r = r<<4 | rune(v)
	}
	p.pos = pos + 4
	return r, true
}

// scanDigits scans decimal digits with numeric separators and returns the end offset.
func (p *parser) scanDigits(pos int, sep *bool) int {
	src := p.src
	begin := pos
	for pos < len(src) {
		c := src[pos]
		if c >= '0' && c <= '9' {
			pos++
			continue
		}
		if c != '_' {
			break
		}
		if pos == begin || src[pos-1] == '_' || pos+1 >= len(src) || charClass[src[pos+1]]&charDigit == 0 {
			p.fail(pos, "numeric separator is not allowed here")
		}
		*sep = true
		pos++
	}
	return pos
}

func (p *parser) finishNumber(pos int) {
	src := p.src
	if pos < len(src) {
		c := src[pos]
		bad := c == '\\' || charClass[c]&charIDPart != 0
		if c >= utf8.RuneSelf {
			r, _ := utf8.DecodeRuneInString(src[pos:])
			bad = isIDStartRune(r)
		}
		if bad {
			p.fail(pos, "identifier starts immediately after numeric literal")
		}
	}
	p.pos = pos
	p.t.end = int32(pos)
}

func (p *parser) scanNumber(start int) {
	src := p.src
	t := &p.t
	t.kind = tNumber
	pos := start
	var sep bool
	var float bool

	if src[pos] == '0' && pos+1 < len(src) {
		switch c := src[pos+1]; {
		case c == 'x' || c == 'X':
			p.scanRadix(start, 16)
			return
		case c == 'o' || c == 'O':
			p.scanRadix(start, 8)
			return
		case c == 'b' || c == 'B':
			p.scanRadix(start, 2)
			return
		case c == '_':
			p.fail(pos+1, "numeric separator is not allowed here")
		case c >= '0' && c <= '9':
			t.octal = true
			octal := true
			for pos < len(src) && charClass[src[pos]]&charDigit != 0 {
				if src[pos] >= '8' {
					octal = false
				}
				pos++
			}
			if octal {
				var v float64
				for _, d := range []byte(src[start:pos]) {
					v = v*8 + float64(d-'0')
				}
				t.num = v
				p.finishNumber(pos)
				return
			}
			if pos < len(src) && src[pos] == '_' {
				p.fail(pos, "numeric separator is not allowed here")
			}
		}
	}

	if pos >= len(src) || src[pos] != '.' {
		pos = p.scanDigits(pos, &sep)
	}
	if !t.octal && pos < len(src) && src[pos] == 'n' {
		t.kind = tBigInt
		digits := src[start:pos]
		if sep {
			digits = strings.ReplaceAll(digits, "_", "")
		}
		t.str = digits
		p.finishNumber(pos + 1)
		return
	}
	if pos < len(src) && src[pos] == '.' {
		float = true
		pos++
		if pos < len(src) && src[pos] == '_' {
			p.fail(pos, "numeric separator is not allowed here")
		}
		pos = p.scanDigits(pos, &sep)
	}
	if pos < len(src) && (src[pos] == 'e' || src[pos] == 'E') {
		float = true
		pos++
		if pos < len(src) && (src[pos] == '+' || src[pos] == '-') {
			pos++
		}
		if pos >= len(src) || charClass[src[pos]]&charDigit == 0 {
			p.fail(pos, "invalid numeric literal")
		}
		pos = p.scanDigits(pos, &sep)
	}

	text := src[start:pos]
	if !float && !sep && len(text) <= 15 {
		var v uint64
		for i := 0; i < len(text); i++ {
			v = v*10 + uint64(text[i]-'0')
		}
		t.num = float64(v)
	} else {
		if sep {
			text = strings.ReplaceAll(text, "_", "")
		}
		// An out of range value yields an infinity together with an error, which is the wanted value.
		t.num, _ = strconv.ParseFloat(text, 64)
	}
	p.finishNumber(pos)
}

func (p *parser) scanRadix(start int, base int) {
	src := p.src
	t := &p.t
	pos := start + 2
	begin := pos
	var sep bool
	for pos < len(src) {
		c := src[pos]
		if v := hexValue(c); v >= 0 && v < base {
			pos++
			continue
		}
		if c != '_' {
			break
		}
		if pos == begin || src[pos-1] == '_' || pos+1 >= len(src) {
			p.fail(pos, "numeric separator is not allowed here")
		}
		if v := hexValue(src[pos+1]); v < 0 || v >= base {
			p.fail(pos, "numeric separator is not allowed here")
		}
		sep = true
		pos++
	}
	if pos == begin {
		p.fail(pos, "invalid numeric literal")
	}
	digits := src[begin:pos]
	if sep {
		digits = strings.ReplaceAll(digits, "_", "")
	}
	if pos < len(src) && src[pos] == 'n' {
		var i big.Int
		i.SetString(digits, base)
		t.kind = tBigInt
		t.str = i.String()
		p.finishNumber(pos + 1)
		return
	}
	bits := 4
	switch base {
	case 8:
		bits = 3
	case 2:
		bits = 1
	}
	if len(digits)*bits <= 64 {
		var v uint64
		for i := 0; i < len(digits); i++ {
			v = v<<bits | uint64(hexValue(digits[i]))
		}
		t.num = float64(v)
	} else {
		var i big.Int
		i.SetString(digits, base)
		t.num, _ = new(big.Float).SetInt(&i).Float64()
	}
	p.finishNumber(pos)
}

func (p *parser) scanString(start int, quote byte) {
	src := p.src
	t := &p.t
	t.kind = tString
	pos := start + 1
	for pos < len(src) {
		c := src[pos]
		if c == quote {
			t.str = src[start+1 : pos]
			p.pos = pos + 1
			t.end = int32(p.pos)
			return
		}
		if c == '\\' || c == '\n' || c == '\r' {
			break
		}
		pos++
	}

	buf := append(make([]byte, 0, pos-start+16), src[start+1:pos]...)
	for {
		if pos >= len(src) {
			p.fail(start, "unterminated string literal")
		}
		c := src[pos]
		switch c {
		case quote:
			t.str = string(buf)
			p.pos = pos + 1
			t.end = int32(p.pos)
			return
		case '\\':
			p.pos = pos + 1
			var st escStatus
			buf, st = p.scanEscape(buf, false)
			if st == escOctal {
				t.octal = true
			}
			pos = p.pos
		case '\n', '\r':
			p.fail(start, "unterminated string literal")
		default:
			buf, pos = appendSourceByte(buf, src, pos)
		}
	}
}

// appendSourceByte appends the source byte at pos to buf and returns the offset of the next byte.
// A lone low surrogate in the source is combined with a high surrogate that ends buf.
func appendSourceByte(buf []byte, src string, pos int) ([]byte, int) {
	if src[pos] == 0xed && src[pos+1] >= 0xb0 {
		r, size := wtf8.DecodeRune(src[pos:])
		return appendWTF8(buf, r), pos + size
	}
	return append(buf, src[pos]), pos + 1
}

type escStatus uint8

const (
	escOK escStatus = iota
	// escOctal is a legacy octal escape or \8 or \9.
	escOctal
	// escInvalid is an escape that is invalid, which only a template reports this way.
	escInvalid
)

// scanEscape scans an escape sequence whose backslash precedes p.pos and appends its value to buf.
// An invalid escape is an error unless template is set.
func (p *parser) scanEscape(buf []byte, template bool) ([]byte, escStatus) {
	src := p.src
	pos := p.pos
	if pos >= len(src) {
		p.fail(pos, "unterminated string literal")
	}
	c := src[pos]
	p.pos = pos + 1
	switch c {
	case 'n':
		return append(buf, '\n'), escOK
	case 't':
		return append(buf, '\t'), escOK
	case 'r':
		return append(buf, '\r'), escOK
	case 'b':
		return append(buf, '\b'), escOK
	case 'v':
		return append(buf, '\v'), escOK
	case 'f':
		return append(buf, '\f'), escOK
	case '\r':
		if p.pos < len(src) && src[p.pos] == '\n' {
			p.pos++
		}
		return buf, escOK
	case '\n':
		return buf, escOK
	case 'x':
		if pos+2 < len(src) {
			h, l := hexValue(src[pos+1]), hexValue(src[pos+2])
			if h >= 0 && l >= 0 {
				p.pos = pos + 3
				return utf8.AppendRune(buf, rune(h<<4|l)), escOK
			}
		}
		if template {
			return buf, escInvalid
		}
		p.fail(pos-1, "invalid hexadecimal escape sequence")
	case 'u':
		r, ok := p.scanUnicodeEscape()
		if ok {
			return appendWTF8(buf, r), escOK
		}
		if template {
			return buf, escInvalid
		}
		p.fail(pos-1, "invalid Unicode escape sequence")
	case '0', '1', '2', '3', '4', '5', '6', '7':
		var next byte
		if pos+1 < len(src) {
			next = src[pos+1]
		}
		if c == '0' && (next < '0' || next > '9') {
			return append(buf, 0), escOK
		}
		if template {
			return buf, escInvalid
		}
		v := rune(c - '0')
		limit := 2
		if c >= '4' {
			limit = 1
		}
		for i := 0; i < limit && p.pos < len(src) && src[p.pos] >= '0' && src[p.pos] <= '7'; i++ {
			v = v*8 + rune(src[p.pos]-'0')
			p.pos++
		}
		return utf8.AppendRune(buf, v), escOctal
	case '8', '9':
		if template {
			return buf, escInvalid
		}
		return append(buf, c), escOctal
	}
	if c < utf8.RuneSelf {
		return append(buf, c), escOK
	}
	r, size := wtf8.DecodeRune(src[pos:])
	p.pos = pos + size
	if r == 0x2028 || r == 0x2029 {
		return buf, escOK
	}
	return appendWTF8(buf, r), escOK
}

// scanTemplate scans template characters from p.pos up to the end of the template or the next substitution.
// tokStart is the offset of the token, which is a backquote or a closing brace.
func (p *parser) scanTemplate(tokStart int) {
	src := p.src
	t := &p.t
	t.kind = tTemplate
	t.start = int32(tokStart)
	t.cooked = true
	t.tail = false
	contentStart := p.pos
	pos := contentStart
	var contentEnd int

	// plain reports that the content so far is its own cooked and raw value.
	plain := true
	var buf []byte
	var hasCR bool
	for {
		if pos >= len(src) {
			p.fail(tokStart, "unterminated template literal")
		}
		c := src[pos]
		if c == '`' {
			t.tail = true
			contentEnd = pos
			pos++
			break
		}
		if c == '$' && pos+1 < len(src) && src[pos+1] == '{' {
			contentEnd = pos
			pos += 2
			break
		}
		if c != '\\' && c != '\r' {
			if plain {
				pos++
			} else {
				buf, pos = appendSourceByte(buf, src, pos)
			}
			continue
		}
		if plain {
			plain = false
			buf = append(make([]byte, 0, pos-contentStart+16), src[contentStart:pos]...)
		}
		if c == '\r' {
			hasCR = true
			buf = append(buf, '\n')
			pos++
			if pos < len(src) && src[pos] == '\n' {
				pos++
			}
			continue
		}
		p.pos = pos + 1
		if p.pos >= len(src) {
			p.fail(tokStart, "unterminated template literal")
		}
		if src[p.pos] == '\r' {
			hasCR = true
		}
		var st escStatus
		buf, st = p.scanEscape(buf, true)
		if st != escOK {
			t.cooked = false
		}
		pos = p.pos
	}

	raw := src[contentStart:contentEnd]
	if hasCR {
		raw = strings.ReplaceAll(raw, "\r\n", "\n")
		raw = strings.ReplaceAll(raw, "\r", "\n")
	}
	t.raw = raw
	switch {
	case plain:
		t.str = raw
	case t.cooked:
		t.str = string(buf)
	default:
		t.str = ""
	}
	p.pos = pos
	t.end = int32(pos)
}

// rescanTemplateContinuation replaces the current '}' token with the template token that starts there.
func (p *parser) rescanTemplateContinuation() {
	p.pos = int(p.t.start) + 1
	p.scanTemplate(int(p.t.start))
}

// rescanRegExp replaces the current '/' or '/=' token with the regular expression literal that starts there.
func (p *parser) rescanRegExp() {
	src := p.src
	start := int(p.t.start)
	pos := start + 1
	var inClass bool
	for {
		if pos >= len(src) {
			p.fail(start, "unterminated regular expression literal")
		}
		c := src[pos]
		if c == '\\' {
			pos++
			if pos >= len(src) {
				p.fail(start, "unterminated regular expression literal")
			}
			c = src[pos]
		} else if c == '[' {
			inClass = true
		} else if c == ']' {
			inClass = false
		} else if c == '/' && !inClass {
			break
		}
		if c == '\n' || c == '\r' || c == 0xe2 && pos+2 < len(src) && src[pos+1] == 0x80 && (src[pos+2] == 0xa8 || src[pos+2] == 0xa9) {
			p.fail(start, "unterminated regular expression literal")
		}
		pos++
	}
	t := &p.t
	t.kind = tRegExp
	t.str = src[start+1 : pos]
	pos++
	flagStart := pos
	for pos < len(src) {
		c := src[pos]
		if c == '\\' {
			p.fail(pos, "invalid regular expression flags")
		}
		if c < utf8.RuneSelf {
			if charClass[c]&charIDPart == 0 {
				break
			}
			pos++
			continue
		}
		r, size := utf8.DecodeRuneInString(src[pos:])
		if !isIDPartRune(r) {
			break
		}
		pos += size
	}
	t.raw = src[flagStart:pos]
	p.pos = pos
	t.end = int32(pos)
}
