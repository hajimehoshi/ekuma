// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"math"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/intl/date"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/localetag"
	"github.com/hajimehoshi/ekuma/internal/temporal"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// dateTimeFormatData holds the internal slots of an Intl.DateTimeFormat object.
type dateTimeFormatData struct {
	locale   string
	calendar string
	nu       string
	// timeZone is [[TimeZone]], and tz the time zone it identifies.
	timeZone string
	tz       temporal.TimeZone
	// hourCycle is [[HourCycle]], or empty when it is undefined.
	hourCycle string
	dateStyle string
	timeStyle string

	format *date.Format
	// The formats of the Temporal types, nil for null.
	plainDate      *date.Format
	plainYearMonth *date.Format
	plainMonthDay  *date.Format
	plainTime      *date.Format
	plainDateTime  *date.Format
	instant        *date.Format

	formatters  map[*date.Format]*date.Formatter
	boundFormat *engine.Object
}

// calendarsOf returns the calendars that DateTimeFormat supports for the available locale l,
// its default first.
func calendarsOf(l string) []string {
	t, _ := locale.Parse(l)
	def := "gregory"
	if cals := locale.CalendarsOf(t); len(cals) > 0 {
		def = cals[0]
	}
	// "islamic" stands for "islamic-tbla".
	return append(append([]string{def}, locale.Calendars...), "islamic")
}

// resolvedCalendar returns the calendar that DateTimeFormat uses for the calendar cal of its
// locale.
func resolvedCalendar(cal string) string {
	if cal == "islamic" {
		return "islamic-tbla"
	}
	return cal
}

// hourCyclesOf returns the values of the key "hc": null, which is an empty string, first.
func hourCyclesOf(string) []string {
	return []string{"", "h11", "h12", "h23", "h24"}
}

// typeOption reads the option name, which must match the type production of a Unicode locale
// identifier.
func typeOption(vm *engine.VM, options *engine.Object, name string) (string, bool) {
	s, ok := getOption(vm, options, name)
	if !ok {
		return "", false
	}
	if !localetag.IsType(wtf8.ToLowerASCII(s)) {
		vm.ThrowRangeError(vm.DisplayString(vm.NewString(s)) + " is not a valid value for " + name)
	}
	return s, true
}

// componentOptions are the options of the components in the order of Table 16, with their
// values.
var componentOptions = []struct {
	name   string
	values []string
}{
	{"weekday", []string{"narrow", "short", "long"}},
	{"era", []string{"narrow", "short", "long"}},
	{"year", []string{"2-digit", "numeric"}},
	{"month", []string{"2-digit", "numeric", "narrow", "short", "long"}},
	{"day", []string{"2-digit", "numeric"}},
	{"dayPeriod", []string{"narrow", "short", "long"}},
	{"hour", []string{"2-digit", "numeric"}},
	{"minute", []string{"2-digit", "numeric"}},
	{"second", []string{"2-digit", "numeric"}},
	{"fractionalSecondDigits", nil},
	{"timeZoneName", []string{"short", "long", "shortOffset", "longOffset", "shortGeneric", "longGeneric"}},
}

