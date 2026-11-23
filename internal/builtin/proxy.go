// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func initProxy(cx *engine.ExecutionContext) {
	ctor := cx.NewNativeConstructor("Proxy", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		if cx.GetNewTarget() == nil {
			cx.ThrowTypeError("constructor Proxy requires 'new'")
		}
		return engine.ObjectValue(cx.ProxyCreate(arg(args, 0), arg(args, 1)))
	})
	cx.Define(cx.GlobalObject().Object(), cx.Intern("Proxy"), engine.ObjectValue(ctor), engine.FlagsHidden)

	method(cx, ctor, "revocable", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		p := cx.ProxyCreate(arg(args, 0), arg(args, 1))
		revocable := p
		revoke := cx.NewNative("", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			if revocable != nil {
				engine.RevokeProxy(revocable)
				revocable = nil
			}
			return engine.Undefined
		})
		r := cx.NewObjectFromProperties(
			[]engine.PropKey{engine.NamedKey(cx.Intern("proxy")), engine.NamedKey(cx.Intern("revoke"))},
			[]engine.Value{engine.ObjectValue(p), engine.ObjectValue(revoke)},
		)
		return engine.ObjectValue(r)
	})
}
