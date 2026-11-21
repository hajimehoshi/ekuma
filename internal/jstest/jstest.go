// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package jstest runs Ekuma's JavaScript test suites from the repository root.
package jstest

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"iter"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

const testdataDir = "internal/jstest/testdata"

type suiteFlags struct {
	name    string
	dir     string
	filter  string
	shuffle string
	report  string
}

func registerSuiteFlags(name string) *suiteFlags {
	f := &suiteFlags{
		name: name,
	}
	flag.StringVar(&f.dir, name+".dir", "", "only run the "+name+" tests in this directory of the suite")
	flag.StringVar(&f.filter, name+".filter", "", "only run the "+name+" tests whose path contains this text")
	flag.StringVar(&f.shuffle, name+".shuffle", "off", "run the "+name+" tests in random order: off, on, or a seed; a replay is exact only with GOMAXPROCS=1")
	flag.StringVar(&f.report, name+".report", "", "write the path and reason for each unexpected "+name+" failure to this file")
	return f
}

func (f *suiteFlags) checkoutDir(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skipf("skipping %s tests in short mode", f.name)
	}
	env := "EKUMA_" + strings.ToUpper(f.name)
	if dir := os.Getenv(env); dir != "" {
		return dir
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skipf("no user cache directory; set %s to a %s checkout", env, f.name)
	}
	dir := filepath.Join(cache, "ekuma", f.name)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		t.Skipf("no %s checkout at %s; set %s", f.name, dir, env)
	}
	return dir
}

// run calls runTest for each test index and waits for all workers to finish.
func (f *suiteFlags) run(t *testing.T, count int, runTest func(int)) {
	t.Helper()
	order := make([]int, count)
	for i := range order {
		order[i] = i
	}
	if f.shuffle != "off" {
		seed := time.Now().UnixNano()
		if f.shuffle != "on" {
			var err error
			seed, err = strconv.ParseInt(f.shuffle, 10, 64)
			if err != nil {
				t.Fatalf("-%s.shuffle must be off, on, or an integer: %v", f.name, err)
			}
		}
		t.Logf("-%s.shuffle %d", f.name, seed)
		r := rand.New(rand.NewPCG(uint64(seed), 0))
		r.Shuffle(count, func(i, j int) {
			order[i], order[j] = order[j], order[i]
		})
	}
	var workers sync.WaitGroup
	next := make(chan int)
	for range min(count, runtime.GOMAXPROCS(0)) {
		workers.Go(func() {
			for i := range next {
				runTest(i)
			}
		})
	}
	for _, i := range order {
		next <- i
	}
	close(next)
	workers.Wait()
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

type suiteFailure struct {
	path   string
	reason string
}

const testTimeout = 5 * time.Second
const retryTimeout = 4 * testTimeout

type suiteOutcome int

const (
	suitePassed suiteOutcome = iota
	suiteFailed              // The test ran and its expectations were not met.
	suiteBroken              // The runner could not obtain a test outcome.
	suiteSkipped
)

type suiteResult struct {
	outcome suiteOutcome
	reason  string
	retry   bool
}

func (outcome suiteOutcome) result(reason string) suiteResult {
	return suiteResult{
		outcome: outcome,
		reason:  reason,
	}
}

func timedOut(reason string) suiteResult {
	return suiteResult{
		outcome: suiteBroken,
		reason:  reason,
		retry:   true,
	}
}

// scriptErrorResult distinguishes a JavaScript exception from a runner failure.
func scriptErrorResult(err error) suiteResult {
	reason := firstLine(err.Error())
	if errors.Is(err, context.DeadlineExceeded) {
		return timedOut(reason)
	}
	if _, ok := errors.AsType[*ekuma.SyntaxError](err); ok {
		return suiteFailed.result(reason)
	}
	if ex, ok := errors.AsType[*ekuma.ScriptError](err); ok && ex.Unwrap() == nil {
		return suiteFailed.result(reason)
	}
	return suiteBroken.result(reason)
}

// expectFailure accepts only a test failure for a listed file.
func (r suiteResult) expectFailure(file, manifest string) suiteResult {
	switch r.outcome {
	case suiteFailed:
		return suiteResult{}
	case suitePassed:
		return suiteBroken.result(fmt.Sprintf("passes now: remove %s from %s", file, manifest))
	case suiteSkipped:
		return suiteBroken.result(fmt.Sprintf("cannot run (%s): remove %s from %s", r.reason, file, manifest))
	default:
		return r
	}
}

// retryInIsolation reruns the selected tests after all parallel work has finished.
func retryInIsolation(results []suiteResult, run func(int, time.Duration) suiteResult) {
	for i, result := range results {
		if result.retry {
			results[i] = run(i, retryTimeout)
		}
	}
}

func inSuiteDir(file, dir string) bool {
	return dir == "." || file == dir || strings.HasPrefix(file, dir+"/")
}

func staleEntries(unseen iter.Seq[string], manifest, dir, filter string) []suiteFailure {
	var failures []suiteFailure
	for _, file := range slices.Sorted(unseen) {
		if inSuiteDir(file, dir) && strings.Contains(file, filter) {
			failures = append(failures, suiteFailure{
				path:   file,
				reason: "not in the suite: remove it from " + manifest,
			})
		}
	}
	return failures
}

func (f *suiteFlags) reportFailures(t *testing.T, failures []suiteFailure) {
	t.Helper()
	if f.report != "" {
		var report strings.Builder
		for _, failure := range failures {
			fmt.Fprintf(&report, "%s\t%s\n", failure.path, failure.reason)
		}
		if err := os.WriteFile(f.report, []byte(report.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const maxShown = 40
	for _, failure := range failures[:min(maxShown, len(failures))] {
		t.Errorf("%s fails: %s", failure.path, failure.reason)
	}
	if len(failures) > maxShown {
		t.Errorf("and %d more failures", len(failures)-maxShown)
	}
}

// runScript parses src as a script of vm and evaluates it.
func runScript(ctx context.Context, vm *ekuma.VM, name, src string) (ekuma.Value, error) {
	return runScriptInRealm(ctx, vm.InitialRealm(), name, src)
}

func runScriptInRealm(ctx context.Context, realm *ekuma.Realm, name, src string) (ekuma.Value, error) {
	s, err := realm.ParseScript(name, src)
	if err != nil {
		return ekuma.Undefined, err
	}
	return s.Evaluate(ctx)
}
