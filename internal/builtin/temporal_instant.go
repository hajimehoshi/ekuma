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
	ctor, proto := temporalConstructor(cx, ns, "Instant", 1, engine.IntrinsicTemporalInstantPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(cx, "Temporal.Instant")
		return newInstant(cx, bigIntToEpochNs(cx, arg(args, 0)), nt)
	})
	tmethod(cx, ctor, "from", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return newInstant(cx, toTemporalInstant(cx, arg(args, 0)), nil)
	})
	tmethod(cx, ctor, "fromEpochMilliseconds", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		n := cx.ToNumber(arg(args, 0))
		if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
			cx.ThrowRangeError("epoch milliseconds must be an integer")
		}
		if math.Abs(n) > 8.64e15 {
			cx.ThrowRangeError("epoch milliseconds are out of range")
		}
		return newInstant(cx, temporal.Mul64(int64(n), 1_000_000), nil)
	})
	tmethod(cx, ctor, "fromEpochNanoseconds", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return newInstant(cx, bigIntToEpochNs(cx, arg(args, 0)), nil)
	})
	tmethod(cx, ctor, "compare", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		one := toTemporalInstant(cx, arg(args, 0))
		two := toTemporalInstant(cx, arg(args, 1))
		return compareResult(one.Cmp(two))
	})

	tgetter(cx, proto, cx.Intern("epochMilliseconds"), "epochMilliseconds", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return epochMilliseconds(thisInstant(cx, this, "epochMilliseconds").ns)
	})
	tgetter(cx, proto, cx.Intern("epochNanoseconds"), "epochNanoseconds", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return epochNsValue(cx, thisInstant(cx, this, "epochNanoseconds").ns)
	})
	for _, name := range []string{"add", "subtract"} {
		tmethod(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
	for _, name := range []string{"until", "since"} {
		tmethod(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
	tmethod(cx, proto, "round", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisInstant(cx, this, "round")
		opts := roundToOptions(cx, arg(args, 0), "smallestUnit")
		increment := getRoundingIncrementOption(cx, opts)
		mode := getRoundingModeOption(cx, opts, temporal.RoundHalfExpand)
		unit := getUnitOption(cx, opts, "smallestUnit", true)
		validateUnit(cx, unit, unitGroupTime)
		check(temporal.ValidateTemporalRoundingIncrement(increment, maximumInstantIncrements[unit], true))
		return newInstant(cx, temporal.RoundTemporalInstant(s.ns, increment, unit, mode), nil)
	})
	tmethod(cx, proto, "equals", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisInstant(cx, this, "equals")
		return engine.Bool(s.ns.Cmp(toTemporalInstant(cx, arg(args, 0))) == 0)
	})
	tmethod(cx, proto, "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisInstant(cx, this, name)
			return cx.NewString(wtf8.String(temporal.TemporalInstantToString(s.ns, nil, temporal.PrecisionAuto)))
		})
	}
	valueOfThrows(cx, proto, "Instant")
	tmethod(cx, proto, "toZonedDateTimeISO", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisInstant(cx, this, "toZonedDateTimeISO")
		tz := toTimeZone(cx, arg(args, 0))
		return newZonedDateTime(cx, s.ns, tz, temporal.ISO8601, nil)
	})
}
