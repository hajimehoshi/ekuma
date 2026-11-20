// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
)

const (
	MaxLinks      = maxLinks
	MaxFreeScopes = maxFreeScopes
	MaxFreeNames  = maxFreeNames
	MaxFreeItems  = maxFreeItems
)

// KeptScratch parses the scripts srcs in turn with one scratch set and returns the number of
// scope records that the set keeps, the largest capacity of their name lists, the largest
// capacity of its list stacks, and whether all that storage is cleared.
func KeptScratch(srcs ...string) (scopes, names, items int, cleared bool) {
	s := &scratch{}
	for _, src := range srcs {
		var p parser
		p.init(src, &Options{}, false)
		p.release()
		p.takeScratch(s)
		p.scan()
		p.pushScope(ast.ScopeScript, true)
		p.parseStatementList(nil, tEOF)
		p.popScope()
		p.giveBack()
	}
	cleared = zero(s.stmts) && zero(s.exprs) && zero(s.pending) && zero(s.assigned) && zero(s.deferred) && zero(s.labels)
	for _, sc := range s.scopes {
		names = max(names, cap(sc.names))
		cleared = cleared && zero(sc.names) && zero(sc.hoist) && sc.ast == nil && sc.parent == nil && sc.index == nil
	}
	return len(s.scopes), names, max(cap(s.stmts), cap(s.exprs)), cleared
}

// zero reports whether every element of list up to its capacity is the zero value.
func zero[T comparable](list []T) bool {
	var z T
	for _, v := range list[:cap(list)] {
		if v != z {
			return false
		}
	}
	return true
}
