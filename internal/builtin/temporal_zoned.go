// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/temporal"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// newZonedDateTime implements CreateTemporalZonedDateTime for valid epoch nanoseconds.
func newZonedDateTime(cx *engine.ExecutionContext, ns temporal.Int128, tz temporal.TimeZone, cal string, nt *storage.Object) storage.Value {
	proto := cx.GetPrototypeFromConstructor(nt, storage.IntrinsicTemporalZonedDateTimePrototype)
	return storage.ObjectValue(cx.NewTemporalObject(proto, &zonedDateTimeSlots{
		ns:  ns,
		tz:  tz,
		cal: cal,
	}))
}

func thisZonedDateTime(cx *engine.ExecutionContext, this storage.Value, name string) *zonedDateTimeSlots {
	s, ok := storage.TemporalData(this).(*zonedDateTimeSlots)
	if !ok {
		incompatible(cx, "Temporal.ZonedDateTime.prototype."+name, this)
	}
	return s
}

// toTemporalZonedDateTime implements ToTemporalZonedDateTime.
func toTemporalZonedDateTime(cx *engine.ExecutionContext, item, options storage.Value) zonedDateTimeSlots {
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
		if s, ok := storage.TemporalData(item).(*zonedDateTimeSlots); ok {
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

// localDateTime returns the local date-time of s.
func (s *zonedDateTimeSlots) localDateTime() temporal.ISODateTime {
	return s.tz.ISODateTimeFor(s.ns)
}

func initTemporalZonedDateTime(cx *engine.ExecutionContext, ns *storage.Object) {
	ctor, proto := temporalConstructor(cx, ns, "ZonedDateTime", 2, storage.IntrinsicTemporalZonedDateTimePrototype, nativeZonedDateTimeConstructor)
	method(cx, ctor, "from", 1, nativeZonedDateTimeFrom)
	method(cx, ctor, "compare", 2, nativeZonedDateTimeCompare)

	getter(cx, proto, cx.Intern("calendarId"), "calendarId", nativeZonedDateTimeCalendarId)
	getter(cx, proto, cx.Intern("era"), "era", nativeZonedDateTimeEra)
	getter(cx, proto, cx.Intern("eraYear"), "eraYear", nativeZonedDateTimeEraYear)
	getter(cx, proto, cx.Intern("year"), "year", nativeZonedDateTimeYear)
	getter(cx, proto, cx.Intern("month"), "month", nativeZonedDateTimeMonth)
	getter(cx, proto, cx.Intern("monthCode"), "monthCode", nativeZonedDateTimeMonthCode)
	getter(cx, proto, cx.Intern("day"), "day", nativeZonedDateTimeDay)
	getter(cx, proto, cx.Intern("dayOfWeek"), "dayOfWeek", nativeZonedDateTimeDayOfWeek)
	getter(cx, proto, cx.Intern("dayOfYear"), "dayOfYear", nativeZonedDateTimeDayOfYear)
	getter(cx, proto, cx.Intern("weekOfYear"), "weekOfYear", nativeZonedDateTimeWeekOfYear)
	getter(cx, proto, cx.Intern("yearOfWeek"), "yearOfWeek", nativeZonedDateTimeYearOfWeek)
	getter(cx, proto, cx.Intern("daysInWeek"), "daysInWeek", nativeZonedDateTimeDaysInWeek)
	getter(cx, proto, cx.Intern("daysInMonth"), "daysInMonth", nativeZonedDateTimeDaysInMonth)
	getter(cx, proto, cx.Intern("daysInYear"), "daysInYear", nativeZonedDateTimeDaysInYear)
	getter(cx, proto, cx.Intern("monthsInYear"), "monthsInYear", nativeZonedDateTimeMonthsInYear)
	getter(cx, proto, cx.Intern("inLeapYear"), "inLeapYear", nativeZonedDateTimeInLeapYear)
	getter(cx, proto, cx.Intern("hour"), "hour", nativeZonedDateTimeHour)
	getter(cx, proto, cx.Intern("minute"), "minute", nativeZonedDateTimeMinute)
	getter(cx, proto, cx.Intern("second"), "second", nativeZonedDateTimeSecond)
	getter(cx, proto, cx.Intern("millisecond"), "millisecond", nativeZonedDateTimeMillisecond)
	getter(cx, proto, cx.Intern("microsecond"), "microsecond", nativeZonedDateTimeMicrosecond)
	getter(cx, proto, cx.Intern("nanosecond"), "nanosecond", nativeZonedDateTimeNanosecond)
	getter(cx, proto, cx.Intern("timeZoneId"), "timeZoneId", nativeZonedDateTimeTimeZoneId)
	getter(cx, proto, cx.Intern("epochMilliseconds"), "epochMilliseconds", nativeZonedDateTimeEpochMilliseconds)
	getter(cx, proto, cx.Intern("epochNanoseconds"), "epochNanoseconds", nativeZonedDateTimeEpochNanoseconds)
	getter(cx, proto, cx.Intern("hoursInDay"), "hoursInDay", nativeZonedDateTimeHoursInDay)
	getter(cx, proto, cx.Intern("offsetNanoseconds"), "offsetNanoseconds", nativeZonedDateTimeOffsetNanoseconds)
	getter(cx, proto, cx.Intern("offset"), "offset", nativeZonedDateTimeOffset)
	method(cx, proto, "with", 1, nativeZonedDateTimeWith)
	method(cx, proto, "withPlainTime", 0, nativeZonedDateTimeWithPlainTime)
	method(cx, proto, "withTimeZone", 1, nativeZonedDateTimeWithTimeZone)
	method(cx, proto, "withCalendar", 1, nativeZonedDateTimeWithCalendar)
	method(cx, proto, "add", 1, nativeZonedDateTimeAdd)
	method(cx, proto, "subtract", 1, nativeZonedDateTimeSubtract)
	method(cx, proto, "until", 1, nativeZonedDateTimeUntil)
	method(cx, proto, "since", 1, nativeZonedDateTimeSince)
	method(cx, proto, "round", 1, nativeZonedDateTimeRound)
	method(cx, proto, "equals", 1, nativeZonedDateTimeEquals)
	method(cx, proto, "toString", 0, nativeZonedDateTimeToString)
	method(cx, proto, "toJSON", 0, nativeZonedDateTimeToJSON)
	method(cx, proto, "toLocaleString", 0, nativeZonedDateTimeToLocaleString)
	method(cx, proto, "valueOf", 0, nativeZonedDateTimeValueOf)
	method(cx, proto, "startOfDay", 0, nativeZonedDateTimeStartOfDay)
	method(cx, proto, "getTimeZoneTransition", 1, nativeZonedDateTimeGetTimeZoneTransition)
	method(cx, proto, "toInstant", 0, nativeZonedDateTimeToInstant)
	method(cx, proto, "toPlainDate", 0, nativeZonedDateTimeToPlainDate)
	method(cx, proto, "toPlainTime", 0, nativeZonedDateTimeToPlainTime)
	method(cx, proto, "toPlainDateTime", 0, nativeZonedDateTimeToPlainDateTime)
}

// The native functions of Temporal.ZonedDateTime.
var (
	nativeZonedDateTimeConstructor           = engine.RegisterNative(temporalNative(zonedDateTimeConstructor))
	nativeZonedDateTimeFrom                  = engine.RegisterNative(temporalNative(zonedDateTimeFrom))
	nativeZonedDateTimeCompare               = engine.RegisterNative(temporalNative(zonedDateTimeCompare))
	nativeZonedDateTimeCalendarId            = engine.RegisterNative(calendarFieldNative("calendarId", zonedDateTimeCalendarDate))
	nativeZonedDateTimeEra                   = engine.RegisterNative(calendarFieldNative("era", zonedDateTimeCalendarDate))
	nativeZonedDateTimeEraYear               = engine.RegisterNative(calendarFieldNative("eraYear", zonedDateTimeCalendarDate))
	nativeZonedDateTimeYear                  = engine.RegisterNative(calendarFieldNative("year", zonedDateTimeCalendarDate))
	nativeZonedDateTimeMonth                 = engine.RegisterNative(calendarFieldNative("month", zonedDateTimeCalendarDate))
	nativeZonedDateTimeMonthCode             = engine.RegisterNative(calendarFieldNative("monthCode", zonedDateTimeCalendarDate))
	nativeZonedDateTimeDay                   = engine.RegisterNative(calendarFieldNative("day", zonedDateTimeCalendarDate))
	nativeZonedDateTimeDayOfWeek             = engine.RegisterNative(calendarFieldNative("dayOfWeek", zonedDateTimeCalendarDate))
	nativeZonedDateTimeDayOfYear             = engine.RegisterNative(calendarFieldNative("dayOfYear", zonedDateTimeCalendarDate))
	nativeZonedDateTimeWeekOfYear            = engine.RegisterNative(calendarFieldNative("weekOfYear", zonedDateTimeCalendarDate))
	nativeZonedDateTimeYearOfWeek            = engine.RegisterNative(calendarFieldNative("yearOfWeek", zonedDateTimeCalendarDate))
	nativeZonedDateTimeDaysInWeek            = engine.RegisterNative(calendarFieldNative("daysInWeek", zonedDateTimeCalendarDate))
	nativeZonedDateTimeDaysInMonth           = engine.RegisterNative(calendarFieldNative("daysInMonth", zonedDateTimeCalendarDate))
	nativeZonedDateTimeDaysInYear            = engine.RegisterNative(calendarFieldNative("daysInYear", zonedDateTimeCalendarDate))
	nativeZonedDateTimeMonthsInYear          = engine.RegisterNative(calendarFieldNative("monthsInYear", zonedDateTimeCalendarDate))
	nativeZonedDateTimeInLeapYear            = engine.RegisterNative(calendarFieldNative("inLeapYear", zonedDateTimeCalendarDate))
	nativeZonedDateTimeHour                  = engine.RegisterNative(zonedDateTimeTimeNative("hour"))
	nativeZonedDateTimeMinute                = engine.RegisterNative(zonedDateTimeTimeNative("minute"))
	nativeZonedDateTimeSecond                = engine.RegisterNative(zonedDateTimeTimeNative("second"))
	nativeZonedDateTimeMillisecond           = engine.RegisterNative(zonedDateTimeTimeNative("millisecond"))
	nativeZonedDateTimeMicrosecond           = engine.RegisterNative(zonedDateTimeTimeNative("microsecond"))
	nativeZonedDateTimeNanosecond            = engine.RegisterNative(zonedDateTimeTimeNative("nanosecond"))
	nativeZonedDateTimeTimeZoneId            = engine.RegisterNative(temporalNative(zonedDateTimeTimeZoneId))
	nativeZonedDateTimeEpochMilliseconds     = engine.RegisterNative(temporalNative(zonedDateTimeEpochMilliseconds))
	nativeZonedDateTimeEpochNanoseconds      = engine.RegisterNative(temporalNative(zonedDateTimeEpochNanoseconds))
	nativeZonedDateTimeHoursInDay            = engine.RegisterNative(temporalNative(zonedDateTimeHoursInDay))
	nativeZonedDateTimeOffsetNanoseconds     = engine.RegisterNative(temporalNative(zonedDateTimeOffsetNanoseconds))
	nativeZonedDateTimeOffset                = engine.RegisterNative(temporalNative(zonedDateTimeOffset))
	nativeZonedDateTimeWith                  = engine.RegisterNative(temporalNative(zonedDateTimeWith))
	nativeZonedDateTimeWithPlainTime         = engine.RegisterNative(temporalNative(zonedDateTimeWithPlainTime))
	nativeZonedDateTimeWithTimeZone          = engine.RegisterNative(temporalNative(zonedDateTimeWithTimeZone))
	nativeZonedDateTimeWithCalendar          = engine.RegisterNative(temporalNative(zonedDateTimeWithCalendar))
	nativeZonedDateTimeAdd                   = engine.RegisterNative(zonedDateTimeAddNative("add"))
	nativeZonedDateTimeSubtract              = engine.RegisterNative(zonedDateTimeAddNative("subtract"))
	nativeZonedDateTimeUntil                 = engine.RegisterNative(zonedDateTimeUntilNative("until"))
	nativeZonedDateTimeSince                 = engine.RegisterNative(zonedDateTimeUntilNative("since"))
	nativeZonedDateTimeRound                 = engine.RegisterNative(temporalNative(zonedDateTimeRound))
	nativeZonedDateTimeEquals                = engine.RegisterNative(temporalNative(zonedDateTimeEquals))
	nativeZonedDateTimeToString              = engine.RegisterNative(temporalNative(zonedDateTimeToString))
	nativeZonedDateTimeToJSON                = engine.RegisterNative(zonedDateTimeToJSONNative("toJSON"))
	nativeZonedDateTimeToLocaleString        = engine.RegisterNative(zonedDateTimeToJSONNative("toLocaleString"))
	nativeZonedDateTimeValueOf               = engine.RegisterNative(temporalValueOfNative("ZonedDateTime"))
	nativeZonedDateTimeStartOfDay            = engine.RegisterNative(temporalNative(zonedDateTimeStartOfDay))
	nativeZonedDateTimeGetTimeZoneTransition = engine.RegisterNative(temporalNative(zonedDateTimeGetTimeZoneTransition))
	nativeZonedDateTimeToInstant             = engine.RegisterNative(temporalNative(zonedDateTimeToInstant))
	nativeZonedDateTimeToPlainDate           = engine.RegisterNative(temporalNative(zonedDateTimeToPlainDate))
	nativeZonedDateTimeToPlainTime           = engine.RegisterNative(temporalNative(zonedDateTimeToPlainTime))
	nativeZonedDateTimeToPlainDateTime       = engine.RegisterNative(temporalNative(zonedDateTimeToPlainDateTime))
)

// zonedDateTimeCalendarDate returns the ISO date and the calendar of this for the getter name.
func zonedDateTimeCalendarDate(cx *engine.ExecutionContext, this storage.Value, name string) (temporal.ISODate, string) {
	s := thisZonedDateTime(cx, this, name)
	dt := s.localDateTime()
	return dt.Date, s.cal
}

// zonedDateTimeTimeNative returns the getter of the time field name of
// Temporal.ZonedDateTime.prototype.
//
//go:noinline
func zonedDateTimeTimeNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		dt := thisZonedDateTime(cx, this, name).localDateTime()
		return storage.Number(float64(timeField(dt.Time, name)))
	})
}

