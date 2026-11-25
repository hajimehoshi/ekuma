// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"errors"
	"math"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/temporal"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// The internal slots of the objects of the Temporal types.
type (
	instantSlots struct {
		ns temporal.Int128
	}
	durationSlots struct {
		d temporal.Duration
	}
	plainDateSlots struct {
		date temporal.ISODate
		cal  string
	}
	plainTimeSlots struct {
		t temporal.Time
	}
	plainDateTimeSlots struct {
		dt  temporal.ISODateTime
		cal string
	}
	plainYearMonthSlots struct {
		date temporal.ISODate
		cal  string
	}
	plainMonthDaySlots struct {
		date temporal.ISODate
		cal  string
	}
	zonedDateTimeSlots struct {
		ns  temporal.Int128
		tz  temporal.TimeZone
		cal string
	}
)

// throwTemporal throws err, an error of the temporal package, as the error it stands for.
func throwTemporal(cx *engine.ExecutionContext, err error) {
	if te, ok := errors.AsType[*temporal.Error](err); ok && te.Kind == temporal.TypeError {
		cx.ThrowTypeError(te.Message)
	}
	cx.ThrowRangeError(err.Error())
}

// temporalFailure carries an error of the temporal package out of a Temporal function to
// the wrapper of [temporalNative], which throws it.
type temporalFailure struct {
	err error
}

// must returns v, or makes the running Temporal function throw err when it is not nil.
func must[T any](v T, err error) T {
	if err != nil {
		panic(temporalFailure{
			err: err,
		})
	}
	return v
}

// must2 is must for functions with two results.
func must2[T, U any](t T, u U, err error) (T, U) {
	if err != nil {
		panic(temporalFailure{
			err: err,
		})
	}
	return t, u
}

// check makes the running Temporal function throw err when it is not nil.
func check(err error) {
	if err != nil {
		panic(temporalFailure{
			err: err,
		})
	}
}

// temporalNative returns f as a function that throws the errors that must and check report.
//
//go:noinline
func temporalNative(f engine.NativeFunc) engine.NativeFunc {
	return func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		defer func() {
			if e := recover(); e != nil {
				if tf, ok := e.(temporalFailure); ok {
					throwTemporal(cx, tf.err)
				}
				panic(e)
			}
		}()
		return f(cx, this, args)
	}
}

// get returns the property name of o.
func get(cx *engine.ExecutionContext, o *engine.Object, name string) engine.Value {
	return cx.Get(o, engine.NamedKey(cx.Intern(wtf8.String(name))))
}

// requireString returns v as a Go string, or throws a TypeError when it is not a String.
func requireString(cx *engine.ExecutionContext, v engine.Value, what string) string {
	if !v.IsString() {
		cx.ThrowTypeError(what + " must be a string, not " + cx.DisplayString(v))
	}
	return wtf8.ToValidUTF8(cx.ToString(v))
}

// toIntegerWithTruncation implements ToIntegerWithTruncation.
func toIntegerWithTruncation(cx *engine.ExecutionContext, v engine.Value, what string) float64 {
	n := cx.ToNumber(v)
	if math.IsNaN(n) || math.IsInf(n, 0) {
		cx.ThrowRangeError(what + " must be a finite number")
	}
	return math.Trunc(n) + 0
}

// toPositiveIntegerWithTruncation implements ToPositiveIntegerWithTruncation.
func toPositiveIntegerWithTruncation(cx *engine.ExecutionContext, v engine.Value, what string) float64 {
	n := toIntegerWithTruncation(cx, v, what)
	if n <= 0 {
		cx.ThrowRangeError(what + " must be positive")
	}
	return n
}

// toIntegerIfIntegral implements ToIntegerIfIntegral.
func toIntegerIfIntegral(cx *engine.ExecutionContext, v engine.Value, what string) float64 {
	n := cx.ToNumber(v)
	if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
		cx.ThrowRangeError(what + " must be an integer")
	}
	return n + 0
}

// getOptionsObject implements GetOptionsObject.
func getOptionsObject(cx *engine.ExecutionContext, v engine.Value) *engine.Object {
	if v.IsUndefined() {
		return cx.NewObject(nil)
	}
	if v.IsObject() {
		return v.Object()
	}
	cx.ThrowTypeError("options must be an object, not " + cx.DisplayString(v))
	return nil
}

