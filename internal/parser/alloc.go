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
	// chunk is the latest chunk, whose first used values are handed out, and next is the length
	// it was asked for.
	chunk []T
	used  int
	next  int
}

const (
	minSlabChunk = 2
	maxSlabChunk = 64
)

// newNode returns a pointer to a value of s that holds v.
func newNode[T any](s *slab[T], v T) *T {
	n := alloc(s)
	*n = v
	return n
}

// alloc returns a pointer to a zero value of s.
func alloc[T any](s *slab[T]) *T {
	if s.used == len(s.chunk) {
		// The chunks start small so that a short source does not pay for large ones.
		s.next = min(max(s.next*2, minSlabChunk), maxSlabChunk)
		// slices.Grow rounds the capacity up to what the allocation can hold.
		s.chunk = slices.Grow([]T(nil), s.next)
		s.chunk = s.chunk[:cap(s.chunk)]
		s.used = 0
	}
	// Handing out a value changes only the count, so it stores no pointer.
	n := &s.chunk[s.used]
	s.used++
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
	newExpr    slab[ast.NewExpr]
	array      slab[ast.ArrayLit]
	object     slab[ast.ObjectLit]
}

// listStack holds the items of lists being parsed, whose lengths are not known in advance.
// Nested lists share it: a list starts at a mark and is taken off before the enclosing list
// continues.
type listStack[T any] struct {
	items []T

	// chunk is the latest chunk that taken lists are copied into, whose first used elements are
	// handed out, and next is the length it was asked for.
	chunk []T
	used  int
	next  int
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
	if n > len(s.chunk)-s.used {
		// The chunks start small so that a short source does not pay for large ones.
		size := min(max(s.next*2, minListStackChunk), maxListStackChunk)
		if n > size {
			return make([]T, n)
		}
		s.next = size
		// slices.Grow rounds the capacity up to what the allocation can hold.
		s.chunk = slices.Grow([]T(nil), size)
		s.chunk = s.chunk[:cap(s.chunk)]
		s.used = 0
	}
	list := s.chunk[s.used : s.used+n : s.used+n]
	s.used += n
	return list
}