// createDateTimeFormat implements CreateDateTimeFormat after OrdinaryCreateFromConstructor.
// toLocaleStringTZ is the time zone of Temporal.ZonedDateTime.prototype.toLocaleString, or
// nil.
func createDateTimeFormat(vm *engine.VM, locales, opts engine.Value, required, defaults string, toLocaleStringTZ *temporal.TimeZone) *dateTimeFormatData {
	requested := canonicalizeLocaleList(vm, locales)
	options := optionsObject(vm, opts)
	bestFit := localeMatcher(vm, options)
	cal, hasCal := typeOption(vm, options, "calendar")
	nu, hasNu := typeOption(vm, options, "numberingSystem")
	var hour12 engine.Value = vm.Get(options, engine.NamedKey(vm.Intern("hour12")))
	if !hour12.IsUndefined() {
		hour12 = engine.Bool(engine.ToBoolean(hour12))
	}
	hourCycle, hasHourCycle := getOption(vm, options, "hourCycle", "h11", "h12", "h23", "h24")
	hcKey := locale.ExtensionKey{
		Key:       "hc",
		Option:    hourCycle,
		HasOption: hasHourCycle,
		Values:    hourCyclesOf,
	}
	if !hour12.IsUndefined() {
		hcKey.Option = ""
		hcKey.HasOption = true
		hcKey.OptionNull = true
	}
	res := locale.ResolveLocale(requested, defaultLocale(vm), bestFit, []locale.ExtensionKey{{
		Key:       "ca",
		Option:    cal,
		HasOption: hasCal,
		Values:    calendarsOf,
	}, hcKey, {
		Key:       "nu",
		Option:    nu,
		HasOption: hasNu,
		Values:    numberingSystemsOf,
	}})
	dtf := &dateTimeFormatData{
		locale:     res.Locale,
		calendar:   resolvedCalendar(res.Values[0]),
		nu:         res.Values[2],
		formatters: map[*date.Format]*date.Formatter{},
	}
	def, hc12, hc24 := date.HourCycles(res.Locale)
	hc := res.Values[1]
	switch {
	case hour12.IsUndefined():
		if hc == "" {
			hc = def
		}
	case engine.ToBoolean(hour12):
		hc = hc12
	default:
		hc = hc24
	}

	tzv := vm.Get(options, engine.NamedKey(vm.Intern("timeZone")))
	switch {
	case tzv.IsUndefined() && toLocaleStringTZ != nil:
		dtf.tz = *toLocaleStringTZ
	case tzv.IsUndefined():
		dtf.tz = builtin.SystemTimeZone(vm)
	default:
		if toLocaleStringTZ != nil {
			vm.ThrowTypeError("the timeZone option is not allowed in Temporal.ZonedDateTime.prototype.toLocaleString")
		}
		name := vm.ToString(tzv)
		id, offset, isOffset, err := temporal.ParseTimeZoneIdentifier(name, vm.Poll)
		switch {
		case err != nil:
			vm.ThrowRangeError("invalid time zone: " + vm.DisplayString(vm.NewString(name)))
		case isOffset:
			dtf.tz = temporal.OffsetTimeZone(offset)
		default:
			z, ok := temporal.GetAvailableNamedTimeZone(id, builtin.SystemTimeZone(vm))
			if !ok {
				vm.ThrowRangeError("invalid time zone: " + vm.DisplayString(vm.NewString(name)))
			}
			dtf.tz = z
		}
	}
	dtf.timeZone = dtf.tz.ID()

	var comps date.Components
	hasExplicit := false
	for _, o := range componentOptions {
		var v string
		if o.name == "fractionalSecondDigits" {
			if n := getNumberOption(vm, options, o.name, 1, 3, -1); n > 0 {
				comps.FractionalSecondDigits = n
				hasExplicit = true
			}
			continue
		}
		v, _ = getOption(vm, options, o.name, o.values...)
		if v != "" {
			hasExplicit = true
		}
		setComponent(&comps, o.name, v)
	}
	// The basic format matcher is the best fit one.
	getStringOption(vm, options, "formatMatcher", "best fit", "basic", "best fit")
	dtf.dateStyle, _ = getOption(vm, options, "dateStyle", "full", "long", "medium", "short")
	dtf.timeStyle, _ = getOption(vm, options, "timeStyle", "full", "long", "medium", "short")

	l, c := dtf.locale, dtf.calendar
	if dtf.dateStyle != "" || dtf.timeStyle != "" {
		if hasExplicit {
			vm.ThrowTypeError("dateStyle and timeStyle cannot be used with date and time components")
		}
		if required == date.Date && dtf.timeStyle != "" {
			vm.ThrowTypeError("timeStyle is not allowed for a date")
		}
		if required == date.Time && dtf.dateStyle != "" {
			vm.ThrowTypeError("dateStyle is not allowed for a time")
		}
		best := date.NewStyleFormat(l, c, hc, dtf.dateStyle, dtf.timeStyle)
		if dtf.dateStyle != "" {
			dtf.plainDate = best.Adjust(hc, "weekday", "era", "year", "month", "day")
			dtf.plainYearMonth = best.Adjust(hc, "era", "year", "month")
			dtf.plainMonthDay = best.Adjust(hc, "month", "day")
		}
		if dtf.timeStyle != "" {
			dtf.plainTime = best.Adjust(hc, "dayPeriod", "hour", "minute", "second", "fractionalSecondDigits")
		}
		dtf.plainDateTime = best.Adjust(hc, "weekday", "era", "year", "month", "day", "dayPeriod", "hour", "minute", "second", "fractionalSecondDigits")
		dtf.instant = best
		dtf.format = best
	} else {
		format := func(required, defaults, inherit string) *date.Format {
			fc, ok := date.TemporalComponents(comps, required, defaults, inherit)
			if !ok {
				return nil
			}
			return date.NewFormat(l, c, hc, fc)
		}
		dtf.format = format(required, defaults, date.InheritAll)
		dtf.plainDate = format(date.Date, date.Date, date.InheritRelevant)
		dtf.plainYearMonth = format(date.YearMonth, date.YearMonth, date.InheritRelevant)
		dtf.plainMonthDay = format(date.MonthDay, date.MonthDay, date.InheritRelevant)
		dtf.plainTime = format(date.Time, date.Time, date.InheritRelevant)
		dtf.plainDateTime = format(date.Any, date.All, date.InheritRelevant)
		if toLocaleStringTZ != nil {
			dtf.instant = format(date.Any, date.ZonedDateTime, date.InheritAll)
		} else {
			dtf.instant = format(date.Any, date.All, date.InheritAll)
		}
	}
	if dtf.format.HasHour() {
		dtf.hourCycle = hc
	}
	return dtf
}

