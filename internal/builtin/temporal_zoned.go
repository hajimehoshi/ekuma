// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"time"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/temporal"
)

// newZonedDateTime implements CreateTemporalZonedDateTime for valid epoch nanoseconds.
func (r *temporalRealm) newZonedDateTime(vm *engine.VM, ns temporal.Int128, tz temporal.TimeZone, cal string, nt *engine.Object) engine.Value {
	return engine.ObjectValue(vm.NewTemporalObject(protoFor(vm, nt, r.zonedDateTime), &zonedDateTimeSlots{
		ns:  ns,
		tz:  tz,
		cal: cal,
	}))
}

func thisZonedDateTime(vm *engine.VM, this engine.Value, name string) *zonedDateTimeSlots {
	s, ok := engine.TemporalData(this).(*zonedDateTimeSlots)
	if !ok {
		incompatible(vm, "Temporal.ZonedDateTime.prototype."+name, this)
	}
	return s
}

// toTemporalZonedDateTime implements ToTemporalZonedDateTime.
func toTemporalZonedDateTime(vm *engine.VM, item, options engine.Value) zonedDateTimeSlots {
	var date temporal.ISODate
	var t temporal.Time
	startOfDay := false
	var tz temporal.TimeZone
	var offset, cal string
	hasOffset := false
	z := false
	match := temporal.MatchExactly
	var disambiguation temporal.Disambiguation
	var offsetOption temporal.OffsetOption
	if item.IsObject() {
		if s, ok := engine.TemporalData(item).(*zonedDateTimeSlots); ok {
			opts := getOptionsObject(vm, options)
			getDisambiguationOption(vm, opts)
			getOffsetOption(vm, opts, temporal.OffsetReject)
			getOverflowOption(vm, opts)
			return *s
		}
		o := item.Object()
		cal = calendarWithISODefault(vm, o)
		names := append(append(append([]string{}, dateFieldNames...), timeFieldNames...), "offset", "timeZone")
		f := prepareCalendarFields(vm, o, names, fieldsTimeZoneRequired)
		tz = f.TimeZone
		offset, hasOffset = f.Offset, f.HasOffset
		opts := getOptionsObject(vm, options)
		disambiguation = getDisambiguationOption(vm, opts)
		offsetOption = getOffsetOption(vm, opts, temporal.OffsetReject)
		overflow := getOverflowOption(vm, opts)
		dt := interpretDateTimeFields(vm, f, overflow)
		date, t = dt.Date, dt.Time
	} else {
		str := requireString(vm, item, "zoned date-time")
		p := must(temporal.ParseISODateTime(str, temporal.GoalZonedDateTime))
		tz = toTimeZone(vm, vm.NewString(p.TimeZoneAnnotation))
		offset, hasOffset = p.Offset, p.Offset != ""
		z = p.Z
		cal = canonicalizeParsedCalendar(vm, p.Calendar)
		match = temporal.MatchMinutes
		if hasOffset && !temporal.IsOffsetWithoutSubMinutes(offset) {
			match = temporal.MatchExactly
		}
		opts := getOptionsObject(vm, options)
		disambiguation = getDisambiguationOption(vm, opts)
		offsetOption = getOffsetOption(vm, opts, temporal.OffsetReject)
		getOverflowOption(vm, opts)
		date = temporal.ISODate{Year: p.Year, Month: p.Month, Day: p.Day}
		t = p.Time
		startOfDay = p.StartOfDay
	}
	behaviour := temporal.OffsetBehaviourOption
	switch {
	case z:
		behaviour = temporal.OffsetBehaviourExact
	case !hasOffset:
		behaviour = temporal.OffsetBehaviourWall
	}
	var offsetNs int64
	if behaviour == temporal.OffsetBehaviourOption {
		offsetNs = must(temporal.ParseDateTimeUTCOffset(offset))
	}
	ns := must(temporal.InterpretISODateTimeOffset(date, t, startOfDay, behaviour, offsetNs, tz, disambiguation, offsetOption, match))
	return zonedDateTimeSlots{
		ns:  ns,
		tz:  tz,
		cal: cal,
	}
}

