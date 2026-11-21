// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"
)

const v8WebKitDir = "test/webkit"

// compareWebKitOutput follows V8's webkit.OutProc and its base ExpectedOutProc.
func compareWebKitOutput(expected, actual string) suiteResult {
	// d8 can exit successfully without output for contradictory flags.
	if trimWebKitLine(actual) == "" {
		return suiteResult{}
	}
	var want []string
	// Python reads the expectation file in universal newline mode.
	for _, line := range strings.FieldsFunc(expected, func(r rune) bool { return r == '\r' || r == '\n' }) {
		line = trimWebKitLine(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			want = append(want, line)
		}
	}
	// Python's str.splitlines recognizes additional separators in stdout.
	lines := strings.FieldsFunc(actual, func(r rune) bool {
		return strings.ContainsRune("\n\r\v\f\x1c\x1d\x1e\u0085\u2028\u2029", r)
	})
	var start int
	for i, line := range lines {
		if !strings.HasPrefix(line, "==") {
			continue
		}
		// In stress output only the blocks between separators are compared.
		if start > 0 {
			if r := compareWebKitBlock(want, lines[start:i]); r.outcome != suitePassed {
				return r
			}
		}
		start = i + 1
	}
	if start > 0 {
		return suiteResult{}
	}
	return compareWebKitBlock(want, lines)
}

func trimWebKitLine(line string) string {
	return strings.TrimFunc(line, func(r rune) bool {
		return unicode.IsSpace(r) || ('\x1c' <= r && r <= '\x1f')
	})
}

func compareWebKitBlock(want, lines []string) suiteResult {
	var i int
	for _, line := range lines {
		line = trimWebKitLine(line)
		switch {
		case line == "", strings.HasPrefix(line, "=="), strings.HasPrefix(line, "**"),
			strings.HasPrefix(line, "ANDROID"), strings.HasPrefix(line, "###"),
			strings.HasPrefix(line, "WARNING: linker:"), strings.HasPrefix(line, "V8 is running with"),
			strings.HasPrefix(line, "Warning: unknown flag"), line == "Try --help for options":
			continue
		}
		if i == len(want) {
			return suiteFailed.result(fmt.Sprintf("unexpected stdout line %d: %q", i+1, firstLine(line)))
		}
		if line != want[i] {
			return suiteFailed.result(fmt.Sprintf("stdout line %d: got %q, want %q", i+1, firstLine(line), firstLine(want[i])))
		}
		i++
	}
	if i < len(want) {
		return suiteFailed.result(fmt.Sprintf("missing stdout line %d: %q", i+1, firstLine(want[i])))
	}
	return suiteResult{}
}

// CheckWebKit checks discovery, script order and printed assertions.
func (host *V8) CheckWebKit(t *testing.T) {
	const file = v8WebKitDir + "/case.js"
	checkout := fstest.MapFS{
		v8WebKitDir + "/resources/dependency.js": {
			Data: []byte("var order = ['dependency'];"),
		},
		v8WebKitDir + "/resources/standalone-pre.js": {
			Data: []byte("order.push('pre');"),
		},
		file: {
			Data: []byte("// Files: test/webkit/resources/dependency.js\norder.push('case'); print('PASS', order);"),
		},
		v8WebKitDir + "/resources/standalone-post.js": {
			Data: []byte("print('COMPLETE');"),
		},
		v8WebKitDir + "/case-expected.txt": {
			Data: []byte("# expectation\r\n PASS dependency,pre,case \r\n\nCOMPLETE\n"),
		},
	}
	suite := v8Suite{
		dir: v8WebKitDir,
	}
	tests := suite.collect(t, checkout, suite.dir, "", nil, nil)
	if len(tests) != 1 || tests[0].file != file {
		t.Fatalf("discovered tests = %v, want only %s", tests, file)
	}
	test := tests[0]
	if r := test.run(t.Context(), host, checkout, testTimeout); r.outcome != suitePassed {
		t.Error(r.reason)
	}
	checkout[file].Data = []byte("// Files: test/webkit/resources/dependency.js\nprint('FAIL');")
	if r := test.run(t.Context(), host, checkout, testTimeout); r.outcome != suiteFailed {
		t.Errorf("printed failure: got %+v, want an output mismatch", r)
	}
}
