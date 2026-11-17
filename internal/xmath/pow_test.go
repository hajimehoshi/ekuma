// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package xmath_test

import (
	"bufio"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/xmath"
)

// ulpError returns |got-want| in units of the last place at want.
func ulpError(got float64, want *big.Float) float64 {
	w, _ := want.Float64()
	if math.IsInf(w, 0) || math.IsInf(got, 0) {
		if got == w {
			return 0
		}
		return math.Inf(1)
	}
	exp := max(want.MantExp(nil)-53, -1074)
	d := new(big.Float).Sub(new(big.Float).SetFloat64(got), want)
	d.SetMantExp(d, -exp)
	e, _ := d.Float64()
	return math.Abs(e)
}

func TestPowTable(t *testing.T) {
	f, err := os.Open("testdata/pow.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var maxErr float64
	var n, misrounded int
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatalf("malformed line %q", line)
		}
		x, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			t.Fatal(err)
		}
		y, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			t.Fatal(err)
		}
		want, _, err := big.ParseFloat(fields[2], 10, 200, big.ToNearestEven)
		if err != nil {
			t.Fatal(err)
		}
		got := xmath.Pow(x, y)
		e := ulpError(got, want)
		if e > 0.51 {
			t.Errorf("Pow(%v, %v) = %v, want %v (%.3f ulp)", x, y, got, want.Text('g', 20), e)
		}
		if w, _ := want.Float64(); got != w {
			misrounded++
		}
		maxErr = max(maxErr, e)
		n++
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d powers, %d not correctly rounded, max error %.4f ulp", n, misrounded, maxErr)
}

func TestPowSpecialValues(t *testing.T) {
	negZero := math.Copysign(0, -1)
	inf := math.Inf(1)
	tests := []struct {
		x, y float64
		want float64
	}{
		{x: math.NaN(), y: 0, want: 1},
		{x: 2, y: math.NaN(), want: math.NaN()},
		{x: math.NaN(), y: 1, want: math.NaN()},
		{x: 1, y: inf, want: 1},
		{x: 0.5, y: inf, want: 0},
		{x: -2, y: inf, want: inf},
		{x: -0.5, y: -inf, want: inf},
		{x: 3, y: -inf, want: 0},
		{x: inf, y: 0.5, want: inf},
		{x: inf, y: -3, want: 0},
		{x: -inf, y: 3, want: -inf},
		{x: -inf, y: 2, want: inf},
		{x: -inf, y: 0.5, want: inf},
		{x: -inf, y: -3, want: negZero},
		{x: -inf, y: -2, want: 0},
		{x: 0, y: 3, want: 0},
		{x: 0, y: -0.5, want: inf},
		{x: negZero, y: 3, want: negZero},
		{x: negZero, y: 0.5, want: 0},
		{x: negZero, y: -3, want: -inf},
		{x: negZero, y: -2, want: inf},
		{x: -8, y: 1.0 / 3, want: math.NaN()},
		{x: -2, y: 3, want: -8},
		{x: -2, y: 1 << 60, want: inf},
		{x: -0.5, y: 1<<53 + 2, want: 0},
		{x: -1, y: 1 << 60, want: 1},
		{x: 2, y: 1024, want: inf},
		{x: 2, y: -1075, want: 0},
		{x: 0.5, y: 1074, want: 5e-324},
		{x: 5e-324, y: 0.5, want: math.Sqrt(5e-324)},
		{x: 5e-324, y: -1, want: inf},
		{x: 1e-200, y: 2, want: 0},
	}
	for _, tt := range tests {
		got := xmath.Pow(tt.x, tt.y)
		if math.Float64bits(got) != math.Float64bits(tt.want) && !(got != got && tt.want != tt.want) {
			t.Errorf("Pow(%v, %v) = %v, want %v", tt.x, tt.y, got, tt.want)
		}
	}
}
