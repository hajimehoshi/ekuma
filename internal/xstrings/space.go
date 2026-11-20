// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package xstrings

import (
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// IsWhiteSpaceOrLineTerminator reports whether r is WhiteSpace or LineTerminator.
func IsWhiteSpaceOrLineTerminator(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// TrimWhiteSpace implements TrimString. start and end select the sides to trim.
func TrimWhiteSpace(s wtf8.String, start, end bool) wtf8.String {
	if start {
		for len(s) > 0 {
			if c := s[0]; c < utf8.RuneSelf {
				if !IsWhiteSpaceOrLineTerminator(rune(c)) {
					break
				}
				s = s[1:]
				continue
			}
			r, size := wtf8.DecodeRune(s)
			if !IsWhiteSpaceOrLineTerminator(r) {
				break
			}
			s = s[size:]
		}
	}
	if end {
		for len(s) > 0 {
			if c := s[len(s)-1]; c < utf8.RuneSelf {
				if !IsWhiteSpaceOrLineTerminator(rune(c)) {
					break
				}
				s = s[:len(s)-1]
				continue
			}
			r, size := wtf8.DecodeLastRune(s)
			if !IsWhiteSpaceOrLineTerminator(r) {
				break
			}
			s = s[:len(s)-size]
		}
	}
	return s
}
