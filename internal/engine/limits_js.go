// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// maxDepth is the number of nested interpreter loops at which a RangeError is thrown.
// It is lower than elsewhere, as the stack of WebAssembly in browsers is smaller.
const maxDepth = 300

// maxNatives is the number of nested calls of native functions at which a RangeError is thrown.
// It is lower than elsewhere, as the stack of WebAssembly in browsers is smaller.
const maxNatives = 1000

// MaxBufferLength is the largest length of an ArrayBuffer in bytes.
// The memory of WebAssembly is limited to 4 GiB.
const MaxBufferLength = 1 << 30
