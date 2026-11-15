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
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
	_ "github.com/hajimehoshi/ekuma/intl"
)

var (
	flagTest262Filter  = flag.String("test262.filter", "", "only run the test262 tests whose path contains this text")
	flagTest262Report  = flag.String("test262.report", "", "write every failing test262 test with its reason to this file")
	flagTest262Shuffle = flag.String("test262.shuffle", "off", "run the test262 tests in random order: off, on, or a seed; a replay is exact only with GOMAXPROCS=1")
	flagTest262Dir     = flag.String("test262.dir", "", "only run the test262 tests in this directory of the suite")
)

// test262Commit is the commit of https://github.com/tc39/test262 that the suite passes at.
const test262Commit = "2e58f87d8350ad51224617a041c6e041d52f8444"

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

// skippedTestsFile lists the test262 tests that are not run, one path per line, each with a
// comment that says why. A line that starts with "#" is a comment.
const skippedTestsFile = "testdata/test262-skips.txt"

// readLines returns the lines of the file name other than the empty lines and the comments,
// sorted.
func readLines(name string) ([]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" && !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	sort.Strings(lines)
	return lines, sc.Err()
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

// runOnce runs the test source at path in one mode and returns why it failed, or "" when it
// passed. timedOut reports whether it failed by running out of time.
func runOnce(h *harness, path, src string, m metadata, strict bool) (reason string, timedOut bool) {
	defer func() {
		if r := recover(); r != nil {
			reason, timedOut = fmt.Sprintf("engine panic: %v", r), false
		}
	}()
	var vm ekuma.VM
	loader := &moduleLoader{
		vm:      &vm,
		dir:     filepath.Dir(path),
		modules: map[string]*ekuma.Module{},
	}
	vm.SetModuleLoader(loader.load)
	// An async test reports its outcome by printing it.
	printed := new([]string)
	print := vm.FuncOf("print", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		if len(args) > 0 {
			*printed = append(*printed, args[0].String())
		}
		return ekuma.Undefined, nil
	})
	if err := vm.Global().Set("print", print); err != nil {
		return "harness: " + err.Error(), false
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	agents := newAgentCluster(ctx)
	defer agents.close()
	// The main agent can block, as in a shell, unless the test says otherwise.
	vm.SetCanBlock(!m.flags["CanBlockIsFalse"])
	if err := define262(&vm, agents); err != nil {
		return "harness: " + err.Error(), false
	}

	if !m.flags["raw"] {
		includes := append([]string{"assert.js", "sta.js"}, m.includes...)
		if m.flags["async"] {
			includes = append(includes, "doneprintHandle.js")
		}
		for _, name := range includes {
			text, err := h.file(name)
			if err != nil {
				return "harness: " + err.Error(), false
			}
			if _, err := runScript(ctx, &vm, name, text); err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					return "timeout in harness " + name, true
				}
				return "harness " + name + ": " + firstLine(err.Error()), false
			}
		}
	}
	if m.flags["module"] {
		return runModule(ctx, &vm, loader, path, src, m, printed)
	}
	if strict {
		src = "\"use strict\";\n" + src
	}
	_, err := runScript(ctx, &vm, path, src)
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", true
	}
	if m.negativePhase != "" {
		if err == nil {
			return "expected " + m.negativeType + " but the test completed", false
		}
		if got := errorName(context.Background(), err); got != m.negativeType {
			return "expected " + m.negativeType + " but got " + firstLine(err.Error()), false
		}
		var se *ekuma.SyntaxError
		if early := errors.As(err, &se); early != (m.negativePhase != "runtime") {
			return "expected the error in the " + m.negativePhase + " phase: " + firstLine(err.Error()), false
		}
		return "", false
	}
	if err != nil {
		return firstLine(err.Error()), false
	}
	if m.flags["async"] {
		if awaitAgents(ctx, &vm, printed) {
			return "timeout", true
		}
		return asyncOutcome(*printed), false
	}
	return "", false
}

// moduleLoader loads the modules of a test from the test262 checkout, resolving a specifier
// relative to the directory of the importing file. It keeps one module record per file and type.
type moduleLoader struct {
	vm *ekuma.VM

	// dir is the directory of the test, for the code that has no file.
	dir string

	modules map[string]*ekuma.Module

	// source is the module of moduleSourceSpecifier, once it is loaded.
	source *ekuma.Module
}

// moduleSourceSpecifier is the specifier that test262 resolves to a module with a module source.
const moduleSourceSpecifier = "<module source>"

