// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// The indices of the components of a date, in the order of the arguments of the Date constructor.
const (
	dateYear = iota
	dateMonth
	dateDay
	dateHours
	dateMinutes
	dateSeconds
	dateMilliseconds
	dateComponentCount
)

// thisDate returns the [[DateValue]] of this, or throws a TypeError naming the method.
func thisDate(cx *engine.ExecutionContext, this engine.Value, name string) float64 {
	tv, ok := engine.ThisTimeValue(this)
	if !ok {
		cx.ThrowTypeError("Date.prototype." + name + " called on incompatible receiver " + cx.DisplayString(this))
	}
	return tv
}

// components returns the components of the time value t, which must be finite.
func components(t float64) [dateComponentCount]float64 {
	year, month, date := datetime.CalendarFromTime(t)
	return [dateComponentCount]float64{
		dateYear:         year,
		dateMonth:        float64(month),
		dateDay:          date,
		dateHours:        datetime.HourFromTime(t),
		dateMinutes:      datetime.MinFromTime(t),
		dateSeconds:      datetime.SecFromTime(t),
		dateMilliseconds: datetime.MsFromTime(t),
	}
}

// makeDate returns the time value of the components c, before TimeClip.
func makeDate(c *[dateComponentCount]float64) float64 {
	day := datetime.MakeDay(c[dateYear], c[dateMonth], c[dateDay])
	return datetime.MakeDate(day, datetime.MakeTime(c[dateHours], c[dateMinutes], c[dateSeconds], c[dateMilliseconds]))
}

// dateTimeZoneName is the function that [SetDateTimeZoneName] sets, or nil.
var dateTimeZoneName func(cx *engine.ExecutionContext, tz datetime.TimeZone, t, offset int64) string

// SetDateTimeZoneName sets the function that returns the name of the time zone tz in Date
// strings for the time value t with the UTC offset offset, or empty for the default name. It
// must be called before any VM is created, typically from an init function.
func SetDateTimeZoneName(name func(cx *engine.ExecutionContext, tz datetime.TimeZone, t, offset int64) string) {
	dateTimeZoneName = name
}

// dateStringOffset returns the local UTC offset and the display name for Date strings.
func dateStringOffset(cx *engine.ExecutionContext, tz datetime.TimeZone, tv float64) (float64, string) {
	offset, name := tz.Offset(tv)
	if dateTimeZoneName != nil {
		if s := dateTimeZoneName(cx, tz, int64(tv), int64(offset)); s != "" {
			name = s
		}
	}
	return offset, name
}

func formatDate(cx *engine.ExecutionContext, method string, tv float64) string {
	tz := cx.DefaultTimeZone()
	switch method {
	case "toString", "toTimeString":
		offset, name := dateStringOffset(cx, tz, tv)
		if method == "toString" {
			return datetime.String(tv, offset, name)
		}
		return datetime.TimeString(tv, offset, name)
	case "toDateString":
		return datetime.DateString(tv, tz)
	case "toUTCString":
		return datetime.UTCString(tv)
	case "toLocaleString":
		return datetime.LocaleString(tv, tz)
	case "toLocaleDateString":
		return datetime.LocaleDateString(tv, tz)
	case "toLocaleTimeString":
		return datetime.LocaleTimeString(tv, tz)
	}
	panic("invalid Date format method")
}

