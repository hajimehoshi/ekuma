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
func throwTemporal(vm *engine.VM, err error) {
	if te, ok := errors.AsType[*temporal.Error](err); ok && te.Kind == temporal.TypeError {
		vm.ThrowTypeError(vm.CurrentRealm(), te.Message)
	}
	vm.ThrowRangeError(vm.CurrentRealm(), err.Error())
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
func temporalNative(f engine.NativeFunc) engine.NativeFunc {
	return func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		defer func() {
			if e := recover(); e != nil {
				if tf, ok := e.(temporalFailure); ok {
					throwTemporal(vm, tf.err)
				}
				panic(e)
			}
		}()
		return f(vm, this, args)
	}
}

// tmethod is method for the functions of Temporal.
func tmethod(vm *engine.VM, o *engine.Object, name wtf8.String, length int, f engine.NativeFunc) *engine.Object {
	return method(vm, o, name, length, temporalNative(f))
}

// tgetter is getter for the functions of Temporal.
func tgetter(vm *engine.VM, o *engine.Object, key *engine.Object, name wtf8.String, f engine.NativeFunc) {
	getter(vm, o, key, name, temporalNative(f))
}

// get returns the property name of o.
func get(vm *engine.VM, o *engine.Object, name string) engine.Value {
	return vm.Get(o, engine.NamedKey(vm.Intern(wtf8.String(name))))
}

// requireString returns v as a Go string, or throws a TypeError when it is not a String.
func requireString(vm *engine.VM, v engine.Value, what string) string {
	if !v.IsString() {
		vm.ThrowTypeError(vm.CurrentRealm(), what+" must be a string, not "+vm.DisplayString(v))
	}
	return wtf8.ToValidUTF8(vm.ToString(v))
}

// toIntegerWithTruncation implements ToIntegerWithTruncation.
func toIntegerWithTruncation(vm *engine.VM, v engine.Value, what string) float64 {
	n := vm.ToNumber(v)
	if math.IsNaN(n) || math.IsInf(n, 0) {
		vm.ThrowRangeError(vm.CurrentRealm(), what+" must be a finite number")
	}
	return math.Trunc(n) + 0
}

// toPositiveIntegerWithTruncation implements ToPositiveIntegerWithTruncation.
func toPositiveIntegerWithTruncation(vm *engine.VM, v engine.Value, what string) float64 {
	n := toIntegerWithTruncation(vm, v, what)
	if n <= 0 {
		vm.ThrowRangeError(vm.CurrentRealm(), what+" must be positive")
	}
	return n
}

// toIntegerIfIntegral implements ToIntegerIfIntegral.
func toIntegerIfIntegral(vm *engine.VM, v engine.Value, what string) float64 {
	n := vm.ToNumber(v)
	if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
		vm.ThrowRangeError(vm.CurrentRealm(), what+" must be an integer")
	}
	return n + 0
}

// getOptionsObject implements GetOptionsObject.
func getOptionsObject(vm *engine.VM, v engine.Value) *engine.Object {
	if v.IsUndefined() {
		return vm.NewObject(nil)
	}
	if v.IsObject() {
		return v.Object()
	}
	vm.ThrowTypeError(vm.CurrentRealm(), "options must be an object, not "+vm.DisplayString(v))
	return nil
}

// temporalOption implements GetOption for a String option whose allowed values are values. It
// returns the index of the value, or fallback when the option is undefined. A negative
// fallback makes the option required.
func temporalOption(vm *engine.VM, opts *engine.Object, name string, values []string, fallback int) int {
	v := get(vm, opts, name)
	if v.IsUndefined() {
		if fallback < 0 {
			vm.ThrowRangeError(vm.CurrentRealm(), name+" is required")
		}
		return fallback
	}
	s := wtf8.ToValidUTF8(vm.ToString(v))
	if i := slices.Index(values, s); i >= 0 {
		return i
	}
	vm.ThrowRangeError(vm.CurrentRealm(), vm.DisplayString(vm.NewString(wtf8.String(s)))+" is not a valid value for "+name)
	return 0
}

func getOverflowOption(vm *engine.VM, opts *engine.Object) temporal.Overflow {
	return temporal.Overflow(temporalOption(vm, opts, "overflow", []string{"constrain", "reject"}, 0))
}

func getDisambiguationOption(vm *engine.VM, opts *engine.Object) temporal.Disambiguation {
	return temporal.Disambiguation(temporalOption(vm, opts, "disambiguation", []string{"compatible", "earlier", "later", "reject"}, 0))
}

