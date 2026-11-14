// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// InternedCount returns the number of entries in the table of interned strings of vm.
func InternedCount(vm *VM) int {
	return len(vm.strings.interned.entries)
}

// ObjectTransitionCount returns the number of transitions from the empty shape of the objects
// whose prototype is %Object.prototype%.
func ObjectTransitionCount(vm *VM) int {
	return len(vm.shapes.object.transitions.entries)
}
