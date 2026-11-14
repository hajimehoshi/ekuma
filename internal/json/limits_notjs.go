// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package json

// maxDepth is the nesting depth at which Stringify and Revive throw the RangeError of a stack overflow.
const maxDepth = 10000
