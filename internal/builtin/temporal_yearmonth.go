// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/temporal"
)

// createPlainYearMonth implements CreateTemporalYearMonth.
func (r *temporalRealm) createPlainYearMonth(vm *engine.VM, d temporal.ISODate, cal string, nt *engine.Object) engine.Value {
	if !temporal.ISOYearMonthWithinLimits(d) {
		vm.ThrowRangeError("year and month are out of range")
	}
	return engine.ObjectValue(vm.NewTemporalObject(protoFor(vm, nt, r.plainYearMonth), &plainYearMonthSlots{
		date: d,
		cal:  cal,
	}))
}

// createPlainMonthDay implements CreateTemporalMonthDay.
func (r *temporalRealm) createPlainMonthDay(vm *engine.VM, d temporal.ISODate, cal string, nt *engine.Object) engine.Value {
	if !temporal.ISODateWithinLimits(d) {
		vm.ThrowRangeError("date is out of range")
	}
	return engine.ObjectValue(vm.NewTemporalObject(protoFor(vm, nt, r.plainMonthDay), &plainMonthDaySlots{
		date: d,
		cal:  cal,
	}))
}

func thisPlainYearMonth(vm *engine.VM, this engine.Value, name string) *plainYearMonthSlots {
	s, ok := engine.TemporalData(this).(*plainYearMonthSlots)
	if !ok {
		incompatible(vm, "Temporal.PlainYearMonth.prototype."+name, this)
	}
	return s
}

func thisPlainMonthDay(vm *engine.VM, this engine.Value, name string) *plainMonthDaySlots {
	s, ok := engine.TemporalData(this).(*plainMonthDaySlots)
	if !ok {
		incompatible(vm, "Temporal.PlainMonthDay.prototype."+name, this)
	}
	return s
}

// toTemporalYearMonth implements ToTemporalYearMonth.
func toTemporalYearMonth(vm *engine.VM, item, options engine.Value) plainYearMonthSlots {
	if item.IsObject() {
		if s, ok := engine.TemporalData(item).(*plainYearMonthSlots); ok {
			getOverflowOption(vm, getOptionsObject(vm, options))
			return *s
		}
		o := item.Object()
		cal := calendarWithISODefault(vm, o)
		f := prepareCalendarFields(vm, o, yearMonthFieldNames, fieldsNoneRequired)
		overflow := getOverflowOption(vm, getOptionsObject(vm, options))
		d := must(temporal.CalendarYearMonthFromFields(f, overflow))
		return plainYearMonthSlots{
			date: d,
			cal:  cal,
		}
	}
	str := requireString(vm, item, "year-month")
	p := must(temporal.ParseISODateTime(str, temporal.GoalYearMonth))
	cal := canonicalizeParsedCalendar(vm, p.Calendar)
	getOverflowOption(vm, getOptionsObject(vm, options))
	d := temporal.ISODate{Year: p.Year, Month: p.Month, Day: p.Day}
	f := temporal.ISODateToFields(d, temporal.FieldsYearMonth)
	d = must(temporal.CalendarYearMonthFromFields(f, temporal.OverflowConstrain))
	return plainYearMonthSlots{
		date: d,
		cal:  cal,
	}
}

// toTemporalMonthDay implements ToTemporalMonthDay.
func toTemporalMonthDay(vm *engine.VM, item, options engine.Value) plainMonthDaySlots {
	if item.IsObject() {
		if s, ok := engine.TemporalData(item).(*plainMonthDaySlots); ok {
			getOverflowOption(vm, getOptionsObject(vm, options))
			return *s
		}
		o := item.Object()
		cal := calendarWithISODefault(vm, o)
		f := prepareCalendarFields(vm, o, dateFieldNames, fieldsNoneRequired)
		overflow := getOverflowOption(vm, getOptionsObject(vm, options))
		d := must(temporal.CalendarMonthDayFromFields(f, overflow))
		return plainMonthDaySlots{
			date: d,
			cal:  cal,
		}
	}
	str := requireString(vm, item, "month-day")
	p := must(temporal.ParseISODateTime(str, temporal.GoalMonthDay))
	cal := canonicalizeParsedCalendar(vm, p.Calendar)
	getOverflowOption(vm, getOptionsObject(vm, options))
	return plainMonthDaySlots{
		date: temporal.ISODate{Year: 1972, Month: p.Month, Day: p.Day},
		cal:  cal,
	}
}

