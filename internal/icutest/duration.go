// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

type durationSuite struct {
	*formatFixture
	format, parts ekuma.Value
}

func runDurationFormats(t *testing.T, root string) {
	const cpp = "icu4c/source/test/intltest/"
	const java = "icu4j/main/core/src/test/java/com/ibm/icu/dev/test/"
	const resources = "icu4j/main/core/src/test/resources/com/ibm/icu/dev/test/duration/testdata/"
	run := func(file string, read func(*durationSuite, intervalSource)) {
		t.Run(filepath.Base(file), func(t *testing.T) {
			path := filepath.Join(root, file)
			f := newFormatFixture(t, path)
			var vm ekuma.VM
			s := durationSuite{
				formatFixture: f,
				format: f.functionIn(&vm, `(locale, duration, options) =>
					new Intl.DurationFormat(locale, options).format(duration)`),
				parts: f.functionIn(&vm, `(locale, duration, options) =>
					new Intl.DurationFormat(locale, options).formatToParts(duration).map(p =>
						p.type + ("unit" in p ? "[" + p.unit + "]" : "") + " " + p.value).join("\n")`),
			}
			read(&s, readIntervalSource(t, path))
		})
	}
	run(cpp+"measfmttest.cpp", (*durationSuite).measureFormats)
	run("icu4j/main/common_tests/src/test/java/com/ibm/icu/dev/test/format/MeasureUnitTest.java", (*durationSuite).numericTime)
	run(java+"format/TimeUnitTest.java", (*durationSuite).timeUnits)
	run(java+"duration/ICUDurationTest.java", (*durationSuite).xmlDurations)
	run(cpp+"numbertest_api.cpp", (*durationSuite).negativeDurations)
	run(cpp+"listformattertest.cpp", func(s *durationSuite, src intervalSource) {
		s.joinedDurationParts(src, filepath.Join(root, "icu4c/source/data/unit/en.txt"))
	})
	for _, locale := range []string{"ar_EG", "en", "es", "he_IL", "ko", "th", "zh_Hans", "zh_Hans_SG"} {
		run(resources+"testdata_"+locale+".txt", func(s *durationSuite, src intervalSource) {
			s.periodFractions(src, strings.ReplaceAll(locale, "_", "-"))
		})
	}
}

var measureUnits = []string{"year", "month", "week", "day", "hour", "minute", "second", "millisecond"}

type durationMeasure struct {
	unit, amount string
}

// decimalDuration splits a nonnegative decimal into integral duration fields.
func decimalDuration(amount string, unit string) map[string]any {
	units := []string{"seconds", "milliseconds", "microseconds", "nanoseconds"}
	units = units[slices.Index(units, unit):]
	whole, fraction, _ := strings.Cut(amount, ".")
	fields := map[string]any{unit: integer(whole)}
	precision := 3 * (len(units) - 1)
	if len(fraction) > precision {
		panic("icutest: fixture exceeds nanosecond precision: " + amount)
	}
	fraction += strings.Repeat("0", precision-len(fraction))
	for i, unit := range units[1:] {
		if value := integer(fraction[i*3 : i*3+3]); value != 0 {
			fields[unit] = value
		}
	}
	return fields
}

func (s *durationSuite) measures(line int, locale, style string, measures []durationMeasure, digits int, want string) {
	fields := map[string]any{}
	opts := map[string]any{"style": style}
	var units []string
	for _, m := range measures {
		if !slices.Contains(measureUnits, m.unit) {
			s.t.Fatalf("%s:%d: unknown measure %q", s.path, line, m.unit)
		}
		if len(units) > 0 && slices.Index(measureUnits, units[len(units)-1]) >= slices.Index(measureUnits, m.unit) {
			s.excluded["repeated or out-of-order units"]++
			return
		}
		units = append(units, m.unit)
		if strings.HasPrefix(m.amount, "-") {
			// The signed measure arrays in these tables have positive later measures.
			s.excluded["mixed-sign measures"]++
			return
		}
		amount := strings.TrimSuffix(m.amount, ".0")
		key := m.unit + "s"
		if strings.Contains(amount, ".") {
			if m.unit != "second" {
				s.excluded["fractional hours or minutes"]++
				return
			}
			maps.Copy(fields, decimalDuration(amount, "seconds"))
			opts["milliseconds"] = "numeric"
			if digits >= 0 {
				opts["fractionalDigits"] = digits
			}
		} else {
			fields[key] = integer(amount)
			if fields[key] == 0 {
				opts[key+"Display"] = "always"
			}
		}
	}
	if style == "digital" {
		switch {
		case len(units) == 1 || !slices.Contains(units, "minute") && !slices.Contains(units, "second"):
			// ICU NUMERIC falls back to NARROW outside multi-field clock patterns.
			opts["style"] = "narrow"
		case units[0] == "minute":
			// The first minute in ICU's m:ss pattern has no minimum padding.
			opts["hours"] = "short"
			opts["hoursDisplay"] = "auto"
			opts["minutes"] = "numeric"
		case !slices.Contains(units, "second"):
			opts["secondsDisplay"] = "auto"
		}
	}
	s.check(line, "format", s.format, want, locale, fields, opts)
	if opts["style"] == "digital" {
		if len(units) == 2 && units[0] == "hour" && units[1] == "second" {
			units = []string{"hour", "minute", "second"}
		}
		s.check(line, "formatToParts", s.parts, s.clockParts(line, want, units, fields), locale, fields, opts)
	}
}

var clockTokens = regexp.MustCompile(`\p{Nd}+|[^\p{Nd}]+`)

func (s *durationSuite) clockParts(line int, want string, units []string, fields map[string]any) string {
	// The source selects hm, ms or hms from the measure sequence. Separators
	// between those fields have no unit. A decimal belongs to seconds.
	tokens := clockTokens.FindAllString(want, -1)
	n := len(units)*2 - 1
	fractional := fields["milliseconds"] != nil || fields["microseconds"] != nil || fields["nanoseconds"] != nil
	if fractional {
		n += 2
	}
	if len(tokens) != n {
		s.t.Fatalf("%s:%d: clock fields %v do not match %q", s.path, line, units, want)
	}
	var parts []string
	for i, unit := range units {
		if i > 0 {
			parts = append(parts, "literal "+tokens[2*i-1])
		}
		parts = append(parts, "integer["+unit+"] "+tokens[2*i])
	}
	if fractional {
		parts = append(parts, "decimal[second] "+tokens[n-2], "fraction[second] "+tokens[n-1])
	}
	return strings.Join(parts, "\n")
}
