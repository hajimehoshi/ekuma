// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/hajimehoshi/ekuma"
)

type numberPartsSuite struct {
	*formatFixture
	format    ekuma.Value
	reference map[int][]intervalPosition
}

func runNumberParts(t *testing.T, root string) {
	references := readNumberFieldReferences(t)
	for _, file := range []struct {
		path string
		run  func(*numberPartsSuite, intervalSource)
	}{
		{
			path: "icu4c/source/test/cintltst/unumberformattertst.c",
			run:  (*numberPartsSuite).cFormatter,
		},
		{
			path: "icu4c/source/test/intltest/numbertest_api.cpp",
			run:  (*numberPartsSuite).cppFormatter,
		},
		{
			path: "icu4c/source/test/intltest/numbertest_simple.cpp",
			run:  (*numberPartsSuite).simpleInteger,
		},
		{
			path: "icu4c/source/test/intltest/numfmtst.cpp",
			run:  (*numberPartsSuite).formatAttributes,
		},
		{
			path: "icu4j/main/common_tests/src/test/java/com/ibm/icu/dev/test/format/NumberFormatTest.java",
			run:  (*numberPartsSuite).accounting,
		},
	} {
		name := filepath.Base(file.path)
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, file.path)
			f := newFormatFixture(t, path)
			s := &numberPartsSuite{
				formatFixture: f,
				format: f.function(`(locale, options, input) => new Intl.NumberFormat(locale, options)
					.formatToParts(input).map(p => p.type + " " + p.value).join("\n")`),
				reference: references[name],
			}
			file.run(s, readIntervalSource(t, path))
			for line := range s.reference {
				t.Errorf("%s:%d: unused reference fields", path, line)
			}
		})
	}
}

func readNumberFieldReferences(t *testing.T) map[string]map[int][]intervalPosition {
	references := map[string]map[int][]intervalPosition{}
	fixtureLines(t, "internal/icutest/testdata/number-fields.txt", func(line int, row string) {
		cols := strings.Fields(row)
		if len(cols) == 0 || strings.HasPrefix(cols[0], "#") {
			return
		}
		file, n, _ := strings.Cut(cols[0], ":")
		if references[file] == nil {
			references[file] = map[int][]intervalPosition{}
		}
		var fields []intervalPosition
		for _, col := range cols[1:] {
			kind, offsets, _ := strings.Cut(col, ":")
			begin, end, _ := strings.Cut(offsets, "-")
			fields = append(fields, intervalPosition{
				label: kind,
				begin: integer(begin),
				end:   integer(end),
			})
		}
		key := integer(n)
		if len(fields) == 0 || references[file][key] != nil {
			t.Fatalf("number-fields.txt:%d: empty or duplicate fields for %s", line, cols[0])
		}
		references[file][key] = fields
	})
	return references
}

func (s *numberPartsSuite) checkParts(line int, locale, skeleton string, input any, want string, rows []intervalRow) {
	s.t.Helper()
	units := utf16.Encode([]rune(want))
	var fields []intervalPosition
	for _, row := range rows {
		cols := row.columns(s.t, 3)
		fields = append(fields, intervalPosition{
			label: cols[0],
			begin: integer(cols[1]),
			end:   integer(cols[2]),
		})
	}
	if rows == nil {
		fields = s.reference[line]
		delete(s.reference, line)
	}
	if len(fields) == 0 {
		s.t.Fatalf("%s:%d: missing fields for %q", s.path, line, want)
	}
	for i, field := range fields {
		if field.begin < 0 || field.end <= field.begin || field.end > len(units) {
			s.t.Fatalf("%s:%d: invalid UTF-16 range %d..%d in %q", s.path, line, field.begin, field.end, want)
		}
		if rows != nil {
			fields[i].label = formatFieldType(s.t, field.label, units[field.begin:field.end], rows[i].line)
		}
	}
	options, divisor, reason := numberSkeletonOptions(strings.Fields(skeleton))
	if reason != "" || divisor != 1 {
		s.t.Fatalf("%s:%d: unmapped skeleton %q (divisor %d, reason %q)", s.path, line, skeleton, divisor, reason)
	}
	s.check(line, "formatToParts", s.format, positionedParts(units, fields, nil, ""), locale, options, input)
}
