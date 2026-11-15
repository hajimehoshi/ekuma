// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/temporal"
)

// createPlainDate implements CreateTemporalDate.
func (r *temporalRealm) createPlainDate(vm *engine.VM, d temporal.ISODate, cal string, nt *engine.Object) engine.Value {
	if !temporal.ISODateWithinLimits(d) {
		vm.ThrowRangeError("date is out of range")
	}
	return engine.ObjectValue(vm.NewTemporalObject(protoFor(vm, nt, r.plainDate), &plainDateSlots{
		date: d,
		cal:  cal,
	}))
}

// createPlainDateTime implements CreateTemporalDateTime.
func (r *temporalRealm) createPlainDateTime(vm *engine.VM, dt temporal.ISODateTime, cal string, nt *engine.Object) engine.Value {
	if !temporal.ISODateTimeWithinLimits(dt) {
		vm.ThrowRangeError("date-time is out of range")
	}
	return engine.ObjectValue(vm.NewTemporalObject(protoFor(vm, nt, r.plainDateTime), &plainDateTimeSlots{
		dt:  dt,
		cal: cal,
	}))
}

// newPlainTime implements CreateTemporalTime.
func (r *temporalRealm) newPlainTime(vm *engine.VM, t temporal.Time, nt *engine.Object) engine.Value {
	return engine.ObjectValue(vm.NewTemporalObject(protoFor(vm, nt, r.plainTime), &plainTimeSlots{
		t: t,
	}))
}

func thisPlainDate(vm *engine.VM, this engine.Value, name string) *plainDateSlots {
	s, ok := engine.TemporalData(this).(*plainDateSlots)
	if !ok {
		incompatible(vm, "Temporal.PlainDate.prototype."+name, this)
	}
	return s
}

func thisPlainTime(vm *engine.VM, this engine.Value, name string) *plainTimeSlots {
	s, ok := engine.TemporalData(this).(*plainTimeSlots)
	if !ok {
		incompatible(vm, "Temporal.PlainTime.prototype."+name, this)
	}
	return s
}

func thisPlainDateTime(vm *engine.VM, this engine.Value, name string) *plainDateTimeSlots {
	s, ok := engine.TemporalData(this).(*plainDateTimeSlots)
	if !ok {
		incompatible(vm, "Temporal.PlainDateTime.prototype."+name, this)
	}
	return s
}

// toTemporalDate implements ToTemporalDate.
func toTemporalDate(vm *engine.VM, item, options engine.Value) plainDateSlots {
	if item.IsObject() {
		switch s := engine.TemporalData(item).(type) {
		case *plainDateSlots:
			getOverflowOption(vm, getOptionsObject(vm, options))
			return *s
		case *zonedDateTimeSlots:
			dt := s.tz.ISODateTimeFor(s.ns)
			getOverflowOption(vm, getOptionsObject(vm, options))
			return plainDateSlots{
				date: dt.Date,
				cal:  s.cal,
			}
		case *plainDateTimeSlots:
			getOverflowOption(vm, getOptionsObject(vm, options))
			return plainDateSlots{
				date: s.dt.Date,
				cal:  s.cal,
			}
		}
		o := item.Object()
		cal := calendarWithISODefault(vm, o)
		f := prepareCalendarFields(vm, cal, o, dateFieldNames, fieldsNoneRequired)
		overflow := getOverflowOption(vm, getOptionsObject(vm, options))
		d := must(temporal.CalendarDateFromFields(cal, f, overflow))
		return plainDateSlots{
			date: d,
			cal:  cal,
		}
	}
	str := requireString(vm, item, "date")
	p := must(temporal.ParseISODateTime(str, temporal.GoalDateTime))
	cal := canonicalizeParsedCalendar(vm, p.Calendar)
	getOverflowOption(vm, getOptionsObject(vm, options))
	d := temporal.ISODate{Year: p.Year, Month: p.Month, Day: p.Day}
	if !temporal.ISODateWithinLimits(d) {
		vm.ThrowRangeError("date is out of range")
	}
	return plainDateSlots{
		date: d,
		cal:  cal,
	}
}

