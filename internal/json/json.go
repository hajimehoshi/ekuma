// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package json implements JSON for JavaScript values, whose strings are in WTF-8: parsing text
// into values, calling a reviver on them, converting values to text, and the lexical grammar.
//
// A scanning function takes the text and the index at which to start. It returns the index
// after what it scanned and true, or the index of the first byte that does not fit the grammar
// and false. An index equal to the length of the text means that the text ended too early.
package json

import (
	"strings"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// IsSpace reports whether c is JSON whitespace.
func IsSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// SkipSpace returns the index of the first byte at or after i that is not whitespace.
func SkipSpace(s string, i int) int {
	for i < len(s) && IsSpace(s[i]) {
		i++
	}
	return i
}

// ScanLiteral scans the literal word, such as true, at i.
func ScanLiteral(s string, i int, word string) (int, bool) {
	if strings.HasPrefix(s[i:], word) {
		return i + len(word), true
	}
	for k := 0; k < len(word) && i < len(s) && s[i] == word[k]; k++ {
		i++
	}
	return i, false
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// ScanNumber scans a number at i and returns its value. poll, which may be nil, is called now and
// then while a long number is scanned, and may stop the scan by panicking.
func ScanNumber(s string, i int, poll func()) (float64, int, bool) {
	start := i
	neg := false
	if i < len(s) && s[i] == '-' {
		neg = true
		i++
	}
	if i >= len(s) || !isDigit(s[i]) {
		return 0, i, false
	}
	var n int64
	digits := 0
	if s[i] == '0' {
		i++
		digits = 1
	} else {
		for i < len(s) && isDigit(s[i]) && digits < 15 {
			n = n*10 + int64(s[i]-'0')
			i++
			digits++
		}
		if i < len(s) && isDigit(s[i]) {
			end := skipDigits(s, i, poll)
			digits += end - i
			i = end
		}
	}
	exact := digits <= 15
	if i < len(s) && s[i] == '.' {
		i++
		if i >= len(s) || !isDigit(s[i]) {
			return 0, i, false
		}
		i = skipDigits(s, i, poll)
		exact = false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if i >= len(s) || !isDigit(s[i]) {
			return 0, i, false
		}
		i = skipDigits(s, i, poll)
		exact = false
	}
	if !exact {
		return numconv.StringToNumber(s[start:i], poll), i, true
	}
	f := float64(n)
	if neg {
		f = -f
	}
	return f, i, true
}

// scanStep is the number of bytes of a long number that ScanNumber scans between two calls of
// its poll function.
const scanStep = 1 << 16

// skipDigits returns the end of the run of digits in s from i, calling poll between the steps of
// a long run.
func skipDigits(s string, i int, poll func()) int {
	for {
		end := min(len(s), i+scanStep)
		for i < end && isDigit(s[i]) {
			i++
		}
		if i < end || i == len(s) {
			return i
		}
		if poll != nil {
			poll()
		}
	}
}

// ScanString scans a string whose opening quote is at i and returns its contents in canonical WTF-8.
// The contents of a string without escapes are a substring of s. Otherwise they are built in b,
// which [ScanString] resets first.
func ScanString(s string, i int, b *wtf8.Builder) (string, int, bool) {
	start := i + 1
	i = start
	for i < len(s) {
		c := s[i]
		if c == '"' {
			return s[start:i], i + 1, true
		}
		if c == '\\' || c < 0x20 {
			break
		}
		i++
	}

	// The string has escapes. b joins the halves of a pair that an escape and the text around it form.
	b.Reset()
	b.WriteString(s[start:i])
	for {
		run := i
		for i < len(s) && s[i] != '"' && s[i] != '\\' && s[i] >= 0x20 {
			i++
		}
		b.WriteString(s[run:i])
		if i >= len(s) || s[i] < 0x20 {
			return "", i, false
		}
		if s[i] == '"' {
			return b.String(), i + 1, true
		}
		i++
		if i >= len(s) {
			return "", i, false
		}
		c := s[i]
		switch c {
		case '"', '\\', '/':
			b.WriteByte(c)
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'u':
			var u uint16
			for range 4 {
				i++
				if i >= len(s) {
					return "", i, false
				}
				d := numconv.DigitValue(s[i])
				if d >= 16 {
					return "", i, false
				}
				u = u<<4 | uint16(d)
			}
			b.WriteUnit(u)
		default:
			return "", i, false
		}
		i++
	}
}

// skipString scans a string whose opening quote is at i without building its contents.
func skipString(s string, i int) (int, bool) {
	i++
	for i < len(s) {
		switch c := s[i]; {
		case c == '"':
			return i + 1, true
		case c < 0x20:
			return i, false
		case c == '\\':
			i++
			if i >= len(s) {
				return i, false
			}
			switch s[i] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			case 'u':
				for range 4 {
					i++
					if i >= len(s) || numconv.DigitValue(s[i]) >= 16 {
						return i, false
					}
				}
			default:
				return i, false
			}
		}
		i++
	}
	return i, false
}

// Valid reports whether s is a JSON text. When it is not, it also returns the index at which
// the grammar fails. poll is as for [ScanNumber].
func Valid(s string, poll func()) (int, bool) {
	// closers holds the closing bracket of every array and object that is open.
	var closers []byte
	i := 0
	for {
		// A value starts here.
		i = SkipSpace(s, i)
		if i >= len(s) {
			return i, false
		}
		ok := true
		switch s[i] {
		case '[':
			i = SkipSpace(s, i+1)
			if i < len(s) && s[i] == ']' {
				i++
				break
			}
			closers = append(closers, ']')
			continue
		case '{':
			i = SkipSpace(s, i+1)
			if i < len(s) && s[i] == '}' {
				i++
				break
			}
			closers = append(closers, '}')
			if i, ok = validKey(s, i); !ok {
				return i, false
			}
			continue
		case '"':
			i, ok = skipString(s, i)
		case 't':
			i, ok = ScanLiteral(s, i, "true")
		case 'f':
			i, ok = ScanLiteral(s, i, "false")
		case 'n':
			i, ok = ScanLiteral(s, i, "null")
		default:
			_, i, ok = ScanNumber(s, i, poll)
		}
		if !ok {
			return i, false
		}

		// A value is complete. Close the arrays and objects that it completes.
		for {
			i = SkipSpace(s, i)
			if len(closers) == 0 {
				return i, i == len(s)
			}
			if i >= len(s) {
				return i, false
			}
			c := s[i]
			if c == ',' {
				i++
				if closers[len(closers)-1] == '}' {
					if i, ok = validKey(s, SkipSpace(s, i)); !ok {
						return i, false
					}
				}
				break
			}
			if c != closers[len(closers)-1] {
				return i, false
			}
			i++
			closers = closers[:len(closers)-1]
		}
	}
}

// validKey scans the key of an object member at i and the colon after it.
func validKey(s string, i int) (int, bool) {
	if i >= len(s) || s[i] != '"' {
		return i, false
	}
	i, ok := skipString(s, i)
	if !ok {
		return i, false
	}
	i = SkipSpace(s, i)
	if i >= len(s) || s[i] != ':' {
		return i, false
	}
	return i + 1, true
}

const hexDigits = "0123456789abcdef"

// plain says which bytes [AppendQuote] copies as they are.
// 0xED is excluded because it may start a lone surrogate.
var plain = func() (t [256]bool) {
	for c := 0x20; c < 0x100; c++ {
		t[c] = c != '"' && c != '\\' && c != 0xED
	}
	return t
}()

// AppendQuote implements QuoteJSONString for the string s in canonical WTF-8 and appends the result to dst.
func AppendQuote(dst []byte, s string) []byte {
	dst = append(dst, '"')
	dst = appendEscaped(dst, s)
	return append(dst, '"')
}

// appendEscaped appends the string s in canonical WTF-8 to dst as QuoteJSONString escapes it,
// without the quotes.
func appendEscaped(dst []byte, s string) []byte {
	run := 0
	for i := 0; i < len(s); {
		if plain[s[i]] {
			i++
			continue
		}
		c := s[i]
		if c == 0xED {
			// A lone surrogate is ED A0..BF xx. A well-formed character never starts with ED
			// followed by A0 or more.
			if s[i+1] < 0xA0 {
				i++
				continue
			}
			dst = append(dst, s[run:i]...)
			r, _ := wtf8.DecodeRune(s[i:])
			dst = append(dst, '\\', 'u', hexDigits[r>>12], hexDigits[r>>8&0xf], hexDigits[r>>4&0xf], hexDigits[r&0xf])
			i += 3
			run = i
			continue
		}
		dst = append(dst, s[run:i]...)
		switch c {
		case '"':
			dst = append(dst, '\\', '"')
		case '\\':
			dst = append(dst, '\\', '\\')
		case '\b':
			dst = append(dst, '\\', 'b')
		case '\f':
			dst = append(dst, '\\', 'f')
		case '\n':
			dst = append(dst, '\\', 'n')
		case '\r':
			dst = append(dst, '\\', 'r')
		case '\t':
			dst = append(dst, '\\', 't')
		default:
			dst = append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
		}
		i++
		run = i
	}
	return append(dst, s[run:]...)
}