// temporalOption implements GetOption for a String option whose allowed values are values. It
// returns the index of the value, or fallback when the option is undefined. A negative
// fallback makes the option required.
func temporalOption(cx *engine.ExecutionContext, opts *engine.Object, name string, values []string, fallback int) int {
	v := get(cx, opts, name)
	if v.IsUndefined() {
		if fallback < 0 {
			cx.ThrowRangeError(name + " is required")
		}
		return fallback
	}
	s := wtf8.ToValidUTF8(cx.ToString(v))
	if i := slices.Index(values, s); i >= 0 {
		return i
	}
	cx.ThrowRangeError(cx.DisplayString(cx.NewString(wtf8.String(s))) + " is not a valid value for " + name)
	return 0
}

func getOverflowOption(cx *engine.ExecutionContext, opts *engine.Object) temporal.Overflow {
	return temporal.Overflow(temporalOption(cx, opts, "overflow", []string{"constrain", "reject"}, 0))
}

func getDisambiguationOption(cx *engine.ExecutionContext, opts *engine.Object) temporal.Disambiguation {
	return temporal.Disambiguation(temporalOption(cx, opts, "disambiguation", []string{"compatible", "earlier", "later", "reject"}, 0))
}

func getOffsetOption(cx *engine.ExecutionContext, opts *engine.Object, fallback temporal.OffsetOption) temporal.OffsetOption {
	return temporal.OffsetOption(temporalOption(cx, opts, "offset", []string{"prefer", "use", "ignore", "reject"}, int(fallback)))
}

func getRoundingModeOption(cx *engine.ExecutionContext, opts *engine.Object, fallback temporal.RoundingMode) temporal.RoundingMode {
	return temporal.RoundingMode(temporalOption(cx, opts, "roundingMode", temporal.RoundingModeNames, int(fallback)))
}

func getShowCalendarOption(cx *engine.ExecutionContext, opts *engine.Object) temporal.ShowCalendar {
	return temporal.ShowCalendar(temporalOption(cx, opts, "calendarName", []string{"auto", "always", "never", "critical"}, 0))
}

func getShowTimeZoneOption(cx *engine.ExecutionContext, opts *engine.Object) temporal.ShowTimeZone {
	return temporal.ShowTimeZone(temporalOption(cx, opts, "timeZoneName", []string{"auto", "never", "critical"}, 0))
}

// getShowOffsetOption reports whether the offset option of toString is "auto".
func getShowOffsetOption(cx *engine.ExecutionContext, opts *engine.Object) bool {
	return temporalOption(cx, opts, "offset", []string{"auto", "never"}, 0) == 0
}

// getRoundingIncrementOption implements GetRoundingIncrementOption.
func getRoundingIncrementOption(cx *engine.ExecutionContext, opts *engine.Object) int64 {
	v := get(cx, opts, "roundingIncrement")
	if v.IsUndefined() {
		return 1
	}
	n := toIntegerWithTruncation(cx, v, "roundingIncrement")
	if n < 1 || n > 1e9 {
		cx.ThrowRangeError("roundingIncrement must be from 1 to 1e9")
	}
	return int64(n)
}

// getFractionalSecondDigitsOption implements GetTemporalFractionalSecondDigitsOption.
func getFractionalSecondDigitsOption(cx *engine.ExecutionContext, opts *engine.Object) temporal.Precision {
	v := get(cx, opts, "fractionalSecondDigits")
	if v.IsUndefined() {
		return temporal.PrecisionAuto
	}
	if !v.IsNumber() {
		if cx.ToString(v) != "auto" {
			cx.ThrowRangeError("fractionalSecondDigits must be 'auto' or from 0 to 9")
		}
		return temporal.PrecisionAuto
	}
	n := v.Number()
	if math.IsNaN(n) || math.IsInf(n, 0) {
		cx.ThrowRangeError("fractionalSecondDigits must be 'auto' or from 0 to 9")
	}
	d := math.Floor(n)
	if d < 0 || d > 9 {
		cx.ThrowRangeError("fractionalSecondDigits must be 'auto' or from 0 to 9")
	}
	return temporal.Precision(d)
}

