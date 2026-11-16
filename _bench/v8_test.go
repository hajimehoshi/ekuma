// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package bench_test

import (
	"fmt"
	"testing"

	"github.com/hajimehoshi/ekuma/_bench/internal/comparison"
)

// v8SelectFormat is formatted with the name of a benchmark of the V8 suite. The script sets the
// benchmark up and defines __run(n), which runs it n times. The benchmarks before it in its
// suite run once first, since a benchmark may use what they leave, as Decrypt does.
const v8SelectFormat = `
var __benchmark = (function (name) {
	var suites = BenchmarkSuite.suites;
	for (var i = 0; i < suites.length; i++) {
		var benchmarks = suites[i].benchmarks;
		for (var j = 0; j < benchmarks.length; j++) {
			if (benchmarks[j].name !== name) {
				continue;
			}
			for (var k = 0; k < j; k++) {
				benchmarks[k].Setup();
				benchmarks[k].run();
				benchmarks[k].TearDown();
			}
			benchmarks[j].Setup();
			return benchmarks[j];
		}
	}
	throw new Error("no benchmark " + name);
})(%q);

function __run(n) {
	for (var i = 0; i < n; i++) {
		__benchmark.run();
	}
}
`

// BenchmarkV8 runs the V8 benchmark suite, version 7. One iteration is one run of a benchmark.
// The benchmarks check their results and throw when they are wrong.
func BenchmarkV8(b *testing.B) {
	srcs := loadSuite(b, comparison.V8)
	for _, bm := range comparison.V8Benchmarks {
		b.Run(bm.Name, func(b *testing.B) {
			setup := []script{
				{
					name: "base.js",
					src:  srcs["base.js"],
				},
				{
					name: bm.File,
					src:  srcs[bm.File],
				},
				{
					name: "select",
					src:  fmt.Sprintf(v8SelectFormat, bm.Name),
				},
			}
			benchEngines(b, setup, "__run(%d)")
		})
	}
}
