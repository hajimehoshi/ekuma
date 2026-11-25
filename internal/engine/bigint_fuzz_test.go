// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"fmt"
	"math"
	"math/big"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// bigIntFromBytes returns the integer of the big-endian two's complement b.
func bigIntFromBytes(b []byte) *big.Int {
	x := new(big.Int).SetBytes(b)
	if len(b) > 0 && b[0] >= 0x80 {
		x.Sub(x, new(big.Int).Lsh(big.NewInt(1), uint(8*len(b))))
	}
	return x
}

// bytesOfBigInt is the inverse of bigIntFromBytes.
func bytesOfBigInt(x *big.Int) []byte {
	n := x.BitLen()/8 + 1
	m := x
	if x.Sign() < 0 {
		m = new(big.Int).Add(x, new(big.Int).Lsh(big.NewInt(1), uint(8*n)))
	}
	return m.FillBytes(make([]byte, n))
}

// bigIntOperators holds the operators under test with the oracle for x and y, which returns nil when
// the operation throws.
var bigIntOperators = []struct {
	expr string
	want func(x, y *big.Int) *big.Int
}{
	{
		expr: "x + y",
		want: func(x, y *big.Int) *big.Int { return new(big.Int).Add(x, y) },
	},
	{
		expr: "x - y",
		want: func(x, y *big.Int) *big.Int { return new(big.Int).Sub(x, y) },
	},
	{
		expr: "x * y",
		want: func(x, y *big.Int) *big.Int { return new(big.Int).Mul(x, y) },
	},
	{
		expr: "x / y",
		want: func(x, y *big.Int) *big.Int {
			if y.Sign() == 0 {
				return nil
			}
			return new(big.Int).Quo(x, y)
		},
	},
	{
		expr: "x % y",
		want: func(x, y *big.Int) *big.Int {
			if y.Sign() == 0 {
				return nil
			}
			return new(big.Int).Rem(x, y)
		},
	},
	{
		expr: "x & y",
		want: func(x, y *big.Int) *big.Int { return new(big.Int).And(x, y) },
	},
	{
		expr: "x | y",
		want: func(x, y *big.Int) *big.Int { return new(big.Int).Or(x, y) },
	},
	{
		expr: "x ^ y",
		want: func(x, y *big.Int) *big.Int { return new(big.Int).Xor(x, y) },
	},
	{
		expr: "-x",
		want: func(x, y *big.Int) *big.Int { return new(big.Int).Neg(x) },
	},
	{
		expr: "~x",
		want: func(x, y *big.Int) *big.Int { return new(big.Int).Not(x) },
	},
	{
		expr: "++x",
		want: func(x, y *big.Int) *big.Int { return new(big.Int).Add(x, big.NewInt(1)) },
	},
	{
		expr: "--x",
		want: func(x, y *big.Int) *big.Int { return new(big.Int).Sub(x, big.NewInt(1)) },
	},
	{
		expr: "x << y",
		want: shiftBigInt,
	},
	{
		expr: "x >> y",
		want: func(x, y *big.Int) *big.Int { return shiftBigInt(x, new(big.Int).Neg(y)) },
	},
	{
		expr: "x ** y",
		want: powBigInt,
	},
}

// maxFuzzBigIntBits is the size of a result from which the operations of a fuzz test throw.
const maxFuzzBigIntBits = 1 << 12

// shiftBigInt returns x shifted left by n, or nil when the result is too large.
func shiftBigInt(x, n *big.Int) *big.Int {
	switch {
	case x.Sign() == 0:
		return x
	case n.Sign() > 0:
		if n.Cmp(big.NewInt(maxFuzzBigIntBits)) > 0 {
			return nil
		}
		return new(big.Int).Lsh(x, uint(n.Int64()))
	case n.Cmp(big.NewInt(-maxFuzzBigIntBits)) < 0:
		return big.NewInt(int64(min(x.Sign(), 0)))
	}
	// The shift rounds toward negative infinity.
	d := new(big.Int).Lsh(big.NewInt(1), uint(-n.Int64()))
	return new(big.Int).Div(x, d)
}

// powBigInt returns x to the power n, or nil when the operation throws.
func powBigInt(x, n *big.Int) *big.Int {
	if n.Sign() < 0 {
		return nil
	}
	if x.CmpAbs(big.NewInt(1)) <= 0 {
		if n.Sign() == 0 || x.Sign() < 0 && n.Bit(0) == 0 {
			return big.NewInt(1)
		}
		return x
	}
	// A power of |x| ≥ 2 has more bits than n.
	if n.Cmp(big.NewInt(maxFuzzBigIntBits)) >= 0 {
		return nil
	}
	r := big.NewInt(1)
	for range n.Int64() {
		r.Mul(r, x)
		if r.BitLen() > maxFuzzBigIntBits {
			return nil
		}
	}
	return r
}

func bigIntBoundaries() []*big.Int {
	var vs []*big.Int
	for _, i := range []int64{0, 1, 2, 7, 31, 32, 62, 63, 64, 65, 127, 128, 1 << 30, 1<<30 + 1, 3037000499, 3037000500} {
		vs = append(vs, big.NewInt(i), big.NewInt(-i))
	}
	for _, n := range []uint{63, 64, 127, 128, 256} {
		for d := int64(-1); d <= 1; d++ {
			v := new(big.Int).Lsh(big.NewInt(1), n)
			v.Add(v, big.NewInt(d))
			vs = append(vs, v, new(big.Int).Neg(v))
		}
	}
	return vs
}