func getOffsetOption(vm *engine.VM, opts *engine.Object, fallback temporal.OffsetOption) temporal.OffsetOption {
	return temporal.OffsetOption(temporalOption(vm, opts, "offset", []string{"prefer", "use", "ignore", "reject"}, int(fallback)))
}

func getRoundingModeOption(vm *engine.VM, opts *engine.Object, fallback temporal.RoundingMode) temporal.RoundingMode {
	return temporal.RoundingMode(temporalOption(vm, opts, "roundingMode", temporal.RoundingModeNames, int(fallback)))
}

func getShowCalendarOption(vm *engine.VM, opts *engine.Object) temporal.ShowCalendar {
	return temporal.ShowCalendar(temporalOption(vm, opts, "calendarName", []string{"auto", "always", "never", "critical"}, 0))
}

func getShowTimeZoneOption(vm *engine.VM, opts *engine.Object) temporal.ShowTimeZone {
	return temporal.ShowTimeZone(temporalOption(vm, opts, "timeZoneName", []string{"auto", "never", "critical"}, 0))
}

// getShowOffsetOption reports whether the offset option of toString is "auto".
func getShowOffsetOption(vm *engine.VM, opts *engine.Object) bool {
	return temporalOption(vm, opts, "offset", []string{"auto", "never"}, 0) == 0
}

// getRoundingIncrementOption implements GetRoundingIncrementOption.
func getRoundingIncrementOption(vm *engine.VM, opts *engine.Object) int64 {
	v := get(vm, opts, "roundingIncrement")
	if v.IsUndefined() {
		return 1
	}
	n := toIntegerWithTruncation(vm, v, "roundingIncrement")
	if n < 1 || n > 1e9 {
		vm.ThrowRangeError(vm.CurrentRealm(), "roundingIncrement must be from 1 to 1e9")
	}
	return int64(n)
}

// getFractionalSecondDigitsOption implements GetTemporalFractionalSecondDigitsOption.
func getFractionalSecondDigitsOption(vm *engine.VM, opts *engine.Object) temporal.Precision {
	v := get(vm, opts, "fractionalSecondDigits")
	if v.IsUndefined() {
		return temporal.PrecisionAuto
	}
	if !v.IsNumber() {
		if vm.ToString(v) != "auto" {
			vm.ThrowRangeError(vm.CurrentRealm(), "fractionalSecondDigits must be 'auto' or from 0 to 9")
		}
		return temporal.PrecisionAuto
	}
	n := v.Number()
	if math.IsNaN(n) || math.IsInf(n, 0) {
		vm.ThrowRangeError(vm.CurrentRealm(), "fractionalSecondDigits must be 'auto' or from 0 to 9")
	}
	d := math.Floor(n)
	if d < 0 || d > 9 {
		vm.ThrowRangeError(vm.CurrentRealm(), "fractionalSecondDigits must be 'auto' or from 0 to 9")
	}
	return temporal.Precision(d)
}

