// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/temporal"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// createPlainYearMonth implements CreateTemporalYearMonth.
func createPlainYearMonth(cx *engine.ExecutionContext, d temporal.ISODate, cal string, nt *engine.Object) engine.Value {
	if !temporal.ISOYearMonthWithinLimits(d) {
		cx.ThrowRangeError("year and month are out of range")
	}
	proto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicTemporalPlainYearMonthPrototype)
	return engine.ObjectValue(cx.NewTemporalObject(proto, &plainYearMonthSlots{
		date: d,
		cal:  cal,
	}))
}

// createPlainMonthDay implements CreateTemporalMonthDay.
func createPlainMonthDay(cx *engine.ExecutionContext, d temporal.ISODate, cal string, nt *engine.Object) engine.Value {
	if !temporal.ISODateWithinLimits(d) {
		cx.ThrowRangeError("date is out of range")
	}
	proto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicTemporalPlainMonthDayPrototype)
	return engine.ObjectValue(cx.NewTemporalObject(proto, &plainMonthDaySlots{
		date: d,
		cal:  cal,
	}))
}

func thisPlainYearMonth(cx *engine.ExecutionContext, this engine.Value, name string) *plainYearMonthSlots {
	s, ok := engine.TemporalData(this).(*plainYearMonthSlots)
	if !ok {
		incompatible(cx, "Temporal.PlainYearMonth.prototype."+name, this)
	}
	return s
}

func thisPlainMonthDay(cx *engine.ExecutionContext, this engine.Value, name string) *plainMonthDaySlots {
	s, ok := engine.TemporalData(this).(*plainMonthDaySlots)
	if !ok {
		incompatible(cx, "Temporal.PlainMonthDay.prototype."+name, this)
	}
	return s
}

// toTemporalYearMonth implements ToTemporalYearMonth.
func toTemporalYearMonth(cx *engine.ExecutionContext, item, options engine.Value) plainYearMonthSlots {
	if item.IsObject() {
		if s, ok := engine.TemporalData(item).(*plainYearMonthSlots); ok {
			getOverflowOption(cx, getOptionsObject(cx, options))
			return *s
		}
		o := item.Object()
		cal := calendarWithISODefault(cx, o)
		f := prepareCalendarFields(cx, cal, o, yearMonthFieldNames, fieldsNoneRequired)
		overflow := getOverflowOption(cx, getOptionsObject(cx, options))
		d := must(temporal.CalendarYearMonthFromFields(cal, f, overflow))
		return plainYearMonthSlots{
			date: d,
			cal:  cal,
		}
	}
	str := requireString(cx, item, "year-month")
	p := must(temporal.ParseISODateTime(str, cx.Poll, temporal.GoalYearMonth))
	cal := canonicalizeParsedCalendar(cx, p.Calendar)
	getOverflowOption(cx, getOptionsObject(cx, options))
	d := temporal.ISODate{Year: p.Year, Month: p.Month, Day: p.Day}
	f := temporal.ISODateToFields(cal, d, temporal.FieldsYearMonth)
	d = must(temporal.CalendarYearMonthFromFields(cal, f, temporal.OverflowConstrain))
	return plainYearMonthSlots{
		date: d,
		cal:  cal,
	}
}

// toTemporalMonthDay implements ToTemporalMonthDay.
func toTemporalMonthDay(cx *engine.ExecutionContext, item, options engine.Value) plainMonthDaySlots {
	if item.IsObject() {
		if s, ok := engine.TemporalData(item).(*plainMonthDaySlots); ok {
			getOverflowOption(cx, getOptionsObject(cx, options))
			return *s
		}
		o := item.Object()
		cal := calendarWithISODefault(cx, o)
		f := prepareCalendarFields(cx, cal, o, dateFieldNames, fieldsNoneRequired)
		overflow := getOverflowOption(cx, getOptionsObject(cx, options))
		d := must(temporal.CalendarMonthDayFromFields(cal, f, overflow))
		return plainMonthDaySlots{
			date: d,
			cal:  cal,
		}
	}
	str := requireString(cx, item, "month-day")
	p := must(temporal.ParseISODateTime(str, cx.Poll, temporal.GoalMonthDay))
	cal := canonicalizeParsedCalendar(cx, p.Calendar)
	getOverflowOption(cx, getOptionsObject(cx, options))
	if cal == temporal.ISO8601 {
		return plainMonthDaySlots{
			date: temporal.ISODate{Year: 1972, Month: p.Month, Day: p.Day},
			cal:  cal,
		}
	}
	d := temporal.ISODate{Year: p.Year, Month: p.Month, Day: p.Day}
	if !temporal.ISODateWithinLimits(d) {
		cx.ThrowRangeError("date is out of range")
	}
	f := temporal.ISODateToFields(cal, d, temporal.FieldsMonthDay)
	return plainMonthDaySlots{
		date: must(temporal.CalendarMonthDayFromFields(cal, f, temporal.OverflowConstrain)),
		cal:  cal,
	}
}

