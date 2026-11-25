// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/temporal"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// createPlainDate implements CreateTemporalDate.
func createPlainDate(cx *engine.ExecutionContext, d temporal.ISODate, cal string, nt *engine.Object) engine.Value {
	if !temporal.ISODateWithinLimits(d) {
		cx.ThrowRangeError("date is out of range")
	}
	proto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicTemporalPlainDatePrototype)
	return engine.ObjectValue(cx.NewTemporalObject(proto, &plainDateSlots{
		date: d,
		cal:  cal,
	}))
}

// createPlainDateTime implements CreateTemporalDateTime.
func createPlainDateTime(cx *engine.ExecutionContext, dt temporal.ISODateTime, cal string, nt *engine.Object) engine.Value {
	if !temporal.ISODateTimeWithinLimits(dt) {
		cx.ThrowRangeError("date-time is out of range")
	}
	proto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicTemporalPlainDateTimePrototype)
	return engine.ObjectValue(cx.NewTemporalObject(proto, &plainDateTimeSlots{
		dt:  dt,
		cal: cal,
	}))
}

// newPlainTime implements CreateTemporalTime.
func newPlainTime(cx *engine.ExecutionContext, t temporal.Time, nt *engine.Object) engine.Value {
	proto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicTemporalPlainTimePrototype)
	return engine.ObjectValue(cx.NewTemporalObject(proto, &plainTimeSlots{
		t: t,
	}))
}

func thisPlainDate(cx *engine.ExecutionContext, this engine.Value, name string) *plainDateSlots {
	s, ok := engine.TemporalData(this).(*plainDateSlots)
	if !ok {
		incompatible(cx, "Temporal.PlainDate.prototype."+name, this)
	}
	return s
}

func thisPlainTime(cx *engine.ExecutionContext, this engine.Value, name string) *plainTimeSlots {
	s, ok := engine.TemporalData(this).(*plainTimeSlots)
	if !ok {
		incompatible(cx, "Temporal.PlainTime.prototype."+name, this)
	}
	return s
}

func thisPlainDateTime(cx *engine.ExecutionContext, this engine.Value, name string) *plainDateTimeSlots {
	s, ok := engine.TemporalData(this).(*plainDateTimeSlots)
	if !ok {
		incompatible(cx, "Temporal.PlainDateTime.prototype."+name, this)
	}
	return s
}

// toTemporalDate implements ToTemporalDate.
func toTemporalDate(cx *engine.ExecutionContext, item, options engine.Value) plainDateSlots {
	if item.IsObject() {
		switch s := engine.TemporalData(item).(type) {
		case *plainDateSlots:
			getOverflowOption(cx, getOptionsObject(cx, options))
			return *s
		case *zonedDateTimeSlots:
			dt := s.localDateTime()
			getOverflowOption(cx, getOptionsObject(cx, options))
			return plainDateSlots{
				date: dt.Date,
				cal:  s.cal,
			}
		case *plainDateTimeSlots:
			getOverflowOption(cx, getOptionsObject(cx, options))
			return plainDateSlots{
				date: s.dt.Date,
				cal:  s.cal,
			}
		}
		o := item.Object()
		cal := calendarWithISODefault(cx, o)
		f := prepareCalendarFields(cx, cal, o, dateFieldNames, fieldsNoneRequired)
		overflow := getOverflowOption(cx, getOptionsObject(cx, options))
		d := must(temporal.CalendarDateFromFields(cal, f, overflow))
		return plainDateSlots{
			date: d,
			cal:  cal,
		}
	}
	str := requireString(cx, item, "date")
	p := must(temporal.ParseISODateTime(str, cx.Poll, temporal.GoalDateTime))
	cal := canonicalizeParsedCalendar(cx, p.Calendar)
	getOverflowOption(cx, getOptionsObject(cx, options))
	d := temporal.ISODate{Year: p.Year, Month: p.Month, Day: p.Day}
	if !temporal.ISODateWithinLimits(d) {
		cx.ThrowRangeError("date is out of range")
	}
	return plainDateSlots{
		date: d,
		cal:  cal,
	}
}