// zonedDateTimeAddNative returns the method name of Temporal.ZonedDateTime.prototype, which is add
// or subtract.
//
//go:noinline
func zonedDateTimeAddNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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

// zonedDateTimeUntilNative returns the method name of Temporal.ZonedDateTime.prototype, which is
// until or since.
//
//go:noinline
func zonedDateTimeUntilNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		s := thisZonedDateTime(cx, this, name)
		other := toTemporalZonedDateTime(cx, arg(args, 0), storage.Undefined)
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

// zonedDateTimeToJSONNative returns the method name of Temporal.ZonedDateTime.prototype, which is
// toJSON or toLocaleString.
//
//go:noinline
func zonedDateTimeToJSONNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		s := thisZonedDateTime(cx, this, name)
		return cx.NewString(wtf8.String(temporal.TemporalZonedDateTimeToString(s.ns, s.tz, s.cal, temporal.PrecisionAuto, temporal.ShowCalendarAuto, temporal.ShowTimeZoneAuto, true)))
	})
}

func zonedDateTimeConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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
}

func zonedDateTimeFrom(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := toTemporalZonedDateTime(cx, arg(args, 0), arg(args, 1))
	return newZonedDateTime(cx, s.ns, s.tz, s.cal, nil)
}

func zonedDateTimeCompare(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	one := toTemporalZonedDateTime(cx, arg(args, 0), storage.Undefined)
	two := toTemporalZonedDateTime(cx, arg(args, 1), storage.Undefined)
	return compareResult(one.ns.Cmp(two.ns))
}

