// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"strings"

	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// ClassElement is an element that the constructor of a class adds to every instance.
type ClassElement struct {
	Kind int
	Key  Value
	Val  *Object

	// Init is the initial value of a field without an initializer function.
	Init Value

	// Dec is what the decorators of a field add to it, or nil.
	Dec *FieldDecoration

	// Cache is the shape transition that defining a public field takes.
	Cache PropCache
}

func (sp *Space) TypeofValue(v Value) Value {
	return sp.Typeofs[typeOf(v)]
}

// ClosurePrototype returns the prototype of the closures of p, which is a generator or async function.
func (p *FuncProto) ClosurePrototype() *Object {
	switch {
	case p.IsGenerator && p.IsAsync:
		return p.Realm.Intrinsics[IntrinsicAsyncGeneratorFunctionPrototype]
	case p.IsGenerator:
		return p.Realm.Intrinsics[IntrinsicGeneratorFunctionPrototype]
	case p.IsAsync:
		return p.Realm.Intrinsics[IntrinsicAsyncFunctionPrototype]
	}
	return p.Realm.Intrinsics[IntrinsicFunctionPrototype]
}

// PairDestructuringPlain reports whether destructuring a new array of two elements into two
// bindings runs nothing that JavaScript could observe.
func (sp *Space) PairDestructuringPlain(r *RealmPart) bool {
	return sp.ArrayIterationUnmodified(r) && r.ArrayIter.NoReturn
}

// Extremum identifies Math.min or Math.max for agent.SetExtremum.
type Extremum uint8

// CapturedScopes returns the dynamic scopes that were open where f was created, innermost last.
func (f *Function) CapturedScopes() []*Object {
	if f.Scopes == nil {
		return nil
	}
	return *f.Scopes
}

// SourceText returns the result of Function.prototype.toString for the function f.
func (f *Object) SourceText() wtf8.String {
	fn := f.Fn
	if fn.Code != nil && fn.Code.Source != "" {
		return fn.Code.Source
	}
	name := fn.NativeName()
	if fn.Code != nil {
		name = fn.Code.Name
	}
	// The name is a PropertyName after an optional accessor word, and is left out when it is not.
	n := strings.TrimPrefix(strings.TrimPrefix(string(name), "get "), "set ")
	if !parser.IsIdentifierName(n) && !strings.HasPrefix(n, "[") {
		name = ""
	}
	return "function " + name + "() { [native code] }"
}
