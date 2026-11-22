// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
	_ "github.com/hajimehoshi/ekuma/intl"
)

// Test262 runs Test262 and local regression scripts with the supplied host operations.
type Test262 struct {
	NewHTMLDDA           func(*ekuma.Realm) ekuma.Value
	AbstractModuleSource func(*ekuma.Realm) ekuma.Value
	NewSourceModule      func(*ekuma.VM, string, string) *ekuma.Module
}

var test262Flags = registerSuiteFlags("test262")

// skippedTestsFile lists the test262 tests that are expected to fail.
const skippedTestsFile = testdataDir + "/test262-skips.txt"

func readTest262Skips(name string) (skipList, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	skips := skipList{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if file := strings.TrimSpace(sc.Text()); file != "" && !strings.HasPrefix(file, "#") {
			skips[file] = &skipEntry{}
		}
	}
	return skips, sc.Err()
}

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
	// YAML line breaks are LF, CR or CRLF.
	front := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(src[start+5 : end])
	for line := range strings.SplitSeq(front, "\n") {
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
				// Older Test262 cases call the parse phase "early".
				if m.negativePhase == "early" {
					m.negativePhase = "parse"
				}
			}
			if v, ok := strings.CutPrefix(trimmed, "type:"); ok {
				m.negativeType = strings.TrimSpace(v)
			}
		}
	}
	return m
}

// A harness pairs the Test262 host operations with the files and scripts shared by a run.
type harness struct {
	suite *Test262
	fs    fs.FS
	mu    sync.Mutex
	files map[string]string

	// scripts are the files parsed as unbound scripts, by name. Every VM of the run binds the
	// same harness scripts instead of parsing them again.
	scripts map[string]*ekuma.UnboundScript
}

func (suite *Test262) newHarness(files fs.FS) *harness {
	return &harness{
		suite:   suite,
		fs:      files,
		files:   map[string]string{},
		scripts: map[string]*ekuma.UnboundScript{},
	}
}

func (suite *Test262) checkoutHarness(t *testing.T, dir string) *harness {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, "harness", "assert.js")); err != nil {
		t.Skipf("no test262 checkout at %s; set EKUMA_TEST262", dir)
	}
	return suite.newHarness(os.DirFS(filepath.Join(dir, "harness")))
}

// script returns the file name parsed as an unbound script.
func (h *harness) script(name string) (*ekuma.UnboundScript, error) {
	text, err := h.file(name)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.scripts[name]; ok {
		return s, nil
	}
	s, err := ekuma.ParseUnboundScript(name, text)
	if err != nil {
		return nil, err
	}
	h.scripts[name] = s
	return s, nil
}

func (h *harness) file(name string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.files[name]; ok {
		return s, nil
	}
	b, err := fs.ReadFile(h.fs, name)
	if err != nil {
		return "", err
	}
	h.files[name] = string(b)
	return string(b), nil
}

