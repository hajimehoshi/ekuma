// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package number

import (
	"strings"
)

// tokenKind is the kind of a token of the affix of a number pattern of CLDR.
type tokenKind int

const (
	tokenLiteral tokenKind = iota
	tokenMinus
	tokenPlus
	tokenPercent
	tokenPermille
	tokenCurrency
	tokenApproximately
)

type token struct {
	kind tokenKind
	// text is the text of a literal.
	text string
}

// subpattern is the positive or negative part of a number pattern.
type subpattern struct {
	prefix []token
	suffix []token
}

func (s subpattern) has(kind tokenKind) bool {
	for _, t := range append(s.prefix[:len(s.prefix):len(s.prefix)], s.suffix...) {
		if t.kind == kind {
			return true
		}
	}
	return false
}

// pattern is a parsed number pattern of CLDR.
type pattern struct {
	positive    subpattern
	negative    subpattern
	hasNegative bool
	hasNumber   bool
	// primary and secondary are the sizes of the groups of integer digits, 0 without
	// grouping.
	primary   int
	secondary int
}

// parsePattern parses the number pattern p of CLDR.
func parsePattern(p string) *pattern {
	parts := splitUnquoted(p, ';')
	pos, number := parseSubpattern(parts[0])
	pat := &pattern{
		positive:  pos,
		hasNumber: number != "",
	}
	if len(parts) > 1 {
		pat.negative, _ = parseSubpattern(parts[1])
		pat.hasNegative = true
	}
	intPart, _, _ := strings.Cut(number, ".")
	intPart, _, _ = strings.Cut(intPart, "E")
	if i := strings.LastIndexByte(intPart, ','); i >= 0 {
		pat.primary = len(intPart) - i - 1
		pat.secondary = pat.primary
		if j := strings.LastIndexByte(intPart[:i], ','); j >= 0 {
			pat.secondary = i - j - 1
		}
	}
	return pat
}

// splitUnquoted splits s at the occurrences of sep outside quotes.
func splitUnquoted(s string, sep byte) []string {
	var out []string
	quoted := false
	start := 0
	for i := range len(s) {
		switch s[i] {
		case '\'':
			quoted = !quoted
		case sep:
			if !quoted {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// isNumberChar reports whether c is part of the number in a pattern.
func isNumberChar(c byte) bool {
	return c == '#' || c == '@' || c == ',' || c == '.' || '0' <= c && c <= '9'
}

// parseSubpattern parses a subpattern into its affixes and returns the number part too.
func parseSubpattern(s string) (subpattern, string) {
	// Find the number part outside quotes.
	quoted := false
	start, end := -1, len(s)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' {
			quoted = !quoted
			continue
		}
		if quoted {
			continue
		}
		if start < 0 {
			if isNumberChar(c) {
				start = i
			}
			continue
		}
		if isNumberChar(c) {
			continue
		}
		if c == 'E' {
			// An exponent such as "E0" or "E+00".
			j := i + 1
			if j < len(s) && s[j] == '+' {
				j++
			}
			for j < len(s) && s[j] == '0' {
				j++
			}
			i = j - 1
			continue
		}
		end = i
		break
	}
	if start < 0 {
		return subpattern{
			prefix: parseAffix(s),
		}, ""
	}
	return subpattern{
		prefix: parseAffix(s[:start]),
		suffix: parseAffix(s[end:]),
	}, s[start:end]
}

// parseAffix parses the affix s of a pattern into tokens.
func parseAffix(s string) []token {
	var tokens []token
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			tokens = append(tokens, token{
				kind: tokenLiteral,
				text: lit.String(),
			})
			lit.Reset()
		}
	}
	add := func(k tokenKind) {
		flush()
		tokens = append(tokens, token{
			kind: k,
		})
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			if i+1 < len(s) && s[i+1] == '\'' {
				lit.WriteByte('\'')
				i++
				continue
			}
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				lit.WriteString(s[i+1:])
				i = len(s)
				continue
			}
			lit.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case c == '-':
			add(tokenMinus)
		case c == '+':
			add(tokenPlus)
		case c == '%':
			add(tokenPercent)
		case c == '~':
			add(tokenApproximately)
		case strings.HasPrefix(s[i:], "‰"):
			add(tokenPermille)
			i += len("‰") - 1
		case strings.HasPrefix(s[i:], "¤"):
			add(tokenCurrency)
			// A sequence of currency signs stands for one currency.
			for strings.HasPrefix(s[i+len("¤"):], "¤") {
				i += len("¤")
			}
			i += len("¤") - 1
		default:
			lit.WriteByte(c)
		}
	}
	flush()
	return tokens
}

// signType is the sign that an affix shows.
type signType int

const (
	signNone signType = iota
	signPlus
	signMinus
)

// affix returns the tokens of the prefix or the suffix of p for the sign, with the
// approximately sign when approximately is true.
func (p *pattern) affix(prefix bool, sign signType, approximately bool) []token {
	plusReplacesMinus := sign == signPlus && !p.positive.has(tokenPlus)
	useNegative := p.hasNegative && (sign == signMinus || p.negative.has(tokenMinus) && (plusReplacesMinus || approximately))
	src := p.positive
	if useNegative {
		src = p.negative
	}
	tokens := src.suffix
	if prefix {
		tokens = src.prefix
	}
	prependSign := prefix && !useNegative && (sign == signMinus || plusReplacesMinus || approximately)
	var signs []tokenKind
	switch {
	case approximately && plusReplacesMinus:
		signs = []tokenKind{tokenApproximately, tokenPlus}
	case approximately && sign == signMinus:
		signs = []tokenKind{tokenApproximately, tokenMinus}
	case approximately:
		signs = []tokenKind{tokenApproximately}
	case plusReplacesMinus:
		signs = []tokenKind{tokenPlus}
	default:
		signs = []tokenKind{tokenMinus}
	}
	var out []token
	if prependSign {
		tokens = append([]token{{
			kind: tokenMinus,
		}}, tokens...)
	}
	for _, t := range tokens {
		if t.kind != tokenMinus {
			out = append(out, t)
			continue
		}
		for _, k := range signs {
			out = append(out, token{
				kind: k,
			})
		}
	}
	return out
}
