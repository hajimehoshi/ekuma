// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// funcProto is the compiled code of a function in the form the interpreter runs,
// shared by all closures made from it.
type funcProto struct {
	*bytecode.Func

	name   string
	code   []bytecode.Instr
	consts []Value
	keys   []*Object

	// caches holds the inline caches, and is nil until a frame of the function is pushed.
	caches []propCache
	protos []*funcProto

	templates []templateSite
	regExps   []regExpSite

	// ncopied is the number of the upvalues that are copies of the variables, by
	// [bytecode.UpvalDesc.ByValue].
	ncopied int

	length  int
	nparams int
	nclear  int
	nregs   int

	// clearFrom is the first register from nparams on that a new frame clears. The first
	// instructions write the registers between nparams and clearFrom before anything reads them.
	clearFrom int

	strict             bool
	isGenerator        bool
	isAsync            bool
	isConstructor      bool
	isClassConstructor bool
	isDerived          bool
	needsExtra         bool

	// legacyCallerArguments says whether the functions have the own properties caller and arguments
	// that implementations add to non-strict ordinary functions.
	legacyCallerArguments bool

	// script is the [[ScriptOrModule]] of the functions.
	script *scriptOrModule

	// realm is the [[Realm]] of the functions.
	realm *realm

	// thisReg is the register that the first instruction loads this into, or -1 when the
	// first instruction does not load this. thisSloppy says whether it converts this the way
	// a non-strict function sees it.
	thisReg    int32
	thisSloppy bool

	// heapRegs says whether the frames of the functions keep their registers in their
	// generator instead of on the stack.
	heapRegs bool

	// inlineFrames says whether the fast paths of the interpreter loop may push frames of the
	// functions: the inline caches exist, and the functions are neither generators, async
	// functions nor class constructors.
	inlineFrames bool
}

type templateSite struct {
	cooked []Value
	raw    []Value
	object *Object
}

// link converts the compiled function f and the functions nested in it to their runtime form.
// script is their [[ScriptOrModule]]. The constants and keys of f are no longer available
// afterwards.
func (vm *VM) link(f *bytecode.Func, script *scriptOrModule) *funcProto {
	return vm.linkFunc(f, script, true)
}

// linkShared is like [VM.link], but leaves f unchanged, so that any number of VMs can link f,
// also at the same time.
func (vm *VM) linkShared(f *bytecode.Func, script *scriptOrModule) *funcProto {
	return vm.linkFunc(f, script, false)
}

// linkFunc implements [VM.link] and [VM.linkShared]. consume says whether the constants and keys
// of f are dropped.
func (vm *VM) linkFunc(f *bytecode.Func, script *scriptOrModule, consume bool) *funcProto {
	legacy := !f.Strict && !f.IsArrow && !f.IsMethod && !f.IsGenerator && !f.IsAsync && !f.IsClassConstructor
	p := &funcProto{
		Func:               f,
		name:               f.Name,
		code:               f.Code,
		consts:             make([]Value, len(f.Consts)),
		keys:               make([]*Object, len(f.Keys)),
		protos:             make([]*funcProto, len(f.Protos)),
		templates:          make([]templateSite, len(f.Templates)),
		regExps:            make([]regExpSite, len(f.RegExps)),
		length:             f.Length,
		nparams:            f.NumParams,
		nclear:             f.NumClear,
		nregs:              f.NumRegs,
		strict:             f.Strict,
		isGenerator:        f.IsGenerator,
		isAsync:            f.IsAsync,
		isConstructor:      f.IsConstructor,
		isClassConstructor: f.IsClassConstructor,
		isDerived:          f.IsDerived,

		// The arguments property of a non-strict function shows all the arguments of its calls.
		needsExtra:            f.NeedsExtra || legacy,
		legacyCallerArguments: legacy,

		script: script,
		realm:  vm.curRealm,

		thisReg:  -1,
		heapRegs: f.IsGenerator || f.IsAsync,
	}
	if len(f.Code) > 0 {
		switch i := f.Code[0]; i.Op {
		case bytecode.OpLoadThis:
			p.thisReg = i.A
		case bytecode.OpLoadThisSloppy:
			p.thisReg, p.thisSloppy = i.A, true
		}
	}
	p.clearFrom = f.NumParams
	for _, i := range f.Code {
		switch i.Op {
		case bytecode.OpLoadThis, bytecode.OpLoadThisSloppy, bytecode.OpLoadNewTarget, bytecode.OpLoadHome, bytecode.OpLoadCallee, bytecode.OpLoadTDZ:
			if int(i.A) == p.clearFrom {
				p.clearFrom++
				continue
			}
		}
		break
	}
	for i, c := range f.Consts {
		switch c.Kind {
		case bytecode.ConstNumber:
			p.consts[i] = Number(c.Num)
		case bytecode.ConstString:
			p.consts[i] = ObjectValue(vm.Intern(c.Str))
		case bytecode.ConstBigInt:
			// A script may be linked when no JavaScript runs, which an interruption
			// cannot stop.
			var poll func()
			if vm.Running() {
				poll = vm.Poll
			}
			n, _ := numconv.ParseBigInt(c.Str, 10, poll)
			p.consts[i] = vm.NewBigInt(n)
		}
	}
	for i, k := range f.Keys {
		p.keys[i] = vm.Intern(k)
	}
	for _, d := range f.Upvals {
		if d.ByValue {
			p.ncopied++
		}
	}
	for i, q := range f.Protos {
		p.protos[i] = vm.linkFunc(q, script, consume)
	}
	for i, r := range f.RegExps {
		p.regExps[i].literal = r
	}
	for i, t := range f.Templates {
		site := templateSite{
			cooked: make([]Value, len(t.Cooked)),
			raw:    make([]Value, len(t.Raw)),
		}
		for j, s := range t.Cooked {
			if t.HasCooked[j] {
				site.cooked[j] = vm.NewString(s)
			}
		}
		for j, s := range t.Raw {
			site.raw[j] = vm.NewString(s)
		}
		p.templates[i] = site
	}
	if consume {
		// The runtime form replaces them.
		f.Consts = nil
		f.Keys = nil
	}
	return p
}

// firstCleared returns the first register that a new frame of p for argc arguments clears.
func (p *funcProto) firstCleared(argc int) int {
	if argc < p.nparams {
		return argc
	}
	return p.clearFrom
}

// prepare allocates the inline caches of p, which a frame of p needs.
func (p *funcProto) prepare() {
	if p.caches == nil {
		p.caches = make([]propCache, p.NumCaches)
		p.inlineFrames = !p.heapRegs && !p.isClassConstructor
	}
}