// errorName returns the name of the constructor of the thrown value of err, or of the kind of error.
func errorName(err error) string {
	if _, ok := errors.AsType[*ekuma.SyntaxError](err); ok {
		return "SyntaxError"
	}
	var ex *ekuma.ScriptError
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

// checkError judges the result of the test body against its negative metadata.
func (m metadata) checkError(phase string, err error) suiteResult {
	if err == nil {
		if m.negativePhase != "" {
			return suiteFailed.result("expected " + m.negativeType + " but the test completed")
		}
		return suiteResult{}
	}
	result := scriptErrorResult(err)
	if result.outcome != suiteFailed || m.negativePhase == "" {
		return result
	}
	phaseMatches := m.negativePhase == phase
	if phase == "parse" && !m.flags["module"] {
		// ParseScript reports both parse and early errors before evaluation.
		phaseMatches = m.negativePhase != "runtime"
	}
	if !phaseMatches {
		return suiteFailed.result("expected the error in the " + m.negativePhase + " phase: " + result.reason)
	}
	if got := errorName(err); got != m.negativeType {
		return suiteFailed.result("expected " + m.negativeType + " but got " + result.reason)
	}
	return suiteResult{}
}

func runOnce(h *harness, path, src string, m metadata, strict bool, timeout time.Duration) (result suiteResult) {
	defer func() {
		if r := recover(); r != nil {
			result = suiteBroken.result(fmt.Sprintf("engine panic: %v", r))
		}
	}()
	var vm ekuma.VM
	// Tests that use the default locale expect en-US, not the host's locale.
	if err := vm.SetLocale("en-US"); err != nil {
		return suiteBroken.result("harness: " + err.Error())
	}
	loader := &moduleLoader{
		realm:    vm.InitialRealm(),
		dir:      filepath.Dir(path),
		readFile: os.ReadFile,
		modules:  map[string]*ekuma.Module{},
	}
	h.suite.setModuleLoader(&vm, loader)
	// An async test reports its outcome by printing it.
	printed := new([]string)
	print := vm.FuncOf("print", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		if len(args) > 0 {
			*printed = append(*printed, args[0].String())
		}
		return ekuma.Undefined, nil
	})
	if err := vm.Global().Set("print", print); err != nil {
		return suiteBroken.result("harness: " + err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	agents := newAgentCluster(ctx)
	defer agents.close()
	// The main agent can block, as in a shell, unless the test says otherwise.
	vm.SetCanBlock(!m.flags["CanBlockIsFalse"])
	if err := h.suite.define262(&vm, agents); err != nil {
		return suiteBroken.result("harness: " + err.Error())
	}

	if !m.flags["raw"] {
		includes := append([]string{"assert.js", "sta.js"}, m.includes...)
		if m.flags["async"] {
			includes = append(includes, "doneprintHandle.js")
		}
		for _, name := range includes {
			s, err := h.script(name)
			if err != nil {
				return suiteBroken.result("harness: " + err.Error())
			}
			if _, err := vm.BindScript(s).Evaluate(ctx); err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					return timedOut("timeout in harness " + name)
				}
				return suiteBroken.result("harness " + name + ": " + firstLine(err.Error()))
			}
		}
	}
	if m.flags["module"] {
		return runModule(ctx, loader, path, src, m, printed)
	}
	if strict {
		src = "\"use strict\";\n" + src
	}
	_, err := runScript(ctx, &vm, path, src)
	if err != nil || m.negativePhase != "" {
		phase := "runtime"
		if _, ok := errors.AsType[*ekuma.SyntaxError](err); ok {
			phase = "parse"
		}
		return m.checkError(phase, err)
	}
	if m.flags["async"] {
		return awaitAgents(ctx, &vm, printed)
	}
	return suiteResult{}
}

// moduleLoader loads the modules of a test from the suite checkout, resolving a specifier
// relative to the directory of the importing file. It keeps one module record per file and type.
type moduleLoader struct {
	realm *ekuma.Realm

	// dir is the directory of the test, for the code that has no file.
	dir string

	readFile func(string) ([]byte, error)
	modules  map[string]*ekuma.Module
}

// moduleSourceSpecifier is the specifier that test262 resolves to a module with a module source.
const moduleSourceSpecifier = "<module source>"

func (suite *Test262) setModuleLoader(vm *ekuma.VM, l *moduleLoader) {
	var source *ekuma.Module
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		if req.Specifier != moduleSourceSpecifier {
			return l.loadFile(l.resolveFile(req), req.Attributes["type"])
		}
		if source == nil {
			source = suite.NewSourceModule(vm, req.Specifier, "Test262ModuleSource")
		}
		return source, nil
	})
}

func (l *moduleLoader) resolveFile(req ekuma.ModuleRequest) string {
	dir := l.dir
	if req.Referrer != nil || filepath.Dir(req.ReferrerName) != "." {
		dir = filepath.Dir(req.ReferrerName)
	}
	return filepath.Join(dir, filepath.FromSlash(req.Specifier))
}

