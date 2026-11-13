// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math/big"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// funcProto is the compiled code of a function in the form the interpreter runs,
// shared by all closures made from it.
type funcProto struct {
	*bytecode.Func

	Name   string
	code   []bytecode.Instr
	consts []Value
	keys   []*Object
	caches []propCache
	protos []*funcProto

	templates []templateSite

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
}

type templateSite struct {
	cooked []Value
	raw    []Value
	object *Object
}

// link converts the compiled function f and the functions nested in it to their runtime form.
func (vm *VM) link(f *bytecode.Func) *funcProto {
	p := &funcProto{
		Func:               f,
		Name:               f.Name,
		code:               f.Code,
		consts:             make([]Value, len(f.Consts)),
		keys:               make([]*Object, len(f.Keys)),
		caches:             make([]propCache, f.NumCaches),
		protos:             make([]*funcProto, len(f.Protos)),
		templates:          make([]templateSite, len(f.Templates)),
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
		needsExtra:         f.NeedsExtra,
	}
	for i, c := range f.Consts {
		switch c.Kind {
		case bytecode.ConstNumber:
			p.consts[i] = Value{N: c.Num}
		case bytecode.ConstString:
			p.consts[i] = Value{O: vm.Intern(c.Str)}
		case bytecode.ConstBigInt:
			n, _ := new(big.Int).SetString(c.Str, 10)
			p.consts[i] = vm.newBigInt(n)
		}
	}
	for i, k := range f.Keys {
		p.keys[i] = vm.Intern(k)
	}
	for i, q := range f.Protos {
		p.protos[i] = vm.link(q)
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
	return p
}
