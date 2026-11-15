// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package dates

import (
	"strings"
)

// The fields of skeletons, in the order of ICU's DateTimePatternGenerator.
const (
	fieldEra = iota
	fieldYear
	fieldQuarter
	fieldMonth
	fieldWeekOfYear
	fieldWeekOfMonth
	fieldWeekday
	fieldDayOfYear
	fieldDayOfWeekInMonth
	fieldDay
	fieldDayPeriod
	fieldHour
	fieldMinute
	fieldSecond
	fieldFractionalSecond
	fieldZone
	fieldCount
)

// dateFieldMask is the mask of the date fields, and timeFieldMask that of the time fields.
const (
	dateFieldMask = 1<<fieldDayPeriod - 1
	timeFieldMask = 1<<fieldCount - 1 - dateFieldMask
)

// The kinds of fields, which matching compares by distance.
const (
	typeNumeric = 0x100
	typeNarrow  = -0x101
	typeShorter = -0x102
	typeShort   = -0x103
	typeLong    = -0x104
	typeDelta   = 0x10
)

// fieldType describes the fields of a pattern letter with lengths from minLen to maxLen,
// where maxLen 0 is unbounded.
type fieldType struct {
	ch     byte
	field  int
	typ    int
	minLen int
	maxLen int
}

// fieldTypes are the pattern letters, grouped by letter in the order of their minimum
// lengths.
var fieldTypes = []fieldType{
	{'G', fieldEra, typeShort, 1, 3},
	{'G', fieldEra, typeLong, 4, 0},
	{'G', fieldEra, typeNarrow, 5, 0},

	{'y', fieldYear, typeNumeric, 1, 20},
	{'Y', fieldYear, typeNumeric + typeDelta, 1, 20},
	{'u', fieldYear, typeNumeric + 2*typeDelta, 1, 20},
	{'r', fieldYear, typeNumeric + 3*typeDelta, 1, 20},
	{'U', fieldYear, typeShort, 1, 3},
	{'U', fieldYear, typeLong, 4, 0},
	{'U', fieldYear, typeNarrow, 5, 0},

	{'Q', fieldQuarter, typeNumeric, 1, 2},
	{'Q', fieldQuarter, typeShort, 3, 0},
	{'Q', fieldQuarter, typeLong, 4, 0},
	{'Q', fieldQuarter, typeNarrow, 5, 0},
	{'q', fieldQuarter, typeNumeric + typeDelta, 1, 2},
	{'q', fieldQuarter, typeShort - typeDelta, 3, 0},
	{'q', fieldQuarter, typeLong - typeDelta, 4, 0},
	{'q', fieldQuarter, typeNarrow - typeDelta, 5, 0},

	{'M', fieldMonth, typeNumeric, 1, 2},
	{'M', fieldMonth, typeShort, 3, 0},
	{'M', fieldMonth, typeLong, 4, 0},
	{'M', fieldMonth, typeNarrow, 5, 0},
	{'L', fieldMonth, typeNumeric + typeDelta, 1, 2},
	{'L', fieldMonth, typeShort - typeDelta, 3, 0},
	{'L', fieldMonth, typeLong - typeDelta, 4, 0},
	{'L', fieldMonth, typeNarrow - typeDelta, 5, 0},
	{'l', fieldMonth, typeNumeric + typeDelta, 1, 1},

	{'w', fieldWeekOfYear, typeNumeric, 1, 2},

	{'W', fieldWeekOfMonth, typeNumeric, 1, 0},

	{'E', fieldWeekday, typeShort, 1, 3},
	{'E', fieldWeekday, typeLong, 4, 0},
	{'E', fieldWeekday, typeNarrow, 5, 0},
	{'E', fieldWeekday, typeShorter, 6, 0},
	{'c', fieldWeekday, typeNumeric + 2*typeDelta, 1, 2},
	{'c', fieldWeekday, typeShort - 2*typeDelta, 3, 0},
	{'c', fieldWeekday, typeLong - 2*typeDelta, 4, 0},
	{'c', fieldWeekday, typeNarrow - 2*typeDelta, 5, 0},
	{'c', fieldWeekday, typeShorter - 2*typeDelta, 6, 0},
	{'e', fieldWeekday, typeNumeric + typeDelta, 1, 2},
	{'e', fieldWeekday, typeShort - typeDelta, 3, 0},
	{'e', fieldWeekday, typeLong - typeDelta, 4, 0},
	{'e', fieldWeekday, typeNarrow - typeDelta, 5, 0},
	{'e', fieldWeekday, typeShorter - typeDelta, 6, 0},

	{'d', fieldDay, typeNumeric, 1, 2},
	{'g', fieldDay, typeNumeric + typeDelta, 1, 20},
	{'D', fieldDayOfYear, typeNumeric, 1, 3},
	{'F', fieldDayOfWeekInMonth, typeNumeric, 1, 0},

	{'a', fieldDayPeriod, typeShort, 1, 3},
	{'a', fieldDayPeriod, typeLong, 4, 0},
	{'a', fieldDayPeriod, typeNarrow, 5, 0},
	{'b', fieldDayPeriod, typeShort - typeDelta, 1, 3},
	{'b', fieldDayPeriod, typeLong - typeDelta, 4, 0},
	{'b', fieldDayPeriod, typeNarrow - typeDelta, 5, 0},
	{'B', fieldDayPeriod, typeShort - 3*typeDelta, 1, 3},
	{'B', fieldDayPeriod, typeLong - 3*typeDelta, 4, 0},
	{'B', fieldDayPeriod, typeNarrow - 3*typeDelta, 5, 0},

	{'H', fieldHour, typeNumeric + 10*typeDelta, 1, 2},
	{'k', fieldHour, typeNumeric + 11*typeDelta, 1, 2},
	{'h', fieldHour, typeNumeric, 1, 2},
	{'K', fieldHour, typeNumeric + typeDelta, 1, 2},
	{'J', fieldHour, typeNumeric + 5*typeDelta, 1, 2},
	{'j', fieldHour, typeNumeric + 6*typeDelta, 1, 6},
	{'C', fieldHour, typeNumeric + 7*typeDelta, 1, 6},

	{'m', fieldMinute, typeNumeric, 1, 2},

	{'s', fieldSecond, typeNumeric, 1, 2},
	{'A', fieldSecond, typeNumeric + typeDelta, 1, 1000},

	{'S', fieldFractionalSecond, typeNumeric, 1, 1000},

	{'v', fieldZone, typeShort - 2*typeDelta, 1, 0},
	{'v', fieldZone, typeLong - 2*typeDelta, 4, 0},
	{'z', fieldZone, typeShort, 1, 3},
	{'z', fieldZone, typeLong, 4, 0},
	{'Z', fieldZone, typeNarrow - typeDelta, 1, 3},
	{'Z', fieldZone, typeLong - typeDelta, 4, 0},
	{'Z', fieldZone, typeShort - typeDelta, 5, 0},
	{'O', fieldZone, typeShort - typeDelta, 1, 0},
	{'O', fieldZone, typeLong - typeDelta, 4, 0},
	{'V', fieldZone, typeShort - typeDelta, 1, 0},
	{'V', fieldZone, typeLong - typeDelta, 2, 0},
	{'V', fieldZone, typeLong - 1 - typeDelta, 3, 0},
	{'V', fieldZone, typeLong - 2 - typeDelta, 4, 0},
	{'x', fieldZone, typeNarrow - typeDelta, 1, 0},
	{'x', fieldZone, typeShort - typeDelta, 2, 0},
	{'x', fieldZone, typeLong - typeDelta, 4, 0},
	{'X', fieldZone, typeNarrow - typeDelta, 1, 0},
	{'X', fieldZone, typeShort - typeDelta, 2, 0},
	{'X', fieldZone, typeLong - typeDelta, 4, 0},
}

