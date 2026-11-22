// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package number_test

import (
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/number"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{
			// More digits than a Number holds stay exact.
			in:   " 1.00000000000000000000001e2\n",
			want: "100.000000000000000000001",
		},
		{
			in:   "0x1F",
			want: "31",
		},
		{
			in:   "-0",
			want: "-0",
		},
		{
			// A value that rounds to zero as a Number is zero.
			in:   "-1e-400",
			want: "-0",
		},
		{
			// A value that rounds to an infinity as a Number is one.
			in:   "1e400",
			want: "Infinity",
		},
		{
			in:   "1e1000000000000",
			want: "Infinity",
		},
		{
			in:   "-0x1",
			want: "NaN",
		},
		{
			in:   "1e",
			want: "NaN",
		},
	} {
		d := number.Parse(tc.in)
		var got string
		switch {
		case d.IsNaN():
			got = "NaN"
		case d.IsInf():
			got = "Infinity"
			if d.Neg() {
				got = "-Infinity"
			}
		default:
			got = d.String()
		}
		if got != tc.want {
			t.Errorf("Parse(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatToString(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     string
		digits number.Digits
		want   string
	}{
		{
			// Rounding up to the next power of ten keeps the number of significant digits.
			name: "precision carry",
			in:   "9.996",
			digits: number.Digits{
				MinimumIntegerDigits:     1,
				MinimumSignificantDigits: 3,
				MaximumSignificantDigits: 3,
				RoundingType:             number.SignificantDigits,
				RoundingMode:             "halfExpand",
			},
			want: "10.0",
		},
		{
			name: "increment",
			in:   "1.125",
			digits: number.Digits{
				MinimumIntegerDigits:  1,
				MinimumFractionDigits: 2,
				MaximumFractionDigits: 2,
				RoundingType:          number.FractionDigits,
				RoundingIncrement:     25,
				RoundingMode:          "halfEven",
			},
			want: "1.00",
		},
		{
			// The tie is exact in decimal, though not in binary.
			name: "exact tie",
			in:   "0.125",
			digits: number.Digits{
				MinimumIntegerDigits:  1,
				MaximumFractionDigits: 2,
				RoundingType:          number.FractionDigits,
				RoundingIncrement:     1,
				RoundingMode:          "halfTrunc",
			},
			want: "0.12",
		},
		{
			name: "negative floor",
			in:   "-1.01",
			digits: number.Digits{
				MinimumIntegerDigits:  1,
				MaximumFractionDigits: 1,
				RoundingType:          number.FractionDigits,
				RoundingIncrement:     1,
				RoundingMode:          "floor",
			},
			want: "1.1",
		},
		{
			// Only the digits near the rounding position matter, however long the input is.
			name: "long fraction above a tie",
			in:   "1.005" + strings.Repeat("0", 1<<23) + "1",
			digits: number.Digits{
				MinimumIntegerDigits:  1,
				MaximumFractionDigits: 2,
				RoundingType:          number.FractionDigits,
				RoundingIncrement:     1,
				RoundingMode:          "halfTrunc",
			},
			want: "1.01",
		},
	} {
		got, _ := tc.digits.FormatToString(number.Parse(tc.in))
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFormat(t *testing.T) {
	digits := number.Digits{
		MinimumIntegerDigits:  1,
		MaximumFractionDigits: 3,
		RoundingType:          number.FractionDigits,
		RoundingIncrement:     1,
		RoundingMode:          "halfExpand",
	}
	for _, tc := range []struct {
		name string
		o    number.Options
		in   string
		want string
	}{
		{
			// zh-TW is available as the language-region form of zh-Hant-TW, whose data
			// applies.
			name: "data of the full locale",
			o: number.Options{
				Locale:          "zh-TW",
				NumberingSystem: "latn",
				Style:           "decimal",
				Notation:        "compact",
				CompactDisplay:  "long",
				UseGrouping:     "min2",
				SignDisplay:     "auto",
				Digits:          digits,
			},
			in:   "12000",
			want: "1.2萬",
		},
		{
			// Indian grouping has groups of two digits above the first three, and the
			// digits are transliterated.
			name: "groups of the integer digits",
			o: number.Options{
				Locale:          "hi-IN",
				NumberingSystem: "deva",
				Style:           "decimal",
				Notation:        "standard",
				UseGrouping:     "auto",
				SignDisplay:     "auto",
				Digits:          digits,
			},
			in:   "123456789.5",
			want: "१२,३४,५६,७८९.५",
		},
		{
			// CLDR has no liter-per-hour, but a pattern for "per hour".
			name: "per unit pattern",
			o: number.Options{
				Locale:          "en",
				NumberingSystem: "latn",
				Style:           "unit",
				Unit:            "liter-per-hour",
				UnitDisplay:     "long",
				UseGrouping:     "auto",
				SignDisplay:     "auto",
				Digits:          digits,
			},
			in:   "2",
			want: "2 liters per hour",
		},
		{
			// CLDR has no pattern for "per acre", so the compound pattern applies.
			name: "compound unit pattern",
			o: number.Options{
				Locale:          "en",
				NumberingSystem: "latn",
				Style:           "unit",
				Unit:            "liter-per-acre",
				UnitDisplay:     "short",
				UseGrouping:     "auto",
				SignDisplay:     "auto",
				Digits:          digits,
			},
			in:   "2",
			want: "2 L/ac",
		},
		{
			// A currency symbol that ends in a letter is separated from the digits.
			name: "currency spacing",
			o: number.Options{
				Locale:          "en",
				NumberingSystem: "latn",
				Style:           "currency",
				Currency:        "CHF",
				CurrencyDisplay: "symbol",
				CurrencySign:    "standard",
				UseGrouping:     "auto",
				SignDisplay:     "auto",
				Digits:          digits,
			},
			in:   "-1",
			want: "-CHF 1",
		},
		{
			// de-AT groups currency amounts with a symbol of its own.
			name: "currency group",
			o: number.Options{
				Locale:          "de-AT",
				NumberingSystem: "latn",
				Style:           "currency",
				Currency:        "EUR",
				CurrencyDisplay: "symbol",
				CurrencySign:    "standard",
				UseGrouping:     "auto",
				SignDisplay:     "auto",
				Digits:          digits,
			},
			in:   "1234567.5",
			want: "€\u00a01.234.567,5",
		},
		{
			// The currency symbols apply to currency names too.
			name: "currency decimal",
			o: number.Options{
				Locale:          "fr-CH",
				NumberingSystem: "latn",
				Style:           "currency",
				Currency:        "EUR",
				CurrencyDisplay: "name",
				CurrencySign:    "standard",
				UseGrouping:     "auto",
				SignDisplay:     "auto",
				Digits:          digits,
			},
			in:   "1234567.5",
			want: "1'234'567.5 euros",
		},
		{
			// en-DE has a pattern of its own for EUR, which replaces the accounting pattern.
			// The separators stay those of en-DE.
			name: "currency pattern",
			o: number.Options{
				Locale:          "en-DE",
				NumberingSystem: "latn",
				Style:           "currency",
				Currency:        "EUR",
				CurrencyDisplay: "symbol",
				CurrencySign:    "accounting",
				UseGrouping:     "auto",
				SignDisplay:     "auto",
				Digits:          digits,
			},
			in:   "-1234567.5",
			want: "-€1.234.567,5",
		},
		{
			// kea has a decimal separator of its own for CVE.
			name: "currency separator",
			o: number.Options{
				Locale:          "kea",
				NumberingSystem: "latn",
				Style:           "currency",
				Currency:        "CVE",
				CurrencyDisplay: "symbol",
				CurrencySign:    "standard",
				UseGrouping:     "auto",
				SignDisplay:     "auto",
				Digits:          digits,
			},
			in:   "1234567.5",
			want: "1\u00a0234\u00a0567$5\u00a0\u200b",
		},
		{
			name: "plain group outside currency",
			o: number.Options{
				Locale:          "de-AT",
				NumberingSystem: "latn",
				Style:           "decimal",
				Notation:        "standard",
				UseGrouping:     "auto",
				SignDisplay:     "auto",
				Digits:          digits,
			},
			in:   "1234567.5",
			want: "1\u00a0234\u00a0567,5",
		},
		{
			// en has no symbols for arab, which come from the root locale.
			name: "symbols of the root locale",
			o: number.Options{
				Locale:          "en",
				NumberingSystem: "arab",
				Style:           "decimal",
				Notation:        "standard",
				UseGrouping:     "auto",
				SignDisplay:     "auto",
				Digits:          digits,
			},
			in:   "1.5",
			want: "١٫٥",
		},
	} {
		f := number.NewFormatter(&tc.o)
		x := number.Parse(tc.in)
		if got := f.FormatToString(x, nil); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
		if got := number.String(f.Format(x, nil)); got != tc.want {
			t.Errorf("%s: the parts make %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCompactPatternSelection(t *testing.T) {
	f := number.NewFormatter(&number.Options{
		Locale:                "fr",
		NumberingSystem:       "latn",
		Style:                 "decimal",
		Notation:              "compact",
		CompactDisplay:        "long",
		MinimumIntegerDigits:  1,
		MinimumFractionDigits: 1,
		MaximumFractionDigits: 1,
		RoundingIncrement:     1,
		RoundingMode:          "halfExpand",
	})
	for _, tc := range []struct {
		in   string
		want string
	}{
		{
			in:   "1000",
			want: "mille",
		},
		{
			// Rounding carries into the thousands and the compact value is 1.0.
			in:   "-999.99",
			want: "-mille",
		},
		{
			in:   "1100",
			want: "1,1 millier",
		},
	} {
		if got := f.FormatToString(number.Parse(tc.in), nil); got != tc.want {
			t.Errorf("formatting %s: got %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestFormatPoll checks that the poll function can stop the formatting of a long number.
func TestFormatPoll(t *testing.T) {
	type stop struct{}
	f := number.NewFormatter(&number.Options{
		Locale:               "en",
		NumberingSystem:      "latn",
		Style:                "decimal",
		Notation:             "standard",
		UseGrouping:          "auto",
		SignDisplay:          "auto",
		MinimumIntegerDigits: 1,
		RoundingType:         number.FractionDigits,
		RoundingIncrement:    1,
		RoundingMode:         "halfExpand",
	})
	x := number.FromInteger(strings.Repeat("9", 1<<20))
	for _, format := range []func(func()){
		func(poll func()) { f.Format(x, poll) },
		func(poll func()) { f.FormatToString(x, poll) },
	} {
		func() {
			defer func() {
				if _, ok := recover().(stop); !ok {
					t.Errorf("the formatting did not stop")
				}
			}()
			format(func() { panic(stop{}) })
		}()
	}
}
