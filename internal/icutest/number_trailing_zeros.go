// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

func boundNumberFormat(f *formatFixture, factory ekuma.Value, locale, skeleton string) ekuma.Value {
	f.t.Helper()
	options, divisor, reason := numberSkeletonOptions(strings.Fields(skeleton))
	if divisor != 1 || reason != "" {
		f.t.Fatalf("%s: unmapped skeleton %q (divisor %d, reason %q)", f.path, skeleton, divisor, reason)
	}
	format, err := factory.Invoke(f.t.Context(), locale, options)
	if err != nil {
		f.t.Fatal(err)
	}
	return format
}

const numberFormatFactory = `(locale, options) => new Intl.NumberFormat(locale, options).format`

func runIncrementTrailingZeros(t *testing.T, path string) {
	f := newFormatFixture(t, path)
	block := readIntervalSource(t, path).from("void Test21674_State(void) {")
	settings := listStrings(t, block.from("unumf_openForSkeletonAndLocale(").lines[0])
	format := boundNumberFormat(f, f.function(numberFormatFactory), settings[1], settings[0])
	for _, row := range block.table("TestCase cases[] =") {
		col := row.columns(t, 2)
		f.check(row.line, "format", format, intervalString(t, col[1]), fixtureFloat(block, col[0]))
	}
}

func runPrecisionTrailingZeros(t *testing.T, path string) {
	f := newFormatFixture(t, path)
	factory := f.function(numberFormatFactory)
	src := readIntervalSource(t, path)
	for _, name := range []string{
		"Hide If Whole A",
		"Hide If Whole B",
		"Hide If Whole with Rounding Mode A (ICU-21881)",
		"Hide If Whole with Rounding Mode B (ICU-21881)",
		"FracSig with Trailing Zero Display",
	} {
		call := src.from(`"` + name + `"`)
		format := boundNumberFormat(f, factory, "en", intervalString(t, call.lines[1]))
		args := call.from("ULocale.ENGLISH,")
		input := fixtureFloat(args, strings.TrimSuffix(args.lines[1], ","))
		f.check(args.line+2, name, format, intervalString(t, args.lines[2]), input)
		f.excluded["matching ICU4C formatter case"]++
	}

	call := src.from(`"Currency Standard with Trailing Zero Display"`)
	format := boundNumberFormat(f, factory, "en", intervalString(t, call.lines[1]))
	results := call.from("ULocale.ENGLISH,")
	inputs := src.from("static void assertFormatDescending(").from("new double[] {")
	values := strings.TrimSuffix(strings.TrimPrefix(inputs.lines[0], "new double[] {"), "};")
	for i, value := range strings.Split(values, ",") {
		result := results.lines[i+1]
		f.check(results.line+i+1, "currency", format, intervalString(t, result), fixtureFloat(inputs, strings.TrimSpace(value)))
		f.excluded["matching ICU4C formatter case"]++
	}

	block := src.from("void testCurrencyPrecisionCacheMutation()")
	currency := intervalString(t, block.from("Currency.getInstance(").lines[0])
	for _, name := range []string{"actual1", "actual2", "actual3"} {
		call := block.from("String " + name + " =")
		input := call.from(".format(")
		skeleton := "currency/" + currency + " precision-currency-standard"
		if strings.Contains(strings.Join(call.lines[:input.line-call.line], ""), "TrailingZeroDisplay.HIDE_IF_WHOLE") {
			skeleton += "/w"
		}
		format := boundNumberFormat(f, factory, "en-US", skeleton)
		value := strings.TrimSuffix(strings.TrimPrefix(input.lines[0], ".format("), ")")
		result := input.from("assertEquals(")
		f.check(result.line, name, format, listStrings(t, result.lines[0])[1], fixtureFloat(input, value))
	}
}