func initTemporalPlainYearMonth(cx *engine.ExecutionContext, ns *engine.Object) {
	ctor, proto := temporalConstructor(cx, ns, "PlainYearMonth", 2, engine.IntrinsicTemporalPlainYearMonthPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(cx, "Temporal.PlainYearMonth")
		y := toIntegerWithTruncation(cx, arg(args, 0), "year")
		m := toIntegerWithTruncation(cx, arg(args, 1), "month")
		cal := temporal.ISO8601
		if c := arg(args, 2); !c.IsUndefined() {
			cal = must(temporal.CanonicalizeCalendar(requireString(cx, c, "calendar")))
		}
		day := 1.0
		if d := arg(args, 3); !d.IsUndefined() {
			day = toIntegerWithTruncation(cx, d, "referenceISODay")
		}
		date := must(temporal.CreateISODateRecord(y, m, day))
		return createPlainYearMonth(cx, date, cal, nt)
	})
	tmethod(cx, ctor, "from", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := toTemporalYearMonth(cx, arg(args, 0), arg(args, 1))
		return createPlainYearMonth(cx, s.date, s.cal, nil)
	})
	tmethod(cx, ctor, "compare", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		one := toTemporalYearMonth(cx, arg(args, 0), engine.Undefined)
		two := toTemporalYearMonth(cx, arg(args, 1), engine.Undefined)
		return compareResult(temporal.CompareISODate(one.date, two.date))
	})
	defineCalendarGetters(cx, proto, []string{"calendarId", "era", "eraYear", "year", "month", "monthCode", "daysInMonth", "daysInYear", "monthsInYear", "inLeapYear"},
		func(cx *engine.ExecutionContext, this engine.Value, name string) (temporal.ISODate, string) {
			s := thisPlainYearMonth(cx, this, name)
			return s.date, s.cal
		})
	tmethod(cx, proto, "with", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainYearMonth(cx, this, "with")
		bag := rejectTemporalLikeObject(cx, arg(args, 0))
		f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsYearMonth)
		partial := prepareCalendarFields(cx, s.cal, bag, yearMonthFieldNames, fieldsPartial)
		f = temporal.CalendarMergeFields(s.cal, f, partial)
		overflow := getOverflowOption(cx, getOptionsObject(cx, arg(args, 1)))
		return createPlainYearMonth(cx, must(temporal.CalendarYearMonthFromFields(s.cal, f, overflow)), s.cal, nil)
	})
	for _, name := range []string{"add", "subtract"} {
		tmethod(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainYearMonth(cx, this, name)
			d := toTemporalDuration(cx, arg(args, 0))
			if name == "subtract" {
				d = d.Negated()
			}
			id := temporal.ToInternalDurationRecord(&d)
			overflow := getOverflowOption(cx, getOptionsObject(cx, arg(args, 1)))
			if id.Date.Weeks != 0 || id.Date.Days != 0 || !id.Time.IsZero() {
				cx.ThrowRangeError("only years and months can be added to a PlainYearMonth")
			}
			f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsYearMonth)
			f.Day, f.HasDay = 1, true
			date := must(temporal.CalendarDateFromFields(s.cal, f, temporal.OverflowConstrain))
			added := must(temporal.CalendarDateAdd(s.cal, date, id.Date, overflow))
			af := temporal.ISODateToFields(s.cal, added, temporal.FieldsYearMonth)
			return createPlainYearMonth(cx, must(temporal.CalendarYearMonthFromFields(s.cal, af, overflow)), s.cal, nil)
		})
	}
	for _, name := range []string{"until", "since"} {
		tmethod(cx, proto, wtf8.String(name), 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainYearMonth(cx, this, name)
			other := toTemporalYearMonth(cx, arg(args, 0), engine.Undefined)
			checkSameCalendar(cx, s.cal, other.cal)
			opts := getOptionsObject(cx, arg(args, 1))
			settings := getDifferenceSettings(cx, name == "since", opts, unitGroupDate, []temporal.Unit{temporal.UnitWeek, temporal.UnitDay}, temporal.UnitMonth, temporal.UnitYear)
			if temporal.CompareISODate(s.date, other.date) == 0 {
				return newDuration(cx, temporal.Duration{}, nil)
			}
			tf := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsYearMonth)
			tf.Day, tf.HasDay = 1, true
			thisDate := must(temporal.CalendarDateFromFields(s.cal, tf, temporal.OverflowConstrain))
			of := temporal.ISODateToFields(s.cal, other.date, temporal.FieldsYearMonth)
			of.Day, of.HasDay = 1, true
			otherDate := must(temporal.CalendarDateFromFields(s.cal, of, temporal.OverflowConstrain))
			dd := temporal.CalendarDateUntil(s.cal, thisDate, otherDate, settings.largestUnit)
			dd = must(temporal.AdjustDateDurationRecord(dd, 0, 0, -1<<63))
			id := temporal.InternalDuration{
				Date: dd,
			}
			if settings.smallestUnit != temporal.UnitMonth || settings.increment != 1 {
				from := temporal.ISODateTime{
					Date: thisDate,
				}
				to := temporal.ISODateTime{
					Date: otherDate,
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
		s := thisPlainYearMonth(cx, this, "equals")
		other := toTemporalYearMonth(cx, arg(args, 0), engine.Undefined)
		return engine.Bool(temporal.CompareISODate(s.date, other.date) == 0 && calendarEquals(s.cal, other.cal))
	})
	tmethod(cx, proto, "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainYearMonth(cx, this, "toString")
		show := getShowCalendarOption(cx, getOptionsObject(cx, arg(args, 0)))
		return cx.NewString(wtf8.String(temporal.TemporalYearMonthToString(s.date, s.cal, show)))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainYearMonth(cx, this, name)
			return cx.NewString(wtf8.String(temporal.TemporalYearMonthToString(s.date, s.cal, temporal.ShowCalendarAuto)))
		})
	}
	valueOfThrows(cx, proto, "PlainYearMonth")
	tmethod(cx, proto, "toPlainDate", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainYearMonth(cx, this, "toPlainDate")
		item := arg(args, 0)
		if !item.IsObject() {
			cx.ThrowTypeError("argument must be an object")
		}
		f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsYearMonth)
		in := prepareCalendarFields(cx, s.cal, item.Object(), []string{"day"}, fieldsNoneRequired)
		f = temporal.CalendarMergeFields(s.cal, f, in)
		return createPlainDate(cx, must(temporal.CalendarDateFromFields(s.cal, f, temporal.OverflowConstrain)), s.cal, nil)
	})
}