// canonicalIndex returns the index in fieldTypes of the field of the letter c with the
// length n, or -1 when c is not a pattern letter.
func canonicalIndex(c byte, n int) int {
	best := -1
	for i, t := range fieldTypes {
		if t.ch != c {
			continue
		}
		best = i
		if i+1 == len(fieldTypes) || fieldTypes[i+1].ch != c || fieldTypes[i+1].minLen > n {
			return i
		}
	}
	return best
}

// skeleton is the fields of a pattern or a skeleton.
type skeleton struct {
	// chars and lens are the letters and lengths of the fields as given, or 0.
	chars [fieldCount]byte
	lens  [fieldCount]int
	// baseLens are the minimum lengths of the kinds of the fields.
	baseChars [fieldCount]byte
	baseLens  [fieldCount]int
	// types are the kinds of the fields, or 0 for an absent field.
	types [fieldCount]int
}

// newSkeleton returns the skeleton of the pattern or skeleton p.
func newSkeleton(p string) *skeleton {
	s := &skeleton{}
	for _, it := range parsePattern(p) {
		if it.ch == 0 {
			continue
		}
		i := canonicalIndex(it.ch, it.n)
		if i < 0 {
			continue
		}
		t := fieldTypes[i]
		s.chars[t.field] = it.ch
		s.lens[t.field] = it.n
		s.baseChars[t.field] = t.ch
		s.baseLens[t.field] = t.minLen
		typ := t.typ
		if typ > 0 {
			typ += it.n
		}
		s.types[t.field] = typ
	}
	// Fractional seconds with minutes need seconds.
	if s.chars[fieldFractionalSecond] != 0 && s.chars[fieldMinute] != 0 && s.chars[fieldSecond] == 0 {
		s.set(fieldSecond, 's', 1)
	}
	if h := s.chars[fieldHour]; h != 0 {
		if h == 'h' || h == 'K' {
			// A 12-hour cycle needs a day period.
			if s.chars[fieldDayPeriod] == 0 {
				s.set(fieldDayPeriod, 'a', 1)
			}
		} else if s.chars[fieldDayPeriod] != 0 {
			// A 24-hour cycle has none.
			s.chars[fieldDayPeriod] = 0
			s.lens[fieldDayPeriod] = 0
			s.baseChars[fieldDayPeriod] = 0
			s.baseLens[fieldDayPeriod] = 0
			s.types[fieldDayPeriod] = 0
		}
	}
	return s
}

