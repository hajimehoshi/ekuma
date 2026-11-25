// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/temporal"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// newInstant implements CreateTemporalInstant for valid epoch nanoseconds.
func newInstant(cx *engine.ExecutionContext, ns temporal.Int128, nt *engine.Object) engine.Value {
	proto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicTemporalInstantPrototype)
	return engine.ObjectValue(cx.NewTemporalObject(proto, &instantSlots{
		ns: ns,
	}))
}

func thisInstant(cx *engine.ExecutionContext, this engine.Value, name string) *instantSlots {
	s, ok := engine.TemporalData(this).(*instantSlots)
	if !ok {
		incompatible(cx, "Temporal.Instant.prototype."+name, this)
	}
	return s
}

// toTemporalInstant implements ToTemporalInstant, returning the epoch nanoseconds.
func toTemporalInstant(cx *engine.ExecutionContext, item engine.Value) temporal.Int128 {
	if item.IsObject() {
		switch s := engine.TemporalData(item).(type) {
		case *instantSlots:
			return s.ns
		case *zonedDateTimeSlots:
			return s.ns
		}
		item = cx.ToPrimitive(item, engine.HintString)
	}
	str := requireString(cx, item, "instant")
	p := must(temporal.ParseISODateTime(str, cx.Poll, temporal.GoalInstant))
	var offsetNs int64
	if !p.Z {
		offsetNs = must(temporal.ParseDateTimeUTCOffset(p.Offset))
	}
	t := p.Time
	balanced := temporal.BalanceISODateTime(temporal.ISODate{Year: p.Year, Month: p.Month, Day: p.Day},
		int64(t.Hour), int64(t.Minute), int64(t.Second), int64(t.Millisecond), int64(t.Microsecond), int64(t.Nanosecond)-offsetNs)
	check(temporal.CheckISODaysRange(balanced.Date))
	ns := balanced.UTCEpochNanoseconds()
	if !temporal.IsValidEpochNanoseconds(ns) {
		cx.ThrowRangeError("instant is out of range")
	}
	return ns
}

// maximumInstantIncrements are the maximum rounding increments of Instant.prototype.round.
var maximumInstantIncrements = map[temporal.Unit]int64{
	temporal.UnitHour:        24,
	temporal.UnitMinute:      1440,
	temporal.UnitSecond:      86400,
	temporal.UnitMillisecond: 86400e3,
	temporal.UnitMicrosecond: 86400e6,
	temporal.UnitNanosecond:  86400e9,
}

func initTemporalInstant(cx *engine.ExecutionContext, ns *engine.Object) {
	ctor, proto := temporalConstructor(cx, ns, "Instant", 1, engine.IntrinsicTemporalInstantPrototype, nativeInstantConstructor)
	method(cx, ctor, "from", 1, nativeInstantFrom)
	method(cx, ctor, "fromEpochMilliseconds", 1, nativeInstantFromEpochMilliseconds)
	method(cx, ctor, "fromEpochNanoseconds", 1, nativeInstantFromEpochNanoseconds)
	method(cx, ctor, "compare", 2, nativeInstantCompare)

	getter(cx, proto, cx.Intern("epochMilliseconds"), "epochMilliseconds", nativeInstantEpochMilliseconds)
	getter(cx, proto, cx.Intern("epochNanoseconds"), "epochNanoseconds", nativeInstantEpochNanoseconds)
	method(cx, proto, "add", 1, nativeInstantAdd)
	method(cx, proto, "subtract", 1, nativeInstantSubtract)
	method(cx, proto, "until", 1, nativeInstantUntil)
	method(cx, proto, "since", 1, nativeInstantSince)
	method(cx, proto, "round", 1, nativeInstantRound)
	method(cx, proto, "equals", 1, nativeInstantEquals)
	method(cx, proto, "toString", 0, nativeInstantToString)
	method(cx, proto, "toJSON", 0, nativeInstantToJSON)
	method(cx, proto, "toLocaleString", 0, nativeInstantToLocaleString)
	method(cx, proto, "valueOf", 0, nativeInstantValueOf)
	method(cx, proto, "toZonedDateTimeISO", 1, nativeInstantToZonedDateTimeISO)
}

// The native functions of Temporal.Instant.
var (
	nativeInstantConstructor           = engine.RegisterNative(temporalNative(instantConstructor))
	nativeInstantFrom                  = engine.RegisterNative(temporalNative(instantFrom))
	nativeInstantFromEpochMilliseconds = engine.RegisterNative(temporalNative(instantFromEpochMilliseconds))
	nativeInstantFromEpochNanoseconds  = engine.RegisterNative(temporalNative(instantFromEpochNanoseconds))
	nativeInstantCompare               = engine.RegisterNative(temporalNative(instantCompare))
	nativeInstantEpochMilliseconds     = engine.RegisterNative(temporalNative(instantEpochMilliseconds))
	nativeInstantEpochNanoseconds      = engine.RegisterNative(temporalNative(instantEpochNanoseconds))
	nativeInstantAdd                   = engine.RegisterNative(instantAddNative("add"))
	nativeInstantSubtract              = engine.RegisterNative(instantAddNative("subtract"))
	nativeInstantUntil                 = engine.RegisterNative(instantUntilNative("until"))
	nativeInstantSince                 = engine.RegisterNative(instantUntilNative("since"))
	nativeInstantRound                 = engine.RegisterNative(temporalNative(instantRound))
	nativeInstantEquals                = engine.RegisterNative(temporalNative(instantEquals))
	nativeInstantToString              = engine.RegisterNative(temporalNative(instantToString))
	nativeInstantToJSON                = engine.RegisterNative(instantToJSONNative("toJSON"))
	nativeInstantToLocaleString        = engine.RegisterNative(instantToJSONNative("toLocaleString"))
	nativeInstantValueOf               = engine.RegisterNative(temporalValueOfNative("Instant"))
	nativeInstantToZonedDateTimeISO    = engine.RegisterNative(temporalNative(instantToZonedDateTimeISO))
)

