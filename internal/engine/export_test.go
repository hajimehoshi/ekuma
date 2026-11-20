// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// NewVMWithBigIntResultLimit returns a [VM] that allows up to maxBits bits in checked BigInt results.
func NewVMWithBigIntResultLimit(maxBits int64) *VM {
	vm := NewVM(nil)
	vm.maxBigIntResultBits = maxBits
	return vm
}

// InternedCount returns the number of entries in the table of interned strings of vm.
func InternedCount(vm *VM) int {
	return len(vm.strings.interned.entries)
}

// ObjectTransitionCount returns the number of transitions from the empty shape of the objects
// whose prototype is %Object.prototype%.
func ObjectTransitionCount(vm *VM) int {
	return vm.shapes.object.transitions.len()
}

// ElementStorage returns the number of values that the storage of the elements of the object
// v has room for, including those that removing its first elements left unused.
func ElementStorage(v Value) int {
	return v.o.elemsStorage()
}

// StackSize returns the number of slots of the stack of vm.
func StackSize(vm *VM) int {
	return len(vm.stack)
}

// JobStorage returns the number of jobs that the storage of the job queue of vm has room for.
func JobStorage(vm *VM) int {
	return cap(vm.jobs.jobs)
}