func initDate(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicDatePrototype)

	ctor := constructor(cx, "Date", 7, engine.IntrinsicDatePrototype, dateConstructor)

	cx.SetMethodKernels(method(cx, ctor, "now", 0, dateNow), shortContract, dateNowKernel, nil, nil)
	method(cx, ctor, "parse", 1, dateParse)
	method(cx, ctor, "UTC", 7, dateUTC)

	method(cx, proto, "getFullYear", 0, dateGetFullYear)
	method(cx, proto, "getUTCFullYear", 0, dateGetUTCFullYear)
	method(cx, proto, "getMonth", 0, dateGetMonth)
	method(cx, proto, "getUTCMonth", 0, dateGetUTCMonth)
	method(cx, proto, "getDate", 0, dateGetDate)
	method(cx, proto, "getUTCDate", 0, dateGetUTCDate)
	method(cx, proto, "getHours", 0, dateGetHours)
	method(cx, proto, "getUTCHours", 0, dateGetUTCHours)
	method(cx, proto, "getMinutes", 0, dateGetMinutes)
	method(cx, proto, "getUTCMinutes", 0, dateGetUTCMinutes)
	method(cx, proto, "getSeconds", 0, dateGetSeconds)
	method(cx, proto, "getUTCSeconds", 0, dateGetUTCSeconds)
	method(cx, proto, "getMilliseconds", 0, dateGetMilliseconds)
	method(cx, proto, "getUTCMilliseconds", 0, dateGetUTCMilliseconds)
	method(cx, proto, "getDay", 0, dateGetDay)
	method(cx, proto, "getUTCDay", 0, dateGetUTCDay)
	method(cx, proto, "getTime", 0, dateGetTime)
	method(cx, proto, "getTimezoneOffset", 0, dateGetTimezoneOffset)
	method(cx, proto, "valueOf", 0, dateValueOf)
	method(cx, proto, "getYear", 0, dateGetYear)

	method(cx, proto, "setFullYear", 3, dateSetFullYear)
	method(cx, proto, "setUTCFullYear", 3, dateSetUTCFullYear)
	method(cx, proto, "setMonth", 2, dateSetMonth)
	method(cx, proto, "setUTCMonth", 2, dateSetUTCMonth)
	method(cx, proto, "setDate", 1, dateSetDate)
	method(cx, proto, "setUTCDate", 1, dateSetUTCDate)
	method(cx, proto, "setHours", 4, dateSetHours)
	method(cx, proto, "setUTCHours", 4, dateSetUTCHours)
	method(cx, proto, "setMinutes", 3, dateSetMinutes)
	method(cx, proto, "setUTCMinutes", 3, dateSetUTCMinutes)
	method(cx, proto, "setSeconds", 2, dateSetSeconds)
	method(cx, proto, "setUTCSeconds", 2, dateSetUTCSeconds)
	method(cx, proto, "setMilliseconds", 1, dateSetMilliseconds)
	method(cx, proto, "setUTCMilliseconds", 1, dateSetUTCMilliseconds)
	method(cx, proto, "setTime", 1, dateSetTime)
	method(cx, proto, "setYear", 1, dateSetYear)

	method(cx, proto, "toString", 0, dateToString)
	method(cx, proto, "toDateString", 0, dateToDateString)
	method(cx, proto, "toTimeString", 0, dateToTimeString)
	toUTCString := method(cx, proto, "toUTCString", 0, dateToUTCString)
	cx.Define(proto, cx.Intern("toGMTString"), engine.ObjectValue(toUTCString), engine.FlagsHidden)
	method(cx, proto, "toLocaleString", 0, dateToLocaleString)
	method(cx, proto, "toLocaleDateString", 0, dateToLocaleDateString)
	method(cx, proto, "toLocaleTimeString", 0, dateToLocaleTimeString)
	method(cx, proto, "toISOString", 0, dateToISOString)
	method(cx, proto, "toJSON", 1, dateToJSON)

	toPrimitive := cx.NewNative("[Symbol.toPrimitive]", 1, dateToPrimitive)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToPrimitive), engine.ObjectValue(toPrimitive), engine.FlagConfigurable)
}

func dateConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := cx.GetNewTarget()
	if nt == nil {
		return cx.StringFromGo(formatDate(cx, "toString", cx.SystemUTCEpochMilliseconds()))
	}
	var tv float64
	switch len(args) {
	case 0:
		tv = cx.SystemUTCEpochMilliseconds()
	case 1:
		if t, ok := engine.ThisTimeValue(args[0]); ok {
			tv = t
			break
		}
		v := cx.ToPrimitive(args[0], engine.HintDefault)
		if v.IsString() {
			tv = datetime.ParsePolled(string(cx.ToString(v)), cx.DefaultTimeZone(), cx.Poll)
		} else {
			tv = datetime.TimeClip(cx.ToNumber(v))
		}
	default:
		c := [dateComponentCount]float64{dateDay: 1}
		for i := range min(len(args), dateComponentCount) {
			c[i] = cx.ToNumber(args[i])
		}
		c[dateYear] = datetime.MakeFullYear(c[dateYear])
		tv = datetime.TimeClip(datetime.UTC(makeDate(&c), cx.DefaultTimeZone()))
	}
	return engine.ObjectValue(cx.NewDateObject(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicDatePrototype), tv))
}

func dateNow(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Number(cx.SystemUTCEpochMilliseconds())
}