func zonedDateTimeTimeZoneId(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return cx.NewString(wtf8.String(thisZonedDateTime(cx, this, "timeZoneId").tz.ID()))
}

func zonedDateTimeEpochMilliseconds(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return epochMilliseconds(thisZonedDateTime(cx, this, "epochMilliseconds").ns)
}

func zonedDateTimeEpochNanoseconds(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return epochNsValue(cx, thisZonedDateTime(cx, this, "epochNanoseconds").ns)
}

func zonedDateTimeHoursInDay(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisZonedDateTime(cx, this, "hoursInDay")
	dt := s.localDateTime()
	today := dt.Date
	tomorrow := temporal.AddDaysToISODate(today, 1)
	start := must(s.tz.StartOfDay(today))
	end := must(s.tz.StartOfDay(tomorrow))
	return storage.Number(temporal.TotalTimeDuration(end.Sub(start), temporal.UnitHour))
}

func zonedDateTimeOffsetNanoseconds(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisZonedDateTime(cx, this, "offsetNanoseconds")
	return storage.Number(float64(s.tz.OffsetNanosecondsFor(s.ns)))
}

func zonedDateTimeOffset(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisZonedDateTime(cx, this, "offset")
	return cx.NewString(wtf8.String(temporal.FormatUTCOffsetNanoseconds(s.tz.OffsetNanosecondsFor(s.ns))))
}

