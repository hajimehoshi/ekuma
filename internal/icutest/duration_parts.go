// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"slices"
	"strconv"
	"strings"
)

// joinedDurationParts substitutes CLDR unit patterns into ICU's annotated nine-element unit list.
func (s *durationSuite) joinedDurationParts(src intervalSource, unitPath string) {
	block := src.from("ICU-21383 Long list")
	input := listArray(block, "inputs[] =")
	result := block.from("expectedString =")
	list := listFieldParts(result, intervalString(s.t, result.lines[0]), input, block.table("expectedFieldPositions[] ="), true)
	units := []string{"year", "month", "week", "day", "hour", "minute", "second", "millisecond", "microsecond"}
	if len(input) != len(units) {
		s.t.Fatalf("%s:%d: expected nine list elements", src.path, block.line)
	}
	patterns := readResource(s.t, unitPath).child("unitsShort").child("duration")
	fields := map[string]any{}
	opts := map[string]any{"style": "short", "yearsDisplay": "always"}
	var want strings.Builder
	var parts []string
	for _, field := range list {
		if field.kind == "literal" {
			want.WriteString(field.value)
			parts = append(parts, "literal "+field.value)
			continue
		}
		i := slices.Index(input, field.value)
		if i < 0 {
			s.t.Fatalf("%s:%d: unknown list element %q", src.path, result.line, field.value)
		}
		unit := units[i]
		fields[unit+"s"] = -i
		plural := "other"
		if i == 1 {
			plural = "one"
		}
		pattern := patterns.child(unit).child(plural)
		if len(pattern.values) != 1 || !strings.HasPrefix(pattern.values[0], "{0} ") {
			s.t.Fatalf("%s:%d: unexpected English short pattern for %s/%s", unitPath, pattern.line, unit, plural)
		}
		// A negative duration carries one sign on the first displayed
		// number, including an explicitly displayed zero.
		if i == 0 {
			want.WriteByte('-')
			parts = append(parts, "minusSign["+unit+"] -")
		}
		number := strconv.Itoa(i)
		want.WriteString(strings.Replace(pattern.values[0], "{0}", number, 1))
		parts = append(parts, "integer["+unit+"] "+number, "literal["+unit+"]  ",
			"unit["+unit+"] "+strings.TrimPrefix(pattern.values[0], "{0} "))
	}
	s.check(result.line, "format", s.format, want.String(), "en", fields, opts)
	s.check(result.line, "formatToParts", s.parts, strings.Join(parts, "\n"), "en", fields, opts)
}
