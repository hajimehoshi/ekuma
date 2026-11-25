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

// defineDateGetter defines the method name of Date.prototype that returns a component of the date,
// in local time or in UTC.
func defineDateGetter(cx *engine.ExecutionContext, name string, local bool, component func(float64) float64) {
	method(cx, cx.Intrinsic(engine.IntrinsicDatePrototype), wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisDate(cx, this, name)
		if math.IsNaN(t) {
			return engine.Number(t)
		}
		if local {
			t = datetime.LocalTime(t, cx.DefaultTimeZone())
		}
		return engine.Number(component(t))
	})
}

// defineDateSetter defines the method name of Date.prototype that sets the components from first on,
// as many as there are arguments up to count, in local time or in UTC.
func defineDateSetter(cx *engine.ExecutionContext, name string, local bool, first, count int) {
	method(cx, cx.Intrinsic(engine.IntrinsicDatePrototype), wtf8.String(name), count, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
	})
}

func initDate(cx *engine.ExecutionContext) {
	proto := cx.Intrinsic(engine.IntrinsicDatePrototype)

	ctor := constructor(cx, "Date", 7, engine.IntrinsicDatePrototype, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
	})

	now := method(cx, ctor, "now", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(cx.SystemUTCEpochMilliseconds())
	})
	cx.SetMethodKernels(now, shortContract, func(q engine.QuietContext, this engine.Value) (engine.Value, bool) {
		// A host clock can reenter JavaScript and observe the native call frame.
		ms, ok := q.SystemUTCEpochMilliseconds()
		return engine.Number(ms), ok
	}, nil, nil)
	method(cx, ctor, "parse", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(datetime.ParsePolled(string(cx.ToString(arg(args, 0))), cx.DefaultTimeZone(), cx.Poll))
	})
	method(cx, ctor, "UTC", 7, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		c := [dateComponentCount]float64{dateDay: 1}
		c[dateYear] = cx.ToNumber(arg(args, 0))
		for i := 1; i < min(len(args), dateComponentCount); i++ {
			c[i] = cx.ToNumber(args[i])
		}
		c[dateYear] = datetime.MakeFullYear(c[dateYear])
		return engine.Number(datetime.TimeClip(makeDate(&c)))
	})

	// getters defines the methods that return one component of the date in local time and in UTC.
	getters := []struct {
		local, utc string
		component  func(float64) float64
	}{
		{
			local:     "getFullYear",
			utc:       "getUTCFullYear",
			component: datetime.YearFromTime,
		},
		{
			local:     "getMonth",
			utc:       "getUTCMonth",
			component: datetime.MonthFromTime,
		},
		{
			local:     "getDate",
			utc:       "getUTCDate",
			component: datetime.DateFromTime,
		},
		{
			local:     "getHours",
			utc:       "getUTCHours",
			component: datetime.HourFromTime,
		},
		{
			local:     "getMinutes",
			utc:       "getUTCMinutes",
			component: datetime.MinFromTime,
		},
		{
			local:     "getSeconds",
			utc:       "getUTCSeconds",
			component: datetime.SecFromTime,
		},
		{
			local:     "getMilliseconds",
			utc:       "getUTCMilliseconds",
			component: datetime.MsFromTime,
		},
	}
	for _, g := range getters {
		defineDateGetter(cx, g.local, true, g.component)
		defineDateGetter(cx, g.utc, false, g.component)
	}
	method(cx, proto, "getDay", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisDate(cx, this, "getDay")
		if math.IsNaN(t) {
			return engine.Number(t)
		}
		return engine.Number(datetime.WeekDay(datetime.LocalTime(t, cx.DefaultTimeZone())))
	})
	method(cx, proto, "getUTCDay", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisDate(cx, this, "getUTCDay")
		if math.IsNaN(t) {
			return engine.Number(t)
		}
		return engine.Number(datetime.WeekDay(t))
	})
	method(cx, proto, "getTime", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(thisDate(cx, this, "getTime"))
	})
	method(cx, proto, "getTimezoneOffset", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisDate(cx, this, "getTimezoneOffset")
		if math.IsNaN(t) {
			return engine.Number(t)
		}
		return engine.Number((t - datetime.LocalTime(t, cx.DefaultTimeZone())) / 60000)
	})
	method(cx, proto, "valueOf", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(thisDate(cx, this, "valueOf"))
	})
	method(cx, proto, "getYear", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		t := thisDate(cx, this, "getYear")
		if math.IsNaN(t) {
			return engine.Number(t)
		}
		return engine.Number(datetime.YearFromTime(datetime.LocalTime(t, cx.DefaultTimeZone())) - 1900)
	})

	// setters defines the methods that set the components from first on, as many as
	// there are arguments up to count, in local time and in UTC.
	setters := []struct {
		local, utc   string
		first, count int
	}{
		{"setFullYear", "setUTCFullYear", dateYear, 3},
		{"setMonth", "setUTCMonth", dateMonth, 2},
		{"setDate", "setUTCDate", dateDay, 1},
		{"setHours", "setUTCHours", dateHours, 4},
		{"setMinutes", "setUTCMinutes", dateMinutes, 3},
		{"setSeconds", "setUTCSeconds", dateSeconds, 2},
		{"setMilliseconds", "setUTCMilliseconds", dateMilliseconds, 1},
	}
	for _, s := range setters {
		defineDateSetter(cx, s.local, true, s.first, s.count)
		defineDateSetter(cx, s.utc, false, s.first, s.count)
	}
	method(cx, proto, "setTime", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		thisDate(cx, this, "setTime")
		t := datetime.TimeClip(cx.ToNumber(arg(args, 0)))
		engine.SetDateValue(this, t)
		return engine.Number(t)
	})
	method(cx, proto, "setYear", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
	})

	for _, name := range []string{"toString", "toDateString", "toTimeString", "toUTCString", "toLocaleString", "toLocaleDateString", "toLocaleTimeString"} {
		fn := method(cx, proto, wtf8.String(name), 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
			tv := thisDate(cx, this, name)
			if math.IsNaN(tv) {
				return cx.NewString("Invalid Date")
			}
			return cx.StringFromGo(formatDate(cx, name, tv))
		})
		if name == "toUTCString" {
			cx.Define(proto, cx.Intern("toGMTString"), engine.ObjectValue(fn), engine.FlagsHidden)
		}
	}
	method(cx, proto, "toISOString", 0, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		tv := thisDate(cx, this, "toISOString")
		if math.IsNaN(tv) {
			cx.ThrowRangeError("invalid time value")
		}
		return cx.NewString(wtf8.String(datetime.ISOString(tv)))
	})
	method(cx, proto, "toJSON", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		o := engine.ObjectValue(cx.ToObject(this))
		tv := cx.ToPrimitive(o, engine.HintNumber)
		if tv.IsNumber() {
			if n := tv.Number(); math.IsNaN(n) || math.IsInf(n, 0) {
				return engine.Null
			}
		}
		return cx.CallValue(cx.GetV(o, cx.Key(engine.KeyToISOString)), o)
	})

	toPrimitive := cx.NewNative("[Symbol.toPrimitive]", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
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
	})
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToPrimitive), engine.ObjectValue(toPrimitive), engine.FlagConfigurable)
}
