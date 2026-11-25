// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// Native identifies a registered native function. The zero value identifies none.
type Native struct {
	Index uint16

	// FastIndex is the index of what callers use instead of calling the function, or 0.
	FastIndex uint8
}

// HasFast reports whether fast returns something.
func (n Native) HasFast() bool {
	// Widening the index as nativeFastOf does lets the compiler share its register.
	return uint(n.FastIndex) != 0
}
