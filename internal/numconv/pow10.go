// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv

import (
	"math"
	"strconv"
	"sync"
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