// getUnitOption implements GetTemporalUnitValuedOption. It returns UnitUnset for an undefined
// option that is not required.
func getUnitOption(cx *engine.ExecutionContext, opts *engine.Object, key string, required bool) temporal.Unit {
	v := get(cx, opts, key)
	if v.IsUndefined() {
		if required {
			cx.ThrowRangeError(key + " is required")
		}
		return temporal.UnitUnset
	}
	s := wtf8.ToValidUTF8(cx.ToString(v))
	u, ok := temporal.UnitFromString(s)
	if !ok {
		cx.ThrowRangeError(cx.DisplayString(cx.NewString(wtf8.String(s))) + " is not a valid value for " + key)
	}
	return u
}

// unitGroup is the unit group of ValidateTemporalUnitValue.
type unitGroup int

const (
	unitGroupDate unitGroup = iota
	unitGroupTime
	unitGroupDateTime
)

// validateUnit implements ValidateTemporalUnitValue.
func validateUnit(cx *engine.ExecutionContext, u temporal.Unit, group unitGroup, extra ...temporal.Unit) {
	if u == temporal.UnitUnset || slices.Contains(extra, u) {
		return
	}
	switch {
	case u == temporal.UnitAuto:
	case u.IsDateUnit() && group != unitGroupTime:
		return
	case u.IsTimeUnit() && group != unitGroupDate:
		return
	}
	cx.ThrowRangeError(u.String() + " is not a valid unit here")
}

// differenceSettings is the result of GetDifferenceSettings.
type differenceSettings struct {
	smallestUnit temporal.Unit
	largestUnit  temporal.Unit
	mode         temporal.RoundingMode
	increment    int64
}

// getDifferenceSettings implements GetDifferenceSettings.
func getDifferenceSettings(cx *engine.ExecutionContext, since bool, opts *engine.Object, group unitGroup, disallowed []temporal.Unit, fallbackSmallest, smallestLargestDefault temporal.Unit) differenceSettings {
	largest := getUnitOption(cx, opts, "largestUnit", false)
	increment := getRoundingIncrementOption(cx, opts)
	mode := getRoundingModeOption(cx, opts, temporal.RoundTrunc)
	smallest := getUnitOption(cx, opts, "smallestUnit", false)
	validateUnit(cx, largest, group, temporal.UnitAuto)
	if largest == temporal.UnitUnset {
		largest = temporal.UnitAuto
	}
	if slices.Contains(disallowed, largest) {
		cx.ThrowRangeError(largest.String() + " is not a valid value for largestUnit")
	}
	validateUnit(cx, smallest, group)
	if smallest == temporal.UnitUnset {
		smallest = fallbackSmallest
	}
	if slices.Contains(disallowed, smallest) {
		cx.ThrowRangeError(smallest.String() + " is not a valid value for smallestUnit")
	}
	defaultLargest := temporal.LargerOfTwoTemporalUnits(smallestLargestDefault, smallest)
	if largest == temporal.UnitAuto {
		largest = defaultLargest
	}
	if temporal.LargerOfTwoTemporalUnits(largest, smallest) != largest {
		cx.ThrowRangeError("largestUnit must not be smaller than smallestUnit")
	}
	if maximum, ok := temporal.MaximumTemporalDurationRoundingIncrement(smallest); ok {
		check(temporal.ValidateTemporalRoundingIncrement(increment, maximum, false))
	}
	if since {
		mode = mode.Negate()
	}
	return differenceSettings{
		smallestUnit: smallest,
		largestUnit:  largest,
		mode:         mode,
		increment:    increment,
	}
}

// roundToOptions returns the options object of a round method, where a string stands for the
// smallestUnit option (or the key option).
func roundToOptions(cx *engine.ExecutionContext, v engine.Value, key string) *engine.Object {
	if v.IsUndefined() {
		cx.ThrowTypeError("options are required")
	}
	if v.IsString() {
		o := cx.NewObject(nil)
		cx.CreateDataPropertyOrThrow(o, engine.NamedKey(cx.Intern(wtf8.String(key))), v)
		return o
	}
	return getOptionsObject(cx, v)
}