func FuzzBigIntOperators(f *testing.F) {
	for _, x := range bigIntBoundaries() {
		for _, y := range bigIntBoundaries() {
			f.Add(bytesOfBigInt(x), bytesOfBigInt(y))
		}
	}

	cx := engine.NewExecutionContextWithBigIntResultLimit(maxFuzzBigIntBits)
	builtin.Install(cx)
	var fns []engine.Value
	for _, op := range bigIntOperators {
		// The result of an operation that throws is undefined.
		fn, err := runScript(cx, "operator", fmt.Sprintf(`(function(x, y) { try { return %s; } catch (e) { if (!(e instanceof RangeError)) throw e; } })`, op.expr))
		if err != nil {
			f.Fatal(err)
		}
		fns = append(fns, fn)
	}
	f.Fuzz(func(t *testing.T, xb, yb []byte) {
		if len(xb) > 64 || len(yb) > 64 {
			t.Skip("too long")
		}
		x, y := bigIntFromBytes(xb), bigIntFromBytes(yb)
		for i, op := range bigIntOperators {
			var got engine.Value
			cx.Run(func() {
				got = cx.Call(fns[i].Object(), storage.Undefined, storage.NewBigInt(new(big.Int).Set(x)), storage.NewBigInt(new(big.Int).Set(y)))
			})
			want := op.want(x, y)
			if want != nil && want.BitLen() > maxFuzzBigIntBits {
				want = nil
			}
			if want == nil {
				if !got.IsUndefined() {
					t.Errorf("%s for x = %v and y = %v does not throw", op.expr, x, y)
				}
				continue
			}
			if !got.IsBigInt() || got.BigInt().Cmp(want) != 0 {
				t.Errorf("%s for x = %v and y = %v: got %s, want %v", op.expr, x, y, cx.DisplayString(got), want)
				continue
			}
			// Each BigInt has one representation.
			if _, ok := got.BigInt64(); ok != want.IsInt64() {
				t.Errorf("%s for x = %v and y = %v: BigInt64 reports %t", op.expr, x, y, ok)
			}
		}
	})
}

func FuzzBigIntConversions(f *testing.F) {
	for _, x := range bigIntBoundaries() {
		for _, n := range []float64{0, math.Copysign(0, -1), 0.5, -0.5, 1 << 53, 0x1.fffffffffffffp62, 1 << 63, -(1 << 63), 1 << 64, 0x1p128, math.Inf(1), math.NaN()} {
			f.Add(bytesOfBigInt(x), math.Float64bits(n), uint8(64))
		}
		for _, bits := range []uint8{0, 1, 2, 32, 63, 65, 127, 128, 129, 255} {
			f.Add(bytesOfBigInt(x), uint64(0), bits)
		}
	}

	cx, _ := engine.NewExecutionContext(nil)
	builtin.Install(cx)
	fn, err := runScript(cx, "conversions", `(function(x, n, bits) {
		return [Number(x), x < n, x <= n, x > n, x >= n, x == n, n < x, n <= x, n > x, n >= x, n == x,
			BigInt.asIntN(bits, x), BigInt.asUintN(bits, x)];
	})`)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, xb []byte, nBits uint64, bits uint8) {
		if len(xb) > 64 {
			t.Skip("too long")
		}
		x, n := bigIntFromBytes(xb), math.Float64frombits(nBits)
		var result *engine.Object
		cx.Run(func() {
			result = cx.Call(fn.Object(), storage.Undefined, storage.NewBigInt(new(big.Int).Set(x)), storage.Number(n), storage.Number(float64(bits))).Object()
		})
		element := func(i int) engine.Value {
			var v engine.Value
			cx.Run(func() {
				v = cx.Get(result, cx.ToPropertyKey(storage.Number(float64(i))))
			})
			return v
		}

		exact := new(big.Rat).SetInt(x)
		want, _ := exact.Float64()
		if got := element(0); !storage.SameValue(got, storage.Number(want)) {
			t.Errorf("Number(%v) = %s, want %v", x, cx.DisplayString(got), want)
		}

		less, equal, greater := false, false, false
		switch {
		case n != n:
		case math.IsInf(n, 0):
			less, greater = n > 0, n < 0
		default:
			c := exact.Cmp(new(big.Rat).SetFloat64(n))
			less, equal, greater = c < 0, c == 0, c > 0
		}
		for i, want := range []bool{
			less, less || equal, greater, greater || equal, equal,
			greater, greater || equal, less, less || equal, equal,
		} {
			if got := element(1 + i); !got.IsBool() || storage.ToBoolean(got) != want {
				t.Errorf("comparison %d of %v and %v: got %s, want %t", i, x, n, cx.DisplayString(got), want)
			}
		}

		modulus := new(big.Int).Lsh(big.NewInt(1), uint(bits))
		unsigned := new(big.Int).Mod(x, modulus)
		signed := new(big.Int).Set(unsigned)
		if bits > 0 && signed.Bit(int(bits)-1) != 0 {
			signed.Sub(signed, modulus)
		}
		for i, want := range []*big.Int{signed, unsigned} {
			got := element(11 + i)
			if !got.IsBigInt() || got.BigInt().Cmp(want) != 0 {
				t.Errorf("%s(%d, %v) = %s, want %v", [...]string{"asIntN", "asUintN"}[i], bits, x, cx.DisplayString(got), want)
				continue
			}
			if _, ok := got.BigInt64(); ok != want.IsInt64() {
				t.Errorf("%s(%d, %v): BigInt64 reports %t", [...]string{"asIntN", "asUintN"}[i], bits, x, ok)
			}
		}
	})
}
