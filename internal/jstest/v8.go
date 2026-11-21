// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hajimehoshi/ekuma"
	_ "github.com/hajimehoshi/ekuma/intl"
)

// V8 runs the V8 conformance suites with the supplied realm operations.
type V8 struct {
	IsEnteredRealm           func(*ekuma.Realm) bool
	ParseScriptValue         func(*ekuma.Realm, string, ekuma.Value) (*ekuma.Script, error)
	InstallGlobalProxy       func(*ekuma.Realm, ekuma.Value, bool) ekuma.Value
	DetachGlobalProxy        func(*ekuma.Realm)
	InvokeInRealm            func(context.Context, *ekuma.Realm, ekuma.Value) error
	DeferUnhandledRejections func(*ekuma.VM)
	TakeUnhandledRejection   func(*ekuma.VM) error
}

const v8SkipsFile = testdataDir + "/v8-skips.txt"
const v8EnvironmentFile = testdataDir + "/v8-environment.json"

var v8Flags = registerSuiteFlags("v8")

type v8Suite struct {
	dir     string
	helpers []string
	exclude []string
	flags   []string
}

// v8Suites lists the enabled test directories and each suite's helpers in load order.
var v8Suites = []v8Suite{
	{
		dir:     "test/intl",
		helpers: []string{"assert.js", "utils.js"},
		// Ekuma enables these features by default.
		flags: []string{"--harmony-intl-best-fit-matcher", "--harmony-intl-locale-info-func"},
	},
	{
		dir:     "test/mjsunit",
		helpers: []string{"mjsunit.js"},
		exclude: []string{"mjsunit_numfuzz.js"},
	},
}

// Run runs the selected V8 tests and checks their results against the skip list.
func (host *V8) Run(t *testing.T) {
	dir := v8Flags.checkoutDir(t)
	skips := readV8Skips(t)
	unseenEnvironments := readV8Environments(t)
	checkout := os.DirFS(dir)
	selectedDir := path.Clean(filepath.ToSlash(v8Flags.dir))
	if !fs.ValidPath(selectedDir) {
		t.Fatalf("-v8.dir must be relative to the V8 checkout: %q", v8Flags.dir)
	}
	var tests []v8Test
	var matchedSuite bool
	for _, suite := range v8Suites {
		root := suite.dir
		if inSuiteDir(selectedDir, suite.dir) {
			root = selectedDir
		} else if !inSuiteDir(suite.dir, selectedDir) {
			continue
		}
		matchedSuite = true
		tests = append(tests, suite.collect(t, checkout, root, v8Flags.filter, skips, unseenEnvironments)...)
	}
	if !matchedSuite {
		t.Fatalf("no enabled V8 suite in %q", selectedDir)
	}
	results := make([]suiteResult, len(tests))
	ctx := t.Context()
	run := func(i int, timeout time.Duration) suiteResult {
		return tests[i].run(ctx, host, checkout, timeout)
	}
	v8Flags.run(t, len(tests), func(i int) {
		results[i] = run(i, testTimeout)
	})
	retryInIsolation(results, run)

	var failures []suiteFailure
	var passed, expected, skipped int
	skipCounts := map[string]int{}
	for i, result := range results {
		switch {
		case result.outcome == suiteBroken || result.outcome == suiteFailed:
			failures = append(failures, suiteFailure{
				path:   tests[i].file,
				reason: result.reason,
			})
		case result.outcome == suiteSkipped:
			skipped++
			skipCounts[result.reason]++
		case tests[i].wantFailure:
			expected++
		default:
			passed++
		}
	}
	failedTests := len(failures)
	staleSkips := staleEntries(skips.unseen, v8SkipsFile, selectedDir, v8Flags.filter)
	staleEnvironments := staleEntries(maps.Keys(unseenEnvironments), v8EnvironmentFile, selectedDir, v8Flags.filter)
	failures = append(failures, staleSkips...)
	failures = append(failures, staleEnvironments...)
	t.Logf("V8: selected %d, passed %d, expected failures %d, skipped %d, failed %d, stale skip entries %d, stale environment entries %d",
		len(tests), passed, expected, skipped, failedTests, len(staleSkips), len(staleEnvironments))
	if testing.Verbose() {
		for _, reason := range slices.Sorted(maps.Keys(skipCounts)) {
			t.Logf("%d skipped: %s", skipCounts[reason], reason)
		}
	}
	v8Flags.reportFailures(t, failures)
}