// SystemTimeZone returns the time zone of the host for Temporal and Intl.DateTimeFormat.
func SystemTimeZone(cx *engine.ExecutionContext) temporal.TimeZone {
	tz := cx.DefaultTimeZone()
	if z, ok := temporal.SystemTimeZone(tz); ok {
		return z
	}
	return temporal.SystemOffsetTimeZone(tz, cx.SystemUTCEpochMilliseconds())
}

// toTimeZone implements ToTemporalTimeZoneIdentifier, returning the time zone.
func toTimeZone(cx *engine.ExecutionContext, v engine.Value) temporal.TimeZone {
	if s, ok := engine.TemporalData(v).(*zonedDateTimeSlots); ok {
		return s.tz
	}
	str := requireString(cx, v, "time zone")
	if str == "UTC" {
		return temporal.UTC
	}
	name, off, isOff, err := temporal.ParseTemporalTimeZoneString(str, cx.Poll)
	check(err)
	var system temporal.TimeZone
	if !isOff {
		system = SystemTimeZone(cx)
	}
	return must(temporal.TimeZoneFromIdentifier(name, off, isOff, system))
}

// calendarOf returns the calendar of v when it is an object with a [[Calendar]] slot.
func calendarOf(v engine.Value) (string, bool) {
	switch s := engine.TemporalData(v).(type) {
	case *plainDateSlots:
		return s.cal, true
	case *plainDateTimeSlots:
		return s.cal, true
	case *plainYearMonthSlots:
		return s.cal, true
	case *plainMonthDaySlots:
		return s.cal, true
	case *zonedDateTimeSlots:
		return s.cal, true
	}
	return "", false
}

// toCalendar implements ToTemporalCalendarIdentifier.
func toCalendar(cx *engine.ExecutionContext, v engine.Value) string {
	if c, ok := calendarOf(v); ok {
		return c
	}
	s := requireString(cx, v, "calendar")
	return must(temporal.CanonicalizeCalendar(parseCalendarString(cx, s)))
}

// parseCalendarString implements ParseTemporalCalendarString.
func parseCalendarString(cx *engine.ExecutionContext, s string) string {
	if r, err := temporal.ParseISODateTime(s, cx.Poll, temporal.AnyGoal...); err == nil {
		if r.Calendar == "" {
			return temporal.ISO8601
		}
		return r.Calendar
	}
	if !temporal.IsAnnotationValue(s, cx.Poll) {
		cx.ThrowRangeError("invalid calendar: " + cx.DisplayString(cx.NewString(wtf8.String(s))))
	}
	return s
}

// calendarWithISODefault implements GetTemporalCalendarIdentifierWithISODefault.
func calendarWithISODefault(cx *engine.ExecutionContext, item *engine.Object) string {
	if c, ok := calendarOf(engine.ObjectValue(item)); ok {
		return c
	}
	v := get(cx, item, "calendar")
	if v.IsUndefined() {
		return temporal.ISO8601
	}
	return toCalendar(cx, v)
}

// canonicalizeParsedCalendar returns the canonical calendar of a parsed string.
func canonicalizeParsedCalendar(cx *engine.ExecutionContext, c string) string {
	if c == "" {
		return temporal.ISO8601
	}
	return must(temporal.CanonicalizeCalendar(c))
}

// The field names of PrepareCalendarFields.
var (
	dateFieldNames      = []string{"day", "month", "monthCode", "year"}
	yearMonthFieldNames = []string{"month", "monthCode", "year"}
	timeFieldNames      = []string{"hour", "microsecond", "millisecond", "minute", "nanosecond", "second"}
)

// fieldsRequirement is the requiredFields of PrepareCalendarFields.
type fieldsRequirement int

const (
	fieldsNoneRequired fieldsRequirement = iota
	fieldsPartial
	fieldsTimeZoneRequired
)

