// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package main_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	jsbench "github.com/hajimehoshi/ekuma/_bench/cmd/jsbench"
)

// TestReport checks that the ratios pair the runs of a round, that a skipped benchmark is left
// out of the geometric mean and reported with its reason, and that the summary gives the version
// of an engine.
func TestReport(t *testing.T) {
	// In the first round, goja takes 2 times as long as Ekuma on Richards, and 1.5 times in the
	// second, so its median ratio is 1.75. The ratio of the medians would be 250 / 150.
	const results = `date: 2026-11-16T00:00:00Z
github.com/dop251/goja: v1.2.3
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
--- FAIL: BenchmarkV8/Richards/ekuma:
BenchmarkV8/Richards/goja-12 10 1000 ns/op
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
		want    []string
	}{
		{
			section: "V8.relative",
			want:    []string{"Richards", "1.00", "1.75", "n/a", "12.5", "n/a", "n/a", "n/a", "n/a", "n/a"},
		},
		{
			section: "summary",
			want:    []string{"goja", "2.65", "v1.2.3"},
		},
		{
			section: "summary",
			want:    []string{"otto", "12.5", "(1/2)"},
		},
	} {
		if got := rowFields(sections[tt.section], tt.want[0]); !slices.Equal(got, tt.want) {
			t.Errorf("section %s: the row %s has the fields %q, want %q\n%s", tt.section, tt.want[0], got, tt.want, sections[tt.section])
		}
	}

	const missing = "- otto in V8 benchmark suite v7 (DeltaBlue): `SyntaxError: unexpected token`\n"
	if !strings.Contains(sections["missing"], missing) {
		t.Errorf("section missing:\n%s\nwant the line\n%s", sections["missing"], missing)
	}
	if !strings.Contains(sections["missing"], "- Ekuma in V8 benchmark suite v7 (Richards): `1/3 rounds missing: no result`") {
		t.Errorf("section missing does not report the failed baseline round:\n%s", sections["missing"])
	}
}

// rowFields returns the fields of the first line of section whose first field is label.
func rowFields(section, label string) []string {
	for line := range strings.Lines(section) {
		if f := strings.Fields(line); len(f) > 0 && f[0] == label {
			return f
		}
	}
	return nil
}
