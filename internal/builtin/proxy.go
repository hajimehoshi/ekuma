// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

var (
	nativeProxyConstructor = engine.RegisterNative(proxyConstructor)
	nativeProxyRevocable   = engine.RegisterNative(proxyRevocable)
)

func initProxy(cx *engine.ExecutionContext) {
	ctor := cx.NewNativeConstructor("Proxy", 2, nativeProxyConstructor)
	cx.Define(cx.GlobalObject().Object(), cx.Intern("Proxy"), storage.ObjectValue(ctor), engine.FlagsHidden)

	method(cx, ctor, "revocable", 2, nativeProxyRevocable)
}

func proxyConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	if cx.GetNewTarget() == nil {
		cx.ThrowTypeError("constructor Proxy requires 'new'")
	}
	return storage.ObjectValue(cx.ProxyCreate(arg(args, 0), arg(args, 1)))
}

var nativeProxyRevoke = engine.RegisterNative(proxyRevoke)

func proxyRevocable(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	p := cx.ProxyCreate(arg(args, 0), arg(args, 1))
	revoke := cx.NewNativeWithState("", 0, nativeProxyRevoke, &proxyRevocation{
		revocableProxy: p,
	})
	r := cx.NewObjectFromProperties(
		[]engine.PropKey{storage.NamedKey(cx.Intern("proxy")), storage.NamedKey(cx.Intern("revoke"))},
		[]engine.Value{storage.ObjectValue(p), storage.ObjectValue(revoke)},
	)
	return storage.ObjectValue(r)
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
	return storage.Undefined
}