// toTemporalDateTime implements ToTemporalDateTime.
func toTemporalDateTime(vm *engine.VM, item, options engine.Value) plainDateTimeSlots {
	if item.IsObject() {
		switch s := engine.TemporalData(item).(type) {
		case *plainDateTimeSlots:
			getOverflowOption(vm, getOptionsObject(vm, options))
			return *s
		case *zonedDateTimeSlots:
			dt := s.tz.ISODateTimeFor(s.ns)
			getOverflowOption(vm, getOptionsObject(vm, options))
			return plainDateTimeSlots{
				dt:  dt,
				cal: s.cal,
			}
		case *plainDateSlots:
			getOverflowOption(vm, getOptionsObject(vm, options))
			return plainDateTimeSlots{
				dt: temporal.ISODateTime{
					Date: s.date,
				},
				cal: s.cal,
			}
		}
		o := item.Object()
		cal := calendarWithISODefault(vm, o)
		f := prepareCalendarFields(vm, cal, o, append(append([]string{}, dateFieldNames...), timeFieldNames...), fieldsNoneRequired)
		overflow := getOverflowOption(vm, getOptionsObject(vm, options))
		dt := interpretDateTimeFields(vm, cal, f, overflow)
		if !temporal.ISODateTimeWithinLimits(dt) {
			vm.ThrowRangeError("date-time is out of range")
		}
		return plainDateTimeSlots{
			dt:  dt,
			cal: cal,
		}
	}
	str := requireString(vm, item, "date-time")
	p := must(temporal.ParseISODateTime(str, temporal.GoalDateTime))
	cal := canonicalizeParsedCalendar(vm, p.Calendar)
	getOverflowOption(vm, getOptionsObject(vm, options))
	dt := temporal.ISODateTime{
		Date: temporal.ISODate{Year: p.Year, Month: p.Month, Day: p.Day},
		Time: p.Time,
	}
	if !temporal.ISODateTimeWithinLimits(dt) {
		vm.ThrowRangeError("date-time is out of range")
	}
	return plainDateTimeSlots{
		dt:  dt,
		cal: cal,
	}
}

// toTemporalTime implements ToTemporalTime.
func toTemporalTime(vm *engine.VM, item, options engine.Value) temporal.Time {
	if item.IsObject() {
		switch s := engine.TemporalData(item).(type) {
		case *plainTimeSlots:
			getOverflowOption(vm, getOptionsObject(vm, options))
			return s.t
		case *plainDateTimeSlots:
			getOverflowOption(vm, getOptionsObject(vm, options))
			return s.dt.Time
		case *zonedDateTimeSlots:
			dt := s.tz.ISODateTimeFor(s.ns)
			getOverflowOption(vm, getOptionsObject(vm, options))
			return dt.Time
		}
		tr := toTemporalTimeRecord(vm, item.Object(), false)
		overflow := getOverflowOption(vm, getOptionsObject(vm, options))
		v := tr.values
		return must(temporal.RegulateTime(v[0], v[1], v[2], v[3], v[4], v[5], overflow))
	}
	str := requireString(vm, item, "time")
	p := must(temporal.ParseISODateTime(str, temporal.GoalTime))
	getOverflowOption(vm, getOptionsObject(vm, options))
	return p.Time
}

// toTimeRecordOrMidnight implements ToTimeRecordOrMidnight.
func toTimeRecordOrMidnight(vm *engine.VM, item engine.Value) temporal.Time {
	if item.IsUndefined() {
		return temporal.MidnightTime
	}
	return toTemporalTime(vm, item, engine.Undefined)
}

