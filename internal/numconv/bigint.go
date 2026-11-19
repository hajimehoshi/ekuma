// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv

import (
	"math/big"
	"math/bits"
	"strings"
)

// parseLeafDigits is the number of digits up to which ParseBigInt converts digits in one step.
const parseLeafDigits = 1 << 13

// formatLeafBits is the size in bits up to which FormatBigInt converts an integer in one step.
const formatLeafBits = 1 << 15

// ParseBigInt returns the integer that the digits s denote in the radix, which is from 2 to 36.
// It reports false when s is empty or has a character other than such a digit. poll, which may
// be nil, is called between the steps of a long conversion and may stop it by panicking.
func ParseBigInt(s string, radix int, poll func()) (*big.Int, bool) {
	if !validDigits(s, radix) {
		return nil, false
	}
	if len(s) <= parseLeafDigits || bits.OnesCount(uint(radix)) == 1 {
		return new(big.Int).SetString(s, radix)
	}
	// pows[k] is radix to the power parseLeafDigits<<k.
	var pows []*big.Int
	return parseBigDigits(s, radix, &pows, poll), true
}

// validDigits reports whether s is a non-empty string of the digits of the radix.
func validDigits(s string, radix int) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if DigitValue(s[i]) >= radix {
			return false
		}
	}
	return true
}

// parseBigDigits returns the integer that the valid digits s denote in the radix.
func parseBigDigits(s string, radix int, pows *[]*big.Int, poll func()) *big.Int {
	if len(s) <= parseLeafDigits {
		z, _ := new(big.Int).SetString(s, radix)
		return z
	}
	k := 0
	for parseLeafDigits<<(k+1) < len(s) {
		k++
	}
	m := len(s) - parseLeafDigits<<k
	hi := parseBigDigits(s[:m], radix, pows, poll)
	lo := parseBigDigits(s[m:], radix, pows, poll)
	p := radixPower(radix, parseLeafDigits, k, pows, poll)
	if poll != nil {
		poll()
	}
	hi = MulBigInt(hi, p, poll)
	return hi.Add(hi, lo)
}

// radixPower returns radix to the power digits<<k, extending the table pows of such powers.
func radixPower(radix, digits, k int, pows *[]*big.Int, poll func()) *big.Int {
	for len(*pows) <= k {
		if poll != nil {
			poll()
		}
		var p *big.Int
		if len(*pows) == 0 {
			p = new(big.Int).Exp(big.NewInt(int64(radix)), big.NewInt(int64(digits)), nil)
		} else {
			q := (*pows)[len(*pows)-1]
			p = MulBigInt(q, q, poll)
		}
		*pows = append(*pows, p)
	}
	return (*pows)[k]
}

// FormatBigInt returns the digits of x in the radix, which is from 2 to 36, as
// [big.Int.Text] does. poll, which may be nil, is called between the steps of a long conversion
// and may stop it by panicking.
func FormatBigInt(x *big.Int, radix int, poll func()) string {
	if x.BitLen() <= formatLeafBits || bits.OnesCount(uint(radix)) == 1 {
		return x.Text(radix)
	}
	var sb strings.Builder
	if x.Sign() < 0 {
		sb.WriteByte('-')
	}
	// radix to the power digits has at most formatLeafBits bits, so that a remainder of a
	// division by it is a leaf.
	digits := formatLeafBits / bits.Len(uint(radix-1))
	var pows, recips []*big.Int
	formatBigDigits(&sb, new(big.Int).Abs(x), radix, digits, 0, &pows, &recips, poll)
	return sb.String()
}

// formatBigDigits writes the digits of the non-negative x in the radix to sb, padded with zeros
// to width digits. pows[k] is radix to the power digits<<k, and recips[k], when it is not nil,
// its reciprocal.
func formatBigDigits(sb *strings.Builder, x *big.Int, radix, digits, width int, pows, recips *[]*big.Int, poll func()) {
	if x.BitLen() <= formatLeafBits {
		s := x.Text(radix)
		if x.Sign() == 0 {
			s = ""
		}
		for range width - len(s) {
			sb.WriteByte('0')
		}
		sb.WriteString(s)
		return
	}
	// x is split at a power with a quarter to a half of its bits, so that the parts have about
	// the same size. The next power has about twice the bits of a power.
	k := 0
	p := radixPower(radix, digits, 0, pows, poll)
	for 4*p.BitLen() <= x.BitLen()+2 {
		k++
		p = radixPower(radix, digits, k, pows, poll)
	}
	if poll != nil {
		poll()
	}
	var hi, lo *big.Int
	if len(x.Bits()) <= 2*arithLeafWords {
		hi, lo = new(big.Int).QuoRem(x, p, new(big.Int))
	} else {
		for len(*recips) <= k {
			*recips = append(*recips, nil)
		}
		if (*recips)[k] == nil && len(p.Bits()) > arithLeafWords {
			(*recips)[k] = reciprocal(p, poll)
		}
		hi, lo = quoRemAbs(x, p, (*recips)[k], poll)
	}
	m := digits << k
	if width > 0 {
		width -= m
	}
	formatBigDigits(sb, hi, radix, digits, width, pows, recips, poll)
	formatBigDigits(sb, lo, radix, digits, m, pows, recips, poll)
}
