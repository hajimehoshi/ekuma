// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/temporal"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// durationFieldNames are the names of the fields of a duration, from years to nanoseconds.
var durationFieldNames = [10]string{"years", "months", "weeks", "days", "hours", "minutes", "seconds", "milliseconds", "microseconds", "nanoseconds"}

// newDuration implements CreateTemporalDuration for a valid duration.
func newDuration(cx *engine.ExecutionContext, d temporal.Duration, nt *engine.Object) engine.Value {
	proto := cx.GetPrototypeFromConstructor(nt, engine.IntrinsicTemporalDurationPrototype)
	return storage.ObjectValue(cx.NewTemporalObject(proto, &durationSlots{
		d: d,
	}))
}

// createDuration implements CreateTemporalDuration.
func createDuration(cx *engine.ExecutionContext, d temporal.Duration, nt *engine.Object) engine.Value {
	if !temporal.IsValidDuration(&d) {
		cx.ThrowRangeError("duration is out of range")
	}
	return newDuration(cx, d, nt)
}

func thisDuration(cx *engine.ExecutionContext, this engine.Value, name string) *durationSlots {
	s, ok := storage.TemporalData(this).(*durationSlots)
	if !ok {
		incompatible(cx, "Temporal.Duration.prototype."+name, this)
	}
	return s
}

// toPartialDuration implements ToTemporalPartialDurationRecord. It returns the fields and
// which of them are present.
func toPartialDuration(cx *engine.ExecutionContext, v engine.Value) ([10]float64, [10]bool) {
	var f [10]float64
	var has [10]bool
	if !v.IsObject() {
		cx.ThrowTypeError("duration-like must be an object")
	}
	o := v.Object()
	any := false
	// The fields in alphabetical order, with their indices.
	for _, i := range []int{3, 4, 8, 7, 5, 1, 9, 6, 2, 0} {
		x := get(cx, o, durationFieldNames[i])
		if x.IsUndefined() {
			continue
		}
		f[i] = toIntegerIfIntegral(cx, x, durationFieldNames[i])
		has[i] = true
		any = true
	}
	if !any {
		cx.ThrowTypeError("duration-like has no duration fields")
	}
	return f, has
}

// toTemporalDuration implements ToTemporalDuration, returning the fields.
func toTemporalDuration(cx *engine.ExecutionContext, item engine.Value) temporal.Duration {
	if s, ok := storage.TemporalData(item).(*durationSlots); ok {
		return s.d
	}
	if !item.IsObject() {
		str := requireString(cx, item, "duration")
		d := must(temporal.ParseTemporalDurationString(str, cx.Poll))
		if !temporal.IsValidDuration(&d) {
			cx.ThrowRangeError("duration is out of range")
		}
		return d
	}
	f, _ := toPartialDuration(cx, item)
	d := temporal.DurationFromFields(f)
	if !temporal.IsValidDuration(&d) {
		cx.ThrowRangeError("duration is out of range")
	}
	return d
}

// relativeTo is the result of GetTemporalRelativeToOption: either of the fields or none is set.
type relativeTo struct {
	plain *plainDateSlots
	zoned *zonedDateTimeSlots
}

