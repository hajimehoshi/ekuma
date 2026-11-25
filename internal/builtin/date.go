// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/quiet"
	"github.com/hajimehoshi/ekuma/internal/storage"
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
	tv, ok := storage.ThisTimeValue(this)
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

var (
	nativeDateConstructor = engine.RegisterNative(dateConstructor)
	nativeDateNow         = engine.RegisterNativeWithKernels(dateNow, engine.Kernels{
		Method0:  quiet.DateNowKernel,
		Contract: shortContract,
	})
	nativeDateParse             = engine.RegisterNative(dateParse)
	nativeDateUTC               = engine.RegisterNative(dateUTC)
	nativeDateGetDay            = engine.RegisterNative(dateGetDay)
	nativeDateGetUTCDay         = engine.RegisterNative(dateGetUTCDay)
	nativeDateGetTime           = engine.RegisterNative(dateGetTime)
	nativeDateGetTimezoneOffset = engine.RegisterNative(dateGetTimezoneOffset)
	nativeDateValueOf           = engine.RegisterNative(dateValueOf)
	nativeDateGetYear           = engine.RegisterNative(dateGetYear)
	nativeDateSetTime           = engine.RegisterNative(dateSetTime)
	nativeDateSetYear           = engine.RegisterNative(dateSetYear)
	nativeDateToISOString       = engine.RegisterNative(dateToISOString)
	nativeDateToJSON            = engine.RegisterNative(dateToJSON)
	nativeDateToPrimitive       = engine.RegisterNative(dateToPrimitive)
)

func initDate(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicDatePrototype)

	ctor := constructor(cx, "Date", 7, engine.IntrinsicDatePrototype, nativeDateConstructor)

	method(cx, ctor, "now", 0, nativeDateNow)
	method(cx, ctor, "parse", 1, nativeDateParse)
	method(cx, ctor, "UTC", 7, nativeDateUTC)

	method(cx, proto, "getFullYear", 0, nativeDateGetFullYear)
	method(cx, proto, "getUTCFullYear", 0, nativeDateGetUTCFullYear)
	method(cx, proto, "getMonth", 0, nativeDateGetMonth)
	method(cx, proto, "getUTCMonth", 0, nativeDateGetUTCMonth)
	method(cx, proto, "getDate", 0, nativeDateGetDate)
	method(cx, proto, "getUTCDate", 0, nativeDateGetUTCDate)
	method(cx, proto, "getHours", 0, nativeDateGetHours)
	method(cx, proto, "getUTCHours", 0, nativeDateGetUTCHours)
	method(cx, proto, "getMinutes", 0, nativeDateGetMinutes)
	method(cx, proto, "getUTCMinutes", 0, nativeDateGetUTCMinutes)
	method(cx, proto, "getSeconds", 0, nativeDateGetSeconds)
	method(cx, proto, "getUTCSeconds", 0, nativeDateGetUTCSeconds)
	method(cx, proto, "getMilliseconds", 0, nativeDateGetMilliseconds)
	method(cx, proto, "getUTCMilliseconds", 0, nativeDateGetUTCMilliseconds)
	method(cx, proto, "getDay", 0, nativeDateGetDay)
	method(cx, proto, "getUTCDay", 0, nativeDateGetUTCDay)
	method(cx, proto, "getTime", 0, nativeDateGetTime)
	method(cx, proto, "getTimezoneOffset", 0, nativeDateGetTimezoneOffset)
	method(cx, proto, "valueOf", 0, nativeDateValueOf)
	method(cx, proto, "getYear", 0, nativeDateGetYear)

	method(cx, proto, "setFullYear", 3, nativeDateSetFullYear)
	method(cx, proto, "setUTCFullYear", 3, nativeDateSetUTCFullYear)
	method(cx, proto, "setMonth", 2, nativeDateSetMonth)
	method(cx, proto, "setUTCMonth", 2, nativeDateSetUTCMonth)
	method(cx, proto, "setDate", 1, nativeDateSetDate)
	method(cx, proto, "setUTCDate", 1, nativeDateSetUTCDate)
	method(cx, proto, "setHours", 4, nativeDateSetHours)
	method(cx, proto, "setUTCHours", 4, nativeDateSetUTCHours)
	method(cx, proto, "setMinutes", 3, nativeDateSetMinutes)
	method(cx, proto, "setUTCMinutes", 3, nativeDateSetUTCMinutes)
	method(cx, proto, "setSeconds", 2, nativeDateSetSeconds)
	method(cx, proto, "setUTCSeconds", 2, nativeDateSetUTCSeconds)
	method(cx, proto, "setMilliseconds", 1, nativeDateSetMilliseconds)
	method(cx, proto, "setUTCMilliseconds", 1, nativeDateSetUTCMilliseconds)
	method(cx, proto, "setTime", 1, nativeDateSetTime)
	method(cx, proto, "setYear", 1, nativeDateSetYear)

	method(cx, proto, "toString", 0, nativeDateToString)
	method(cx, proto, "toDateString", 0, nativeDateToDateString)
	method(cx, proto, "toTimeString", 0, nativeDateToTimeString)
	toUTCString := method(cx, proto, "toUTCString", 0, nativeDateToUTCString)
	cx.Define(proto, cx.Intern("toGMTString"), storage.ObjectValue(toUTCString), engine.FlagsHidden)
	method(cx, proto, "toLocaleString", 0, nativeDateToLocaleString)
	method(cx, proto, "toLocaleDateString", 0, nativeDateToLocaleDateString)
	method(cx, proto, "toLocaleTimeString", 0, nativeDateToLocaleTimeString)
	method(cx, proto, "toISOString", 0, nativeDateToISOString)
	method(cx, proto, "toJSON", 1, nativeDateToJSON)

	toPrimitive := cx.NewNative("[Symbol.toPrimitive]", 1, nativeDateToPrimitive)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToPrimitive), storage.ObjectValue(toPrimitive), engine.FlagConfigurable)
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
		if t, ok := storage.ThisTimeValue(args[0]); ok {
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
	return storage.ObjectValue(cx.NewDateObject(cx.GetPrototypeFromConstructor(nt, engine.IntrinsicDatePrototype), tv))
}