// getUnitOption implements GetTemporalUnitValuedOption. It returns UnitUnset for an undefined
// option that is not required.
func getUnitOption(vm *engine.VM, opts *engine.Object, key string, required bool) temporal.Unit {
	v := get(vm, opts, key)
	if v.IsUndefined() {
		if required {
			vm.ThrowRangeError(vm.CurrentRealm(), key+" is required")
		}
		return temporal.UnitUnset
	}
	s := wtf8.ToValidUTF8(vm.ToString(v))
	u, ok := temporal.UnitFromString(s)
	if !ok {
		vm.ThrowRangeError(vm.CurrentRealm(), vm.DisplayString(vm.NewString(wtf8.String(s)))+" is not a valid value for "+key)
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
func validateUnit(vm *engine.VM, u temporal.Unit, group unitGroup, extra ...temporal.Unit) {
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
	vm.ThrowRangeError(vm.CurrentRealm(), u.String()+" is not a valid unit here")
}

// differenceSettings is the result of GetDifferenceSettings.
type differenceSettings struct {
	smallestUnit temporal.Unit
	largestUnit  temporal.Unit
	mode         temporal.RoundingMode
	increment    int64
}

// getDifferenceSettings implements GetDifferenceSettings.
func getDifferenceSettings(vm *engine.VM, since bool, opts *engine.Object, group unitGroup, disallowed []temporal.Unit, fallbackSmallest, smallestLargestDefault temporal.Unit) differenceSettings {
	largest := getUnitOption(vm, opts, "largestUnit", false)
	increment := getRoundingIncrementOption(vm, opts)
	mode := getRoundingModeOption(vm, opts, temporal.RoundTrunc)
	smallest := getUnitOption(vm, opts, "smallestUnit", false)
	validateUnit(vm, largest, group, temporal.UnitAuto)
	if largest == temporal.UnitUnset {
		largest = temporal.UnitAuto
	}
	if slices.Contains(disallowed, largest) {
		vm.ThrowRangeError(vm.CurrentRealm(), largest.String()+" is not a valid value for largestUnit")
	}
	validateUnit(vm, smallest, group)
	if smallest == temporal.UnitUnset {
		smallest = fallbackSmallest
	}
	if slices.Contains(disallowed, smallest) {
		vm.ThrowRangeError(vm.CurrentRealm(), smallest.String()+" is not a valid value for smallestUnit")
	}
	defaultLargest := temporal.LargerOfTwoTemporalUnits(smallestLargestDefault, smallest)
	if largest == temporal.UnitAuto {
		largest = defaultLargest
	}
	if temporal.LargerOfTwoTemporalUnits(largest, smallest) != largest {
		vm.ThrowRangeError(vm.CurrentRealm(), "largestUnit must not be smaller than smallestUnit")
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
func roundToOptions(vm *engine.VM, v engine.Value, key string) *engine.Object {
	if v.IsUndefined() {
		vm.ThrowTypeError(vm.CurrentRealm(), "options are required")
	}
	if v.IsString() {
		o := vm.NewObject(nil)
		vm.CreateDataPropertyOrThrow(o, engine.NamedKey(vm.Intern(wtf8.String(key))), v)
		return o
	}
	return getOptionsObject(vm, v)
}

// SystemTimeZone returns the time zone of the host for Temporal and Intl.DateTimeFormat.
func SystemTimeZone(vm *engine.VM) temporal.TimeZone {
	tz := vm.DefaultTimeZone()
	if z, ok := temporal.SystemTimeZone(tz); ok {
		return z
	}
	return temporal.SystemOffsetTimeZone(tz, vm.SystemUTCEpochMilliseconds())
}

// toTimeZone implements ToTemporalTimeZoneIdentifier, returning the time zone.
func toTimeZone(vm *engine.VM, v engine.Value) temporal.TimeZone {
	if s, ok := engine.TemporalData(v).(*zonedDateTimeSlots); ok {
		return s.tz
	}
	str := requireString(vm, v, "time zone")
	if str == "UTC" {
		return temporal.UTC
	}
	name, off, isOff, err := temporal.ParseTemporalTimeZoneString(str, vm.Poll)
	check(err)
	var system temporal.TimeZone
	if !isOff {
		system = SystemTimeZone(vm)
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
func toCalendar(vm *engine.VM, v engine.Value) string {
	if c, ok := calendarOf(v); ok {
		return c
	}
	s := requireString(vm, v, "calendar")
	return must(temporal.CanonicalizeCalendar(parseCalendarString(vm, s)))
}

// parseCalendarString implements ParseTemporalCalendarString.
func parseCalendarString(vm *engine.VM, s string) string {
	if r, err := temporal.ParseISODateTime(s, vm.Poll, temporal.AnyGoal...); err == nil {
		if r.Calendar == "" {
			return temporal.ISO8601
		}
		return r.Calendar
	}
	if !temporal.IsAnnotationValue(s, vm.Poll) {
		vm.ThrowRangeError(vm.CurrentRealm(), "invalid calendar: "+vm.DisplayString(vm.NewString(wtf8.String(s))))
	}
	return s
}

// calendarWithISODefault implements GetTemporalCalendarIdentifierWithISODefault.
func calendarWithISODefault(vm *engine.VM, item *engine.Object) string {
	if c, ok := calendarOf(engine.ObjectValue(item)); ok {
		return c
	}
	v := get(vm, item, "calendar")
	if v.IsUndefined() {
		return temporal.ISO8601
	}
	return toCalendar(vm, v)
}

// canonicalizeParsedCalendar returns the canonical calendar of a parsed string.
func canonicalizeParsedCalendar(vm *engine.VM, c string) string {
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
func prepareCalendarFields(vm *engine.VM, cal string, bag *engine.Object, names []string, req fieldsRequirement) temporal.CalendarFields {
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
		v := get(vm, bag, name)
		if v.IsUndefined() {
			if req == fieldsTimeZoneRequired && name == "timeZone" {
				vm.ThrowTypeError(vm.CurrentRealm(), "timeZone is required")
			}
			continue
		}
		any = true
		switch name {
		case "era":
			f.Era, f.HasEra = wtf8.ToValidUTF8(vm.ToString(v)), true
		case "eraYear":
			f.EraYear, f.HasEraYear = toIntegerWithTruncation(vm, v, name), true
		case "year":
			f.Year, f.HasYear = toIntegerWithTruncation(vm, v, name), true
		case "month":
			f.Month, f.HasMonth = toPositiveIntegerWithTruncation(vm, v, name), true
		case "day":
			f.Day, f.HasDay = toPositiveIntegerWithTruncation(vm, v, name), true
		case "monthCode":
			p := vm.ToPrimitive(v, engine.HintString)
			if !p.IsString() {
				vm.ThrowTypeError(vm.CurrentRealm(), "monthCode must be a string")
			}
			s := wtf8.ToValidUTF8(vm.ToString(p))
			n, leap := must2(temporal.ParseMonthCode(s))
			f.MonthCode, f.HasMonthCode = temporal.CreateMonthCode(n, leap), true
		case "hour":
			f.Hour, f.HasHour = toIntegerWithTruncation(vm, v, name), true
		case "minute":
			f.Minute, f.HasMinute = toIntegerWithTruncation(vm, v, name), true
		case "second":
			f.Second, f.HasSecond = toIntegerWithTruncation(vm, v, name), true
		case "millisecond":
			f.Millisecond, f.HasMillisecond = toIntegerWithTruncation(vm, v, name), true
		case "microsecond":
			f.Microsecond, f.HasMicrosecond = toIntegerWithTruncation(vm, v, name), true
		case "nanosecond":
			f.Nanosecond, f.HasNanosecond = toIntegerWithTruncation(vm, v, name), true
		case "offset":
			p := vm.ToPrimitive(v, engine.HintString)
			if !p.IsString() {
				vm.ThrowTypeError(vm.CurrentRealm(), "offset must be a string")
			}
			s := wtf8.ToValidUTF8(vm.ToString(p))
			must(temporal.ParseDateTimeUTCOffset(s))
			f.Offset, f.HasOffset = s, true
		case "timeZone":
			f.TimeZone, f.HasTimeZone = toTimeZone(vm, v), true
		}
	}
	if req == fieldsPartial && !any {
		vm.ThrowTypeError(vm.CurrentRealm(), "no fields are given")
	}
	return f
}

// timeRecordFields is a partial time record of ToTemporalTimeRecord.
type timeRecordFields struct {
	values [6]float64 // hour, minute, second, millisecond, microsecond, nanosecond
	has    [6]bool
}

// toTemporalTimeRecord implements ToTemporalTimeRecord.
func toTemporalTimeRecord(vm *engine.VM, bag *engine.Object, partial bool) timeRecordFields {
	var r timeRecordFields
	any := false
	// The fields in alphabetical order, with their indices in values.
	for _, f := range []struct {
		name  string
		index int
	}{{"hour", 0}, {"microsecond", 4}, {"millisecond", 3}, {"minute", 1}, {"nanosecond", 5}, {"second", 2}} {
		v := get(vm, bag, f.name)
		if v.IsUndefined() {
			continue
		}
		r.values[f.index] = toIntegerWithTruncation(vm, v, f.name)
		r.has[f.index] = true
		any = true
	}
	if !any {
		vm.ThrowTypeError(vm.CurrentRealm(), "no time fields are given")
	}
	if !partial {
		r.has = [6]bool{true, true, true, true, true, true}
	}
	return r
}

// interpretDateTimeFields implements InterpretTemporalDateTimeFields.
func interpretDateTimeFields(vm *engine.VM, cal string, f temporal.CalendarFields, overflow temporal.Overflow) temporal.ISODateTime {
	d := must(temporal.CalendarDateFromFields(cal, f, overflow))
	t := must(temporal.RegulateTime(f.Hour, f.Minute, f.Second, f.Millisecond, f.Microsecond, f.Nanosecond, overflow))
	return temporal.ISODateTime{
		Date: d,
		Time: t,
	}
}

// rejectTemporalLikeObject implements IsPartialTemporalObject's rejections.
func rejectTemporalLikeObject(vm *engine.VM, v engine.Value) *engine.Object {
	if !v.IsObject() {
		vm.ThrowTypeError(vm.CurrentRealm(), "argument must be an object")
	}
	switch engine.TemporalData(v).(type) {
	case *plainDateSlots, *plainDateTimeSlots, *plainMonthDaySlots, *plainTimeSlots, *plainYearMonthSlots, *zonedDateTimeSlots:
		vm.ThrowTypeError(vm.CurrentRealm(), "argument must be a partial object, not a Temporal object")
	}
	o := v.Object()
	if !get(vm, o, "calendar").IsUndefined() {
		vm.ThrowTypeError(vm.CurrentRealm(), "calendar is not allowed")
	}
	if !get(vm, o, "timeZone").IsUndefined() {
		vm.ThrowTypeError(vm.CurrentRealm(), "timeZone is not allowed")
	}
	return o
}

// newTarget returns the new.target of a constructor, or throws a TypeError when it was called.
func newTarget(vm *engine.VM, name string) *engine.Object {
	nt := vm.GetNewTarget()
	if nt == nil {
		vm.ThrowTypeError(vm.CurrentRealm(), "constructor "+name+" requires 'new'")
	}
	return nt
}

// temporalConstructor defines a Temporal constructor and returns it and its intrinsic prototype.
func temporalConstructor(vm *engine.VM, ns *engine.Object, name wtf8.String, length int, protoID engine.Intrinsic, f engine.NativeFunc) (*engine.Object, *engine.Object) {
	proto := vm.NewOrdinaryObject()
	vm.SetIntrinsic(protoID, proto)
	c := vm.NewNativeConstructor(name, length, temporalNative(f))
	vm.DefineConstructorPrototype(c, protoID)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(c), engine.FlagsHidden)
	vm.Define(ns, vm.Intern(name), engine.ObjectValue(c), engine.FlagsHidden)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Temporal."+name), engine.FlagConfigurable)
	return c, proto
}

// valueOfThrows defines the valueOf method of the prototype of the type name, which throws.
func valueOfThrows(vm *engine.VM, proto *engine.Object, name string) {
	tmethod(vm, proto, "valueOf", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		vm.ThrowTypeError(vm.CurrentRealm(), "use Temporal."+name+".compare or equals to compare Temporal."+name+" values, and toString to convert them")
		return engine.Undefined
	})
}

// incompatible throws the TypeError of a method called on a receiver of a wrong type.
func incompatible(vm *engine.VM, what string, this engine.Value) {
	vm.ThrowTypeError(vm.CurrentRealm(), what+" called on incompatible receiver "+vm.DisplayString(this))
}

// bigIntToEpochNs returns the BigInt v as epoch nanoseconds, or throws a RangeError when they
// are not valid.
func bigIntToEpochNs(vm *engine.VM, v engine.Value) temporal.Int128 {
	b := vm.ToBigInt(v).BigInt()
	ns, ok := temporal.Int128FromBig(b)
	if !ok || !temporal.IsValidEpochNanoseconds(ns) {
		vm.ThrowRangeError(vm.CurrentRealm(), "epoch nanoseconds are out of range")
	}
	return ns
}

// epochNsValue returns ns as a BigInt.
func epochNsValue(vm *engine.VM, ns temporal.Int128) engine.Value {
	return vm.NewBigInt(ns.Big())
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
func initTemporal(vm *engine.VM) {
	ns := vm.NewLazyObject(vm.Intrinsic(engine.IntrinsicObjectPrototype), func(ns *engine.Object) {
		vm.Define(ns, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Temporal"), engine.FlagConfigurable)
		initTemporalInstant(vm, ns)
		initTemporalDuration(vm, ns)
		initTemporalPlainDate(vm, ns)
		initTemporalPlainTime(vm, ns)
		initTemporalPlainDateTime(vm, ns)
		initTemporalPlainYearMonth(vm, ns)
		initTemporalPlainMonthDay(vm, ns)
		initTemporalZonedDateTime(vm, ns)
		initTemporalNow(vm, ns)
	})
	vm.SetIntrinsic(engine.IntrinsicTemporal, ns)
	vm.Define(vm.GlobalObject().Object(), vm.Intern("Temporal"), engine.ObjectValue(ns), engine.FlagsHidden)

	tmethod(vm, vm.Intrinsic(engine.IntrinsicDatePrototype), "toTemporalInstant", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		tv := thisDate(vm, this, "toTemporalInstant")
		if math.IsNaN(tv) {
			vm.ThrowRangeError(vm.CurrentRealm(), "invalid time value")
		}
		return newInstant(vm, temporal.Mul64(int64(tv), 1_000_000), nil)
	})
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
