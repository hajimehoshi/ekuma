// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package bench_test

import (
	"fmt"
	"testing"

	"github.com/hajimehoshi/ekuma/_bench/internal/comparison"
)

// BenchmarkMicro runs the benchmark functions of QuickJS's microbench.js. One iteration is one
// iteration of the loop of a function, which may perform its operation several times.
func BenchmarkMicro(b *testing.B) {
	srcs := loadSuite(b, comparison.Micro)
	src, names, err := comparison.Microbench(srcs["microbench.js"])
	if err != nil {
		b.Fatal(err)
	}
	setup := []script{
		{
			name: "microbench.js",
			src:  src,
		},
	}
	for _, name := range names {
		b.Run(name, func(b *testing.B) {
			// A function returns the number of operations that it performed, or a negative number
			// when its result is wrong.
			runFormat := fmt.Sprintf(`if (!(%s(%%d) > 0)) throw new Error(%q);`, name, name+" failed")
			benchEngines(b, setup, runFormat)
		})
	}
}
