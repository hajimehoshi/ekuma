// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package wtf8

import "strings"

// MapWellFormed applies f to each maximal run without lone surrogates, preserving the lone surrogates.
// f must return valid UTF-8.
func MapWellFormed(s String, f func(string) string) String {
	var b Buffer
	for len(s) > 0 {
		i := loneSurrogateIndex(s)
		b.WriteString(String(f(string(s[:i]))))
		s = s[i:]
		if len(s) > 0 {
			r, size := DecodeRune(s)
			b.WriteRune(r)
			s = s[size:]
		}
	}
	return b.String()
}

// loneSurrogateIndex returns the offset of the first lone surrogate in s, or len(s).
func loneSurrogateIndex(s String) int {
	// A surrogate is ED A0..BF xx, and a well-formed character never starts with ED followed by
	// A0 or more.
	for i := 0; ; {
		j := strings.IndexByte(string(s[i:]), 0xED)
		if j < 0 {
			return len(s)
		}
		i += j
		if i+1 < len(s) && s[i+1] >= 0xA0 {
			return i
		}
		i++
	}
}