// instantAddNative returns the method name of Temporal.Instant.prototype, which is add or subtract.
//
//go:noinline
func instantAddNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisInstant(cx, this, name)
		d := toTemporalDuration(cx, arg(args, 0))
		if name == "subtract" {
			d = d.Negated()
		}
		if temporal.DefaultTemporalLargestUnit(&d).IsDateUnit() {
			cx.ThrowRangeError("years, months, weeks and days cannot be added to an instant")
		}
		id := temporal.ToInternalDurationRecordWith24HourDays(&d)
		return newInstant(cx, must(temporal.AddInstant(s.ns, id.Time)), nil)
	})
}

// instantUntilNative returns the method name of Temporal.Instant.prototype, which is until or
// since.
//
//go:noinline
func instantUntilNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisInstant(cx, this, name)
		other := toTemporalInstant(cx, arg(args, 0))
		opts := getOptionsObject(cx, arg(args, 1))
		settings := getDifferenceSettings(cx, name == "since", opts, unitGroupTime, nil, temporal.UnitNanosecond, temporal.UnitSecond)
		id := must(temporal.DifferenceEpochNanoseconds(s.ns, other, settings.increment, settings.smallestUnit, settings.mode))
		d := must(temporal.TemporalDurationFromInternal(id, settings.largestUnit))
		if name == "since" {
			d = d.Negated()
		}
		return newDuration(cx, d, nil)
	})
}

// instantToJSONNative returns the method name of Temporal.Instant.prototype, which is toJSON or
// toLocaleString.
//
//go:noinline
func instantToJSONNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisInstant(cx, this, name)
		return cx.NewString(wtf8.String(temporal.TemporalInstantToString(s.ns, nil, temporal.PrecisionAuto)))
	})
}

func instantConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := newTarget(cx, "Temporal.Instant")
	return newInstant(cx, bigIntToEpochNs(cx, arg(args, 0)), nt)
}

func instantFrom(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return newInstant(cx, toTemporalInstant(cx, arg(args, 0)), nil)
}

func instantFromEpochMilliseconds(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	n := cx.ToNumber(arg(args, 0))
	if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
		cx.ThrowRangeError("epoch milliseconds must be an integer")
	}
	if math.Abs(n) > 8.64e15 {
		cx.ThrowRangeError("epoch milliseconds are out of range")
	}
	return newInstant(cx, temporal.Mul64(int64(n), 1_000_000), nil)
}

func instantFromEpochNanoseconds(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return newInstant(cx, bigIntToEpochNs(cx, arg(args, 0)), nil)
}

func instantCompare(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	one := toTemporalInstant(cx, arg(args, 0))
	two := toTemporalInstant(cx, arg(args, 1))
	return compareResult(one.Cmp(two))
}

func instantEpochMilliseconds(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return epochMilliseconds(thisInstant(cx, this, "epochMilliseconds").ns)
}

func instantEpochNanoseconds(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return epochNsValue(cx, thisInstant(cx, this, "epochNanoseconds").ns)
}

func instantRound(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	s := thisInstant(cx, this, "round")
	opts := roundToOptions(cx, arg(args, 0), "smallestUnit")
	increment := getRoundingIncrementOption(cx, opts)
	mode := getRoundingModeOption(cx, opts, temporal.RoundHalfExpand)
	unit := getUnitOption(cx, opts, "smallestUnit", true)
	validateUnit(cx, unit, unitGroupTime)
	check(temporal.ValidateTemporalRoundingIncrement(increment, maximumInstantIncrements[unit], true))
	return newInstant(cx, temporal.RoundTemporalInstant(s.ns, increment, unit, mode), nil)
}

func instantEquals(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	s := thisInstant(cx, this, "equals")
	return engine.Bool(s.ns.Cmp(toTemporalInstant(cx, arg(args, 0))) == 0)
}

func instantToString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	s := thisInstant(cx, this, "toString")
	opts := getOptionsObject(cx, arg(args, 0))
	digits := getFractionalSecondDigitsOption(cx, opts)
	mode := getRoundingModeOption(cx, opts, temporal.RoundTrunc)
	unit := getUnitOption(cx, opts, "smallestUnit", false)
	tzv := get(cx, opts, "timeZone")
	validateUnit(cx, unit, unitGroupTime)
	if unit == temporal.UnitHour {
		cx.ThrowRangeError("smallestUnit must not be hour")
	}
	var tz *temporal.TimeZone
	if !tzv.IsUndefined() {
		z := toTimeZone(cx, tzv)
		tz = &z
	}
	p := temporal.ToSecondsStringPrecisionRecord(unit, digits)
	rounded := temporal.RoundTemporalInstant(s.ns, p.Increment, p.Unit, mode)
	return cx.NewString(wtf8.String(temporal.TemporalInstantToString(rounded, tz, p.Precision)))
}

func instantToZonedDateTimeISO(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	s := thisInstant(cx, this, "toZonedDateTimeISO")
	tz := toTimeZone(cx, arg(args, 0))
	return newZonedDateTime(cx, s.ns, tz, temporal.ISO8601, nil)
}
