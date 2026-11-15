// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/temporal"
)

// durationFieldNames are the names of the fields of a duration, from years to nanoseconds.
var durationFieldNames = [10]string{"years", "months", "weeks", "days", "hours", "minutes", "seconds", "milliseconds", "microseconds", "nanoseconds"}

// newDuration implements CreateTemporalDuration for a valid duration.
func (r *temporalRealm) newDuration(vm *engine.VM, d temporal.Duration, nt *engine.Object) engine.Value {
	return engine.ObjectValue(vm.NewTemporalObject(protoFor(vm, nt, r.duration), &durationSlots{
		d: d,
	}))
}

// createDuration implements CreateTemporalDuration.
func (r *temporalRealm) createDuration(vm *engine.VM, d temporal.Duration, nt *engine.Object) engine.Value {
	if !temporal.IsValidDuration(&d) {
		vm.ThrowRangeError("duration is out of range")
	}
	return r.newDuration(vm, d, nt)
}

func thisDuration(vm *engine.VM, this engine.Value, name string) *durationSlots {
	s, ok := engine.TemporalData(this).(*durationSlots)
	if !ok {
		incompatible(vm, "Temporal.Duration.prototype."+name, this)
	}
	return s
}

// toPartialDuration implements ToTemporalPartialDurationRecord. It returns the fields and
// which of them are present.
func toPartialDuration(vm *engine.VM, v engine.Value) ([10]float64, [10]bool) {
	var f [10]float64
	var has [10]bool
	if !v.IsObject() {
		vm.ThrowTypeError("duration-like must be an object")
	}
	o := v.Object()
	any := false
	// The fields in alphabetical order, with their indices.
	for _, i := range []int{3, 4, 8, 7, 5, 1, 9, 6, 2, 0} {
		x := get(vm, o, durationFieldNames[i])
		if x.IsUndefined() {
			continue
		}
		f[i] = toIntegerIfIntegral(vm, x, durationFieldNames[i])
		has[i] = true
		any = true
	}
	if !any {
		vm.ThrowTypeError("duration-like has no duration fields")
	}
	return f, has
}

// toTemporalDuration implements ToTemporalDuration, returning the fields.
func toTemporalDuration(vm *engine.VM, item engine.Value) temporal.Duration {
	if s, ok := engine.TemporalData(item).(*durationSlots); ok {
		return s.d
	}
	if !item.IsObject() {
		str := requireString(vm, item, "duration")
		d := must(temporal.ParseTemporalDurationString(str))
		if !temporal.IsValidDuration(&d) {
			vm.ThrowRangeError("duration is out of range")
		}
		return d
	}
	f, _ := toPartialDuration(vm, item)
	d := temporal.DurationFromFields(f)
	if !temporal.IsValidDuration(&d) {
		vm.ThrowRangeError("duration is out of range")
	}
	return d
}

// relativeTo is the result of GetTemporalRelativeToOption: either of the fields or none is set.
type relativeTo struct {
	plain *plainDateSlots
	zoned *zonedDateTimeSlots
}