func dateNowKernel(q engine.QuietContext, this engine.Value) (engine.Value, bool) {
	// A host clock can reenter JavaScript and observe the native call frame.
	ms, ok := q.SystemUTCEpochMilliseconds()
	return engine.Number(ms), ok
}

func dateParse(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Number(datetime.ParsePolled(string(cx.ToString(arg(args, 0))), cx.DefaultTimeZone(), cx.Poll))
}

func dateUTC(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	c := [dateComponentCount]float64{dateDay: 1}
	c[dateYear] = cx.ToNumber(arg(args, 0))
	for i := 1; i < min(len(args), dateComponentCount); i++ {
		c[i] = cx.ToNumber(args[i])
	}
	c[dateYear] = datetime.MakeFullYear(c[dateYear])
	return engine.Number(datetime.TimeClip(makeDate(&c)))
}

var (
	dateGetFullYear        = dateGetNative("getFullYear", true, datetime.YearFromTime)
	dateGetUTCFullYear     = dateGetNative("getUTCFullYear", false, datetime.YearFromTime)
	dateGetMonth           = dateGetNative("getMonth", true, datetime.MonthFromTime)
	dateGetUTCMonth        = dateGetNative("getUTCMonth", false, datetime.MonthFromTime)
	dateGetDate            = dateGetNative("getDate", true, datetime.DateFromTime)
	dateGetUTCDate         = dateGetNative("getUTCDate", false, datetime.DateFromTime)
	dateGetHours           = dateGetNative("getHours", true, datetime.HourFromTime)
	dateGetUTCHours        = dateGetNative("getUTCHours", false, datetime.HourFromTime)
	dateGetMinutes         = dateGetNative("getMinutes", true, datetime.MinFromTime)
	dateGetUTCMinutes      = dateGetNative("getUTCMinutes", false, datetime.MinFromTime)
	dateGetSeconds         = dateGetNative("getSeconds", true, datetime.SecFromTime)
	dateGetUTCSeconds      = dateGetNative("getUTCSeconds", false, datetime.SecFromTime)
	dateGetMilliseconds    = dateGetNative("getMilliseconds", true, datetime.MsFromTime)
	dateGetUTCMilliseconds = dateGetNative("getUTCMilliseconds", false, datetime.MsFromTime)
)

// dateGetNative returns the method name of Date.prototype, which returns a component of the date, in
// local time or in UTC.
//
//go:noinline
func dateGetNative(name string, local bool, component func(float64) float64) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisDate(cx, this, name)
		if math.IsNaN(t) {
			return engine.Number(t)
		}
		if local {
			t = datetime.LocalTime(t, cx.DefaultTimeZone())
		}
		return engine.Number(component(t))
	}
}

func dateGetDay(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisDate(cx, this, "getDay")
	if math.IsNaN(t) {
		return engine.Number(t)
	}
	return engine.Number(datetime.WeekDay(datetime.LocalTime(t, cx.DefaultTimeZone())))
}

func dateGetUTCDay(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisDate(cx, this, "getUTCDay")
	if math.IsNaN(t) {
		return engine.Number(t)
	}
	return engine.Number(datetime.WeekDay(t))
}

func dateGetTime(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Number(thisDate(cx, this, "getTime"))
}

func dateGetTimezoneOffset(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisDate(cx, this, "getTimezoneOffset")
	if math.IsNaN(t) {
		return engine.Number(t)
	}
	return engine.Number((t - datetime.LocalTime(t, cx.DefaultTimeZone())) / 60000)
}

func dateValueOf(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return engine.Number(thisDate(cx, this, "valueOf"))
}

func dateGetYear(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisDate(cx, this, "getYear")
	if math.IsNaN(t) {
		return engine.Number(t)
	}
	return engine.Number(datetime.YearFromTime(datetime.LocalTime(t, cx.DefaultTimeZone())) - 1900)
}

