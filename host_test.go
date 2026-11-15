// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"math"
	"math/rand/v2"
	"strconv"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

// run runs src in vm and returns the result as a string.
func run(t *testing.T, vm *ekuma.VM, src string) string {
	t.Helper()
	v, err := runScript(t.Context(), vm, "host", src)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	return toString(t, vm, v)
}

func TestRandomSource(t *testing.T) {
	var vm ekuma.VM
	vm.SetRandomSource(rand.NewPCG(1, 2))
	r := rand.New(rand.NewPCG(1, 2))
	for i := range 3 {
		got, err := strconv.ParseFloat(run(t, &vm, `Math.random()`), 64)
		if err != nil {
			t.Fatal(err)
		}
		if want := r.Float64(); got != want {
			t.Errorf("call %d: got %v, want %v", i, got, want)
		}
	}
}

// constSource is a [rand.Source] that returns its value.
type constSource uint64

func (s constSource) Uint64() uint64 {
	return uint64(s)
}

func TestRandomSourceBounds(t *testing.T) {
	var vm ekuma.VM
	for _, tc := range []struct {
		src  constSource
		want float64
	}{
		{
			src:  0,
			want: 0,
		},
		{
			src:  math.MaxUint64,
			want: 1 - 0x1p-53,
		},
	} {
		vm.SetRandomSource(tc.src)
		got, err := strconv.ParseFloat(run(t, &vm, `Math.random()`), 64)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("source %#x: got %v, want %v", uint64(tc.src), got, tc.want)
		}
	}
}

func TestRandomSourceNil(t *testing.T) {
	var vm ekuma.VM
	vm.SetRandomSource(constSource(0))
	vm.SetRandomSource(nil)
	// Two zeros in a row from a random source are practically impossible.
	if got := run(t, &vm, `var a = Math.random(), b = Math.random(); 0 <= a && a < 1 && 0 <= b && b < 1 && (a !== 0 || b !== 0)`); got != "true" {
		t.Errorf("got %s, want true", got)
	}
}

func TestTimeZone(t *testing.T) {
	var vm ekuma.VM
	vm.SetTimeZone(time.FixedZone("", 5*3600))
	if got, want := run(t, &vm, `new Date().getTimezoneOffset()`), "-300"; got != want {
		t.Errorf("fixed zone: got %q, want %q", got, want)
	}
	vm.SetTimeZone(nil)
	_, off := time.Now().Zone()
	if got, want := run(t, &vm, `new Date().getTimezoneOffset()`), strconv.Itoa(-off/60); got != want {
		t.Errorf("nil: got %q, want %q", got, want)
	}
}
