// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package json

// maxDepth is the nesting depth at which Stringify and [Revive] throw the RangeError of a stack
// overflow. It is lower than elsewhere, as the stack of WebAssembly in browsers is smaller.
const maxDepth = 2000