// prepareCalendarFields implements PrepareCalendarFields. names are
// the names of the fields to read.
func prepareCalendarFields(cx *engine.ExecutionContext, cal string, bag *engine.Object, names []string, req fieldsRequirement) temporal.CalendarFields {
	names = slices.Clone(names)
	// CalendarExtraFields.
	if slices.Contains(names, "year") && temporal.CalendarSupportsEra(cal) {
		names = append(names, "era", "eraYear")
	}
	slices.Sort(names)
	var f temporal.CalendarFields
	if req != fieldsPartial {
		f.SetTime(temporal.Time{})
	}
	any := false
	for _, name := range names {
		v := get(cx, bag, name)
		if v.IsUndefined() {
			if req == fieldsTimeZoneRequired && name == "timeZone" {
				cx.ThrowTypeError("timeZone is required")
			}
			continue
		}
		any = true
		switch name {
		case "era":
			f.Era, f.HasEra = wtf8.ToValidUTF8(cx.ToString(v)), true
		case "eraYear":
			f.EraYear, f.HasEraYear = toIntegerWithTruncation(cx, v, name), true
		case "year":
			f.Year, f.HasYear = toIntegerWithTruncation(cx, v, name), true
		case "month":
			f.Month, f.HasMonth = toPositiveIntegerWithTruncation(cx, v, name), true
		case "day":
			f.Day, f.HasDay = toPositiveIntegerWithTruncation(cx, v, name), true
		case "monthCode":
			p := cx.ToPrimitive(v, engine.HintString)
			if !p.IsString() {
				cx.ThrowTypeError("monthCode must be a string")
			}
			s := wtf8.ToValidUTF8(cx.ToString(p))
			n, leap := must2(temporal.ParseMonthCode(s))
			f.MonthCode, f.HasMonthCode = temporal.CreateMonthCode(n, leap), true
		case "hour":
			f.Hour, f.HasHour = toIntegerWithTruncation(cx, v, name), true
		case "minute":
			f.Minute, f.HasMinute = toIntegerWithTruncation(cx, v, name), true
		case "second":
			f.Second, f.HasSecond = toIntegerWithTruncation(cx, v, name), true
		case "millisecond":
			f.Millisecond, f.HasMillisecond = toIntegerWithTruncation(cx, v, name), true
		case "microsecond":
			f.Microsecond, f.HasMicrosecond = toIntegerWithTruncation(cx, v, name), true
		case "nanosecond":
			f.Nanosecond, f.HasNanosecond = toIntegerWithTruncation(cx, v, name), true
		case "offset":
			p := cx.ToPrimitive(v, engine.HintString)
			if !p.IsString() {
				cx.ThrowTypeError("offset must be a string")
			}
			s := wtf8.ToValidUTF8(cx.ToString(p))
			must(temporal.ParseDateTimeUTCOffset(s))
			f.Offset, f.HasOffset = s, true
		case "timeZone":
			f.TimeZone, f.HasTimeZone = toTimeZone(cx, v), true
		}
	}
	if req == fieldsPartial && !any {
		cx.ThrowTypeError("no fields are given")
	}
	return f
}

// timeRecordFields is a partial time record of ToTemporalTimeRecord.
type timeRecordFields struct {
	values [6]float64 // hour, minute, second, millisecond, microsecond, nanosecond
	has    [6]bool
}

// toTemporalTimeRecord implements ToTemporalTimeRecord.
func toTemporalTimeRecord(cx *engine.ExecutionContext, bag *engine.Object, partial bool) timeRecordFields {
	var r timeRecordFields
	any := false
	// The fields in alphabetical order, with their indices in values.
	for _, f := range []struct {
		name  string
		index int
	}{{"hour", 0}, {"microsecond", 4}, {"millisecond", 3}, {"minute", 1}, {"nanosecond", 5}, {"second", 2}} {
		v := get(cx, bag, f.name)
		if v.IsUndefined() {
			continue
		}
		r.values[f.index] = toIntegerWithTruncation(cx, v, f.name)
		r.has[f.index] = true
		any = true
	}
	if !any {
		cx.ThrowTypeError("no time fields are given")
	}
	if !partial {
		r.has = [6]bool{true, true, true, true, true, true}
	}
	return r
}

// interpretDateTimeFields implements InterpretTemporalDateTimeFields.
func interpretDateTimeFields(cx *engine.ExecutionContext, cal string, f temporal.CalendarFields, overflow temporal.Overflow) temporal.ISODateTime {
	d := must(temporal.CalendarDateFromFields(cal, f, overflow))
	t := must(temporal.RegulateTime(f.Hour, f.Minute, f.Second, f.Millisecond, f.Microsecond, f.Nanosecond, overflow))
	return temporal.ISODateTime{
		Date: d,
		Time: t,
	}
}