// getRelativeToOption implements GetTemporalRelativeToOption.
func getRelativeToOption(vm *engine.VM, opts *engine.Object) relativeTo {
	v := get(vm, opts, "relativeTo")
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
		switch s := engine.TemporalData(v).(type) {
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
		cal = calendarWithISODefault(vm, o)
		names := append(append(append([]string{}, dateFieldNames...), timeFieldNames...), "offset", "timeZone")
		f := prepareCalendarFields(vm, cal, o, names, fieldsNoneRequired)
		dt := interpretDateTimeFields(vm, cal, f, temporal.OverflowConstrain)
		date, t = dt.Date, dt.Time
		tz, hasTZ = f.TimeZone, f.HasTimeZone
		offset = f.Offset
		if !f.HasOffset {
			behaviour = temporal.OffsetBehaviourWall
		}
	} else {
		str := requireString(vm, v, "relativeTo")
		p := must(temporal.ParseISODateTime(str, temporal.GoalZonedDateTime, temporal.GoalDateTime))
		if p.TimeZoneAnnotation != "" {
			tz = toTimeZone(vm, vm.NewString(p.TimeZoneAnnotation))
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
			vm.ThrowRangeError("a UTC designator needs a time zone annotation in relativeTo")
		}
		cal = canonicalizeParsedCalendar(vm, p.Calendar)
		date = temporal.ISODate{Year: p.Year, Month: p.Month, Day: p.Day}
		t = p.Time
		startOfDay = p.StartOfDay
		offset = p.Offset
	}
	if !hasTZ {
		if !temporal.ISODateWithinLimits(date) {
			vm.ThrowRangeError("relativeTo is out of range")
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
func addDurations(vm *engine.VM, d temporal.Duration, otherV engine.Value, subtract bool) temporal.Duration {
	other := toTemporalDuration(vm, otherV)
	if subtract {
		other = other.Negated()
	}
	largest := temporal.LargerOfTwoTemporalUnits(temporal.DefaultTemporalLargestUnit(&d), temporal.DefaultTemporalLargestUnit(&other))
	if largest.IsCalendarUnit() {
		vm.ThrowRangeError("durations with years, months or weeks need a relativeTo to be added")
	}
	d1 := temporal.ToInternalDurationRecordWith24HourDays(&d)
	d2 := temporal.ToInternalDurationRecordWith24HourDays(&other)
	sum := must(temporal.AddTimeDuration(d1.Time, d2.Time))
	return must(temporal.TemporalDurationFromInternal(temporal.CombineDateAndTimeDuration(temporal.DateDuration{}, sum), largest))
}

// plainRelativeEnd returns the date-times of a duration added to a plain relativeTo, as
// Duration.prototype.round and total compute them.
func plainRelativeEnd(vm *engine.VM, d *temporal.Duration, rel *plainDateSlots) (temporal.InternalDuration, temporal.ISODateTime, temporal.ISODateTime) {
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

// maximumDurationRoundIncrements are the limits of the rounding increments of the time units.
func validateDifferenceIncrement(vm *engine.VM, unit temporal.Unit, increment int64) {
	if maximum, ok := temporal.MaximumTemporalDurationRoundingIncrement(unit); ok {
		check(temporal.ValidateTemporalRoundingIncrement(increment, maximum, false))
	}
}

func initTemporalDuration(vm *engine.VM, ns *engine.Object, r *temporalRealm) {
	proto := r.duration
	ctor := temporalConstructor(vm, ns, "Duration", 0, proto, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		nt := newTarget(vm, "Temporal.Duration")
		var f [10]float64
		for i := range f {
			if v := arg(args, i); !v.IsUndefined() {
				f[i] = toIntegerIfIntegral(vm, v, durationFieldNames[i])
			}
		}
		return r.createDuration(vm, temporal.DurationFromFields(f), nt)
	})
	tmethod(vm, ctor, "from", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return r.newDuration(vm, toTemporalDuration(vm, arg(args, 0)), nil)
	})
	tmethod(vm, ctor, "compare", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		one := toTemporalDuration(vm, arg(args, 0))
		two := toTemporalDuration(vm, arg(args, 1))
		opts := getOptionsObject(vm, arg(args, 2))
		rel := getRelativeToOption(vm, opts)
		if one.Fields() == two.Fields() {
			return engine.Number(0)
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
				vm.ThrowRangeError("durations with years, months or weeks need a relativeTo to be compared")
			}
			days1 = must(temporal.DateDurationDays(d1.Date, rel.plain.date, rel.plain.cal))
			days2 = must(temporal.DateDurationDays(d2.Date, rel.plain.date, rel.plain.cal))
		}
		t1 := must(temporal.Add24HourDaysToTimeDuration(d1.Time, days1))
		t2 := must(temporal.Add24HourDaysToTimeDuration(d2.Time, days2))
		return compareResult(t1.Cmp(t2))
	})

	for i, name := range durationFieldNames {
		tgetter(vm, proto, vm.Intern(name), name, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			f := thisDuration(vm, this, name).d.Fields()
			return engine.Number(f[i])
		})
	}
	tgetter(vm, proto, vm.Intern("sign"), "sign", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Number(float64(temporal.DurationSign(&thisDuration(vm, this, "sign").d)))
	})
	tgetter(vm, proto, vm.Intern("blank"), "blank", func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(temporal.DurationSign(&thisDuration(vm, this, "blank").d) == 0)
	})
	tmethod(vm, proto, "with", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisDuration(vm, this, "with")
		pf, has := toPartialDuration(vm, arg(args, 0))
		f := s.d.Fields()
		for i := range f {
			if has[i] {
				f[i] = pf[i]
			}
		}
		return r.createDuration(vm, temporal.DurationFromFields(f), nil)
	})
	tmethod(vm, proto, "negated", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return r.newDuration(vm, thisDuration(vm, this, "negated").d.Negated(), nil)
	})
	tmethod(vm, proto, "abs", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		f := thisDuration(vm, this, "abs").d.Fields()
		for i := range f {
			f[i] = math.Abs(f[i])
		}
		return r.newDuration(vm, temporal.DurationFromFields(f), nil)
	})
	for _, name := range []string{"add", "subtract"} {
		tmethod(vm, proto, name, 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			s := thisDuration(vm, this, name)
			return r.newDuration(vm, addDurations(vm, s.d, arg(args, 0), name == "subtract"), nil)
		})
	}
	tmethod(vm, proto, "round", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisDuration(vm, this, "round")
		d := s.d
		opts := roundToOptions(vm, arg(args, 0), "smallestUnit")
		existingLargest := temporal.DefaultTemporalLargestUnit(&d)
		largest := getUnitOption(vm, opts, "largestUnit", false)
		rel := getRelativeToOption(vm, opts)
		increment := getRoundingIncrementOption(vm, opts)
		mode := getRoundingModeOption(vm, opts, temporal.RoundHalfExpand)
		smallest := getUnitOption(vm, opts, "smallestUnit", false)
		validateUnit(vm, smallest, unitGroupDateTime)
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
			vm.ThrowRangeError("smallestUnit or largestUnit is required")
		}
		validateUnit(vm, largest, unitGroupDateTime)
		if temporal.LargerOfTwoTemporalUnits(largest, smallest) != largest {
			vm.ThrowRangeError("largestUnit must not be smaller than smallestUnit")
		}
		validateDifferenceIncrement(vm, smallest, increment)
		if increment > 1 && smallest.IsDateUnit() && largest != smallest {
			vm.ThrowRangeError("a roundingIncrement of a date unit needs the same largestUnit")
		}
		if z := rel.zoned; z != nil {
			id := temporal.ToInternalDurationRecord(&d)
			target := must(temporal.AddZonedDateTime(z.ns, z.tz, z.cal, id, temporal.OverflowConstrain))
			id = must(temporal.DifferenceZonedDateTimeWithRounding(z.ns, target, z.tz, z.cal, largest, increment, smallest, mode))
			if largest.IsDateUnit() {
				largest = temporal.UnitHour
			}
			return r.newDuration(vm, must(temporal.TemporalDurationFromInternal(id, largest)), nil)
		}
		if p := rel.plain; p != nil {
			_, from, to := plainRelativeEnd(vm, &d, p)
			id := must(temporal.DifferencePlainDateTimeWithRounding(from, to, p.cal, largest, increment, smallest, mode))
			return r.newDuration(vm, must(temporal.TemporalDurationFromInternal(id, largest)), nil)
		}
		if existingLargest.IsCalendarUnit() || largest.IsCalendarUnit() {
			vm.ThrowRangeError("durations with years, months or weeks need a relativeTo to be rounded")
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
		return r.newDuration(vm, must(temporal.TemporalDurationFromInternal(id, largest)), nil)
	})
	tmethod(vm, proto, "total", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisDuration(vm, this, "total")
		d := s.d
		opts := roundToOptions(vm, arg(args, 0), "unit")
		rel := getRelativeToOption(vm, opts)
		unit := getUnitOption(vm, opts, "unit", true)
		validateUnit(vm, unit, unitGroupDateTime)
		if z := rel.zoned; z != nil {
			id := temporal.ToInternalDurationRecord(&d)
			target := must(temporal.AddZonedDateTime(z.ns, z.tz, z.cal, id, temporal.OverflowConstrain))
			return engine.Number(must(temporal.DifferenceZonedDateTimeWithTotal(z.ns, target, z.tz, z.cal, unit)))
		}
		if p := rel.plain; p != nil {
			_, from, to := plainRelativeEnd(vm, &d, p)
			return engine.Number(must(temporal.DifferencePlainDateTimeWithTotal(from, to, p.cal, unit)))
		}
		largest := temporal.DefaultTemporalLargestUnit(&d)
		if largest.IsCalendarUnit() || unit.IsCalendarUnit() {
			vm.ThrowRangeError("durations with years, months or weeks need a relativeTo to be totaled")
		}
		id := temporal.ToInternalDurationRecordWith24HourDays(&d)
		return engine.Number(temporal.TotalTimeDuration(id.Time, unit))
	})
	tmethod(vm, proto, "toString", 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		s := thisDuration(vm, this, "toString")
		d := s.d
		opts := getOptionsObject(vm, arg(args, 0))
		digits := getFractionalSecondDigitsOption(vm, opts)
		mode := getRoundingModeOption(vm, opts, temporal.RoundTrunc)
		unit := getUnitOption(vm, opts, "smallestUnit", false)
		validateUnit(vm, unit, unitGroupTime)
		if unit == temporal.UnitHour || unit == temporal.UnitMinute {
			vm.ThrowRangeError("smallestUnit must not be hour or minute")
		}
		p := temporal.ToSecondsStringPrecisionRecord(unit, digits)
		if p.Unit == temporal.UnitNanosecond && p.Increment == 1 {
			return vm.NewString(temporal.TemporalDurationToString(&d, p.Precision))
		}
		largest := temporal.DefaultTemporalLargestUnit(&d)
		id := temporal.ToInternalDurationRecord(&d)
		t := must(temporal.RoundTimeDuration(id.Time, p.Increment, p.Unit, mode))
		id = temporal.CombineDateAndTimeDuration(id.Date, t)
		rd := must(temporal.TemporalDurationFromInternal(id, temporal.LargerOfTwoTemporalUnits(largest, temporal.UnitSecond)))
		return vm.NewString(temporal.TemporalDurationToString(&rd, p.Precision))
	})
	for _, name := range []string{"toJSON", "toLocaleString"} {
		tmethod(vm, proto, name, 0, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
			return vm.NewString(temporal.TemporalDurationToString(&thisDuration(vm, this, name).d, temporal.PrecisionAuto))
		})
	}
	valueOfThrows(vm, proto, "Duration")
}
