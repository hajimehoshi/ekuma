// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"github.com/hajimehoshi/ekuma/_bench/internal/comparison"
)

// engineModules holds the module or repository of each engine, whose version the results files
// record.
var engineModules = map[string]string{
	comparison.Baseline: ekumaModule,
	"goja":              "github.com/dop251/goja",
	"moejs":             "github.com/MoeForge/moejs",
	"sobek":             "github.com/grafana/sobek",
	"otto":              "github.com/robertkrimen/otto",
	"quickjs":           "modernc.org/quickjs",
	"qjs":               "github.com/fastschema/qjs",
	"quickjs-c":         comparison.QuickJSRepository,
	"v8go-jitless":      "github.com/tommie/v8go",
	"v8go":              "github.com/tommie/v8go",
}

// runTimeout limits the process of one run of a benchmark.
const runTimeout = 10 * time.Minute

var errBaselineFailed = errors.New("Ekuma benchmark failed")

// buildTest builds the test binary of the module into dir and returns its path.
func buildTest(ctx context.Context, dir string) (string, error) {
	flags, err := goEnv(ctx, "CGO_CXXFLAGS")
	if err != nil {
		return "", err
	}
	// v8go compiles against the libc++ headers that it bundles, which needs -nostdinc++, a flag
	// that #cgo directives do not allow.
	if !slices.Contains(strings.Fields(flags), "-nostdinc++") {
		flags = strings.TrimSpace(flags + " -nostdinc++")
	}
	bin := filepath.Join(dir, "bench.test")
	cmd := exec.CommandContext(ctx, "go", "test", "-c", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_CXXFLAGS="+flags)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	log.Print("building the benchmarks")
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go test -c: %w", err)
	}
	return bin, nil
}

// environment returns the configuration lines that describe the environment of the benchmarks
// of the test binary bin.
func environment(ctx context.Context, bin string, engines []string) ([]string, error) {
	goVersion, err := goEnv(ctx, "GOVERSION")
	if err != nil {
		return nil, err
	}
	lines := []string{
		"os: " + osVersion(ctx),
		"go: " + goVersion,
	}

	out, err := exec.CommandContext(ctx, "go", "version", "-m", bin).Output()
	if err != nil {
		return nil, fmt.Errorf("go version -m: %w", err)
	}
	// deps holds the versions of the dependencies of bin, keyed by module path.
	deps := map[string]string{}
	for line := range strings.Lines(string(out)) {
		// A dependency is a line "\tdep\tPATH\tVERSION\tSUM".
		if f := strings.Split(strings.TrimSpace(line), "\t"); len(f) >= 3 && f[0] == "dep" {
			deps[f[1]] = f[2]
		}
	}

	var recorded []string
	for _, e := range engines {
		m := engineModules[e]
		if slices.Contains(recorded, m) {
			continue
		}
		recorded = append(recorded, m)
		var v string
		switch m {
		case ekumaModule:
			if v, err = ekumaVersion(ctx); err != nil {
				return nil, err
			}
		case comparison.QuickJSRepository:
			v = comparison.QuickJSVersion
		default:
			var ok bool
			if v, ok = deps[m]; !ok {
				return nil, fmt.Errorf("the test binary does not depend on %s", m)
			}
		}
		lines = append(lines, m+": "+v)
	}
	return lines, nil
}

// ekumaModule is the path of the module of Ekuma, which the module of this command replaces
// with its parent directory.
const ekumaModule = "github.com/hajimehoshi/ekuma"

// ekumaVersion returns the version of the Ekuma in the parent directory in the form that the go
// command stamps on a main module: the tag of its commit, or a pseudo-version, with +dirty when
// the files of Ekuma other than this module differ from the commit.
func ekumaVersion(ctx context.Context) (string, error) {
	git := func(args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, "git", slices.Concat([]string{"-C", ".."}, args)...).Output()
		if err != nil {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return strings.TrimSpace(string(out)), nil
	}

	var version string
	if tag, err := git("describe", "--tags", "--exact-match", "--match", "v[0-9]*", "HEAD"); err == nil && semver.IsValid(tag) {
		version = tag
	} else {
		rev, err := git("rev-parse", "--short=12", "HEAD")
		if err != nil {
			return "", err
		}
		t, err := git("log", "-1", "--format=%ct", "HEAD")
		if err != nil {
			return "", err
		}
		sec, err := strconv.ParseInt(t, 10, 64)
		if err != nil {
			return "", err
		}
		// A pseudo-version follows the latest tag before the commit, if any.
		older, err := git("describe", "--tags", "--abbrev=0", "--match", "v[0-9]*", "HEAD")
		if err != nil || !semver.IsValid(older) {
			older = ""
		}
		version = module.PseudoVersion(semver.Major(older), older, time.Unix(sec, 0), rev)
	}

	status, err := git("status", "--porcelain", "--", ".", ":(exclude)_bench")
	if err != nil {
		return "", err
	}
	if status != "" {
		version += "+dirty"
	}
	return version, nil
}

