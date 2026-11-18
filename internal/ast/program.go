// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ast

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

	// Assigned maps a variable name to the [Function.Order] of each function whose own code, outside
	// the functions nested in it, assigns to the name, in increasing order. The names in the
	// targets of assignment expressions, update expressions and for-in and for-of heads count.
	Assigned map[string][]int32
}

// Text returns the source text of n.
func (p *Program) Text(n Node) string {
	return p.Source[n.Start():n.End()]
}
