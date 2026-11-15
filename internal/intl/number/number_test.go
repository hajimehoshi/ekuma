// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package number_test

import (
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
	} {
		got := number.String(number.NewFormatter(&tc.o).Format(number.Parse(tc.in)))
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
