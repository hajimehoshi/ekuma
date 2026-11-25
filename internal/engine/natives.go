// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// The native functions of the engine itself.
var (
	nativeFunctionPrototype      = RegisterNative(functionPrototype)
	nativeGoFunction             = RegisterNative(goFunction)
	nativePromiseRejectFunction  = RegisterNative(promiseRejectFunction)
	nativeCapabilityExecutor     = RegisterNative(capabilityExecutor)
	nativeAddInitializerFunction = RegisterNative(addInitializerFunction)
)

// The native functions whose code leads to the code that makes their function objects. A
// declaration cannot register them.
var (
	nativePromiseResolveFunction      storage.Native
	nativeAutoAccessorGetter          storage.Native
	nativeAutoAccessorSetter          storage.Native
	nativeDecoratorAccessGetter       storage.Native
	nativeDecoratorAccessSetter       storage.Native
	nativeDecoratorAccessHas          storage.Native
	nativeAsyncDisposeFunction        storage.Native
	nativeAsyncFromSyncIteratorNext   storage.Native
	nativeAsyncFromSyncIteratorReturn storage.Native
	nativeAsyncFromSyncIteratorThrow  storage.Native
)

func init() {
	nativePromiseResolveFunction = RegisterNative(promiseResolveFunction)
	nativeAutoAccessorGetter = RegisterNative(autoAccessorGetter)
	nativeAutoAccessorSetter = RegisterNative(autoAccessorSetter)
	nativeDecoratorAccessGetter = RegisterNative(decoratorAccessGetter)
	nativeDecoratorAccessSetter = RegisterNative(decoratorAccessSetter)
	nativeDecoratorAccessHas = RegisterNative(decoratorAccessHas)
	nativeAsyncDisposeFunction = RegisterNative(asyncDisposeFunction)
	nativeAsyncFromSyncIteratorNext = RegisterNative(asyncFromSyncIteratorNext)
	nativeAsyncFromSyncIteratorReturn = RegisterNative(asyncFromSyncIteratorReturn)
	nativeAsyncFromSyncIteratorThrow = RegisterNative(asyncFromSyncIteratorThrow)
	storage.ProxyInfo.Native = RegisterNative(proxyFunction)
}

// functionPrototype is the code of %Function.prototype%.
func functionPrototype(cx *ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return storage.Undefined
}