// toTemporalDateTime implements ToTemporalDateTime.
func toTemporalDateTime(cx *engine.ExecutionContext, item, options engine.Value) plainDateTimeSlots {
	if item.IsObject() {
		switch s := engine.TemporalData(item).(type) {
		case *plainDateTimeSlots:
			getOverflowOption(cx, getOptionsObject(cx, options))
			return *s
		case *zonedDateTimeSlots:
			dt := s.localDateTime()
			getOverflowOption(cx, getOptionsObject(cx, options))
			return plainDateTimeSlots{
				dt:  dt,
				cal: s.cal,
			}
		case *plainDateSlots:
			getOverflowOption(cx, getOptionsObject(cx, options))
			return plainDateTimeSlots{
				dt: temporal.ISODateTime{
					Date: s.date,
				},
				cal: s.cal,
			}
		}
		o := item.Object()
		cal := calendarWithISODefault(cx, o)
		f := prepareCalendarFields(cx, cal, o, append(append([]string{}, dateFieldNames...), timeFieldNames...), fieldsNoneRequired)
		overflow := getOverflowOption(cx, getOptionsObject(cx, options))
		dt := interpretDateTimeFields(cx, cal, f, overflow)
		if !temporal.ISODateTimeWithinLimits(dt) {
			cx.ThrowRangeError("date-time is out of range")
		}
		return plainDateTimeSlots{
			dt:  dt,
			cal: cal,
		}
	}
	str := requireString(cx, item, "date-time")
	p := must(temporal.ParseISODateTime(str, cx.Poll, temporal.GoalDateTime))
	cal := canonicalizeParsedCalendar(cx, p.Calendar)
	getOverflowOption(cx, getOptionsObject(cx, options))
	dt := temporal.ISODateTime{
		Date: temporal.ISODate{Year: p.Year, Month: p.Month, Day: p.Day},
		Time: p.Time,
	}
	if !temporal.ISODateTimeWithinLimits(dt) {
		cx.ThrowRangeError("date-time is out of range")
	}
	return plainDateTimeSlots{
		dt:  dt,
		cal: cal,
	}
}

// toTemporalTime implements ToTemporalTime.
func toTemporalTime(cx *engine.ExecutionContext, item, options engine.Value) temporal.Time {
	if item.IsObject() {
		switch s := engine.TemporalData(item).(type) {
		case *plainTimeSlots:
			getOverflowOption(cx, getOptionsObject(cx, options))
			return s.t
		case *plainDateTimeSlots:
			getOverflowOption(cx, getOptionsObject(cx, options))
			return s.dt.Time
		case *zonedDateTimeSlots:
			dt := s.localDateTime()
			getOverflowOption(cx, getOptionsObject(cx, options))
			return dt.Time
		}
		tr := toTemporalTimeRecord(cx, item.Object(), false)
		overflow := getOverflowOption(cx, getOptionsObject(cx, options))
		v := tr.values
		return must(temporal.RegulateTime(v[0], v[1], v[2], v[3], v[4], v[5], overflow))
	}
	str := requireString(cx, item, "time")
	p := must(temporal.ParseISODateTime(str, cx.Poll, temporal.GoalTime))
	getOverflowOption(cx, getOptionsObject(cx, options))
	return p.Time
}

// toTimeRecordOrMidnight implements ToTimeRecordOrMidnight.
func toTimeRecordOrMidnight(cx *engine.ExecutionContext, item engine.Value) temporal.Time {
	if item.IsUndefined() {
		return temporal.MidnightTime
	}
	return toTemporalTime(cx, item, engine.Undefined)
}

