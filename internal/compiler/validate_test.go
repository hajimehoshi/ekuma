// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// testFlags returns the flags in the Test262 front matter of src.
func testFlags(src string) map[string]bool {
	flags := map[string]bool{}
	start := strings.Index(src, "/*---")
	end := strings.Index(src, "---*/")
	if start < 0 || end < start {
		return flags
	}
	lines := strings.Split(src[start:end], "\n")
	for i, line := range lines {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "flags:")
		if !ok {
			continue
		}
		if rest = strings.TrimSpace(rest); rest != "" {
			for f := range strings.SplitSeq(strings.Trim(rest, "[]"), ",") {
				flags[strings.TrimSpace(f)] = true
			}
			continue
		}
		for _, l := range lines[i+1:] {
			f, ok := strings.CutPrefix(strings.TrimSpace(l), "- ")
			if !ok {
				break
			}
			flags[strings.TrimSpace(f)] = true
		}
	}
	return flags
}

// compileSource compiles the test text at path in every mode that its flags ask for, and returns
// the compiled code of each mode that compiles. A source that does not compile is a negative test.
func compileSource(path, text string) []*bytecode.Func {
	src := wtf8.FromValidUTF8(text)
	flags := testFlags(text)
	if flags["module"] {
		prog, err := parser.ParseModule(src, parser.Options{})
		if err != nil {
			return nil
		}
		f, _, err := compiler.CompileModule(prog, path)
		if err != nil {
			return nil
		}
		return []*bytecode.Func{f}
	}
	var srcs []wtf8.String
	if !flags["onlyStrict"] {
		srcs = append(srcs, src)
	}
	if !flags["noStrict"] && !flags["raw"] {
		srcs = append(srcs, "\"use strict\";\n"+src)
	}
	var out []*bytecode.Func
	for _, s := range srcs {
		prog, err := parser.ParseScript(s, parser.Options{})
		if err != nil {
			continue
		}
		f, err := compiler.CompileScript(prog, path)
		if err != nil {
			continue
		}
		out = append(out, f)
	}
	return out
}

// validateTree compiles every JavaScript file under the directories dirs and validates the code.
func validateTree(t *testing.T, dirs ...string) {
	var paths []string
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !e.IsDir() && strings.HasSuffix(p, ".js") {
				paths = append(paths, p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var funcs atomic.Int64
	var failures atomic.Int64
	next := make(chan string)
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for p := range next {
				b, err := os.ReadFile(p)
				if err != nil {
					t.Error(err)
					continue
				}
				for _, f := range compileSource(p, string(b)) {
					funcs.Add(int64(countFuncs(f)))
					if err := f.Validate(); err != nil {
						// Keep the output short when a slip breaks many files.
						if failures.Add(1) <= 50 {
							t.Errorf("%s: %v", p, err)
						}
					}
				}
			}
		})
	}
	for _, p := range paths {
		next <- p
	}
	close(next)
	wg.Wait()
	if n := failures.Load(); n > 0 {
		t.Errorf("%d compilations failed validation", n)
	}
	t.Logf("validated %d functions of %d files", funcs.Load(), len(paths))
}

func countFuncs(f *bytecode.Func) int {
	n := 1
	for _, p := range f.Protos {
		n += countFuncs(p)
	}
	return n
}

// TestValidateScripts tests that the compiled code of the local scripts and their harness is valid.
func TestValidateScripts(t *testing.T) {
	validateTree(t, filepath.Join("..", "..", "testdata", "scripts"), filepath.Join("..", "..", "testdata", "harness"))
}

// TestValidateTest262 tests that the compiled code of the Test262 suite and its harness is valid.
func TestValidateTest262(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test262 in short mode")
	}
	dir := os.Getenv("EKUMA_TEST262")
	if dir == "" {
		c, err := os.UserCacheDir()
		if err != nil {
			t.Skip(err)
		}
		dir = filepath.Join(c, "ekuma", "test262")
	}
	if _, err := os.Stat(filepath.Join(dir, "harness", "assert.js")); err != nil {
		t.Skipf("no test262 checkout at %s; set EKUMA_TEST262", dir)
	}
	validateTree(t, filepath.Join(dir, "harness"), filepath.Join(dir, "test"))
}
