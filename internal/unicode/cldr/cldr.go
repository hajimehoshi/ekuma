// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:generate go run gen.go

// Package cldr holds the data of the Unicode Common Locale Data Repository that Intl uses,
// generated from a CLDR release.
//
// Each table is a [Map] or [Strings] named after the CLDR data it comes from. Locale
// identifiers in the tables are BCP 47 language tags with "und" for the root locale.
//
// The tables are under the Unicode License v3, the license of the CLDR data, which is in the
// file LICENSE of this directory. That license covers this package only.
package cldr

import (
	"iter"
	"strings"
)

// Strings is a sorted list of strings.
type Strings struct {
	data string
	// ends holds the end offset in data of each string; a string starts where the previous
	// one ends.
	ends []uint32
}

// Len returns the number of strings in s.
func (s Strings) Len() int {
	return len(s.ends)
}

// At returns the string i of s.
func (s Strings) At(i int) string {
	var start uint32
	if i > 0 {
		start = s.ends[i-1]
	}
	return s.data[start:s.ends[i]]
}

// Index returns the index of x in s, or -1 when s does not contain x.
func (s Strings) Index(x string) int {
	lo, hi := 0, len(s.ends)
	for lo < hi {
		m := int(uint(lo+hi) >> 1)
		switch c := strings.Compare(s.At(m), x); {
		case c == 0:
			return m
		case c < 0:
			lo = m + 1
		default:
			hi = m
		}
	}
	return -1
}

// Contains reports whether s contains x.
func (s Strings) Contains(x string) bool {
	return s.Index(x) >= 0
}

// Map maps the sorted strings of keys to the strings of values with the same index.
type Map struct {
	keys   Strings
	values Strings
}

// Lookup returns the value of the key k, and reports whether m has the key.
func (m Map) Lookup(k string) (string, bool) {
	i := m.keys.Index(k)
	if i < 0 {
		return "", false
	}
	return m.values.At(i), true
}

// Len returns the number of keys in m.
func (m Map) Len() int {
	return m.keys.Len()
}

// Key returns the key i of m in sorted order.
func (m Map) Key(i int) string {
	return m.keys.At(i)
}

// Value returns the value of the key i of m.
func (m Map) Value(i int) string {
	return m.values.At(i)
}

// Field returns the value of the field key in record, a sequence of fields that each start
// with U+001E and hold a key and a value separated by U+001F, and reports whether record
// has the field.
func Field(record, key string) (string, bool) {
	i := 0
	for {
		j := strings.Index(record[i:], key)
		if j < 0 {
			return "", false
		}
		j += i
		end := j + len(key)
		if j > 0 && record[j-1] == '\x1e' && end < len(record) && record[end] == '\x1f' {
			v := record[end+1:]
			if k := strings.IndexByte(v, '\x1e'); k >= 0 {
				v = v[:k]
			}
			return v, true
		}
		i = j + 1
	}
}

// Fields returns the keys and values of the fields of record, which is in the form of
// [Field].
func Fields(record string) iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for f := range strings.SplitSeq(record, "\x1e") {
			k, v, ok := strings.Cut(f, "\x1f")
			if ok && !yield(k, v) {
				return
			}
		}
	}
}
