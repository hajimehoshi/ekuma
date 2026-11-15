// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"errors"
	"math"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/temporal"
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

// temporalRealm holds the prototypes of the Temporal types of a VM.
type temporalRealm struct {
	instant        *engine.Object
	duration       *engine.Object
	plainDate      *engine.Object
	plainTime      *engine.Object
	plainDateTime  *engine.Object
	plainYearMonth *engine.Object
	plainMonthDay  *engine.Object
	zonedDateTime  *engine.Object
}

// throwTemporal throws err, an error of the temporal package, as the error it stands for.
func throwTemporal(vm *engine.VM, err error) {
	if te, ok := errors.AsType[*temporal.Error](err); ok && te.Kind == temporal.TypeError {
		vm.ThrowTypeError(te.Message)
	}
	vm.ThrowRangeError(err.Error())
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
func tmethod(vm *engine.VM, o *engine.Object, name string, length int, f engine.NativeFunc) *engine.Object {
	return method(vm, o, name, length, temporalNative(f))
}

// tgetter is getter for the functions of Temporal.
func tgetter(vm *engine.VM, o *engine.Object, key *engine.Object, name string, f engine.NativeFunc) {
	getter(vm, o, key, name, temporalNative(f))
}

// get returns the property name of o.
func get(vm *engine.VM, o *engine.Object, name string) engine.Value {
	return vm.Get(o, engine.NamedKey(vm.Intern(name)))
}

// requireString returns v as a Go string, or throws a TypeError when it is not a String.
func requireString(vm *engine.VM, v engine.Value, what string) string {
	if !v.IsString() {
		vm.ThrowTypeError(what + " must be a string, not " + vm.DisplayString(v))
	}
	return vm.ToString(v)
}

// toIntegerWithTruncation implements ToIntegerWithTruncation.
func toIntegerWithTruncation(vm *engine.VM, v engine.Value, what string) float64 {
	n := vm.ToNumber(v)
	if math.IsNaN(n) || math.IsInf(n, 0) {
		vm.ThrowRangeError(what + " must be a finite number")
	}
	return math.Trunc(n) + 0
}

// toPositiveIntegerWithTruncation implements ToPositiveIntegerWithTruncation.
func toPositiveIntegerWithTruncation(vm *engine.VM, v engine.Value, what string) float64 {
	n := toIntegerWithTruncation(vm, v, what)
	if n <= 0 {
		vm.ThrowRangeError(what + " must be positive")
	}
	return n
}

// toIntegerIfIntegral implements ToIntegerIfIntegral.
func toIntegerIfIntegral(vm *engine.VM, v engine.Value, what string) float64 {
	n := vm.ToNumber(v)
	if math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
		vm.ThrowRangeError(what + " must be an integer")
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
	vm.ThrowTypeError("options must be an object, not " + vm.DisplayString(v))
	return nil
}

// temporalOption implements GetOption for a String option whose allowed values are values. It
// returns the index of the value, or fallback when the option is undefined. A negative
// fallback makes the option required.
func temporalOption(vm *engine.VM, opts *engine.Object, name string, values []string, fallback int) int {
	v := get(vm, opts, name)
	if v.IsUndefined() {
		if fallback < 0 {
			vm.ThrowRangeError(name + " is required")
		}
		return fallback
	}
	s := vm.ToString(v)
	if i := slices.Index(values, s); i >= 0 {
		return i
	}
	vm.ThrowRangeError(vm.DisplayString(vm.NewString(s)) + " is not a valid value for " + name)
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
		vm.ThrowRangeError("roundingIncrement must be from 1 to 1e9")
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
			vm.ThrowRangeError("fractionalSecondDigits must be 'auto' or from 0 to 9")
		}
		return temporal.PrecisionAuto
	}
	n := v.Number()
	if math.IsNaN(n) || math.IsInf(n, 0) {
		vm.ThrowRangeError("fractionalSecondDigits must be 'auto' or from 0 to 9")
	}
	d := math.Floor(n)
	if d < 0 || d > 9 {
		vm.ThrowRangeError("fractionalSecondDigits must be 'auto' or from 0 to 9")
	}
	return temporal.Precision(d)
}

