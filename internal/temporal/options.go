// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package temporal

import (
	"fmt"
	"unicode/utf8"
)

// ErrorKind is the kind of ECMAScript error that an [Error] stands for.
type ErrorKind int

const (
	RangeError ErrorKind = iota
	TypeError
)

// Error is an error that the specification throws as a RangeError or a TypeError.
type Error struct {
	Kind    ErrorKind
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

func rangeErrorf(format string, args ...any) error {
	return &Error{
		Kind:    RangeError,
		Message: fmt.Sprintf(format, clipArgs(args)...),
	}
}

func typeErrorf(format string, args ...any) error {
	return &Error{
		Kind:    TypeError,
		Message: fmt.Sprintf(format, clipArgs(args)...),
	}
}

// maxMessageArg is the length in bytes up to which a message shows a string, such as the input
// that failed to parse.
const maxMessageArg = 1 << 10

// clipArgs shortens the long strings of args in place and returns args.
func clipArgs(args []any) []any {
	for i, a := range args {
		s, ok := a.(string)
		if !ok || len(s) <= maxMessageArg {
			continue
		}
		n := maxMessageArg
		for n > 0 && !utf8.RuneStart(s[n]) {
			n--
		}
		args[i] = s[:n] + "..."
	}
	return args
}

// Overflow is the value of the overflow option.
type Overflow int

const (
	OverflowConstrain Overflow = iota
	OverflowReject
)

// Disambiguation is the value of the disambiguation option.
type Disambiguation int

const (
	DisambiguationCompatible Disambiguation = iota
	DisambiguationEarlier
	DisambiguationLater
	DisambiguationReject
)

// OffsetOption is the value of the offset option of ZonedDateTime.
type OffsetOption int

const (
	OffsetPrefer OffsetOption = iota
	OffsetUse
	OffsetIgnore
	OffsetReject
)

// OffsetBehaviour says how a UTC offset given with a date-time is used.
type OffsetBehaviour int

const (
	OffsetBehaviourOption OffsetBehaviour = iota
	OffsetBehaviourExact
	OffsetBehaviourWall
)

// ShowCalendar is the value of the calendarName option.
type ShowCalendar int

const (
	ShowCalendarAuto ShowCalendar = iota
	ShowCalendarAlways
	ShowCalendarNever
	ShowCalendarCritical
)

// ShowTimeZone is the value of the timeZoneName option.
type ShowTimeZone int

const (
	ShowTimeZoneAuto ShowTimeZone = iota
	ShowTimeZoneNever
	ShowTimeZoneCritical
)

// RoundingMode is a rounding mode.
type RoundingMode int

const (
	RoundCeil RoundingMode = iota
	RoundFloor
	RoundExpand
	RoundTrunc
	RoundHalfCeil
	RoundHalfFloor
	RoundHalfExpand
	RoundHalfTrunc
	RoundHalfEven
)

// RoundingModeNames are the option values of the rounding modes, by mode.
var RoundingModeNames = []string{"ceil", "floor", "expand", "trunc", "halfCeil", "halfFloor", "halfExpand", "halfTrunc", "halfEven"}

// Negate implements NegateRoundingMode.
func (m RoundingMode) Negate() RoundingMode {
	switch m {
	case RoundCeil:
		return RoundFloor
	case RoundFloor:
		return RoundCeil
	case RoundHalfCeil:
		return RoundHalfFloor
	case RoundHalfFloor:
		return RoundHalfCeil
	}
	return m
}

// unsignedRoundingMode is an unsigned rounding mode.
type unsignedRoundingMode int

const (
	unsignedInfinity unsignedRoundingMode = iota
	unsignedZero
	unsignedHalfInfinity
	unsignedHalfZero
	unsignedHalfEven
)

// unsigned implements GetUnsignedRoundingMode.
func (m RoundingMode) unsigned(negative bool) unsignedRoundingMode {
	switch m {
	case RoundCeil:
		if negative {
			return unsignedZero
		}
		return unsignedInfinity
	case RoundFloor:
		if negative {
			return unsignedInfinity
		}
		return unsignedZero
	case RoundExpand:
		return unsignedInfinity
	case RoundTrunc:
		return unsignedZero
	case RoundHalfCeil:
		if negative {
			return unsignedHalfZero
		}
		return unsignedHalfInfinity
	case RoundHalfFloor:
		if negative {
			return unsignedHalfInfinity
		}
		return unsignedHalfZero
	case RoundHalfExpand:
		return unsignedHalfInfinity
	case RoundHalfTrunc:
		return unsignedHalfZero
	}
	return unsignedHalfEven
}

// apply implements ApplyUnsignedRoundingMode: it chooses r1 or r2, the candidates below and
// above the value, by cmp, the comparison of the value's distance from r1 with half the
// distance to r2, and by whether r1 is even.
func (m unsignedRoundingMode) apply(cmp int, even bool) bool {
	switch m {
	case unsignedZero:
		return false
	case unsignedInfinity:
		return true
	}
	if cmp < 0 {
		return false
	}
	if cmp > 0 {
		return true
	}
	switch m {
	case unsignedHalfZero:
		return false
	case unsignedHalfInfinity:
		return true
	}
	return !even
}

// Unit is a Temporal unit.
type Unit int

const (
	UnitYear Unit = iota
	UnitMonth
	UnitWeek
	UnitDay
	UnitHour
	UnitMinute
	UnitSecond
	UnitMillisecond
	UnitMicrosecond
	UnitNanosecond
	UnitAuto
	UnitUnset
)

var unitNames = [...]string{"year", "month", "week", "day", "hour", "minute", "second", "millisecond", "microsecond", "nanosecond", "auto"}

// UnitFromString returns the unit that the singular or plural name s stands for, accepting
// "auto" too.
func UnitFromString(s string) (Unit, bool) {
	for u, n := range unitNames {
		if s == n || (Unit(u) != UnitAuto && s == n+"s") {
			return Unit(u), true
		}
	}
	return 0, false
}

func (u Unit) String() string {
	if int(u) < len(unitNames) {
		return unitNames[u]
	}
	return "unset"
}

// IsCalendarUnit implements IsCalendarUnit.
func (u Unit) IsCalendarUnit() bool {
	return u <= UnitWeek
}

// IsDateUnit reports whether the temporal unit category of u is date.
func (u Unit) IsDateUnit() bool {
	return u <= UnitDay
}

// IsTimeUnit reports whether the temporal unit category of u is time.
func (u Unit) IsTimeUnit() bool {
	return u >= UnitHour && u <= UnitNanosecond
}

// Nanoseconds returns the length of the time unit or day u in nanoseconds.
func (u Unit) Nanoseconds() int64 {
	switch u {
	case UnitDay:
		return NsPerDay
	case UnitHour:
		return NsPerHour
	case UnitMinute:
		return NsPerMinute
	case UnitSecond:
		return NsPerSecond
	case UnitMillisecond:
		return NsPerMillisecond
	case UnitMicrosecond:
		return NsPerMicrosecond
	}
	return 1
}

// LargerOfTwoTemporalUnits implements LargerOfTwoTemporalUnits.
func LargerOfTwoTemporalUnits(x, y Unit) Unit {
	return min(x, y)
}

// MaximumTemporalDurationRoundingIncrement implements MaximumTemporalDurationRoundingIncrement.
// It reports false for the unset result.
func MaximumTemporalDurationRoundingIncrement(u Unit) (int64, bool) {
	switch u {
	case UnitHour:
		return 24, true
	case UnitMinute, UnitSecond:
		return 60, true
	case UnitMillisecond, UnitMicrosecond, UnitNanosecond:
		return 1000, true
	}
	return 0, false
}

// ValidateTemporalRoundingIncrement implements ValidateTemporalRoundingIncrement.
func ValidateTemporalRoundingIncrement(increment, dividend int64, inclusive bool) error {
	maximum := dividend
	if !inclusive {
		maximum--
	}
	if increment > maximum {
		return rangeErrorf("roundingIncrement %d is out of range", increment)
	}
	if dividend%increment != 0 {
		return rangeErrorf("roundingIncrement %d does not divide %d", increment, dividend)
	}
	return nil
}

// Precision is the precision of fractional seconds: a number of digits, or PrecisionAuto or
// PrecisionMinute.
type Precision int

const (
	PrecisionAuto   Precision = -1
	PrecisionMinute Precision = -2
)

// SecondsStringPrecision is a record that ToSecondsStringPrecisionRecord returns.
type SecondsStringPrecision struct {
	Precision Precision
	Unit      Unit
	Increment int64
}

// ToSecondsStringPrecisionRecord implements ToSecondsStringPrecisionRecord.
func ToSecondsStringPrecisionRecord(smallestUnit Unit, digits Precision) SecondsStringPrecision {
	switch smallestUnit {
	case UnitMinute:
		return SecondsStringPrecision{
			Precision: PrecisionMinute,
			Unit:      UnitMinute,
			Increment: 1,
		}
	case UnitSecond:
		return SecondsStringPrecision{
			Precision: 0,
			Unit:      UnitSecond,
			Increment: 1,
		}
	case UnitMillisecond:
		return SecondsStringPrecision{
			Precision: 3,
			Unit:      UnitMillisecond,
			Increment: 1,
		}
	case UnitMicrosecond:
		return SecondsStringPrecision{
			Precision: 6,
			Unit:      UnitMicrosecond,
			Increment: 1,
		}
	case UnitNanosecond:
		return SecondsStringPrecision{
			Precision: 9,
			Unit:      UnitNanosecond,
			Increment: 1,
		}
	}
	switch {
	case digits == PrecisionAuto:
		return SecondsStringPrecision{
			Precision: PrecisionAuto,
			Unit:      UnitNanosecond,
			Increment: 1,
		}
	case digits == 0:
		return SecondsStringPrecision{
			Precision: 0,
			Unit:      UnitSecond,
			Increment: 1,
		}
	case digits <= 3:
		return SecondsStringPrecision{
			Precision: digits,
			Unit:      UnitMillisecond,
			Increment: pow10(3 - int(digits)),
		}
	case digits <= 6:
		return SecondsStringPrecision{
			Precision: digits,
			Unit:      UnitMicrosecond,
			Increment: pow10(6 - int(digits)),
		}
	}
	return SecondsStringPrecision{
		Precision: digits,
		Unit:      UnitNanosecond,
		Increment: pow10(9 - int(digits)),
	}
}

func pow10(n int) int64 {
	p := int64(1)
	for range n {
		p *= 10
	}
	return p
}