func (l *moduleLoader) loadFile(path, typ string) (*ekuma.Module, error) {
	key := path + "\x00" + typ
	if m, ok := l.modules[key]; ok {
		return m, nil
	}
	b, err := l.readFile(path)
	if err != nil {
		return nil, err
	}
	var m *ekuma.Module
	switch typ {
	case "":
		m, err = l.realm.ParseModule(path, string(b))
	case "json":
		m, err = l.realm.ParseJSONModule(path, string(b))
	case "text":
		m = l.realm.NewSyntheticModule(path, l.realm.ValueOf(string(b)))
	case "bytes":
		var v ekuma.Value
		v, err = l.call("(n => new Uint8Array(n))", len(b))
		if err == nil {
			ekuma.CopyBytesToJS(v, b)
			v, err = l.call("(a => new Uint8Array(a.buffer.transferToImmutable()))", v)
		}
		m = l.realm.NewSyntheticModule(path, v)
	default:
		return nil, fmt.Errorf("unsupported module type %q", typ)
	}
	if err != nil {
		return nil, err
	}
	l.modules[key] = m
	return m, nil
}

// call calls the function that the script fn evaluates to with arg.
func (l *moduleLoader) call(fn string, arg any) (ekuma.Value, error) {
	f, err := runScriptInRealm(context.Background(), l.realm, "loader", fn)
	if err != nil {
		return ekuma.Undefined, err
	}
	return f.Invoke(context.Background(), arg)
}

// evaluate returns the module's evaluation promise or the phase and error that stopped it.
func (l *moduleLoader) evaluate(ctx context.Context, path, src string) (promise ekuma.Value, phase string, err error) {
	mod, err := l.realm.ParseModule(path, src)
	if err != nil {
		return ekuma.Undefined, "parse", err
	}
	l.modules[path+"\x00"] = mod
	p, err := mod.Evaluate(ctx)
	if err != nil {
		return ekuma.Undefined, "resolution", err
	}
	if st, r := p.PromiseState(); st == ekuma.PromiseRejected {
		return ekuma.Undefined, "runtime", &ekuma.ScriptError{
			Value: r,
		}
	}
	return p, "", nil
}

func runModule(ctx context.Context, loader *moduleLoader, path, src string, m metadata, printed *[]string) suiteResult {
	if _, phase, err := loader.evaluate(ctx, path, src); err != nil {
		return m.checkError(phase, err)
	}
	if m.negativePhase != "" {
		return m.checkError("runtime", nil)
	}
	if m.flags["async"] {
		result, _ := asyncOutcome(*printed)
		return result
	}
	return suiteResult{}
}

// asyncOutcome returns the test outcome and whether the test reported completion.
func asyncOutcome(printed []string) (suiteResult, bool) {
	for _, l := range printed {
		if l == "Test262:AsyncTestComplete" {
			return suiteResult{}, true
		}
		if r, ok := strings.CutPrefix(l, "Test262:AsyncTestFailure:"); ok {
			return suiteFailed.result("async: " + firstLine(r)), true
		}
	}
	return suiteFailed.result("the async test did not complete"), false
}

// define262 defines the host object $262 of the harness, with the members that the engine supports.
// agents implements $262.agent.
func (suite *Test262) define262(vm *ekuma.VM, agents *agentCluster) error {
	o, err := suite.new262(vm, vm.InitialRealm())
	if err != nil {
		return err
	}
	if err := agents.define(vm, o); err != nil {
		return err
	}
	return vm.Global().Set("$262", o)
}

