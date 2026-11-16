// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package regexp

// maxDepth is the limit of the nesting depth of a pattern.
// It is lower than elsewhere, as the stack of WebAssembly in browsers is smaller.
const maxDepth = 400
