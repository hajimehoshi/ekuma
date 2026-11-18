// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
)

// appendTargetNames appends to names the names of the variables that the assignment target pat
// assigns to, and returns the extended slice.
func appendTargetNames(names []string, pat ast.Pattern) []string {
	switch pat := pat.(type) {
	case *ast.Identifier:
		names = append(names, pat.Name)
	case *ast.ObjectPattern:
		for i := range pat.Properties {
			names = appendTargetNames(names, pat.Properties[i].Value)
		}
		if pat.Rest != nil {
			names = appendTargetNames(names, pat.Rest)
		}
	case *ast.ArrayPattern:
		for _, e := range pat.Elements {
			if e != nil {
				names = appendTargetNames(names, e)
			}
		}
	case *ast.AssignPattern:
		names = appendTargetNames(names, pat.Target)
	case *ast.RestElement:
		names = appendTargetNames(names, pat.Target)
	}
	return names
}

// minAssigned is the capacity that parser.assigned starts with.
const minAssigned = 8

// noteAssigned notes the names that the assignment target pat assigns to as names that the own
// code of the current function assigns to.
func (p *parser) noteAssigned(pat ast.Pattern) {
	if p.skipsAssigned() {
		return
	}
	if p.assigned == nil {
		p.assigned = make([]string, 0, minAssigned)
	}
	p.assigned = appendTargetNames(p.assigned, pat)
}

// commitDeferred counts the deferred names from the index mark on as names that the own code of
// the current function assigns to.
func (p *parser) commitDeferred(mark int) {
	if len(p.deferred) > mark {
		if !p.skipsAssigned() {
			p.assigned = append(p.assigned, p.deferred[mark:]...)
		}
		p.deferred = p.deferred[:mark]
	}
}

// skipsAssigned reports whether the names that the code being parsed assigns to need no record:
// the own code of the top level is never asked about, unless it may turn out to be the head of an
// arrow function.
func (p *parser) skipsAssigned() bool {
	return p.f.topLevel && p.arrowBase == nil
}

// endAssigned numbers the function fn, which ends now, and records the names that its own code
// assigns to.
func (p *parser) endAssigned(fn *ast.Function) {
	p.funcs++
	order := p.funcs
	fn.Order = order
	fn.Nested = order - 1 - p.f.funcsBefore
	base := p.f.assignedBase
	for _, name := range p.assigned[base:] {
		orders := p.assignedIn[name]
		if n := len(orders); n > 0 && orders[n-1] == order {
			continue
		}
		if orders == nil {
			if p.assignedIn == nil {
				p.assignedIn = map[string][]int32{}
			}
			orders = p.newList()
		}
		p.assignedIn[name] = append(orders, order)
	}
	p.assigned = p.assigned[:base]
}

const (
	minListChunk = 16
	maxListChunk = 512
)

// newList returns an empty list with room for two, taken from p.lists.
func (p *parser) newList() []int32 {
	n := len(p.lists)
	if cap(p.lists)-n < 2 {
		p.lists = make([]int32, 0, min(max(2*cap(p.lists), minListChunk), maxListChunk))
		n = 0
	}
	p.lists = p.lists[:n+2]
	return p.lists[n : n : n+2]
}
