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
	}
	for i, c := range f.Consts {
		switch c.Kind {
		case bytecode.ConstNumber:
			p.consts[i] = Value{n: c.Num}
		case bytecode.ConstString:
			p.consts[i] = Value{o: vm.Intern(c.Str)}
		case bytecode.ConstBigInt:
			n, _ := numconv.ParseBigInt(c.Str, 10, nil)
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
			} else {
				site.cooked[j] = Undefined
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

// prepare allocates the inline caches of p, which a frame of p needs.
func (p *funcProto) prepare() {
	if p.caches == nil {
		p.caches = make([]propCache, p.NumCaches)
	}
}
