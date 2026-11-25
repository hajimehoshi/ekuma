// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// NewShadowRealmObject returns a ShadowRealm object with the prototype proto whose
// [[ShadowRealm]] is the realm of target.
func (cx *ExecutionContext) NewShadowRealmObject(proto *storage.Object, target *ExecutionContext) *storage.Object {
	return storage.NewObjectWithState(cx.ShapeFor(proto), storage.ClassShadowRealm, target.realm)
}

// ValidateShadowRealmObject implements ValidateShadowRealmObject, and returns the
// context of its shadow realm.
func (cx *ExecutionContext) ValidateShadowRealmObject(o storage.Value) *ExecutionContext {
	if !o.IsObject() || o.O.Class != storage.ClassShadowRealm {
		cx.ThrowTypeError("the receiver is not a ShadowRealm")
	}
	return &o.O.ExtState().(*realm).context
}

// EvalInRealm evaluates the script src in the target realm as an indirect eval there would, as code
// without a ScriptOrModule. It throws a SyntaxError of the current realm when src is not a valid
// script. It reports false when the evaluation throws, and the result is the exception then.
func (cx *ExecutionContext) EvalInRealm(target *ExecutionContext, src wtf8.String) (storage.Value, bool) {
	bc := cx.compileEval(nil, 0, src)
	var result, e storage.Value
	var ok bool
	target.Run(func() {
		f := target.newScriptFunction(target.link(bc, target.realm.referrer))
		e, ok = target.Completion(func() {
			result = target.Call(f, storage.Value{O: target.realm.GlobalThis})
		})
	})
	if !ok {
		return e, false
	}
	return result, true
}

// ImportInRealm loads specifier with the execution realm as the referrer and returns
// a promise of that realm for the module namespace.
func (cx *ExecutionContext) ImportInRealm(specifier wtf8.String) *storage.Object {
	var p *storage.Object
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