// new262 returns the object $262 for the realm r of vm, without $262.agent.
func (suite *Test262) new262(vm *ekuma.VM, r *ekuma.Realm) (ekuma.Value, error) {
	ctx := context.Background()
	s, err := r.ParseScript("$262", "({})")
	if err != nil {
		return ekuma.Undefined, err
	}
	o, err := s.Evaluate(ctx)
	if err != nil {
		return ekuma.Undefined, err
	}
	createRealm := r.FuncOf("createRealm", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		nr := vm.NewRealm()
		o, err := suite.new262(vm, nr)
		if err != nil {
			return nil, err
		}
		if err := nr.Global().Set("$262", o); err != nil {
			return nil, err
		}
		return o, nil
	})
	if err := o.Set("createRealm", createRealm); err != nil {
		return ekuma.Undefined, err
	}
	if err := o.Set("AbstractModuleSource", suite.AbstractModuleSource(r)); err != nil {
		return ekuma.Undefined, err
	}
	detach := r.FuncOf("detachArrayBuffer", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		v := ekuma.Undefined
		if len(args) > 0 {
			v = args[0]
		}
		if detachArrayBuffer(vm, v) {
			return ekuma.Undefined, nil
		}
		// DetachArrayBuffer throws a TypeError for a buffer that cannot be detached.
		c, err := r.Global().Get("TypeError")
		if err != nil {
			return nil, err
		}
		e, err := c.New(ctx, "cannot detach "+v.String())
		if err != nil {
			return nil, err
		}
		return nil, &ekuma.ScriptError{Value: e}
	})
	if err := o.Set("detachArrayBuffer", detach); err != nil {
		return ekuma.Undefined, err
	}
	evalScript := r.FuncOf("evalScript", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		var src string
		if len(args) > 0 {
			src = args[0].String()
		}
		s, err := r.ParseScript("evalScript", src)
		if se, ok := errors.AsType[*ekuma.SyntaxError](err); ok {
			// An early error of the script is a SyntaxError thrown to the caller.
			c, err := r.Global().Get("SyntaxError")
			if err != nil {
				return nil, err
			}
			e, err := c.New(ctx, se.Message)
			if err != nil {
				return nil, err
			}
			return nil, &ekuma.ScriptError{Value: e}
		}
		if err != nil {
			return nil, err
		}
		return s.Evaluate(ctx)
	})
	if err := o.Set("evalScript", evalScript); err != nil {
		return ekuma.Undefined, err
	}
	if err := o.Set("global", r.Global()); err != nil {
		return ekuma.Undefined, err
	}
	if err := o.Set("IsHTMLDDA", suite.NewHTMLDDA(r)); err != nil {
		return ekuma.Undefined, err
	}
	gc := r.FuncOf("gc", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		runtime.GC()
		return ekuma.Undefined, nil
	})
	if err := o.Set("gc", gc); err != nil {
		return ekuma.Undefined, err
	}
	return o, nil
}

// detachArrayBuffer detaches the ArrayBuffer v of vm, and reports false when [ekuma.VM.DetachArrayBuffer]
// refuses v.
func detachArrayBuffer(vm *ekuma.VM, v ekuma.Value) (ok bool) {
	defer func() {
		if x := recover(); x != nil {
			if _, isValueError := x.(*ekuma.ValueError); !isValueError {
				panic(x)
			}
			ok = false
		}
	}()
	vm.DetachArrayBuffer(v)
	return true
}

// test262Test is a test of the suite that is read and ready to run.
type test262Test struct {
	path string
	src  string
	m    metadata
}

func readTest(path string) (*test262Test, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src := string(b)
	return &test262Test{
		path: path,
		src:  src,
		m:    parseMetadata(src),
	}, nil
}

// waitsForAgents reports whether the test spends its time waiting for its agents rather than
// computing.
func (t *test262Test) waitsForAgents() bool {
	return slices.Contains(t.m.includes, "atomicsHelper.js")
}

// run runs the test in every mode that its flags ask for.
func (t *test262Test) run(h *harness, timeout time.Duration) suiteResult {
	if t.m.flags["module"] {
		// Module code is always strict.
		return runOnce(h, t.path, t.src, t.m, false, timeout)
	}
	if !t.m.flags["onlyStrict"] {
		if result := runOnce(h, t.path, t.src, t.m, false, timeout); result.outcome != suitePassed {
			return result
		}
	}
	if !t.m.flags["noStrict"] && !t.m.flags["raw"] {
		if result := runOnce(h, t.path, t.src, t.m, true, timeout); result.outcome != suitePassed {
			result.reason = "strict mode: " + result.reason
			return result
		}
	}
	return suiteResult{}
}

func runTest(h *harness, path string, timeout time.Duration) suiteResult {
	t, err := readTest(path)
	if err != nil {
		return suiteBroken.result(err.Error())
	}
	return t.run(h, timeout)
}

