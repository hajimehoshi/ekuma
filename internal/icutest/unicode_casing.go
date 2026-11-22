// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

type caseMapping struct {
	lower, upper string
}

type casingRow struct {
	line            int
	cp              rune
	locale, context string
}

type casingRule struct {
	casingRow
	mapping caseMapping
}

type casingData struct {
	defaults                map[rune]caseMapping
	conditional             map[rune][]casingRule
	simpleRows, specialRows []casingRow
	unmapped, unconditional int
}

func readCasingData(t *testing.T, dir string) *casingData {
	d := &casingData{
		defaults:    make(map[rune]caseMapping),
		conditional: make(map[rune][]casingRule),
	}
	d.readSimple(t, filepath.Join(dir, "UnicodeData.txt"))
	d.readSpecial(t, filepath.Join(dir, "SpecialCasing.txt"))
	return d
}

func (d *casingData) readSimple(t *testing.T, path string) {
	fixtureLines(t, path, func(line int, text string) {
		fields := strings.Split(text, ";")
		if len(fields) != 15 {
			t.Fatalf("%s:%d: got %d fields, want 15", path, line, len(fields))
		}
		if fields[12] == "" && fields[13] == "" {
			d.unmapped++
			return
		}
		scalar := func(field string) rune {
			r, err := parseScalar(field)
			if err != nil {
				t.Fatalf("%s:%d: %v", path, line, err)
			}
			return r
		}
		cp := scalar(fields[0])
		m := caseMapping{
			lower: string(cp),
			upper: string(cp),
		}
		if fields[12] != "" {
			m.upper = string(scalar(fields[12]))
		}
		if fields[13] != "" {
			m.lower = string(scalar(fields[13]))
		}
		d.defaults[cp] = m
		d.simpleRows = append(d.simpleRows, casingRow{
			line: line,
			cp:   cp,
		})
	})
}

func (d *casingData) readSpecial(t *testing.T, path string) {
	fixtureLines(t, path, func(line int, text string) {
		text, _, _ = strings.Cut(text, "#")
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		fields := strings.Split(strings.TrimSuffix(text, ";"), ";")
		if len(fields) != 4 && len(fields) != 5 {
			t.Fatalf("%s:%d: invalid special casing record %q", path, line, text)
		}
		cp, err := parseScalar(strings.TrimSpace(fields[0]))
		if err != nil {
			t.Fatalf("%s:%d: %v", path, line, err)
		}
		sequence := func(field string) string {
			if strings.TrimSpace(field) == "" {
				return ""
			}
			s, err := parseCodePoints(field)
			if err != nil {
				t.Fatalf("%s:%d: %U: %v", path, line, cp, err)
			}
			return s
		}
		m := caseMapping{
			lower: sequence(fields[1]),
			upper: sequence(fields[3]),
		}
		row := casingRow{
			line: line,
			cp:   cp,
		}
		if len(fields) == 4 {
			d.defaults[cp] = m
			d.unconditional++
		} else {
			for condition := range strings.FieldsSeq(fields[4]) {
				switch condition {
				case "lt", "tr", "az":
					row.locale = condition
				case "Final_Sigma", "After_Soft_Dotted", "More_Above", "After_I", "Not_Before_Dot":
					row.context = condition
				default:
					t.Fatalf("%s:%d: %U: unknown casing condition %q", path, line, cp, condition)
				}
			}
			d.conditional[cp] = append(d.conditional[cp], casingRule{
				casingRow: row,
				mapping:   m,
			})
		}
		d.specialRows = append(d.specialRows, row)
	})
}

// Each sample contains the source character once, at @. The named context
// identifies the only contextual rule satisfied anywhere in the sample.
// For example, in I + dot, After_I applies to the dot.
type casingSample struct {
	pattern, active string
}