func initTemporalZonedDateTime(vm *engine.VM, ns *engine.Object, r *temporalRealm) {
	proto := r.zonedDateTime
	ctor := temporalConstructor(vm, ns, "ZonedDateTime", 2, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(vm, "Temporal.ZonedDateTime")
		epochNs := bigIntToEpochNs(vm, arg(args, 0))
		id := requireString(vm, arg(args, 1), "time zone")
		name, off, isOff, err := temporal.ParseTimeZoneIdentifier(id)
		check(err)
		var system temporal.TimeZone
		if !isOff && name != "UTC" {
			system = systemTimeZone(vm)
		}
		tz := must(temporal.TimeZoneFromIdentifier(name, off, isOff, system))
		cal := temporal.ISO8601
		if c := arg(args, 2); !c.IsUndefined() {
			cal = must(temporal.CanonicalizeCalendar(requireString(vm, c, "calendar")))
		}
		return r.newZonedDateTime(vm, epochNs, tz, cal, nt)
	})
	tmethod(vm, ctor, "from", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := toTemporalZonedDateTime(vm, arg(args, 0), arg(args, 1))
		return r.newZonedDateTime(vm, s.ns, s.tz, s.cal, nil)
	})
	tmethod(vm, ctor, "compare", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		one := toTemporalZonedDateTime(vm, arg(args, 0), engine.Undefined)
		two := toTemporalZonedDateTime(vm, arg(args, 1), engine.Undefined)
		return compareResult(one.ns.Cmp(two.ns))
	})

	// dateTimeOf returns the local date-time of a ZonedDateTime receiver.
	dateTimeOf := func(vm *engine.VM, this engine.Value, name string) (*zonedDateTimeSlots, temporal.ISODateTime) {
		s := thisZonedDateTime(vm, this, name)
		return s, s.tz.ISODateTimeFor(s.ns)
	}
	defineCalendarGetters(vm, proto, fullDateGetters, func(vm *engine.VM, this engine.Value, name string) (temporal.ISODate, string) {
		s, dt := dateTimeOf(vm, this, name)
		return dt.Date, s.cal
	})
	for _, name := range timeGetterNames {
		tgetter(vm, proto, vm.Intern(name), name, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			_, dt := dateTimeOf(vm, this, name)
			return engine.Number(float64(timeField(dt.Time, name)))
		})
	}
	tgetter(vm, proto, vm.Intern("timeZoneId"), "timeZoneId", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.NewString(thisZonedDateTime(vm, this, "timeZoneId").tz.ID())
	})
	tgetter(vm, proto, vm.Intern("epochMilliseconds"), "epochMilliseconds", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return epochMilliseconds(thisZonedDateTime(vm, this, "epochMilliseconds").ns)
	})
	tgetter(vm, proto, vm.Intern("epochNanoseconds"), "epochNanoseconds", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return epochNsValue(vm, thisZonedDateTime(vm, this, "epochNanoseconds").ns)
	})
	tgetter(vm, proto, vm.Intern("hoursInDay"), "hoursInDay", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(vm, this, "hoursInDay")
		today := dt.Date
		tomorrow := temporal.AddDaysToISODate(today, 1)
		start := must(s.tz.StartOfDay(today))
		end := must(s.tz.StartOfDay(tomorrow))
		return engine.Number(temporal.TotalTimeDuration(end.Sub(start), temporal.UnitHour))
	})
	tgetter(vm, proto, vm.Intern("offsetNanoseconds"), "offsetNanoseconds", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(vm, this, "offsetNanoseconds")
		return engine.Number(float64(s.tz.OffsetNanosecondsFor(s.ns)))
	})
	tgetter(vm, proto, vm.Intern("offset"), "offset", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(vm, this, "offset")
		return vm.NewString(temporal.FormatUTCOffsetNanoseconds(s.tz.OffsetNanosecondsFor(s.ns)))
	})
	tmethod(vm, proto, "with", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(vm, this, "with")
		bag := rejectTemporalLikeObject(vm, arg(args, 0))
		offsetNs := s.tz.OffsetNanosecondsFor(s.ns)
		f := temporal.ISODateToFields(dt.Date, temporal.FieldsDate)
		f.SetTime(dt.Time)
		f.Offset, f.HasOffset = temporal.FormatUTCOffsetNanoseconds(offsetNs), true
		names := append(append(append([]string{}, dateFieldNames...), timeFieldNames...), "offset")
		partial := prepareCalendarFields(vm, bag, names, fieldsPartial)
		f = temporal.CalendarMergeFields(f, partial)
		opts := getOptionsObject(vm, arg(args, 1))
		disambiguation := getDisambiguationOption(vm, opts)
		offsetOption := getOffsetOption(vm, opts, temporal.OffsetPrefer)
		overflow := getOverflowOption(vm, opts)
		ndt := interpretDateTimeFields(vm, f, overflow)
		newOffsetNs := must(temporal.ParseDateTimeUTCOffset(f.Offset))
		epochNs := must(temporal.InterpretISODateTimeOffset(ndt.Date, ndt.Time, false, temporal.OffsetBehaviourOption, newOffsetNs, s.tz, disambiguation, offsetOption, temporal.MatchExactly))
		return r.newZonedDateTime(vm, epochNs, s.tz, s.cal, nil)
	})
	tmethod(vm, proto, "withPlainTime", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(vm, this, "withPlainTime")
		var epochNs temporal.Int128
		if t := arg(args, 0); t.IsUndefined() {
			epochNs = must(s.tz.StartOfDay(dt.Date))
		} else {
			tt := toTemporalTime(vm, t, engine.Undefined)
			epochNs = must(s.tz.EpochNanosecondsFor(temporal.ISODateTime{
				Date: dt.Date,
				Time: tt,
			}, temporal.DisambiguationCompatible))
		}
		return r.newZonedDateTime(vm, epochNs, s.tz, s.cal, nil)
	})
	tmethod(vm, proto, "withTimeZone", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(vm, this, "withTimeZone")
		tz := toTimeZone(vm, arg(args, 0))
		return r.newZonedDateTime(vm, s.ns, tz, s.cal, nil)
	})
	tmethod(vm, proto, "withCalendar", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(vm, this, "withCalendar")
		cal := toCalendar(vm, arg(args, 0))
		return r.newZonedDateTime(vm, s.ns, s.tz, cal, nil)
	})
	for _, name := range []string{"add", "subtract"} {
		tmethod(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisZonedDateTime(vm, this, name)
			d := toTemporalDuration(vm, arg(args, 0))
			if name == "subtract" {
				d = d.Negated()
			}
			overflow := getOverflowOption(vm, getOptionsObject(vm, arg(args, 1)))
			id := temporal.ToInternalDurationRecord(&d)
			epochNs := must(temporal.AddZonedDateTime(s.ns, s.tz, id, overflow))
			return r.newZonedDateTime(vm, epochNs, s.tz, s.cal, nil)
		})
	}
	for _, name := range []string{"until", "since"} {
		tmethod(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisZonedDateTime(vm, this, name)
			other := toTemporalZonedDateTime(vm, arg(args, 0), engine.Undefined)
			checkSameCalendar(vm, s.cal, other.cal)
			opts := getOptionsObject(vm, arg(args, 1))
			settings := getDifferenceSettings(vm, name == "since", opts, unitGroupDateTime, nil, temporal.UnitNanosecond, temporal.UnitHour)
			var d temporal.Duration
			if settings.largestUnit.IsTimeUnit() {
				id := must(temporal.DifferenceEpochNanoseconds(s.ns, other.ns, settings.increment, settings.smallestUnit, settings.mode))
				d = must(temporal.TemporalDurationFromInternal(id, settings.largestUnit))
			} else {
				if !temporal.TimeZoneEquals(s.tz, other.tz) {
					vm.ThrowRangeError("the time zones differ, so the largestUnit must be hours or smaller")
				}
				if s.ns.Cmp(other.ns) == 0 {
					return r.newDuration(vm, temporal.Duration{}, nil)
				}
				id := must(temporal.DifferenceZonedDateTimeWithRounding(s.ns, other.ns, s.tz, settings.largestUnit, settings.increment, settings.smallestUnit, settings.mode))
				d = must(temporal.TemporalDurationFromInternal(id, temporal.UnitHour))
			}
			if name == "since" {
				d = d.Negated()
			}
			return r.newDuration(vm, d, nil)
		})
	}
	tmethod(vm, proto, "round", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(vm, this, "round")
		increment, mode, unit := roundOptions(vm, arg(args, 0), true)
		if unit == temporal.UnitNanosecond && increment == 1 {
			return r.newZonedDateTime(vm, s.ns, s.tz, s.cal, nil)
		}
		var epochNs temporal.Int128
		if unit == temporal.UnitDay {
			start := must(s.tz.StartOfDay(dt.Date))
			end := must(s.tz.StartOfDay(temporal.AddDaysToISODate(dt.Date, 1)))
			thisNs := s.ns
			if thisNs.Cmp(end) >= 0 {
				thisNs = end.Sub(temporal.I128(1))
			}
			progress := thisNs.Sub(start)
			rounded := temporal.RoundInt128ToIncrement(progress, end.Sub(start), mode)
			epochNs = start.Add(rounded)
		} else {
			rdt := temporal.RoundISODateTime(dt, increment, unit, mode)
			offsetNs := s.tz.OffsetNanosecondsFor(s.ns)
			epochNs = must(temporal.InterpretISODateTimeOffset(rdt.Date, rdt.Time, false, temporal.OffsetBehaviourOption, offsetNs, s.tz, temporal.DisambiguationCompatible, temporal.OffsetPrefer, temporal.MatchExactly))
		}
		return r.newZonedDateTime(vm, epochNs, s.tz, s.cal, nil)
	})
	tmethod(vm, proto, "equals", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(vm, this, "equals")
		other := toTemporalZonedDateTime(vm, arg(args, 0), engine.Undefined)
		return engine.Bool(s.ns.Cmp(other.ns) == 0 && temporal.TimeZoneEquals(s.tz, other.tz) && calendarEquals(s.cal, other.cal))
	})
	tmethod(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(vm, this, "toString")
		opts := getOptionsObject(vm, arg(args, 0))
		showCalendar := getShowCalendarOption(vm, opts)
		digits := getFractionalSecondDigitsOption(vm, opts)
		showOffset := getShowOffsetOption(vm, opts)
		mode := getRoundingModeOption(vm, opts, temporal.RoundTrunc)
		unit := getUnitOption(vm, opts, "smallestUnit", false)
		showTimeZone := getShowTimeZoneOption(vm, opts)
		checkToStringUnit(vm, unit)
		p := temporal.ToSecondsStringPrecisionRecord(unit, digits)
		epochNs := temporal.RoundTemporalInstant(s.ns, p.Increment, p.Unit, mode)
		return vm.NewString(temporal.TemporalZonedDateTimeToString(epochNs, s.tz, s.cal, p.Precision, showCalendar, showTimeZone, showOffset))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisZonedDateTime(vm, this, name)
			return vm.NewString(temporal.TemporalZonedDateTimeToString(s.ns, s.tz, s.cal, temporal.PrecisionAuto, temporal.ShowCalendarAuto, temporal.ShowTimeZoneAuto, true))
		})
	}
	valueOfThrows(vm, proto, "ZonedDateTime")
	tmethod(vm, proto, "startOfDay", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(vm, this, "startOfDay")
		return r.newZonedDateTime(vm, must(s.tz.StartOfDay(dt.Date)), s.tz, s.cal, nil)
	})
	tmethod(vm, proto, "getTimeZoneTransition", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(vm, this, "getTimeZoneTransition")
		opts := roundToOptions(vm, arg(args, 0), "direction")
		next := temporalOption(vm, opts, "direction", []string{"next", "previous"}, -1) == 0
		t, ok := s.tz.Transition(s.ns, next)
		if !ok {
			return engine.Null
		}
		return r.newZonedDateTime(vm, t, s.tz, s.cal, nil)
	})
	tmethod(vm, proto, "toInstant", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return r.newInstant(vm, thisZonedDateTime(vm, this, "toInstant").ns, nil)
	})
	tmethod(vm, proto, "toPlainDate", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(vm, this, "toPlainDate")
		return r.createPlainDate(vm, dt.Date, s.cal, nil)
	})
	tmethod(vm, proto, "toPlainTime", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		_, dt := dateTimeOf(vm, this, "toPlainTime")
		return r.newPlainTime(vm, dt.Time, nil)
	})
	tmethod(vm, proto, "toPlainDateTime", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(vm, this, "toPlainDateTime")
		return r.createPlainDateTime(vm, dt, s.cal, nil)
	})
}

