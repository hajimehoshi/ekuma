// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/ast"
)

// slab hands out values of T from shared chunks, so that the many small nodes of a syntax
// tree do not need an allocation each.
type slab[T any] struct {
	free []T
	next int
}

const (
	minSlabChunk = 2
	maxSlabChunk = 64
)

// newNode returns a pointer to a value of s that holds v.
func newNode[T any](s *slab[T], v T) *T {
	if len(s.free) == 0 {
		// The chunks start small so that a short source does not pay for large ones.
		s.next = min(max(s.next*2, minSlabChunk), maxSlabChunk)
		// slices.Grow rounds the capacity up to what the allocation can hold.
		s.free = slices.Grow([]T(nil), s.next)
		s.free = s.free[:cap(s.free)]
	}
	n := &s.free[0]
	s.free = s.free[1:]
	*n = v
	return n
}

// nodeSlabs holds the slabs of the most frequent kinds of nodes.
type nodeSlabs struct {
	identifier slab[ast.Identifier]
	member     slab[ast.MemberExpr]
	call       slab[ast.CallExpr]
	function   slab[ast.Function]
	exprStmt   slab[ast.ExprStmt]
	assign     slab[ast.AssignExpr]
	number     slab[ast.NumberLit]
	str        slab[ast.StringLit]
	scope      slab[ast.Scope]
	ifStmt     slab[ast.IfStmt]
	varDecl    slab[ast.VarDecl]
	this       slab[ast.ThisExpr]
	binary     slab[ast.BinaryExpr]
	block      slab[ast.BlockStmt]
	logical    slab[ast.LogicalExpr]
	ret        slab[ast.ReturnStmt]
}

// listStack holds the items of lists being parsed, whose lengths are not known in advance.
// Nested lists share it: a list starts at a mark and is taken off before the enclosing list
// continues.
type listStack[T any] struct {
	items []T

	// free is the unused part of the chunk that taken lists are copied into, and next is the
	// length of the latest chunk.
	free []T
	next int
}

// The lengths of the first and of the longest chunks of a listStack.
const (
	minListStackChunk = 4
	maxListStackChunk = 256
)

// mark returns the position where a list that starts now begins.
func (s *listStack[T]) mark() int {
	return len(s.items)
}

func (s *listStack[T]) push(v T) {
	s.items = append(s.items, v)
}

// take removes the items from mark on and returns them in a slice with no spare capacity, or
// nil when there are none.
func (s *listStack[T]) take(mark int) []T {
	items := s.items[mark:]
	list := s.alloc(len(items))
	copy(list, items)
	// The items from mark on stay in the storage, which goes away with the parser.
	s.items = s.items[:mark]
	return list
}

// alloc returns a slice of n zero values with no spare capacity, or nil when n is 0.
func (s *listStack[T]) alloc(n int) []T {
	if n == 0 {
		return nil
	}
	if n > len(s.free) {
		// The chunks start small so that a short source does not pay for large ones.
		size := min(max(s.next*2, minListStackChunk), maxListStackChunk)
		if n > size {
			return make([]T, n)
		}
		s.next = size
		// slices.Grow rounds the capacity up to what the allocation can hold.
		s.free = slices.Grow([]T(nil), size)
		s.free = s.free[:cap(s.free)]
	}
	list := s.free[:n:n]
	s.free = s.free[n:]
	return list
}