// set sets the field to the letter c with the length n.
func (s *skeleton) set(field int, c byte, n int) {
	i := canonicalIndex(c, n)
	t := fieldTypes[i]
	s.chars[field] = c
	s.lens[field] = n
	s.baseChars[field] = t.ch
	s.baseLens[field] = t.minLen
	typ := t.typ
	if typ > 0 {
		typ += n
	}
	s.types[field] = typ
}

// fieldMask returns the mask of the fields of s.
func (s *skeleton) fieldMask() int {
	m := 0
	for i := range fieldCount {
		if s.types[i] != 0 {
			m |= 1 << i
		}
	}
	return m
}

// base returns the base pattern of s, which is the letters of its fields with the minimum
// lengths of their kinds.
func (s *skeleton) base() string {
	var b strings.Builder
	for i := range fieldCount {
		for range s.baseLens[i] {
			b.WriteByte(s.baseChars[i])
		}
	}
	return b.String()
}

// String returns the fields of s as a skeleton.
func (s *skeleton) String() string {
	var b strings.Builder
	for i := range fieldCount {
		for range s.lens[i] {
			b.WriteByte(s.chars[i])
		}
	}
	return b.String()
}

// distanceInfo is the fields that a pattern lacks or has in excess of a request.
type distanceInfo struct {
	missing int
	extra   int
}

const (
	extraFieldDistance   = 0x10000
	missingFieldDistance = 0x1000
)

// distance returns the distance of the skeleton other from s for the fields of mask.
func (s *skeleton) distance(other *skeleton, mask int, info *distanceInfo) int {
	d := 0
	*info = distanceInfo{}
	for i := range fieldCount {
		my := 0
		if mask&(1<<i) != 0 {
			my = s.types[i]
		}
		o := other.types[i]
		if my == o {
			continue
		}
		switch {
		case my == 0:
			d += extraFieldDistance
			info.extra |= 1 << i
		case o == 0:
			d += missingFieldDistance
			info.missing |= 1 << i
		default:
			d += abs(my - o)
		}
	}
	return d
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// SkeletonOf returns the skeleton of the date pattern p, the fields of p without literals,
// in the order of ICU's DateTimePatternGenerator.staticGetSkeleton.
func SkeletonOf(p string) string {
	return newSkeleton(p).String()
}
