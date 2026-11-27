// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"math/big"
	"math/bits"
	"slices"
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// MaxBigIntBits is the largest number of bits of the magnitude of a BigInt.
const MaxBigIntBits = numconv.MaxBigIntBits

// checkBigIntBits throws a RangeError when a BigInt result of n bits exceeds the limit.
func (cx *ExecutionContext) checkBigIntBits(n int64) {
	if n > cx.MaxBigIntResultBits {
		cx.ThrowRangeError("maximum BigInt size exceeded")
	}
}

const bigIntInlineWords = storage.BigIntInlineWords

func bigIntMagnitude(v storage.Value, buf *[64 / bits.UintSize]big.Word) (words []big.Word, negative bool) {
	if i, ok := v.BigInt64(); ok {
		if i == 0 {
			return nil, false
		}
		magnitude := uint64(i)
		if i < 0 {
			magnitude = -magnitude
		}
		buf[0] = big.Word(magnitude)
		if bits.UintSize == 32 && magnitude>>32 != 0 {
			buf[len(buf)-1] = big.Word(magnitude >> 32)
			return buf[:], i < 0
		}
		return buf[:1], i < 0
	}
	x := v.BigInt()
	return x.Bits(), x.Sign() < 0
}

func wordBitLen(words []big.Word) int64 {
	if len(words) == 0 {
		return 0
	}
	return int64(len(words)-1)*bits.UintSize + int64(bits.Len(uint(words[len(words)-1])))
}

func addWords(dst, x, y []big.Word) int {
	var carry uint
	for i, word := range y {
		sum, c := bits.Add(uint(x[i]), uint(word), carry)
		dst[i], carry = big.Word(sum), c
	}
	for i := len(y); i < len(x); i++ {
		sum, c := bits.Add(uint(x[i]), 0, carry)
		dst[i], carry = big.Word(sum), c
	}
	if carry != 0 {
		dst[len(x)] = big.Word(carry)
		return len(x) + 1
	}
	return len(x)
}

func compareWords(x, y []big.Word) int {
	if len(x) != len(y) {
		return len(x) - len(y)
	}
	for i, word := range slices.Backward(x) {
		if word < y[i] {
			return -1
		}
		if word > y[i] {
			return 1
		}
	}
	return 0
}

func subWords(dst, x, y []big.Word) int {
	var borrow uint
	for i, word := range y {
		diff, b := bits.Sub(uint(x[i]), uint(word), borrow)
		dst[i], borrow = big.Word(diff), b
	}
	for i := len(y); i < len(x); i++ {
		diff, b := bits.Sub(uint(x[i]), 0, borrow)
		dst[i], borrow = big.Word(diff), b
	}
	return significantWords(dst[:len(x)])
}

func mulWords(dst, x, y []big.Word) int {
	for i, a := range x {
		if a == 0 {
			continue
		}
		row := dst[i : i+len(y)+1]
		var carry uint
		for j, b := range y {
			hi, lo := bits.Mul(uint(a), uint(b))
			lo, c := bits.Add(lo, uint(row[j]), 0)
			lo, c2 := bits.Add(lo, carry, 0)
			row[j] = big.Word(lo)
			// A word product and two word addends fit in two words.
			carry = hi + c + c2
		}
		row[len(y)] = big.Word(carry)
	}
	return significantWords(dst[:len(x)+len(y)])
}

func significantWords(words []big.Word) int {
	n := len(words)
	for n > 0 && words[n-1] == 0 {
		n--
	}
	return n
}

// makeBigInt publishes a result with a normalized magnitude of up to bigIntInlineWords+1 words.
func (cx *ExecutionContext) makeBigInt(words []big.Word, negative bool) storage.Value {
	if int64(len(words))*bits.UintSize > cx.MaxBigIntResultBits {
		cx.checkBigIntBits(wordBitLen(words))
	}
	return storage.NewBigIntFromWords(words, negative)
}

func (cx *ExecutionContext) addBigInts(a, b storage.Value) storage.Value {
	return cx.addSubBigInts(a, b, false)
}

func (cx *ExecutionContext) subBigInts(a, b storage.Value) storage.Value {
	if cx.InterruptFlag.Load() {
		cx.Poll()
	}
	return cx.addSubBigInts(a, b, true)
}