// CheckFixedTime checks that helpers and test code observe the same configured time.
func (host *V8) CheckFixedTime(t *testing.T) {
	const file = "test/clock.js"
	checkout := fstest.MapFS{
		"test/helper.js": {
			Data: []byte("const helperTime = Date.now();"),
		},
		file: {
			Data: []byte(`if (helperTime !== 1000 || Date.now() !== helperTime) throw new Error("unexpected clock");`),
		},
	}
	suite := v8Suite{
		dir:     "test",
		helpers: []string{"helper.js"},
	}
	now := time.Unix(1, 0)
	environments := map[string]v8TestEnvironment{
		file: {
			FixedTime: &now,
		},
	}
	for _, test := range suite.collect(t, checkout, suite.dir, "", nil, environments) {
		result := test.run(t.Context(), host, checkout, testTimeout)
		if result.outcome != suitePassed {
			t.Errorf("%s: %s", test.file, result.reason)
		}
	}
}

type v8TestEnvironment struct {
	Comment   string     `json:"comment"`
	FixedTime *time.Time `json:"fixedTime"`
}

// CheckFilesOrder checks the execution order of helpers named by V8 Files directives.
func (host *V8) CheckFilesOrder(t *testing.T) {
	const file = "test/order.js"
	checkout := fstest.MapFS{
		"test/harness.js": {
			Data: []byte("var order = 'H';"),
		},
		"helpers/a.js": {
			Data: []byte("order += 'A';"),
		},
		"helpers/b.js": {
			Data: []byte("order += 'B';"),
		},
		file: {
			Data: []byte(`// Files: helpers/a.js helpers/b.js
// Files: helpers/a.js
if (order !== 'HABA') throw new Error(order);`),
		},
	}
	suite := v8Suite{
		dir:     "test",
		helpers: []string{"harness.js"},
	}
	for _, test := range suite.collect(t, checkout, file, "", nil, nil) {
		if result := test.run(t.Context(), host, checkout, testTimeout); result.outcome != suitePassed {
			t.Error(result.reason)
		}
	}
}

// CheckV8ScriptLoader checks nested script loads and error propagation through d8.file.execute.
func CheckV8ScriptLoader(t *testing.T) {
	checkout := fstest.MapFS{
		"helpers/append.mjs": {
			Data: []byte("var appended = true; order += 'A';"),
		},
		"helpers/nested.js": {
			Data: []byte("d8.file.execute('./helpers/append.mjs');"),
		},
		"helpers/throw.js": {
			Data: []byte("throw token;"),
		},
		"helpers/invalid.js": {
			Data: []byte("let x = ;"),
		},
	}
	var vm ekuma.VM
	realm := vm.NewRealm()
	ctx := t.Context()
	if err := installV8ScriptLoader(ctx, realm, checkout); err != nil {
		t.Fatal(err)
	}
	const src = `
		let order = '';
		const next = {toString() { order += 'B'; return 'helpers/nested.js'; }};
		function caller() {
			let order = 'local';
			return load('helpers/append.mjs', next);
		}
		if (caller() !== undefined || !globalThis.appended || order !== 'ABA') {
			throw new Error('load order: ' + order);
		}
		const token = {};
		function caught(...files) {
			try { d8.file.execute(...files); } catch (e) { return e; }
		}
		if (caught('helpers/throw.js', next) !== token || order !== 'ABA') {
			throw new Error('load did not stop at the thrown value');
		}
		if (caught({toString() { throw token; }}) !== token) throw new Error('expected conversion exception');
		if (!(caught('helpers/invalid.js') instanceof SyntaxError)) throw new Error('expected SyntaxError');
		if (caught('missing.js')?.constructor !== Error) throw new Error('expected file error');
	`
	if _, err := runScriptInRealm(ctx, realm, "test/mjsunit/loader.js", src); err != nil {
		t.Error(err)
	}
}

