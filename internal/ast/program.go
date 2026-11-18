// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ast

import (
	"sort"
	"strings"
	"sync"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Program is a parsed script or module.
type Program struct {
	Span

	// Source is the source text that all node offsets refer to.
	Source string

	Module bool

	// Strict reports whether the top level code is strict mode code.
	Strict bool

	Body  []Stmt
	Scope *Scope

	lineOnce   sync.Once
	lineStarts []int32

	// ascii reports whether [Program.Source] consists of ASCII characters only.
	ascii bool

	codePointsOnce sync.Once

	// codePoints holds the number of code points before each multiple of codePointBlock in
	// [Program.Source].
	codePoints []int32
}

// codePointBlock is the interval in bytes at which [Program] counts the code points of its source.
const codePointBlock = 256

// Text returns the source text of n.
func (p *Program) Text(n Node) string {
	return p.Source[n.Start():n.End()]
}

// lines returns the offsets at which the lines of [Program.Source] start, and whether the source
// consists of ASCII characters only.
func (p *Program) lines() (starts []int32, ascii bool) {
	p.lineOnce.Do(func() {
		p.lineStarts = lineStarts(p.Source)
		p.ascii = wtf8.IsASCII(p.Source)
	})
	return p.lineStarts, p.ascii
}

// clampOffset returns offset limited to the range of [Program.Source].
func (p *Program) clampOffset(offset int32) int32 {
	return min(max(offset, 0), int32(len(p.Source)))
}

// Position converts a byte offset into a 1-based line and a 1-based column counted in code points.
func (p *Program) Position(offset int32) (line, column int) {
	offset = p.clampOffset(offset)
	starts, ascii := p.lines()
	i := sort.Search(len(starts), func(i int) bool {
		return starts[i] > offset
	}) - 1
	return i + 1, p.column(starts[i], offset, ascii)
}

// column returns the 1-based column of offset on the line starting at lineStart. ascii reports
// whether [Program.Source] consists of ASCII characters only.
func (p *Program) column(lineStart, offset int32, ascii bool) int {
	if ascii {
		return int(offset-lineStart) + 1
	}
	if offset-lineStart <= 2*codePointBlock {
		return countCodePoints(p.Source[lineStart:offset]) + 1
	}
	p.codePointsOnce.Do(func() {
		p.codePoints = make([]int32, len(p.Source)/codePointBlock+1)
		for i := 1; i < len(p.codePoints); i++ {
			p.codePoints[i] = p.codePoints[i-1] + int32(countCodePoints(p.Source[(i-1)*codePointBlock:i*codePointBlock]))
		}
	})
	return p.codePointsBefore(offset) - p.codePointsBefore(lineStart) + 1
}

// codePointsBefore returns the number of code points in [Program.Source] before offset.
func (p *Program) codePointsBefore(offset int32) int {
	k := int(offset) / codePointBlock
	return int(p.codePoints[k]) + countCodePoints(p.Source[k*codePointBlock:offset])
}

// Positioner converts byte offsets in the source of a [Program] into positions as
// [Program.Position] does. It is fastest when the offsets mostly increase.
// A Positioner must not be used by multiple goroutines at the same time.
type Positioner struct {
	p *Program

	valid bool

	// offset is the last offset converted, line the 0-based index of its line, and column its
	// 1-based column.
	offset int32
	line   int
	column int
}

// NewPositioner returns a Positioner for p.
func NewPositioner(p *Program) *Positioner {
	return &Positioner{
		p: p,
	}
}

// Position converts a byte offset into a 1-based line and a 1-based column counted in code points.
func (r *Positioner) Position(offset int32) (line, column int) {
	p := r.p
	offset = p.clampOffset(offset)
	if r.valid && offset == r.offset {
		return r.line + 1, r.column
	}
	starts, ascii := p.lines()
	i := 0
	if r.valid {
		i = r.line
	}
	if offset >= starts[i] {
		// Step forward a few lines before searching the rest.
		for range 4 {
			if i+1 >= len(starts) || starts[i+1] > offset {
				break
			}
			i++
		}
		if i+1 < len(starts) && starts[i+1] <= offset {
			rest := starts[i+1:]
			i += sort.Search(len(rest), func(j int) bool {
				return rest[j] > offset
			})
		}
	} else {
		i = sort.Search(i, func(j int) bool {
			return starts[j] > offset
		}) - 1
	}
	if !ascii && r.valid && i == r.line && offset > r.offset && offset-r.offset <= 2*codePointBlock {
		column = r.column + countCodePoints(p.Source[r.offset:offset])
	} else {
		column = p.column(starts[i], offset, ascii)
	}
	r.valid = true
	r.offset = offset
	r.line = i
	r.column = column
	return i + 1, column
}

// Position converts a byte offset in src into a 1-based line and a 1-based column counted in code points.
func Position(src string, offset int) (line, column int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(src) {
		offset = len(src)
	}
	starts := lineStarts(src[:offset])
	return len(starts), countCodePoints(src[starts[len(starts)-1]:offset]) + 1
}

// countCodePoints returns the number of code points in the WTF-8 text s.
func countCodePoints(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i]&0xc0 != 0x80 {
			n++
		}
	}
	return n
}

// lineStarts returns the offsets at which the lines of src start.
func lineStarts(src string) []int32 {
	starts := make([]int32, 1, 1+strings.Count(src, "\n"))
	if strings.IndexByte(src, '\r') < 0 && !hasLineSeparator(src) {
		// Every line ends with LF.
		for i := 0; ; {
			j := strings.IndexByte(src[i:], '\n')
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
func hasLineSeparator(s string) bool {
	for {
		i := strings.IndexByte(s, 0xe2)
		if i < 0 || i+2 >= len(s) {
			return false
		}
		if s[i+1] == 0x80 && (s[i+2] == 0xa8 || s[i+2] == 0xa9) {
			return true
		}
		s = s[i+1:]
	}
}
