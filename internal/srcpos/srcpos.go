// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package srcpos converts byte offsets in source text into lines and columns.
package srcpos

import (
	"sort"
	"strings"
	"sync"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Table converts byte offsets in a source text into positions. A Table may be used by multiple
// goroutines at the same time.
type Table struct {
	src wtf8.String

	linesOnce  sync.Once
	lineStarts []int32

	// ascii reports whether src consists of ASCII characters only.
	ascii bool

	codePointsOnce sync.Once

	// codePoints holds the number of code points before each multiple of codePointBlock in src.
	codePoints []int32
}

// codePointBlock is the interval in bytes at which a [Table] counts the code points of its source.
const codePointBlock = 256

// NewTable returns a Table for the text src.
func NewTable(src wtf8.String) *Table {
	return &Table{
		src: src,
	}
}

func (t *Table) locate(offset int32) (index int, at int32) {
	t.linesOnce.Do(func() {
		t.lineStarts = lineStarts(t.src)
		t.ascii = wtf8.IsASCII(t.src)
	})
	at = min(max(offset, 0), int32(len(t.src)))
	index = sort.Search(len(t.lineStarts), func(i int) bool {
		return t.lineStarts[i] > at
	}) - 1
	return index, at
}

// Position converts a byte offset into a 1-based line and a 1-based column counted in code points.
func (t *Table) Position(offset int32) (line, column int) {
	i, at := t.locate(offset)
	return i + 1, t.column(t.lineStarts[i], at, t.ascii)
}

// SourceLine returns the line containing offset and its 1-based line and UTF-16 column numbers.
func (t *Table) SourceLine(offset int32) (text wtf8.String, line, column int) {
	i, at := t.locate(offset)
	start, end := t.lineStarts[i], int32(len(t.src))
	if i+1 < len(t.lineStarts) {
		end = t.lineStarts[i+1]
	}
	column = int(at-start) + 1
	if !t.ascii {
		column = wtf8.Len(t.src[start:at]) + 1
	}
	return wtf8.String(strings.TrimRight(string(t.src[start:end]), "\r\n\u2028\u2029")), i + 1, column
}

// column returns the 1-based column of offset on the line starting at lineStart. ascii reports
// whether the source consists of ASCII characters only.
func (t *Table) column(lineStart, offset int32, ascii bool) int {
	if ascii {
		return int(offset-lineStart) + 1
	}
	if offset-lineStart <= 2*codePointBlock {
		return wtf8.CountCodePoints(t.src[lineStart:offset]) + 1
	}
	t.codePointsOnce.Do(func() {
		t.codePoints = make([]int32, len(t.src)/codePointBlock+1)
		for i := 1; i < len(t.codePoints); i++ {
			t.codePoints[i] = t.codePoints[i-1] + int32(wtf8.CountCodePoints(t.src[(i-1)*codePointBlock:i*codePointBlock]))
		}
	})
	return t.codePointsBefore(offset) - t.codePointsBefore(lineStart) + 1
}

// codePointsBefore returns the number of code points in the source before offset.
func (t *Table) codePointsBefore(offset int32) int {
	k := int(offset) / codePointBlock
	return int(t.codePoints[k]) + wtf8.CountCodePoints(t.src[k*codePointBlock:offset])
}

// Position converts a byte offset in src into a 1-based line and a 1-based column counted in code points.
func Position(src wtf8.String, offset int) (line, column int) {
	offset = min(max(offset, 0), len(src))
	starts := lineStarts(src[:offset])
	return len(starts), wtf8.CountCodePoints(src[starts[len(starts)-1]:offset]) + 1
}

// lineStarts returns the offsets at which the lines of src start.
func lineStarts(src wtf8.String) []int32 {
	starts := make([]int32, 1, 1+strings.Count(string(src), "\n"))
	if strings.IndexByte(string(src), '\r') < 0 && !hasLineSeparator(src) {
		// Every line ends with LF.
		for i := 0; ; {
			j := strings.IndexByte(string(src[i:]), '\n')
			if j < 0 {
				return starts
			}
			i += j + 1
			starts = append(starts, int32(i))
		}
	}
	for i := 0; i < len(src); i++ {
		switch c := src[i]; {
		case c == '\n':
			starts = append(starts, int32(i+1))
		case c == '\r':
			if i+1 < len(src) && src[i+1] == '\n' {
				i++
			}
			starts = append(starts, int32(i+1))
		case c == 0xe2 && i+2 < len(src) && src[i+1] == 0x80 && (src[i+2] == 0xa8 || src[i+2] == 0xa9):
			i += 2
			starts = append(starts, int32(i+1))
		}
	}
	return starts
}

// hasLineSeparator reports whether s contains U+2028 or U+2029.
func hasLineSeparator(s wtf8.String) bool {
	for {
		i := strings.IndexByte(string(s), 0xe2)
		if i < 0 || i+2 >= len(s) {
			return false
		}
		if s[i+1] == 0x80 && (s[i+2] == 0xa8 || s[i+2] == 0xa9) {
			return true
		}
		s = s[i+1:]
	}
}
