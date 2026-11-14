// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

// BenchmarkWorkload runs testdata/workload.js, a synthetic workload shaped like a scene of a
// 2D tile-based game. One iteration is one frame of the scene.
func BenchmarkWorkload(b *testing.B) {
	src, err := os.ReadFile("testdata/workload.js")
	if err != nil {
		b.Fatal(err)
	}
	for _, plugins := range []int{12, 0} {
		b.Run(fmt.Sprintf("plugins=%d", plugins), func(b *testing.B) {
			ctx := b.Context()
			vm := ekuma.New()
			if _, err := vm.RunScript(ctx, "workload.js", string(src)); err != nil {
				b.Fatal(err)
			}
			if _, err := vm.RunScript(ctx, "setup", fmt.Sprintf("setup(60, %d)", plugins)); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			if _, err := vm.RunScript(ctx, "run", fmt.Sprintf("run(%d)", b.N)); err != nil {
				b.Fatal(err)
			}
		})
	}
}

// gameDataJSON returns JSON text shaped like the data files of a 2D role-playing game:
// an array of n records with mixed fields, nested arrays of numbers and some non-ASCII strings.
func gameDataJSON(n int) string {
	names := []string{"Slime", "Bat", "スライム", "コウモリ", "Goblin", "ゴブリン王", "Dragon", "Ωmega"}
	seed := uint32(1)
	rnd := func(m int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % m
	}
	var b strings.Builder
	b.WriteString("[null")
	for i := 1; i <= n; i++ {
		name := names[rnd(len(names))]
		fmt.Fprintf(&b, `,{"id":%d,"name":"%s %d","note":"<hp:%d>\n<drop:%d>","battlerName":"%s","battlerHue":%d,`,
			i, name, i, rnd(1000), rnd(50), name, rnd(360))
		fmt.Fprintf(&b, `"exp":%d,"gold":%d,"rate":%d.%02d,"hidden":%t,"script":null,`, rnd(10000), rnd(5000), rnd(10), rnd(100), rnd(2) == 0)
		b.WriteString(`"params":[`)
		for j := range 8 {
			if j > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%d", rnd(999)+1)
		}
		b.WriteString(`],"dropItems":[`)
		for j := range 3 {
			if j > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"kind":%d,"dataId":%d,"denominator":%d}`, rnd(4), rnd(200), rnd(10)+1)
		}
		b.WriteString(`],"traits":[`)
		for j := range rnd(4) + 1 {
			if j > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"code":%d,"dataId":%d,"value":%d.%d}`, rnd(70), rnd(10), rnd(2), rnd(10))
		}
		b.WriteString(`]}`)
	}
	b.WriteString("]")
	return b.String()
}

// BenchmarkJSON parses and stringifies generated game data.
func BenchmarkJSON(b *testing.B) {
	text := gameDataJSON(3000)
	ctx := b.Context()
	vm := ekuma.New()
	if err := vm.Global().Set("text", text); err != nil {
		b.Fatal(err)
	}
	if _, err := vm.RunScript(ctx, "setup", `
		function parse() { return JSON.parse(text); }
		var data = parse();
		function stringify() { return JSON.stringify(data); }`); err != nil {
		b.Fatal(err)
	}
	b.Run("parse", func(b *testing.B) {
		b.SetBytes(int64(len(text)))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := vm.Global().Call(ctx, "parse"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("stringify", func(b *testing.B) {
		out, err := vm.Global().Call(ctx, "stringify")
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(out.String())))
		b.ReportAllocs()
		for b.Loop() {
			if _, err := vm.Global().Call(ctx, "stringify"); err != nil {
				b.Fatal(err)
			}
		}
	})
}
