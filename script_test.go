// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

func TestScripts(t *testing.T) {
	h := &harness{
		fs:      os.DirFS("testdata/harness"),
		files:   map[string]string{},
		scripts: map[string]*ekuma.UnboundScript{},
	}
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
			reason, timedOut := test.run(h, testTimeout)
			if timedOut {
				reason, _ = test.run(h, retryTimeout)
			}
			if reason != "" {
				t.Error(reason)
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLocalScriptHarness(t *testing.T) {
	h := &harness{
		fs:      os.DirFS("testdata/harness"),
		files:   map[string]string{},
		scripts: map[string]*ekuma.UnboundScript{},
	}
	for _, tt := range []struct {
		name         string
		src          string
		async        bool
		negativeType string
		wantFailure  bool
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
			name:        "async rejection",
			src:         `Promise.reject(new Test262Error("rejected")).then(() => $DONE(), $DONE);`,
			async:       true,
			wantFailure: true,
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
			reason, timedOut := runOnce(h, tt.name, tt.src, m, false, testTimeout)
			if timedOut {
				t.Error("harness test timed out")
			}
			if failed := reason != ""; failed != tt.wantFailure {
				t.Errorf("failed = %v, want %v (reason: %s)", failed, tt.wantFailure, reason)
			}
		})
	}
}
