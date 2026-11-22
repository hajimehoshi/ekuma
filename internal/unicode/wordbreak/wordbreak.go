// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:generate go run gen.go

// Package wordbreak holds word dictionaries from ICU4C.
// The NOTICE files alongside the dictionaries contain their third-party notices.
package wordbreak

import _ "embed"

// The DEFLATE streams contain breadth-first tries. Each node has a little-endian
// uint16 label, a byte word cost (zero for a non-word), and a uint16 child count.
// The three fields occupy separate columns, with siblings ordered by label.

// Thai is the Thai word dictionary.
//
//go:embed thai.deflate
var Thai string

// Khmer is the Khmer word dictionary.
//
//go:embed khmer.deflate
var Khmer string

// Burmese is the Burmese word dictionary.
//
//go:embed burmese.deflate
var Burmese string

// CJ is the Chinese/Japanese word dictionary.
//
//go:embed cj.deflate
var CJ string
