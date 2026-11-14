// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

var (
	flagTest262Update = flag.Bool("test262.update", false, "rewrite the list of expected test262 failures from this run")
	flagTest262Filter = flag.String("test262.filter", "", "only run the test262 tests whose path contains this text; the expected failures are then not checked")
	flagTest262Report = flag.String("test262.report", "", "write every failing test262 test with its reason to this file")
)

// test262Commit is the commit of https://github.com/tc39/test262 that the list of expected failures was made with.
const test262Commit = "2e58f87d8350ad51224617a041c6e041d52f8444"

// expectedFailuresFile lists the test262 tests that are known to fail, one path per line.
const expectedFailuresFile = "testdata/test262-failures.txt"

// test262Dir returns the directory of the test262 checkout: $EKUMA_TEST262,
// or ekuma/test262 in the user cache directory.
func test262Dir() string {
	if d := os.Getenv("EKUMA_TEST262"); d != "" {
		return d
	}
	c, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(c, "ekuma", "test262")
}

// test262Dirs are the directories of the suite that are run. intl402 tests the optional
// internationalization API, and staging holds tests of proposals.
var test262Dirs = []string{"test/language", "test/built-ins", "test/annexB"}

// metadata is the part of the front matter of a test that the runner uses.
type metadata struct {
	includes []string
	flags    map[string]bool

	// negativePhase is "parse", "resolution" or "runtime" for a test that must fail, and empty otherwise.
	negativePhase string
	negativeType  string
}

// parseMetadata reads the YAML front matter of a test. It understands the subset of YAML
// that the suite uses for the keys of interest.
func parseMetadata(src string) metadata {
	m := metadata{flags: map[string]bool{}}
	start := strings.Index(src, "/*---")
	end := strings.Index(src, "---*/")
	if start < 0 || end < start {
		return m
	}
	list := func(s string) []string {
		s = strings.Trim(strings.TrimSpace(s), "[]")
		var out []string
		for e := range strings.SplitSeq(s, ",") {
			if e = strings.TrimSpace(e); e != "" {
				out = append(out, e)
			}
		}
		return out
	}
	key := ""
	for line := range strings.SplitSeq(src[start+5:end], "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			k, v, ok := strings.Cut(trimmed, ":")
			if !ok {
				continue
			}
			key = k
			switch key {
			case "includes":
				m.includes = append(m.includes, list(v)...)
			case "flags":
				for _, f := range list(v) {
					m.flags[f] = true
				}
			}
			continue
		}
		switch key {
		case "includes":
			if s, ok := strings.CutPrefix(trimmed, "- "); ok {
				m.includes = append(m.includes, strings.TrimSpace(s))
			}
		case "flags":
			if s, ok := strings.CutPrefix(trimmed, "- "); ok {
				m.flags[strings.TrimSpace(s)] = true
			}
		case "negative":
			if v, ok := strings.CutPrefix(trimmed, "phase:"); ok {
				m.negativePhase = strings.TrimSpace(v)
			}
			if v, ok := strings.CutPrefix(trimmed, "type:"); ok {
				m.negativeType = strings.TrimSpace(v)
			}
		}
	}
	return m
}

// harness caches the files of the harness directory of the suite.
type harness struct {
	dir   string
	mu    sync.Mutex
	files map[string]string
}

func (h *harness) file(name string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.files[name]; ok {
		return s, nil
	}
	b, err := os.ReadFile(filepath.Join(h.dir, "harness", name))
	if err != nil {
		return "", err
	}
	h.files[name] = string(b)
	return string(b), nil
}

// testTimeout is how long one run of a test may take before it is interrupted.
const testTimeout = 5 * time.Second

// errorName returns the name of the constructor of the thrown value of err, or of the kind of error.
func errorName(ctx context.Context, err error) string {
	if _, ok := errors.AsType[*ekuma.SyntaxError](err); ok {
		return "SyntaxError"
	}
	var ex *ekuma.Error
	if !errors.As(err, &ex) {
		return ""
	}
	c, cerr := ex.Value.Get("constructor")
	if cerr != nil {
		return ""
	}
	n, cerr := c.Get("name")
	if cerr != nil || n.Type() != ekuma.TypeString {
		return ""
	}
	return n.String()
}

// runOnce runs the test source in one mode and returns why it failed, or "" when it passed.
func runOnce(h *harness, src string, m metadata, strict bool) (reason string) {
	defer func() {
		if r := recover(); r != nil {
			reason = fmt.Sprintf("engine panic: %v", r)
		}
	}()
	if m.flags["module"] {
		return "modules are not supported"
	}
	vm := ekuma.NewVM()
	// An async test reports its outcome by printing it.
	var printed []string
	print := vm.FuncOf("print", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		if len(args) > 0 {
			printed = append(printed, args[0].String())
		}
		return ekuma.Undefined, nil
	})
	if err := vm.Global().Set("print", print); err != nil {
		return "harness: " + err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	if !m.flags["raw"] {
		includes := append([]string{"assert.js", "sta.js"}, m.includes...)
		if m.flags["async"] {
			includes = append(includes, "doneprintHandle.js")
		}
		for _, name := range includes {
			text, err := h.file(name)
			if err != nil {
				return "harness: " + err.Error()
			}
			if _, err := vm.RunScript(ctx, name, text); err != nil {
				return "harness " + name + ": " + firstLine(err.Error())
			}
		}
	}
	if strict {
		src = "\"use strict\";\n" + src
	}
	_, err := vm.RunScript(ctx, "test", src)
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if m.negativePhase != "" {
		if err == nil {
			return "expected " + m.negativeType + " but the test completed"
		}
		if got := errorName(context.Background(), err); got != m.negativeType {
			return "expected " + m.negativeType + " but got " + firstLine(err.Error())
		}
		var se *ekuma.SyntaxError
		if early := errors.As(err, &se); early != (m.negativePhase != "runtime") {
			return "expected the error in the " + m.negativePhase + " phase: " + firstLine(err.Error())
		}
		return ""
	}
	if err != nil {
		return firstLine(err.Error())
	}
	if m.flags["async"] {
		return asyncOutcome(printed)
	}
	return ""
}