// rejectTemporalLikeObject implements IsPartialTemporalObject's rejections.
func rejectTemporalLikeObject(cx *engine.ExecutionContext, v engine.Value) *engine.Object {
	if !v.IsObject() {
		cx.ThrowTypeError("argument must be an object")
	}
	switch engine.TemporalData(v).(type) {
	case *plainDateSlots, *plainDateTimeSlots, *plainMonthDaySlots, *plainTimeSlots, *plainYearMonthSlots, *zonedDateTimeSlots:
		cx.ThrowTypeError("argument must be a partial object, not a Temporal object")
	}
	o := v.Object()
	if !get(cx, o, "calendar").IsUndefined() {
		cx.ThrowTypeError("calendar is not allowed")
	}
	if !get(cx, o, "timeZone").IsUndefined() {
		cx.ThrowTypeError("timeZone is not allowed")
	}
	return o
}

// newTarget returns the new.target of a constructor, or throws a TypeError when it was called.
func newTarget(cx *engine.ExecutionContext, name string) *engine.Object {
	nt := cx.GetNewTarget()
	if nt == nil {
		cx.ThrowTypeError("constructor " + name + " requires 'new'")
	}
	return nt
}

// temporalConstructor defines a Temporal constructor and returns it and its intrinsic prototype.
func temporalConstructor(cx *engine.ExecutionContext, ns *engine.Object, name wtf8.String, length int, protoID engine.Intrinsic, f engine.NativeFunc) (*engine.Object, *engine.Object) {
	proto := cx.NewOrdinaryObject()
	cx.SetIntrinsic(protoID, proto)
	c := cx.NewNativeConstructor(name, length, f)
	cx.DefineConstructorPrototype(c, protoID)
	cx.Define(proto, cx.Key(engine.KeyConstructor), engine.ObjectValue(c), engine.FlagsHidden)
	cx.Define(ns, cx.Intern(name), engine.ObjectValue(c), engine.FlagsHidden)
	cx.Define(proto, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Temporal."+name), engine.FlagConfigurable)
	return c, proto
}

// temporalValueOfNative returns the valueOf method of the prototype of the type name, which throws.
//
//go:noinline
func temporalValueOfNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		cx.ThrowTypeError("use Temporal." + name + ".compare or equals to compare Temporal." + name + " values, and toString to convert them")
		return engine.Undefined
	})
}

// incompatible throws the TypeError of a method called on a receiver of a wrong type.
func incompatible(cx *engine.ExecutionContext, what string, this engine.Value) {
	cx.ThrowTypeError(what + " called on incompatible receiver " + cx.DisplayString(this))
}

// bigIntToEpochNs returns the BigInt v as epoch nanoseconds, or throws a RangeError when they
// are not valid.
func bigIntToEpochNs(cx *engine.ExecutionContext, v engine.Value) temporal.Int128 {
	b := cx.ToBigInt(v).BigInt()
	ns, ok := temporal.Int128FromBig(b)
	if !ok || !temporal.IsValidEpochNanoseconds(ns) {
		cx.ThrowRangeError("epoch nanoseconds are out of range")
	}
	return ns
}

// epochNsValue returns ns as a BigInt.
func epochNsValue(cx *engine.ExecutionContext, ns temporal.Int128) engine.Value {
	return cx.NewBigInt(ns.Big())
}

// epochMilliseconds returns floor(ns / 10⁶) as a Number.
func epochMilliseconds(ns temporal.Int128) engine.Value {
	ms, _ := ns.FloorDivInt64(1_000_000)
	return engine.Number(ms.Float64())
}

// compareResult returns c as a Number.
func compareResult(c int) engine.Value {
	return engine.Number(float64(c))
}