// defineCalendarGetters defines the getters of the calendar fields names on proto. dateOf
// returns the ISO date and the calendar of a receiver, or throws a TypeError.
func defineCalendarGetters(cx *engine.ExecutionContext, proto *engine.Object, names []string, dateOf func(cx *engine.ExecutionContext, this engine.Value, name string) (temporal.ISODate, string)) {
	for _, name := range names {
		tgetter(cx, proto, cx.Intern(wtf8.String(name)), wtf8.String(name), func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			d, cal := dateOf(cx, this, name)
			if name == "calendarId" {
				return cx.NewString(wtf8.String(cal))
			}
			c := temporal.CalendarISOToDate(cal, d)
			switch name {
			case "era":
				if c.Era == "" {
					return engine.Undefined
				}
				return cx.NewString(wtf8.String(c.Era))
			case "eraYear":
				if c.Era == "" {
					return engine.Undefined
				}
				return engine.Number(float64(c.EraYear))
			case "year":
				return engine.Number(float64(c.Year))
			case "month":
				return engine.Number(float64(c.Month))
			case "monthCode":
				return cx.NewString(wtf8.String(c.MonthCode))
			case "day":
				return engine.Number(float64(c.Day))
			case "dayOfWeek":
				return engine.Number(float64(c.DayOfWeek))
			case "dayOfYear":
				return engine.Number(float64(c.DayOfYear))
			case "weekOfYear":
				if c.WeekOfYear == 0 {
					return engine.Undefined
				}
				return engine.Number(float64(c.WeekOfYear))
			case "yearOfWeek":
				if c.WeekOfYear == 0 {
					return engine.Undefined
				}
				return engine.Number(float64(c.YearOfWeek))
			case "daysInWeek":
				return engine.Number(float64(c.DaysInWeek))
			case "daysInMonth":
				return engine.Number(float64(c.DaysInMonth))
			case "daysInYear":
				return engine.Number(float64(c.DaysInYear))
			case "monthsInYear":
				return engine.Number(float64(c.MonthsInYear))
			case "inLeapYear":
				return engine.Bool(c.InLeapYear)
			}
			return engine.Undefined
		})
	}
}

// fullDateGetters are the calendar getters of PlainDate, PlainDateTime and ZonedDateTime.
var fullDateGetters = []string{"calendarId", "era", "eraYear", "year", "month", "monthCode", "day", "dayOfWeek", "dayOfYear", "weekOfYear", "yearOfWeek", "daysInWeek", "daysInMonth", "daysInYear", "monthsInYear", "inLeapYear"}

// timeGetterNames are the names of the time getters.
var timeGetterNames = []string{"hour", "minute", "second", "millisecond", "microsecond", "nanosecond"}

// timeField returns the field name of t.
func timeField(t temporal.Time, name string) int {
	switch name {
	case "hour":
		return t.Hour
	case "minute":
		return t.Minute
	case "second":
		return t.Second
	case "millisecond":
		return t.Millisecond
	case "microsecond":
		return t.Microsecond
	}
	return t.Nanosecond
}

// calendarEquals implements CalendarEquals.
func calendarEquals(a, b string) bool {
	return a == b
}

// checkSameCalendar throws a RangeError when the calendars of a difference are not equal.
func checkSameCalendar(cx *engine.ExecutionContext, a, b string) {
	if !calendarEquals(a, b) {
		cx.ThrowRangeError("the calendars " + a + " and " + b + " differ")
	}
}

// toZonedDateTimeArgs reads the argument of PlainDate.prototype.toZonedDateTime.
func toZonedDateTimeArgs(cx *engine.ExecutionContext, item engine.Value) (temporal.TimeZone, engine.Value) {
	if item.IsObject() {
		o := item.Object()
		tzLike := get(cx, o, "timeZone")
		if tzLike.IsUndefined() {
			return toTimeZone(cx, item), engine.Undefined
		}
		tz := toTimeZone(cx, tzLike)
		return tz, get(cx, o, "plainTime")
	}
	return toTimeZone(cx, item), engine.Undefined
}

