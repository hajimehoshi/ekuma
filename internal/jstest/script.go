// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// localTest262HarnessFS resolves upstream Test262 include names to Ekuma's local helpers.
type localTest262HarnessFS struct{}

func (localTest262HarnessFS) Open(name string) (fs.File, error) {
	if name == "doneprintHandle.js" {
		name = "done-print-handle.js"
	}
	return os.DirFS(testdataDir + "/harness").Open("test262-" + name)
}

// RunScripts runs the JavaScript tests under scriptsDir.
func (suite *Test262) RunScripts(t *testing.T, scriptsDir string) {
	suite.newHarness(localTest262HarnessFS{}).runScripts(t, scriptsDir)
}

// RunV8LocalTests runs V8's local Test262 cases with the official Test262 harness.
func (suite *Test262) RunV8LocalTests(t *testing.T) {
	v8Dir := v8Flags.checkoutDir(t)
	h := suite.checkoutHarness(t, test262Flags.checkoutDir(t))
	h.runScripts(t, filepath.Join(v8Dir, "test/test262/local-tests/test"))
}

func (h *harness) runScripts(t *testing.T, scriptsDir string) {
	err := fs.WalkDir(os.DirFS(scriptsDir), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".js") || strings.HasSuffix(path, "_FIXTURE.js") {
			return nil
		}
		t.Run(path, func(t *testing.T) {
			test, err := readTest(filepath.Join(scriptsDir, filepath.FromSlash(path)))
			if err != nil {
				t.Fatal(err)
			}
			result := test.run(h, testTimeout)
			if result.retry {
				result = test.run(h, retryTimeout)
			}
			if result.outcome != suitePassed {
				t.Error(result.reason)
			}
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", scriptsDir, err)
	}
}

// CheckLocalScriptHarness checks the local harness's assertions and asynchronous completion.
func (suite *Test262) CheckLocalScriptHarness(t *testing.T) {
	h := suite.newHarness(localTest262HarnessFS{})
	for _, tt := range []struct {
		name         string
		src          string
		async        bool
		negativeType string
		want         suiteOutcome
	}{
		{
			name: "NaN and object identity",
			src:  `assert.sameValue(NaN, NaN); var object = {}; assert.sameValue(object, object);`,
		},
		{
			name:         "signed zero",
			src:          `assert.sameValue(0, -0);`,
			negativeType: "Test262Error",
		},
		{
			name:         "different types",
			src:          `assert.sameValue(1, "1");`,
			negativeType: "Test262Error",
		},
		{
			name:         "different objects",
			src:          `assert.sameValue({}, {});`,
			negativeType: "Test262Error",
		},
		{
			name:  "async rejection",
			src:   `Promise.reject(new Test262Error("rejected")).then(() => $DONE(), $DONE);`,
			async: true,
			want:  suiteFailed,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := metadata{
				flags: map[string]bool{
					"async": tt.async,
				},
			}
			if tt.negativeType != "" {
				m.negativePhase = "runtime"
				m.negativeType = tt.negativeType
			}
			result := runOnce(h, tt.name, tt.src, m, false, testTimeout)
			if result.outcome != tt.want {
				t.Errorf("outcome = %v, want %v (reason: %s)", result.outcome, tt.want, result.reason)
			}
		})
	}
}
