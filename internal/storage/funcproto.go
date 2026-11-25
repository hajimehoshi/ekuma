// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// FuncProto is the compiled code of a function in the form the interpreter runs,
// shared by all closures made from it.
type FuncProto struct {
	*bytecode.Func

	Name   wtf8.String
	Code   []bytecode.Instr
	Consts []Value
	Keys   []*Object

	// Caches holds the inline Caches, and is nil until a frame of the function is pushed.
	Caches []PropCache
	Protos []*FuncProto

	Templates []TemplateSite
	RegExps   []RegExpSite

	// Ncopied is the number of the upvalues that are copies of the variables, by
	// [bytecode.UpvalDesc.ByValue].
	Ncopied int

	Length  int
	Nparams int
	Nclear  int
	Nregs   int

	// ClearFrom is the first register from nparams on that a new frame clears. The first
	// instructions write the registers between nparams and ClearFrom before anything reads them.
	ClearFrom int

	Strict             bool
	IsGenerator        bool
	IsAsync            bool
	IsConstructor      bool
	IsClassConstructor bool
	IsDerived          bool
	NeedsExtra         bool

	// LegacyCallerArguments says whether the functions have the own properties caller and arguments
	// that implementations add to non-strict ordinary functions.
	LegacyCallerArguments bool

	// Script is the [[ScriptOrModule]] of the functions. Only scriptOrModuleOf reads it.
	Script any

	// Realm is the [[Realm]] of the functions. Each frame enters the context of the Realm of its
	// linked code, including a replacement tail frame.
	Realm *RealmPart

	// ThisReg is the register that the first instruction loads this into, or -1 when the
	// first instruction does not load this. ThisSloppy says whether it converts this the way
	// a non-strict function sees it.
	ThisReg    int32
	ThisSloppy bool

	// HeapRegs says whether the frames of the functions keep their registers in their
	// generator instead of on the stack.
	HeapRegs bool

	// InlineFrames says whether the fast paths of the interpreter loop may push frames of the
	// functions: the inline caches exist, and the functions are neither generators, async
	// functions nor class constructors.
	InlineFrames bool

	// ReturnedProperty is the cache for x when the function starts with return this.x.
	ReturnedProperty *PropCache

	// Globals holds the inline caches of global variables, which prepare allocates with
	// caches. It comes after the fields that calls read.
	Globals []GlobalCache

	// plainFunction is the initial state of ordinary closures without upvalues.
	plainFunction *Function
}

type TemplateSite struct {
	Cooked []Value
	Raw    []Value
	Object *Object
}

// FirstCleared returns the first register that a new frame of p for argc arguments clears.
func (p *FuncProto) FirstCleared(argc int) int {
	if argc < p.Nparams {
		return argc
	}
	return p.ClearFrom
}

// Prepare allocates the inline caches of p, which a frame of p needs.
func (p *FuncProto) Prepare() {
	if p.Caches == nil {
		p.Caches = make([]PropCache, p.NumCaches)
		p.Globals = make([]GlobalCache, p.NumGlobalCaches)
		p.InlineFrames = !p.HeapRegs && !p.IsClassConstructor
		if p.InlineFrames && p.ThisReg >= 0 && len(p.Code) >= 3 {
			get, ret := p.Code[1], p.Code[2]
			if get.Op == bytecode.OpGetProp && get.B == p.ThisReg && ret.Op == bytecode.OpReturn && ret.A == get.A {
				p.ReturnedProperty = &p.Caches[get.C]
			}
		}
	}
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
