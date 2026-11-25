// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

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
	nativePromiseResolveFunction      Native
	nativeAutoAccessorGetter          Native
	nativeAutoAccessorSetter          Native
	nativeDecoratorAccessGetter       Native
	nativeDecoratorAccessSetter       Native
	nativeDecoratorAccessHas          Native
	nativeAsyncDisposeFunction        Native
	nativeAsyncFromSyncIteratorNext   Native
	nativeAsyncFromSyncIteratorReturn Native
	nativeAsyncFromSyncIteratorThrow  Native
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
	proxyInfo.native = RegisterNative(proxyFunction)
}

// functionPrototype is the code of %Function.prototype%.
func functionPrototype(cx *ExecutionContext, this Value, args []Value) Value {
	return Undefined
}
