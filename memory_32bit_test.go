// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"runtime/debug"
	"strconv"
)

func init() {
	if strconv.IntSize != 32 {
		return
	}
	// The address space of a 32-bit process is as small as 2 GiB, which the garbage collector does
	// not take into account. All that is allocated during a collection counts as live after it. When
	// conformance tests that need hundreds of MiB run at the same time, the heap would reach the end
	// of the address space before the next collection. A limit of a quarter of it leaves room for
	// what they allocate during a collection.
	debug.SetMemoryLimit(512 << 20)
}
