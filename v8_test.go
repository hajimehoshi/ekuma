// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
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

const v8SkipsFile = "testdata/v8-skips.txt"
const v8EnvironmentFile = "testdata/v8-environment.json"

var v8Flags = registerSuiteFlags("v8")

type v8Suite struct {
	dir     string
	helpers []string
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
}

func TestV8(t *testing.T) {
	dir := v8Flags.checkoutDir(t)
	unseenSkips := readV8Skips(t)
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
		tests = append(tests, suite.collect(t, checkout, root, v8Flags.filter, unseenSkips, unseenEnvironments)...)
	}
	if !matchedSuite {
		t.Fatalf("no enabled V8 suite in %q", selectedDir)
	}
	results := make([]suiteResult, len(tests))
	ctx := t.Context()
	run := func(i int, timeout time.Duration) suiteResult {
		return tests[i].run(ctx, checkout, timeout)
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
	staleSkips := staleEntries(maps.Keys(unseenSkips), v8SkipsFile, selectedDir, v8Flags.filter)
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

func TestV8FixedTime(t *testing.T) {
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
		result := test.run(t.Context(), checkout, testTimeout)
		if result.outcome != suitePassed {
			t.Errorf("%s: %s", test.file, result.reason)
		}
	}
}

type v8TestEnvironment struct {
	Comment   string     `json:"comment"`
	FixedTime *time.Time `json:"fixedTime"`
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

func readV8Skips(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(v8SkipsFile)
	if err != nil {
		t.Fatal(err)
	}
	// An empty reason marks an expected failure that still runs.
	skips := map[string]string{}
	lines := strings.Split(string(data), "\n")
	for i := 0; i < len(lines); i++ {
		file := strings.TrimSpace(lines[i])
		if file == "" {
			continue
		}
		var reason string
		if comment, ok := strings.CutPrefix(file, "#"); ok {
			value, ok := strings.CutPrefix(strings.TrimSpace(comment), "SKIP:")
			if !ok {
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
		skips[file] = reason
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

func (s *v8Suite) collect(t *testing.T, checkout fs.FS, root, filter string, unseenSkips map[string]string, unseenEnvironments map[string]v8TestEnvironment) []v8Test {
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
		if !entry.Type().IsRegular() || path.Ext(file) != ".js" || slices.Contains(s.helpers, name) {
			return nil
		}
		if !strings.Contains(file, filter) {
			return nil
		}
		skipReason, listed := unseenSkips[file]
		delete(unseenSkips, file)
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

func (v *v8Test) run(ctx context.Context, checkout fs.FS, timeout time.Duration) (result suiteResult) {
	defer func() {
		if r := recover(); r != nil {
			result = suiteBroken.result(fmt.Sprintf("engine panic: %v", r))
		}
	}()
	result, lastPrint := v.execute(ctx, checkout, timeout)
	if v.wantFailure {
		result = result.expectFailure(v.file, v8SkipsFile)
	}
	if lastPrint != "" && result.outcome != suitePassed {
		result.reason += fmt.Sprintf(" (last print: %q)", lastPrint)
	}
	return result
}

// v8Environment translates d8 options into VM settings and reports any unsupported directive.
func v8Environment(src string, enabledFlags []string) (locale, zone, unsupported string) {
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
					return "", "", "requires V8 flag " + flag
				}
			}
		case "Environment Variables":
			for pair := range strings.FieldsSeq(value) {
				name, v, _ := strings.Cut(pair, "=")
				switch name {
				case "LC_ALL", "LC_MESSAGES", "LANG", "TZ":
					env[name] = v
				default:
					return "", "", "requires environment setting " + pair
				}
			}
		case "Files":
			return "", "", "requires additional scripts: " + strings.TrimSpace(value)
		}
	}
	locale = cmp.Or(flagLocale, env["LC_ALL"], env["LC_MESSAGES"], env["LANG"], "en-US")
	return strings.ReplaceAll(locale, "_", "-"), cmp.Or(env["TZ"], "UTC"), ""
}

func (v *v8Test) execute(ctx context.Context, checkout fs.FS, timeout time.Duration) (suiteResult, string) {
	if v.skipReason != "" {
		return suiteSkipped.result(v.skipReason), ""
	}
	src, err := fs.ReadFile(checkout, v.file)
	if err != nil {
		return suiteBroken.result(err.Error()), ""
	}
	source := string(src)
	locale, zone, unsupported := v8Environment(source, v.flags)
	if unsupported != "" {
		return suiteSkipped.result(unsupported), ""
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var vm ekuma.VM
	if err := vm.SetLocale(locale); err != nil {
		return suiteBroken.result(firstLine(err.Error())), ""
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return suiteBroken.result(firstLine(err.Error())), ""
	}
	vm.SetTimeZone(loc)
	if v.environment.FixedTime != nil {
		now := *v.environment.FixedTime
		vm.SetClock(func() time.Time { return now })
	}
	var lastPrint string
	print := vm.FuncOf("print", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		lastPrint = firstLine(fmt.Sprint(args))
		return ekuma.Undefined, nil
	})
	if err := vm.Global().Set("print", print); err != nil {
		return suiteBroken.result(firstLine(err.Error())), ""
	}
	for _, helper := range v.helpers {
		if _, err := vm.BindScript(helper).Evaluate(ctx); err != nil {
			reason := firstLine("V8 harness: " + err.Error())
			if errors.Is(err, context.DeadlineExceeded) {
				return timedOut(reason), ""
			}
			return suiteBroken.result(reason), ""
		}
	}
	if _, err := runScript(ctx, &vm, v.file, source); err != nil {
		// A ScriptError retains its VM through the thrown value.
		return scriptErrorResult(err), lastPrint
	}
	return suiteResult{}, lastPrint
}