func (cx *ExecutionContext) addSubBigInts(a, b storage.Value, subtract bool) storage.Value {
	var xBuf, yBuf [64 / bits.UintSize]big.Word
	x, xNegative := bigIntMagnitude(a, &xBuf)
	y, yNegative := bigIntMagnitude(b, &yBuf)
	if len(y) == 0 {
		cx.checkBigIntBits(wordBitLen(x))
		return a
	}
	if len(x) == 0 && !subtract {
		cx.checkBigIntBits(wordBitLen(y))
		return b
	}
	if max(len(x), len(y)) > bigIntInlineWords {
		if !subtract && cx.InterruptFlag.Load() {
			cx.Poll()
		}
		z := new(big.Int)
		if subtract {
			z.Sub(a.BigInt(), b.BigInt())
		} else {
			z.Add(a.BigInt(), b.BigInt())
		}
		cx.checkBigIntBits(int64(z.BitLen()))
		return storage.NewBigInt(z)
	}

	var result [bigIntInlineWords + 1]big.Word
	n, negative := addSignedWords(result[:], x, y, xNegative, yNegative != subtract)
	return cx.makeBigInt(result[:n], negative)
}

func addSignedWords(dst, x, y []big.Word, xNegative, yNegative bool) (int, bool) {
	if xNegative == yNegative {
		if len(x) < len(y) {
			x, y = y, x
		}
		return addWords(dst, x, y), xNegative
	}
	switch c := compareWords(x, y); {
	case c == 0:
		return 0, false
	case c < 0:
		x, y, xNegative = y, x, yNegative
	}
	return subWords(dst, x, y), xNegative
}

func (cx *ExecutionContext) mulBigInts(a, b storage.Value) storage.Value {
	if cx.InterruptFlag.Load() {
		cx.Poll()
	}
	var xBuf, yBuf [64 / bits.UintSize]big.Word
	x, xNegative := bigIntMagnitude(a, &xBuf)
	y, yNegative := bigIntMagnitude(b, &yBuf)
	if len(x) == 0 || len(y) == 0 {
		return storage.BigIntFromInt64(0)
	}
	if len(x)+len(y) > bigIntInlineWords {
		// The product has the sum of the bits of x and y, or one less.
		cx.checkBigIntBits(wordBitLen(x) + wordBitLen(y) - 1)
		z := numconv.MulBigInt(a.BigInt(), b.BigInt(), cx.Poll)
		cx.checkBigIntBits(int64(z.BitLen()))
		return storage.NewBigInt(z)
	}

	var result [bigIntInlineWords]big.Word
	n := mulWords(result[:], x, y)
	return cx.makeBigInt(result[:n], xNegative != yNegative)
}

// mulAddBigInts computes a*b+c when both operations fit in bounded scratch storage.
// The caller has set the frame's PC for the multiplication.
func (cx *ExecutionContext) mulAddBigInts(a, b, c storage.Value, addPC int) (storage.Value, bool) {
	var xBuf, yBuf, zBuf [64 / bits.UintSize]big.Word
	x, xNegative := bigIntMagnitude(a, &xBuf)
	y, yNegative := bigIntMagnitude(b, &yBuf)
	z, zNegative := bigIntMagnitude(c, &zBuf)
	if len(x) == 0 || len(y) == 0 || len(z) == 0 || len(x)+len(y) > bigIntInlineWords || len(z) > bigIntInlineWords {
		return storage.Value{}, false
	}
	if cx.InterruptFlag.Load() {
		cx.Poll()
	}
	var product [bigIntInlineWords]big.Word
	n := mulWords(product[:], x, y)
	if int64(n)*bits.UintSize > cx.MaxBigIntResultBits {
		cx.checkBigIntBits(wordBitLen(product[:n]))
	}
	cx.frames[len(cx.frames)-1].pc = addPC
	var sum [bigIntInlineWords + 1]big.Word
	n, negative := addSignedWords(sum[:], product[:n], z, xNegative != yNegative, zNegative)
	return cx.makeBigInt(sum[:n], negative), true
}

// BigIntToString implements BigInt::toString. It throws a RangeError when the result would be
// longer than MaxStringLength.
func (cx *ExecutionContext) BigIntToString(v storage.Value, radix int) wtf8.String {
	if i, ok := v.BigInt64(); ok {
		return wtf8.String(strconv.FormatInt(i, radix))
	}
	x := v.BigInt()
	n := x.BitLen()
	// In a power-of-two radix 2^k, x has ceil(BitLen(x)/k) digits. In any other radix, it has
	// more than (BitLen(x)-1)/log2(radix) digits.
	if bits.OnesCount(uint(radix)) == 1 {
		k := bits.TrailingZeros(uint(radix))
		var sign int
		if x.Sign() < 0 {
			sign = 1
		}
		if sign+(n+k-1)/k > storage.MaxStringLength {
			cx.ThrowRangeError("invalid string length")
		}
	} else if float64(n-1)/math.Log2(float64(radix)) > storage.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	s := numconv.FormatBigInt(x, radix, cx.Poll)
	if len(s) > storage.MaxStringLength {
		cx.ThrowRangeError("invalid string length")
	}
	return s
}

// bigExpStepBits is the size in bits of a power above which numconv.ExpBigInt computes it.
const bigExpStepBits = 1 << 16