// getUnitOption implements GetTemporalUnitValuedOption. It returns UnitUnset for an undefined
// option that is not required.
func getUnitOption(vm *engine.VM, opts *engine.Object, key string, required bool) temporal.Unit {
	v := get(vm, opts, key)
	if v.IsUndefined() {
		if required {
			vm.ThrowRangeError(key + " is required")
		}
		return temporal.UnitUnset
	}
	s := vm.ToString(v)
	u, ok := temporal.UnitFromString(s)
	if !ok {
		vm.ThrowRangeError(vm.DisplayString(vm.NewString(s)) + " is not a valid value for " + key)
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
	vm.ThrowRangeError(u.String() + " is not a valid unit here")
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
		vm.ThrowRangeError(largest.String() + " is not a valid value for largestUnit")
	}
	validateUnit(vm, smallest, group)
	if smallest == temporal.UnitUnset {
		smallest = fallbackSmallest
	}
	if slices.Contains(disallowed, smallest) {
		vm.ThrowRangeError(smallest.String() + " is not a valid value for smallestUnit")
	}
	defaultLargest := temporal.LargerOfTwoTemporalUnits(smallestLargestDefault, smallest)
	if largest == temporal.UnitAuto {
		largest = defaultLargest
	}
	if temporal.LargerOfTwoTemporalUnits(largest, smallest) != largest {
		vm.ThrowRangeError("largestUnit must not be smaller than smallestUnit")
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
		vm.ThrowTypeError("options are required")
	}
	if v.IsString() {
		o := vm.NewObject(nil)
		vm.CreateDataPropertyOrThrow(o, engine.NamedKey(vm.Intern(key)), v)
		return o
	}
	return getOptionsObject(vm, v)
}

// systemTimeZone returns the time zone of the host for Temporal.
func systemTimeZone(vm *engine.VM) temporal.TimeZone {
	return temporal.SystemTimeZone(vm.DefaultTimeZone())
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
	name, off, isOff, err := temporal.ParseTemporalTimeZoneString(str)
	check(err)
	var system temporal.TimeZone
	if !isOff {
		system = systemTimeZone(vm)
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
	if r, err := temporal.ParseISODateTime(s, temporal.AnyGoal...); err == nil {
		if r.Calendar == "" {
			return temporal.ISO8601
		}
		return r.Calendar
	}
	if !temporal.IsAnnotationValue(s) {
		vm.ThrowRangeError("invalid calendar: " + s)
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

// prepareCalendarFields implements PrepareCalendarFields for the ISO 8601 calendar. names are
// the names of the fields to read.
func prepareCalendarFields(vm *engine.VM, bag *engine.Object, names []string, req fieldsRequirement) temporal.CalendarFields {
	names = slices.Clone(names)
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
				vm.ThrowTypeError("timeZone is required")
			}
			continue
		}
		any = true
		switch name {
		case "year":
			f.Year, f.HasYear = toIntegerWithTruncation(vm, v, name), true
		case "month":
			f.Month, f.HasMonth = toPositiveIntegerWithTruncation(vm, v, name), true
		case "day":
			f.Day, f.HasDay = toPositiveIntegerWithTruncation(vm, v, name), true
		case "monthCode":
			p := vm.ToPrimitive(v, engine.HintString)
			if !p.IsString() {
				vm.ThrowTypeError("monthCode must be a string")
			}
			s := vm.ToString(p)
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
				vm.ThrowTypeError("offset must be a string")
			}
			s := vm.ToString(p)
			must(temporal.ParseDateTimeUTCOffset(s))
			f.Offset, f.HasOffset = s, true
		case "timeZone":
			f.TimeZone, f.HasTimeZone = toTimeZone(vm, v), true
		}
	}
	if req == fieldsPartial && !any {
		vm.ThrowTypeError("no fields are given")
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
		vm.ThrowTypeError("no time fields are given")
	}
	if !partial {
		r.has = [6]bool{true, true, true, true, true, true}
	}
	return r
}

// interpretDateTimeFields implements InterpretTemporalDateTimeFields.
func interpretDateTimeFields(vm *engine.VM, f temporal.CalendarFields, overflow temporal.Overflow) temporal.ISODateTime {
	d := must(temporal.CalendarDateFromFields(f, overflow))
	t := must(temporal.RegulateTime(f.Hour, f.Minute, f.Second, f.Millisecond, f.Microsecond, f.Nanosecond, overflow))
	return temporal.ISODateTime{
		Date: d,
		Time: t,
	}
}

// rejectTemporalLikeObject implements IsPartialTemporalObject's rejections.
func rejectTemporalLikeObject(vm *engine.VM, v engine.Value) *engine.Object {
	if !v.IsObject() {
		vm.ThrowTypeError("argument must be an object")
	}
	switch engine.TemporalData(v).(type) {
	case *plainDateSlots, *plainDateTimeSlots, *plainMonthDaySlots, *plainTimeSlots, *plainYearMonthSlots, *zonedDateTimeSlots:
		vm.ThrowTypeError("argument must be a partial object, not a Temporal object")
	}
	o := v.Object()
	if !get(vm, o, "calendar").IsUndefined() {
		vm.ThrowTypeError("calendar is not allowed")
	}
	if !get(vm, o, "timeZone").IsUndefined() {
		vm.ThrowTypeError("timeZone is not allowed")
	}
	return o
}

