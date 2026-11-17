// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package main_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	jsbench "github.com/hajimehoshi/ekuma/_bench/cmd/jsbench"
)

func TestMain(m *testing.M) {
	if os.Getenv("JSBENCH_TEST_PROCESS") == "1" {
		for i, arg := range os.Args {
			if arg != "-test.bench" || i+1 >= len(os.Args) {
				continue
			}
			name := strings.NewReplacer("^", "", "$", "").Replace(os.Args[i+1])
			if strings.Contains(name, "/Failure/") {
				fmt.Println("panic: benchmark fixture failure")
				os.Exit(1)
			}
			fmt.Printf("%s 1 100 ns/op\n", name)
			os.Exit(0)
		}
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func TestRunSuitePreservesFailures(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("JSBENCH_TEST_PROCESS", "1")
	for _, engine := range []string{"ekuma", "goja"} {
		t.Run(engine, func(t *testing.T) {
			dir := t.TempDir()
			err := jsbench.RunSuite(t.Context(), bin, []string{"Failure", "Success"}, []string{engine}, dir)
			if engine == "ekuma" {
				if !errors.Is(err, jsbench.ErrBaselineFailed) {
					t.Errorf("RunSuite error = %v, want baseline failure", err)
				}
			} else if err != nil {
				t.Errorf("RunSuite error = %v, want nil", err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "Example.txt"))
			if err != nil {
				t.Fatal(err)
			}
			for _, record := range []string{
				"--- FAIL: BenchmarkExample/Failure/" + engine + ":",
				"BenchmarkExample/Success/" + engine + " 1 100 ns/op",
			} {
				if got := strings.Count(string(data), record); got != 2 {
					t.Errorf("record %q appears %d times, want 2", record, got)
				}
			}
		})
	}
}