func initTemporalPlainDate(cx *engine.ExecutionContext, ns *engine.Object) {
	ctor, proto := temporalConstructor(cx, ns, "PlainDate", 3, engine.IntrinsicTemporalPlainDatePrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(cx, "Temporal.PlainDate")
		y := toIntegerWithTruncation(cx, arg(args, 0), "year")
		m := toIntegerWithTruncation(cx, arg(args, 1), "month")
		d := toIntegerWithTruncation(cx, arg(args, 2), "day")
		cal := temporal.ISO8601
		if c := arg(args, 3); !c.IsUndefined() {
			cal = must(temporal.CanonicalizeCalendar(requireString(cx, c, "calendar")))
		}
		date := must(temporal.CreateISODateRecord(y, m, d))
		return createPlainDate(cx, date, cal, nt)
	})
	tmethod(cx, ctor, "from", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := toTemporalDate(cx, arg(args, 0), arg(args, 1))
		return createPlainDate(cx, s.date, s.cal, nil)
	})
	tmethod(cx, ctor, "compare", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		one := toTemporalDate(cx, arg(args, 0), engine.Undefined)
		two := toTemporalDate(cx, arg(args, 1), engine.Undefined)
		return compareResult(temporal.CompareISODate(one.date, two.date))
	})

	defineCalendarGetters(cx, proto, fullDateGetters, func(cx *engine.ExecutionContext, this engine.Value, name string) (temporal.ISODate, string) {
		s := thisPlainDate(cx, this, name)
		return s.date, s.cal
	})
	tmethod(cx, proto, "toPlainYearMonth", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(cx, this, "toPlainYearMonth")
		f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsDate)
		return createPlainYearMonth(cx, must(temporal.CalendarYearMonthFromFields(s.cal, f, temporal.OverflowConstrain)), s.cal, nil)
	})
	tmethod(cx, proto, "toPlainMonthDay", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(cx, this, "toPlainMonthDay")
		f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsDate)
		return createPlainMonthDay(cx, must(temporal.CalendarMonthDayFromFields(s.cal, f, temporal.OverflowConstrain)), s.cal, nil)
	})
	tmethod(cx, proto, "toPlainDateTime", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(cx, this, "toPlainDateTime")
		t := toTimeRecordOrMidnight(cx, arg(args, 0))
		return createPlainDateTime(cx, temporal.ISODateTime{
			Date: s.date,
			Time: t,
		}, s.cal, nil)
	})
	tmethod(cx, proto, "toZonedDateTime", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(cx, this, "toZonedDateTime")
		tz, plainTime := toZonedDateTimeArgs(cx, arg(args, 0))
		var epochNs temporal.Int128
		if plainTime.IsUndefined() {
			epochNs = must(tz.StartOfDay(s.date))
		} else {
			t := toTemporalTime(cx, plainTime, engine.Undefined)
			epochNs = must(tz.EpochNanosecondsFor(temporal.ISODateTime{
				Date: s.date,
				Time: t,
			}, temporal.DisambiguationCompatible))
		}
		return newZonedDateTime(cx, epochNs, tz, s.cal, nil)
	})
	tmethod(cx, proto, "with", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(cx, this, "with")
		bag := rejectTemporalLikeObject(cx, arg(args, 0))
		f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsDate)
		partial := prepareCalendarFields(cx, s.cal, bag, dateFieldNames, fieldsPartial)
		f = temporal.CalendarMergeFields(s.cal, f, partial)
		overflow := getOverflowOption(cx, getOptionsObject(cx, arg(args, 1)))
		return createPlainDate(cx, must(temporal.CalendarDateFromFields(s.cal, f, overflow)), s.cal, nil)
	})
	tmethod(cx, proto, "withCalendar", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(cx, this, "withCalendar")
		cal := toCalendar(cx, arg(args, 0))
		return createPlainDate(cx, s.date, cal, nil)
	})
	for _, name := range []string{"add", "subtract"} {
		tmethod(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainDate(cx, this, name)
			d := toTemporalDuration(cx, arg(args, 0))
			if name == "subtract" {
				d = d.Negated()
			}
			dd := must(temporal.ToDateDurationRecordWithoutTime(&d))
			overflow := getOverflowOption(cx, getOptionsObject(cx, arg(args, 1)))
			return createPlainDate(cx, must(temporal.CalendarDateAdd(s.cal, s.date, dd, overflow)), s.cal, nil)
		})
	}
	for _, name := range []string{"until", "since"} {
		tmethod(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainDate(cx, this, name)
			other := toTemporalDate(cx, arg(args, 0), engine.Undefined)
			checkSameCalendar(cx, s.cal, other.cal)
			opts := getOptionsObject(cx, arg(args, 1))
			settings := getDifferenceSettings(cx, name == "since", opts, unitGroupDate, nil, temporal.UnitDay, temporal.UnitDay)
			if temporal.CompareISODate(s.date, other.date) == 0 {
				return newDuration(cx, temporal.Duration{}, nil)
			}
			dd := temporal.CalendarDateUntil(s.cal, s.date, other.date, settings.largestUnit)
			id := temporal.InternalDuration{
				Date: dd,
			}
			if settings.smallestUnit != temporal.UnitDay || settings.increment != 1 {
				from := temporal.ISODateTime{
					Date: s.date,
				}
				to := temporal.ISODateTime{
					Date: other.date,
				}
				id = must(temporal.RoundRelativeDuration(id, from.UTCEpochNanoseconds(), to.UTCEpochNanoseconds(), from, s.cal, nil,
					settings.largestUnit, settings.increment, settings.smallestUnit, settings.mode))
			}
			d := must(temporal.TemporalDurationFromInternal(id, temporal.UnitDay))
			if name == "since" {
				d = d.Negated()
			}
			return newDuration(cx, d, nil)
		})
	}
	tmethod(cx, proto, "equals", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(cx, this, "equals")
		other := toTemporalDate(cx, arg(args, 0), engine.Undefined)
		return engine.Bool(temporal.CompareISODate(s.date, other.date) == 0 && calendarEquals(s.cal, other.cal))
	})
	tmethod(cx, proto, "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(cx, this, "toString")
		show := getShowCalendarOption(cx, getOptionsObject(cx, arg(args, 0)))
		return cx.NewString(wtf8.String(temporal.TemporalDateToString(s.date, s.cal, show)))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainDate(cx, this, name)
			return cx.NewString(wtf8.String(temporal.TemporalDateToString(s.date, s.cal, temporal.ShowCalendarAuto)))
		})
	}
	valueOfThrows(cx, proto, "PlainDate")
}

