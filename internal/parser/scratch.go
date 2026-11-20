// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser

import (
	"sync"
	"sync/atomic"

	"github.com/hajimehoshi/ekuma/internal/ast"
)

// scratch is the storage of a parse that does not become part of the syntax tree, which later
// parses reuse. Every slice is empty.
type scratch struct {
	// scopes are free scope records, cleared.
	scopes []*scope

	stmts    []ast.Stmt
	exprs    []ast.Expr
	props    []ast.Property
	pending  []*ast.Scope
	assigned []string
	deferred []string
	labels   []label
}

// Limits of what is kept for later parses, so that parsing a large program does not keep its
// memory alive: the number of scratch sets in freeScratch, and what each set holds. The name and
// hoist limits are per scope record.
const (
	maxFreeScratch = 4
	maxFreeScopes  = 32
	maxFreeNames   = 128
	maxFreeHoists  = 32
	maxFreeItems   = 1024
	maxFreeList    = 256
)

// freeScratch holds scratch sets for reuse, which garbage collection does not take away. An
// empty slot is nil.
var freeScratch [maxFreeScratch]atomic.Pointer[scratch]

// scratchPool holds the scratch sets for reuse that do not fit in freeScratch.
var scratchPool sync.Pool

// getScratch returns a scratch set that no other parse uses.
func getScratch() *scratch {
	for i := range freeScratch {
		if s := freeScratch[i].Load(); s != nil && freeScratch[i].CompareAndSwap(s, nil) {
			return s
		}
	}
	if s, ok := scratchPool.Get().(*scratch); ok {
		return s
	}
	return &scratch{}
}

// putScratch gives back the scratch set s for reuse.
func putScratch(s *scratch) {
	for i := range freeScratch {
		if freeScratch[i].Load() == nil && freeScratch[i].CompareAndSwap(nil, s) {
			return
		}
	}
	scratchPool.Put(s)
}

// takeScratch moves the storage of the scratch set s into p.
func (p *parser) takeScratch(s *scratch) {
	p.scratch = s
	p.pool = s.scopes
	p.stmts.items = s.stmts
	p.exprs.items = s.exprs
	p.props.items = s.props
	p.pending = s.pending
	p.assigned = s.assigned
	p.deferred = s.deferred
	p.labels = s.labels
}

// release gives the storage of p that is not part of the syntax tree back for later parses.
// p must not be used afterwards.
func (p *parser) release() {
	if s := p.giveBack(); s != nil {
		putScratch(s)
	}
}

// giveBack moves the storage of p that is not part of the syntax tree back into the scratch set
// that it came from, within the limits, and returns the set, or nil when p has none.
func (p *parser) giveBack() *scratch {
	s := p.scratch
	if s == nil {
		return nil
	}
	p.scratch = nil
	*s = scratch{
		scopes:   keptScopes(p.pool),
		stmts:    kept(p.stmts.items, maxFreeItems),
		exprs:    kept(p.exprs.items, maxFreeItems),
		props:    kept(p.props.items, maxFreeList),
		pending:  kept(p.pending, maxFreeList),
		assigned: kept(p.assigned, maxFreeList),
		deferred: kept(p.deferred, maxFreeList),
		labels:   kept(p.labels, maxFreeList),
	}
	return s
}

// kept returns list emptied for reuse, or nil when its capacity is above limit.
func kept[T any](list []T, limit int) []T {
	if cap(list) > limit {
		return nil
	}
	// The elements beyond the length can still point into the tree of the parse.
	list = list[:cap(list)]
	clear(list)
	return list[:0]
}

// keptScopes returns the free scope records of pool that are kept for reuse.
func keptScopes(pool []*scope) []*scope {
	// The elements beyond the length are records that were taken out of the pool, which can
	// still point into the tree.
	clear(pool[len(pool):cap(pool)])
	if cap(pool) > maxFreeScopes {
		pool = append(make([]*scope, 0, maxFreeScopes), pool[:min(len(pool), maxFreeScopes)]...)
	}
	for _, s := range pool {
		if cap(s.names) > maxFreeNames {
			s.names = nil
		}
		if cap(s.hoist) > maxFreeHoists {
			s.hoist = nil
		}
	}
	return pool
}