func zonedDateTimeWith(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisZonedDateTime(cx, this, "with")
	dt := s.localDateTime()
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
}

func zonedDateTimeWithPlainTime(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisZonedDateTime(cx, this, "withPlainTime")
	dt := s.localDateTime()
	var epochNs temporal.Int128
	if t := arg(args, 0); t.IsUndefined() {
		epochNs = must(s.tz.StartOfDay(dt.Date))
	} else {
		tt := toTemporalTime(cx, t, storage.Undefined)
		epochNs = must(s.tz.EpochNanosecondsFor(temporal.ISODateTime{
			Date: dt.Date,
			Time: tt,
		}, temporal.DisambiguationCompatible))
	}
	return newZonedDateTime(cx, epochNs, s.tz, s.cal, nil)
}

func zonedDateTimeWithTimeZone(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisZonedDateTime(cx, this, "withTimeZone")
	tz := toTimeZone(cx, arg(args, 0))
	return newZonedDateTime(cx, s.ns, tz, s.cal, nil)
}

func zonedDateTimeWithCalendar(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisZonedDateTime(cx, this, "withCalendar")
	cal := toCalendar(cx, arg(args, 0))
	return newZonedDateTime(cx, s.ns, s.tz, cal, nil)
}

func zonedDateTimeRound(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisZonedDateTime(cx, this, "round")
	dt := s.localDateTime()
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
}