// setComponent sets the component name of c to v.
func setComponent(c *date.Components, name, v string) {
	switch name {
	case "weekday":
		c.Weekday = v
	case "era":
		c.Era = v
	case "year":
		c.Year = v
	case "month":
		c.Month = v
	case "day":
		c.Day = v
	case "dayPeriod":
		c.DayPeriod = v
	case "hour":
		c.Hour = v
	case "minute":
		c.Minute = v
	case "second":
		c.Second = v
	case "timeZoneName":
		c.TimeZoneName = v
	}
}

// formatter returns the formatter of the format f.
func (dtf *dateTimeFormatData) formatter(f *date.Format) *date.Formatter {
	fm, ok := dtf.formatters[f]
	if !ok {
		fm = date.NewFormatter(f, dtf.nu, dtf.tz)
		dtf.formatters[f] = fm
	}
	return fm
}

// temporalKind is a type of the Temporal objects that DateTimeFormat formats.
type temporalKind int

const (
	temporalNone temporalKind = iota
	temporalPlainDate
	temporalPlainYearMonth
	temporalPlainMonthDay
	temporalPlainTime
	temporalPlainDateTime
	temporalZonedDateTime
	temporalInstant
)

// temporalValue is the internal slots of a Temporal object that DateTimeFormat formats.
type temporalValue struct {
	kind temporalKind
	// dateTime is the date and time of a plain object.
	dateTime temporal.ISODateTime
	calendar string
	// epochNs is the instant of a ZonedDateTime or an Instant.
	epochNs  temporal.Int128
	timeZone temporal.TimeZone
}

// temporalValueOf implements IsTemporalObject: it returns the internal slots of v, and
// reports whether v is a Temporal object that DateTimeFormat formats.
func temporalValueOf(v engine.Value) (temporalValue, bool) {
	if d, cal, ok := builtin.TemporalPlainDateOf(v); ok {
		return temporalValue{
			kind: temporalPlainDate,
			dateTime: temporal.ISODateTime{
				Date: d,
			},
			calendar: cal,
		}, true
	}
	if d, cal, ok := builtin.TemporalPlainYearMonthOf(v); ok {
		return temporalValue{
			kind: temporalPlainYearMonth,
			dateTime: temporal.ISODateTime{
				Date: d,
			},
			calendar: cal,
		}, true
	}
	if d, cal, ok := builtin.TemporalPlainMonthDayOf(v); ok {
		return temporalValue{
			kind: temporalPlainMonthDay,
			dateTime: temporal.ISODateTime{
				Date: d,
			},
			calendar: cal,
		}, true
	}
	if t, ok := builtin.TemporalPlainTimeOf(v); ok {
		return temporalValue{
			kind: temporalPlainTime,
			dateTime: temporal.ISODateTime{
				Date: temporal.ISODate{
					Year:  1970,
					Month: 1,
					Day:   1,
				},
				Time: t,
			},
		}, true
	}
	if dt, cal, ok := builtin.TemporalPlainDateTimeOf(v); ok {
		return temporalValue{
			kind:     temporalPlainDateTime,
			dateTime: dt,
			calendar: cal,
		}, true
	}
	if ns, tz, cal, ok := builtin.TemporalZonedDateTimeOf(v); ok {
		return temporalValue{
			kind:     temporalZonedDateTime,
			calendar: cal,
			epochNs:  ns,
			timeZone: tz,
		}, true
	}
	if ns, ok := builtin.TemporalInstantOf(v); ok {
		return temporalValue{
			kind:    temporalInstant,
			epochNs: ns,
		}, true
	}
	return temporalValue{}, false
}

