// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// NewShadowRealmObject returns a ShadowRealm object with the prototype proto whose
// [[ShadowRealm]] is the realm of target.
func (cx *ExecutionContext) NewShadowRealmObject(proto *Object, target *ExecutionContext) *Object {
	o := cx.NewObject(proto)
	o.class = classShadowRealm
	o.setExtState(target.realm)
	return o
}

// ValidateShadowRealmObject implements ValidateShadowRealmObject, and returns the
// context of its shadow realm.
func (cx *ExecutionContext) ValidateShadowRealmObject(o Value) *ExecutionContext {
	if !o.IsObject() || o.o.class != classShadowRealm {
		cx.ThrowTypeError("the receiver is not a ShadowRealm")
	}
	return &o.o.extState().(*realm).context
}

// EvalInRealm evaluates the script src in the target realm as an indirect eval there would, as code
// without a ScriptOrModule. It throws a SyntaxError of the current realm when src is not a valid
// script. It reports false when the evaluation throws, and the result is the exception then.
func (cx *ExecutionContext) EvalInRealm(target *ExecutionContext, src wtf8.String) (Value, bool) {
	bc := cx.compileEval(nil, 0, src)
	var result, e Value
	var ok bool
	target.Run(func() {
		f := target.newScriptFunction(target.link(bc, target.realm.referrer))
		e, ok = target.Completion(func() {
			result = target.Call(f, Value{o: target.realm.globalThis})
		})
	})
	if !ok {
		return e, false
	}
	return result, true
}

// ImportInRealm loads specifier with the execution realm as the referrer and returns
// a promise of that realm for the module namespace.
func (cx *ExecutionContext) ImportInRealm(specifier wtf8.String) *Object {
	var p *Object
	cx.Run(func() {
		c := cx.newIntrinsicCapability()
		p = c.promise
		m, e, ok := cx.loadImportedModule(cx.realm.referrer, ModuleRequest{
			Specifier: specifier,
		})
		if !ok {
			cx.RejectCapability(c, e)
			return
		}
		cx.continueDynamicImport(c, m, bytecode.PhaseEvaluation)
	})
	return p
}
