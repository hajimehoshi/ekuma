// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// maxNumberStringLen bounds the length of the string of a number, such as
// "-1.2345678901234567e-308".
const maxNumberStringLen = 32

// IsCanonicalNumericString reports whether CanonicalNumericIndexString returns a number for the string key.
// The array indices are not named keys, so they are not considered.
func IsCanonicalNumericString(key *Object) bool {
	if key.Kind != KindString {
		return false
	}
	s := key.Text
	if len(s) == 0 {
		return false
	}
	if c := s[0]; c != '-' && c != 'I' && c != 'N' && (c < '0' || c > '9') {
		return false
	}
	if s == "-0" {
		return true
	}
	// The string of a number is short.
	if len(s) > maxNumberStringLen {
		return false
	}
	return numconv.NumberToString(numconv.StringToNumber(s, nil)) == s
}