// formattable is a value that DateTimeFormat formats: a time value or a Temporal object.
type formattable struct {
	t        float64
	temporal temporalValue
	// isTemporal reports whether the value is a Temporal object.
	isTemporal bool
}

// toDateTimeFormattable implements ToDateTimeFormattable.
func toDateTimeFormattable(vm *engine.VM, v engine.Value) formattable {
	if tv, ok := temporalValueOf(v); ok {
		return formattable{
			temporal:   tv,
			isTemporal: true,
		}
	}
	return formattable{
		t: vm.ToNumber(v),
	}
}

// dateArg implements the conversion of the argument date of format and formatToParts.
func dateArg(vm *engine.VM, v engine.Value) formattable {
	if v.IsUndefined() {
		return formattable{
			t: vm.SystemUTCEpochMilliseconds(),
		}
	}
	return toDateTimeFormattable(vm, v)
}

// epochMilliseconds returns the epoch nanoseconds ns in milliseconds, rounded toward negative
// infinity.
func epochMilliseconds(ns temporal.Int128) int64 {
	q, _ := ns.FloorDivInt64(temporal.NsPerMillisecond)
	ms, _ := q.Int64()
	return ms
}

// handleDateTimeValue implements HandleDateTimeValue, returning the format, the time in
// milliseconds since the epoch and whether it is a plain time.
func (dtf *dateTimeFormatData) handleDateTimeValue(vm *engine.VM, x formattable) (*date.Format, int64, bool) {
	if !x.isTemporal {
		t := datetime.TimeClip(x.t)
		if math.IsNaN(t) {
			vm.ThrowRangeError("invalid time value")
		}
		return dtf.format, int64(t), false
	}
	tv := x.temporal
	var f *date.Format
	switch tv.kind {
	case temporalPlainDate:
		if tv.calendar != dtf.calendar && tv.calendar != temporal.ISO8601 {
			vm.ThrowRangeError("the calendar of the Temporal.PlainDate differs from that of the DateTimeFormat")
		}
		tv.dateTime.Time = temporal.Time{Hour: 12}
		f = dtf.plainDate
	case temporalPlainYearMonth:
		if tv.calendar != dtf.calendar {
			vm.ThrowRangeError("the calendar of the Temporal.PlainYearMonth differs from that of the DateTimeFormat")
		}
		tv.dateTime.Time = temporal.Time{Hour: 12}
		f = dtf.plainYearMonth
	case temporalPlainMonthDay:
		if tv.calendar != dtf.calendar {
			vm.ThrowRangeError("the calendar of the Temporal.PlainMonthDay differs from that of the DateTimeFormat")
		}
		tv.dateTime.Time = temporal.Time{Hour: 12}
		f = dtf.plainMonthDay
	case temporalPlainTime:
		f = dtf.plainTime
	case temporalPlainDateTime:
		if tv.calendar != dtf.calendar && tv.calendar != temporal.ISO8601 {
			vm.ThrowRangeError("the calendar of the Temporal.PlainDateTime differs from that of the DateTimeFormat")
		}
		f = dtf.plainDateTime
	case temporalInstant:
		return dtf.instant, epochMilliseconds(tv.epochNs), false
	default:
		vm.ThrowTypeError("Intl.DateTimeFormat cannot format a Temporal.ZonedDateTime")
	}
	if f == nil {
		vm.ThrowTypeError("the DateTimeFormat has no format for the Temporal object")
	}
	return f, epochMilliseconds(tv.dateTime.UTCEpochNanoseconds()), true
}

// partitionDateTimePattern implements PartitionDateTimePattern.
func (dtf *dateTimeFormatData) partitionDateTimePattern(vm *engine.VM, x formattable) []date.Part {
	f, t, plain := dtf.handleDateTimeValue(vm, x)
	return dtf.formatter(f).Format(t, plain)
}

// formatDateTime implements FormatDateTime.
func (dtf *dateTimeFormatData) formatDateTime(vm *engine.VM, x formattable) engine.Value {
	var b strings.Builder
	for _, p := range dtf.partitionDateTimePattern(vm, x) {
		b.WriteString(p.Value)
	}
	return vm.NewString(b.String())
}