func (l *moduleLoader) load(req ekuma.ModuleRequest) (*ekuma.Module, error) {
	if req.Specifier == moduleSourceSpecifier {
		// The specifier names a module that provides a module source, which no module that
		// the loader parses does.
		if l.source == nil {
			l.source = ekuma.NewSourceModule(l.vm, req.Specifier, "Test262ModuleSource")
		}
		return l.source, nil
	}
	dir := l.dir
	if filepath.IsAbs(req.ReferrerName) {
		dir = filepath.Dir(req.ReferrerName)
	}
	path := filepath.Join(dir, filepath.FromSlash(req.Specifier))
	typ := req.Attributes["type"]
	key := path + "\x00" + typ
	if m, ok := l.modules[key]; ok {
		return m, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m *ekuma.Module
	switch typ {
	case "":
		m, err = l.vm.ParseModule(path, string(b))
	case "json":
		m, err = l.vm.ParseJSONModule(path, string(b))
	case "text":
		m = l.vm.NewSyntheticModule(path, l.vm.ValueOf(string(b)))
	case "bytes":
		var v ekuma.Value
		v, err = l.call("(n => new Uint8Array(n))", len(b))
		if err == nil {
			ekuma.CopyBytesToJS(v, b)
			v, err = l.call("(a => new Uint8Array(a.buffer.transferToImmutable()))", v)
		}
		m = l.vm.NewSyntheticModule(path, v)
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
	f, err := runScript(context.Background(), l.vm, "loader", fn)
	if err != nil {
		return ekuma.Undefined, err
	}
	return f.Invoke(context.Background(), arg)
}

// runModule runs the test source at path as a module, after the harness ran in vm.
func runModule(ctx context.Context, vm *ekuma.VM, loader *moduleLoader, path, src string, m metadata, printed *[]string) (reason string, timedOut bool) {
	// phase judges the error of a phase of a negative test.
	phase := func(name string, err error) string {
		if m.negativePhase != name {
			return "expected the error in the " + m.negativePhase + " phase: " + firstLine(err.Error())
		}
		if got := errorName(context.Background(), err); got != m.negativeType {
			return "expected " + m.negativeType + " but got " + firstLine(err.Error())
		}
		return ""
	}
	mod, err := vm.ParseModule(path, src)
	if err != nil {
		if m.negativePhase != "" {
			return phase("parse", err), false
		}
		return firstLine(err.Error()), false
	}
	loader.modules[path+"\x00"] = mod
	p, err := mod.Evaluate(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", true
	}
	if err != nil {
		if m.negativePhase != "" {
			return phase("resolution", err), false
		}
		return firstLine(err.Error()), false
	}
	// Evaluate ran the jobs: the promise is settled unless the evaluation waits forever.
	var rejected *ekuma.Error
	if st, r := p.PromiseState(); st == ekuma.PromiseRejected {
		rejected = &ekuma.Error{Value: r}
	}
	if m.negativePhase != "" {
		if rejected == nil {
			return "expected " + m.negativeType + " but the test completed", false
		}
		return phase("runtime", rejected), false
	}
	if rejected != nil {
		return firstLine(rejected.Error()), false
	}
	if m.flags["async"] {
		return asyncOutcome(*printed), false
	}
	return "", false
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
	return asyncIncomplete
}

// asyncIncomplete is the reason of an async test that has not printed its outcome.
const asyncIncomplete = "the async test did not complete"

// define262 defines the host object $262 of the harness, with the members that the engine supports.
// agents implements $262.agent.
func define262(vm *ekuma.VM, agents *agentCluster) error {
	o, err := new262(vm, vm.InitialRealm())
	if err != nil {
		return err
	}
	if err := agents.define(vm, o); err != nil {
		return err
	}
	return vm.Global().Set("$262", o)
}

// new262 returns the object $262 for the realm r of vm, without $262.agent.
func new262(vm *ekuma.VM, r *ekuma.Realm) (ekuma.Value, error) {
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
		o, err := new262(vm, nr)
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
	if err := o.Set("AbstractModuleSource", ekuma.AbstractModuleSource(r)); err != nil {
		return ekuma.Undefined, err
	}
	detach := r.FuncOf("detachArrayBuffer", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		v := ekuma.Undefined
		if len(args) > 0 {
			v = args[0]
		}
		return ekuma.Undefined, ekuma.DetachArrayBuffer(vm, v)
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
			return nil, &ekuma.Error{Value: e}
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
	if err := o.Set("IsHTMLDDA", ekuma.NewHTMLDDA(r)); err != nil {
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

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// runTest runs the test at path in every mode that its flags ask for. It returns why the test
// failed, or "" when it passed, and whether it failed by running out of time.
func runTest(h *harness, path string) (reason string, timedOut bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return err.Error(), false
	}
	src := string(b)
	m := parseMetadata(src)
	if m.flags["module"] {
		// Module code is always strict.
		return runOnce(h, path, src, m, false)
	}
	if !m.flags["onlyStrict"] {
		if r, t := runOnce(h, path, src, m, false); r != "" {
			return r, t
		}
	}
	if !m.flags["noStrict"] && !m.flags["raw"] {
		if r, t := runOnce(h, path, src, m, true); r != "" {
			return "strict mode: " + r, t
		}
	}
	return "", false
}

// test262ShuffleSeed parses the -test262.shuffle value. ok reports whether the tests are shuffled.
func test262ShuffleSeed(value string) (seed int64, ok bool, err error) {
	switch value {
	case "off":
		return 0, false, nil
	case "on":
		return time.Now().UnixNano(), true, nil
	}
	seed, err = strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("-test262.shuffle must be off, on, or an integer: %w", err)
	}
	return seed, true, nil
}

// TestTest262 runs the ECMAScript conformance suite. Every test must pass.
func TestTest262(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test262 in short mode")
	}
	dir := test262Dir()
	if _, err := os.Stat(filepath.Join(dir, "harness", "assert.js")); err != nil {
		t.Skipf("no test262 checkout at %s; set EKUMA_TEST262", dir)
	}

	// testDir is the directory of the suite whose tests run.
	testDir := "test"
	if *flagTest262Dir != "" {
		testDir = *flagTest262Dir
	}
	skips, err := readLines(skippedTestsFile)
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
		if *flagTest262Filter != "" && !strings.Contains(rel, *flagTest262Filter) {
			return nil
		}
		if _, ok := slices.BinarySearch(skips, rel); ok {
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
	// order holds the indices in paths in the order they are handed to the workers.
	order := make([]int, len(paths))
	for i := range order {
		order[i] = i
	}
	if seed, ok, err := test262ShuffleSeed(*flagTest262Shuffle); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Logf("-test262.shuffle %d", seed)
		r := rand.New(rand.NewPCG(uint64(seed), 0))
		r.Shuffle(len(order), func(i, j int) {
			order[i], order[j] = order[j], order[i]
		})
	}

	h := &harness{
		dir:   dir,
		files: map[string]string{},
	}
	reasons := make([]string, len(paths))
	timedOut := make([]bool, len(paths))
	var wg sync.WaitGroup
	next := make(chan int)
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range next {
				reasons[i], timedOut[i] = runTest(h, filepath.Join(dir, filepath.FromSlash(paths[i])))
			}
		})
	}
	for _, i := range order {
		next <- i
	}
	close(next)
	wg.Wait()
	// A slow test can run out of time only because the other workers load the machine, so a
	// timed-out test runs once more on its own.
	for i := range paths {
		if timedOut[i] {
			reasons[i], _ = runTest(h, filepath.Join(dir, filepath.FromSlash(paths[i])))
		}
	}

	// failed holds the indices in paths of the failing tests.
	var failed []int
	byArea := map[string][2]int{}
	for i, p := range paths {
		parts := strings.SplitN(p, "/", 4)
		area := strings.Join(parts[:min(3, len(parts)-1)], "/")
		c := byArea[area]
		c[1]++
		if reasons[i] == "" {
			c[0]++
		} else {
			failed = append(failed, i)
		}
		byArea[area] = c
	}
	t.Logf("test262 at %s", test262Commit)
	t.Logf("test262: %d of %d tests pass (%.1f%%)", len(paths)-len(failed), len(paths), 100*float64(len(paths)-len(failed))/float64(max(len(paths), 1)))
	if len(skipped) > 0 {
		t.Logf("test262: %d tests skipped, as %s lists", len(skipped), skippedTestsFile)
	}
	// A skipped test still runs, so that the list loses a test that passes again.
	for _, p := range skipped {
		if r, _ := runTest(h, filepath.Join(dir, filepath.FromSlash(p))); r == "" {
			t.Errorf("%s passes now: remove it from %s", p, skippedTestsFile)
		}
	}
	if *flagTest262Filter == "" {
		for _, p := range skips {
			if _, ok := slices.BinarySearch(skipped, p); strings.HasPrefix(p, testDir+"/") && !ok {
				t.Errorf("%s is not in the suite: remove it from %s", p, skippedTestsFile)
			}
		}
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
	const maxShown = 40
	for n, i := range failed {
		if n == maxShown {
			t.Errorf("and %d more failures", len(failed)-maxShown)
			break
		}
		t.Errorf("%s fails: %s", paths[i], reasons[i])
	}
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
// printed its outcome or nothing is left to wait for. It reports whether ctx ended it.
func awaitAgents(ctx context.Context, vm *ekuma.VM, printed *[]string) bool {
	for asyncOutcome(*printed) == asyncIncomplete {
		ok, err := vm.AwaitJobs(ctx)
		if err != nil {
			return ctx.Err() != nil
		}
		if !ok {
			break
		}
	}
	return false
}