func initTemporalPlainYearMonth(vm *engine.VM, ns *engine.Object, r *temporalRealm) {
	proto := r.plainYearMonth
	ctor := temporalConstructor(vm, ns, "PlainYearMonth", 2, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(vm, "Temporal.PlainYearMonth")
		y := toIntegerWithTruncation(vm, arg(args, 0), "year")
		m := toIntegerWithTruncation(vm, arg(args, 1), "month")
		cal := temporal.ISO8601
		if c := arg(args, 2); !c.IsUndefined() {
			cal = must(temporal.CanonicalizeCalendar(requireString(vm, c, "calendar")))
		}
		day := 1.0
		if d := arg(args, 3); !d.IsUndefined() {
			day = toIntegerWithTruncation(vm, d, "referenceISODay")
		}
		date := must(temporal.CreateISODateRecord(y, m, day))
		return r.createPlainYearMonth(vm, date, cal, nt)
	})
	tmethod(vm, ctor, "from", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := toTemporalYearMonth(vm, arg(args, 0), arg(args, 1))
		return r.createPlainYearMonth(vm, s.date, s.cal, nil)
	})
	tmethod(vm, ctor, "compare", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		one := toTemporalYearMonth(vm, arg(args, 0), engine.Undefined)
		two := toTemporalYearMonth(vm, arg(args, 1), engine.Undefined)
		return compareResult(temporal.CompareISODate(one.date, two.date))
	})
	defineCalendarGetters(vm, proto, []string{"calendarId", "era", "eraYear", "year", "month", "monthCode", "daysInMonth", "daysInYear", "monthsInYear", "inLeapYear"},
		func(vm *engine.VM, this engine.Value, name string) (temporal.ISODate, string) {
			s := thisPlainYearMonth(vm, this, name)
			return s.date, s.cal
		})
	tmethod(vm, proto, "with", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainYearMonth(vm, this, "with")
		bag := rejectTemporalLikeObject(vm, arg(args, 0))
		f := temporal.ISODateToFields(s.date, temporal.FieldsYearMonth)
		partial := prepareCalendarFields(vm, bag, yearMonthFieldNames, fieldsPartial)
		f = temporal.CalendarMergeFields(f, partial)
		overflow := getOverflowOption(vm, getOptionsObject(vm, arg(args, 1)))
		return r.createPlainYearMonth(vm, must(temporal.CalendarYearMonthFromFields(f, overflow)), s.cal, nil)
	})
	for _, name := range []string{"add", "subtract"} {
		tmethod(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainYearMonth(vm, this, name)
			d := toTemporalDuration(vm, arg(args, 0))
			if name == "subtract" {
				d = d.Negated()
			}
			id := temporal.ToInternalDurationRecord(&d)
			overflow := getOverflowOption(vm, getOptionsObject(vm, arg(args, 1)))
			if id.Date.Weeks != 0 || id.Date.Days != 0 || !id.Time.IsZero() {
				vm.ThrowRangeError("only years and months can be added to a PlainYearMonth")
			}
			f := temporal.ISODateToFields(s.date, temporal.FieldsYearMonth)
			f.Day, f.HasDay = 1, true
			date := must(temporal.CalendarDateFromFields(f, temporal.OverflowConstrain))
			added := must(temporal.CalendarDateAdd(date, id.Date, overflow))
			af := temporal.ISODateToFields(added, temporal.FieldsYearMonth)
			return r.createPlainYearMonth(vm, must(temporal.CalendarYearMonthFromFields(af, overflow)), s.cal, nil)
		})
	}
	for _, name := range []string{"until", "since"} {
		tmethod(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainYearMonth(vm, this, name)
			other := toTemporalYearMonth(vm, arg(args, 0), engine.Undefined)
			checkSameCalendar(vm, s.cal, other.cal)
			opts := getOptionsObject(vm, arg(args, 1))
			settings := getDifferenceSettings(vm, name == "since", opts, unitGroupDate, []temporal.Unit{temporal.UnitWeek, temporal.UnitDay}, temporal.UnitMonth, temporal.UnitYear)
			if temporal.CompareISODate(s.date, other.date) == 0 {
				return r.newDuration(vm, temporal.Duration{}, nil)
			}
			tf := temporal.ISODateToFields(s.date, temporal.FieldsYearMonth)
			tf.Day, tf.HasDay = 1, true
			thisDate := must(temporal.CalendarDateFromFields(tf, temporal.OverflowConstrain))
			of := temporal.ISODateToFields(other.date, temporal.FieldsYearMonth)
			of.Day, of.HasDay = 1, true
			otherDate := must(temporal.CalendarDateFromFields(of, temporal.OverflowConstrain))
			dd := temporal.CalendarDateUntil(thisDate, otherDate, settings.largestUnit)
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
				id = must(temporal.RoundRelativeDuration(id, from.UTCEpochNanoseconds(), to.UTCEpochNanoseconds(), from, nil,
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
		s := thisPlainYearMonth(vm, this, "equals")
		other := toTemporalYearMonth(vm, arg(args, 0), engine.Undefined)
		return engine.Bool(temporal.CompareISODate(s.date, other.date) == 0 && calendarEquals(s.cal, other.cal))
	})
	tmethod(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainYearMonth(vm, this, "toString")
		show := getShowCalendarOption(vm, getOptionsObject(vm, arg(args, 0)))
		return vm.NewString(temporal.TemporalYearMonthToString(s.date, s.cal, show))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainYearMonth(vm, this, name)
			return vm.NewString(temporal.TemporalYearMonthToString(s.date, s.cal, temporal.ShowCalendarAuto))
		})
	}
	valueOfThrows(vm, proto, "PlainYearMonth")
	tmethod(vm, proto, "toPlainDate", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainYearMonth(vm, this, "toPlainDate")
		item := arg(args, 0)
		if !item.IsObject() {
			vm.ThrowTypeError("argument must be an object")
		}
		f := temporal.ISODateToFields(s.date, temporal.FieldsYearMonth)
		in := prepareCalendarFields(vm, item.Object(), []string{"day"}, fieldsNoneRequired)
		f = temporal.CalendarMergeFields(f, in)
		return r.createPlainDate(vm, must(temporal.CalendarDateFromFields(f, temporal.OverflowConstrain)), s.cal, nil)
	})
}

