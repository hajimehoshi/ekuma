// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// PromiseResolver resolves or rejects the promise that [Realm.NewPromise] returns with it, as
// the resolving functions of the promise do: once the promise is resolved, later calls of
// [PromiseResolver.Resolve] and [PromiseResolver.Reject] have no effect. A PromiseResolver
// must not be used while its VM is used on another goroutine.
type PromiseResolver struct {
	realm *Realm
	c     *engine.PromiseCapability
}

// NewPromise is [Realm.NewPromise] of the initial realm.
func (vm *VM) NewPromise() (Value, *PromiseResolver) {
	vm.initIfNeeded()
	return vm.initialRealm.NewPromise()
}

// Resolve resolves the promise with [VM.ValueOf](v), or with [Realm.ValueOf](v) of its realm for
// a promise of another realm. A thenable is adopted, and reading its then property can run
// JavaScript. The pending jobs run before Resolve returns, unless it is called from a host
// callback.
//
// If ctx is already done, Resolve does not attempt to settle the promise. If ctx stops
// JavaScript at a loop iteration or call, pending jobs are discarded. An interruption
// after resolution begins can leave the promise pending even though later calls to the
// resolver have no effect.
//
// If ctx stops an outermost call to Resolve, Resolve returns the cause of ctx. If ctx
// stops a call from a host callback, the interruption unwinds the callback and the
// outermost call reports the cause. Internal operation failures return an [*InternalError].
func (r *PromiseResolver) Resolve(ctx context.Context, v any) error {
	return r.settle(ctx, v, false)
}

// Reject is like [PromiseResolver.Resolve], but rejects the promise with v. A rejected promise
// without a handler is reported to the callback of [VM.SetUnhandledRejection] after the jobs ran,
// unless ctx stops them.
func (r *PromiseResolver) Reject(ctx context.Context, v any) error {
	return r.settle(ctx, v, true)
}

// settle implements [PromiseResolver.Resolve] and [PromiseResolver.Reject].
func (r *PromiseResolver) settle(ctx context.Context, x any, reject bool) error {
	vm := r.realm.vm
	_, err := vm.run(ctx, func() (storage.Value, error) {
		cx := r.realm.context()
		v := vm.valueOf(cx, x)
		if reject {
			return storage.Undefined, cx.HostReject(r.c, v)
		}
		return storage.Undefined, cx.HostResolve(r.c, v)
	})
	return err
}
