// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma

import (
	"context"

	"github.com/hajimehoshi/ekuma/internal/engine"
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
// When ctx is done, the JavaScript stops at its next loop iteration or call, the pending jobs are
// discarded, and the error is the cause of ctx. The promise stays pending if it was not settled
// before the stop, and later calls still have no effect. A call whose ctx is already done only
// returns the cause. Internal operation failures return an [*InternalError].
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
	_, err := vm.run(ctx, func() (engine.Value, error) {
		var err error
		r.realm.in(func() {
			v := vm.valueOf(r.realm, x)
			if reject {
				err = vm.e.HostReject(r.c, v)
				return
			}
			err = vm.e.HostResolve(r.c, v)
		})
		return engine.Undefined, err
	})
	return err
}