var (
	dateSetFullYear        = dateSetNative("setFullYear", true, dateYear, 3)
	dateSetUTCFullYear     = dateSetNative("setUTCFullYear", false, dateYear, 3)
	dateSetMonth           = dateSetNative("setMonth", true, dateMonth, 2)
	dateSetUTCMonth        = dateSetNative("setUTCMonth", false, dateMonth, 2)
	dateSetDate            = dateSetNative("setDate", true, dateDay, 1)
	dateSetUTCDate         = dateSetNative("setUTCDate", false, dateDay, 1)
	dateSetHours           = dateSetNative("setHours", true, dateHours, 4)
	dateSetUTCHours        = dateSetNative("setUTCHours", false, dateHours, 4)
	dateSetMinutes         = dateSetNative("setMinutes", true, dateMinutes, 3)
	dateSetUTCMinutes      = dateSetNative("setUTCMinutes", false, dateMinutes, 3)
	dateSetSeconds         = dateSetNative("setSeconds", true, dateSeconds, 2)
	dateSetUTCSeconds      = dateSetNative("setUTCSeconds", false, dateSeconds, 2)
	dateSetMilliseconds    = dateSetNative("setMilliseconds", true, dateMilliseconds, 1)
	dateSetUTCMilliseconds = dateSetNative("setUTCMilliseconds", false, dateMilliseconds, 1)
)

// dateSetNative returns the method name of Date.prototype, which sets the components from first on,
// as many as there are arguments up to count, in local time or in UTC.
//
//go:noinline
func dateSetNative(name string, local bool, first, count int) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisDate(cx, this, name)
		var vals [dateComponentCount]float64
		n := max(1, min(len(args), count))
		for i := range n {
			vals[i] = cx.ToNumber(arg(args, i))
		}
		tz := cx.DefaultTimeZone()
		switch {
		case math.IsNaN(t) && first == dateYear:
			t = 0
		case math.IsNaN(t):
			return engine.Number(t)
		case local:
			t = datetime.LocalTime(t, tz)
		}
		c := components(t)
		copy(c[first:first+n], vals[:n])
		u := makeDate(&c)
		if local {
			u = datetime.UTC(u, tz)
		}
		u = datetime.TimeClip(u)
		engine.SetDateValue(this, u)
		return engine.Number(u)
	}
}

func dateSetTime(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	thisDate(cx, this, "setTime")
	t := datetime.TimeClip(cx.ToNumber(arg(args, 0)))
	engine.SetDateValue(this, t)
	return engine.Number(t)
}

func dateSetYear(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisDate(cx, this, "setYear")
	y := cx.ToNumber(arg(args, 0))
	tz := cx.DefaultTimeZone()
	if math.IsNaN(t) {
		t = 0
	} else {
		t = datetime.LocalTime(t, tz)
	}
	c := components(t)
	c[dateYear] = datetime.MakeFullYear(y)
	u := datetime.TimeClip(datetime.UTC(makeDate(&c), tz))
	engine.SetDateValue(this, u)
	return engine.Number(u)
}

var (
	dateToString           = dateStringNative("toString")
	dateToDateString       = dateStringNative("toDateString")
	dateToTimeString       = dateStringNative("toTimeString")
	dateToUTCString        = dateStringNative("toUTCString")
	dateToLocaleString     = dateStringNative("toLocaleString")
	dateToLocaleDateString = dateStringNative("toLocaleDateString")
	dateToLocaleTimeString = dateStringNative("toLocaleTimeString")
)

// dateStringNative returns the method name of Date.prototype, which formats the date.
//
//go:noinline
func dateStringNative(name string) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		tv := thisDate(cx, this, name)
		if math.IsNaN(tv) {
			return cx.NewString("Invalid Date")
		}
		return cx.StringFromGo(formatDate(cx, name, tv))
	}
}

func dateToISOString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	tv := thisDate(cx, this, "toISOString")
	if math.IsNaN(tv) {
		cx.ThrowRangeError("invalid time value")
	}
	return cx.NewString(wtf8.String(datetime.ISOString(tv)))
}

func dateToJSON(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	o := engine.ObjectValue(cx.ToObject(this))
	tv := cx.ToPrimitive(o, engine.HintNumber)
	if tv.IsNumber() {
		if n := tv.Number(); math.IsNaN(n) || math.IsInf(n, 0) {
			return engine.Null
		}
	}
	return cx.CallValue(cx.GetV(o, cx.Key(engine.KeyToISOString)), o)
}

func dateToPrimitive(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	o := thisObject(cx, this, "Date.prototype[Symbol.toPrimitive]")
	h := arg(args, 0)
	if h.IsString() {
		switch h.Str() {
		case "string", "default":
			return cx.OrdinaryToPrimitive(o, engine.HintString)
		case "number":
			return cx.OrdinaryToPrimitive(o, engine.HintNumber)
		}
	}
	cx.ThrowTypeError("invalid hint: " + cx.DisplayString(h))
	return engine.Undefined
}
