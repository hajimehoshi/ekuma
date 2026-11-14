// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// maxDepth is the number of nested interpreter loops at which a RangeError is thrown.
// It is lower than elsewhere, as the stack of WebAssembly in browsers is smaller.
const maxDepth = 300
