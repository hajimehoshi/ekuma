// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma"
)

func runRBBITest(t *testing.T, path, unicodeData string, failures map[int]string) {
	var vm ekuma.VM
	newSegmenter := segmenterFactory(t, &vm)
	executed := map[string]int{}
	var known int
	forEachBreakCase(t, path, unicodeData, func(c breakCase) {
		executed[c.mode]++
		changes, listed := failures[c.line]
		delete(failures, c.line)
		where := fmt.Sprintf("%s:%d: Intl.Segmenter(%q, %s) on %q", path, c.line, c.locale, c.mode, c.input)
		boundaries, err := newSegmenter.Invoke(t.Context(), c.locale, c.mode)
		if err != nil {
			t.Errorf("%s: %v", where, err)
			return
		}
		got, err := boundaries.Invoke(t.Context(), c.points)
		if err != nil {
			t.Errorf("%s: %v", where, err)
			return
		}
		want := formatBreakOffsets(c.offsets)
		if got.String() == want {
			if listed {
				t.Errorf("%s: got expected [%s]; remove the %s entry", where, want, failuresFile)
			}
			return
		}
		if listed {
			recorded, err := applyBreakChanges(c.offsets, changes)
			if err != nil {
				t.Errorf("%s: %s: %v", where, failuresFile, err)
				return
			}
			if got.String() == formatBreakOffsets(recorded) {
				known++
				return
			}
		}
		t.Errorf("%s: got [%s], want [%s] (recorded changes %q)", where, got.String(), want, changes)
	})
	for line := range failures {
		t.Errorf("%s:%d: known failure matches no executed break test", path, line)
	}
	for _, mode := range []string{"grapheme", "word", "sentence"} {
		if executed[mode] == 0 {
			t.Errorf("%s: no %s cases", path, mode)
		}
	}
	t.Logf("%d grapheme, %d word, %d sentence cases executed; %d known failures", executed["grapheme"], executed["word"], executed["sentence"], known)
}

func applyBreakChanges(offsets []int, changes string) ([]int, error) {
	offsets = slices.Clone(offsets)
	for field := range strings.SplitSeq(changes, ",") {
		if len(field) < 2 || field[0] != '+' && field[0] != '-' {
			return nil, fmt.Errorf("invalid break change %q", field)
		}
		offset, err := strconv.Atoi(field[1:])
		if err != nil || offset < 0 {
			return nil, fmt.Errorf("invalid break offset in %q", field)
		}
		i, found := slices.BinarySearch(offsets, offset)
		if found != (field[0] == '-') {
			return nil, fmt.Errorf("change %s does not apply to the fixture boundaries", field)
		}
		if found {
			offsets = slices.Delete(offsets, i, i+1)
		} else {
			offsets = slices.Insert(offsets, i, offset)
		}
	}
	return offsets, nil
}

type breakCase struct {
	line                int
	mode, locale, input string
	points              []any
	offsets             []int
}

func forEachBreakCase(t *testing.T, path, unicodeData string, visit func(breakCase)) {
	t.Helper()
	names := readCharacterNames(t, unicodeData)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := breakSource{
		rest: strings.ReplaceAll(strings.TrimPrefix(string(data), "\uFEFF"), "\r\n", "\n"),
		line: 1,
	}
	locale := "und"
	var mode, activeLocale, exclusion string
	excluded := map[string]int{}
	var badRules int
	for len(source.rest) > 0 {
		r, n := utf8.DecodeRuneInString(source.rest)
		if unicode.IsSpace(r) {
			source.take(n)
			continue
		}
		if r == '#' {
			source.skipComment()
			continue
		}
		line := source.line
		tag, _, ok := strings.Cut(source.rest, ">")
		if !ok || !strings.HasPrefix(tag, "<") {
			t.Fatalf("%s:%d: <%s> (%s): expected directive at %.80q", path, line, mode, activeLocale, source.rest)
		}
		source.take(len(tag) + 1)
		tag = tag[1:]
		switch tag {
		case "data":
			if mode == "" {
				t.Fatalf("%s:%d: <data> before a break mode: %.80q", path, line, source.rest)
			}
			raw := source.rest
			points, want, err := source.breakData(names)
			if err != nil {
				t.Fatalf("%s:%d: <%s> (%s), data from line %d: %v at %.80q", path, source.line, mode, activeLocale, line, err, source.rest)
			}
			if exclusion != "" {
				excluded[exclusion]++
				continue
			}
			visit(breakCase{
				line:    line,
				mode:    mode,
				locale:  activeLocale,
				input:   raw[:len(raw)-len(source.rest)-len("</data>")],
				points:  points,
				offsets: want,
			})
		case "rules", "badrules":
			end := "</" + tag + ">"
			rules, _, ok := strings.Cut(source.rest, end)
			if !ok {
				t.Fatalf("%s:%d: unclosed <%s> at %.80q", path, line, tag, source.rest)
			}
			source.take(len(rules) + len(end))
			if tag == "badrules" {
				// ICU tries to compile these rules without replacing the active iterator.
				badRules++
			} else {
				mode, exclusion = tag, "custom ICU rules"
			}
		case "char", "word", "sent", "line", "lineML", "title":
			// ICU snapshots the current locale when it creates the iterator.
			mode, activeLocale, exclusion = tag, locale, ""
			switch tag {
			case "char":
				mode = "grapheme"
			case "sent":
				mode = "sentence"
				if strings.HasSuffix(locale, "-u-ss-standard") {
					exclusion = "ICU sentence-break suppression"
				}
			case "line", "lineML", "title":
				exclusion = tag + " breaking"
			}
		default:
			directive, id, _ := strings.Cut(tag, " ")
			if directive != "locale" {
				t.Fatalf("%s:%d: <%s> (%s): unsupported directive <%s>", path, line, mode, activeLocale, tag)
			}
			locale, err = breakLocale(strings.TrimSpace(id))
			if err != nil {
				t.Fatalf("%s:%d: <%s>: %v", path, line, tag, err)
			}
		}
	}
	for _, reason := range slices.Sorted(maps.Keys(excluded)) {
		t.Logf("%d data blocks excluded: %s", excluded[reason], reason)
	}
	t.Logf("%d ICU rule-rejection tests excluded", badRules)
}

