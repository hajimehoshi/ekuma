// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package duration formats durations as Intl.DurationFormat does.
package duration

import (
	"math"
	"math/big"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/intl/list"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/intl/number"
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
)

// Units are the units of a duration from the largest, in the plural form of Temporal.
var Units = [...]string{
	"years", "months", "weeks", "days", "hours", "minutes", "seconds", "milliseconds",
	"microseconds", "nanoseconds",
}

// Unit indices into [Units] and the arrays indexed like it.
const (
	Years = iota
	Months
	Weeks
	Days
	Hours
	Minutes
	Seconds
	Milliseconds
	Microseconds
	Nanoseconds
)

// UnitOptions is a Duration Unit Options Record.
type UnitOptions struct {
	// Style is "long", "short", "narrow", "numeric", "2-digit" or "fractional".
	Style string
	// Display is "auto" or "always".
	Display string
}

// DigitalFormat holds the locale data of numeric durations.
type DigitalFormat struct {
	HourMinuteSeparator   string
	MinuteSecondSeparator string
	TwoDigitHours         bool
}

// DigitalFormatOf returns the digital format of the available locale l.
func DigitalFormatOf(l string) DigitalFormat {
	hm, _ := locale.LookupField(cldr.UnitData, l, "Dhm")
	ms, _ := locale.LookupField(cldr.UnitData, l, "Dms")
	return DigitalFormat{
		HourMinuteSeparator:   separator(hm, 'h', 'm'),
		MinuteSecondSeparator: separator(ms, 'm', 's'),
		TwoDigitHours:         strings.HasPrefix(hm, "hh"),
	}
}

// separator returns the text between the last a and the first b of the pattern p.
func separator(p string, a, b byte) string {
	i := strings.LastIndexByte(p, a)
	j := strings.IndexByte(p, b)
	if i < 0 || j <= i {
		return ":"
	}
	return strings.Trim(p[i+1:j], "'")
}

// Options holds the resolved options of an Intl.DurationFormat.
type Options struct {
	// Locale is the available locale whose data is used.
	Locale          string
	NumberingSystem string
	// Style is "long", "short", "narrow" or "digital".
	Style string
	Units [len(Units)]UnitOptions
	// FractionalDigits is the number of fractional digits, or -1 when it is undefined.
	FractionalDigits int
	DigitalFormat
}

// Part is a part of a formatted duration.
type Part struct {
	Type  string
	Value string
	// Unit is the singular unit of a part of a number, or empty.
	Unit string
}

// Duration is a Duration Record. Every field is an integral Number, and no two fields have
// opposite signs.
type Duration [len(Units)]float64

// sign implements DurationSign.
func (d *Duration) sign() int {
	for _, v := range d {
		if v < 0 {
			return -1
		}
		if v > 0 {
			return 1
		}
	}
	return 0
}

// IsValid implements IsValidDuration.
func (d *Duration) IsValid() bool {
	sign := 0
	for _, v := range d {
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return false
		}
		if v < 0 {
			if sign > 0 {
				return false
			}
			sign = -1
		} else if v > 0 {
			if sign < 0 {
				return false
			}
			sign = 1
		}
	}
	for _, v := range d[:Days] {
		if math.Abs(v) >= 1<<32 {
			return false
		}
	}
	s := new(big.Rat)
	for i, scale := range []int64{86400, 3600, 60, 1} {
		s.Add(s, new(big.Rat).Mul(rat(d[Days+i]), big.NewRat(scale, 1)))
	}
	for i, den := range []int64{1e3, 1e6, 1e9} {
		s.Add(s, new(big.Rat).Quo(rat(d[Milliseconds+i]), big.NewRat(den, 1)))
	}
	return s.Abs(s).Cmp(new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), 53))) < 0
}

func rat(f float64) *big.Rat {
	return new(big.Rat).SetFloat64(f)
}

// fraction implements ComputeFractionalDigits.
func (o *Options) fraction(d *Duration) *big.Rat {
	r := new(big.Rat)
	den := big.NewRat(1000, 1)
	for i := Milliseconds; i <= Nanoseconds; i++ {
		if o.Units[i].Style != "fractional" {
			continue
		}
		r.Add(r, new(big.Rat).Quo(rat(d[i]), den))
		den = new(big.Rat).Mul(den, big.NewRat(1000, 1))
	}
	return r
}

// nextUnitFractional implements NextUnitFractional.
func (o *Options) nextUnitFractional(unit int) bool {
	return unit >= Seconds && unit < Nanoseconds && o.Units[unit+1].Style == "fractional"
}

// decimal returns the exact decimal value of r, which has a denominator that divides 10^9.
func decimal(r *big.Rat) number.Decimal {
	s := strings.TrimRight(r.FloatString(9), "0")
	return number.Parse(strings.TrimSuffix(s, "."))
}

// numberOptions returns the options of a NumberFormat with the default options of the
// constructor, for the locale and the numbering system of o.
func (o *Options) numberOptions() *number.Options {
	return &number.Options{
		Locale:                o.Locale,
		NumberingSystem:       o.NumberingSystem,
		Style:                 "decimal",
		Notation:              "standard",
		UseGrouping:           "auto",
		SignDisplay:           "auto",
		MinimumIntegerDigits:  1,
		MaximumFractionDigits: 3,
		RoundingType:          number.FractionDigits,
		RoundingIncrement:     1,
		RoundingMode:          "halfExpand",
	}
}

