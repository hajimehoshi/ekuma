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
func thisDate(vm *engine.VM, this engine.Value, name string) float64 {
	tv, ok := engine.ThisTimeValue(this)
	if !ok {
		vm.ThrowTypeError("Date.prototype." + name + " called on incompatible receiver " + vm.DisplayString(this))
	}
	return tv
}

// components returns the components of the time value t, which must be finite.
func components(t float64) [dateComponentCount]float64 {
	return [dateComponentCount]float64{
		dateYear:         datetime.YearFromTime(t),
		dateMonth:        datetime.MonthFromTime(t),
		dateDay:          datetime.DateFromTime(t),
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

func initDate(vm *engine.VM) {
	proto := vm.Intrinsic(engine.IntrinsicDatePrototype)

	ctor := constructor(vm, "Date", 7, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := vm.GetNewTarget()
		tz := vm.DefaultTimeZone()
		if nt == nil {
			return vm.StringFromGo(datetime.String(vm.SystemUTCEpochMilliseconds(), tz))
		}
		var tv float64
		switch len(args) {
		case 0:
			tv = vm.SystemUTCEpochMilliseconds()
		case 1:
			if t, ok := engine.ThisTimeValue(args[0]); ok {
				tv = t
				break
			}
			v := vm.ToPrimitive(args[0], engine.HintDefault)
			if v.IsString() {
				tv = datetime.ParsePolled(string(vm.ToString(v)), tz, vm.Poll)
			} else {
				tv = datetime.TimeClip(vm.ToNumber(v))
			}
		default:
			c := [dateComponentCount]float64{dateDay: 1}
			for i := range min(len(args), dateComponentCount) {
				c[i] = vm.ToNumber(args[i])
			}
			c[dateYear] = datetime.MakeFullYear(c[dateYear])
			tv = datetime.TimeClip(datetime.UTC(makeDate(&c), tz))
		}
		return engine.ObjectValue(vm.NewDateObject(vm.GetPrototypeFromConstructor(nt, vm.Intrinsic(engine.IntrinsicDatePrototype)), tv))
	})

	method(vm, ctor, "now", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(vm.SystemUTCEpochMilliseconds())
	})
	method(vm, ctor, "parse", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(datetime.ParsePolled(string(vm.ToString(arg(args, 0))), vm.DefaultTimeZone(), vm.Poll))
	})
	method(vm, ctor, "UTC", 7, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		c := [dateComponentCount]float64{dateDay: 1}
		c[dateYear] = vm.ToNumber(arg(args, 0))
		for i := 1; i < min(len(args), dateComponentCount); i++ {
			c[i] = vm.ToNumber(args[i])
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
		for _, local := range []bool{true, false} {
			name := g.utc
			if local {
				name = g.local
			}
			method(vm, proto, wtf8.String(name), 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
				t := thisDate(vm, this, name)
				if math.IsNaN(t) {
					return engine.Number(t)
				}
				if local {
					t = datetime.LocalTime(t, vm.DefaultTimeZone())
				}
				return engine.Number(g.component(t))
			})
		}
	}
	method(vm, proto, "getDay", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisDate(vm, this, "getDay")
		if math.IsNaN(t) {
			return engine.Number(t)
		}
		return engine.Number(datetime.WeekDay(datetime.LocalTime(t, vm.DefaultTimeZone())))
	})
	method(vm, proto, "getUTCDay", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisDate(vm, this, "getUTCDay")
		if math.IsNaN(t) {
			return engine.Number(t)
		}
		return engine.Number(datetime.WeekDay(t))
	})
	method(vm, proto, "getTime", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(thisDate(vm, this, "getTime"))
	})
	method(vm, proto, "getTimezoneOffset", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisDate(vm, this, "getTimezoneOffset")
		if math.IsNaN(t) {
			return engine.Number(t)
		}
		return engine.Number((t - datetime.LocalTime(t, vm.DefaultTimeZone())) / 60000)
	})
	method(vm, proto, "valueOf", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(thisDate(vm, this, "valueOf"))
	})
	method(vm, proto, "getYear", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisDate(vm, this, "getYear")
		if math.IsNaN(t) {
			return engine.Number(t)
		}
		return engine.Number(datetime.YearFromTime(datetime.LocalTime(t, vm.DefaultTimeZone())) - 1900)
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
		for _, local := range []bool{true, false} {
			name := s.utc
			if local {
				name = s.local
			}
			first, count := s.first, s.count
			method(vm, proto, wtf8.String(name), count, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
				t := thisDate(vm, this, name)
				var vals [dateComponentCount]float64
				n := max(1, min(len(args), count))
				for i := range n {
					vals[i] = vm.ToNumber(arg(args, i))
				}
				tz := vm.DefaultTimeZone()
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
	}
	method(vm, proto, "setTime", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		thisDate(vm, this, "setTime")
		t := datetime.TimeClip(vm.ToNumber(arg(args, 0)))
		engine.SetDateValue(this, t)
		return engine.Number(t)
	})
	method(vm, proto, "setYear", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		t := thisDate(vm, this, "setYear")
		y := vm.ToNumber(arg(args, 0))
		tz := vm.DefaultTimeZone()
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

	// formats defines the methods that return a string, or "Invalid Date" for an invalid date.
	formats := []struct {
		name   string
		format func(tv float64, tz datetime.TimeZone) string
	}{
		{"toString", datetime.String},
		{"toDateString", datetime.DateString},
		{"toTimeString", datetime.TimeString},
		{"toUTCString", func(tv float64, tz datetime.TimeZone) string { return datetime.UTCString(tv) }},
		{"toLocaleString", datetime.LocaleString},
		{"toLocaleDateString", datetime.LocaleDateString},
		{"toLocaleTimeString", datetime.LocaleTimeString},
	}
	for _, f := range formats {
		fn := method(vm, proto, wtf8.String(f.name), 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			tv := thisDate(vm, this, f.name)
			if math.IsNaN(tv) {
				return vm.NewString("Invalid Date")
			}
			return vm.StringFromGo(f.format(tv, vm.DefaultTimeZone()))
		})
		if f.name == "toUTCString" {
			vm.Define(proto, vm.Intern("toGMTString"), engine.ObjectValue(fn), engine.FlagsHidden)
		}
	}
	method(vm, proto, "toISOString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		tv := thisDate(vm, this, "toISOString")
		if math.IsNaN(tv) {
			vm.ThrowRangeError("invalid time value")
		}
		return vm.NewString(wtf8.String(datetime.ISOString(tv)))
	})
	toISOString := vm.Intern("toISOString")
	method(vm, proto, "toJSON", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := engine.ObjectValue(vm.ToObject(this))
		tv := vm.ToPrimitive(o, engine.HintNumber)
		if tv.IsNumber() {
			if n := tv.Number(); math.IsNaN(n) || math.IsInf(n, 0) {
				return engine.Null
			}
		}
		return vm.CallValue(vm.GetV(o, toISOString), o)
	})

	toPrimitive := vm.NewNative("[Symbol.toPrimitive]", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		o := thisObject(vm, this, "Date.prototype[Symbol.toPrimitive]")
		h := arg(args, 0)
		if h.IsString() {
			switch h.Str() {
			case "string", "default":
				return vm.OrdinaryToPrimitive(o, engine.HintString)
			case "number":
				return vm.OrdinaryToPrimitive(o, engine.HintNumber)
			}
		}
		vm.ThrowTypeError("invalid hint: " + vm.DisplayString(h))
		return engine.Undefined
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToPrimitive), engine.ObjectValue(toPrimitive), engine.FlagConfigurable)
}
