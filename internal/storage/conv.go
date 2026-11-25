// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"strconv"
)

type CmpResult uint8

const (
	CmpLess CmpResult = iota
	CmpEqual
	CmpGreater
	CmpUndefined
)

func CompareNumbers(x, y float64) CmpResult {
	switch {
	case x < y:
		return CmpLess
	case x > y:
		return CmpGreater
	case x == y:
		return CmpEqual
	}
	return CmpUndefined
}

// MaxStringLength is the largest length a string may have, in bytes.
// It is smaller where an int has 32 bits.
const MaxStringLength = 1<<(27+3*(strconv.IntSize/64)) - 1