func zonedDateTimeEquals(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisZonedDateTime(cx, this, "equals")
	other := toTemporalZonedDateTime(cx, arg(args, 0), storage.Undefined)
	return storage.Bool(s.ns.Cmp(other.ns) == 0 && temporal.TimeZoneEquals(s.tz, other.tz) && calendarEquals(s.cal, other.cal))
}

func zonedDateTimeToString(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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
}

func zonedDateTimeStartOfDay(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisZonedDateTime(cx, this, "startOfDay")
	dt := s.localDateTime()
	return newZonedDateTime(cx, must(s.tz.StartOfDay(dt.Date)), s.tz, s.cal, nil)
}

func zonedDateTimeGetTimeZoneTransition(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisZonedDateTime(cx, this, "getTimeZoneTransition")
	opts := roundToOptions(cx, arg(args, 0), "direction")
	next := temporalOption(cx, opts, "direction", []string{"next", "previous"}, -1) == 0
	t, ok := s.tz.Transition(s.ns, next)
	if !ok {
		return storage.Null
	}
	return newZonedDateTime(cx, t, s.tz, s.cal, nil)
}

func zonedDateTimeToInstant(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return newInstant(cx, thisZonedDateTime(cx, this, "toInstant").ns, nil)
}

func zonedDateTimeToPlainDate(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisZonedDateTime(cx, this, "toPlainDate")
	dt := s.localDateTime()
	return createPlainDate(cx, dt.Date, s.cal, nil)
}