// setFractionDigits sets the fraction digits of the numbers with a fraction to no.
func (o *Options) setFractionDigits(no *number.Options) {
	if o.FractionalDigits < 0 {
		no.MinimumFractionDigits = 0
		no.MaximumFractionDigits = 9
	} else {
		no.MinimumFractionDigits = o.FractionalDigits
		no.MaximumFractionDigits = o.FractionalDigits
	}
	no.RoundingMode = "trunc"
}

// withUnit returns the parts of the number with the singular unit.
func withUnit(parts []number.Part, unit string) []Part {
	out := make([]Part, len(parts))
	for i, p := range parts {
		out[i] = Part{
			Type:  p.Type,
			Value: p.Value,
			Unit:  unit,
		}
	}
	return out
}

// singular returns the singular form of the unit at the index i.
func singular(i int) string {
	return strings.TrimSuffix(Units[i], "s")
}

// formatNumeric implements FormatNumericHours, FormatNumericMinutes and FormatNumericSeconds
// for the unit i. separator is the separator before the number, or empty.
func (o *Options) formatNumeric(i int, x number.Decimal, separator string, signDisplayed bool) []Part {
	var out []Part
	if separator != "" {
		out = append(out, Part{
			Type:  "literal",
			Value: separator,
		})
	}
	no := o.numberOptions()
	if o.Units[i].Style == "2-digit" {
		no.MinimumIntegerDigits = 2
	}
	if !signDisplayed {
		no.SignDisplay = "never"
	}
	no.UseGrouping = ""
	if i == Seconds {
		o.setFractionDigits(no)
	}
	return append(out, withUnit(number.NewFormatter(no).Format(x, nil), singular(i))...)
}

// negativeZero is -0.
var negativeZero = number.FromFloat(math.Copysign(0, -1))

// formatNumericUnits implements FormatNumericUnits.
func (o *Options) formatNumericUnits(d *Duration, first int, signDisplayed bool) []Part {
	var out []Part
	hours, minutes := d[Hours], d[Minutes]
	seconds := rat(d[Seconds])
	if d[Milliseconds] != 0 || d[Microseconds] != 0 || d[Nanoseconds] != 0 {
		seconds.Add(seconds, o.fraction(d))
	}
	hoursFormatted := first == Hours && (hours != 0 || o.Units[Hours].Display == "always")
	secondsFormatted := seconds.Sign() != 0 || o.Units[Seconds].Display == "always"
	minutesFormatted := false
	if first == Hours || first == Minutes {
		minutesFormatted = hoursFormatted && secondsFormatted || minutes != 0 || o.Units[Minutes].Display == "always"
	}
	value := func(v float64) number.Decimal {
		if signDisplayed && v == 0 && d.sign() < 0 {
			return negativeZero
		}
		return number.FromFloat(v)
	}
	if hoursFormatted {
		out = append(out, o.formatNumeric(Hours, value(hours), "", signDisplayed)...)
		signDisplayed = false
	}
	if minutesFormatted {
		sep := ""
		if hoursFormatted {
			sep = o.HourMinuteSeparator
		}
		out = append(out, o.formatNumeric(Minutes, value(minutes), sep, signDisplayed)...)
		signDisplayed = false
	}
	if secondsFormatted {
		sep := ""
		if minutesFormatted {
			sep = o.MinuteSecondSeparator
		}
		out = append(out, o.formatNumeric(Seconds, decimal(seconds), sep, signDisplayed)...)
	}
	return out
}

// Format implements PartitionDurationFormatPattern.
func (o *Options) Format(d *Duration) []Part {
	var result [][]Part
	signDisplayed := true
	for i := range len(Units) {
		uo := o.Units[i]
		if uo.Style == "numeric" || uo.Style == "2-digit" {
			if parts := o.formatNumericUnits(d, i, signDisplayed); len(parts) > 0 {
				result = append(result, parts)
			}
			break
		}
		no := o.numberOptions()
		v := rat(d[i])
		last := false
		if o.nextUnitFractional(i) {
			v.Add(v, o.fraction(d))
			o.setFractionDigits(no)
			last = true
		}
		if uo.Display == "always" || v.Sign() != 0 {
			x := decimal(v)
			if signDisplayed {
				signDisplayed = false
				if v.Sign() == 0 && d.sign() < 0 {
					x = negativeZero
				}
			} else {
				no.SignDisplay = "never"
			}
			no.Style = "unit"
			no.Unit = singular(i)
			no.UnitDisplay = uo.Style
			result = append(result, withUnit(number.NewFormatter(no).Format(x, nil), singular(i)))
		}
		if last {
			break
		}
	}
	return o.listParts(result)
}

// listParts implements ListFormatParts.
func (o *Options) listParts(partitioned [][]Part) []Part {
	style := o.Style
	if style == "digital" {
		style = "short"
	}
	strs := make([]string, len(partitioned))
	for i, parts := range partitioned {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Value)
		}
		strs[i] = b.String()
	}
	var out []Part
	j := 0
	for _, lp := range list.NewFormatter(o.Locale, "unit", style).Format(strs) {
		if lp.Type == "element" {
			out = append(out, partitioned[j]...)
			j++
			continue
		}
		out = append(out, Part{
			Type:  "literal",
			Value: lp.Value,
		})
	}
	return out
}