func breakLocale(id string) (string, error) {
	base, keyword, _ := strings.Cut(id, "@")
	tag := strings.ReplaceAll(base, "_", "-")
	if tag == "" || tag == "root" {
		tag = "und"
	}
	if before, ok := strings.CutSuffix(tag, "-POSIX"); ok {
		tag = before + "-u-va-posix"
	}
	switch keyword {
	case "":
		return tag, nil
	case "ss=standard", "lb=strict", "lb=normal", "lb=loose", "lw=phrase":
		return tag + "-u-" + strings.ReplaceAll(keyword, "=", "-"), nil
	default:
		return "", fmt.Errorf("unsupported ICU locale keyword %q", keyword)
	}
}

func readCharacterNames(t *testing.T, path string) map[string]rune {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	names := map[string]rune{}
	s := bufio.NewScanner(f)
	for line := 1; s.Scan(); line++ {
		cp, fields, ok := strings.Cut(s.Text(), ";")
		name, _, _ := strings.Cut(fields, ";")
		if strings.HasPrefix(name, "<") {
			continue
		}
		r, err := parseScalar(cp)
		if !ok || name == "" || err != nil {
			t.Fatalf("%s:%d: invalid character name record %q", path, line, s.Text())
		}
		names[name] = r
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return names
}

type breakSource struct {
	rest   string
	line   int
	column int // UTF-16 units consumed on the current source line
}

func (s *breakSource) take(n int) {
	for _, r := range s.rest[:n] {
		if r == '\r' || r == '\n' {
			s.line++
			s.column = 0
		} else {
			s.column += utf16.RuneLen(r)
		}
	}
	s.rest = s.rest[n:]
}

func (s *breakSource) skipComment() {
	n := strings.IndexAny(s.rest, "\r\n")
	if n < 0 {
		s.take(len(s.rest))
		return
	}
	s.take(n + 1)
}

func (s *breakSource) breakData(names map[string]rune) ([]any, []int, error) {
	var points []any
	var breaks []int
	var offset int
	lastBreak := -1
	for s.rest != "" {
		if strings.HasPrefix(s.rest, "</data>") {
			s.take(len("</data>"))
			return points, breaks, nil
		}
		r, n := utf8.DecodeRuneInString(s.rest)
		switch r {
		case '#':
			// TestExtended recognizes an in-data comment at its column 3.
			// It computes that column after reading the '#' and adds one.
			if s.column == 1 {
				s.skipComment()
				continue
			}
		case '•', '<':
			if r == '<' {
				status, _, ok := strings.Cut(s.rest[1:], ">")
				if !ok || strings.IndexFunc(status, func(r rune) bool {
					return !unicode.IsSpace(r) && !unicode.IsDigit(r)
				}) >= 0 {
					return nil, nil, fmt.Errorf("invalid break status")
				}
				n = len(status) + 2
			}
			if lastBreak == offset {
				if strings.HasPrefix(s.rest, "<>") {
					s.take(2)
					continue
				}
				return nil, nil, fmt.Errorf("duplicate break at UTF-16 offset %d", offset)
			}
			lastBreak = offset
			breaks = append(breaks, offset)
			s.take(n)
			continue
		case '\\':
			if len(s.rest) > 1 && (s.rest[1] == '\n' || s.rest[1] == '\r') {
				s.take(2)
				// ICU sets the source column origin to the escaped newline itself.
				s.column = 1
				continue
			}
			var err error
			r, n, err = breakEscape(s.rest, names)
			if err != nil {
				return nil, nil, err
			}
		}
		points = append(points, uint32(r))
		// An escaped surrogate denotes a single UTF-16 unit.
		offset += max(1, utf16.RuneLen(r))
		s.take(n)
	}
	return nil, nil, fmt.Errorf("unclosed <data>")
}

func breakEscape(s string, names map[string]rune) (rune, int, error) {
	if strings.HasPrefix(s, `\N{`) {
		name, _, closed := strings.Cut(s[3:], "}")
		if r, ok := names[name]; ok && closed {
			return r, len(name) + 4, nil
		}
		return 0, 0, fmt.Errorf("unknown or unterminated character name %.80q", name)
	}
	if len(s) < 2 {
		return 0, 0, fmt.Errorf("incomplete escape")
	}
	switch s[1] {
	case 'u', 'U':
		width := 4
		if s[1] == 'U' {
			width = 8
		}
		if len(s) >= 2+width {
			cp, err := strconv.ParseUint(s[2:2+width], 16, 32)
			if err == nil && cp <= utf8.MaxRune {
				return rune(cp), 2 + width, nil
			}
		}
		return 0, 0, fmt.Errorf("invalid Unicode escape")
	case 'n':
		return '\n', 2, nil
	case 'r':
		return '\r', 2, nil
	case 't':
		return '\t', 2, nil
	}
	r, n := utf8.DecodeRuneInString(s[1:])
	if unicode.IsLetter(r) || unicode.IsNumber(r) {
		return 0, 0, fmt.Errorf("unsupported escape \\%c", r)
	}
	return r, n + 1, nil
}