func dateNow(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return storage.Number(cx.SystemUTCEpochMilliseconds())
}

func dateParse(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return storage.Number(datetime.ParsePolled(string(cx.ToString(arg(args, 0))), cx.DefaultTimeZone(), cx.Poll))
}

func dateUTC(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	c := [dateComponentCount]float64{dateDay: 1}
	c[dateYear] = cx.ToNumber(arg(args, 0))
	for i := 1; i < min(len(args), dateComponentCount); i++ {
		c[i] = cx.ToNumber(args[i])
	}
	c[dateYear] = datetime.MakeFullYear(c[dateYear])
	return storage.Number(datetime.TimeClip(makeDate(&c)))
}

var (
	nativeDateGetFullYear        = engine.RegisterNative(dateGetNative("getFullYear", true, datetime.YearFromTime))
	nativeDateGetUTCFullYear     = engine.RegisterNative(dateGetNative("getUTCFullYear", false, datetime.YearFromTime))
	nativeDateGetMonth           = engine.RegisterNative(dateGetNative("getMonth", true, datetime.MonthFromTime))
	nativeDateGetUTCMonth        = engine.RegisterNative(dateGetNative("getUTCMonth", false, datetime.MonthFromTime))
	nativeDateGetDate            = engine.RegisterNative(dateGetNative("getDate", true, datetime.DateFromTime))
	nativeDateGetUTCDate         = engine.RegisterNative(dateGetNative("getUTCDate", false, datetime.DateFromTime))
	nativeDateGetHours           = engine.RegisterNative(dateGetNative("getHours", true, datetime.HourFromTime))
	nativeDateGetUTCHours        = engine.RegisterNative(dateGetNative("getUTCHours", false, datetime.HourFromTime))
	nativeDateGetMinutes         = engine.RegisterNative(dateGetNative("getMinutes", true, datetime.MinFromTime))
	nativeDateGetUTCMinutes      = engine.RegisterNative(dateGetNative("getUTCMinutes", false, datetime.MinFromTime))
	nativeDateGetSeconds         = engine.RegisterNative(dateGetNative("getSeconds", true, datetime.SecFromTime))
	nativeDateGetUTCSeconds      = engine.RegisterNative(dateGetNative("getUTCSeconds", false, datetime.SecFromTime))
	nativeDateGetMilliseconds    = engine.RegisterNative(dateGetNative("getMilliseconds", true, datetime.MsFromTime))
	nativeDateGetUTCMilliseconds = engine.RegisterNative(dateGetNative("getUTCMilliseconds", false, datetime.MsFromTime))
)

// dateGetNative returns the method name of Date.prototype, which returns a component of the date, in
// local time or in UTC.
//
//go:noinline
func dateGetNative(name string, local bool, component func(float64) float64) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisDate(cx, this, name)
		if math.IsNaN(t) {
			return storage.Number(t)
		}
		if local {
			t = datetime.LocalTime(t, cx.DefaultTimeZone())
		}
		return storage.Number(component(t))
	}
}

