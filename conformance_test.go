// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

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

type suiteFailure struct {
	path   string
	reason string
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