func readV8Environments(t *testing.T) map[string]v8TestEnvironment {
	t.Helper()
	data, err := os.ReadFile(v8EnvironmentFile)
	if err != nil {
		t.Fatal(err)
	}
	var environments map[string]v8TestEnvironment
	if err := json.Unmarshal(data, &environments, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("%s: %v", v8EnvironmentFile, err)
	}
	return environments
}

func readV8Skips(t *testing.T) skipList {
	t.Helper()
	data, err := os.ReadFile(v8SkipsFile)
	if err != nil {
		t.Fatal(err)
	}
	skips := skipList{}
	lines := strings.Split(string(data), "\n")
	var reason string
	for i := 0; i < len(lines); i++ {
		file := strings.TrimSpace(lines[i])
		if file == "" {
			reason = ""
			continue
		}
		if comment, ok := strings.CutPrefix(file, "#"); ok {
			value, ok := strings.CutPrefix(strings.TrimSpace(comment), "SKIP:")
			if !ok {
				reason = ""
				continue
			}
			reason = strings.TrimSpace(value)
			if reason == "" || i+1 >= len(lines) {
				t.Fatalf("%s:%d: SKIP needs a reason and a test path on the next line", v8SkipsFile, i+1)
			}
			i++
			file = strings.TrimSpace(lines[i])
		}
		if file == "" || strings.Contains(file, "#") {
			t.Fatalf("%s:%d: expected a test path on its own line", v8SkipsFile, i+1)
		}
		if _, exists := skips[file]; exists {
			t.Fatalf("duplicate path %s in %s", file, v8SkipsFile)
		}
		skips[file] = &skipEntry{
			reason: reason,
		}
	}
	return skips
}

type v8Test struct {
	file        string
	helpers     []*ekuma.UnboundScript
	flags       []string
	environment v8TestEnvironment
	skipReason  string
	wantFailure bool
}