func dateGetDay(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisDate(cx, this, "getDay")
	if math.IsNaN(t) {
		return storage.Number(t)
	}
	return storage.Number(datetime.WeekDay(datetime.LocalTime(t, cx.DefaultTimeZone())))
}

func dateGetUTCDay(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisDate(cx, this, "getUTCDay")
	if math.IsNaN(t) {
		return storage.Number(t)
	}
	return storage.Number(datetime.WeekDay(t))
}

func dateGetTime(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return storage.Number(thisDate(cx, this, "getTime"))
}

func dateGetTimezoneOffset(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisDate(cx, this, "getTimezoneOffset")
	if math.IsNaN(t) {
		return storage.Number(t)
	}
	return storage.Number((t - datetime.LocalTime(t, cx.DefaultTimeZone())) / 60000)
}

func dateValueOf(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return storage.Number(thisDate(cx, this, "valueOf"))
}

func dateGetYear(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	t := thisDate(cx, this, "getYear")
	if math.IsNaN(t) {
		return storage.Number(t)
	}
	return storage.Number(datetime.YearFromTime(datetime.LocalTime(t, cx.DefaultTimeZone())) - 1900)
}

var (
	nativeDateSetFullYear        = engine.RegisterNative(dateSetNative("setFullYear", true, dateYear, 3))
	nativeDateSetUTCFullYear     = engine.RegisterNative(dateSetNative("setUTCFullYear", false, dateYear, 3))
	nativeDateSetMonth           = engine.RegisterNative(dateSetNative("setMonth", true, dateMonth, 2))
	nativeDateSetUTCMonth        = engine.RegisterNative(dateSetNative("setUTCMonth", false, dateMonth, 2))
	nativeDateSetDate            = engine.RegisterNative(dateSetNative("setDate", true, dateDay, 1))
	nativeDateSetUTCDate         = engine.RegisterNative(dateSetNative("setUTCDate", false, dateDay, 1))
	nativeDateSetHours           = engine.RegisterNative(dateSetNative("setHours", true, dateHours, 4))
	nativeDateSetUTCHours        = engine.RegisterNative(dateSetNative("setUTCHours", false, dateHours, 4))
	nativeDateSetMinutes         = engine.RegisterNative(dateSetNative("setMinutes", true, dateMinutes, 3))
	nativeDateSetUTCMinutes      = engine.RegisterNative(dateSetNative("setUTCMinutes", false, dateMinutes, 3))
	nativeDateSetSeconds         = engine.RegisterNative(dateSetNative("setSeconds", true, dateSeconds, 2))
	nativeDateSetUTCSeconds      = engine.RegisterNative(dateSetNative("setUTCSeconds", false, dateSeconds, 2))
	nativeDateSetMilliseconds    = engine.RegisterNative(dateSetNative("setMilliseconds", true, dateMilliseconds, 1))
	nativeDateSetUTCMilliseconds = engine.RegisterNative(dateSetNative("setUTCMilliseconds", false, dateMilliseconds, 1))
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
			return storage.Number(t)
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
		storage.SetDateValue(this, u)
		return storage.Number(u)
	}
}

func dateSetTime(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	thisDate(cx, this, "setTime")
	t := datetime.TimeClip(cx.ToNumber(arg(args, 0)))
	storage.SetDateValue(this, t)
	return storage.Number(t)
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
	storage.SetDateValue(this, u)
	return storage.Number(u)
}

var (
	nativeDateToString           = engine.RegisterNative(dateStringNative("toString"))
	nativeDateToDateString       = engine.RegisterNative(dateStringNative("toDateString"))
	nativeDateToTimeString       = engine.RegisterNative(dateStringNative("toTimeString"))
	nativeDateToUTCString        = engine.RegisterNative(dateStringNative("toUTCString"))
	nativeDateToLocaleString     = engine.RegisterNative(dateStringNative("toLocaleString"))
	nativeDateToLocaleDateString = engine.RegisterNative(dateStringNative("toLocaleDateString"))
	nativeDateToLocaleTimeString = engine.RegisterNative(dateStringNative("toLocaleTimeString"))
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
	o := storage.ObjectValue(cx.ToObject(this))
	tv := cx.ToPrimitive(o, engine.HintNumber)
	if tv.IsNumber() {
		if n := tv.Number(); math.IsNaN(n) || math.IsInf(n, 0) {
			return storage.Null
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
	return storage.Undefined
}
