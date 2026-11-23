// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/temporal"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// newZonedDateTime implements CreateTemporalZonedDateTime for valid epoch nanoseconds.
func newZonedDateTime(cx *engine.ExecutionContext, ns temporal.Int128, tz temporal.TimeZone, cal string, nt *engine.Object) engine.Value {
	proto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicTemporalZonedDateTimePrototype)
	return engine.ObjectValue(cx.NewTemporalObject(proto, &zonedDateTimeSlots{
		ns:  ns,
		tz:  tz,
		cal: cal,
	}))
}

func thisZonedDateTime(cx *engine.ExecutionContext, this engine.Value, name string) *zonedDateTimeSlots {
	s, ok := engine.TemporalData(this).(*zonedDateTimeSlots)
	if !ok {
		incompatible(cx, "Temporal.ZonedDateTime.prototype."+name, this)
	}
	return s
}

// toTemporalZonedDateTime implements ToTemporalZonedDateTime.
func toTemporalZonedDateTime(cx *engine.ExecutionContext, item, options engine.Value) zonedDateTimeSlots {
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
			opts := getOptionsObject(cx, options)
			getDisambiguationOption(cx, opts)
			getOffsetOption(cx, opts, temporal.OffsetReject)
			getOverflowOption(cx, opts)
			return *s
		}
		o := item.Object()
		cal = calendarWithISODefault(cx, o)
		names := append(append(append([]string{}, dateFieldNames...), timeFieldNames...), "offset", "timeZone")
		f := prepareCalendarFields(cx, cal, o, names, fieldsTimeZoneRequired)
		tz = f.TimeZone
		offset, hasOffset = f.Offset, f.HasOffset
		opts := getOptionsObject(cx, options)
		disambiguation = getDisambiguationOption(cx, opts)
		offsetOption = getOffsetOption(cx, opts, temporal.OffsetReject)
		overflow := getOverflowOption(cx, opts)
		dt := interpretDateTimeFields(cx, cal, f, overflow)
		date, t = dt.Date, dt.Time
	} else {
		str := requireString(cx, item, "zoned date-time")
		p := must(temporal.ParseISODateTime(str, cx.Poll, temporal.GoalZonedDateTime))
		tz = toTimeZone(cx, cx.NewString(wtf8.String(p.TimeZoneAnnotation)))
		offset, hasOffset = p.Offset, p.Offset != ""
		z = p.Z
		cal = canonicalizeParsedCalendar(cx, p.Calendar)
		match = temporal.MatchMinutes
		if hasOffset && !temporal.IsOffsetWithoutSubMinutes(offset) {
			match = temporal.MatchExactly
		}
		opts := getOptionsObject(cx, options)
		disambiguation = getDisambiguationOption(cx, opts)
		offsetOption = getOffsetOption(cx, opts, temporal.OffsetReject)
		getOverflowOption(cx, opts)
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

func initTemporalZonedDateTime(cx *engine.ExecutionContext, ns *engine.Object) {
	ctor, proto := temporalConstructor(cx, ns, "ZonedDateTime", 2, engine.IntrinsicTemporalZonedDateTimePrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(cx, "Temporal.ZonedDateTime")
		epochNs := bigIntToEpochNs(cx, arg(args, 0))
		id := requireString(cx, arg(args, 1), "time zone")
		name, off, isOff, err := temporal.ParseTimeZoneIdentifier(id, cx.Poll)
		check(err)
		var system temporal.TimeZone
		if !isOff && name != "UTC" {
			system = SystemTimeZone(cx)
		}
		tz := must(temporal.TimeZoneFromIdentifier(name, off, isOff, system))
		cal := temporal.ISO8601
		if c := arg(args, 2); !c.IsUndefined() {
			cal = must(temporal.CanonicalizeCalendar(requireString(cx, c, "calendar")))
		}
		return newZonedDateTime(cx, epochNs, tz, cal, nt)
	})
	tmethod(cx, ctor, "from", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := toTemporalZonedDateTime(cx, arg(args, 0), arg(args, 1))
		return newZonedDateTime(cx, s.ns, s.tz, s.cal, nil)
	})
	tmethod(cx, ctor, "compare", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		one := toTemporalZonedDateTime(cx, arg(args, 0), engine.Undefined)
		two := toTemporalZonedDateTime(cx, arg(args, 1), engine.Undefined)
		return compareResult(one.ns.Cmp(two.ns))
	})

	// dateTimeOf returns the local date-time of a ZonedDateTime receiver.
	dateTimeOf := func(cx *engine.ExecutionContext, this engine.Value, name string) (*zonedDateTimeSlots, temporal.ISODateTime) {
		s := thisZonedDateTime(cx, this, name)
		return s, s.tz.ISODateTimeFor(s.ns)
	}
	defineCalendarGetters(cx, proto, fullDateGetters, func(cx *engine.ExecutionContext, this engine.Value, name string) (temporal.ISODate, string) {
		s, dt := dateTimeOf(cx, this, name)
		return dt.Date, s.cal
	})
	for _, name := range timeGetterNames {
		tgetter(cx, proto, cx.Intern(wtf8.String(name)), wtf8.String(name), func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			_, dt := dateTimeOf(cx, this, name)
			return engine.Number(float64(timeField(dt.Time, name)))
		})
	}
	tgetter(cx, proto, cx.Intern("timeZoneId"), "timeZoneId", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return cx.NewString(wtf8.String(thisZonedDateTime(cx, this, "timeZoneId").tz.ID()))
	})
	tgetter(cx, proto, cx.Intern("epochMilliseconds"), "epochMilliseconds", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return epochMilliseconds(thisZonedDateTime(cx, this, "epochMilliseconds").ns)
	})
	tgetter(cx, proto, cx.Intern("epochNanoseconds"), "epochNanoseconds", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return epochNsValue(cx, thisZonedDateTime(cx, this, "epochNanoseconds").ns)
	})
	tgetter(cx, proto, cx.Intern("hoursInDay"), "hoursInDay", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(cx, this, "hoursInDay")
		today := dt.Date
		tomorrow := temporal.AddDaysToISODate(today, 1)
		start := must(s.tz.StartOfDay(today))
		end := must(s.tz.StartOfDay(tomorrow))
		return engine.Number(temporal.TotalTimeDuration(end.Sub(start), temporal.UnitHour))
	})
	tgetter(cx, proto, cx.Intern("offsetNanoseconds"), "offsetNanoseconds", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(cx, this, "offsetNanoseconds")
		return engine.Number(float64(s.tz.OffsetNanosecondsFor(s.ns)))
	})
	tgetter(cx, proto, cx.Intern("offset"), "offset", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(cx, this, "offset")
		return cx.NewString(wtf8.String(temporal.FormatUTCOffsetNanoseconds(s.tz.OffsetNanosecondsFor(s.ns))))
	})
	tmethod(cx, proto, "with", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(cx, this, "with")
		bag := rejectTemporalLikeObject(cx, arg(args, 0))
		offsetNs := s.tz.OffsetNanosecondsFor(s.ns)
		f := temporal.ISODateToFields(s.cal, dt.Date, temporal.FieldsDate)
		f.SetTime(dt.Time)
		f.Offset, f.HasOffset = temporal.FormatUTCOffsetNanoseconds(offsetNs), true
		names := append(append(append([]string{}, dateFieldNames...), timeFieldNames...), "offset")
		partial := prepareCalendarFields(cx, s.cal, bag, names, fieldsPartial)
		f = temporal.CalendarMergeFields(s.cal, f, partial)
		opts := getOptionsObject(cx, arg(args, 1))
		disambiguation := getDisambiguationOption(cx, opts)
		offsetOption := getOffsetOption(cx, opts, temporal.OffsetPrefer)
		overflow := getOverflowOption(cx, opts)
		ndt := interpretDateTimeFields(cx, s.cal, f, overflow)
		newOffsetNs := must(temporal.ParseDateTimeUTCOffset(f.Offset))
		epochNs := must(temporal.InterpretISODateTimeOffset(ndt.Date, ndt.Time, false, temporal.OffsetBehaviourOption, newOffsetNs, s.tz, disambiguation, offsetOption, temporal.MatchExactly))
		return newZonedDateTime(cx, epochNs, s.tz, s.cal, nil)
	})
	tmethod(cx, proto, "withPlainTime", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(cx, this, "withPlainTime")
		var epochNs temporal.Int128
		if t := arg(args, 0); t.IsUndefined() {
			epochNs = must(s.tz.StartOfDay(dt.Date))
		} else {
			tt := toTemporalTime(cx, t, engine.Undefined)
			epochNs = must(s.tz.EpochNanosecondsFor(temporal.ISODateTime{
				Date: dt.Date,
				Time: tt,
			}, temporal.DisambiguationCompatible))
		}
		return newZonedDateTime(cx, epochNs, s.tz, s.cal, nil)
	})
	tmethod(cx, proto, "withTimeZone", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(cx, this, "withTimeZone")
		tz := toTimeZone(cx, arg(args, 0))
		return newZonedDateTime(cx, s.ns, tz, s.cal, nil)
	})
	tmethod(cx, proto, "withCalendar", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(cx, this, "withCalendar")
		cal := toCalendar(cx, arg(args, 0))
		return newZonedDateTime(cx, s.ns, s.tz, cal, nil)
	})
	for _, name := range []string{"add", "subtract"} {
		tmethod(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisZonedDateTime(cx, this, name)
			d := toTemporalDuration(cx, arg(args, 0))
			if name == "subtract" {
				d = d.Negated()
			}
			overflow := getOverflowOption(cx, getOptionsObject(cx, arg(args, 1)))
			id := temporal.ToInternalDurationRecord(&d)
			epochNs := must(temporal.AddZonedDateTime(s.ns, s.tz, s.cal, id, overflow))
			return newZonedDateTime(cx, epochNs, s.tz, s.cal, nil)
		})
	}
	for _, name := range []string{"until", "since"} {
		tmethod(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisZonedDateTime(cx, this, name)
			other := toTemporalZonedDateTime(cx, arg(args, 0), engine.Undefined)
			checkSameCalendar(cx, s.cal, other.cal)
			opts := getOptionsObject(cx, arg(args, 1))
			settings := getDifferenceSettings(cx, name == "since", opts, unitGroupDateTime, nil, temporal.UnitNanosecond, temporal.UnitHour)
			var d temporal.Duration
			if settings.largestUnit.IsTimeUnit() {
				id := must(temporal.DifferenceEpochNanoseconds(s.ns, other.ns, settings.increment, settings.smallestUnit, settings.mode))
				d = must(temporal.TemporalDurationFromInternal(id, settings.largestUnit))
			} else {
				if !temporal.TimeZoneEquals(s.tz, other.tz) {
					cx.ThrowRangeError("the time zones differ, so the largestUnit must be hours or smaller")
				}
				if s.ns.Cmp(other.ns) == 0 {
					return newDuration(cx, temporal.Duration{}, nil)
				}
				id := must(temporal.DifferenceZonedDateTimeWithRounding(s.ns, other.ns, s.tz, s.cal, settings.largestUnit, settings.increment, settings.smallestUnit, settings.mode))
				d = must(temporal.TemporalDurationFromInternal(id, temporal.UnitHour))
			}
			if name == "since" {
				d = d.Negated()
			}
			return newDuration(cx, d, nil)
		})
	}
	tmethod(cx, proto, "round", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(cx, this, "round")
		increment, mode, unit := roundOptions(cx, arg(args, 0), true)
		if unit == temporal.UnitNanosecond && increment == 1 {
			return newZonedDateTime(cx, s.ns, s.tz, s.cal, nil)
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
		return newZonedDateTime(cx, epochNs, s.tz, s.cal, nil)
	})
	tmethod(cx, proto, "equals", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(cx, this, "equals")
		other := toTemporalZonedDateTime(cx, arg(args, 0), engine.Undefined)
		return engine.Bool(s.ns.Cmp(other.ns) == 0 && temporal.TimeZoneEquals(s.tz, other.tz) && calendarEquals(s.cal, other.cal))
	})
	tmethod(cx, proto, "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(cx, this, "toString")
		opts := getOptionsObject(cx, arg(args, 0))
		showCalendar := getShowCalendarOption(cx, opts)
		digits := getFractionalSecondDigitsOption(cx, opts)
		showOffset := getShowOffsetOption(cx, opts)
		mode := getRoundingModeOption(cx, opts, temporal.RoundTrunc)
		unit := getUnitOption(cx, opts, "smallestUnit", false)
		showTimeZone := getShowTimeZoneOption(cx, opts)
		checkToStringUnit(cx, unit)
		p := temporal.ToSecondsStringPrecisionRecord(unit, digits)
		epochNs := temporal.RoundTemporalInstant(s.ns, p.Increment, p.Unit, mode)
		return cx.NewString(wtf8.String(temporal.TemporalZonedDateTimeToString(epochNs, s.tz, s.cal, p.Precision, showCalendar, showTimeZone, showOffset)))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisZonedDateTime(cx, this, name)
			return cx.NewString(wtf8.String(temporal.TemporalZonedDateTimeToString(s.ns, s.tz, s.cal, temporal.PrecisionAuto, temporal.ShowCalendarAuto, temporal.ShowTimeZoneAuto, true)))
		})
	}
	valueOfThrows(cx, proto, "ZonedDateTime")
	tmethod(cx, proto, "startOfDay", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(cx, this, "startOfDay")
		return newZonedDateTime(cx, must(s.tz.StartOfDay(dt.Date)), s.tz, s.cal, nil)
	})
	tmethod(cx, proto, "getTimeZoneTransition", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisZonedDateTime(cx, this, "getTimeZoneTransition")
		opts := roundToOptions(cx, arg(args, 0), "direction")
		next := temporalOption(cx, opts, "direction", []string{"next", "previous"}, -1) == 0
		t, ok := s.tz.Transition(s.ns, next)
		if !ok {
			return engine.Null
		}
		return newZonedDateTime(cx, t, s.tz, s.cal, nil)
	})
	tmethod(cx, proto, "toInstant", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return newInstant(cx, thisZonedDateTime(cx, this, "toInstant").ns, nil)
	})
	tmethod(cx, proto, "toPlainDate", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(cx, this, "toPlainDate")
		return createPlainDate(cx, dt.Date, s.cal, nil)
	})
	tmethod(cx, proto, "toPlainTime", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		_, dt := dateTimeOf(cx, this, "toPlainTime")
		return newPlainTime(cx, dt.Time, nil)
	})
	tmethod(cx, proto, "toPlainDateTime", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s, dt := dateTimeOf(cx, this, "toPlainDateTime")
		return createPlainDateTime(cx, dt, s.cal, nil)
	})
}

