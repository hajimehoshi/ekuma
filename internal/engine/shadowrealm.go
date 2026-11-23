// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// NewShadowRealmObject returns a ShadowRealm object with the prototype proto whose
// [[ShadowRealm]] is r.
func (cx *ExecutionContext) NewShadowRealmObject(proto *Object, r *Realm) *Object {
	o := cx.NewObject(proto)
	o.class = classShadowRealm
	o.setExtState(r)
	return o
}

// ValidateShadowRealmObject implements ValidateShadowRealmObject, and returns the
// [[ShadowRealm]] of o.
func (cx *ExecutionContext) ValidateShadowRealmObject(o Value) *Realm {
	if !o.IsObject() || o.o.class != classShadowRealm {
		cx.ThrowTypeError("the receiver is not a ShadowRealm")
	}
	return o.o.extState().(*Realm)
}

// EvalInRealm evaluates the script src in the realm r as an indirect eval there would, as code
// without a ScriptOrModule. It throws a SyntaxError of the current realm when src is not a valid
// script. It reports false when the evaluation throws, and the result is the exception then.
func (cx *ExecutionContext) EvalInRealm(r *Realm, src wtf8.String) (Value, bool) {
	bc := cx.compileEval(nil, 0, src)
	var result, e Value
	var ok bool
	r.Context().Run(func() {
		cx := r.Context()
		f := cx.newScriptFunction(cx.link(bc, cx.realm.referrer))
		e, ok = cx.Completion(func() {
			result = cx.Call(f, Value{o: cx.realm.globalThis})
		})
	})
	if !ok {
		return e, false
	}
	return result, true
}

// ImportInRealm loads the module specifier with the realm r as the referrer, as import() in code
// of r without a ScriptOrModule does, and returns a promise of r for its module namespace.
func (cx *ExecutionContext) ImportInRealm(r *Realm, specifier wtf8.String) *Object {
	var p *Object
	r.Context().Run(func() {
		cx := r.Context()
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