// partitionDateTimeRangePattern implements PartitionDateTimeRangePattern.
func (dtf *dateTimeFormatData) partitionDateTimeRangePattern(vm *engine.VM, x, y formattable) []date.RangePart {
	if x.isTemporal || y.isTemporal {
		if !x.isTemporal || !y.isTemporal || x.temporal.kind != y.temporal.kind {
			vm.ThrowTypeError("formatRange requires two Temporal objects of the same type")
		}
	}
	f, tx, plain := dtf.handleDateTimeValue(vm, x)
	_, ty, _ := dtf.handleDateTimeValue(vm, y)
	return dtf.formatter(f).FormatRange(tx, ty, plain)
}

// newDateTimeFormat implements the Intl.DateTimeFormat constructor.
func (r *realm) newDateTimeFormat(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
	nt := vm.GetNewTarget()
	newTarget := nt
	if newTarget == nil {
		newTarget = r.dateTimeFormatCtor
	}
	proto := protoFor(vm, newTarget, func(r *realm) *engine.Object { return r.dateTimeFormatProto })
	dtf := vm.NewIntlObject(proto, createDateTimeFormat(vm, arg(args, 0), arg(args, 1), date.Any, date.Date, nil))
	// ChainDateTimeFormat.
	if nt == nil && vm.OrdinaryHasInstance(engine.ObjectValue(r.dateTimeFormatCtor), this) {
		var d engine.PropDesc
		d.SetValue(engine.ObjectValue(dtf))
		d.SetWritable(false)
		d.SetEnumerable(false)
		d.SetConfigurable(false)
		vm.DefinePropertyOrThrow(this.Object(), engine.NamedKey(r.fallbackSymbol), d)
		return this
	}
	return engine.ObjectValue(dtf)
}

// unwrapDateTimeFormat implements UnwrapDateTimeFormat followed by the check of the internal
// slot.
func (r *realm) unwrapDateTimeFormat(vm *engine.VM, this engine.Value, name string) *dateTimeFormatData {
	if !this.IsObject() {
		vm.ThrowTypeError("Intl.DateTimeFormat.prototype." + name + " called on a value that is not an object")
	}
	if dtf, ok := engine.IntlData(this).(*dateTimeFormatData); ok {
		return dtf
	}
	if vm.OrdinaryHasInstance(engine.ObjectValue(r.dateTimeFormatCtor), this) {
		if dtf, ok := engine.IntlData(vm.Get(this.Object(), engine.NamedKey(r.fallbackSymbol))).(*dateTimeFormatData); ok {
			return dtf
		}
	}
	vm.ThrowTypeError("Intl.DateTimeFormat.prototype." + name + " called on an object that is not an Intl.DateTimeFormat")
	return nil
}

// thisDateTimeFormat implements RequireInternalSlot for an Intl.DateTimeFormat.
func thisDateTimeFormat(vm *engine.VM, this engine.Value, name string) *dateTimeFormatData {
	dtf, ok := engine.IntlData(this).(*dateTimeFormatData)
	if !ok {
		vm.ThrowTypeError("Intl.DateTimeFormat.prototype." + name + " called on an object that is not an Intl.DateTimeFormat")
	}
	return dtf
}

// rangeArgs implements the checks and conversions of the arguments of formatRange and
// formatRangeToParts.
func rangeArgs(vm *engine.VM, args []engine.Value) (formattable, formattable) {
	start, end := arg(args, 0), arg(args, 1)
	if start.IsUndefined() || end.IsUndefined() {
		vm.ThrowTypeError("formatRange requires a start and an end")
	}
	return toDateTimeFormattable(vm, start), toDateTimeFormattable(vm, end)
}