// maximumTimeIncrements are the maximum rounding increments of the time units within a day.
var maximumTimeIncrements = map[temporal.Unit]int64{
	temporal.UnitDay:         1,
	temporal.UnitHour:        24,
	temporal.UnitMinute:      60,
	temporal.UnitSecond:      60,
	temporal.UnitMillisecond: 1000,
	temporal.UnitMicrosecond: 1000,
	temporal.UnitNanosecond:  1000,
}

// toStringRounding reads the options of the toString methods of PlainTime, PlainDateTime and
// ZonedDateTime that are common to them, after the options that come before them.
func toStringRounding(cx *engine.ExecutionContext, opts *engine.Object) (temporal.Precision, temporal.RoundingMode, temporal.Unit) {
	digits := getFractionalSecondDigitsOption(cx, opts)
	mode := getRoundingModeOption(cx, opts, temporal.RoundTrunc)
	unit := getUnitOption(cx, opts, "smallestUnit", false)
	return digits, mode, unit
}

// checkToStringUnit validates the smallestUnit of a toString method.
func checkToStringUnit(cx *engine.ExecutionContext, unit temporal.Unit) {
	validateUnit(cx, unit, unitGroupTime)
	if unit == temporal.UnitHour {
		cx.ThrowRangeError("smallestUnit must not be hour")
	}
}

// roundOptions reads the options of the round methods of PlainTime, PlainDateTime and
// ZonedDateTime. withDay allows the day unit.
func roundOptions(cx *engine.ExecutionContext, roundTo engine.Value, withDay bool) (int64, temporal.RoundingMode, temporal.Unit) {
	opts := roundToOptions(cx, roundTo, "smallestUnit")
	increment := getRoundingIncrementOption(cx, opts)
	mode := getRoundingModeOption(cx, opts, temporal.RoundHalfExpand)
	unit := getUnitOption(cx, opts, "smallestUnit", true)
	if withDay {
		validateUnit(cx, unit, unitGroupTime, temporal.UnitDay)
	} else {
		validateUnit(cx, unit, unitGroupTime)
	}
	maximum := maximumTimeIncrements[unit]
	check(temporal.ValidateTemporalRoundingIncrement(increment, maximum, maximum == 1))
	return increment, mode, unit
}