// getRelativeToOption implements GetTemporalRelativeToOption.
func getRelativeToOption(cx *engine.ExecutionContext, opts *engine.Object) relativeTo {
	v := get(cx, opts, "relativeTo")
	if v.IsUndefined() {
		return relativeTo{}
	}
	behaviour := temporal.OffsetBehaviourOption
	match := temporal.MatchExactly
	var date temporal.ISODate
	var t temporal.Time
	startOfDay := false
	var cal, offset string
	var tz temporal.TimeZone
	hasTZ := false
	if v.IsObject() {
		switch s := storage.TemporalData(v).(type) {
		case *zonedDateTimeSlots:
			return relativeTo{
				zoned: s,
			}
		case *plainDateSlots:
			return relativeTo{
				plain: s,
			}
		case *plainDateTimeSlots:
			return relativeTo{
				plain: &plainDateSlots{
					date: s.dt.Date,
					cal:  s.cal,
				},
			}
		}
		o := v.Object()
		cal = calendarWithISODefault(cx, o)
		names := append(append(append([]string{}, dateFieldNames...), timeFieldNames...), "offset", "timeZone")
		f := prepareCalendarFields(cx, cal, o, names, fieldsNoneRequired)
		dt := interpretDateTimeFields(cx, cal, f, temporal.OverflowConstrain)
		date, t = dt.Date, dt.Time
		tz, hasTZ = f.TimeZone, f.HasTimeZone
		offset = f.Offset
		if !f.HasOffset {
			behaviour = temporal.OffsetBehaviourWall
		}
	} else {
		str := requireString(cx, v, "relativeTo")
		p := must(temporal.ParseISODateTime(str, cx.Poll, temporal.GoalZonedDateTime, temporal.GoalDateTime))
		if p.TimeZoneAnnotation != "" {
			tz = toTimeZone(cx, cx.NewString(wtf8.String(p.TimeZoneAnnotation)))
			hasTZ = true
			switch {
			case p.Z:
				behaviour = temporal.OffsetBehaviourExact
			case p.Offset == "":
				behaviour = temporal.OffsetBehaviourWall
			}
			match = temporal.MatchMinutes
			if p.Offset != "" && !temporal.IsOffsetWithoutSubMinutes(p.Offset) {
				match = temporal.MatchExactly
			}
		} else if p.Z {
			cx.ThrowRangeError("a UTC designator needs a time zone annotation in relativeTo")
		}
		cal = canonicalizeParsedCalendar(cx, p.Calendar)
		date = temporal.ISODate{Year: p.Year, Month: p.Month, Day: p.Day}
		t = p.Time
		startOfDay = p.StartOfDay
		offset = p.Offset
	}
	if !hasTZ {
		if !temporal.ISODateWithinLimits(date) {
			cx.ThrowRangeError("relativeTo is out of range")
		}
		return relativeTo{
			plain: &plainDateSlots{
				date: date,
				cal:  cal,
			},
		}
	}
	var offsetNs int64
	if behaviour == temporal.OffsetBehaviourOption {
		offsetNs = must(temporal.ParseDateTimeUTCOffset(offset))
	}
	ns := must(temporal.InterpretISODateTimeOffset(date, t, startOfDay, behaviour, offsetNs, tz, temporal.DisambiguationCompatible, temporal.OffsetReject, match))
	return relativeTo{
		zoned: &zonedDateTimeSlots{
			ns:  ns,
			tz:  tz,
			cal: cal,
		},
	}
}

// addDurations implements AddDurations.
func addDurations(cx *engine.ExecutionContext, d temporal.Duration, otherV engine.Value, subtract bool) temporal.Duration {
	other := toTemporalDuration(cx, otherV)
	if subtract {
		other = other.Negated()
	}
	largest := temporal.LargerOfTwoTemporalUnits(temporal.DefaultTemporalLargestUnit(&d), temporal.DefaultTemporalLargestUnit(&other))
	if largest.IsCalendarUnit() {
		cx.ThrowRangeError("durations with years, months or weeks need a relativeTo to be added")
	}
	d1 := temporal.ToInternalDurationRecordWith24HourDays(&d)
	d2 := temporal.ToInternalDurationRecordWith24HourDays(&other)
	sum := must(temporal.AddTimeDuration(d1.Time, d2.Time))
	return must(temporal.TemporalDurationFromInternal(temporal.CombineDateAndTimeDuration(temporal.DateDuration{}, sum), largest))
}

