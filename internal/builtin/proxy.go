// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func initProxy(vm *engine.VM) {
	ctor := vm.NewNativeConstructor("Proxy", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		if vm.GetNewTarget() == nil {
			vm.ThrowTypeError("constructor Proxy requires 'new'")
		}
		return engine.ObjectValue(vm.ProxyCreate(arg(args, 0), arg(args, 1)))
	})
	vm.Define(vm.GlobalObject().Object(), vm.Intern("Proxy"), engine.ObjectValue(ctor), engine.FlagsHidden)

	method(vm, ctor, "revocable", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		p := vm.ProxyCreate(arg(args, 0), arg(args, 1))
		revocable := p
		revoke := vm.NewNative("", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			if revocable != nil {
				engine.RevokeProxy(revocable)
				revocable = nil
			}
			return engine.Undefined
		})
		r := vm.NewObjectFromProperties(
			[]engine.PropKey{engine.NamedKey(vm.Intern("proxy")), engine.NamedKey(vm.Intern("revoke"))},
			[]engine.Value{engine.ObjectValue(p), engine.ObjectValue(revoke)},
		)
		return engine.ObjectValue(r)
	})
}