func initTemporalPlainMonthDay(vm *engine.VM, ns *engine.Object, r *temporalRealm) {
	proto := r.plainMonthDay
	ctor := temporalConstructor(vm, ns, "PlainMonthDay", 2, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(vm, "Temporal.PlainMonthDay")
		m := toIntegerWithTruncation(vm, arg(args, 0), "month")
		d := toIntegerWithTruncation(vm, arg(args, 1), "day")
		cal := temporal.ISO8601
		if c := arg(args, 2); !c.IsUndefined() {
			cal = must(temporal.CanonicalizeCalendar(requireString(vm, c, "calendar")))
		}
		year := 1972.0
		if y := arg(args, 3); !y.IsUndefined() {
			year = toIntegerWithTruncation(vm, y, "referenceISOYear")
		}
		date := must(temporal.CreateISODateRecord(year, m, d))
		return r.createPlainMonthDay(vm, date, cal, nt)
	})
	tmethod(vm, ctor, "from", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := toTemporalMonthDay(vm, arg(args, 0), arg(args, 1))
		return r.createPlainMonthDay(vm, s.date, s.cal, nil)
	})
	defineCalendarGetters(vm, proto, []string{"calendarId", "monthCode", "day"}, func(vm *engine.VM, this engine.Value, name string) (temporal.ISODate, string) {
		s := thisPlainMonthDay(vm, this, name)
		return s.date, s.cal
	})
	tmethod(vm, proto, "with", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainMonthDay(vm, this, "with")
		bag := rejectTemporalLikeObject(vm, arg(args, 0))
		f := temporal.ISODateToFields(s.date, temporal.FieldsMonthDay)
		partial := prepareCalendarFields(vm, bag, dateFieldNames, fieldsPartial)
		f = temporal.CalendarMergeFields(f, partial)
		overflow := getOverflowOption(vm, getOptionsObject(vm, arg(args, 1)))
		return r.createPlainMonthDay(vm, must(temporal.CalendarMonthDayFromFields(f, overflow)), s.cal, nil)
	})
	tmethod(vm, proto, "equals", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainMonthDay(vm, this, "equals")
		other := toTemporalMonthDay(vm, arg(args, 0), engine.Undefined)
		return engine.Bool(temporal.CompareISODate(s.date, other.date) == 0 && calendarEquals(s.cal, other.cal))
	})
	tmethod(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainMonthDay(vm, this, "toString")
		show := getShowCalendarOption(vm, getOptionsObject(vm, arg(args, 0)))
		return vm.NewString(temporal.TemporalMonthDayToString(s.date, s.cal, show))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisPlainMonthDay(vm, this, name)
			return vm.NewString(temporal.TemporalMonthDayToString(s.date, s.cal, temporal.ShowCalendarAuto))
		})
	}
	valueOfThrows(vm, proto, "PlainMonthDay")
	tmethod(vm, proto, "toPlainDate", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisPlainMonthDay(vm, this, "toPlainDate")
		item := arg(args, 0)
		if !item.IsObject() {
			vm.ThrowTypeError("argument must be an object")
		}
		f := temporal.ISODateToFields(s.date, temporal.FieldsMonthDay)
		in := prepareCalendarFields(vm, item.Object(), []string{"year"}, fieldsNoneRequired)
		f = temporal.CalendarMergeFields(f, in)
		return r.createPlainDate(vm, must(temporal.CalendarDateFromFields(f, temporal.OverflowConstrain)), s.cal, nil)
	})
}
