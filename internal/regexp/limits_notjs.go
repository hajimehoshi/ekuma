// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package regexp

// maxDepth is the limit of the nesting depth of a pattern.
const maxDepth = 3000
