// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"cmp"
	"context"
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
	"time"

	"github.com/hajimehoshi/ekuma"
	_ "github.com/hajimehoshi/ekuma/intl"
)

const v8SkipsFile = "testdata/v8-skips.txt"

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
	if testing.Short() {
		t.Skip("skipping V8 tests in short mode")
	}
	dir := os.Getenv("EKUMA_V8")
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			t.Skip("no user cache directory; set EKUMA_V8 to a V8 checkout")
		}
		dir = filepath.Join(cache, "ekuma", "v8")
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			t.Skipf("no V8 checkout at %s; set EKUMA_V8", dir)
		}
	}
	unseenSkips := readV8Skips(t)
	checkout := os.DirFS(dir)
	selectedDir := path.Clean(filepath.ToSlash(v8Flags.dir))
	if !fs.ValidPath(selectedDir) {
		t.Fatalf("-v8.dir must be relative to the V8 checkout: %q", v8Flags.dir)
	}
	var tests []v8Test
	var matchedSuite bool
	for _, suite := range v8Suites {
		root := suite.dir
		if inV8Dir(selectedDir, suite.dir) {
			root = selectedDir
		} else if !inV8Dir(suite.dir, selectedDir) {
			continue
		}
		matchedSuite = true
		tests = append(tests, suite.collect(t, checkout, root, v8Flags.filter, unseenSkips)...)
	}
	if !matchedSuite {
		t.Fatalf("no enabled V8 suite in %q", selectedDir)
	}
	results := make([]v8Result, len(tests))
	ctx := t.Context()
	v8Flags.run(t, len(tests), func(i int) {
		results[i] = tests[i].run(ctx, checkout, testTimeout)
	})
	for i, result := range results {
		if result.timedOut {
			results[i] = tests[i].run(ctx, checkout, retryTimeout)
		}
	}

	var failures []suiteFailure
	var passed, expected, skipped int
	skipCounts := map[string]int{}
	for i, result := range results {
		switch {
		case result.err != nil:
			failures = append(failures, suiteFailure{
				path:   tests[i].file,
				reason: result.err.Error(),
			})
		case result.skipReason != "":
			skipped++
			skipCounts[result.skipReason]++
		case tests[i].wantFailure:
			expected++
		default:
			passed++
		}
	}
	failedTests := len(failures)
	for _, file := range slices.Sorted(maps.Keys(unseenSkips)) {
		if inV8Dir(file, selectedDir) && strings.Contains(file, v8Flags.filter) {
			failures = append(failures, suiteFailure{
				path:   file,
				reason: "not in the suite: remove it from " + v8SkipsFile,
			})
		}
	}
	t.Logf("V8: selected %d, passed %d, expected failures %d, skipped %d, failed %d, stale skip entries %d",
		len(tests), passed, expected, skipped, failedTests, len(failures)-failedTests)
	if testing.Verbose() {
		for _, reason := range slices.Sorted(maps.Keys(skipCounts)) {
			t.Logf("%d skipped: %s", skipCounts[reason], reason)
		}
	}
	v8Flags.reportFailures(t, failures)
}

func inV8Dir(file, dir string) bool {
	return dir == "." || file == dir || strings.HasPrefix(file, dir+"/")
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
	skipReason  string
	wantFailure bool
}

type v8Result struct {
	skipReason string
	err        error
	timedOut   bool
}

func (s *v8Suite) collect(t *testing.T, checkout fs.FS, root, filter string, unseenSkips map[string]string) []v8Test {
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
		tests = append(tests, v8Test{
			file:        file,
			helpers:     helpers,
			flags:       s.flags,
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

func (v *v8Test) run(ctx context.Context, checkout fs.FS, timeout time.Duration) (result v8Result) {
	defer func() {
		if r := recover(); r != nil {
			result.err = fmt.Errorf("engine panic: %v", r)
		}
	}()
	if v.skipReason != "" {
		result.skipReason = v.skipReason
		return result
	}
	src, err := fs.ReadFile(checkout, v.file)
	if err != nil {
		result.err = err
		return result
	}
	source := string(src)
	locale, zone, unsupported := v8Environment(source, v.flags)
	if unsupported != "" {
		if v.wantFailure {
			result.err = fmt.Errorf("cannot run (%s): remove %s from %s", unsupported, v.file, v8SkipsFile)
			return result
		}
		result.skipReason = unsupported
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if lastPrint, err := v.eval(ctx, source, locale, zone); err != nil {
		result.timedOut = errors.Is(err, context.DeadlineExceeded)
		// A ScriptError retains its VM through the thrown value.
		message := firstLine(err.Error())
		if lastPrint != "" {
			message += fmt.Sprintf(" (last print: %q)", lastPrint)
		}
		result.err = errors.New(message)
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

// eval returns an unexpected test error together with the last print output for context.
func (v *v8Test) eval(ctx context.Context, src, locale, zone string) (string, error) {
	var vm ekuma.VM
	if err := vm.SetLocale(locale); err != nil {
		return "", err
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return "", err
	}
	vm.SetTimeZone(loc)
	var lastPrint string
	print := vm.FuncOf("print", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		lastPrint = firstLine(fmt.Sprint(args))
		return ekuma.Undefined, nil
	})
	if err := vm.Global().Set("print", print); err != nil {
		return "", err
	}
	for _, helper := range v.helpers {
		if _, err := vm.BindScript(helper).Evaluate(ctx); err != nil {
			return "", fmt.Errorf("V8 harness: %w", err)
		}
	}
	if _, err := runScript(ctx, &vm, v.file, src); err != nil {
		if _, ok := errors.AsType[*ekuma.ScriptError](err); v.wantFailure && ok {
			return "", nil
		}
		return lastPrint, err
	}
	if v.wantFailure {
		return lastPrint, fmt.Errorf("passes now: remove %s from %s", v.file, v8SkipsFile)
	}
	return "", nil
}
