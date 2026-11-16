// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	jsbench "github.com/hajimehoshi/ekuma/_bench/cmd/jsbench"
)

// TestReport checks that the ratios pair the runs of a round, and that a skipped benchmark is
// left out of the geometric mean and reported with its reason.
func TestReport(t *testing.T) {
	// In the first round, goja takes 2 times as long as Ekuma on Richards, and 1.5 times in the
	// second, so its median ratio is 1.75. The ratio of the medians would be 250 / 150.
	const results = `date: 2026-11-16T00:00:00Z
goos: darwin
BenchmarkV8/Richards/ekuma-12 	 10	 100 ns/op
BenchmarkV8/Richards/goja-12 	 10	 200 ns/op
BenchmarkV8/Richards/otto-12 	 1	 1000 ns/op
BenchmarkV8/DeltaBlue/goja-12 	 10	 400 ns/op
--- SKIP: BenchmarkV8/DeltaBlue/otto: SyntaxError: unexpected token
BenchmarkV8/DeltaBlue/ekuma-12 	 10	 100 ns/op
BenchmarkV8/Richards/otto-12 	 1	 3000 ns/op
BenchmarkV8/Richards/ekuma-12 	 10	 200 ns/op
BenchmarkV8/Richards/goja-12 	 10	 300 ns/op
BenchmarkV8/DeltaBlue/ekuma-12 	 10	 100 ns/op
--- SKIP: BenchmarkV8/DeltaBlue/otto: SyntaxError: unexpected token
BenchmarkV8/DeltaBlue/goja-12 	 10	 400 ns/op
`
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "V8.txt"), []byte(results), 0o644); err != nil {
		t.Fatal(err)
	}
	sections, err := jsbench.Report(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		section string
		line    string
	}{
		{
			section: "V8.relative",
			line:    "| Richards | 1.00 | 1.75 | n/a | 12.5 | n/a | n/a | n/a | n/a | n/a |",
		},
		{
			section: "summary",
			line:    "| V8 benchmark suite v7 | 1.00 | 2.65 | n/a | 12.5 (1 of 2) | n/a | n/a | n/a | n/a | n/a |",
		},
		{
			section: "missing",
			line:    "- otto in V8 benchmark suite v7 (DeltaBlue): `SyntaxError: unexpected token`",
		},
	} {
		if !strings.Contains(sections[tt.section], tt.line+"\n") {
			t.Errorf("section %s:\n%s\nwant the line\n%s", tt.section, sections[tt.section], tt.line)
		}
	}
}