func zonedDateTimeToPlainTime(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	dt := thisZonedDateTime(cx, this, "toPlainTime").localDateTime()
	return newPlainTime(cx, dt.Time, nil)
}

func zonedDateTimeToPlainDateTime(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisZonedDateTime(cx, this, "toPlainDateTime")
	dt := s.localDateTime()
	return createPlainDateTime(cx, dt, s.cal, nil)
}

// systemUTCEpochNanoseconds implements SystemUTCEpochNanoseconds.
func systemUTCEpochNanoseconds(cx *engine.ExecutionContext) temporal.Int128 {
	t := cx.Now()
	return temporal.Mul64(t.Unix(), temporal.NsPerSecond).Add(temporal.I128(int64(t.Nanosecond())))
}

// temporalNowTimeZone returns the time zone argument of the methods of Temporal.Now.
func temporalNowTimeZone(cx *engine.ExecutionContext, v storage.Value) temporal.TimeZone {
	if v.IsUndefined() {
		return SystemTimeZone(cx)
	}
	return toTimeZone(cx, v)
}

func initTemporalNow(cx *engine.ExecutionContext, ns *storage.Object) {
	now := cx.NewOrdinaryObject()
	cx.Define(ns, cx.Intern("Now"), storage.ObjectValue(now), storage.FlagsHidden)
	cx.Define(now, cx.WellKnownSymbol(storage.SymbolToStringTag), cx.SharedString("Temporal.Now"), storage.FlagConfigurable)
	method(cx, now, "timeZoneId", 0, nativeTemporalNowTimeZoneId)
	method(cx, now, "instant", 0, nativeTemporalNowInstant)
	method(cx, now, "plainDateTimeISO", 0, nativeTemporalNowPlainDateTimeISO)
	method(cx, now, "zonedDateTimeISO", 0, nativeTemporalNowZonedDateTimeISO)
	method(cx, now, "plainDateISO", 0, nativeTemporalNowPlainDateISO)
	method(cx, now, "plainTimeISO", 0, nativeTemporalNowPlainTimeISO)
}

// The native functions of Temporal.Now.
var (
	nativeTemporalNowTimeZoneId       = engine.RegisterNative(temporalNative(temporalNowTimeZoneId))
	nativeTemporalNowInstant          = engine.RegisterNative(temporalNative(temporalNowInstant))
	nativeTemporalNowPlainDateTimeISO = engine.RegisterNative(temporalNative(temporalNowPlainDateTimeISO))
	nativeTemporalNowZonedDateTimeISO = engine.RegisterNative(temporalNative(temporalNowZonedDateTimeISO))
	nativeTemporalNowPlainDateISO     = engine.RegisterNative(temporalNative(temporalNowPlainDateISO))
	nativeTemporalNowPlainTimeISO     = engine.RegisterNative(temporalNative(temporalNowPlainTimeISO))
)

func temporalNowTimeZoneId(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return cx.NewString(wtf8.String(SystemTimeZone(cx).ID()))
}

func temporalNowInstant(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	return newInstant(cx, systemUTCEpochNanoseconds(cx), nil)
}

func temporalNowPlainDateTimeISO(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	tz := temporalNowTimeZone(cx, arg(args, 0))
	return createPlainDateTime(cx, tz.ISODateTimeFor(systemUTCEpochNanoseconds(cx)), temporal.ISO8601, nil)
}

func temporalNowZonedDateTimeISO(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	tz := temporalNowTimeZone(cx, arg(args, 0))
	return newZonedDateTime(cx, systemUTCEpochNanoseconds(cx), tz, temporal.ISO8601, nil)
}

func temporalNowPlainDateISO(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	tz := temporalNowTimeZone(cx, arg(args, 0))
	return createPlainDate(cx, tz.ISODateTimeFor(systemUTCEpochNanoseconds(cx)).Date, temporal.ISO8601, nil)
}

func temporalNowPlainTimeISO(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	tz := temporalNowTimeZone(cx, arg(args, 0))
	return newPlainTime(cx, tz.ISODateTimeFor(systemUTCEpochNanoseconds(cx)).Time, nil)
}
