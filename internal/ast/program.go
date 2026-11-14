// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ast

import (
	"sort"
	"strings"
	"sync"
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

// Position converts a byte offset into a 1-based line and a 1-based column counted in code points.
func (p *Program) Position(offset int32) (line, column int) {
	p.lineOnce.Do(func() {
		p.lineStarts = lineStarts(p.Source)
		p.codePoints = make([]int32, len(p.Source)/codePointBlock+1)
		for i := 1; i < len(p.codePoints); i++ {
			p.codePoints[i] = p.codePoints[i-1] + int32(countCodePoints(p.Source[(i-1)*codePointBlock:i*codePointBlock]))
		}
	})
	if offset < 0 {
		offset = 0
	}
	if int(offset) > len(p.Source) {
		offset = int32(len(p.Source))
	}
	i := sort.Search(len(p.lineStarts), func(i int) bool {
		return p.lineStarts[i] > offset
	}) - 1
	return i + 1, p.codePointsBefore(offset) - p.codePointsBefore(p.lineStarts[i]) + 1
}

// codePointsBefore returns the number of code points in [Program.Source] before offset.
func (p *Program) codePointsBefore(offset int32) int {
	k := int(offset) / codePointBlock
	return int(p.codePoints[k]) + countCodePoints(p.Source[k*codePointBlock:offset])
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

func lineStarts(src string) []int32 {
	starts := make([]int32, 1, 1+strings.Count(src, "\n"))
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
