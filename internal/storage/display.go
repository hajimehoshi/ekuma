// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"math/big"
	"strconv"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// maxDisplayString is the length in bytes up to which a message shows a string.
const maxDisplayString = 1 << 10

// DisplayString is agent.DisplayString.
func DisplayString(v Value) string {
	if ValidateExec {
		CheckValue(v)
	}
	o := v.O
	if o == nil {
		return "undefined"
	}
	if o == NumberObject {
		return string(numconv.NumberToString(v.N))
	}
	switch o.Kind {
	case KindNull:
		return "null"
	case KindFalse:
		return "false"
	case KindTrue:
		return "true"
	case KindString:
		return wtf8.ToValidUTF8(Clip(v.Str(), maxDisplayString, "..."))
	case KindSymbol:
		return "Symbol(" + wtf8.ToValidUTF8(o.Text()) + ")"
	case KindBigInt:
		return displayBigInt(v)
	case KindArray:
		return "[object Array]"
	}
	if o.Fn != nil {
		return "[object Function]"
	}
	return "[object Object]"
}

// maxDisplayBigIntBits is the number of bits up to which a message shows every digit of a BigInt.
const maxDisplayBigIntBits = 1 << 12

// displayBigInt describes the BigInt v for messages. A long one is shown as its leading
// hexadecimal digits and its number of bits, which take no division to find.
func displayBigInt(v Value) string {
	if i, ok := v.BigInt64(); ok {
		return strconv.FormatInt(i, 10)
	}
	x := v.BigInt()
	n := x.BitLen()
	if n <= maxDisplayBigIntBits {
		return string(numconv.FormatBigInt(x, 10, nil))
	}
	// The shift is a multiple of 4, so the digits are those of x.
	lead := new(big.Int).Rsh(new(big.Int).Abs(x), uint(n-64)&^3)
	sign := ""
	if x.Sign() < 0 {
		sign = "-"
	}
	return sign + "0x" + lead.Text(16) + "... (" + strconv.Itoa(n) + " bits)"
}

// Clip returns s, or its longest prefix that ends at a character boundary and leaves room for
// the suffix within max bytes, followed by the suffix.
func Clip[S ~string](s S, max int, suffix S) S {
	if len(s) <= max {
		return s
	}
	i := max - len(suffix)
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i] + suffix
}
