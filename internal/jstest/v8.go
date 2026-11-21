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
	"runtime"
	"slices"
	"strconv"
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
	ObjectCreationRealm      func(ekuma.Value) *ekuma.Realm
	ParseScriptValue         func(*ekuma.Realm, string, ekuma.Value) (*ekuma.Script, error)
	CallScript               func(*ekuma.Script) ekuma.Value
	EvaluateScriptTask       func(context.Context, *ekuma.Script) (bool, error)
	InstallGlobalProxy       func(*ekuma.Realm, ekuma.Value, *ekuma.Realm, bool) ekuma.Value
	NewRealmShared           func(*ekuma.VM) ekuma.Value
	DetachGlobalProxy        func(*ekuma.Realm)
	CanAccessReceiver        func(*ekuma.Realm, ekuma.Value) bool
	InvokeInRealm            func(context.Context, *ekuma.Realm, ekuma.Value) error
	SetExceptionReporter     func(*ekuma.Realm, ekuma.Value)
	RequestInterrupt         func(*ekuma.Realm, error) func()
	InterruptNow             func(*ekuma.Realm, error)
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
		flags: []string{
			"--bundle",
			"--expose-gc",
			"--throws",

			// Ekuma uses time zone rules and localized names for Date.
			"--icu-timezone-data",

			// Ekuma enables these language features by default.
			"--harmony-shadow-realm",
			"--harmony-top-level-await",
			"--js-decorators",
			"--js-defer-import-eval",
			"--js-explicit-resource-management",
			"--js-immutable-arraybuffer",
			"--js-iterator-chunking",
			"--js-iterator-includes",
			"--js-iterator-join",
			"--js-iterator-sequencing",
			"--js-joint-iteration",
			"--js-nonextensible-applies-to-private",

			// At the pinned revision the staged JS features are immutable buffers,
			// byte imports, deferred imports, iterator chunking and restricting
			// private field addition on non-extensible objects. Older tests use
			// --es-staging for staged language features.
			"--es-staging",
			"--harmony",
			"--js-staging",

			// The module loader resolves the exact filename in the specifier.
			"--no-fuzzy-module-file-extensions",

			// Ekuma compiles nested functions with their enclosing code.
			"--no-lazy",
			"--nolazy",
		},
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
		test := tests[i]
		test.checkoutDir = dir
		return test.run(ctx, host, checkout, timeout)
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