// systemUTCEpochNanoseconds implements SystemUTCEpochNanoseconds.
func systemUTCEpochNanoseconds(cx *engine.ExecutionContext) temporal.Int128 {
	t := cx.Now()
	return temporal.Mul64(t.Unix(), temporal.NsPerSecond).Add(temporal.I128(int64(t.Nanosecond())))
}

func initTemporalNow(cx *engine.ExecutionContext, ns *engine.Object) {
	now := cx.NewOrdinaryObject()
	cx.Define(ns, cx.Intern("Now"), engine.ObjectValue(now), engine.FlagsHidden)
	cx.Define(now, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Temporal.Now"), engine.FlagConfigurable)
	// nowTimeZone returns the time zone argument of the methods of Temporal.Now.
	nowTimeZone := func(cx *engine.ExecutionContext, v engine.Value) temporal.TimeZone {
		if v.IsUndefined() {
			return SystemTimeZone(cx)
		}
		return toTimeZone(cx, v)
	}
	tmethod(cx, now, "timeZoneId", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return cx.NewString(wtf8.String(SystemTimeZone(cx).ID()))
	})
	tmethod(cx, now, "instant", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return newInstant(cx, systemUTCEpochNanoseconds(cx), nil)
	})
	tmethod(cx, now, "plainDateTimeISO", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		tz := nowTimeZone(cx, arg(args, 0))
		return createPlainDateTime(cx, tz.ISODateTimeFor(systemUTCEpochNanoseconds(cx)), temporal.ISO8601, nil)
	})
	tmethod(cx, now, "zonedDateTimeISO", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		tz := nowTimeZone(cx, arg(args, 0))
		return newZonedDateTime(cx, systemUTCEpochNanoseconds(cx), tz, temporal.ISO8601, nil)
	})
	tmethod(cx, now, "plainDateISO", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		tz := nowTimeZone(cx, arg(args, 0))
		return createPlainDate(cx, tz.ISODateTimeFor(systemUTCEpochNanoseconds(cx)).Date, temporal.ISO8601, nil)
	})
	tmethod(cx, now, "plainTimeISO", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		tz := nowTimeZone(cx, arg(args, 0))
		return newPlainTime(cx, tz.ISODateTimeFor(systemUTCEpochNanoseconds(cx)).Time, nil)
	})
}
