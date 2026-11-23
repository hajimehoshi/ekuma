// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"cmp"
	"maps"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

type numberChecks struct {
	*formatFixture
	format, selectPlural ekuma.Value
}

func newNumberChecks(t *testing.T, path string) *numberChecks {
	f := newFormatFixture(t, path)
	return &numberChecks{
		formatFixture: f,
		format:        f.function(`(locale, options, input) => new Intl.NumberFormat(locale, options).format(input)`),
		selectPlural:  f.function(`(locale, options, input) => new Intl.PluralRules(locale, options).select(Number(input))`),
	}
}

func (f *numberChecks) decimal(line int, settings map[string]string) {
	input := settings["format"]
	pattern, hasPattern := settings["pattern"]
	if !hasPattern {
		pattern = "0"
	}
	locale := strings.ReplaceAll(cmp.Or(settings["locale"], "en"), "_", "-")
	opts, reason := decimalOptions(pattern, settings)
	if reason != "" {
		f.excluded[reason]++
		return
	}
	if input == "Inf" || input == "-Inf" {
		input += "inity"
	}
	op := "Intl.NumberFormat.format pattern=" + pattern
	if multiplier := settings["multiplier"]; multiplier != "" && multiplier != "1" {
		op += " multiplier=" + multiplier + " input=" + input
		if multiplier == "0" {
			f.excluded["zero multiplier is ignored by ICU4C and rejected by ICU4J"]++
			return
		}
		input = scaleDecimal(input, int64(integer(multiplier)), 1)
	}
	if want := settings["plural"]; want != "" {
		delete(opts, "useGrouping")
		f.check(line, "Intl.PluralRules.select", f.selectPlural, want, locale, opts, input)
		return
	}
	f.check(line, op, f.format, settings["output"], locale, opts, input)
}

// scaleDecimal applies an ICU multiplier without passing the input through binary64.
func scaleDecimal(s string, numerator, denominator int64) string {
	switch s {
	case "NaN", "Infinity", "-Infinity":
		if numerator < 0 && s != "NaN" {
			if s == "Infinity" {
				return "-Infinity"
			}
			return "Infinity"
		}
		return s
	}
	v, ok := new(big.Rat).SetString(s)
	if !ok {
		panic("invalid decimal input " + s)
	}
	v.Mul(v, big.NewRat(numerator, denominator))
	if v.Sign() == 0 && strings.HasPrefix(s, "-") != (numerator < 0) {
		return "-0"
	}
	places, exact := v.FloatPrec()
	if !exact {
		panic("non-terminating decimal scale")
	}
	return v.FloatString(places)
}

var unicodeEscape = regexp.MustCompile(`\\u[0-9A-Fa-f]{4}`)

func decodeEscapes(s string) string {
	return unicodeEscape.ReplaceAllStringFunc(s, func(escape string) string {
		value, _ := strconv.ParseUint(escape[2:], 16, 16)
		return string(rune(value))
	})
}

func runNumberSpecification(t *testing.T, path string, known map[int]string) {
	f := newNumberChecks(t, path)
	f.track("Intl.PluralRules.select", known)
	var defaults map[string]string
	var headers []string
	var afterBegin bool
	fixtureLines(t, path, func(line int, row string) {
		switch {
		case strings.TrimSpace(row) == "" || strings.HasPrefix(row, "//"):
			return
		case strings.HasPrefix(row, "test "):
			defaults = map[string]string{}
			headers = nil
			return
		case strings.HasPrefix(row, "set "):
			k, v, _ := strings.Cut(row[4:], " ")
			defaults[k] = decodeEscapes(v)
			return
		case row == "begin":
			afterBegin = true
			return
		case afterBegin:
			headers = strings.Split(row, "\t")
			afterBegin = false
			return
		}
		fields := strings.Split(row, "\t")
		if len(headers) == 0 || len(fields) > len(headers) {
			t.Fatalf("%s:%d: invalid row %q under %v", path, line, row, headers)
		}
		s := maps.Clone(defaults)
		for i, h := range headers {
			delete(s, h)
			if i < len(fields) {
				s[h] = decodeEscapes(fields[i])
			}
		}
		_, hasParse := s["parse"]
		switch {
		case hasParse:
			f.excluded["Intl.NumberFormat has no parsing operation"]++
		case s["toPattern"] != "" || s["toLocalizedPattern"] != "":
			f.excluded["Intl does not serialize ICU patterns"]++
		case s["output"] == "fail":
			f.excluded["ICU pattern syntax rejection"]++
		case s["format"] != "" && (s["output"] != "" || s["plural"] != ""):
			f.decimal(line, s)
		default:
			t.Fatalf("%s:%d: unrecognized operation %v", path, line, s)
		}
	})
}