// CheckExpectedFailureWithMissingInclude checks that an expected failure still requires a working harness.
func (suite *Test262) CheckExpectedFailureWithMissingInclude(t *testing.T) {
	h := suite.newHarness(localTest262HarnessFS{})
	test := test262Test{
		path: "missing-include.js",
		src:  "throw new Error();",
		m: metadata{
			includes: []string{"missing.js"},
		},
	}
	result := test.run(h, testTimeout).expectFailure(skippedTestsFile)
	if result.outcome != suiteBroken {
		t.Errorf("expected failure with missing include: got %v, want %v", result.outcome, suiteBroken)
	}
}

// CheckEarlyNegativeWithRuntimeError checks phase matching for a module that throws during evaluation.
func (suite *Test262) CheckEarlyNegativeWithRuntimeError(t *testing.T) {
	const src = `/*---
flags: [module]
negative:
  phase: early
  type: SyntaxError
---*/
throw new SyntaxError();
`
	h := suite.newHarness(localTest262HarnessFS{})
	result := runOnce(h, "runtime-error.js", src, parseMetadata(src), false, testTimeout)
	if result.outcome != suiteFailed {
		t.Errorf("runtime SyntaxError in an early-negative module: got %v, want %v", result.outcome, suiteFailed)
	}
}