// plainRelativeEnd returns the date-times of a duration added to a plain relativeTo, as
// Duration.prototype.round and total compute them.
func plainRelativeEnd(cx *engine.ExecutionContext, d *temporal.Duration, rel *plainDateSlots) (temporal.InternalDuration, temporal.ISODateTime, temporal.ISODateTime) {
	id := temporal.ToInternalDurationRecordWith24HourDays(d)
	targetTime, days := temporal.AddTime(temporal.MidnightTime, id.Time)
	dd := must(temporal.AdjustDateDurationRecord(id.Date, days, -1<<63, -1<<63))
	targetDate := must(temporal.CalendarDateAdd(rel.cal, rel.date, dd, temporal.OverflowConstrain))
	return id, temporal.ISODateTime{
		Date: rel.date,
	}, temporal.ISODateTime{
		Date: targetDate,
		Time: targetTime,
	}
}

// validateDifferenceIncrement throws a RangeError when increment is not a valid rounding increment for unit.
func validateDifferenceIncrement(cx *engine.ExecutionContext, unit temporal.Unit, increment int64) {
	if maximum, ok := temporal.MaximumTemporalDurationRoundingIncrement(unit); ok {
		check(temporal.ValidateTemporalRoundingIncrement(increment, maximum, false))
	}
}

func initTemporalDuration(cx *engine.ExecutionContext, ns *engine.Object) {
	ctor, proto := temporalConstructor(cx, ns, "Duration", 0, engine.IntrinsicTemporalDurationPrototype, nativeDurationConstructor)
	method(cx, ctor, "from", 1, nativeDurationFrom)
	method(cx, ctor, "compare", 2, nativeDurationCompare)

	getter(cx, proto, cx.Intern("years"), "years", nativeDurationYears)
	getter(cx, proto, cx.Intern("months"), "months", nativeDurationMonths)
	getter(cx, proto, cx.Intern("weeks"), "weeks", nativeDurationWeeks)
	getter(cx, proto, cx.Intern("days"), "days", nativeDurationDays)
	getter(cx, proto, cx.Intern("hours"), "hours", nativeDurationHours)
	getter(cx, proto, cx.Intern("minutes"), "minutes", nativeDurationMinutes)
	getter(cx, proto, cx.Intern("seconds"), "seconds", nativeDurationSeconds)
	getter(cx, proto, cx.Intern("milliseconds"), "milliseconds", nativeDurationMilliseconds)
	getter(cx, proto, cx.Intern("microseconds"), "microseconds", nativeDurationMicroseconds)
	getter(cx, proto, cx.Intern("nanoseconds"), "nanoseconds", nativeDurationNanoseconds)
	getter(cx, proto, cx.Intern("sign"), "sign", nativeDurationSign)
	getter(cx, proto, cx.Intern("blank"), "blank", nativeDurationBlank)
	method(cx, proto, "with", 1, nativeDurationWith)
	method(cx, proto, "negated", 0, nativeDurationNegated)
	method(cx, proto, "abs", 0, nativeDurationAbs)
	method(cx, proto, "add", 1, nativeDurationAdd)
	method(cx, proto, "subtract", 1, nativeDurationSubtract)
	method(cx, proto, "round", 1, nativeDurationRound)
	method(cx, proto, "total", 1, nativeDurationTotal)
	method(cx, proto, "toString", 0, nativeDurationToString)
	method(cx, proto, "toJSON", 0, nativeDurationToJSON)
	method(cx, proto, "toLocaleString", 0, nativeDurationToLocaleString)
	method(cx, proto, "valueOf", 0, nativeDurationValueOf)
}