// asyncOutcome judges an async test by the lines that it printed, after its jobs ran.
func asyncOutcome(printed []string) string {
	for _, l := range printed {
		if l == "Test262:AsyncTestComplete" {
			return ""
		}
		if r, ok := strings.CutPrefix(l, "Test262:AsyncTestFailure:"); ok {
			return "async: " + firstLine(r)
		}
	}
	return "the async test did not complete"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// runTest runs the test at path in every mode that its flags ask for.
func runTest(h *harness, path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	src := string(b)
	m := parseMetadata(src)
	if !m.flags["onlyStrict"] {
		if r := runOnce(h, src, m, false); r != "" {
			return r
		}
	}
	if !m.flags["noStrict"] && !m.flags["raw"] {
		if r := runOnce(h, src, m, true); r != "" {
			return "strict mode: " + r
		}
	}
	return ""
}

// TestTest262 runs the ECMAScript conformance suite and compares the set of failing tests
// with the list in testdata. A test that fails without being listed is a regression, and
// a listed test that passes has to be removed from the list, so the list only ever shrinks.
func TestTest262(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test262 in short mode")
	}
	dir := test262Dir()
	if _, err := os.Stat(filepath.Join(dir, "harness", "assert.js")); err != nil {
		t.Skipf("no test262 checkout at %s; set EKUMA_TEST262", dir)
	}

	var paths []string
	for _, d := range test262Dirs {
		err := filepath.WalkDir(filepath.Join(dir, d), func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() || !strings.HasSuffix(p, ".js") || strings.HasSuffix(p, "_FIXTURE.js") {
				return nil
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if *flagTest262Filter == "" || strings.Contains(rel, *flagTest262Filter) {
				paths = append(paths, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(paths)

	h := &harness{
		dir:   dir,
		files: map[string]string{},
	}
	reasons := make([]string, len(paths))
	var wg sync.WaitGroup
	next := make(chan int)
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range next {
				reasons[i] = runTest(h, filepath.Join(dir, filepath.FromSlash(paths[i])))
			}
		})
	}
	for i := range paths {
		next <- i
	}
	close(next)
	wg.Wait()

	var failed []string
	byArea := map[string][2]int{}
	for i, p := range paths {
		parts := strings.SplitN(p, "/", 4)
		area := strings.Join(parts[:min(3, len(parts)-1)], "/")
		c := byArea[area]
		c[1]++
		if reasons[i] == "" {
			c[0]++
		} else {
			failed = append(failed, p)
		}
		byArea[area] = c
	}
	t.Logf("the expected failures are for test262 at %s", test262Commit)
	t.Logf("test262: %d of %d tests pass (%.1f%%)", len(paths)-len(failed), len(paths), 100*float64(len(paths)-len(failed))/float64(max(len(paths), 1)))
	if testing.Verbose() {
		areas := make([]string, 0, len(byArea))
		for a := range byArea {
			areas = append(areas, a)
		}
		sort.Strings(areas)
		for _, a := range areas {
			t.Logf("%6d / %6d  %s", byArea[a][0], byArea[a][1], a)
		}
	}

	if *flagTest262Report != "" {
		var sb strings.Builder
		for i, p := range paths {
			if reasons[i] != "" {
				fmt.Fprintf(&sb, "%s\t%s\n", p, reasons[i])
			}
		}
		if err := os.WriteFile(*flagTest262Report, []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if *flagTest262Filter != "" {
		return
	}
	if *flagTest262Update {
		if err := os.WriteFile(expectedFailuresFile, []byte(strings.Join(failed, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	expected, err := readLines(expectedFailuresFile)
	if err != nil {
		t.Fatalf("reading the expected failures: %v; run with -test262.update to create the list", err)
	}
	const maxShown = 40
	shown := 0
	for _, p := range failed {
		if _, ok := slices.BinarySearch(expected, p); !ok {
			if shown++; shown <= maxShown {
				t.Errorf("regression: %s fails", p)
			}
		}
	}
	for _, p := range expected {
		if _, ok := slices.BinarySearch(failed, p); !ok {
			if shown++; shown <= maxShown {
				t.Errorf("%s passes now: remove it from %s (or run with -test262.update)", p, expectedFailuresFile)
			}
		}
	}
	if shown > maxShown {
		t.Errorf("and %d more differences", shown-maxShown)
	}
}

func readLines(name string) ([]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			lines = append(lines, l)
		}
	}
	sort.Strings(lines)
	return lines, sc.Err()
}
