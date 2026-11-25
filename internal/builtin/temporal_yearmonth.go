// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/temporal"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// createPlainYearMonth implements CreateTemporalYearMonth.
func createPlainYearMonth(cx *engine.ExecutionContext, d temporal.ISODate, cal string, nt *storage.Object) storage.Value {
	if !temporal.ISOYearMonthWithinLimits(d) {
		cx.ThrowRangeError("year and month are out of range")
	}
	proto := cx.GetPrototypeFromConstructor(nt, storage.IntrinsicTemporalPlainYearMonthPrototype)
	return storage.ObjectValue(cx.NewTemporalObject(proto, &plainYearMonthSlots{
		date: d,
		cal:  cal,
	}))
}

// createPlainMonthDay implements CreateTemporalMonthDay.
func createPlainMonthDay(cx *engine.ExecutionContext, d temporal.ISODate, cal string, nt *storage.Object) storage.Value {
	if !temporal.ISODateWithinLimits(d) {
		cx.ThrowRangeError("date is out of range")
	}
	proto := cx.GetPrototypeFromConstructor(nt, storage.IntrinsicTemporalPlainMonthDayPrototype)
	return storage.ObjectValue(cx.NewTemporalObject(proto, &plainMonthDaySlots{
		date: d,
		cal:  cal,
	}))
}

func thisPlainYearMonth(cx *engine.ExecutionContext, this storage.Value, name string) *plainYearMonthSlots {
	s, ok := storage.TemporalData(this).(*plainYearMonthSlots)
	if !ok {
		incompatible(cx, "Temporal.PlainYearMonth.prototype."+name, this)
	}
	return s
}

func thisPlainMonthDay(cx *engine.ExecutionContext, this storage.Value, name string) *plainMonthDaySlots {
	s, ok := storage.TemporalData(this).(*plainMonthDaySlots)
	if !ok {
		incompatible(cx, "Temporal.PlainMonthDay.prototype."+name, this)
	}
	return s
}

