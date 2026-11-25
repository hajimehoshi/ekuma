// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// scriptOrModuleOf returns the [[ScriptOrModule]] of the functions.
func scriptOrModuleOf(p *storage.FuncProto) *scriptOrModule {
	return p.Script.(*scriptOrModule)
}

// link converts the compiled function f and the functions nested in it to their runtime form.
// script is their [[ScriptOrModule]]. The constants and keys of f are no longer available
// afterwards.
func (cx *ExecutionContext) link(f *bytecode.Func, script *scriptOrModule) *storage.FuncProto {
	return cx.linkFunc(f, script, true)
}

// linkShared is like [ExecutionContext.link], but leaves f unchanged, so that any number of agents can link f,
// also at the same time.
func (cx *ExecutionContext) linkShared(f *bytecode.Func, script *scriptOrModule) *storage.FuncProto {
	return cx.linkFunc(f, script, false)
}

// linkFunc implements [ExecutionContext.link] and [ExecutionContext.linkShared]. consume says whether the constants and keys
// of f are dropped.
func (cx *ExecutionContext) linkFunc(f *bytecode.Func, script *scriptOrModule, consume bool) *storage.FuncProto {
	legacy := !f.Strict && !f.IsArrow && !f.IsMethod && !f.IsGenerator && !f.IsAsync && !f.IsClassConstructor
	p := &storage.FuncProto{
		Func:               f,
		Name:               f.Name,
		Code:               f.Code,
		Consts:             make([]storage.Value, len(f.Consts)),
		Keys:               make([]*storage.Object, len(f.Keys)),
		Protos:             make([]*storage.FuncProto, len(f.Protos)),
		Templates:          make([]storage.TemplateSite, len(f.Templates)),
		RegExps:            make([]storage.RegExpSite, len(f.RegExps)),
		Length:             f.Length,
		Nparams:            f.NumParams,
		Nclear:             f.NumClear,
		Nregs:              f.NumRegs,
		Strict:             f.Strict,
		IsGenerator:        f.IsGenerator,
		IsAsync:            f.IsAsync,
		IsConstructor:      f.IsConstructor,
		IsClassConstructor: f.IsClassConstructor,
		IsDerived:          f.IsDerived,

		// The arguments property of a non-strict function shows all the arguments of its calls.
		NeedsExtra:            f.NeedsExtra || legacy,
		LegacyCallerArguments: legacy,

		Script: script,
		Realm:  cx.realmPart,

		ThisReg:  -1,
		HeapRegs: f.IsGenerator || f.IsAsync,
	}
	if len(f.Code) > 0 {
		switch i := f.Code[0]; i.Op {
		case bytecode.OpLoadThis:
			p.ThisReg = i.A
		case bytecode.OpLoadThisSloppy:
			p.ThisReg, p.ThisSloppy = i.A, true
		}
	}
	p.ClearFrom = f.NumParams
	for _, i := range f.Code {
		switch i.Op {
		case bytecode.OpLoadThis, bytecode.OpLoadThisSloppy, bytecode.OpLoadNewTarget, bytecode.OpLoadHome, bytecode.OpLoadCallee, bytecode.OpLoadTDZ:
			if int(i.A) == p.ClearFrom {
				p.ClearFrom++
				continue
			}
		}
		break
	}
	for i, c := range f.Consts {
		switch c.Kind {
		case bytecode.ConstUndefined:
			p.Consts[i] = storage.Undefined
		case bytecode.ConstNull:
			p.Consts[i] = storage.Null
		case bytecode.ConstFalse:
			p.Consts[i] = storage.False
		case bytecode.ConstTrue:
			p.Consts[i] = storage.True
		case bytecode.ConstNumber:
			p.Consts[i] = storage.Number(c.Num)
		case bytecode.ConstString:
			p.Consts[i] = storage.ObjectValue(cx.Intern(c.Str))
		case bytecode.ConstBigInt:
			// A script may be linked when no JavaScript runs, which an interruption
			// cannot stop.
			var poll func()
			if cx.Running() {
				poll = cx.Poll
			}
			n, _ := numconv.ParseBigInt(string(c.Str), 10, poll)
			p.Consts[i] = storage.NewBigInt(n)
		}
	}
	for i, k := range f.Keys {
		p.Keys[i] = cx.Intern(k)
	}
	for _, d := range f.Upvals {
		if d.ByValue {
			p.Ncopied++
		}
	}
	for i, q := range f.Protos {
		p.Protos[i] = cx.linkFunc(q, script, consume)
	}
	for i, r := range f.RegExps {
		p.RegExps[i].Literal = r
	}
	for i, t := range f.Templates {
		site := storage.TemplateSite{
			Cooked: make([]storage.Value, len(t.Cooked)),
			Raw:    make([]storage.Value, len(t.Raw)),
		}
		for j, s := range t.Cooked {
			if t.HasCooked[j] {
				site.Cooked[j] = cx.NewString(s)
			}
		}
		for j, s := range t.Raw {
			site.Raw[j] = cx.NewString(s)
		}
		p.Templates[i] = site
	}
	if consume {
		// The runtime form replaces them.
		f.Consts = nil
		f.Keys = nil
	}
	return p
}