// Run runs the ECMAScript conformance suite and checks its failures against the skip list.
func (suite *Test262) Run(t *testing.T) {
	dir := test262Flags.checkoutDir(t)
	h := suite.checkoutHarness(t, dir)
	pin, err := os.ReadFile(testdataDir + "/test262-revision.txt")
	if err != nil {
		t.Fatal(err)
	}
	// A custom suite can be inside another repository. Read only the suite's own HEAD.
	head := "unavailable"
	cmd := exec.CommandContext(t.Context(), "git", "-C", dir, "--git-dir=.git", "rev-parse", "--verify", "HEAD")
	if out, err := cmd.Output(); err == nil {
		head = strings.TrimSpace(string(out))
	}
	t.Logf("test262 suite %q (Git HEAD: %s, expected pin: %s)", dir, head, strings.TrimSpace(string(pin)))

	// testDir is the directory of the suite whose tests run.
	testDir := "test"
	if test262Flags.dir != "" {
		testDir = path.Clean(filepath.ToSlash(test262Flags.dir))
	}
	skips, err := readTest262Skips(skippedTestsFile)
	if err != nil {
		t.Fatalf("reading the skipped tests: %v", err)
	}
	// skipped holds the paths of the skipped tests in testDir.
	var skipped []string
	var paths []string
	err = filepath.WalkDir(filepath.Join(dir, testDir), func(p string, e fs.DirEntry, err error) error {
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
		if test262Flags.filter != "" && !strings.Contains(rel, test262Flags.filter) {
			return nil
		}
		if skips.match(rel) != nil {
			skipped = append(skipped, rel)
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	sort.Strings(skipped)

	results := make([]suiteResult, len(paths))
	var waitingTests sync.WaitGroup
	// waitingSlots limits the tests that run without a worker.
	waitingSlots := make(chan struct{}, 4*runtime.GOMAXPROCS(0))
	test262Flags.run(t, len(paths), func(i int) {
		tt, err := readTest(filepath.Join(dir, filepath.FromSlash(paths[i])))
		if err != nil {
			results[i] = suiteBroken.result(err.Error())
			return
		}
		if !tt.waitsForAgents() {
			results[i] = tt.run(h, testTimeout)
			return
		}
		// A test that waits needs no processor, so the worker goes on to the next test.
		waitingSlots <- struct{}{}
		waitingTests.Go(func() {
			defer func() {
				<-waitingSlots
			}()
			results[i] = tt.run(h, testTimeout)
			// Waiters can time out before the main agent resumes to notify them.
			results[i].retry = results[i].outcome != suitePassed
		})
	})
	waitingTests.Wait()
	retryTests := func(paths []string, results []suiteResult) {
		retryInIsolation(results, func(i int, timeout time.Duration) suiteResult {
			t.Logf("retrying %s in isolation: %s", paths[i], results[i].reason)
			return runTest(h, filepath.Join(dir, filepath.FromSlash(paths[i])), timeout)
		})
	}
	retryTests(paths, results)

	var failed []suiteFailure
	byArea := map[string][2]int{}
	for i, p := range paths {
		parts := strings.SplitN(p, "/", 4)
		area := strings.Join(parts[:min(3, len(parts)-1)], "/")
		c := byArea[area]
		c[1]++
		if results[i].outcome == suitePassed {
			c[0]++
		} else {
			failed = append(failed, suiteFailure{
				path:   p,
				reason: results[i].reason,
			})
		}
		byArea[area] = c
	}
	t.Logf("test262: %d of %d tests pass (%.1f%%)", len(paths)-len(failed), len(paths), 100*float64(len(paths)-len(failed))/float64(max(len(paths), 1)))
	if len(skipped) > 0 {
		t.Logf("test262: %d tests skipped, as %s lists", len(skipped), skippedTestsFile)
	}
	skippedResults := make([]suiteResult, len(skipped))
	for i, p := range skipped {
		skippedResults[i] = runTest(h, filepath.Join(dir, filepath.FromSlash(p)), testTimeout)
	}
	retryTests(skipped, skippedResults)
	for i, p := range skipped {
		result := skippedResults[i].expectFailure(skippedTestsFile)
		if result.outcome != suitePassed {
			t.Errorf("%s: %s", p, result.reason)
		}
	}
	for _, failure := range staleEntries(skips.unseen, skippedTestsFile, testDir, test262Flags.filter) {
		t.Errorf("%s: %s", failure.path, failure.reason)
	}
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

	test262Flags.reportFailures(t, failed)
}

// agentCluster implements $262.agent for the agents that a test starts. Each agent is a VM that
// runs on its own goroutine, and shares memory with the others through the SharedArrayBuffers
// that broadcast hands it.
type agentCluster struct {
	// ctx ends the agents and the blocking functions of the main agent.
	ctx    context.Context
	cancel context.CancelFunc

	agents []*testAgent
	wg     sync.WaitGroup
	start  time.Time

	mu      sync.Mutex
	reports []string
}

// testAgent is an agent that a test started.
type testAgent struct {
	broadcasts chan agentBroadcast
}

// agentBroadcast is the message of $262.agent.broadcast.
type agentBroadcast struct {
	block *ekuma.SharedDataBlock

	// num is the number that comes with the buffer, or nil.
	num any

	// received gets a value when the agent received the message.
	received chan<- struct{}
}

func newAgentCluster(ctx context.Context) *agentCluster {
	ctx, cancel := context.WithCancel(ctx)
	return &agentCluster{
		ctx:    ctx,
		cancel: cancel,
		start:  time.Now(),
	}
}

// close ends the agents and waits for their goroutines.
func (c *agentCluster) close() {
	c.cancel()
	c.wg.Wait()
}

func (c *agentCluster) report(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reports = append(c.reports, s)
}

// takeReport removes and returns the oldest report, if any.
func (c *agentCluster) takeReport() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.reports) == 0 {
		return "", false
	}
	r := c.reports[0]
	c.reports = c.reports[1:]
	return r, true
}

// sleep implements $262.agent.sleep for any agent.
func (c *agentCluster) sleep(args []ekuma.Value) {
	var ms float64
	if len(args) > 0 && args[0].Type() == ekuma.TypeNumber {
		ms = args[0].Float()
	}
	t := time.NewTimer(time.Duration(ms * float64(time.Millisecond)))
	defer t.Stop()
	select {
	case <-t.C:
	case <-c.ctx.Done():
	}
}

// monotonicNow implements $262.agent.monotonicNow.
func (c *agentCluster) monotonicNow() float64 {
	return float64(time.Since(c.start).Nanoseconds()) / 1e6
}

// define defines $262.agent of the main agent in vm.
func (c *agentCluster) define(vm *ekuma.VM, o ekuma.Value) error {
	start := vm.FuncOf("start", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		var src string
		if len(args) > 0 {
			src = args[0].String()
		}
		a := &testAgent{
			broadcasts: make(chan agentBroadcast, 1),
		}
		c.agents = append(c.agents, a)
		c.wg.Go(func() {
			c.run(a, src)
		})
		return ekuma.Undefined, nil
	})
	broadcast := vm.FuncOf("broadcast", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		if len(args) == 0 {
			return nil, errors.New("$262.agent.broadcast: not a SharedArrayBuffer")
		}
		blk := args[0].SharedDataBlock()
		var num any
		if len(args) > 1 && args[1].Type() == ekuma.TypeNumber {
			num = args[1].Float()
		}
		received := make(chan struct{}, len(c.agents))
		for _, a := range c.agents {
			select {
			case a.broadcasts <- agentBroadcast{block: blk, num: num, received: received}:
			case <-c.ctx.Done():
				return nil, context.Cause(c.ctx)
			}
		}
		for range c.agents {
			select {
			case <-received:
			case <-c.ctx.Done():
				return nil, context.Cause(c.ctx)
			}
		}
		return ekuma.Undefined, nil
	})
	getReport := vm.FuncOf("getReport", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		if r, ok := c.takeReport(); ok {
			return r, nil
		}
		return ekuma.Null, nil
	})
	sleep := vm.FuncOf("sleep", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		c.sleep(args)
		return ekuma.Undefined, nil
	})
	now := vm.FuncOf("monotonicNow", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return c.monotonicNow(), nil
	})
	f, err := runScript(context.Background(), vm, "$262.agent", "(start, broadcast, getReport, sleep, monotonicNow) => ({start, broadcast, getReport, sleep, monotonicNow})")
	if err != nil {
		return err
	}
	agent, err := f.Invoke(context.Background(), start, broadcast, getReport, sleep, now)
	if err != nil {
		return err
	}
	return o.Set("agent", agent)
}

