// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package dates formats dates and times as Intl.DateTimeFormat does, with the CLDR data of
// package cldr. It reproduces the patterns that ICU chooses for the options of
// Intl.DateTimeFormat.
package dates

import (
	"strings"
)

// item is a field or a literal of a date pattern.
type item struct {
	// ch is the pattern letter of a field, or 0 for a literal.
	ch byte
	// n is the length of a field.
	n int
	// lit is the text of a literal.
	lit string
}

func isLetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// parsePattern returns the items of the date pattern p. Adjacent literals are merged.
func parsePattern(p string) []item {
	var items []item
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			items = append(items, item{lit: lit.String()})
			lit.Reset()
		}
	}
	inQuote := false
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '\'':
			if i+1 < len(p) && p[i+1] == '\'' {
				lit.WriteByte('\'')
				i++
				continue
			}
			inQuote = !inQuote
		case inQuote || !isLetter(c):
			lit.WriteByte(c)
		default:
			flush()
			n := 1
			for i+1 < len(p) && p[i+1] == c {
				n++
				i++
			}
			items = append(items, item{ch: c, n: n})
		}
	}
	flush()
	return items
}

// patternString returns the pattern of items, quoting the literals that need it.
func patternString(items []item) string {
	var b strings.Builder
	for _, it := range items {
		if it.ch != 0 {
			for range it.n {
				b.WriteByte(it.ch)
			}
			continue
		}
		b.WriteString(quoteLiteral(it.lit))
	}
	return b.String()
}

// quoteLiteral returns the literal s quoted for a pattern.
func quoteLiteral(s string) string {
	need := false
	for i := range len(s) {
		if isLetter(s[i]) || s[i] == '\'' {
			need = true
			break
		}
	}
	if !need {
		return s
	}
	var b strings.Builder
	inQuote := false
	for i := range len(s) {
		c := s[i]
		switch {
		case c == '\'':
			b.WriteString("''")
		case isLetter(c):
			if !inQuote {
				b.WriteByte('\'')
				inQuote = true
			}
			b.WriteByte(c)
		default:
			if inQuote {
				b.WriteByte('\'')
				inQuote = false
			}
			b.WriteByte(c)
		}
	}
	if inQuote {
		b.WriteByte('\'')
	}
	return b.String()
}

// formatPattern replaces "{0}", "{1}" and "{2}" in the message pattern p with args. A
// message pattern has no quoting other than doubled apostrophes, which are kept as they are,
// since the result is a date pattern.
func formatPattern(p string, args ...string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] == '{' && i+2 < len(p) && p[i+2] == '}' && '0' <= p[i+1] && p[i+1] <= '9' {
			if n := int(p[i+1] - '0'); n < len(args) {
				b.WriteString(args[n])
			}
			i += 2
			continue
		}
		b.WriteByte(p[i])
	}
	return b.String()
}