// CheckThrows checks the whole-run outcome of an mjsunit test with --throws.
func (host *V8) CheckThrows(t *testing.T) {
	test := v8Test{
		file:  "test/mjsunit/throws.js",
		flags: []string{"--throws"},
	}
	check := func(src string, want suiteOutcome) {
		t.Helper()
		checkout := fstest.MapFS{
			test.file: {
				Data: []byte("// Flags: --throws\n" + src),
			},
		}
		if got := test.run(t.Context(), host, checkout, testTimeout); got.outcome != want {
			t.Errorf("%s: outcome = %v, want %v (%s)", src, got.outcome, want, got.reason)
		}
	}
	check("try { throw 1; } catch {}", suiteFailed)
	check("setTimeout(() => Promise.reject(1));", suitePassed)
	check("// Flags: --ignore-unhandled-promises\nPromise.reject(1);", suiteFailed)
	check("// Files: missing.js", suiteBroken)
	check("// Flags: --allow-natives-syntax\nthrow 1;", suiteSkipped)
	check("quit(1);", suiteFailed)
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

// CheckScriptLoader checks d8 file lookups and nested script loads.
func (host *V8) CheckScriptLoader(t *testing.T) {
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
		"nested": {
			Mode: fs.ModeSymlink,
			Data: []byte("helpers/subdir"),
		},
		"helpers/subdir": {
			Mode: fs.ModeDir,
		},
	}
	var vm ekuma.VM
	realm := vm.NewRealm()
	ctx := t.Context()
	scripts := newV8Scripts(host, realm, checkout, ".")
	if err := scripts.installLoader(ctx, realm); err != nil {
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
		const exists = d8.file.exists;
		const filename = {toString() { return './helpers/nested.js\x00suffix'; }};
		if (!exists(filename) || !exists('nested/../../helpers/append.mjs') ||
			exists('') || exists('missing/../helpers')) {
			throw new Error('unexpected file lookup');
		}
		try {
			exists({toString() { throw token; }});
			throw new Error('expected filename conversion to throw');
		} catch (e) {
			if (e !== token) throw e;
		}
	`
	if _, err := runScriptInRealm(ctx, realm, "test/mjsunit/loader.js", src); err != nil {
		t.Error(err)
	}
}

// CheckDataURL checks module identity and relative imports from a data URL.
func (host *V8) CheckDataURL(t *testing.T) {
	const src = `
		const payload = "export const url = import.meta.url; export { token } from './dep.mjs'; // %20+#\\raw";
		const url = "data:text/javascript," + payload;
		const ns = await import(url);
		if (ns.url !== url) throw Error(ns.url);
		if (ns !== await import(url + "\0suffix")) throw Error("module identity");
		const text = await import(url, {with: {type: "text"}});
		if (text.default !== payload) throw Error("text payload");
		const file = await import("../../data:text/javascript,0", {with: {type: "text"}});
		if (file.default !== "from file") throw Error("expected a filesystem import");
		Realm.shared = url;
		const child = Realm.createAllowCrossRealmAccess();
		const other = await Realm.eval(child, "import(Realm.shared)");
		if (ns.token === other.token) throw Error("shared module across realms");
	`
	host.checkRealmScript(t, "test/mjsunit/data-url.mjs", src, fstest.MapFS{
		"dep.mjs": {
			Data: []byte("export const token = {};"),
		},
		"data:text/javascript,0": {
			Data: []byte("from file"),
		},
	})
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
	var group skipEntry
	for i := 0; i < len(lines); i++ {
		file := strings.TrimSpace(lines[i])
		if file == "" {
			group = skipEntry{}
			continue
		}
		if comment, ok := strings.CutPrefix(file, "#"); ok {
			group = skipEntry{}
			directive, value, ok := strings.Cut(strings.TrimSpace(comment), ":")
			if !ok {
				continue
			}
			switch directive {
			case "SKIP":
			case "SKIP(64-bit)":
				group.inactive = strconv.IntSize != 64
			default:
				continue
			}
			group.reason = strings.TrimSpace(value)
			if group.reason == "" || i+1 >= len(lines) {
				t.Fatalf("%s:%d: %s needs a reason and a test path on the next line", v8SkipsFile, i+1, directive)
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
		entry := group
		skips[file] = &entry
	}
	return skips
}

type v8Test struct {
	checkoutDir string
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
	locale                  string
	zone                    string
	files                   []string
	bundle                  bool
	noHarness               bool
	exposeGC                bool
	throws                  bool
	ignoreUnhandledPromises bool
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
				} else if normalized == "--ignore-unhandled-promises" {
					directives.ignoreUnhandledPromises = true
				} else if !slices.Contains(enabledFlags, normalized) {
					return v8Directives{}, "requires V8 flag " + flag
				} else if normalized == "--bundle" {
					directives.bundle = true
				} else if normalized == "--expose-gc" {
					directives.exposeGC = true
				} else if normalized == "--throws" {
					directives.throws = true
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
	var directives v8Directives
	defer func() {
		switch r := recover().(type) {
		case nil:
			// d8 applies --throws to the whole run, after reporting rejections.
			// An explicit quit exits with its own status.
			if directives.throws {
				switch result.outcome {
				case suitePassed:
					result = suiteFailed.result("--throws run completed successfully")
				case suiteFailed:
					result = suiteResult{}
				}
			}
		case v8GCWithArguments:
			result = suiteSkipped.result("requires V8 gc with arguments")
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
	directives, unsupported := parseV8Directives(string(src), v.flags)
	if unsupported != "" {
		return suiteSkipped.result(unsupported), ""
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var vm ekuma.VM
	if directives.exposeGC {
		v8GCEnabled.Store(&vm, struct{}{})
		defer v8GCEnabled.Delete(&vm)
	}
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
	loop := v8EventLoop{
		host: host,
	}
	cwd, err := filepath.Abs(v.checkoutDir)
	if err != nil {
		return suiteBroken.result(err.Error()), ""
	}
	scripts := newV8Scripts(host, vm.InitialRealm(), checkout, cwd)
	scripts.entry = v.file
	if directives.bundle {
		scripts.bundle = &v8Bundle{
			modules: map[string]string{},
		}
	}
	if inSuiteDir(v.file, "test/mjsunit") {
		realms := v8Realms{
			host:    host,
			vm:      &vm,
			ctx:     ctx,
			print:   print,
			shell:   newV8Shell(io.Discard, io.Discard),
			loop:    &loop,
			scripts: scripts,
			shared:  host.NewRealmShared(&vm),
		}
		defer realms.shell.close()
		if _, err := realms.add(scripts.current, false); err != nil {
			return suiteBroken.result(firstLine(err.Error())), ""
		}
	} else if err := vm.Global().Set("print", vm.FuncOf("print", print)); err != nil {
		return suiteBroken.result(firstLine(err.Error())), ""
	}
	// A later timer can attach a handler before d8 reports unhandled rejections.
	if !directives.ignoreUnhandledPromises {
		host.DeferUnhandledRejections(&vm)
	}
	helpers := v.helpers
	if directives.noHarness {
		helpers = nil
	}
	for _, helper := range helpers {
		if completed, err := scripts.evaluate(ctx, scripts.current.BindScript(helper)); err != nil {
			if errors.Is(err, errV8Termination) {
				return loop.terminationResult(ctx, completed), lastPrint
			}
			reason := firstLine("V8 harness: " + err.Error())
			if errors.Is(err, context.DeadlineExceeded) {
				return timedOut(reason), ""
			}
			return suiteBroken.result(reason), ""
		}
	}
	vm.SetModuleLoader(scripts.loadModule)
	vm.SetImportMeta(func(m *ekuma.Module) map[string]any {
		return map[string]any{"url": m.Name()}
	})
	for _, name := range directives.files {
		result = scripts.runFile(ctx, &loop, name, nil)
		if result.outcome != suitePassed {
			result.reason = name + ": " + result.reason
			break
		}
		if loop.terminated {
			return result, lastPrint
		}
	}
	if result.outcome == suitePassed {
		result = scripts.runFile(ctx, &loop, v.file, src)
	}
	result = loop.drain(ctx, result)
	if loop.terminated || result.outcome != suitePassed || directives.ignoreUnhandledPromises {
		return result, lastPrint
	}
	if err := host.TakeUnhandledRejection(&vm); err != nil {
		return scriptErrorResult(err), lastPrint
	}
	return suiteResult{}, lastPrint
}

// v8Scripts tracks d8's selection across classic script execution boundaries.
type v8Scripts struct {
	host     *V8
	current  *ekuma.Realm
	next     *ekuma.Realm
	checkout fs.FS
	cwd      string
	entry    string
	contexts map[*ekuma.Realm]*v8ScriptContext
	bundle   *v8Bundle
}

type v8ScriptContext struct {
	modules         moduleLoader
	errorCtor       ekuma.Value
	syntaxErrorCtor ekuma.Value
}

func newV8Scripts(host *V8, r *ekuma.Realm, checkout fs.FS, cwd string) *v8Scripts {
	return &v8Scripts{
		host:     host,
		current:  r,
		next:     r,
		checkout: checkout,
		cwd:      cwd,
		contexts: map[*ekuma.Realm]*v8ScriptContext{},
	}
}

func (s *v8Scripts) contextFor(r *ekuma.Realm) *v8ScriptContext {
	if c := s.contexts[r]; c != nil {
		return c
	}
	c := &v8ScriptContext{
		modules: moduleLoader{
			realm:    r,
			modules:  map[string]*ekuma.Module{},
			readFile: s.readModuleFile,
		},
	}
	s.contexts[r] = c
	return c
}

const v8DataURLPrefix = "data:text/javascript,"

func v8ModuleURL(name string) bool {
	return strings.HasPrefix(name, v8DataURLPrefix) || strings.HasPrefix(name, "http://") || strings.HasPrefix(name, "https://")
}

func v8ModuleName(dir, name string) string {
	if v8ModuleURL(name) {
		return name
	}
	// d8 tests for an absolute path before replacing backslashes. On Windows
	// it recognizes only paths starting with a drive and a separator.
	absolute := strings.HasPrefix(name, "/")
	if runtime.GOOS == "windows" {
		absolute = len(name) >= 3 && name[1] == ':' && os.IsPathSeparator(name[2])
	}
	if absolute {
		dir = ""
	}
	return filepath.ToSlash(filepath.Join(dir, strings.ReplaceAll(name, `\`, "/")))
}

func (s *v8Scripts) readModuleFile(name string) ([]byte, error) {
	if s.bundle != nil {
		if src, ok := s.bundle.modules[name]; ok {
			return []byte(src), nil
		}
	}
	if src, ok := strings.CutPrefix(name, v8DataURLPrefix); ok {
		return []byte(src), nil
	}
	file, err := filepath.Rel(s.cwd, name)
	if err != nil {
		return nil, err
	}
	return fs.ReadFile(s.checkout, filepath.ToSlash(file))
}

func (s *v8Scripts) loadModule(req ekuma.ModuleRequest) (*ekuma.Module, error) {
	loader := &s.contextFor(req.Realm).modules
	typ, typed := req.Attributes["type"]
	if strings.HasPrefix(req.Specifier, v8DataURLPrefix) {
		// d8 converts the specifier to a NUL-terminated UTF-8 string.
		name, _, _ := strings.Cut(req.Specifier, "\x00")
		if !typed && strings.HasSuffix(name, ".wasm") {
			return nil, errors.New("d8: WebAssembly modules are only supported in source phase imports")
		}
		// d8 reads byte modules from files. A data URL supplies source text.
		if typed && typ != "text" && typ != "json" {
			return nil, fmt.Errorf("d8: unsupported data URL module type %q", typ)
		}
		return loader.loadFile(name, typ)
	}
	dir := s.cwd
	if !v8ModuleURL(req.ReferrerName) {
		referrer := cmp.Or(req.ReferrerName, s.entry)
		dir = filepath.Dir(v8ModuleName(s.cwd, referrer))
	}
	return loader.loadFile(v8ModuleName(dir, req.Specifier), typ)
}

func (s *v8Scripts) evaluate(ctx context.Context, script *ekuma.Script) (bool, error) {
	completed, err := s.host.EvaluateScriptTask(ctx, script)
	s.current = s.next
	return completed, err
}

func (s *v8Scripts) call(script *ekuma.Script) {
	// A nested load applies a pending realm switch even when the script throws.
	defer func() { s.current = s.next }()
	s.host.CallScript(script)
}

func (s *v8Scripts) installLoader(ctx context.Context, realm *ekuma.Realm) error {
	global := realm.Global()
	errorCtor, err := global.Get("Error")
	if err != nil {
		return err
	}
	syntaxErrorCtor, err := global.Get("SyntaxError")
	if err != nil {
		return err
	}
	c := s.contextFor(realm)
	c.errorCtor = errorCtor
	c.syntaxErrorCtor = syntaxErrorCtor
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
			selected := s.contextFor(s.current)
			src, err := fs.ReadFile(s.checkout, path.Clean(filepath.ToSlash(name)))
			if err != nil {
				return nil, throw(selected.errorCtor, "Error loading file: "+name)
			}
			// d8 executes a classic script even when its name ends in .mjs.
			script, err := s.current.ParseScript(filepath.FromSlash(name), string(src))
			if err != nil {
				if se, ok := err.(*ekuma.SyntaxError); ok {
					return nil, throw(selected.syntaxErrorCtor, se.Message)
				}
				return nil, err
			}
			s.call(script)
		}
		return ekuma.Undefined, nil
	}
	if err := global.Set("load", realm.FuncOf("load", execute)); err != nil {
		return err
	}
	exists := func(this ekuma.Value, args []ekuma.Value) (any, error) {
		if len(args) == 0 {
			return nil, throw(errorCtor, "exists() takes one argument")
		}
		str, err := toString.Invoke(ctx, args[0])
		if err != nil {
			return nil, err
		}
		name, _, _ := strings.Cut(str.String(), "\x00")
		return v8FileExists(s.checkout, name), nil
	}
	return global.Set("d8", realm.ValueOf(map[string]any{
		"file": map[string]any{
			"execute": realm.FuncOf("execute", execute),
			"exists":  realm.FuncOf("exists", exists),
		},
	}))
}

func v8FileExists(checkout fs.FS, name string) bool {
	name = filepath.ToSlash(name)
	if name == "" || strings.HasPrefix(name, "/") || filepath.VolumeName(name) != "" {
		return false
	}
	// io/fs takes clean names. Resolve links and check intervening directories
	// before applying ".." or a trailing separator.
	prefix := "."
	var links int
	for {
		part, rest, more := strings.Cut(name, "/")
		if part == ".." && prefix == "." {
			return false
		}
		entry := path.Join(prefix, part)
		info, err := fs.Lstat(checkout, entry)
		if err != nil {
			return false
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			target, err := fs.ReadLink(checkout, entry)
			if err != nil {
				return false
			}
			links++
			target = filepath.ToSlash(target)
			if links > 40 || target == "" || strings.HasPrefix(target, "/") || filepath.VolumeName(target) != "" {
				return false
			}
			name = target
			if more {
				name += "/" + rest
			}
			continue
		}
		if more && !info.IsDir() {
			return false
		}
		if !more {
			return true
		}
		prefix = entry
		name = rest
	}
}

func (s *v8Scripts) runFile(ctx context.Context, loop *v8EventLoop, name string, src []byte) suiteResult {
	if path.Ext(name) == ".mjs" {
		return s.runModule(ctx, loop, v8ModuleName(s.cwd, name))
	}
	file := filepath.FromSlash(name)
	if src == nil {
		var err error
		src, err = fs.ReadFile(s.checkout, name)
		if err != nil {
			return suiteBroken.result(err.Error())
		}
	}
	source := string(src)
	if s.bundle != nil {
		if parts := splitV8Bundle(source); parts != nil {
			return s.runBundle(ctx, loop, file, parts)
		}
	}
	return s.runScript(ctx, loop, file, source)
}

func (s *v8Scripts) runScript(ctx context.Context, loop *v8EventLoop, file, src string) suiteResult {
	script, err := s.current.ParseScript(file, src)
	if err != nil {
		return scriptErrorResult(err)
	}
	if completed, err := s.evaluate(ctx, script); err != nil {
		if errors.Is(err, errV8Termination) {
			return loop.terminationResult(ctx, completed)
		}
		return scriptErrorResult(err)
	}
	return suiteResult{}
}

func (s *v8Scripts) runModule(ctx context.Context, loop *v8EventLoop, name string) suiteResult {
	mod, err := s.contextFor(s.current).modules.loadFile(name, "")
	if err != nil {
		return loop.scriptError(ctx, err)
	}
	p, err := mod.Evaluate(ctx)
	if err != nil {
		return loop.scriptError(ctx, err)
	}
	// d8 pumps tasks before checking the module's evaluation promise. A
	// timer exception during this checkpoint does not fail the module.
	if result := loop.drain(ctx, suiteResult{}); result.outcome == suiteBroken {
		return result
	}
	if loop.terminated {
		return suiteResult{}
	}
	switch state, reason := p.PromiseState(); state {
	case ekuma.PromisePending:
		return suiteFailed.result("module evaluation is still pending")
	case ekuma.PromiseRejected:
		return loop.scriptError(ctx, &ekuma.ScriptError{
			Value: reason,
		})
	}
	return suiteResult{}
}
