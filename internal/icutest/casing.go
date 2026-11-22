// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"text/scanner"

	"github.com/hajimehoshi/ekuma"
)

func runCasingTest(t *testing.T, path string) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var s scanner.Scanner
	s.Init(f)
	s.Filename = path
	s.Mode = scanner.ScanIdents | scanner.ScanStrings | scanner.ScanComments | scanner.SkipComments
	s.Error = func(s *scanner.Scanner, msg string) {
		t.Fatalf("%s: %s", s.Pos(), msg)
	}
	token := s.Scan()
	advance := func() { token = s.Scan() }
	expect := func(text string) {
		t.Helper()
		if s.TokenText() != text {
			t.Fatalf("%s: expected %q, got %q", s.Position, text, s.TokenText())
		}
		advance()
	}
	for _, text := range []string{"casing", ":", "table", "(", "nofallback", ")", "{", "Info", "{"} {
		expect(text)
	}
	// Info is descriptive resource-bundle metadata, including nested strings.
	for depth := 1; depth > 0; advance() {
		switch token {
		case '{':
			depth++
		case '}':
			depth--
		case scanner.EOF:
			t.Fatalf("%s: unterminated Info block", s.Position)
		}
	}
	expect("TestData")
	expect("{")
	readArray := func() []string {
		t.Helper()
		expect("{")
		var fields []string
		for token == scanner.String {
			field, err := strconv.Unquote(s.TokenText())
			if err != nil {
				t.Fatalf("%s: %v", s.Position, err)
			}
			fields = append(fields, field)
			advance()
			if token != ',' {
				break
			}
			advance()
		}
		expect("}")
		return fields
	}

	var vm ekuma.VM
	counts := map[string]int{}
	for token != '}' {
		op := s.TokenText()
		var header string
		switch op {
		case "lowercasing", "uppercasing":
			header = "Input Output Locale"
		case "titlecasing":
			header = "Input Output Locale Type Options"
		case "casefolding":
			header = "Input Output Options"
		default:
			t.Fatalf("%s: unknown casing operation %q", s.Position, op)
		}
		advance()
		expect("{")
		expect("Headers")
		fields := readArray()
		if !slices.Equal(fields, strings.Fields(header)) {
			t.Fatalf("%s: unexpected %s headers %q", s.Position, op, fields)
		}
		expect("Cases")
		expect("{")
		for token == '{' {
			line := s.Line
			row := readArray()
			if len(row) != len(fields) {
				t.Fatalf("%s:%d: %s: expected %d columns, got %q", path, line, op, len(fields), row)
			}
			if token == ',' {
				advance()
			}
			counts[op]++
			if op == "titlecasing" || op == "casefolding" {
				continue
			}
			method := "LowerCase"
			if op == "uppercasing" {
				method = "UpperCase"
			}
			var args []any
			input, want, locale := row[0], row[1], row[2]
			if locale == "" {
				method = "to" + method
			} else {
				method = "toLocale" + method
				args = append(args, locale)
			}
			got, err := vm.ValueOf(input).Call(t.Context(), method, args...)
			if err != nil || !got.Equal(vm.ValueOf(want)) {
				t.Errorf("%s:%d: %+q.%s(%v): got %+q (error %v), want %+q", path, line, input, method, args, got.String(), err, want)
			}
		}
		expect("}")
		expect("}")
	}
	expect("}")
	expect("}")
	if token != scanner.EOF || counts["lowercasing"] == 0 || counts["uppercasing"] == 0 {
		t.Fatalf("%s: incomplete casing fixture or trailing token %q", s.Position, s.TokenText())
	}
	t.Logf("%d lower and %d upper cases executed, %d titlecasing and %d casefolding rows excluded (no corresponding String method), 0 known failures",
		counts["lowercasing"], counts["uppercasing"], counts["titlecasing"], counts["casefolding"])
}
