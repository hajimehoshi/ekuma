// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv

import (
	"math"
	"strconv"
	"sync"

	"github.com/hajimehoshi/ekuma/internal/xmath"
)

// pow10Table holds the doubles nearest to 10**n for n from -323 to 308.
var pow10Table = sync.OnceValue(func() []float64 {
	t := make([]float64, 308+323+1)
	for i := range t {
		t[i], _ = strconv.ParseFloat("1e"+strconv.Itoa(i-323), 64)
	}
	return t
})

// Pow10 returns the double nearest to 10**n.
func Pow10(n int) float64 {
	switch {
	case n < -323:
		return 0
	case n > 308:
		return math.Inf(1)
	}
	return pow10Table()[n+323]
}

// NumberPow implements Number::exponentiate.
func NumberPow(x, y float64) float64 {
	// The exponents NaN and ±Infinity with a base of ±1 give NaN, unlike in IEEE 754.
	if y != y || (math.IsInf(y, 0) && (x == 1 || x == -1)) {
		return math.NaN()
	}
	// math.Pow is off by a unit in the last place for some powers of ten.
	if x == 10 && y == math.Trunc(y) && math.Abs(y) <= 400 {
		return Pow10(int(y))
	}
	return xmath.Pow(x, y)
}
