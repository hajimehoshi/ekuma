// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func initProxy(cx *engine.ExecutionContext) {
	ctor := cx.NewNativeConstructor("Proxy", 2, proxyConstructor)
	cx.Define(cx.GlobalObject().Object(), cx.Intern("Proxy"), engine.ObjectValue(ctor), engine.FlagsHidden)

	method(cx, ctor, "revocable", 2, proxyRevocable)
}

func proxyConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if cx.GetNewTarget() == nil {
		cx.ThrowTypeError("constructor Proxy requires 'new'")
	}
	return engine.ObjectValue(cx.ProxyCreate(arg(args, 0), arg(args, 1)))
}

func proxyRevocable(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	p := cx.ProxyCreate(arg(args, 0), arg(args, 1))
	revoke := cx.NewNativeWithState("", 0, proxyRevoke, &proxyRevocation{
		revocableProxy: p,
	})
	r := cx.NewObjectFromProperties(
		[]engine.PropKey{engine.NamedKey(cx.Intern("proxy")), engine.NamedKey(cx.Intern("revoke"))},
		[]engine.Value{engine.ObjectValue(p), engine.ObjectValue(revoke)},
	)
	return engine.ObjectValue(r)
}

// proxyRevocation is the state of a proxy revocation function.
type proxyRevocation struct {
	// revocableProxy is nil when the proxy is revoked.
	revocableProxy *engine.Object
}

// proxyRevoke is the code of the proxy revocation functions. Their state is a *proxyRevocation.
func proxyRevoke(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	r := cx.ActiveNativeState().(*proxyRevocation)
	if p := r.revocableProxy; p != nil {
		engine.RevokeProxy(p)
		r.revocableProxy = nil
	}
	return engine.Undefined
}
