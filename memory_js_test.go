// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import "runtime/debug"

func init() {
	// The memory of js/wasm ends at 4 GiB, which the garbage collector does not take into account.
	// After a collection that finds much of it live, as the tests of strings of the maximum length
	// do, the heap would reach the end before the next collection. A limit of half the memory
	// leaves room for the allocations of those strings, of 1 GiB each.
	debug.SetMemoryLimit(2 << 30)
}