func initTemporalPlainTime(cx *engine.ExecutionContext, ns *engine.Object) {
	ctor, proto := temporalConstructor(cx, ns, "PlainTime", 0, engine.IntrinsicTemporalPlainTimePrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(cx, "Temporal.PlainTime")
		var v [6]float64
		for i := range v {
			if a := arg(args, i); !a.IsUndefined() {
				v[i] = toIntegerWithTruncation(cx, a, timeGetterNames[i])
			}
		}
		t := must(temporal.CreateTimeRecord(v[0], v[1], v[2], v[3], v[4], v[5]))
		return newPlainTime(cx, t, nt)
	})
	tmethod(cx, ctor, "from", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return newPlainTime(cx, toTemporalTime(cx, arg(args, 0), arg(args, 1)), nil)
	})
	tmethod(cx, ctor, "compare", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		one := toTemporalTime(cx, arg(args, 0), engine.Undefined)
		two := toTemporalTime(cx, arg(args, 1), engine.Undefined)
		return compareResult(temporal.CompareTimeRecord(one, two))
	})
	for _, name := range timeGetterNames {
		tgetter(cx, proto, cx.Intern(wtf8.String(name)), wtf8.String(name), func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			return engine.Number(float64(timeField(thisPlainTime(cx, this, name).t, name)))
		})
	}
	tmethod(cx, proto, "with", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainTime(cx, this, "with")
		bag := rejectTemporalLikeObject(cx, arg(args, 0))
		partial := toTemporalTimeRecord(cx, bag, true)
		t := s.t
		v := [6]float64{float64(t.Hour), float64(t.Minute), float64(t.Second), float64(t.Millisecond), float64(t.Microsecond), float64(t.Nanosecond)}
		for i := range v {
			if partial.has[i] {
				v[i] = partial.values[i]
			}
		}
		overflow := getOverflowOption(cx, getOptionsObject(cx, arg(args, 1)))
		return newPlainTime(cx, must(temporal.RegulateTime(v[0], v[1], v[2], v[3], v[4], v[5], overflow)), nil)
	})
	for _, name := range []string{"add", "subtract"} {
		tmethod(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainTime(cx, this, name)
			d := toTemporalDuration(cx, arg(args, 0))
			if name == "subtract" {
				d = d.Negated()
			}
			id := temporal.ToInternalDurationRecordWith24HourDays(&d)
			t, _ := temporal.AddTime(s.t, id.Time)
			return newPlainTime(cx, t, nil)
		})
	}
	for _, name := range []string{"until", "since"} {
		tmethod(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainTime(cx, this, name)
			other := toTemporalTime(cx, arg(args, 0), engine.Undefined)
			opts := getOptionsObject(cx, arg(args, 1))
			settings := getDifferenceSettings(cx, name == "since", opts, unitGroupTime, nil, temporal.UnitNanosecond, temporal.UnitHour)
			t := must(temporal.RoundTimeDuration(temporal.DifferenceTime(s.t, other), settings.increment, settings.smallestUnit, settings.mode))
			d := must(temporal.TemporalDurationFromInternal(temporal.CombineDateAndTimeDuration(temporal.DateDuration{}, t), settings.largestUnit))
			if name == "since" {
				d = d.Negated()
			}
			return newDuration(cx, d, nil)
		})
	}
	tmethod(cx, proto, "round", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainTime(cx, this, "round")
		increment, mode, unit := roundOptions(cx, arg(args, 0), false)
		t, _ := temporal.RoundTime(s.t, increment, unit, mode)
		return newPlainTime(cx, t, nil)
	})
	tmethod(cx, proto, "equals", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainTime(cx, this, "equals")
		return engine.Bool(temporal.CompareTimeRecord(s.t, toTemporalTime(cx, arg(args, 0), engine.Undefined)) == 0)
	})
	tmethod(cx, proto, "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainTime(cx, this, "toString")
		digits, mode, unit := toStringRounding(cx, getOptionsObject(cx, arg(args, 0)))
		checkToStringUnit(cx, unit)
		p := temporal.ToSecondsStringPrecisionRecord(unit, digits)
		t, _ := temporal.RoundTime(s.t, p.Increment, p.Unit, mode)
		return cx.NewString(wtf8.String(temporal.TimeRecordToString(t, p.Precision)))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			return cx.NewString(wtf8.String(temporal.TimeRecordToString(thisPlainTime(cx, this, name).t, temporal.PrecisionAuto)))
		})
	}
	valueOfThrows(cx, proto, "PlainTime")
}