// toTemporalYearMonth implements ToTemporalYearMonth.
func toTemporalYearMonth(cx *engine.ExecutionContext, item, options storage.Value) plainYearMonthSlots {
	if item.IsObject() {
		if s, ok := storage.TemporalData(item).(*plainYearMonthSlots); ok {
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
func toTemporalMonthDay(cx *engine.ExecutionContext, item, options storage.Value) plainMonthDaySlots {
	if item.IsObject() {
		if s, ok := storage.TemporalData(item).(*plainMonthDaySlots); ok {
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

func initTemporalPlainYearMonth(cx *engine.ExecutionContext, ns *storage.Object) {
	ctor, proto := temporalConstructor(cx, ns, "PlainYearMonth", 2, storage.IntrinsicTemporalPlainYearMonthPrototype, nativePlainYearMonthConstructor)
	method(cx, ctor, "from", 1, nativePlainYearMonthFrom)
	method(cx, ctor, "compare", 2, nativePlainYearMonthCompare)
	getter(cx, proto, cx.Intern("calendarId"), "calendarId", nativePlainYearMonthCalendarId)
	getter(cx, proto, cx.Intern("era"), "era", nativePlainYearMonthEra)
	getter(cx, proto, cx.Intern("eraYear"), "eraYear", nativePlainYearMonthEraYear)
	getter(cx, proto, cx.Intern("year"), "year", nativePlainYearMonthYear)
	getter(cx, proto, cx.Intern("month"), "month", nativePlainYearMonthMonth)
	getter(cx, proto, cx.Intern("monthCode"), "monthCode", nativePlainYearMonthMonthCode)
	getter(cx, proto, cx.Intern("daysInMonth"), "daysInMonth", nativePlainYearMonthDaysInMonth)
	getter(cx, proto, cx.Intern("daysInYear"), "daysInYear", nativePlainYearMonthDaysInYear)
	getter(cx, proto, cx.Intern("monthsInYear"), "monthsInYear", nativePlainYearMonthMonthsInYear)
	getter(cx, proto, cx.Intern("inLeapYear"), "inLeapYear", nativePlainYearMonthInLeapYear)
	method(cx, proto, "with", 1, nativePlainYearMonthWith)
	method(cx, proto, "add", 1, nativePlainYearMonthAdd)
	method(cx, proto, "subtract", 1, nativePlainYearMonthSubtract)
	method(cx, proto, "until", 1, nativePlainYearMonthUntil)
	method(cx, proto, "since", 1, nativePlainYearMonthSince)
	method(cx, proto, "equals", 1, nativePlainYearMonthEquals)
	method(cx, proto, "toString", 0, nativePlainYearMonthToString)
	method(cx, proto, "toJSON", 0, nativePlainYearMonthToJSON)
	method(cx, proto, "toLocaleString", 0, nativePlainYearMonthToLocaleString)
	method(cx, proto, "valueOf", 0, nativePlainYearMonthValueOf)
	method(cx, proto, "toPlainDate", 1, nativePlainYearMonthToPlainDate)
}

// The native functions of Temporal.PlainYearMonth.
var (
	nativePlainYearMonthConstructor    = engine.RegisterNative(temporalNative(plainYearMonthConstructor))
	nativePlainYearMonthFrom           = engine.RegisterNative(temporalNative(plainYearMonthFrom))
	nativePlainYearMonthCompare        = engine.RegisterNative(temporalNative(plainYearMonthCompare))
	nativePlainYearMonthCalendarId     = engine.RegisterNative(calendarFieldNative("calendarId", plainYearMonthCalendarDate))
	nativePlainYearMonthEra            = engine.RegisterNative(calendarFieldNative("era", plainYearMonthCalendarDate))
	nativePlainYearMonthEraYear        = engine.RegisterNative(calendarFieldNative("eraYear", plainYearMonthCalendarDate))
	nativePlainYearMonthYear           = engine.RegisterNative(calendarFieldNative("year", plainYearMonthCalendarDate))
	nativePlainYearMonthMonth          = engine.RegisterNative(calendarFieldNative("month", plainYearMonthCalendarDate))
	nativePlainYearMonthMonthCode      = engine.RegisterNative(calendarFieldNative("monthCode", plainYearMonthCalendarDate))
	nativePlainYearMonthDaysInMonth    = engine.RegisterNative(calendarFieldNative("daysInMonth", plainYearMonthCalendarDate))
	nativePlainYearMonthDaysInYear     = engine.RegisterNative(calendarFieldNative("daysInYear", plainYearMonthCalendarDate))
	nativePlainYearMonthMonthsInYear   = engine.RegisterNative(calendarFieldNative("monthsInYear", plainYearMonthCalendarDate))
	nativePlainYearMonthInLeapYear     = engine.RegisterNative(calendarFieldNative("inLeapYear", plainYearMonthCalendarDate))
	nativePlainYearMonthWith           = engine.RegisterNative(temporalNative(plainYearMonthWith))
	nativePlainYearMonthAdd            = engine.RegisterNative(plainYearMonthAddNative("add"))
	nativePlainYearMonthSubtract       = engine.RegisterNative(plainYearMonthAddNative("subtract"))
	nativePlainYearMonthUntil          = engine.RegisterNative(plainYearMonthUntilNative("until"))
	nativePlainYearMonthSince          = engine.RegisterNative(plainYearMonthUntilNative("since"))
	nativePlainYearMonthEquals         = engine.RegisterNative(temporalNative(plainYearMonthEquals))
	nativePlainYearMonthToString       = engine.RegisterNative(temporalNative(plainYearMonthToString))
	nativePlainYearMonthToJSON         = engine.RegisterNative(plainYearMonthToJSONNative("toJSON"))
	nativePlainYearMonthToLocaleString = engine.RegisterNative(plainYearMonthToJSONNative("toLocaleString"))
	nativePlainYearMonthValueOf        = engine.RegisterNative(temporalValueOfNative("PlainYearMonth"))
	nativePlainYearMonthToPlainDate    = engine.RegisterNative(temporalNative(plainYearMonthToPlainDate))
)

// plainYearMonthCalendarDate returns the ISO date and the calendar of this for the getter name.
func plainYearMonthCalendarDate(cx *engine.ExecutionContext, this storage.Value, name string) (temporal.ISODate, string) {
	s := thisPlainYearMonth(cx, this, name)
	return s.date, s.cal
}

// plainYearMonthAddNative returns the method name of Temporal.PlainYearMonth.prototype, which is
// add or subtract.
//
//go:noinline
func plainYearMonthAddNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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

// plainYearMonthUntilNative returns the method name of Temporal.PlainYearMonth.prototype, which is
// until or since.
//
//go:noinline
func plainYearMonthUntilNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		s := thisPlainYearMonth(cx, this, name)
		other := toTemporalYearMonth(cx, arg(args, 0), storage.Undefined)
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

// plainYearMonthToJSONNative returns the method name of Temporal.PlainYearMonth.prototype, which is
// toJSON or toLocaleString.
//
//go:noinline
func plainYearMonthToJSONNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		s := thisPlainYearMonth(cx, this, name)
		return cx.NewString(wtf8.String(temporal.TemporalYearMonthToString(s.date, s.cal, temporal.ShowCalendarAuto)))
	})
}

func plainYearMonthConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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
}

func plainYearMonthFrom(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := toTemporalYearMonth(cx, arg(args, 0), arg(args, 1))
	return createPlainYearMonth(cx, s.date, s.cal, nil)
}

func plainYearMonthCompare(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	one := toTemporalYearMonth(cx, arg(args, 0), storage.Undefined)
	two := toTemporalYearMonth(cx, arg(args, 1), storage.Undefined)
	return compareResult(temporal.CompareISODate(one.date, two.date))
}

func plainYearMonthWith(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisPlainYearMonth(cx, this, "with")
	bag := rejectTemporalLikeObject(cx, arg(args, 0))
	f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsYearMonth)
	partial := prepareCalendarFields(cx, s.cal, bag, yearMonthFieldNames, fieldsPartial)
	f = temporal.CalendarMergeFields(s.cal, f, partial)
	overflow := getOverflowOption(cx, getOptionsObject(cx, arg(args, 1)))
	return createPlainYearMonth(cx, must(temporal.CalendarYearMonthFromFields(s.cal, f, overflow)), s.cal, nil)
}

func plainYearMonthEquals(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisPlainYearMonth(cx, this, "equals")
	other := toTemporalYearMonth(cx, arg(args, 0), storage.Undefined)
	return storage.Bool(temporal.CompareISODate(s.date, other.date) == 0 && calendarEquals(s.cal, other.cal))
}

func plainYearMonthToString(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisPlainYearMonth(cx, this, "toString")
	show := getShowCalendarOption(cx, getOptionsObject(cx, arg(args, 0)))
	return cx.NewString(wtf8.String(temporal.TemporalYearMonthToString(s.date, s.cal, show)))
}

func plainYearMonthToPlainDate(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisPlainYearMonth(cx, this, "toPlainDate")
	item := arg(args, 0)
	if !item.IsObject() {
		cx.ThrowTypeError("argument must be an object")
	}
	f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsYearMonth)
	in := prepareCalendarFields(cx, s.cal, item.Object(), []string{"day"}, fieldsNoneRequired)
	f = temporal.CalendarMergeFields(s.cal, f, in)
	return createPlainDate(cx, must(temporal.CalendarDateFromFields(s.cal, f, temporal.OverflowConstrain)), s.cal, nil)
}

