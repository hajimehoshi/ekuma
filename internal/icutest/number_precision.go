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

func runNumberFormatterPrecision(t *testing.T, path string) {
	f := newFormatFixture(t, path)
	src := readIntervalSource(t, path)
	checkPrecisionTrailingZeros(f, src)
	checkRoundingPriority(f, src)
}

func checkPrecisionTrailingZeros(f *formatFixture, src intervalSource) {
	t := f.t
	factory := f.function(numberFormatFactory)
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

func checkRoundingPriority(f *formatFixture, src intervalSource) {
	block := src.from("void roundingPriorityCoverageTest()")
	format := f.function(`(value, minimumFractionDigits, minimumSignificantDigits, roundingPriority) =>
		new Intl.NumberFormat("en", {
			minimumFractionDigits, maximumFractionDigits: 1,
			minimumSignificantDigits, maximumSignificantDigits: 3,
			roundingPriority, roundingMode: "halfEven", useGrouping: false,
		}).format(value)`)
	for _, row := range block.table("new String[][] {") {
		col := row.columns(f.t, 5)
		input := fixtureFloat(block, intervalString(f.t, col[0]))
		f.check(row.line, "roundingPriority", format, intervalString(f.t, col[1]), input, 0, 1, "morePrecision")
		f.check(row.line, "roundingPriority", format, intervalString(f.t, col[2]), input, 0, 1, "lessPrecision")
		f.check(row.line, "roundingPriority", format, intervalString(f.t, col[3]), input, 1, 3, "morePrecision")
		f.check(row.line, "roundingPriority", format, intervalString(f.t, col[4]), input, 1, 3, "lessPrecision")
		f.excluded["matching ICU4C formatter case"] += 4
	}
}