// run runs the agent a with the source src until the cluster ends.
func (c *agentCluster) run(a *testAgent, src string) {
	var vm ekuma.VM
	vm.SetCanBlock(true)
	receiver := ekuma.Undefined
	receive := vm.FuncOf("receiveBroadcast", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		if len(args) > 0 {
			receiver = args[0]
		}
		return ekuma.Undefined, nil
	})
	report := vm.FuncOf("report", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		if len(args) > 0 {
			c.report(args[0].String())
		}
		return ekuma.Undefined, nil
	})
	sleep := vm.FuncOf("sleep", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		c.sleep(args)
		return ekuma.Undefined, nil
	})
	now := vm.FuncOf("monotonicNow", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		return c.monotonicNow(), nil
	})
	const define = `(receiveBroadcast, report, sleep, monotonicNow) => {
	globalThis.$262 = {
		agent: {
			receiveBroadcast,
			report(value) { report(String(value)); },
			leaving() {},
			sleep,
			monotonicNow,
		},
	};
}`
	f, err := runScript(c.ctx, &vm, "$262.agent", define)
	if err != nil {
		return
	}
	if _, err := f.Invoke(c.ctx, receive, report, sleep, now); err != nil {
		return
	}
	if _, err := runScript(c.ctx, &vm, "agent", src); err != nil {
		return
	}
	for {
		select {
		case <-c.ctx.Done():
			return
		case b := <-a.broadcasts:
			sab := vm.ValueOf(b.block)
			b.received <- struct{}{}
			if receiver.Type() == ekuma.TypeFunction {
				num := ekuma.Undefined
				if b.num != nil {
					num = vm.ValueOf(b.num)
				}
				if _, err := receiver.Invoke(c.ctx, sab, num); err != nil {
					return
				}
			}
			// The waiters of Atomics.waitAsync resolve as other agents notify them.
			for {
				if ok, err := vm.AwaitJobs(c.ctx); !ok || err != nil {
					break
				}
			}
		}
	}
}

// awaitAgents lets an async test whose promises wait for other agents complete: it runs the jobs
// whenever a waiter of Atomics.waitAsync of vm is notified or times out, until the test has
// printed its outcome or nothing is left to wait for.
func awaitAgents(ctx context.Context, vm *ekuma.VM, printed *[]string) suiteResult {
	for {
		result, done := asyncOutcome(*printed)
		if done {
			return result
		}
		ok, err := vm.AwaitJobs(ctx)
		if err != nil {
			return scriptErrorResult(err)
		}
		if !ok {
			return result
		}
	}
}