func initTemporalPlainMonthDay(cx *engine.ExecutionContext, ns *storage.Object) {
	ctor, proto := temporalConstructor(cx, ns, "PlainMonthDay", 2, storage.IntrinsicTemporalPlainMonthDayPrototype, nativePlainMonthDayConstructor)
	method(cx, ctor, "from", 1, nativePlainMonthDayFrom)
	getter(cx, proto, cx.Intern("calendarId"), "calendarId", nativePlainMonthDayCalendarId)
	getter(cx, proto, cx.Intern("monthCode"), "monthCode", nativePlainMonthDayMonthCode)
	getter(cx, proto, cx.Intern("day"), "day", nativePlainMonthDayDay)
	method(cx, proto, "with", 1, nativePlainMonthDayWith)
	method(cx, proto, "equals", 1, nativePlainMonthDayEquals)
	method(cx, proto, "toString", 0, nativePlainMonthDayToString)
	method(cx, proto, "toJSON", 0, nativePlainMonthDayToJSON)
	method(cx, proto, "toLocaleString", 0, nativePlainMonthDayToLocaleString)
	method(cx, proto, "valueOf", 0, nativePlainMonthDayValueOf)
	method(cx, proto, "toPlainDate", 1, nativePlainMonthDayToPlainDate)
}

