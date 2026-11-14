// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// This file holds the entry points of the features that are not implemented yet.
// Nothing creates the objects that would reach the proxy functions.

// unsupported throws an Error that says that the feature what is not implemented. It does not return.
func (vm *VM) unsupported(what string) {
	vm.throwError(ErrorKindError, "ekuma: "+what+" is not implemented yet")
}

func (vm *VM) proxyGet(o *Object, k PropKey, receiver Value) Value {
	vm.unsupported("Proxy")
	return Undefined
}

func (vm *VM) proxySet(o *Object, k PropKey, val, receiver Value) bool {
	vm.unsupported("Proxy")
	return false
}

func (vm *VM) proxyHas(o *Object, k PropKey) bool {
	vm.unsupported("Proxy")
	return false
}

func (vm *VM) proxyDelete(o *Object, k PropKey) bool {
	vm.unsupported("Proxy")
	return false
}

func (vm *VM) proxyGetOwnProperty(o *Object, k PropKey) (PropDesc, bool) {
	vm.unsupported("Proxy")
	return PropDesc{}, false
}

func (vm *VM) proxyDefineOwnProperty(o *Object, k PropKey, d PropDesc) bool {
	vm.unsupported("Proxy")
	return false
}

func (vm *VM) proxyOwnKeys(o *Object) []PropKey {
	vm.unsupported("Proxy")
	return nil
}

func (vm *VM) proxyGetPrototypeOf(o *Object) *Object {
	vm.unsupported("Proxy")
	return nil
}

func (vm *VM) proxySetPrototypeOf(o *Object, proto *Object) bool {
	vm.unsupported("Proxy")
	return false
}

func (vm *VM) proxyIsExtensible(o *Object) bool {
	vm.unsupported("Proxy")
	return false
}

func (vm *VM) proxyPreventExtensions(o *Object) bool {
	vm.unsupported("Proxy")
	return false
}

func (vm *VM) proxyIsConstructor(o *Object) bool {
	return false
}
