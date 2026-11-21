// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:generate go run gen.go

// Package wordbreak holds the Thai and Han word dictionaries from ICU4C.
// Their license notices are in internal/unicode/LICENSE.
package wordbreak

import _ "embed"

// The DEFLATE streams contain breadth-first tries. Each node has a little-endian
// uint16 label, a byte word cost (zero for a non-word), and a uint16 child count.
// The three fields occupy separate columns, with siblings ordered by label.

// Thai is the Thai word dictionary.
//
//go:embed thai.deflate
var Thai string

// Han is the Han word dictionary.
//
//go:embed han.deflate
var Han string
