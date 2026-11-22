// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"slices"
	"strings"
	"testing"
)

// numberSkeletonOptions returns the NumberFormat options and input divisor for an ICU skeleton.
func numberSkeletonOptions(parts []string) (map[string]any, int, string) {
	o := map[string]any{"roundingMode": "halfEven"}
	divisor := 1
	var width string
	for _, part := range parts {
		switch part {
		case "compact-short", "compact-long":
			o["notation"] = "compact"
			o["compactDisplay"] = strings.TrimPrefix(part, "compact-")
		case "scientific":
			o["notation"] = "scientific"
		case "scientific/+ee/sign-always":
			return nil, 0, "Intl cannot pad or force the sign of the exponent"
		case "percent":
			if slices.Contains(parts, "unit-width-full-name") {
				o["style"] = "unit"
				o["unit"] = "percent"
				o["unitDisplay"] = "long"
			} else {
				// ICU's percent skeleton affixes a symbol without scaling.
				o["style"] = "percent"
				divisor *= 100
			}
		case "%x100":
			o["style"] = "percent"
		case "currency/EUR", "currency/GBP", "currency/CHF", "currency/USD", "currency/JPY", "currency/PTE":
			o["style"] = "currency"
			o["currency"] = strings.TrimPrefix(part, "currency/")
		case "meter", "fahrenheit", "inch":
			o["style"], o["unit"] = "unit", part
		case "measure-unit/length-furlong":
			return nil, 0, "furlong is not an ECMA-402 sanctioned unit"
		case "unit-width-narrow", "unit-width-full-name", "unit-width-iso-code":
			width = part
		case "precision-integer":
			o["maximumFractionDigits"] = 0
			o["minimumFractionDigits"] = 0
		case ".000":
			o["maximumFractionDigits"] = 3
			o["minimumFractionDigits"] = 3
		case ".###":
			o["maximumFractionDigits"] = 3
		case ".##/@@@+":
			o["minimumFractionDigits"] = 0
			o["maximumFractionDigits"] = 2
			o["minimumSignificantDigits"] = 1
			o["maximumSignificantDigits"] = 3
			o["roundingPriority"] = "morePrecision"
		case "@@":
			o["minimumSignificantDigits"] = 2
			o["maximumSignificantDigits"] = 2
		case "rounding-mode-floor":
			o["roundingMode"] = "floor"
		case "rounding-mode-half-up":
			o["roundingMode"] = "halfExpand"
		case "integer-width/##00":
			return nil, 0, "Intl cannot truncate the integer part to four digits"
		case "scale/0.5":
			divisor *= 2
		case "group-on-aligned":
			o["useGrouping"] = "always"
		case "latin":
			o["numberingSystem"] = "latn"
		case "numbering-system/thai", "numbering-system/arab":
			o["numberingSystem"] = strings.TrimPrefix(part, "numbering-system/")
		case "sign-accounting-except-zero":
			o["signDisplay"] = "exceptZero"
			o["currencySign"] = "accounting"
		case "decimal-always":
			return nil, 0, "Intl cannot force a decimal separator after an integer"
		default:
			panic("unknown ICU number skeleton " + part)
		}
	}
	if o["style"] == "currency" {
		switch width {
		case "unit-width-narrow":
			o["currencyDisplay"] = "narrowSymbol"
		case "unit-width-full-name":
			o["currencyDisplay"] = "name"
		case "unit-width-iso-code":
			o["currencyDisplay"] = "code"
		}
	} else if o["style"] == "unit" {
		switch width {
		case "unit-width-full-name":
			o["unitDisplay"] = "long"
		case "unit-width-narrow":
			o["unitDisplay"] = "narrow"
		}
	}
	if o["notation"] != "compact" && o["currency"] == nil && o["maximumSignificantDigits"] == nil && o["maximumFractionDigits"] == nil {
		o["maximumFractionDigits"] = 6
	}
	return o, divisor, ""
}

func runNumberPermutations(t *testing.T, path string) {
	f := newFormatFixture(t, path)
	format := f.function(`(locale, options, input) => new Intl.NumberFormat(locale, options).format(input)`)
	// The upstream reader supplies these formatDouble inputs in each locale block.
	inputs := [...]string{"0", "91827.3645", "-0.22222"}
	locales := [...]string{"es-MX", "zh-TW", "bn-BD"}
	var skeleton, reason, locale string
	var opts map[string]any
	var divisor, localeCount, inputCount int
	endBlock := func(line int) {
		if skeleton != "" && (localeCount != len(locales) || inputCount != len(inputs)) {
			t.Fatalf("%s:%d: incomplete skeleton %q", path, line, skeleton)
		}
	}
	fixtureLines(t, path, func(line int, row string) {
		switch {
		case row == "" || strings.HasPrefix(row, "#"):
			return
		case strings.HasPrefix(row, "    "):
			if localeCount == 0 || inputCount >= len(inputs) {
				t.Fatalf("%s:%d: unexpected result", path, line)
			}
			input := inputs[inputCount]
			inputCount++
			if reason != "" {
				f.excluded[reason]++
				return
			}
			value := input
			if divisor != 1 {
				value = scaleDecimal(input, 1, int64(divisor))
			}
			f.check(line, "Intl.NumberFormat.format "+skeleton+" input="+input, format, row[4:], locale, opts, value)
		case strings.HasPrefix(row, "  "):
			locale = row[2:]
			if localeCount >= len(locales) || locale != locales[localeCount] || (localeCount > 0 && inputCount != len(inputs)) {
				t.Fatalf("%s:%d: unexpected locale block %q", path, line, row)
			}
			localeCount++
			inputCount = 0
		default:
			endBlock(line)
			skeleton = row
			opts, divisor, reason = numberSkeletonOptions(strings.Fields(row))
			localeCount = 0
		}
	})
	endBlock(0)
}