// defineCalendarGetters defines the getters of the calendar fields names on proto. dateOf
// returns the ISO date and the calendar of a receiver, or throws a TypeError.
func defineCalendarGetters(vm *engine.VM, proto *engine.Object, names []string, dateOf func(vm *engine.VM, this engine.Value, name string) (temporal.ISODate, string)) {
	for _, name := range names {
		tgetter(vm, proto, vm.Intern(name), name, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			d, cal := dateOf(vm, this, name)
			if name == "calendarId" {
				return vm.NewString(cal)
			}
			c := temporal.CalendarISOToDate(cal, d)
			switch name {
			case "era":
				if c.Era == "" {
					return engine.Undefined
				}
				return vm.NewString(c.Era)
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
				return vm.NewString(c.MonthCode)
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
func checkSameCalendar(vm *engine.VM, a, b string) {
	if !calendarEquals(a, b) {
		vm.ThrowRangeError("the calendars " + a + " and " + b + " differ")
	}
}

// toZonedDateTimeArgs reads the argument of PlainDate.prototype.toZonedDateTime.
func toZonedDateTimeArgs(vm *engine.VM, item engine.Value) (temporal.TimeZone, engine.Value) {
	if item.IsObject() {
		o := item.Object()
		tzLike := get(vm, o, "timeZone")
		if tzLike.IsUndefined() {
			return toTimeZone(vm, item), engine.Undefined
		}
		tz := toTimeZone(vm, tzLike)
		return tz, get(vm, o, "plainTime")
	}
	return toTimeZone(vm, item), engine.Undefined
}

func initTemporalPlainDate(vm *engine.VM, ns *engine.Object, r *temporalRealm) {
	proto := r.plainDate
	ctor := temporalConstructor(vm, ns, "PlainDate", 3, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(vm, "Temporal.PlainDate")
		y := toIntegerWithTruncation(vm, arg(args, 0), "year")
		m := toIntegerWithTruncation(vm, arg(args, 1), "month")
		d := toIntegerWithTruncation(vm, arg(args, 2), "day")
		cal := temporal.ISO8601
		if c := arg(args, 3); !c.IsUndefined() {
			cal = must(temporal.CanonicalizeCalendar(requireString(vm, c, "calendar")))
		}
		date := must(temporal.CreateISODateRecord(y, m, d))
		return r.createPlainDate(vm, date, cal, nt)
	})
	tmethod(vm, ctor, "from", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := toTemporalDate(vm, arg(args, 0), arg(args, 1))
		return r.createPlainDate(vm, s.date, s.cal, nil)
	})
	tmethod(vm, ctor, "compare", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		one := toTemporalDate(vm, arg(args, 0), engine.Undefined)
		two := toTemporalDate(vm, arg(args, 1), engine.Undefined)
		return compareResult(temporal.CompareISODate(one.date, two.date))
	})

	defineCalendarGetters(vm, proto, fullDateGetters, func(vm *engine.VM, this engine.Value, name string) (temporal.ISODate, string) {
		s := thisPlainDate(vm, this, name)
		return s.date, s.cal
	})
	tmethod(vm, proto, "toPlainYearMonth", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(vm, this, "toPlainYearMonth")
		f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsDate)
		return r.createPlainYearMonth(vm, must(temporal.CalendarYearMonthFromFields(s.cal, f, temporal.OverflowConstrain)), s.cal, nil)
	})
	tmethod(vm, proto, "toPlainMonthDay", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(vm, this, "toPlainMonthDay")
		f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsDate)
		return r.createPlainMonthDay(vm, must(temporal.CalendarMonthDayFromFields(s.cal, f, temporal.OverflowConstrain)), s.cal, nil)
	})
	tmethod(vm, proto, "toPlainDateTime", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(vm, this, "toPlainDateTime")
		t := toTimeRecordOrMidnight(vm, arg(args, 0))
		return r.createPlainDateTime(vm, temporal.ISODateTime{
			Date: s.date,
			Time: t,
		}, s.cal, nil)
	})
	tmethod(vm, proto, "toZonedDateTime", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(vm, this, "toZonedDateTime")
		tz, plainTime := toZonedDateTimeArgs(vm, arg(args, 0))
		var epochNs temporal.Int128
		if plainTime.IsUndefined() {
			epochNs = must(tz.StartOfDay(s.date))
		} else {
			t := toTemporalTime(vm, plainTime, engine.Undefined)
			epochNs = must(tz.EpochNanosecondsFor(temporal.ISODateTime{
				Date: s.date,
				Time: t,
			}, temporal.DisambiguationCompatible))
		}
		return r.newZonedDateTime(vm, epochNs, tz, s.cal, nil)
	})
	tmethod(vm, proto, "with", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(vm, this, "with")
		bag := rejectTemporalLikeObject(vm, arg(args, 0))
		f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsDate)
		partial := prepareCalendarFields(vm, s.cal, bag, dateFieldNames, fieldsPartial)
		f = temporal.CalendarMergeFields(s.cal, f, partial)
		overflow := getOverflowOption(vm, getOptionsObject(vm, arg(args, 1)))
		return r.createPlainDate(vm, must(temporal.CalendarDateFromFields(s.cal, f, overflow)), s.cal, nil)
	})
	tmethod(vm, proto, "withCalendar", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(vm, this, "withCalendar")
		cal := toCalendar(vm, arg(args, 0))
		return r.createPlainDate(vm, s.date, cal, nil)
	})
	for _, name := range []string{"add", "subtract"} {
		tmethod(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainDate(vm, this, name)
			d := toTemporalDuration(vm, arg(args, 0))
			if name == "subtract" {
				d = d.Negated()
			}
			dd := must(temporal.ToDateDurationRecordWithoutTime(&d))
			overflow := getOverflowOption(vm, getOptionsObject(vm, arg(args, 1)))
			return r.createPlainDate(vm, must(temporal.CalendarDateAdd(s.cal, s.date, dd, overflow)), s.cal, nil)
		})
	}
	for _, name := range []string{"until", "since"} {
		tmethod(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainDate(vm, this, name)
			other := toTemporalDate(vm, arg(args, 0), engine.Undefined)
			checkSameCalendar(vm, s.cal, other.cal)
			opts := getOptionsObject(vm, arg(args, 1))
			settings := getDifferenceSettings(vm, name == "since", opts, unitGroupDate, nil, temporal.UnitDay, temporal.UnitDay)
			if temporal.CompareISODate(s.date, other.date) == 0 {
				return r.newDuration(vm, temporal.Duration{}, nil)
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
			return r.newDuration(vm, d, nil)
		})
	}
	tmethod(vm, proto, "equals", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(vm, this, "equals")
		other := toTemporalDate(vm, arg(args, 0), engine.Undefined)
		return engine.Bool(temporal.CompareISODate(s.date, other.date) == 0 && calendarEquals(s.cal, other.cal))
	})
	tmethod(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDate(vm, this, "toString")
		show := getShowCalendarOption(vm, getOptionsObject(vm, arg(args, 0)))
		return vm.NewString(temporal.TemporalDateToString(s.date, s.cal, show))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainDate(vm, this, name)
			return vm.NewString(temporal.TemporalDateToString(s.date, s.cal, temporal.ShowCalendarAuto))
		})
	}
	valueOfThrows(vm, proto, "PlainDate")
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
func toStringRounding(vm *engine.VM, opts *engine.Object) (temporal.Precision, temporal.RoundingMode, temporal.Unit) {
	digits := getFractionalSecondDigitsOption(vm, opts)
	mode := getRoundingModeOption(vm, opts, temporal.RoundTrunc)
	unit := getUnitOption(vm, opts, "smallestUnit", false)
	return digits, mode, unit
}