// initDateTimeFormat defines Intl.DateTimeFormat.
func (r *realm) initDateTimeFormat(vm *engine.VM) {
	proto := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	ctor := vm.NewNativeConstructor("DateTimeFormat", 0, r.newDateTimeFormat)
	vm.Define(ctor, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(ctor), engine.FlagsHidden)
	vm.Define(r.ns, vm.Intern("DateTimeFormat"), engine.ObjectValue(ctor), engine.FlagsHidden)
	r.dateTimeFormatProto = proto
	r.dateTimeFormatCtor = ctor

	method(vm, ctor, "supportedLocalesOf", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return supportedLocalesOf(vm, args)
	})
	method(vm, proto, "resolvedOptions", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return r.unwrapDateTimeFormat(vm, this, "resolvedOptions").resolvedOptions(vm)
	})
	getter(vm, proto, "format", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		dtf := r.unwrapDateTimeFormat(vm, this, "format")
		if dtf.boundFormat == nil {
			dtf.boundFormat = vm.NewNative("", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
				return dtf.formatDateTime(vm, dateArg(vm, arg(args, 0)))
			})
		}
		return engine.ObjectValue(dtf.boundFormat)
	})
	method(vm, proto, "formatToParts", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		dtf := thisDateTimeFormat(vm, this, "formatToParts")
		parts := dtf.partitionDateTimePattern(vm, dateArg(vm, arg(args, 0)))
		return partsArray(vm, len(parts), func(i int) (string, string, string) {
			return parts[i].Type, parts[i].Value, ""
		})
	})
	method(vm, proto, "formatRange", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		dtf := thisDateTimeFormat(vm, this, "formatRange")
		x, y := rangeArgs(vm, args)
		var b strings.Builder
		for _, p := range dtf.partitionDateTimeRangePattern(vm, x, y) {
			b.WriteString(p.Value)
		}
		return vm.NewString(b.String())
	})
	method(vm, proto, "formatRangeToParts", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		dtf := thisDateTimeFormat(vm, this, "formatRangeToParts")
		x, y := rangeArgs(vm, args)
		parts := dtf.partitionDateTimeRangePattern(vm, x, y)
		return partsArray(vm, len(parts), func(i int) (string, string, string) {
			return parts[i].Type, parts[i].Value, parts[i].Source
		})
	})
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Intl.DateTimeFormat"), engine.FlagConfigurable)
}

// resolvedOptions implements Intl.DateTimeFormat.prototype.resolvedOptions after the check of
// the internal slot.
func (dtf *dateTimeFormatData) resolvedOptions(vm *engine.VM) engine.Value {
	out := vm.NewObject(vm.Intrinsic(engine.IntrinsicObjectPrototype))
	set := func(name string, v engine.Value) {
		vm.CreateDataPropertyOrThrow(out, engine.NamedKey(vm.Intern(name)), v)
	}
	str := func(name, v string) {
		if v != "" {
			set(name, vm.NewString(v))
		}
	}
	str("locale", dtf.locale)
	str("calendar", dtf.calendar)
	str("numberingSystem", dtf.nu)
	str("timeZone", dtf.timeZone)
	if dtf.hourCycle != "" {
		str("hourCycle", dtf.hourCycle)
		set("hour12", engine.Bool(dtf.hourCycle == "h11" || dtf.hourCycle == "h12"))
	}
	if dtf.dateStyle == "" && dtf.timeStyle == "" {
		c := dtf.format.Components()
		str("weekday", c.Weekday)
		str("era", c.Era)
		str("year", c.Year)
		str("month", c.Month)
		str("day", c.Day)
		str("dayPeriod", c.DayPeriod)
		str("hour", c.Hour)
		str("minute", c.Minute)
		str("second", c.Second)
		if c.FractionalSecondDigits > 0 {
			set("fractionalSecondDigits", engine.Number(float64(c.FractionalSecondDigits)))
		}
		str("timeZoneName", c.TimeZoneName)
	}
	str("dateStyle", dtf.dateStyle)
	str("timeStyle", dtf.timeStyle)
	return engine.ObjectValue(out)
}

