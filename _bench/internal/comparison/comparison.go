// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package comparison describes the comparison of the engines: the engines, and the benchmark
// suites with their benchmarks. It fetches and reads the third-party benchmark programs, which
// the repository does not contain.
package comparison

import (
	"errors"
	"regexp"
	"slices"
	"strings"
)

// Baseline is the engine that the times of the others are relative to.
const Baseline = "ekuma"

// Engines lists the engines recognized by the benchmark harness.
var Engines = []string{
	Baseline,
	"goja",
	"sobek",
	"moejs",
	"otto",
	"quickjs",
	"qjs",
	"quickjs-c",
	"v8go-jitless",
	"v8go",
}

// A V8Benchmark is a benchmark of the V8 benchmark suite.
type V8Benchmark struct {
	// Name is the name of the benchmark.
	Name string

	// File is the file of the suite that defines the benchmark.
	File string
}

// V8Benchmarks lists the benchmarks of the V8 benchmark suite in the order that its runner runs
// them.
var V8Benchmarks = []V8Benchmark{
	{
		Name: "Richards",
		File: "richards.js",
	},
	{
		Name: "DeltaBlue",
		File: "deltablue.js",
	},
	{
		Name: "Encrypt",
		File: "crypto.js",
	},
	{
		Name: "Decrypt",
		File: "crypto.js",
	},
	{
		Name: "RayTrace",
		File: "raytrace.js",
	},
	{
		Name: "Earley",
		File: "earley-boyer.js",
	},
	{
		Name: "Boyer",
		File: "earley-boyer.js",
	},
	{
		Name: "RegExp",
		File: "regexp.js",
	},
	{
		Name: "Splay",
		File: "splay.js",
	},
	{
		Name: "NavierStokes",
		File: "navier-stokes.js",
	},
}

var (
	// microListRe matches the array of the benchmark functions in the runner of microbench.js.
	microListRe = regexp.MustCompile(`(?s)var test_list = \[(.*?)\];`)

	// microPushRe matches an addition to that array.
	microPushRe = regexp.MustCompile(`test_list\.push\((\w+)\);`)
)

// microRunnerStart starts the code at the end of microbench.js that runs its runner.
const microRunnerStart = `if (typeof scriptArgs === "undefined") {`

// Microbench returns the source of microbench.js without the code that runs its runner, and the
// names of the benchmark functions that the runner runs, other than sort_bench, which measures
// time itself.
func Microbench(src string) (string, []string, error) {
	i := strings.LastIndex(src, microRunnerStart)
	if i < 0 {
		return "", nil, errors.New("comparison: microbench.js: the code that runs the runner was not found")
	}
	m := microListRe.FindStringSubmatch(src)
	if m == nil {
		return "", nil, errors.New("comparison: microbench.js: test_list was not found")
	}
	var names []string
	for name := range strings.SplitSeq(m[1], ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	for _, m := range microPushRe.FindAllStringSubmatch(src, -1) {
		names = append(names, m[1])
	}
	names = slices.DeleteFunc(names, func(name string) bool {
		return name == "sort_bench"
	})
	return src[:i], names, nil
}