func initTemporalPlainDateTime(cx *engine.ExecutionContext, ns *engine.Object) {
	ctor, proto := temporalConstructor(cx, ns, "PlainDateTime", 3, engine.IntrinsicTemporalPlainDateTimePrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(cx, "Temporal.PlainDateTime")
		y := toIntegerWithTruncation(cx, arg(args, 0), "year")
		m := toIntegerWithTruncation(cx, arg(args, 1), "month")
		d := toIntegerWithTruncation(cx, arg(args, 2), "day")
		var v [6]float64
		for i := range v {
			if a := arg(args, 3+i); !a.IsUndefined() {
				v[i] = toIntegerWithTruncation(cx, a, timeGetterNames[i])
			}
		}
		cal := temporal.ISO8601
		if c := arg(args, 9); !c.IsUndefined() {
			cal = must(temporal.CanonicalizeCalendar(requireString(cx, c, "calendar")))
		}
		date := must(temporal.CreateISODateRecord(y, m, d))
		t := must(temporal.CreateTimeRecord(v[0], v[1], v[2], v[3], v[4], v[5]))
		return createPlainDateTime(cx, temporal.ISODateTime{
			Date: date,
			Time: t,
		}, cal, nt)
	})
	tmethod(cx, ctor, "from", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := toTemporalDateTime(cx, arg(args, 0), arg(args, 1))
		return createPlainDateTime(cx, s.dt, s.cal, nil)
	})
	tmethod(cx, ctor, "compare", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		one := toTemporalDateTime(cx, arg(args, 0), engine.Undefined)
		two := toTemporalDateTime(cx, arg(args, 1), engine.Undefined)
		return compareResult(temporal.CompareISODateTime(one.dt, two.dt))
	})

	defineCalendarGetters(cx, proto, fullDateGetters, func(cx *engine.ExecutionContext, this engine.Value, name string) (temporal.ISODate, string) {
		s := thisPlainDateTime(cx, this, name)
		return s.dt.Date, s.cal
	})
	for _, name := range timeGetterNames {
		tgetter(cx, proto, cx.Intern(wtf8.String(name)), wtf8.String(name), func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			return engine.Number(float64(timeField(thisPlainDateTime(cx, this, name).dt.Time, name)))
		})
	}
	tmethod(cx, proto, "with", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(cx, this, "with")
		bag := rejectTemporalLikeObject(cx, arg(args, 0))
		f := temporal.ISODateToFields(s.cal, s.dt.Date, temporal.FieldsDate)
		f.SetTime(s.dt.Time)
		partial := prepareCalendarFields(cx, s.cal, bag, append(append([]string{}, dateFieldNames...), timeFieldNames...), fieldsPartial)
		f = temporal.CalendarMergeFields(s.cal, f, partial)
		overflow := getOverflowOption(cx, getOptionsObject(cx, arg(args, 1)))
		return createPlainDateTime(cx, interpretDateTimeFields(cx, s.cal, f, overflow), s.cal, nil)
	})
	tmethod(cx, proto, "withPlainTime", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(cx, this, "withPlainTime")
		t := toTimeRecordOrMidnight(cx, arg(args, 0))
		return createPlainDateTime(cx, temporal.ISODateTime{
			Date: s.dt.Date,
			Time: t,
		}, s.cal, nil)
	})
	tmethod(cx, proto, "withCalendar", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(cx, this, "withCalendar")
		cal := toCalendar(cx, arg(args, 0))
		return createPlainDateTime(cx, s.dt, cal, nil)
	})
	for _, name := range []string{"add", "subtract"} {
		tmethod(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainDateTime(cx, this, name)
			d := toTemporalDuration(cx, arg(args, 0))
			if name == "subtract" {
				d = d.Negated()
			}
			overflow := getOverflowOption(cx, getOptionsObject(cx, arg(args, 1)))
			id := temporal.ToInternalDurationRecordWith24HourDays(&d)
			t, days := temporal.AddTime(s.dt.Time, id.Time)
			dd := must(temporal.AdjustDateDurationRecord(id.Date, days, -1<<63, -1<<63))
			date := must(temporal.CalendarDateAdd(s.cal, s.dt.Date, dd, overflow))
			return createPlainDateTime(cx, temporal.ISODateTime{
				Date: date,
				Time: t,
			}, s.cal, nil)
		})
	}
	for _, name := range []string{"until", "since"} {
		tmethod(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainDateTime(cx, this, name)
			other := toTemporalDateTime(cx, arg(args, 0), engine.Undefined)
			checkSameCalendar(cx, s.cal, other.cal)
			opts := getOptionsObject(cx, arg(args, 1))
			settings := getDifferenceSettings(cx, name == "since", opts, unitGroupDateTime, nil, temporal.UnitNanosecond, temporal.UnitDay)
			if temporal.CompareISODateTime(s.dt, other.dt) == 0 {
				return newDuration(cx, temporal.Duration{}, nil)
			}
			id := must(temporal.DifferencePlainDateTimeWithRounding(s.dt, other.dt, s.cal, settings.largestUnit, settings.increment, settings.smallestUnit, settings.mode))
			d := must(temporal.TemporalDurationFromInternal(id, settings.largestUnit))
			if name == "since" {
				d = d.Negated()
			}
			return newDuration(cx, d, nil)
		})
	}
	tmethod(cx, proto, "round", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(cx, this, "round")
		increment, mode, unit := roundOptions(cx, arg(args, 0), true)
		if unit == temporal.UnitNanosecond && increment == 1 {
			return createPlainDateTime(cx, s.dt, s.cal, nil)
		}
		return createPlainDateTime(cx, temporal.RoundISODateTime(s.dt, increment, unit, mode), s.cal, nil)
	})
	tmethod(cx, proto, "equals", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(cx, this, "equals")
		other := toTemporalDateTime(cx, arg(args, 0), engine.Undefined)
		return engine.Bool(temporal.CompareISODateTime(s.dt, other.dt) == 0 && calendarEquals(s.cal, other.cal))
	})
	tmethod(cx, proto, "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(cx, this, "toString")
		opts := getOptionsObject(cx, arg(args, 0))
		show := getShowCalendarOption(cx, opts)
		digits, mode, unit := toStringRounding(cx, opts)
		checkToStringUnit(cx, unit)
		p := temporal.ToSecondsStringPrecisionRecord(unit, digits)
		dt := temporal.RoundISODateTime(s.dt, p.Increment, p.Unit, mode)
		if !temporal.ISODateTimeWithinLimits(dt) {
			cx.ThrowRangeError("date-time is out of range")
		}
		return cx.NewString(wtf8.String(temporal.ISODateTimeToString(dt, s.cal, p.Precision, show)))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainDateTime(cx, this, name)
			return cx.NewString(wtf8.String(temporal.ISODateTimeToString(s.dt, s.cal, temporal.PrecisionAuto, temporal.ShowCalendarAuto)))
		})
	}
	valueOfThrows(cx, proto, "PlainDateTime")
	tmethod(cx, proto, "toZonedDateTime", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(cx, this, "toZonedDateTime")
		tz := toTimeZone(cx, arg(args, 0))
		d := getDisambiguationOption(cx, getOptionsObject(cx, arg(args, 1)))
		epochNs := must(tz.EpochNanosecondsFor(s.dt, d))
		return newZonedDateTime(cx, epochNs, tz, s.cal, nil)
	})
	tmethod(cx, proto, "toPlainDate", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(cx, this, "toPlainDate")
		return createPlainDate(cx, s.dt.Date, s.cal, nil)
	})
	tmethod(cx, proto, "toPlainTime", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(cx, this, "toPlainTime")
		return newPlainTime(cx, s.dt.Time, nil)
	})
}