// runSuite runs rounds rounds of the benchmarks names of s on engines with the test binary bin,
// and writes the results file of s into dir. The file starts with the configuration lines env.
func runSuite(ctx context.Context, bin string, s suite, names, engines []string, rounds int, env []string, dir string) error {
	n := rounds * len(names) * len(engines)
	log.Printf("%s: %d rounds of %d benchmarks on %d engines: %d runs of %s each", s.name, rounds, len(names), len(engines), n, s.benchtime)

	header := []string{
		"date: " + time.Now().Format(time.RFC3339),
	}
	header = append(header, env...)
	header = append(header,
		"rounds: "+fmt.Sprint(rounds),
		"benchtime: "+s.benchtime,
		"load-start: "+loadAverage(ctx),
	)

	// testHeader holds the configuration lines that go test prints.
	var testHeader []string
	var records []string
	var failures int
	start := time.Now()
	for round := range rounds {
		for i, name := range names {
			for _, engine := range rotate(engines, round+i) {
				full := "Benchmark" + s.name + "/" + name + "/" + engine
				r := runBenchmark(ctx, bin, s.benchtime, full)
				if err := ctx.Err(); err != nil {
					return err
				}
				if r.failed && engine == comparison.Baseline {
					failures++
					log.Print(r.record)
				}
				if testHeader == nil {
					testHeader = r.header
				}
				records = append(records, r.record)
			}
		}
		elapsed := time.Since(start)
		left := elapsed / time.Duration(round+1) * time.Duration(rounds-round-1)
		log.Printf("%s: round %d of %d done in %s, about %s left", s.name, round+1, rounds, elapsed.Round(time.Second), left.Round(time.Second))
	}
	header = append(header, "load-end: "+loadAverage(ctx))
	header = append(header, testHeader...)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	text := strings.Join(header, "\n") + "\n" + strings.Join(records, "\n") + "\n"
	if err := os.WriteFile(s.resultsPath(dir), []byte(text), 0o644); err != nil {
		return err
	}
	if failures > 0 {
		return fmt.Errorf("%w: %s (%d benchmark runs failed)", errBaselineFailed, s.name, failures)
	}
	return nil
}

// rotate returns a copy of s rotated left by k elements.
func rotate(s []string, k int) []string {
	k %= len(s)
	return slices.Concat(s[k:], s[:k])
}

// A run is the outcome of one run of a benchmark.
type run struct {
	// record is the line of the outcome in the results file: the result line that go test
	// printed, or a SKIP or FAIL line with the reason.
	record string

	// header holds the configuration lines that go test printed.
	header []string

	// failed reports whether the benchmark failed, rather than ran or was skipped.
	failed bool
}

// runBenchmark runs the benchmark name with the test binary bin in a process of its own.
func runBenchmark(ctx context.Context, bin, benchtime, name string) run {
	var pattern []string
	for p := range strings.SplitSeq(name, "/") {
		pattern = append(pattern, "^"+regexp.QuoteMeta(p)+"$")
	}
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin,
		"-test.run", "^$",
		"-test.bench", strings.Join(pattern, "/"),
		"-test.benchtime", benchtime,
		"-test.timeout", runTimeout.String(),
		"-test.v",
	).CombinedOutput()
	return parseRun(name, string(out), err)
}

// logPrefixRe matches the position that starts a line that a test logged.
var logPrefixRe = regexp.MustCompile(`^\s+\S+\.go:\d+: `)

// parseRun returns the outcome of the benchmark name from the output of its process and the
// error of running it.
func parseRun(name, out string, err error) run {
	var r run
	var result, reason string
	var skipped bool
	for line := range strings.Lines(out) {
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "goos: "), strings.HasPrefix(line, "goarch: "), strings.HasPrefix(line, "pkg: "), strings.HasPrefix(line, "cpu: "):
			r.header = append(r.header, line)
		case isResultLine(name, line):
			result = line
		case logPrefixRe.MatchString(line):
			if reason == "" {
				reason = logPrefixRe.ReplaceAllString(line, "")
			}
		case line == "--- SKIP: "+name, strings.HasPrefix(line, "--- SKIP: "+name+" "):
			skipped = true
		case strings.HasPrefix(line, "panic: "):
			if reason == "" {
				reason = line
			}
		}
	}
	switch {
	case result != "" && err == nil:
		r.record = result
	case skipped && err == nil:
		r.record = "--- SKIP: " + name + ": " + reason
	default:
		if reason == "" && err != nil {
			reason = err.Error()
		}
		r.record = "--- FAIL: " + name + ": " + reason
		r.failed = true
	}
	return r
}

// isResultLine reports whether line is the result line of the benchmark name.
func isResultLine(name, line string) bool {
	rest, ok := strings.CutPrefix(line, name)
	if !ok || !strings.Contains(rest, " ns/op") {
		return false
	}
	// The name may have the suffix of GOMAXPROCS.
	rest = strings.TrimLeft(strings.TrimPrefix(rest, "-"), "0123456789")
	return strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t")
}

// goEnv returns the value of the go environment variable key.
func goEnv(ctx context.Context, key string) (string, error) {
	out, err := exec.CommandContext(ctx, "go", "env", key).Output()
	if err != nil {
		return "", fmt.Errorf("go env %s: %w", key, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// osVersion returns the name and the version of the operating system.
func osVersion(ctx context.Context) string {
	if runtime.GOOS == "darwin" {
		name, err1 := exec.CommandContext(ctx, "sw_vers", "-productName").Output()
		version, err2 := exec.CommandContext(ctx, "sw_vers", "-productVersion").Output()
		if err1 == nil && err2 == nil {
			return strings.TrimSpace(string(name)) + " " + strings.TrimSpace(string(version))
		}
	}
	return runtime.GOOS
}

// loadAverage returns the load averages of the system over 1, 5 and 15 minutes, or an empty
// string if uptime does not tell them.
func loadAverage(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "uptime").Output()
	if err != nil {
		return ""
	}
	// uptime ends with "load average: 1.00, 2.00, 3.00" on Linux and with
	// "load averages: 1.00 2.00 3.00" on macOS.
	_, avg, ok := strings.Cut(string(out), "load average")
	if !ok {
		return ""
	}
	_, avg, _ = strings.Cut(avg, ":")
	return strings.Join(strings.Fields(strings.ReplaceAll(avg, ",", " ")), " ")
}