// checkToStringUnit validates the smallestUnit of a toString method.
func checkToStringUnit(vm *engine.VM, unit temporal.Unit) {
	validateUnit(vm, unit, unitGroupTime)
	if unit == temporal.UnitHour {
		vm.ThrowRangeError("smallestUnit must not be hour")
	}
}

// roundOptions reads the options of the round methods of PlainTime, PlainDateTime and
// ZonedDateTime. withDay allows the day unit.
func roundOptions(vm *engine.VM, roundTo engine.Value, withDay bool) (int64, temporal.RoundingMode, temporal.Unit) {
	opts := roundToOptions(vm, roundTo, "smallestUnit")
	increment := getRoundingIncrementOption(vm, opts)
	mode := getRoundingModeOption(vm, opts, temporal.RoundHalfExpand)
	unit := getUnitOption(vm, opts, "smallestUnit", true)
	if withDay {
		validateUnit(vm, unit, unitGroupTime, temporal.UnitDay)
	} else {
		validateUnit(vm, unit, unitGroupTime)
	}
	maximum := maximumTimeIncrements[unit]
	check(temporal.ValidateTemporalRoundingIncrement(increment, maximum, maximum == 1))
	return increment, mode, unit
}

func initTemporalPlainTime(vm *engine.VM, ns *engine.Object, r *temporalRealm) {
	proto := r.plainTime
	ctor := temporalConstructor(vm, ns, "PlainTime", 0, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(vm, "Temporal.PlainTime")
		var v [6]float64
		for i := range v {
			if a := arg(args, i); !a.IsUndefined() {
				v[i] = toIntegerWithTruncation(vm, a, timeGetterNames[i])
			}
		}
		t := must(temporal.CreateTimeRecord(v[0], v[1], v[2], v[3], v[4], v[5]))
		return r.newPlainTime(vm, t, nt)
	})
	tmethod(vm, ctor, "from", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return r.newPlainTime(vm, toTemporalTime(vm, arg(args, 0), arg(args, 1)), nil)
	})
	tmethod(vm, ctor, "compare", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		one := toTemporalTime(vm, arg(args, 0), engine.Undefined)
		two := toTemporalTime(vm, arg(args, 1), engine.Undefined)
		return compareResult(temporal.CompareTimeRecord(one, two))
	})
	for _, name := range timeGetterNames {
		tgetter(vm, proto, vm.Intern(name), name, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			return engine.Number(float64(timeField(thisPlainTime(vm, this, name).t, name)))
		})
	}
	tmethod(vm, proto, "with", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainTime(vm, this, "with")
		bag := rejectTemporalLikeObject(vm, arg(args, 0))
		partial := toTemporalTimeRecord(vm, bag, true)
		t := s.t
		v := [6]float64{float64(t.Hour), float64(t.Minute), float64(t.Second), float64(t.Millisecond), float64(t.Microsecond), float64(t.Nanosecond)}
		for i := range v {
			if partial.has[i] {
				v[i] = partial.values[i]
			}
		}
		overflow := getOverflowOption(vm, getOptionsObject(vm, arg(args, 1)))
		return r.newPlainTime(vm, must(temporal.RegulateTime(v[0], v[1], v[2], v[3], v[4], v[5], overflow)), nil)
	})
	for _, name := range []string{"add", "subtract"} {
		tmethod(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainTime(vm, this, name)
			d := toTemporalDuration(vm, arg(args, 0))
			if name == "subtract" {
				d = d.Negated()
			}
			id := temporal.ToInternalDurationRecordWith24HourDays(&d)
			t, _ := temporal.AddTime(s.t, id.Time)
			return r.newPlainTime(vm, t, nil)
		})
	}
	for _, name := range []string{"until", "since"} {
		tmethod(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainTime(vm, this, name)
			other := toTemporalTime(vm, arg(args, 0), engine.Undefined)
			opts := getOptionsObject(vm, arg(args, 1))
			settings := getDifferenceSettings(vm, name == "since", opts, unitGroupTime, nil, temporal.UnitNanosecond, temporal.UnitHour)
			t := must(temporal.RoundTimeDuration(temporal.DifferenceTime(s.t, other), settings.increment, settings.smallestUnit, settings.mode))
			d := must(temporal.TemporalDurationFromInternal(temporal.CombineDateAndTimeDuration(temporal.DateDuration{}, t), settings.largestUnit))
			if name == "since" {
				d = d.Negated()
			}
			return r.newDuration(vm, d, nil)
		})
	}
	tmethod(vm, proto, "round", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainTime(vm, this, "round")
		increment, mode, unit := roundOptions(vm, arg(args, 0), false)
		t, _ := temporal.RoundTime(s.t, increment, unit, mode)
		return r.newPlainTime(vm, t, nil)
	})
	tmethod(vm, proto, "equals", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainTime(vm, this, "equals")
		return engine.Bool(temporal.CompareTimeRecord(s.t, toTemporalTime(vm, arg(args, 0), engine.Undefined)) == 0)
	})
	tmethod(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainTime(vm, this, "toString")
		digits, mode, unit := toStringRounding(vm, getOptionsObject(vm, arg(args, 0)))
		checkToStringUnit(vm, unit)
		p := temporal.ToSecondsStringPrecisionRecord(unit, digits)
		t, _ := temporal.RoundTime(s.t, p.Increment, p.Unit, mode)
		return vm.NewString(temporal.TimeRecordToString(t, p.Precision))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			return vm.NewString(temporal.TimeRecordToString(thisPlainTime(vm, this, name).t, temporal.PrecisionAuto))
		})
	}
	valueOfThrows(vm, proto, "PlainTime")
}

