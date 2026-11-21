// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

func TestScripts(t *testing.T) {
	h := newHarness(os.DirFS("testdata/harness"))
	scriptFS := os.DirFS(".")
	err := fs.WalkDir(scriptFS, "testdata/scripts", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".js") || strings.HasSuffix(path, "_FIXTURE.js") {
			return nil
		}
		t.Run(strings.TrimPrefix(path, "testdata/scripts/"), func(t *testing.T) {
			data, err := fs.ReadFile(scriptFS, path)
			if err != nil {
				t.Fatal(err)
			}
			src := string(data)
			test := &test262Test{
				path: path,
				src:  src,
				m:    parseMetadata(src),
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
		t.Fatal(err)
	}
}

func TestLocalScriptHarness(t *testing.T) {
	h := newHarness(os.DirFS("testdata/harness"))
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