func runDecimalFormat(t *testing.T, path string) {
	f := newNumberChecks(t, path)
	formatRow := regexp.MustCompile(`^format\s+(\S+)\s+(\w+)\s+"([^"]*)"\s+"([^"]*)"(?:\s*#.*)?$`)
	fixtureLines(t, path, func(line int, row string) {
		row = strings.TrimSpace(row)
		if row == "" || strings.HasPrefix(row, "#") {
			return
		}
		if strings.HasPrefix(row, "parse ") {
			f.excluded["Intl.NumberFormat has no parsing operation"]++
			return
		}
		m := formatRow.FindStringSubmatch(row)
		if m == nil {
			t.Fatalf("%s:%d: unknown decimal-format case %q", path, line, row)
		}
		f.decimal(line, map[string]string{"locale": "en-US", "pattern": m[1], "roundingMode": m[2], "format": m[3], "output": m[4]})
	})
}

func runNumberCases(t *testing.T, path string) {
	f := newNumberChecks(t, path)
	token := regexp.MustCompile(`"[^"]*"|\S+`)
	locale := "en-US"
	var pattern, currencyLocale string
	fixtureLines(t, path, func(line int, row string) {
		row = strings.TrimSpace(row)
		if row == "" || strings.HasPrefix(row, "#") {
			return
		}
		words := token.FindAllString(row, -1)
		for i, w := range words {
			words[i] = decodeEscapes(strings.Trim(w, "\""))
		}
		switch words[0] {
		case "ref=":
			if words[1] != "#.#" {
				t.Fatalf("%s:%d: unknown numeric input syntax %q", path, line, words[1])
			}
		case "loc=":
			locale = strings.ReplaceAll(words[1], "_", "-")
		case "strict=": // Applies only to parsing.
		case "pat:":
			if words[2] == "err" {
				f.excluded["ICU pattern syntax rejection"]++
			} else {
				f.excluded["Intl does not serialize ICU patterns"]++
			}
		case "p:":
			f.excluded["Intl.NumberFormat has no parsing operation"]++
		case "fp:", "rt:", "f:":
			if words[1] != "-" {
				pattern = words[1]
			}
			if words[0] != "f:" {
				f.excluded["parse side of the format/parse round trip"]++
			}
			f.decimal(line, map[string]string{"locale": locale, "pattern": pattern, "format": words[2], "output": words[3]})
		case "fpc:":
			if words[1] != "-" {
				currencyLocale = strings.ReplaceAll(words[1], "_", "-")
			}
			input, currency, _ := strings.Cut(words[2], "/")
			f.check(line, "Intl.NumberFormat.format", f.format, words[3], currencyLocale, map[string]any{"style": "currency", "currency": currency, "roundingMode": "halfEven"}, input)
			f.excluded["parse side of the currency round trip"]++
		default:
			t.Fatalf("%s:%d: unknown directive %q", path, line, words[0])
		}
	})
}

func runCompactSerbian(t *testing.T, path string) {
	f := newNumberChecks(t, path)
	src := readIntervalSource(t, path)
	opts := map[string]any{"notation": "compact", "compactDisplay": "long", "maximumSignificantDigits": 2}
	for _, row := range src.table("Object[][] SerbianTestDataLongNegative =") {
		c := row.columns(t, 2)
		literal, single := strings.CutSuffix(c[0], "f")
		bits := 64
		if single {
			bits = 32
		}
		input, err := strconv.ParseFloat(literal, bits)
		if err != nil {
			t.Fatalf("%s:%d: %v", path, row.line, err)
		}
		f.check(row.line, "TestSerbianLongNegative", f.format, intervalString(t, c[1]), "sr", opts, input)
	}
}

func runCurrencyFractionDigits(t *testing.T, path string) {
	f := newNumberChecks(t, path)
	block := readIntervalSource(t, path).from("static void TestFractionDigitOverride(void) {")
	locale := strings.ReplaceAll(intervalString(t, block.from("unum_open(").lines[0]), "_", "-")
	opts := map[string]any{"style": "currency", "currency": "HUF"}
	var input float64
	for i, line := range block.lines {
		switch {
		case strings.HasPrefix(line, "unum_setAttribute(fmt, UNUM_FRACTION_DIGITS, "):
			digits := integer(strings.TrimSuffix(strings.TrimPrefix(line, "unum_setAttribute(fmt, UNUM_FRACTION_DIGITS, "), ");"))
			opts["minimumFractionDigits"] = digits
			opts["maximumFractionDigits"] = digits
		case strings.HasPrefix(line, "unum_formatDouble("):
			input = fixtureFloat(block, strings.TrimSpace(strings.Split(line, ",")[1]))
		case strings.HasPrefix(line, "u_unescape("):
			name, _, _ := strings.Cut(strings.TrimPrefix(line, "u_unescape("), ",")
			want := intervalString(t, block.from("const char " + name + "[] =").lines[0])
			f.check(block.line+i, "TestFractionDigitOverride", f.format, want, locale, opts, input)
		case strings.HasPrefix(line, "unum_close("):
			return
		}
	}
	t.Fatalf("%s:%d: unterminated currency fixture", path, block.line)
}