func (s *v8Suite) collect(t *testing.T, checkout fs.FS, root, filter string, skips skipList, unseenEnvironments map[string]v8TestEnvironment) []v8Test {
	t.Helper()
	var helpers []*ekuma.UnboundScript
	for _, name := range s.helpers {
		file := path.Join(s.dir, name)
		src, err := fs.ReadFile(checkout, file)
		if err != nil {
			t.Fatal(err)
		}
		script, err := ekuma.ParseUnboundScript(file, string(src))
		if err != nil {
			t.Fatal(err)
		}
		helpers = append(helpers, script)
	}
	var tests []v8Test
	err := fs.WalkDir(checkout, root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(file, s.dir+"/")
		ext := path.Ext(file)
		if !entry.Type().IsRegular() || (ext != ".js" && ext != ".mjs") {
			return nil
		}
		if slices.Contains(s.helpers, name) || slices.Contains(s.exclude, name) || !strings.Contains(file, filter) {
			return nil
		}
		skipReason, listed := skips.match(file)
		environment := unseenEnvironments[file]
		delete(unseenEnvironments, file)
		tests = append(tests, v8Test{
			file:        file,
			helpers:     helpers,
			flags:       s.flags,
			environment: environment,
			skipReason:  skipReason,
			wantFailure: listed && skipReason == "",
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tests) == 0 && filter == "" {
		t.Errorf("no V8 tests found in %s", root)
	}
	return tests
}

func (v *v8Test) run(ctx context.Context, host *V8, checkout fs.FS, timeout time.Duration) suiteResult {
	result, lastPrint := v.execute(ctx, host, checkout, timeout)
	if v.wantFailure {
		result = result.expectFailure(v8SkipsFile)
	}
	if lastPrint != "" && result.outcome != suitePassed {
		result.reason += fmt.Sprintf(" (last print: %q)", lastPrint)
	}
	return result
}

type v8Directives struct {
	locale    string
	zone      string
	files     []string
	noHarness bool
}

func parseV8Directives(src string, enabledFlags []string) (v8Directives, string) {
	var directives v8Directives
	var flagLocale string
	env := map[string]string{}
	for line := range strings.SplitSeq(src, "\n") {
		comment, ok := strings.CutPrefix(strings.TrimSpace(line), "//")
		if !ok {
			continue
		}
		key, value, _ := strings.Cut(strings.TrimSpace(comment), ":")
		switch key {
		case "Flags":
			for flag := range strings.FieldsSeq(value) {
				normalized := strings.ReplaceAll(flag, "_", "-")
				if l, ok := strings.CutPrefix(normalized, "--icu-locale="); ok {
					flagLocale = l
				} else if !slices.Contains(enabledFlags, normalized) {
					// TODO: Review mjsunit flags and enable those whose behavior Ekuma provides.
					return v8Directives{}, "requires V8 flag " + flag
				}
			}
		case "Environment Variables":
			for pair := range strings.FieldsSeq(value) {
				name, v, _ := strings.Cut(pair, "=")
				switch name {
				case "LC_ALL", "LC_MESSAGES", "LANG", "TZ":
					env[name] = v
				default:
					return v8Directives{}, "requires environment setting " + pair
				}
			}
		case "Files":
			directives.files = append(directives.files, strings.Fields(value)...)
		case "NO HARNESS":
			directives.noHarness = true
		}
	}
	envLocale := cmp.Or(env["LC_ALL"], env["LC_MESSAGES"], env["LANG"])
	envLocale, _, _ = strings.Cut(envLocale, ".")
	directives.locale = strings.ReplaceAll(cmp.Or(flagLocale, envLocale, "en-US"), "_", "-")
	directives.zone = cmp.Or(env["TZ"], "UTC")
	return directives, ""
}

func (v *v8Test) execute(ctx context.Context, host *V8, checkout fs.FS, timeout time.Duration) (result suiteResult, lastPrint string) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case v8ExitCode:
			if r == 0 {
				result = suiteResult{}
			} else {
				result = suiteFailed.result(fmt.Sprintf("quit(%d)", r))
			}
		default:
			result = suiteBroken.result(fmt.Sprintf("engine panic: %v", r))
		}
	}()
	if v.skipReason != "" {
		return suiteSkipped.result(v.skipReason), ""
	}
	src, err := fs.ReadFile(checkout, v.file)
	if err != nil {
		return suiteBroken.result(err.Error()), ""
	}
	source := string(src)
	directives, unsupported := parseV8Directives(source, v.flags)
	if unsupported != "" {
		return suiteSkipped.result(unsupported), ""
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var vm ekuma.VM
	if err := vm.SetLocale(directives.locale); err != nil {
		return suiteBroken.result(firstLine(err.Error())), ""
	}
	loc, err := time.LoadLocation(directives.zone)
	if err != nil {
		return suiteBroken.result(firstLine(err.Error())), ""
	}
	vm.SetTimeZone(loc)
	if v.environment.FixedTime != nil {
		now := *v.environment.FixedTime
		vm.SetClock(func() time.Time { return now })
	}
	print := func(this ekuma.Value, args []ekuma.Value) (any, error) {
		lastPrint = firstLine(fmt.Sprint(args))
		return ekuma.Undefined, nil
	}
	timeouts := v8Timeouts{
		invoke: host.InvokeInRealm,
	}
	if inSuiteDir(v.file, "test/mjsunit") {
		realms := v8Realms{
			host:     host,
			vm:       &vm,
			ctx:      ctx,
			checkout: checkout,
			print:    print,
			shell:    newV8Shell(io.Discard, io.Discard),
			timeouts: &timeouts,
		}
		if _, err := realms.add(vm.InitialRealm(), false); err != nil {
			return suiteBroken.result(firstLine(err.Error())), ""
		}
	} else if err := vm.Global().Set("print", vm.FuncOf("print", print)); err != nil {
		return suiteBroken.result(firstLine(err.Error())), ""
	}
	// A later timer can attach a handler before d8 reports unhandled rejections.
	host.DeferUnhandledRejections(&vm)
	helpers := v.helpers
	if directives.noHarness {
		helpers = nil
	}
	for _, helper := range helpers {
		if _, err := vm.BindScript(helper).Evaluate(ctx); err != nil {
			reason := firstLine("V8 harness: " + err.Error())
			if errors.Is(err, context.DeadlineExceeded) {
				return timedOut(reason), ""
			}
			return suiteBroken.result(reason), ""
		}
	}
	loader := moduleLoader{
		vm:      &vm,
		dir:     filepath.Dir(filepath.FromSlash(v.file)),
		modules: map[string]*ekuma.Module{},
		readFile: func(name string) ([]byte, error) {
			return fs.ReadFile(checkout, filepath.ToSlash(name))
		},
	}
	vm.SetModuleLoader(loader.load)
	for _, name := range directives.files {
		b, err := fs.ReadFile(checkout, name)
		if err != nil {
			return suiteBroken.result(fmt.Sprintf("V8 Files: %s: %v", name, err)), lastPrint
		}
		result = runV8File(ctx, &loader, &timeouts, name, string(b))
		if result.outcome != suitePassed {
			result.reason = name + ": " + result.reason
			break
		}
	}
	if result.outcome == suitePassed {
		result = runV8File(ctx, &loader, &timeouts, v.file, source)
	}
	result = timeouts.drain(ctx, result)
	if result.outcome != suitePassed {
		return result, lastPrint
	}
	if err := host.TakeUnhandledRejection(&vm); err != nil {
		return scriptErrorResult(err), lastPrint
	}
	return suiteResult{}, lastPrint
}