func casingSamples(context string) []casingSample {
	// U+0323 (below) is transparent to the dot rules. U+0301 (above)
	// blocks a search for a dot, and a starter ends a combining sequence.
	switch context {
	case "Final_Sigma":
		return []casingSample{
			{
				pattern: "A\u0301@\u0301",
				active:  context,
			},
			{
				pattern: "A\u0301@\u0301B",
			},
			{
				pattern: ".\u0301@",
			},
		}
	case "After_Soft_Dotted":
		return []casingSample{
			{
				pattern: "i\u0323@",
				active:  context,
			},
			{
				pattern: "i\u0301@",
			},
		}
	case "More_Above":
		return []casingSample{
			{
				pattern: "@\u0323\u0301",
				active:  context,
			},
			{
				pattern: "@\u0323A\u0301",
			},
		}
	case "After_I":
		return []casingSample{
			{
				pattern: "I\u0323@",
				active:  context,
			},
			{
				pattern: "A\u0323@",
			},
		}
	case "Not_Before_Dot":
		return []casingSample{
			{
				pattern: "@\u0301\u0307",
				active:  context,
			},
			{
				pattern: "@\u0307",
				active:  "After_I",
			},
		}
	default:
		return []casingSample{
			{
				pattern: "@",
			},
		}
	}
}

func (d *casingData) expected(input, locale, active string) caseMapping {
	var lower, upper strings.Builder
	for _, cp := range input {
		m, ok := d.defaults[cp]
		if !ok {
			m = caseMapping{
				lower: string(cp),
				upper: string(cp),
			}
		}
		for _, rule := range d.conditional[cp] {
			if rule.locale != "" && rule.locale != locale {
				continue
			}
			if rule.context == "" || rule.context == active {
				m = rule.mapping
				break
			}
		}
		lower.WriteString(m.lower)
		upper.WriteString(m.upper)
	}
	return caseMapping{
		lower: lower.String(),
		upper: upper.String(),
	}
}

func runUnicodeCasing(t *testing.T, dir, file string) {
	d := readCasingData(t, dir)
	rows := d.simpleRows
	if file == "SpecialCasing.txt" {
		rows = d.specialRows
	}
	var vm ekuma.VM
	var checks int
	for _, row := range rows {
		for i, sample := range casingSamples(row.context) {
			input := strings.Replace(sample.pattern, "@", string(row.cp), 1)
			locales := []string{row.locale}
			if row.locale != "" && i == 0 {
				locales = append(locales, "en")
			}
			for _, locale := range locales {
				want := d.expected(input, locale, sample.active)
				checkMapping(t, &vm, file, row, input, locale, sample.active, want)
				checks += 2
			}
		}
	}
	if file == "UnicodeData.txt" {
		if len(rows) != 2989 {
			t.Errorf("%s: got %d mapped rows, want 2989 in Unicode 17", file, len(rows))
		}
		t.Logf("%d rows with simple mappings, %d checks executed, %d rows excluded (no simple upper/lower mapping), 0 known differences", len(rows), checks, d.unmapped)
	} else {
		conditional := len(rows) - d.unconditional
		if d.unconditional != 103 || conditional != 16 {
			t.Errorf("%s: got %d unconditional and %d conditional rows, want 103 and 16 in Unicode 17", file, d.unconditional, conditional)
		}
		t.Logf("%d unconditional and %d conditional rows, %d checks executed, 0 rows excluded (titlecase column unused), 0 known differences", d.unconditional, conditional, checks)
	}
}

func checkMapping(t *testing.T, vm *ekuma.VM, file string, row casingRow, input, locale, active string, want caseMapping) {
	t.Helper()
	prefix := "to"
	var args []any
	if locale != "" {
		prefix = "toLocale"
		args = append(args, locale)
	}
	check := func(op, expected string) {
		t.Helper()
		got, err := vm.ValueOf(input).Call(t.Context(), prefix+op, args...)
		if err != nil || !got.Equal(vm.ValueOf(expected)) {
			t.Errorf("%s:%d: %U in %+q.%s(%v), rule %q, active context %q: got %+q (error %v), want %+q",
				file, row.line, row.cp, input, prefix+op, args, row.context, active, got.String(), err, expected)
		}
	}
	check("UpperCase", want.upper)
	check("LowerCase", want.lower)
}
