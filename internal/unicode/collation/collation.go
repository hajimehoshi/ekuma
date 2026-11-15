// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:generate go run gen.go

// Package collation holds the collation data of CLDR: the CLDR root collation and the
// tailorings of the locales, generated from the XML release of CLDR.
//
// The tables are under the Unicode License v3, the license of the CLDR data, which is in the
// file LICENSE of this directory. That license covers this package only.
//
// A collation element (CE) of the root is a uint32: the primary weight in bits 16 to 31,
// the secondary weight in bits 7 to 15, the tertiary weight in bits 2 to 6, and a tag in
// bits 0 and 1. Tag 0 is a single CE, and the value 0 means that the root has no entry.
// Tag 1 is an expansion: bits 10 to 31 are the index of its first CE in Expansions, and
// bits 2 to 9 the number of CEs. Tag 2 is a contraction: bits 2 to 31 are the index of
// its entry in Contractions. Tag 3 is a CE whose weights are all zero.
//
// A tailored CE has wider weights: a 31-bit primary whose upper 16 bits are a root primary
// weight and whose lower 15 bits order tailored weights after it, a 20-bit secondary (a
// root secondary weight shifted left by 11), an 11-bit tertiary (a root tertiary weight
// shifted left by 6) and a 2-bit case (0 lower, 1 mixed, 2 upper).
package collation
