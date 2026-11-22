// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"bufio"
	"cmp"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/intl/collate"
	"github.com/hajimehoshi/ekuma/internal/unorm"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

type collationResults struct {
	path                  string
	failures              map[int]string
	rows, compared, known int
	excluded              map[string]int
}

func (c *collationResults) report(t *testing.T) {
	t.Helper()
	var excluded int
	for _, reason := range slices.Sorted(maps.Keys(c.excluded)) {
		n := c.excluded[reason]
		t.Logf("%d pairs excluded: %s", n, reason)
		excluded += n
	}
	t.Logf("%d input rows, %d comparisons executed (pairs), %d excluded, %d known failures", c.rows, c.compared, excluded, c.known)
	if c.compared == 0 {
		t.Error("fixture contains no executable comparisons")
	}
}

func forEachCollationLine(t *testing.T, path string, visit func(int, string)) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for line := 1; scanner.Scan(); line++ {
		data := scanner.Text()
		if line == 1 {
			data = strings.TrimPrefix(data, "\uFEFF")
		}
		data, _, _ = strings.Cut(data, "#")
		if data = strings.TrimRight(data, " \t　"); data != "" {
			visit(line, data)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

const unpairedReason = "ICU orders unpaired UTF-16 surrogates separately; Ekuma compares well-formed UTF-8"

func runOrderedCollation(t *testing.T, path string, shifted bool, failures map[int]string) {
	// Intl.Collator negotiates a locale and exposes sensitivity through tertiary.
	// The fixtures use the root at identical strength. Their Han pairs also
	// hold in Ekuma's code-point order and need no exception.
	root := collate.New("und", "standard", collate.Options{
		Strength:          3,
		IgnorePunctuation: shifted,
	})
	results := newCollationResults(t, path, failures)
	settings := fmt.Sprintf("root strength=identical shifted=%t normalization=on caseFirst=off caseLevel=off maxVariable=punct", shifted)
	compare := func(a, b string, identical int) int {
		if order := root.Compare(a, b); order != 0 {
			return order
		}
		if shifted {
			if order := collate.CompareRootShiftedQuaternary(a, b); order != 0 {
				return order
			}
		}
		return identical
	}
	var prev wtf8.String
	var prevLine int
	forEachCollationLine(t, path, func(line int, data string) {
		var buf wtf8.Buffer
		for field := range strings.FieldsSeq(data) {
			cp, err := strconv.ParseUint(field, 16, 32)
			if err != nil || cp > utf8.MaxRune {
				t.Fatalf("%s:%d: invalid code point %q", path, line, field)
			}
			buf.WriteRune(rune(cp))
		}
		curr := buf.String()
		a, b := prev, curr
		leftLine := prevLine
		prev, prevLine = curr, line
		results.rows++
		if leftLine == 0 {
			return
		}
		if !wtf8.IsWellFormed(a) || !wtf8.IsWellFormed(b) {
			results.excluded[unpairedReason]++
			return
		}
		tie := identicalOrder(string(a), string(b))
		got := compare(string(a), string(b), tie)
		reverse := compare(string(b), string(a), -tie)
		want := "<"
		if tie == 0 {
			want = "="
		}
		results.check(t, line, leftLine, settings, a, b, orderSign(got), orderSign(reverse), want)
	})
	results.report(t)
}

// The identical level orders NFD code points with the merge separator below U+0000.
func identicalOrder(a, b string) int {
	a = unorm.String(unorm.NFD, a, nil)
	b = unorm.String(unorm.NFD, b, nil)
	for a != "" && b != "" {
		x, nx := utf8.DecodeRuneInString(a)
		y, ny := utf8.DecodeRuneInString(b)
		if x != y {
			switch {
			case x == '\uFFFE':
				return -1
			case y == '\uFFFE':
				return 1
			default:
				return cmp.Compare(x, y)
			}
		}
		a, b = a[nx:], b[ny:]
	}
	return cmp.Compare(len(a), len(b))
}

func parseCollationString(s string) (wtf8.String, error) {
	var buf wtf8.Buffer
	for s != "" {
		r, n := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && n == 1 {
			return "", fmt.Errorf("invalid UTF-8")
		}
		s = s[n:]
		switch r {
		case '|':
			return "", fmt.Errorf("prefix in comparison string")
		case '\\':
			if s == "" {
				return "", fmt.Errorf("incomplete escape")
			}
			escape := s[0]
			s = s[1:]
			switch escape {
			case 't':
				r = '\t'
			case 'r':
				r = '\r'
			case 'x', 'u', 'U':
				width := 4
				if escape == 'x' {
					width = 2
				} else if escape == 'U' {
					width = 8
				}
				if len(s) < width {
					return "", fmt.Errorf("short \\%c escape", escape)
				}
				cp, err := strconv.ParseUint(s[:width], 16, 32)
				if err != nil || cp > utf8.MaxRune {
					return "", fmt.Errorf("invalid \\%c escape %q", escape, s[:width])
				}
				r = rune(cp)
				s = s[width:]
			default:
				return "", fmt.Errorf("unknown escape \\%c", escape)
			}
		}
		buf.WriteRune(r)
	}
	return buf.String(), nil
}

func newCollationResults(t *testing.T, fixture string, failures map[int]string) *collationResults {
	t.Helper()
	t.Cleanup(func() {
		for line := range failures {
			t.Errorf("%s:%d: known failure matches no executed comparison", fixture, line)
		}
	})
	return &collationResults{
		path:     fixture,
		excluded: map[string]int{},
		failures: failures,
	}
}

func orderSign(order int) string {
	switch {
	case order < 0:
		return "<"
	case order > 0:
		return ">"
	default:
		return "="
	}
}

func (c *collationResults) check(t *testing.T, line, prevLine int, settings string, a, b wtf8.String, got, reverse, want string) {
	c.compared++
	known, listed := c.failures[line]
	delete(c.failures, line)
	wantReverse := got
	switch got[0] {
	case '<':
		wantReverse = ">" + got[1:]
	case '>':
		wantReverse = "<" + got[1:]
	}
	matches := got == want || want == "<" && strings.HasPrefix(got, "<")
	if reverse == wantReverse {
		if matches && !listed {
			return
		}
		if !matches && listed && got == known {
			c.known++
			return
		}
	}
	var note string
	if listed {
		note = fmt.Sprintf(" (recorded failure %s)", known)
	}
	t.Errorf("%s:%d (previous line %d): %s: %+q vs %+q: got %s (reverse %s), want %s%s", c.path, line, prevLine, settings, a, b, got, reverse, want, note)
}