// The native functions of Temporal.Duration.
var (
	nativeDurationConstructor    = engine.RegisterNative(temporalNative(durationConstructor))
	nativeDurationFrom           = engine.RegisterNative(temporalNative(durationFrom))
	nativeDurationCompare        = engine.RegisterNative(temporalNative(durationCompare))
	nativeDurationYears          = engine.RegisterNative(durationFieldNative(0, "years"))
	nativeDurationMonths         = engine.RegisterNative(durationFieldNative(1, "months"))
	nativeDurationWeeks          = engine.RegisterNative(durationFieldNative(2, "weeks"))
	nativeDurationDays           = engine.RegisterNative(durationFieldNative(3, "days"))
	nativeDurationHours          = engine.RegisterNative(durationFieldNative(4, "hours"))
	nativeDurationMinutes        = engine.RegisterNative(durationFieldNative(5, "minutes"))
	nativeDurationSeconds        = engine.RegisterNative(durationFieldNative(6, "seconds"))
	nativeDurationMilliseconds   = engine.RegisterNative(durationFieldNative(7, "milliseconds"))
	nativeDurationMicroseconds   = engine.RegisterNative(durationFieldNative(8, "microseconds"))
	nativeDurationNanoseconds    = engine.RegisterNative(durationFieldNative(9, "nanoseconds"))
	nativeDurationSign           = engine.RegisterNative(temporalNative(durationSign))
	nativeDurationBlank          = engine.RegisterNative(temporalNative(durationBlank))
	nativeDurationWith           = engine.RegisterNative(temporalNative(durationWith))
	nativeDurationNegated        = engine.RegisterNative(temporalNative(durationNegated))
	nativeDurationAbs            = engine.RegisterNative(temporalNative(durationAbs))
	nativeDurationAdd            = engine.RegisterNative(durationAddNative("add"))
	nativeDurationSubtract       = engine.RegisterNative(durationAddNative("subtract"))
	nativeDurationRound          = engine.RegisterNative(temporalNative(durationRound))
	nativeDurationTotal          = engine.RegisterNative(temporalNative(durationTotal))
	nativeDurationToString       = engine.RegisterNative(temporalNative(durationToString))
	nativeDurationToJSON         = engine.RegisterNative(durationToJSONNative("toJSON"))
	nativeDurationToLocaleString = engine.RegisterNative(durationToJSONNative("toLocaleString"))
	nativeDurationValueOf        = engine.RegisterNative(temporalValueOfNative("Duration"))
)

// durationFieldNative returns the getter of the field i, which is called name, of
// Temporal.Duration.prototype.
//
//go:noinline
func durationFieldNative(i int, name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		f := thisDuration(cx, this, name).d.Fields()
		return storage.Number(f[i])
	})
}

// durationAddNative returns the method name of Temporal.Duration.prototype, which is add or
// subtract.
//
//go:noinline
func durationAddNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		s := thisDuration(cx, this, name)
		return newDuration(cx, addDurations(cx, s.d, arg(args, 0), name == "subtract"), nil)
	})
}

// durationToJSONNative returns the method name of Temporal.Duration.prototype, which is toJSON or
// toLocaleString.
//
//go:noinline
func durationToJSONNative(name string) engine.NativeFunc {
	return temporalNative(func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return cx.NewString(wtf8.String(temporal.TemporalDurationToString(&thisDuration(cx, this, name).d, temporal.PrecisionAuto)))
	})
}

func durationConstructor(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	nt := newTarget(cx, "Temporal.Duration")
	var f [10]float64
	for i := range f {
		if v := arg(args, i); !v.IsUndefined() {
			f[i] = toIntegerIfIntegral(cx, v, durationFieldNames[i])
		}
	}
	return createDuration(cx, temporal.DurationFromFields(f), nt)
}

func durationFrom(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return newDuration(cx, toTemporalDuration(cx, arg(args, 0)), nil)
}

func durationCompare(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	one := toTemporalDuration(cx, arg(args, 0))
	two := toTemporalDuration(cx, arg(args, 1))
	opts := getOptionsObject(cx, arg(args, 2))
	rel := getRelativeToOption(cx, opts)
	if one.Fields() == two.Fields() {
		return storage.Number(0)
	}
	lu1 := temporal.DefaultTemporalLargestUnit(&one)
	lu2 := temporal.DefaultTemporalLargestUnit(&two)
	d1 := temporal.ToInternalDurationRecord(&one)
	d2 := temporal.ToInternalDurationRecord(&two)
	if rel.zoned != nil && (lu1.IsDateUnit() || lu2.IsDateUnit()) {
		z := rel.zoned
		after1 := must(temporal.AddZonedDateTime(z.ns, z.tz, z.cal, d1, temporal.OverflowConstrain))
		after2 := must(temporal.AddZonedDateTime(z.ns, z.tz, z.cal, d2, temporal.OverflowConstrain))
		return compareResult(after1.Cmp(after2))
	}
	days1, days2 := d1.Date.Days, d2.Date.Days
	if lu1.IsCalendarUnit() || lu2.IsCalendarUnit() {
		if rel.plain == nil {
			cx.ThrowRangeError("durations with years, months or weeks need a relativeTo to be compared")
		}
		days1 = must(temporal.DateDurationDays(d1.Date, rel.plain.date, rel.plain.cal))
		days2 = must(temporal.DateDurationDays(d2.Date, rel.plain.date, rel.plain.cal))
	}
	t1 := must(temporal.Add24HourDaysToTimeDuration(d1.Time, days1))
	t2 := must(temporal.Add24HourDaysToTimeDuration(d2.Time, days2))
	return compareResult(t1.Cmp(t2))
}