func initTemporalPlainDateTime(vm *engine.VM, ns *engine.Object, r *temporalRealm) {
	proto := r.plainDateTime
	ctor := temporalConstructor(vm, ns, "PlainDateTime", 3, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(vm, "Temporal.PlainDateTime")
		y := toIntegerWithTruncation(vm, arg(args, 0), "year")
		m := toIntegerWithTruncation(vm, arg(args, 1), "month")
		d := toIntegerWithTruncation(vm, arg(args, 2), "day")
		var v [6]float64
		for i := range v {
			if a := arg(args, 3+i); !a.IsUndefined() {
				v[i] = toIntegerWithTruncation(vm, a, timeGetterNames[i])
			}
		}
		cal := temporal.ISO8601
		if c := arg(args, 9); !c.IsUndefined() {
			cal = must(temporal.CanonicalizeCalendar(requireString(vm, c, "calendar")))
		}
		date := must(temporal.CreateISODateRecord(y, m, d))
		t := must(temporal.CreateTimeRecord(v[0], v[1], v[2], v[3], v[4], v[5]))
		return r.createPlainDateTime(vm, temporal.ISODateTime{
			Date: date,
			Time: t,
		}, cal, nt)
	})
	tmethod(vm, ctor, "from", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := toTemporalDateTime(vm, arg(args, 0), arg(args, 1))
		return r.createPlainDateTime(vm, s.dt, s.cal, nil)
	})
	tmethod(vm, ctor, "compare", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		one := toTemporalDateTime(vm, arg(args, 0), engine.Undefined)
		two := toTemporalDateTime(vm, arg(args, 1), engine.Undefined)
		return compareResult(temporal.CompareISODateTime(one.dt, two.dt))
	})

	defineCalendarGetters(vm, proto, fullDateGetters, func(vm *engine.VM, this engine.Value, name string) (temporal.ISODate, string) {
		s := thisPlainDateTime(vm, this, name)
		return s.dt.Date, s.cal
	})
	for _, name := range timeGetterNames {
		tgetter(vm, proto, vm.Intern(name), name, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			return engine.Number(float64(timeField(thisPlainDateTime(vm, this, name).dt.Time, name)))
		})
	}
	tmethod(vm, proto, "with", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(vm, this, "with")
		bag := rejectTemporalLikeObject(vm, arg(args, 0))
		f := temporal.ISODateToFields(s.cal, s.dt.Date, temporal.FieldsDate)
		f.SetTime(s.dt.Time)
		partial := prepareCalendarFields(vm, s.cal, bag, append(append([]string{}, dateFieldNames...), timeFieldNames...), fieldsPartial)
		f = temporal.CalendarMergeFields(s.cal, f, partial)
		overflow := getOverflowOption(vm, getOptionsObject(vm, arg(args, 1)))
		return r.createPlainDateTime(vm, interpretDateTimeFields(vm, s.cal, f, overflow), s.cal, nil)
	})
	tmethod(vm, proto, "withPlainTime", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(vm, this, "withPlainTime")
		t := toTimeRecordOrMidnight(vm, arg(args, 0))
		return r.createPlainDateTime(vm, temporal.ISODateTime{
			Date: s.dt.Date,
			Time: t,
		}, s.cal, nil)
	})
	tmethod(vm, proto, "withCalendar", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(vm, this, "withCalendar")
		cal := toCalendar(vm, arg(args, 0))
		return r.createPlainDateTime(vm, s.dt, cal, nil)
	})
	for _, name := range []string{"add", "subtract"} {
		tmethod(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainDateTime(vm, this, name)
			d := toTemporalDuration(vm, arg(args, 0))
			if name == "subtract" {
				d = d.Negated()
			}
			overflow := getOverflowOption(vm, getOptionsObject(vm, arg(args, 1)))
			id := temporal.ToInternalDurationRecordWith24HourDays(&d)
			t, days := temporal.AddTime(s.dt.Time, id.Time)
			dd := must(temporal.AdjustDateDurationRecord(id.Date, days, -1<<63, -1<<63))
			date := must(temporal.CalendarDateAdd(s.cal, s.dt.Date, dd, overflow))
			return r.createPlainDateTime(vm, temporal.ISODateTime{
				Date: date,
				Time: t,
			}, s.cal, nil)
		})
	}
	for _, name := range []string{"until", "since"} {
		tmethod(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainDateTime(vm, this, name)
			other := toTemporalDateTime(vm, arg(args, 0), engine.Undefined)
			checkSameCalendar(vm, s.cal, other.cal)
			opts := getOptionsObject(vm, arg(args, 1))
			settings := getDifferenceSettings(vm, name == "since", opts, unitGroupDateTime, nil, temporal.UnitNanosecond, temporal.UnitDay)
			if temporal.CompareISODateTime(s.dt, other.dt) == 0 {
				return r.newDuration(vm, temporal.Duration{}, nil)
			}
			id := must(temporal.DifferencePlainDateTimeWithRounding(s.dt, other.dt, s.cal, settings.largestUnit, settings.increment, settings.smallestUnit, settings.mode))
			d := must(temporal.TemporalDurationFromInternal(id, settings.largestUnit))
			if name == "since" {
				d = d.Negated()
			}
			return r.newDuration(vm, d, nil)
		})
	}
	tmethod(vm, proto, "round", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(vm, this, "round")
		increment, mode, unit := roundOptions(vm, arg(args, 0), true)
		if unit == temporal.UnitNanosecond && increment == 1 {
			return r.createPlainDateTime(vm, s.dt, s.cal, nil)
		}
		return r.createPlainDateTime(vm, temporal.RoundISODateTime(s.dt, increment, unit, mode), s.cal, nil)
	})
	tmethod(vm, proto, "equals", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(vm, this, "equals")
		other := toTemporalDateTime(vm, arg(args, 0), engine.Undefined)
		return engine.Bool(temporal.CompareISODateTime(s.dt, other.dt) == 0 && calendarEquals(s.cal, other.cal))
	})
	tmethod(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(vm, this, "toString")
		opts := getOptionsObject(vm, arg(args, 0))
		show := getShowCalendarOption(vm, opts)
		digits, mode, unit := toStringRounding(vm, opts)
		checkToStringUnit(vm, unit)
		p := temporal.ToSecondsStringPrecisionRecord(unit, digits)
		dt := temporal.RoundISODateTime(s.dt, p.Increment, p.Unit, mode)
		if !temporal.ISODateTimeWithinLimits(dt) {
			vm.ThrowRangeError("date-time is out of range")
		}
		return vm.NewString(temporal.ISODateTimeToString(dt, s.cal, p.Precision, show))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainDateTime(vm, this, name)
			return vm.NewString(temporal.ISODateTimeToString(s.dt, s.cal, temporal.PrecisionAuto, temporal.ShowCalendarAuto))
		})
	}
	valueOfThrows(vm, proto, "PlainDateTime")
	tmethod(vm, proto, "toZonedDateTime", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(vm, this, "toZonedDateTime")
		tz := toTimeZone(vm, arg(args, 0))
		d := getDisambiguationOption(vm, getOptionsObject(vm, arg(args, 1)))
		epochNs := must(tz.EpochNanosecondsFor(s.dt, d))
		return r.newZonedDateTime(vm, epochNs, tz, s.cal, nil)
	})
	tmethod(vm, proto, "toPlainDate", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(vm, this, "toPlainDate")
		return r.createPlainDate(vm, s.dt.Date, s.cal, nil)
	})
	tmethod(vm, proto, "toPlainTime", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainDateTime(vm, this, "toPlainTime")
		return r.newPlainTime(vm, s.dt.Time, nil)
	})
}
