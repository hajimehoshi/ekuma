// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var digitsPattern = regexp.MustCompile(`^[#@0-9,]*(?:\.[#0-9]*)?(?:E\+?0+)?$`)

var roundingModes = map[string]string{
	"default": "halfEven", "ceiling": "ceil", "floor": "floor", "up": "expand", "down": "trunc",
	"halfeven": "halfEven", "halfup": "halfExpand", "halfdown": "halfTrunc", "halfceiling": "halfCeil", "halffloor": "halfFloor",
}

func digitSetting(s map[string]string, k string, def int) int {
	if v := s[k]; v != "" {
		return integer(v)
	}
	return def
}

// decimalOptions maps the subset of DecimalFormat patterns and setters with
// an ECMA-402 equivalent. DecimalFormat defaults to half-even rounding.
func decimalOptions(pattern string, s map[string]string) (map[string]any, string) {
	mode := strings.ToLower(cmp.Or(s["roundingMode"], "default"))
	if roundingModes[mode] == "" {
		return nil, "Intl has no " + mode + " rounding mode"
	}
	o := map[string]any{"roundingMode": roundingModes[mode], "useGrouping": false}
	if s["formatWidth"] != "" || s["padPosition"] != "" || strings.Contains(pattern, "*") {
		return nil, "ICU pads the formatted text to a field width"
	}
	if s["positivePrefix"] != "" || s["positiveSuffix"] != "" || s["negativePrefix"] != "" || s["negativeSuffix"] != "" {
		return nil, "ICU supplies literal prefix or suffix overrides"
	}
	if s["currencyUsage"] == "cash" {
		return nil, "Intl has no cash currency usage setting"
	}

	if s["decimalSeparatorAlwaysShown"] == "1" || strings.HasSuffix(pattern, ".") {
		return nil, "Intl cannot force a decimal separator after an integer"
	}
	if s["exponentSignAlwaysShown"] == "1" || digitSetting(s, "minimumExponentDigits", 1) > 1 {
		return nil, "Intl cannot customize the exponent sign or width"
	}
	if s["signAlwaysShown"] == "1" {
		o["signDisplay"] = "always"
	}
	switch {
	case pattern == "+0;-#":
		pattern = "0"
		o["signDisplay"] = "always"
	case pattern == "0;0":
		pattern = "0"
		o["signDisplay"] = "never"
	case strings.HasSuffix(pattern, ";"):
		pattern = strings.TrimSuffix(pattern, ";")
	}
	switch {
	case strings.HasPrefix(pattern, "¤") && strings.Count(pattern, "¤") == 1 && s["locale"] == "en":
		o["style"] = "currency"
		o["currency"] = s["currency"]
		pattern = strings.TrimPrefix(pattern, "¤")
	case strings.Contains(pattern, "¤"):
		return nil, "literal currency affix placement or spacing differs from the locale currency pattern"
	case strings.HasSuffix(pattern, "%") && s["locale"] == "en":
		pattern = strings.TrimSuffix(pattern, "%")
		o["style"] = "percent"
	case strings.Contains(pattern, "%"):
		return nil, "literal percent affix placement or spacing differs from the locale percent pattern"
	case strings.Contains(pattern, "‰"):
		return nil, "Intl has no per-mille style"
	}
	if !digitsPattern.MatchString(pattern) {
		return nil, "ICU pattern supplies literal affixes or a custom negative subpattern"
	}
	if pattern == "" {
		// An empty DecimalFormat pattern has unlimited precision and no grouping.
		o["maximumFractionDigits"] = 100
		return o, ""
	}
	mantissa, exp, scientific := strings.Cut(pattern, "E")
	if exp != "" && exp != "0" {
		return nil, "Intl cannot customize the exponent sign or width"
	}
	scientific = scientific || s["useScientific"] == "1"
	whole, frac, point := strings.Cut(mantissa, ".")
	groups := strings.Split(whole, ",")
	if len(groups) > 1 || s["useGrouping"] == "1" {
		primary := digitSetting(s, "grouping", len(groups[len(groups)-1]))
		secondary := primary
		if len(groups) > 2 {
			secondary = len(groups[len(groups)-2])
		}
		secondary = digitSetting(s, "grouping2", secondary)
		if primary != 3 || secondary != 3 {
			return nil, "ICU overrides the locale's primary or secondary grouping width"
		}
		o["useGrouping"] = "always"
		if s["minGroupingDigits"] == "2" {
			o["useGrouping"] = "min2"
		}
	}
	if s["useGrouping"] == "0" {
		o["useGrouping"] = false
	}
	whole = strings.ReplaceAll(whole, ",", "")
	minInt := digitSetting(s, "minIntegerDigits", len(strings.TrimLeft(whole, "#")))
	maxInt := digitSetting(s, "maxIntegerDigits", len(whole))
	minFrac := digitSetting(s, "minFractionDigits", len(strings.TrimRight(frac, "#")))
	maxFrac := digitSetting(s, "maxFractionDigits", max(minFrac, len(frac)))
	minSig := digitSetting(s, "minSigDigits", strings.Count(whole, "@"))
	maxSig := digitSetting(s, "maxSigDigits", len(strings.TrimLeft(whole, "#")))
	if scientific {
		switch {
		case strings.Contains(whole, "@") || maxInt <= 1 || maxInt > 8 && minInt <= 1:
			o["notation"] = "scientific"
		case maxInt == 3 && minInt < 3:
			o["notation"] = "engineering"
		default:
			return nil, "ICU uses a mantissa width or exponent interval unavailable in Intl"
		}
		if minInt == 0 && (whole == "" || maxInt == 0 && maxFrac != 0) {
			return nil, "ICU suppresses the scientific mantissa's leading zero"
		}
		if !strings.Contains(whole, "@") && s["maxSigDigits"] == "" {
			// Scientific DecimalFormat rounds the mantissa to the sum of
			// the minimum integer width and the fraction digit count.
			minSig = max(1, minInt+minFrac)
			maxSig = minInt + maxFrac
			if maxInt > minInt && minInt > 1 {
				minSig = 1 + minFrac
			}
			if minInt == 0 && minFrac == 0 && maxFrac > 0 {
				maxSig++
			}
			if maxSig == 0 {
				maxSig = 21
			}
			maxSig = max(maxSig, minSig)
		}
	} else {
		if s["maxIntegerDigits"] != "" {
			return nil, "Intl has no maximumIntegerDigits option"
		}
		if !strings.Contains(whole, "@") && minInt == 0 && (whole == "" || minFrac > 0 || point && frac == "") {
			return nil, "ICU suppresses the integer zero or forces the decimal separator"
		}
		if !strings.Contains(whole, "@") {
			o["minimumIntegerDigits"] = max(1, minInt)
		}
	}
	if scientific || minSig != 0 || s["maxSigDigits"] != "" {
		if maxSig > 21 {
			return nil, "Intl allows at most 21 significant digits"
		}
		o["minimumSignificantDigits"] = max(1, minSig)
		o["maximumSignificantDigits"] = max(1, maxSig)
	} else if o["style"] != "currency" {
		o["minimumFractionDigits"] = minFrac
		o["maximumFractionDigits"] = maxFrac
	}
	incText := s["roundingIncrement"]
	if incText == "" && strings.ContainsAny(mantissa, "123456789") {
		incText = strings.Map(func(c rune) rune {
			if c == '#' || c == ',' {
				return -1
			}
			return c
		}, mantissa)
	}
	if incText != "" {
		fracInc := strings.TrimLeft(strings.ReplaceAll(incText, ".", ""), "0")
		inc, err := strconv.Atoi(fracInc)
		if err != nil || !slices.Contains([]int{1, 2, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1000, 2000, 2500, 5000}, inc) {
			return nil, "ICU rounding increment is outside the ECMA-402 increment set"
		}
		if strings.Contains(whole, "@") || s["maxSigDigits"] != "" || minFrac != maxFrac || scientific && (minInt != 1 || maxInt != 1) {
			return nil, "Intl rounding increments require fixed fraction digits"
		}
		delete(o, "minimumSignificantDigits")
		delete(o, "maximumSignificantDigits")
		o["minimumFractionDigits"] = minFrac
		o["maximumFractionDigits"] = maxFrac
		o["roundingIncrement"] = inc
	}
	return o, ""
}