// initTemporalToLocaleString replaces the toLocaleString methods of the Temporal types with
// the versions of ECMA-402. It replaces the namespace Temporal of
// the current realm with one that gets the properties of the original when it is first
// needed, and replaces the methods then. Date.prototype.toTemporalInstant, which creates
// Temporal objects without the namespace, makes the namespace get its properties first.
func initTemporalToLocaleString(vm *engine.VM) {
	global := vm.GlobalObject().Object()
	key := engine.NamedKey(vm.Intern("Temporal"))
	orig := vm.Get(global, key)
	if !orig.IsObject() {
		return
	}
	ns := vm.NewLazyObject(vm.Intrinsic(engine.IntrinsicObjectPrototype), func(ns *engine.Object) {
		o := orig.Object()
		for _, k := range vm.OwnKeys(o) {
			d, _ := vm.GetOwnProperty(o, k)
			vm.DefineOwnProperty(ns, k, d)
		}
		for _, c := range []struct {
			name string
			kind temporalKind
		}{
			{"Instant", temporalInstant},
			{"PlainDate", temporalPlainDate},
			{"PlainDateTime", temporalPlainDateTime},
			{"PlainMonthDay", temporalPlainMonthDay},
			{"PlainTime", temporalPlainTime},
			{"PlainYearMonth", temporalPlainYearMonth},
			{"ZonedDateTime", temporalZonedDateTime},
		} {
			ctor := vm.Get(ns, engine.NamedKey(vm.Intern(c.name)))
			proto := vm.Get(ctor.Object(), engine.NamedKey(vm.Key(engine.KeyPrototype))).Object()
			method(vm, proto, "toLocaleString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
				tv, ok := temporalValueOf(this)
				if !ok || tv.kind != c.kind {
					vm.ThrowTypeError("Temporal." + c.name + ".prototype.toLocaleString called on an object that is not a Temporal." + c.name)
				}
				return formatTemporalLocaleString(vm, tv, arg(args, 0), arg(args, 1))
			})
		}
		ctor := vm.Get(ns, engine.NamedKey(vm.Intern("Duration")))
		proto := vm.Get(ctor.Object(), engine.NamedKey(vm.Key(engine.KeyPrototype))).Object()
		method(vm, proto, "toLocaleString", 0, durationToLocaleString)
	})
	vm.Define(global, vm.Intern("Temporal"), engine.ObjectValue(ns), engine.FlagsHidden)
	dateProto := vm.Intrinsic(engine.IntrinsicDatePrototype)
	toInstant := vm.Get(dateProto, engine.NamedKey(vm.Intern("toTemporalInstant")))
	if !toInstant.IsObject() {
		return
	}
	method(vm, dateProto, "toTemporalInstant", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		vm.InitLazyObject(ns)
		return vm.CallValue(toInstant, this, args...)
	})
}

// formatTemporalLocaleString implements the toLocaleString method of the Temporal object tv.
func formatTemporalLocaleString(vm *engine.VM, tv temporalValue, locales, options engine.Value) engine.Value {
	switch tv.kind {
	case temporalPlainDate, temporalPlainYearMonth, temporalPlainMonthDay:
		dtf := createDateTimeFormat(vm, locales, options, date.Date, date.Date, nil)
		return dtf.formatDateTime(vm, formattable{temporal: tv, isTemporal: true})
	case temporalPlainTime:
		dtf := createDateTimeFormat(vm, locales, options, date.Time, date.Time, nil)
		return dtf.formatDateTime(vm, formattable{temporal: tv, isTemporal: true})
	case temporalZonedDateTime:
		tz := tv.timeZone
		dtf := createDateTimeFormat(vm, locales, options, date.Any, date.All, &tz)
		if tv.calendar != temporal.ISO8601 && tv.calendar != dtf.calendar {
			vm.ThrowRangeError("the calendar of the Temporal.ZonedDateTime differs from that of the DateTimeFormat")
		}
		return dtf.formatDateTime(vm, formattable{
			temporal: temporalValue{
				kind:    temporalInstant,
				epochNs: tv.epochNs,
			},
			isTemporal: true,
		})
	}
	dtf := createDateTimeFormat(vm, locales, options, date.Any, date.All, nil)
	return dtf.formatDateTime(vm, formattable{temporal: tv, isTemporal: true})
}

// initDateToLocaleString replaces the toLocaleString, toLocaleDateString and
// toLocaleTimeString of Date.prototype with the versions of ECMA-402.
func initDateToLocaleString(vm *engine.VM) {
	for _, m := range []struct {
		name               string
		required, defaults string
	}{
		{"toLocaleString", date.Any, date.All},
		{"toLocaleDateString", date.Date, date.Date},
		{"toLocaleTimeString", date.Time, date.Time},
	} {
		method(vm, vm.Intrinsic(engine.IntrinsicDatePrototype), m.name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			t, ok := engine.ThisTimeValue(this)
			if !ok {
				vm.ThrowTypeError("Date.prototype." + m.name + " requires that 'this' be a Date")
			}
			if math.IsNaN(t) {
				return vm.NewString("Invalid Date")
			}
			dtf := createDateTimeFormat(vm, arg(args, 0), arg(args, 1), m.required, m.defaults, nil)
			return dtf.formatDateTime(vm, formattable{
				t: t,
			})
		})
	}
}