func installV8ScriptLoader(ctx context.Context, realm *ekuma.Realm, checkout fs.FS) error {
	global := realm.Global()
	errorCtor, err := global.Get("Error")
	if err != nil {
		return err
	}
	syntaxErrorCtor, err := global.Get("SyntaxError")
	if err != nil {
		return err
	}
	toString, err := runScriptInRealm(ctx, realm, "d8", "name => `${name}`")
	if err != nil {
		return err
	}
	throw := func(ctor ekuma.Value, message string) error {
		e, err := ctor.New(ctx, message)
		if err != nil {
			return err
		}
		return &ekuma.ScriptError{
			Value: e,
		}
	}
	execute := func(this ekuma.Value, args []ekuma.Value) (any, error) {
		for _, arg := range args {
			str, err := toString.Invoke(ctx, arg)
			if err != nil {
				return nil, err
			}
			name := str.String()
			src, err := fs.ReadFile(checkout, path.Clean(filepath.ToSlash(name)))
			if err != nil {
				return nil, throw(errorCtor, "Error loading file: "+name)
			}
			// d8 executes a classic script even when its name ends in .mjs.
			if _, err := runScriptInRealm(ctx, realm, filepath.FromSlash(name), string(src)); err != nil {
				if se, ok := err.(*ekuma.SyntaxError); ok {
					return nil, throw(syntaxErrorCtor, se.Message)
				}
				return nil, err
			}
		}
		return ekuma.Undefined, nil
	}
	if err := global.Set("load", realm.FuncOf("load", execute)); err != nil {
		return err
	}
	return global.Set("d8", realm.ValueOf(map[string]any{
		"file": map[string]any{
			"execute": realm.FuncOf("execute", execute),
		},
	}))
}

func runV8File(ctx context.Context, loader *moduleLoader, timeouts *v8Timeouts, name, src string) suiteResult {
	file := filepath.FromSlash(name)
	if path.Ext(name) == ".mjs" {
		p, _, err := loader.evaluate(ctx, file, src)
		if err != nil {
			return scriptErrorResult(err)
		}
		// d8 pumps tasks before checking the module's evaluation promise. A
		// timer exception during this checkpoint does not fail the module.
		if result := timeouts.drain(ctx, suiteResult{}); result.outcome == suiteBroken {
			return result
		}
		switch state, reason := p.PromiseState(); state {
		case ekuma.PromisePending:
			return suiteFailed.result("module evaluation is still pending")
		case ekuma.PromiseRejected:
			return scriptErrorResult(&ekuma.ScriptError{
				Value: reason,
			})
		}
	} else if _, err := runScript(ctx, loader.vm, file, src); err != nil {
		return scriptErrorResult(err)
	}
	return suiteResult{}
}