// newTarget returns the new.target of a constructor, or throws a TypeError when it was called.
func newTarget(vm *engine.VM, name string) *engine.Object {
	nt := vm.GetNewTarget()
	if nt == nil {
		vm.ThrowTypeError("constructor " + name + " requires 'new'")
	}
	return nt
}

// protoFor returns the prototype of a new object for newTarget, or proto when newTarget is nil.
func protoFor(vm *engine.VM, nt *engine.Object, proto *engine.Object) *engine.Object {
	if nt == nil {
		return proto
	}
	return vm.GetPrototypeFromConstructor(nt, proto)
}

// temporalConstructor defines the constructor name of the Temporal namespace ns with the
// prototype proto and the Symbol.toStringTag of the prototype.
func temporalConstructor(vm *engine.VM, ns *engine.Object, name string, length int, proto *engine.Object, f engine.NativeFunc) *engine.Object {
	c := vm.NewNativeConstructor(name, length, temporalNative(f))
	vm.Define(c, vm.Key(engine.KeyPrototype), engine.ObjectValue(proto), 0)
	vm.Define(proto, vm.Key(engine.KeyConstructor), engine.ObjectValue(c), engine.FlagsHidden)
	vm.Define(ns, vm.Intern(name), engine.ObjectValue(c), engine.FlagsHidden)
	vm.Define(proto, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Temporal."+name), engine.FlagConfigurable)
	return c
}

// valueOfThrows defines the valueOf method of the prototype of the type name, which throws.
func valueOfThrows(vm *engine.VM, proto *engine.Object, name string) {
	tmethod(vm, proto, "valueOf", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		vm.ThrowTypeError("use Temporal." + name + ".compare or equals to compare Temporal." + name + " values, and toString to convert them")
		return engine.Undefined
	})
}

// incompatible throws the TypeError of a method called on a receiver of a wrong type.
func incompatible(vm *engine.VM, what string, this engine.Value) {
	vm.ThrowTypeError(what + " called on incompatible receiver " + vm.DisplayString(this))
}

// bigIntToEpochNs returns the BigInt v as epoch nanoseconds, or throws a RangeError when they
// are not valid.
func bigIntToEpochNs(vm *engine.VM, v engine.Value) temporal.Int128 {
	b := vm.ToBigInt(v).Object().BigInt()
	ns, ok := temporal.Int128FromBig(b)
	if !ok || !temporal.IsValidEpochNanoseconds(ns) {
		vm.ThrowRangeError("epoch nanoseconds are out of range")
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
	var r *temporalRealm
	ns := vm.NewLazyObject(vm.Intrinsic(engine.IntrinsicObjectPrototype), func(ns *engine.Object) {
		r = newTemporalRealm(vm, ns)
	})
	vm.Define(vm.GlobalObject().Object(), vm.Intern("Temporal"), engine.ObjectValue(ns), engine.FlagsHidden)

	tmethod(vm, vm.Intrinsic(engine.IntrinsicDatePrototype), "toTemporalInstant", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		tv := thisDate(vm, this, "toTemporalInstant")
		if math.IsNaN(tv) {
			vm.ThrowRangeError("invalid time value")
		}
		vm.InitLazyObject(ns)
		return r.newInstant(vm, temporal.Mul64(int64(tv), 1_000_000), nil)
	})
}

// newTemporalRealm defines the properties of the Temporal namespace ns.
func newTemporalRealm(vm *engine.VM, ns *engine.Object) *temporalRealm {
	vm.Define(ns, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.SharedString("Temporal"), engine.FlagConfigurable)
	objectProto := vm.Intrinsic(engine.IntrinsicObjectPrototype)
	r := &temporalRealm{
		instant:        vm.NewObject(objectProto),
		duration:       vm.NewObject(objectProto),
		plainDate:      vm.NewObject(objectProto),
		plainTime:      vm.NewObject(objectProto),
		plainDateTime:  vm.NewObject(objectProto),
		plainYearMonth: vm.NewObject(objectProto),
		plainMonthDay:  vm.NewObject(objectProto),
		zonedDateTime:  vm.NewObject(objectProto),
	}
	initTemporalInstant(vm, ns, r)
	initTemporalDuration(vm, ns, r)
	initTemporalPlainDate(vm, ns, r)
	initTemporalPlainTime(vm, ns, r)
	initTemporalPlainDateTime(vm, ns, r)
	initTemporalPlainYearMonth(vm, ns, r)
	initTemporalPlainMonthDay(vm, ns, r)
	initTemporalZonedDateTime(vm, ns, r)
	initTemporalNow(vm, ns, r)
	return r
}
