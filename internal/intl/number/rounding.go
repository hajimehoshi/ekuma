// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package number

import (
	"strings"
)

// RoundingType is the [[RoundingType]] of an Intl.NumberFormat or Intl.PluralRules.
type RoundingType int

const (
	FractionDigits RoundingType = iota
	SignificantDigits
	MorePrecision
	LessPrecision
)

// Digits holds the options that SetNumberFormatDigitOptions resolves, which decide how
// FormatNumericToString rounds a number.
type Digits struct {
	MinimumIntegerDigits     int
	MinimumFractionDigits    int
	MaximumFractionDigits    int
	MinimumSignificantDigits int
	MaximumSignificantDigits int
	RoundingType             RoundingType
	RoundingIncrement        int
	// RoundingMode is the identifier of a rounding mode, such as "halfExpand".
	RoundingMode string
	// StripIfInteger is whether [[TrailingZeroDisplay]] is "stripIfInteger".
	StripIfInteger bool
}

// FormatToString implements FormatNumericToString for the finite x: it returns x rounded and
// formatted as a decimal string with ASCII digits and a full stop, and the rounded value.
func (d *Digits) FormatToString(x Decimal) (string, Decimal) {
	neg := x.neg
	x = x.abs()
	m := unsignedRoundingModeOf(d.RoundingMode, neg)
	var result rawResult
	switch d.RoundingType {
	case SignificantDigits:
		result = toRawPrecision(x, d.MinimumSignificantDigits, d.MaximumSignificantDigits, m)
	case FractionDigits:
		result = toRawFixed(x, d.MinimumFractionDigits, d.MaximumFractionDigits, d.RoundingIncrement, m)
	default:
		sr := toRawPrecision(x, d.MinimumSignificantDigits, d.MaximumSignificantDigits, m)
		fr := toRawFixed(x, d.MinimumFractionDigits, d.MaximumFractionDigits, d.RoundingIncrement, m)
		fixedIsMorePrecise := fr.roundingMagnitude < sr.roundingMagnitude
		switch {
		case d.RoundingType == MorePrecision && fixedIsMorePrecise:
			result = fr
		case d.RoundingType == LessPrecision && !fixedIsMorePrecise:
			result = fr
		default:
			result = sr
		}
	}
	rounded := result.roundedNumber
	s := result.formattedString
	if d.StripIfInteger && rounded.exp >= 0 {
		s, _, _ = strings.Cut(s, ".")
	}
	if n := d.MinimumIntegerDigits - result.integerDigits; n > 0 {
		s = strings.Repeat("0", n) + s
	}
	rounded.neg = neg
	return s, rounded
}