func initTemporalPlainMonthDay(cx *engine.ExecutionContext, ns *engine.Object) {
	ctor, proto := temporalConstructor(cx, ns, "PlainMonthDay", 2, engine.IntrinsicTemporalPlainMonthDayPrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(cx, "Temporal.PlainMonthDay")
		m := toIntegerWithTruncation(cx, arg(args, 0), "month")
		d := toIntegerWithTruncation(cx, arg(args, 1), "day")
		cal := temporal.ISO8601
		if c := arg(args, 2); !c.IsUndefined() {
			cal = must(temporal.CanonicalizeCalendar(requireString(cx, c, "calendar")))
		}
		year := 1972.0
		if y := arg(args, 3); !y.IsUndefined() {
			year = toIntegerWithTruncation(cx, y, "referenceISOYear")
		}
		date := must(temporal.CreateISODateRecord(year, m, d))
		return createPlainMonthDay(cx, date, cal, nt)
	})
	tmethod(cx, ctor, "from", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := toTemporalMonthDay(cx, arg(args, 0), arg(args, 1))
		return createPlainMonthDay(cx, s.date, s.cal, nil)
	})
	defineCalendarGetters(cx, proto, []string{"calendarId", "monthCode", "day"}, func(cx *engine.ExecutionContext, this engine.Value, name string) (temporal.ISODate, string) {
		s := thisPlainMonthDay(cx, this, name)
		return s.date, s.cal
	})
	tmethod(cx, proto, "with", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainMonthDay(cx, this, "with")
		bag := rejectTemporalLikeObject(cx, arg(args, 0))
		f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsMonthDay)
		partial := prepareCalendarFields(cx, s.cal, bag, dateFieldNames, fieldsPartial)
		f = temporal.CalendarMergeFields(s.cal, f, partial)
		overflow := getOverflowOption(cx, getOptionsObject(cx, arg(args, 1)))
		return createPlainMonthDay(cx, must(temporal.CalendarMonthDayFromFields(s.cal, f, overflow)), s.cal, nil)
	})
	tmethod(cx, proto, "equals", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainMonthDay(cx, this, "equals")
		other := toTemporalMonthDay(cx, arg(args, 0), engine.Undefined)
		return engine.Bool(temporal.CompareISODate(s.date, other.date) == 0 && calendarEquals(s.cal, other.cal))
	})
	tmethod(cx, proto, "toString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainMonthDay(cx, this, "toString")
		show := getShowCalendarOption(cx, getOptionsObject(cx, arg(args, 0)))
		return cx.NewString(wtf8.String(temporal.TemporalMonthDayToString(s.date, s.cal, show)))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainMonthDay(cx, this, name)
			return cx.NewString(wtf8.String(temporal.TemporalMonthDayToString(s.date, s.cal, temporal.ShowCalendarAuto)))
		})
	}
	valueOfThrows(cx, proto, "PlainMonthDay")
	tmethod(cx, proto, "toPlainDate", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainMonthDay(cx, this, "toPlainDate")
		item := arg(args, 0)
		if !item.IsObject() {
			cx.ThrowTypeError("argument must be an object")
		}
		f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsMonthDay)
		in := prepareCalendarFields(cx, s.cal, item.Object(), []string{"year"}, fieldsNoneRequired)
		f = temporal.CalendarMergeFields(s.cal, f, in)
		return createPlainDate(cx, must(temporal.CalendarDateFromFields(s.cal, f, temporal.OverflowConstrain)), s.cal, nil)
	})
}