// The native functions of Temporal.PlainMonthDay.
var (
	nativePlainMonthDayConstructor    = engine.RegisterNative(temporalNative(plainMonthDayConstructor))
	nativePlainMonthDayFrom           = engine.RegisterNative(temporalNative(plainMonthDayFrom))
	nativePlainMonthDayCalendarId     = engine.RegisterNative(calendarFieldNative("calendarId", plainMonthDayCalendarDate))
	nativePlainMonthDayMonthCode      = engine.RegisterNative(calendarFieldNative("monthCode", plainMonthDayCalendarDate))
	nativePlainMonthDayDay            = engine.RegisterNative(calendarFieldNative("day", plainMonthDayCalendarDate))
	nativePlainMonthDayWith           = engine.RegisterNative(temporalNative(plainMonthDayWith))
	nativePlainMonthDayEquals         = engine.RegisterNative(temporalNative(plainMonthDayEquals))
	nativePlainMonthDayToString       = engine.RegisterNative(temporalNative(plainMonthDayToString))
	nativePlainMonthDayToJSON         = engine.RegisterNative(plainMonthDayToJSONNative("toJSON"))
	nativePlainMonthDayToLocaleString = engine.RegisterNative(plainMonthDayToJSONNative("toLocaleString"))
	nativePlainMonthDayValueOf        = engine.RegisterNative(temporalValueOfNative("PlainMonthDay"))
	nativePlainMonthDayToPlainDate    = engine.RegisterNative(temporalNative(plainMonthDayToPlainDate))
)

// plainMonthDayCalendarDate returns the ISO date and the calendar of this for the getter name.
func plainMonthDayCalendarDate(cx *engine.ExecutionContext, this storage.Value, name string) (temporal.ISODate, string) {
	s := thisPlainMonthDay(cx, this, name)
	return s.date, s.cal
}

// plainMonthDayToJSONNative returns the method name of Temporal.PlainMonthDay.prototype, which is
// toJSON or toLocaleString.
//
//go:noinline
func plainMonthDayToJSONNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
		s := thisPlainMonthDay(cx, this, name)
		return cx.NewString(wtf8.String(temporal.TemporalMonthDayToString(s.date, s.cal, temporal.ShowCalendarAuto)))
	})
}

func plainMonthDayConstructor(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
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
}

func plainMonthDayFrom(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := toTemporalMonthDay(cx, arg(args, 0), arg(args, 1))
	return createPlainMonthDay(cx, s.date, s.cal, nil)
}

func plainMonthDayWith(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisPlainMonthDay(cx, this, "with")
	bag := rejectTemporalLikeObject(cx, arg(args, 0))
	f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsMonthDay)
	partial := prepareCalendarFields(cx, s.cal, bag, dateFieldNames, fieldsPartial)
	f = temporal.CalendarMergeFields(s.cal, f, partial)
	overflow := getOverflowOption(cx, getOptionsObject(cx, arg(args, 1)))
	return createPlainMonthDay(cx, must(temporal.CalendarMonthDayFromFields(s.cal, f, overflow)), s.cal, nil)
}

func plainMonthDayEquals(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisPlainMonthDay(cx, this, "equals")
	other := toTemporalMonthDay(cx, arg(args, 0), storage.Undefined)
	return storage.Bool(temporal.CompareISODate(s.date, other.date) == 0 && calendarEquals(s.cal, other.cal))
}

func plainMonthDayToString(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisPlainMonthDay(cx, this, "toString")
	show := getShowCalendarOption(cx, getOptionsObject(cx, arg(args, 0)))
	return cx.NewString(wtf8.String(temporal.TemporalMonthDayToString(s.date, s.cal, show)))
}

func plainMonthDayToPlainDate(cx *engine.ExecutionContext, this storage.Value, args []storage.Value) storage.Value {
	s := thisPlainMonthDay(cx, this, "toPlainDate")
	item := arg(args, 0)
	if !item.IsObject() {
		cx.ThrowTypeError("argument must be an object")
	}
	f := temporal.ISODateToFields(s.cal, s.date, temporal.FieldsMonthDay)
	in := prepareCalendarFields(cx, s.cal, item.Object(), []string{"year"}, fieldsNoneRequired)
	f = temporal.CalendarMergeFields(s.cal, f, in)
	return createPlainDate(cx, must(temporal.CalendarDateFromFields(s.cal, f, temporal.OverflowConstrain)), s.cal, nil)
}
