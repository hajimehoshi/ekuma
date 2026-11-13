// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"fmt"
	"os"
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
