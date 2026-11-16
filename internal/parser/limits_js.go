// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser

// maxDepth is the limit of the nesting depth of the syntax.
// It is lower than elsewhere, as the stack of WebAssembly in browsers is smaller.
const maxDepth = 400

// maxLinks is the limit of the total length of the chains being parsed, such as a+b+c.
const maxLinks = 1000
