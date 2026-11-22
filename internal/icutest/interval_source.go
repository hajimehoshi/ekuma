// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// intervalSource is a section of a pinned C or C++ fixture with its original line numbers.
type intervalSource struct {
	t     *testing.T
	path  string
	lines []string
	line  int
}

func readIntervalSource(t *testing.T, path string) intervalSource {
	s := intervalSource{
		t:    t,
		path: path,
		line: 1,
	}
	fixtureLines(t, path, func(_ int, row string) {
		row, _, _ = strings.Cut(row, "//")
		s.lines = append(s.lines, strings.TrimSpace(row))
	})
	return s
}

func (s intervalSource) from(marker string) intervalSource {
	s.t.Helper()
	for i, line := range s.lines {
		if strings.Contains(line, marker) {
			s.lines = s.lines[i:]
			s.line += i
			return s
		}
	}
	s.t.Fatalf("%s:%d: missing %q", s.path, s.line, marker)
	return s
}

type intervalRow struct {
	line int
	text string
}

func (s intervalSource) table(marker string) []intervalRow {
	s.t.Helper()
	s = s.from(marker)
	var rows []intervalRow
	for i, line := range s.lines[1:] {
		row, _, end := strings.Cut(line, "};")
		row = strings.TrimSpace(row)
		if row != "" && row != "{" && !strings.HasPrefix(row, "#") {
			rows = append(rows, intervalRow{
				line: s.line + i + 1,
				text: row,
			})
		}
		if end {
			return rows
		}
	}
	s.t.Fatalf("%s:%d: unterminated %s", s.path, s.line, marker)
	return nil
}

const intervalLiteralPattern = `"(?:[^"\\]|\\.)*"`

var intervalLiteral = regexp.MustCompile(intervalLiteralPattern)
var intervalToken = regexp.MustCompile(intervalLiteralPattern + "|,")

func (r intervalRow) columns(t *testing.T, count int) []string {
	t.Helper()
	row := strings.Trim(strings.TrimSpace(r.text), "{}, ")
	var cols []string
	var start int
	for _, match := range intervalToken.FindAllStringIndex(row, -1) {
		if row[match[0]] == ',' {
			cols = append(cols, strings.TrimSpace(row[start:match[0]]))
			start = match[1]
		}
	}
	cols = append(cols, strings.TrimSpace(row[start:]))
	if len(cols) != count {
		t.Fatalf("line %d: expected %d columns in %s, got %d", r.line, count, r.text, len(cols))
	}
	return cols
}

// ICU's char* fixtures pass C string literals through a second Unicode unescape.
func intervalString(t *testing.T, expr string) string {
	t.Helper()
	var out strings.Builder
	tokens := intervalLiteral.FindAllString(expr, -1)
	if len(tokens) == 0 {
		t.Fatalf("missing C string literal in %q", expr)
	}
	for _, token := range tokens {
		s, err := strconv.Unquote(token)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(s, `\u`) {
			s, err = strconv.Unquote(`"` + s + `"`)
			if err != nil {
				t.Fatal(err)
			}
		}
		out.WriteString(s)
	}
	return out.String()
}