func durationSign(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return storage.Number(float64(temporal.DurationSign(&thisDuration(cx, this, "sign").d)))
}

func durationBlank(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return storage.Bool(temporal.DurationSign(&thisDuration(cx, this, "blank").d) == 0)
}

func durationWith(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	s := thisDuration(cx, this, "with")
	pf, has := toPartialDuration(cx, arg(args, 0))
	f := s.d.Fields()
	for i := range f {
		if has[i] {
			f[i] = pf[i]
		}
	}
	return createDuration(cx, temporal.DurationFromFields(f), nil)
}

func durationNegated(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return newDuration(cx, thisDuration(cx, this, "negated").d.Negated(), nil)
}

func durationAbs(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	f := thisDuration(cx, this, "abs").d.Fields()
	for i := range f {
		f[i] = math.Abs(f[i])
	}
	return newDuration(cx, temporal.DurationFromFields(f), nil)
}

func durationRound(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	s := thisDuration(cx, this, "round")
	d := s.d
	opts := roundToOptions(cx, arg(args, 0), "smallestUnit")
	existingLargest := temporal.DefaultTemporalLargestUnit(&d)
	largest := getUnitOption(cx, opts, "largestUnit", false)
	rel := getRelativeToOption(cx, opts)
	increment := getRoundingIncrementOption(cx, opts)
	mode := getRoundingModeOption(cx, opts, temporal.RoundHalfExpand)
	smallest := getUnitOption(cx, opts, "smallestUnit", false)
	validateUnit(cx, smallest, unitGroupDateTime)
	smallestPresent := smallest != temporal.UnitUnset
	if !smallestPresent {
		smallest = temporal.UnitNanosecond
	}
	defaultLargest := temporal.LargerOfTwoTemporalUnits(existingLargest, smallest)
	largestPresent := largest != temporal.UnitUnset
	if !largestPresent || largest == temporal.UnitAuto {
		largest = defaultLargest
	}
	if !smallestPresent && !largestPresent {
		cx.ThrowRangeError("smallestUnit or largestUnit is required")
	}
	validateUnit(cx, largest, unitGroupDateTime)
	if temporal.LargerOfTwoTemporalUnits(largest, smallest) != largest {
		cx.ThrowRangeError("largestUnit must not be smaller than smallestUnit")
	}
	validateDifferenceIncrement(cx, smallest, increment)
	if increment > 1 && smallest.IsDateUnit() && largest != smallest {
		cx.ThrowRangeError("a roundingIncrement of a date unit needs the same largestUnit")
	}
	if z := rel.zoned; z != nil {
		id := temporal.ToInternalDurationRecord(&d)
		target := must(temporal.AddZonedDateTime(z.ns, z.tz, z.cal, id, temporal.OverflowConstrain))
		id = must(temporal.DifferenceZonedDateTimeWithRounding(z.ns, target, z.tz, z.cal, largest, increment, smallest, mode))
		if largest.IsDateUnit() {
			largest = temporal.UnitHour
		}
		return newDuration(cx, must(temporal.TemporalDurationFromInternal(id, largest)), nil)
	}
	if p := rel.plain; p != nil {
		_, from, to := plainRelativeEnd(cx, &d, p)
		id := must(temporal.DifferencePlainDateTimeWithRounding(from, to, p.cal, largest, increment, smallest, mode))
		return newDuration(cx, must(temporal.TemporalDurationFromInternal(id, largest)), nil)
	}
	if existingLargest.IsCalendarUnit() || largest.IsCalendarUnit() {
		cx.ThrowRangeError("durations with years, months or weeks need a relativeTo to be rounded")
	}
	id := temporal.ToInternalDurationRecordWith24HourDays(&d)
	if smallest == temporal.UnitDay {
		rounded := must(temporal.RoundTimeDuration(id.Time, increment, temporal.UnitDay, mode))
		days, _ := rounded.QuoRemInt64(temporal.NsPerDay)
		dd, _ := days.Int64()
		id = temporal.CombineDateAndTimeDuration(temporal.DateDuration{
			Days: dd,
		}, temporal.Int128{})
	} else {
		t := must(temporal.RoundTimeDuration(id.Time, increment, smallest, mode))
		id = temporal.CombineDateAndTimeDuration(temporal.DateDuration{}, t)
	}
	return newDuration(cx, must(temporal.TemporalDurationFromInternal(id, largest)), nil)
}