// initTemporal defines the Temporal namespace and Date.prototype.toTemporalInstant. The
// contents of the namespace are created when they are first needed.
func initTemporal(cx *engine.ExecutionContext) {
	ns := cx.NewLazyObject(cx.Intrinsic(engine.IntrinsicObjectPrototype), func(cx *engine.ExecutionContext, ns *engine.Object) {
		cx.Define(ns, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("Temporal"), engine.FlagConfigurable)
		initTemporalInstant(cx, ns)
		initTemporalDuration(cx, ns)
		initTemporalPlainDate(cx, ns)
		initTemporalPlainTime(cx, ns)
		initTemporalPlainDateTime(cx, ns)
		initTemporalPlainYearMonth(cx, ns)
		initTemporalPlainMonthDay(cx, ns)
		initTemporalZonedDateTime(cx, ns)
		initTemporalNow(cx, ns)
	})
	cx.SetIntrinsic(engine.IntrinsicTemporal, ns)
	cx.Define(cx.GlobalObject().Object(), cx.Intern("Temporal"), engine.ObjectValue(ns), engine.FlagsHidden)

	method(cx, cx.Intrinsic(engine.IntrinsicDatePrototype), "toTemporalInstant", 0, nativeDateToTemporalInstant)
}

var nativeDateToTemporalInstant = temporalNative(dateToTemporalInstant)

func dateToTemporalInstant(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	tv := thisDate(cx, this, "toTemporalInstant")
	if math.IsNaN(tv) {
		cx.ThrowRangeError("invalid time value")
	}
	return newInstant(cx, temporal.Mul64(int64(tv), 1_000_000), nil)
}

// TemporalPlainDateOf returns the ISO date and the calendar of v, and reports whether v is a
// Temporal.PlainDate.
func TemporalPlainDateOf(v engine.Value) (temporal.ISODate, string, bool) {
	s, ok := engine.TemporalData(v).(*plainDateSlots)
	if !ok {
		return temporal.ISODate{}, "", false
	}
	return s.date, s.cal, true
}

// TemporalPlainYearMonthOf returns the ISO date and the calendar of v, and reports whether v
// is a Temporal.PlainYearMonth.
func TemporalPlainYearMonthOf(v engine.Value) (temporal.ISODate, string, bool) {
	s, ok := engine.TemporalData(v).(*plainYearMonthSlots)
	if !ok {
		return temporal.ISODate{}, "", false
	}
	return s.date, s.cal, true
}

// TemporalPlainMonthDayOf returns the ISO date and the calendar of v, and reports whether v
// is a Temporal.PlainMonthDay.
func TemporalPlainMonthDayOf(v engine.Value) (temporal.ISODate, string, bool) {
	s, ok := engine.TemporalData(v).(*plainMonthDaySlots)
	if !ok {
		return temporal.ISODate{}, "", false
	}
	return s.date, s.cal, true
}

// TemporalPlainTimeOf returns the time of v, and reports whether v is a Temporal.PlainTime.
func TemporalPlainTimeOf(v engine.Value) (temporal.Time, bool) {
	s, ok := engine.TemporalData(v).(*plainTimeSlots)
	if !ok {
		return temporal.Time{}, false
	}
	return s.t, true
}

// TemporalPlainDateTimeOf returns the ISO date and time and the calendar of v, and reports
// whether v is a Temporal.PlainDateTime.
func TemporalPlainDateTimeOf(v engine.Value) (temporal.ISODateTime, string, bool) {
	s, ok := engine.TemporalData(v).(*plainDateTimeSlots)
	if !ok {
		return temporal.ISODateTime{}, "", false
	}
	return s.dt, s.cal, true
}

// TemporalZonedDateTimeOf returns the epoch nanoseconds, the time zone and the calendar of v,
// and reports whether v is a Temporal.ZonedDateTime.
func TemporalZonedDateTimeOf(v engine.Value) (temporal.Int128, temporal.TimeZone, string, bool) {
	s, ok := engine.TemporalData(v).(*zonedDateTimeSlots)
	if !ok {
		return temporal.Int128{}, temporal.TimeZone{}, "", false
	}
	return s.ns, s.tz, s.cal, true
}

// TemporalInstantOf returns the epoch nanoseconds of v, and reports whether v is a
// Temporal.Instant.
func TemporalInstantOf(v engine.Value) (temporal.Int128, bool) {
	s, ok := engine.TemporalData(v).(*instantSlots)
	if !ok {
		return temporal.Int128{}, false
	}
	return s.ns, true
}

// TemporalDurationOf returns the fields of v, and reports whether v is a Temporal.Duration.
func TemporalDurationOf(v engine.Value) (temporal.Duration, bool) {
	s, ok := engine.TemporalData(v).(*durationSlots)
	if !ok {
		return temporal.Duration{}, false
	}
	return s.d, true
}
