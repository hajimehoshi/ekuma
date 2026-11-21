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
func newInstant(vm *engine.VM, ns temporal.Int128, nt *engine.Object) engine.Value {
	proto := vm.GetPrototypeFromConstructor(nt, engine.IntrinsicTemporalInstantPrototype)
	return engine.ObjectValue(vm.NewTemporalObject(proto, &instantSlots{
		ns: ns,
	}))
}

func thisInstant(vm *engine.VM, this engine.Value, name string) *instantSlots {
	s, ok := engine.TemporalData(this).(*instantSlots)
	if !ok {
		incompatible(vm, "Temporal.Instant.prototype."+name, this)
	}
	return s
}

// toTemporalInstant implements ToTemporalInstant, returning the epoch nanoseconds.
func toTemporalInstant(vm *engine.VM, item engine.Value) temporal.Int128 {
	if item.IsObject() {
		switch s := engine.TemporalData(item).(type) {
		case *instantSlots:
			return s.ns
		case *zonedDateTimeSlots:
			return s.ns
		}
		item = vm.ToPrimitive(item, engine.HintString)
	}
	str := requireString(vm, item, "instant")
	p := must(temporal.ParseISODateTime(str, vm.Poll, temporal.GoalInstant))
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
		vm.ThrowRangeError("instant is out of range")
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

func initTemporalInstant(vm *engine.VM, ns *engine.Object) {
	ctor, proto := temporalConstructor(vm, ns, "Instant", 1, engine.IntrinsicTemporalInstantPrototype, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(vm, "Temporal.Instant")
		return newInstant(vm, bigIntToEpochNs(vm, arg(args, 0)), nt)
	})
	tmethod(vm, ctor, "from", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return newInstant(vm, toTemporalInstant(vm, arg(args, 0)), nil)
	})
	tmethod(vm, ctor, "fromEpochMilliseconds", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		n := vm.ToNumber(arg(args, 0))
		if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
			vm.ThrowRangeError("epoch milliseconds must be an integer")
		}
		if math.Abs(n) > 8.64e15 {
			vm.ThrowRangeError("epoch milliseconds are out of range")
		}
		return newInstant(vm, temporal.Mul64(int64(n), 1_000_000), nil)
	})
	tmethod(vm, ctor, "fromEpochNanoseconds", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return newInstant(vm, bigIntToEpochNs(vm, arg(args, 0)), nil)
	})
	tmethod(vm, ctor, "compare", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		one := toTemporalInstant(vm, arg(args, 0))
		two := toTemporalInstant(vm, arg(args, 1))
		return compareResult(one.Cmp(two))
	})

	tgetter(vm, proto, vm.Intern("epochMilliseconds"), "epochMilliseconds", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return epochMilliseconds(thisInstant(vm, this, "epochMilliseconds").ns)
	})
	tgetter(vm, proto, vm.Intern("epochNanoseconds"), "epochNanoseconds", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return epochNsValue(vm, thisInstant(vm, this, "epochNanoseconds").ns)
	})
	for _, name := range []string{"add", "subtract"} {
		tmethod(vm, proto, wtf8.String(name), 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisInstant(vm, this, name)
			d := toTemporalDuration(vm, arg(args, 0))
			if name == "subtract" {
				d = d.Negated()
			}
			if temporal.DefaultTemporalLargestUnit(&d).IsDateUnit() {
				vm.ThrowRangeError("years, months, weeks and days cannot be added to an instant")
			}
			id := temporal.ToInternalDurationRecordWith24HourDays(&d)
			return newInstant(vm, must(temporal.AddInstant(s.ns, id.Time)), nil)
		})
	}
	for _, name := range []string{"until", "since"} {
		tmethod(vm, proto, wtf8.String(name), 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisInstant(vm, this, name)
			other := toTemporalInstant(vm, arg(args, 0))
			opts := getOptionsObject(vm, arg(args, 1))
			settings := getDifferenceSettings(vm, name == "since", opts, unitGroupTime, nil, temporal.UnitNanosecond, temporal.UnitSecond)
			id := must(temporal.DifferenceEpochNanoseconds(s.ns, other, settings.increment, settings.smallestUnit, settings.mode))
			d := must(temporal.TemporalDurationFromInternal(id, settings.largestUnit))
			if name == "since" {
				d = d.Negated()
			}
			return newDuration(vm, d, nil)
		})
	}
	tmethod(vm, proto, "round", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisInstant(vm, this, "round")
		opts := roundToOptions(vm, arg(args, 0), "smallestUnit")
		increment := getRoundingIncrementOption(vm, opts)
		mode := getRoundingModeOption(vm, opts, temporal.RoundHalfExpand)
		unit := getUnitOption(vm, opts, "smallestUnit", true)
		validateUnit(vm, unit, unitGroupTime)
		check(temporal.ValidateTemporalRoundingIncrement(increment, maximumInstantIncrements[unit], true))
		return newInstant(vm, temporal.RoundTemporalInstant(s.ns, increment, unit, mode), nil)
	})
	tmethod(vm, proto, "equals", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisInstant(vm, this, "equals")
		return engine.Bool(s.ns.Cmp(toTemporalInstant(vm, arg(args, 0))) == 0)
	})
	tmethod(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisInstant(vm, this, "toString")
		opts := getOptionsObject(vm, arg(args, 0))
		digits := getFractionalSecondDigitsOption(vm, opts)
		mode := getRoundingModeOption(vm, opts, temporal.RoundTrunc)
		unit := getUnitOption(vm, opts, "smallestUnit", false)
		tzv := get(vm, opts, "timeZone")
		validateUnit(vm, unit, unitGroupTime)
		if unit == temporal.UnitHour {
			vm.ThrowRangeError("smallestUnit must not be hour")
		}
		var tz *temporal.TimeZone
		if !tzv.IsUndefined() {
			z := toTimeZone(vm, tzv)
			tz = &z
		}
		p := temporal.ToSecondsStringPrecisionRecord(unit, digits)
		rounded := temporal.RoundTemporalInstant(s.ns, p.Increment, p.Unit, mode)
		return vm.NewString(wtf8.String(temporal.TemporalInstantToString(rounded, tz, p.Precision)))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(vm, proto, wtf8.String(name), 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisInstant(vm, this, name)
			return vm.NewString(wtf8.String(temporal.TemporalInstantToString(s.ns, nil, temporal.PrecisionAuto)))
		})
	}
	valueOfThrows(vm, proto, "Instant")
	tmethod(vm, proto, "toZonedDateTimeISO", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisInstant(vm, this, "toZonedDateTimeISO")
		tz := toTimeZone(vm, arg(args, 0))
		return newZonedDateTime(vm, s.ns, tz, temporal.ISO8601, nil)
	})
}