// systemUTCEpochNanoseconds implements SystemUTCEpochNanoseconds.
func systemUTCEpochNanoseconds() temporal.Int128 {
	return temporal.I128(time.Now().UnixNano())
}

func initTemporalNow(vm *engine.VM, ns *engine.Object, r *temporalRealm) {
	now := vm.NewOrdinaryObject()
	vm.Define(ns, vm.Intern("Now"), engine.ObjectValue(now), engine.FlagsHidden)
	vm.Define(now, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.NewString("Temporal.Now"), engine.FlagConfigurable)
	// nowTimeZone returns the time zone argument of the methods of Temporal.Now.
	nowTimeZone := func(vm *engine.VM, v engine.Value) temporal.TimeZone {
		if v.IsUndefined() {
			return systemTimeZone(vm)
		}
		return toTimeZone(vm, v)
	}
	tmethod(vm, now, "timeZoneId", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return vm.NewString(systemTimeZone(vm).ID())
	})
	tmethod(vm, now, "instant", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return r.newInstant(vm, systemUTCEpochNanoseconds(), nil)
	})
	tmethod(vm, now, "plainDateTimeISO", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		tz := nowTimeZone(vm, arg(args, 0))
		return r.createPlainDateTime(vm, tz.ISODateTimeFor(systemUTCEpochNanoseconds()), temporal.ISO8601, nil)
	})
	tmethod(vm, now, "zonedDateTimeISO", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		tz := nowTimeZone(vm, arg(args, 0))
		return r.newZonedDateTime(vm, systemUTCEpochNanoseconds(), tz, temporal.ISO8601, nil)
	})
	tmethod(vm, now, "plainDateISO", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		tz := nowTimeZone(vm, arg(args, 0))
		return r.createPlainDate(vm, tz.ISODateTimeFor(systemUTCEpochNanoseconds()).Date, temporal.ISO8601, nil)
	})
	tmethod(vm, now, "plainTimeISO", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		tz := nowTimeZone(vm, arg(args, 0))
		return r.newPlainTime(vm, tz.ISODateTimeFor(systemUTCEpochNanoseconds()).Time, nil)
	})
}
