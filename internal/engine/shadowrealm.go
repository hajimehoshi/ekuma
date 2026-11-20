// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// NewShadowRealmObject returns a ShadowRealm object with the prototype proto whose
// [[ShadowRealm]] is r.
func (vm *VM) NewShadowRealmObject(proto *Object, r *Realm) *Object {
	o := vm.NewObject(proto)
	o.class = classShadowRealm
	o.setExtState(r)
	return o
}

// ValidateShadowRealmObject implements ValidateShadowRealmObject, and returns the
// [[ShadowRealm]] of o.
func (vm *VM) ValidateShadowRealmObject(o Value) *Realm {
	if !o.IsObject() || o.o.class != classShadowRealm {
		vm.ThrowTypeError("the receiver is not a ShadowRealm")
	}
	return o.o.extState().(*Realm)
}

// GetFunctionRealm implements GetFunctionRealm.
func (vm *VM) GetFunctionRealm(f *Object) *Realm {
	return (*Realm)(vm.getFunctionRealm(f))
}

// EvalInRealm evaluates the script src in the realm r as an indirect eval there would, as code
// without a ScriptOrModule. It throws a SyntaxError of the current realm when src is not a valid
// script. It reports false when the evaluation throws, and the result is the exception then.
func (vm *VM) EvalInRealm(r *Realm, src wtf8.String) (Value, bool) {
	bc := vm.compileEval(nil, 0, src)
	var result, e Value
	var ok bool
	vm.InRealm(r, func() {
		f := vm.newScriptFunction(vm.link(bc, vm.referrer))
		e, ok = vm.Completion(func() {
			result = vm.Call(f, Value{o: vm.global})
		})
	})
	if !ok {
		return e, false
	}
	return result, true
}

// ImportInRealm loads the module specifier with the realm r as the referrer, as import() in code
// of r without a ScriptOrModule does, and returns a promise of r for its module namespace.
func (vm *VM) ImportInRealm(r *Realm, specifier wtf8.String) *Object {
	var p *Object
	vm.InRealm(r, func() {
		c := vm.newIntrinsicCapability()
		p = c.promise
		m, e, ok := vm.loadImportedModule(vm.referrer, ModuleRequest{
			Specifier: specifier,
		})
		if !ok {
			vm.RejectCapability(c, e)
			return
		}
		vm.continueDynamicImport(c, m, bytecode.PhaseEvaluation)
	})
	return p
}
