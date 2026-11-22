// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"errors"
	"io/fs"
	"path"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hajimehoshi/ekuma"
)

const v8MozillaDir = "test/mozilla"

var v8MozillaSuite = v8Suite{
	dir:     v8MozillaDir,
	helpers: []string{"mozilla-shell-emulation.js"},
}

// Platform exclusions in mozilla.status for the default variant on native builds.
func mozillaPlatformSkip(file string) bool {
	switch strings.TrimPrefix(file, v8MozillaDir+"/data/") {
	case "js1_5/Regress/regress-360969-05.js", "js1_5/Regress/regress-360969-06.js":
		return runtime.GOOS == "windows"
	case "js1_5/Regress/regress-280769-2.js", "js1_5/Regress/regress-80981.js":
		return runtime.GOARCH == "arm64"
	case "ecma/Date/15.9.5.8.js", "ecma/Date/15.9.5.10-2.js", "ecma/Date/15.9.5.11-2.js",
		"ecma/Date/15.9.5.12-2.js", "ecma_3/RegExp/regress-311414.js",
		"js1_5/Array/regress-99120-02.js", "js1_5/extensions/regress-371636.js",
		"js1_5/Regress/regress-203278-1.js", "js1_5/Regress/regress-404755.js",
		"js1_5/Regress/regress-451322.js":
		switch runtime.GOARCH {
		case "arm", "arm64", "mips64", "mips64le":
			return true
		}
	}
	return false
}

func walkMozillaTests(checkout fs.FS, root string, visit fs.WalkDirFunc) error {
	// -v8.dir can start the walk inside an excluded directory.
	for part := range strings.SplitSeq(root, "/") {
		if part == "CVS" || strings.HasPrefix(part, ".") {
			return nil
		}
	}
	for _, name := range []string{"ecma", "ecma_2", "ecma_3", "js1_1", "js1_2", "js1_3", "js1_4", "js1_5"} {
		dir := path.Join(v8MozillaDir, "data", name)
		if inSuiteDir(root, dir) {
			dir = root
		} else if !inSuiteDir(dir, root) {
			continue
		}
		err := fs.WalkDir(checkout, dir, func(file string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := entry.Name()
			if entry.IsDir() && (name == "CVS" || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			switch name {
			case "browser.js", "shell.js", "jsref.js", "template.js":
				return nil
			}
			return visit(file, entry, nil)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Each directory inherits its parent's shell scripts, starting with V8's emulation.
type mozillaShells map[string][]*ekuma.UnboundScript

func (s mozillaShells) forDir(checkout fs.FS, dir string) ([]*ekuma.UnboundScript, error) {
	if scripts, ok := s[dir]; ok {
		return scripts, nil
	}
	scripts, err := s.forDir(checkout, path.Dir(dir))
	if err != nil {
		return nil, err
	}
	script, err := readV8Helper(checkout, path.Join(dir, "shell.js"))
	switch {
	case err == nil:
		scripts = append(slices.Clip(scripts), script)
	case !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	s[dir] = scripts
	return scripts, nil
}

// Mozilla's output processor searches all of stdout, including partial writes.
type mozillaOutput struct {
	tail    string
	failure string
}

func (o *mozillaOutput) Write(b []byte) (int, error) {
	return o.WriteString(string(b))
}

func (o *mozillaOutput) WriteString(s string) (int, error) {
	const marker = "FAILED!"
	if o.failure == "" {
		text := o.tail + s
		if i := strings.Index(text, marker); i >= 0 {
			o.failure = strings.Clone(firstLine(text[i:]))
		} else {
			o.tail = strings.Clone(text[max(0, len(text)-len(marker)+1):])
		}
	}
	return len(s), nil
}

func (o *mozillaOutput) result(r suiteResult, negative bool) suiteResult {
	if r.outcome == suitePassed && o.failure != "" {
		r = suiteFailed.result(o.failure)
	}
	if negative {
		switch r.outcome {
		case suiteFailed:
			return suiteResult{}
		case suitePassed:
			return suiteFailed.result("negative Mozilla test completed successfully")
		}
	}
	return r
}

// CheckMozilla checks Mozilla discovery, harness ordering and negative outcomes.
func (host *V8) CheckMozilla(t *testing.T) {
	const root = v8MozillaDir + "/data"
	const dir = root + "/ecma_3/RegExp"
	const file = dir + "/case-n.js"
	checkout := fstest.MapFS{
		v8MozillaDir + "/mozilla-shell-emulation.js": {
			Data: []byte("let order = 'V';"),
		},
		root + "/shell.js": {
			Data: []byte("order += 'M';"),
		},
		root + "/ecma_3/shell.js": {
			Data: []byte("order += 'E';"),
		},
		dir + "/shell.js": {
			Data: []byte("order += 'R';"),
		},
		dir + "/browser.js":         {},
		dir + "/CVS/ignored.js":     {},
		dir + "/.hidden/ignored.js": {},
		file: {
			Data: []byte(`
				if (typeof order === "string" && order === "VMER" && typeof gc === "function") {
					print("x".repeat(300) + "FAILED! expected assertion");
					print("done");
				}
			`),
		},
	}
	tests := v8MozillaSuite.collect(t, checkout, dir, "", nil, nil)
	if len(tests) != 1 || tests[0].file != file {
		t.Fatalf("discovered tests = %v, want only %s", tests, file)
	}
	check := func(want suiteOutcome) {
		t.Helper()
		got := tests[0].run(t.Context(), host, checkout, testTimeout)
		if got.outcome != want {
			t.Errorf("outcome = %v, want %v (%s)", got.outcome, want, got.reason)
		}
	}
	check(suitePassed)
	checkout[file].Data = []byte("printErr('FAILED!');")
	check(suiteFailed)
	checkout[file].Data = []byte("let = ;")
	check(suitePassed)
	checkout[dir+"/shell.js"].Data = []byte("print('FAILED!'); throw Error('broken harness');")
	tests = v8MozillaSuite.collect(t, checkout, file, "", skipList{file: {}}, nil)
	check(suiteBroken)
}