func durationTotal(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	s := thisDuration(cx, this, "total")
	d := s.d
	opts := roundToOptions(cx, arg(args, 0), "unit")
	rel := getRelativeToOption(cx, opts)
	unit := getUnitOption(cx, opts, "unit", true)
	validateUnit(cx, unit, unitGroupDateTime)
	if z := rel.zoned; z != nil {
		id := temporal.ToInternalDurationRecord(&d)
		target := must(temporal.AddZonedDateTime(z.ns, z.tz, z.cal, id, temporal.OverflowConstrain))
		return storage.Number(must(temporal.DifferenceZonedDateTimeWithTotal(z.ns, target, z.tz, z.cal, unit)))
	}
	if p := rel.plain; p != nil {
		_, from, to := plainRelativeEnd(cx, &d, p)
		return storage.Number(must(temporal.DifferencePlainDateTimeWithTotal(from, to, p.cal, unit)))
	}
	largest := temporal.DefaultTemporalLargestUnit(&d)
	if largest.IsCalendarUnit() || unit.IsCalendarUnit() {
		cx.ThrowRangeError("durations with years, months or weeks need a relativeTo to be totaled")
	}
	id := temporal.ToInternalDurationRecordWith24HourDays(&d)
	return storage.Number(temporal.TotalTimeDuration(id.Time, unit))
}

func durationToString(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	s := thisDuration(cx, this, "toString")
	d := s.d
	opts := getOptionsObject(cx, arg(args, 0))
	digits := getFractionalSecondDigitsOption(cx, opts)
	mode := getRoundingModeOption(cx, opts, temporal.RoundTrunc)
	unit := getUnitOption(cx, opts, "smallestUnit", false)
	validateUnit(cx, unit, unitGroupTime)
	if unit == temporal.UnitHour || unit == temporal.UnitMinute {
		cx.ThrowRangeError("smallestUnit must not be hour or minute")
	}
	p := temporal.ToSecondsStringPrecisionRecord(unit, digits)
	if p.Unit == temporal.UnitNanosecond && p.Increment == 1 {
		return cx.NewString(wtf8.String(temporal.TemporalDurationToString(&d, p.Precision)))
	}
	largest := temporal.DefaultTemporalLargestUnit(&d)
	id := temporal.ToInternalDurationRecord(&d)
	t := must(temporal.RoundTimeDuration(id.Time, p.Increment, p.Unit, mode))
	id = temporal.CombineDateAndTimeDuration(id.Date, t)
	rd := must(temporal.TemporalDurationFromInternal(id, temporal.LargerOfTwoTemporalUnits(largest, temporal.UnitSecond)))
	return cx.NewString(wtf8.String(temporal.TemporalDurationToString(&rd, p.Precision)))
}
