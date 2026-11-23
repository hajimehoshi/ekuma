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
		check, err := newSegmenter.Invoke(t.Context(), c.locale, c.mode)
		if err != nil {
			t.Errorf("%s: %v", where, err)
			return
		}
		err = checkBreakRecords(t, check, c.points, c.boundaries)
		if !listed {
			if err != nil {
				t.Errorf("%s: %v", where, err)
			}
			return
		}
		if err == nil {
			t.Errorf("%s: matches ICU; remove the %s entry", where, failuresFile)
			return
		}
		recorded, err := applyBreakChanges(c.boundaries, changes)
		if err != nil {
			t.Errorf("%s: %s: %v", where, failuresFile, err)
			return
		}
		if err := checkBreakRecords(t, check, c.points, recorded); err != nil {
			t.Errorf("%s (recorded changes %q): %v", where, changes, err)
			return
		}
		known++
	})
	for line := range failures {
		t.Errorf("%s:%d: known failure matches no executed break test", path, line)
	}
	for _, mode := range []string{"grapheme", "word", "sentence"} {
		if executed[mode] == 0 {
			t.Errorf("%s: no %s cases", path, mode)
		}
	}
	t.Logf("%d grapheme, %d word, %d sentence cases executed; %d known differences", executed["grapheme"], executed["word"], executed["sentence"], known)
}

func applyBreakChanges(boundaries []breakBoundary, changes string) ([]breakBoundary, error) {
	boundaries = slices.Clone(boundaries)
	for field := range strings.SplitSeq(changes, ",") {
		offsetText, value, changeStatus := strings.Cut(field, "=")
		if !changeStatus {
			if len(field) < 2 || field[0] != '+' && field[0] != '-' {
				return nil, fmt.Errorf("invalid break change %q", field)
			}
			offsetText = field[1:]
		}
		offset, err := strconv.Atoi(offsetText)
		if err != nil || offset < 0 {
			return nil, fmt.Errorf("invalid break offset in %q", field)
		}
		i, found := slices.BinarySearchFunc(boundaries, offset, func(b breakBoundary, offset int) int {
			return cmp.Compare(b.offset, offset)
		})
		if changeStatus {
			if !found || i == 0 || boundaries[i].wordLike == nil || value != "true" && value != "false" {
				return nil, fmt.Errorf("invalid word status change %q", field)
			}
			like := value == "true"
			if *boundaries[i].wordLike == like {
				return nil, fmt.Errorf("word status change %q already matches the fixture", field)
			}
			boundaries[i].wordLike = &like
			continue
		}
		if found != (field[0] == '-') {
			return nil, fmt.Errorf("change %s does not apply to the fixture boundaries", field)
		}
		if found {
			boundaries = slices.Delete(boundaries, i, i+1)
		} else {
			boundaries = slices.Insert(boundaries, i, breakBoundary{
				offset: offset,
			})
			i++
		}
		// The next boundary now ends a different segment.
		if i < len(boundaries) {
			boundaries[i].wordLike = nil
		}
	}
	return boundaries, nil
}

type breakCase struct {
	line                int
	mode, locale, input string
	points              []any
	boundaries          []breakBoundary
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
			points, want, err := source.breakData(names, mode == "word")
			if err != nil {
				t.Fatalf("%s:%d: <%s> (%s), data from line %d: %v at %.80q", path, source.line, mode, activeLocale, line, err, source.rest)
			}
			if exclusion != "" {
				excluded[exclusion]++
				continue
			}
			visit(breakCase{
				line:       line,
				mode:       mode,
				locale:     activeLocale,
				input:      raw[:len(raw)-len(source.rest)-len("</data>")],
				points:     points,
				boundaries: want,
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

func (s *breakSource) breakData(names map[string]rune, word bool) ([]any, []breakBoundary, error) {
	var points []any
	var breaks []breakBoundary
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
			var status int
			if r == '<' {
				digits, _, ok := strings.Cut(s.rest[1:], ">")
				if !ok {
					return nil, nil, fmt.Errorf("unterminated break status")
				}
				for _, ch := range digits {
					if unicode.IsSpace(ch) {
						continue
					}
					if ch < '0' || ch > '9' {
						return nil, nil, fmt.Errorf("invalid break status %q", digits)
					}
					status = status*10 + int(ch-'0')
				}
				n = len(digits) + 2
			}
			if lastBreak == offset {
				if !strings.HasPrefix(s.rest, "<>") {
					return nil, nil, fmt.Errorf("duplicate break at UTF-16 offset %d", offset)
				}
				// ICU lets <> replace the status at the current boundary.
				breaks = breaks[:len(breaks)-1]
			}
			lastBreak = offset
			boundary := breakBoundary{
				offset: offset,
			}
			if word {
				// ICU word statuses 0..99 denote nonwords and 100..499
				// denote numbers, letters, kana, or ideographs.
				if status >= 500 {
					return nil, nil, fmt.Errorf("unknown ICU word status %d", status)
				}
				boundary.wordLike = new(status >= 100)
			}
			breaks = append(breaks, boundary)
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
