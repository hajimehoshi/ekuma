// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package parser

// maxDepth is the limit of the nesting depth of the syntax.
const maxDepth = 3000

// maxLinks is the limit of the total length of the chains being parsed, such as a+b+c.
const maxLinks = 1 << 17
