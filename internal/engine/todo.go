// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// This file holds the entry points of the features that are not implemented yet.
// Nothing creates the objects that would reach the proxy and typed array functions.

// Unsupported throws an Error that says that the feature what is not implemented. It does not return.
func (vm *VM) Unsupported(what string) {
	vm.throwError(ErrorKindError, "ekuma: "+what+" is not implemented yet")
}

func (vm *VM) directEval(src Value, scope int, regs []Value) Value {
	vm.Unsupported("direct eval")
	return Undefined
}

func (vm *VM) proxyGet(o *Object, k PropKey, receiver Value) Value {
	vm.Unsupported("Proxy")
	return Undefined
}

func (vm *VM) proxySet(o *Object, k PropKey, val, receiver Value) bool {
	vm.Unsupported("Proxy")
	return false
}

func (vm *VM) proxyHas(o *Object, k PropKey) bool {
	vm.Unsupported("Proxy")
	return false
}

func (vm *VM) proxyDelete(o *Object, k PropKey) bool {
	vm.Unsupported("Proxy")
	return false
}

func (vm *VM) proxyGetOwnProperty(o *Object, k PropKey) (PropDesc, bool) {
	vm.Unsupported("Proxy")
	return PropDesc{}, false
}

func (vm *VM) proxyDefineOwnProperty(o *Object, k PropKey, d PropDesc) bool {
	vm.Unsupported("Proxy")
	return false
}

func (vm *VM) proxyOwnKeys(o *Object) []PropKey {
	vm.Unsupported("Proxy")
	return nil
}

func (vm *VM) proxyGetPrototypeOf(o *Object) *Object {
	vm.Unsupported("Proxy")
	return nil
}

func (vm *VM) proxySetPrototypeOf(o *Object, proto *Object) bool {
	vm.Unsupported("Proxy")
	return false
}

func (vm *VM) proxyIsExtensible(o *Object) bool {
	vm.Unsupported("Proxy")
	return false
}

func (vm *VM) proxyPreventExtensions(o *Object) bool {
	vm.Unsupported("Proxy")
	return false
}

func (vm *VM) proxyIsConstructor(o *Object) bool {
	return false
}

func (vm *VM) typedArrayGet(o *Object, i uint32) Value {
	vm.Unsupported("typed arrays")
	return Undefined
}

func (vm *VM) typedArraySet(o *Object, i uint32, val Value) {
	vm.Unsupported("typed arrays")
}

func (vm *VM) typedArrayHas(o *Object, i uint32) bool {
	return false
}

func (vm *VM) typedArrayLength(o *Object) int {
	return 0
}

func (vm *VM) typedArrayDefine(o *Object, i uint32, d PropDesc) bool {
	vm.Unsupported("typed arrays")
	return false
}
