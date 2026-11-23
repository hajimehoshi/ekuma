// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package temporal

// AddTime implements AddTime. It returns the time and the number of days beyond it.
func AddTime(t Time, d Int128) (Time, int64) {
	return balanceTimeNs(I128(t.NanosecondOfDay()).Add(d))
}

// DifferenceTime implements DifferenceTime.
func DifferenceTime(t1, t2 Time) Int128 {
	return I128(t2.NanosecondOfDay() - t1.NanosecondOfDay())
}

// RoundTime implements RoundTime. It returns the time and the number of days beyond it.
func RoundTime(t Time, increment int64, unit Unit, mode RoundingMode) (Time, int64) {
	var quantity int64
	switch unit {
	case UnitDay, UnitHour:
		quantity = t.NanosecondOfDay()
	case UnitMinute:
		quantity = t.NanosecondOfDay() - int64(t.Hour)*NsPerHour
	case UnitSecond:
		quantity = int64(t.Second)*NsPerSecond + int64(t.SubSecondNanoseconds())
	case UnitMillisecond:
		quantity = int64(t.SubSecondNanoseconds())
	case UnitMicrosecond:
		quantity = int64(t.Microsecond)*NsPerMicrosecond + int64(t.Nanosecond)
	case UnitNanosecond:
		quantity = int64(t.Nanosecond)
	}
	nsPerUnit := unit.Nanoseconds()
	result := RoundNumberToIncrement(quantity, nsPerUnit*increment, mode) / nsPerUnit
	h, m, s, ms, us := int64(t.Hour), int64(t.Minute), int64(t.Second), int64(t.Millisecond), int64(t.Microsecond)
	switch unit {
	case UnitDay:
		return Time{}, result
	case UnitHour:
		return BalanceTime(result, 0, 0, 0, 0, 0)
	case UnitMinute:
		return BalanceTime(h, result, 0, 0, 0, 0)
	case UnitSecond:
		return BalanceTime(h, m, result, 0, 0, 0)
	case UnitMillisecond:
		return BalanceTime(h, m, s, result, 0, 0)
	case UnitMicrosecond:
		return BalanceTime(h, m, s, ms, result, 0)
	}
	return BalanceTime(h, m, s, ms, us, result)
}

// RoundISODateTime implements RoundISODateTime.
func RoundISODateTime(dt ISODateTime, increment int64, unit Unit, mode RoundingMode) ISODateTime {
	t, days := RoundTime(dt.Time, increment, unit, mode)
	return ISODateTime{
		Date: AddDaysToISODate(dt.Date, days),
		Time: t,
	}
}

// RoundTemporalInstant implements RoundTemporalInstant.
func RoundTemporalInstant(ns Int128, increment int64, unit Unit, mode RoundingMode) Int128 {
	return RoundNumberToIncrementAsIfPositive(ns, unit.Nanoseconds()*increment, mode)
}

// AddInstant implements AddInstant.
func AddInstant(ns Int128, d Int128) (Int128, error) {
	r := ns.Add(d)
	if !IsValidEpochNanoseconds(r) {
		return Int128{}, rangeErrorf("instant is out of range")
	}
	return r, nil
}

// AddZonedDateTime implements AddZonedDateTime.
func AddZonedDateTime(ns Int128, tz TimeZone, cal string, d InternalDuration, overflow Overflow) (Int128, error) {
	if DateDurationSign(d.Date) == 0 {
		return AddInstant(ns, d.Time)
	}
	dt := tz.ISODateTimeFor(ns)
	added, err := CalendarDateAdd(cal, dt.Date, d.Date, overflow)
	if err != nil {
		return Int128{}, err
	}
	intermediate, err := tz.EpochNanosecondsFor(ISODateTime{
		Date: added,
		Time: dt.Time,
	}, DisambiguationCompatible)
	if err != nil {
		return Int128{}, err
	}
	return AddInstant(intermediate, d.Time)
}

// DifferenceISODateTime implements DifferenceISODateTime.
func DifferenceISODateTime(dt1, dt2 ISODateTime, cal string, largestUnit Unit) (InternalDuration, error) {
	t := DifferenceTime(dt1.Time, dt2.Time)
	timeSign := t.Sign()
	dateSign := CompareISODate(dt1.Date, dt2.Date)
	adjusted := dt2.Date
	if timeSign == dateSign {
		adjusted = AddDaysToISODate(adjusted, int64(timeSign))
		t = t.Add(Mul64(int64(-timeSign), NsPerDay))
	}
	dateLargest := LargerOfTwoTemporalUnits(UnitDay, largestUnit)
	dd := CalendarDateUntil(cal, dt1.Date, adjusted, dateLargest)
	if largestUnit != dateLargest {
		var err error
		if t, err = Add24HourDaysToTimeDuration(t, dd.Days); err != nil {
			return InternalDuration{}, err
		}
		dd.Days = 0
	}
	return CombineDateAndTimeDuration(dd, t), nil
}

// DifferenceZonedDateTime implements DifferenceZonedDateTime.
func DifferenceZonedDateTime(ns1, ns2 Int128, tz TimeZone, cal string, largestUnit Unit) (InternalDuration, error) {
	diff := ns2.Sub(ns1)
	if diff.IsZero() {
		return InternalDuration{}, nil
	}
	sign := -1
	if diff.Sign() < 0 {
		sign = 1
	}
	start := tz.ISODateTimeFor(ns1)
	end := tz.ISODateTimeFor(ns2)
	if CompareISODate(start.Date, end.Date) == 0 {
		return CombineDateAndTimeDuration(DateDuration{}, diff), nil
	}
	maxCorrection := 1
	if sign == -1 {
		maxCorrection = 2
	}
	correction := 0
	t := DifferenceTime(start.Time, end.Time)
	if t.Sign() == sign {
		correction++
	}
	var intermediate ISODateTime
	found := false
	for ; correction <= maxCorrection; correction++ {
		intermediate = ISODateTime{
			Date: AddDaysToISODate(end.Date, int64(correction*sign)),
			Time: start.Time,
		}
		ins, err := tz.EpochNanosecondsFor(intermediate, DisambiguationCompatible)
		if err != nil {
			return InternalDuration{}, err
		}
		t = ns2.Sub(ins)
		if t.Sign() != sign {
			found = true
			break
		}
	}
	if !found {
		return InternalDuration{}, rangeErrorf("the time zone has inconsistent offsets")
	}
	dateLargest := LargerOfTwoTemporalUnits(UnitDay, largestUnit)
	dd := CalendarDateUntil(cal, start.Date, intermediate.Date, dateLargest)
	return CombineDateAndTimeDuration(dd, t), nil
}

// relativeEpochNs returns the epoch nanoseconds of dt in tz, or as UTC when tz is nil.
func relativeEpochNs(dt ISODateTime, tz *TimeZone) (Int128, error) {
	if tz == nil {
		return dt.UTCEpochNanoseconds(), nil
	}
	return tz.EpochNanosecondsFor(dt, DisambiguationCompatible)
}

// nudgeWindow is the result of ComputeNudgeWindow.
type nudgeWindow struct {
	r1, r2                   int64
	startEpochNs, endEpochNs Int128
	startDuration            DateDuration
	endDuration              DateDuration
}

// computeNudgeWindow implements ComputeNudgeWindow.
func computeNudgeWindow(sign int, d InternalDuration, originEpochNs Int128, dt ISODateTime, cal string, tz *TimeZone, increment int64, unit Unit, additionalShift bool) (nudgeWindow, error) {
	var w nudgeWindow
	s := int64(sign)
	var err error
	switch unit {
	case UnitYear:
		years := RoundNumberToIncrement(d.Date.Years, increment, RoundTrunc)
		w.r1 = years
		if additionalShift {
			w.r1 = years + increment*s
		}
		w.r2 = w.r1 + increment*s
		w.startDuration = DateDuration{
			Years: w.r1,
		}
		w.endDuration = DateDuration{
			Years: w.r2,
		}
	case UnitMonth:
		months := RoundNumberToIncrement(d.Date.Months, increment, RoundTrunc)
		w.r1 = months
		if additionalShift {
			w.r1 = months + increment*s
		}
		w.r2 = w.r1 + increment*s
		if w.startDuration, err = AdjustDateDurationRecord(d.Date, 0, 0, w.r1); err != nil {
			return w, err
		}
		if w.endDuration, err = AdjustDateDurationRecord(d.Date, 0, 0, w.r2); err != nil {
			return w, err
		}
	case UnitWeek:
		yearsMonths, err := AdjustDateDurationRecord(d.Date, 0, 0, keep)
		if err != nil {
			return w, err
		}
		weeksStart, err := CalendarDateAdd(cal, dt.Date, yearsMonths, OverflowConstrain)
		if err != nil {
			return w, err
		}
		weeksEnd := AddDaysToISODate(weeksStart, d.Date.Days)
		until := CalendarDateUntil(cal, weeksStart, weeksEnd, UnitWeek)
		weeks := RoundNumberToIncrement(d.Date.Weeks+until.Weeks, increment, RoundTrunc)
		w.r1 = weeks
		w.r2 = weeks + increment*s
		if w.startDuration, err = AdjustDateDurationRecord(d.Date, 0, w.r1, keep); err != nil {
			return w, err
		}
		if w.endDuration, err = AdjustDateDurationRecord(d.Date, 0, w.r2, keep); err != nil {
			return w, err
		}
	case UnitDay:
		days := RoundNumberToIncrement(d.Date.Days, increment, RoundTrunc)
		w.r1 = days
		w.r2 = days + increment*s
		if w.startDuration, err = AdjustDateDurationRecord(d.Date, w.r1, keep, keep); err != nil {
			return w, err
		}
		if w.endDuration, err = AdjustDateDurationRecord(d.Date, w.r2, keep, keep); err != nil {
			return w, err
		}
	}
	if DateDurationSign(w.startDuration) == 0 {
		w.startEpochNs = originEpochNs
	} else {
		start, err := CalendarDateAdd(cal, dt.Date, w.startDuration, OverflowConstrain)
		if err != nil {
			return w, err
		}
		if w.startEpochNs, err = relativeEpochNs(ISODateTime{
			Date: start,
			Time: dt.Time,
		}, tz); err != nil {
			return w, err
		}
	}
	end, err := CalendarDateAdd(cal, dt.Date, w.endDuration, OverflowConstrain)
	if err != nil {
		return w, err
	}
	if w.endEpochNs, err = relativeEpochNs(ISODateTime{
		Date: end,
		Time: dt.Time,
	}, tz); err != nil {
		return w, err
	}
	return w, nil
}

// nudgeResult is a Duration Nudge Result Record.
type nudgeResult struct {
	duration              InternalDuration
	nudgedEpochNs         Int128
	didExpandCalendarUnit bool
}

func between(a, x, b Int128) bool {
	return a.Cmp(x) <= 0 && x.Cmp(b) <= 0
}

// nudgeToCalendarUnit implements NudgeToCalendarUnit. It also returns the total.
func nudgeToCalendarUnit(sign int, d InternalDuration, originEpochNs, destEpochNs Int128, dt ISODateTime, cal string, tz *TimeZone, increment int64, unit Unit, mode RoundingMode) (nudgeResult, float64, error) {
	didExpand := false
	w, err := computeNudgeWindow(sign, d, originEpochNs, dt, cal, tz, increment, unit, false)
	if err != nil {
		return nudgeResult{}, 0, err
	}
	inside := between(w.startEpochNs, destEpochNs, w.endEpochNs)
	if sign == -1 {
		inside = between(w.endEpochNs, destEpochNs, w.startEpochNs)
	}
	if !inside {
		if w, err = computeNudgeWindow(sign, d, originEpochNs, dt, cal, tz, increment, unit, true); err != nil {
			return nudgeResult{}, 0, err
		}
		didExpand = true
	}
	if w.endEpochNs.Cmp(w.startEpochNs) == 0 {
		return nudgeResult{}, 0, rangeErrorf("the calendar unit has no length")
	}
	numerator := destEpochNs.Sub(w.startEpochNs)
	denominator := w.endEpochNs.Sub(w.startEpochNs)
	cmp := numerator.Add(numerator).Abs().Cmp(denominator.Abs())
	ar1, ar2 := abs64(w.r1), abs64(w.r2)
	even := (ar1/increment)%2 == 0
	var rounded int64
	switch {
	case numerator.IsZero():
		rounded = ar1
	case numerator.Cmp(denominator) == 0:
		rounded = ar2
	case mode.unsigned(sign < 0).apply(cmp, even):
		rounded = ar2
	default:
		rounded = ar1
	}
	var total float64
	if increment == 1 {
		fake := denominator.MulInt64(w.r1).Add(numerator.MulInt64(int64(sign)))
		total = DivToFloat64(fake, denominator)
	}
	didExpand = didExpand || rounded == ar2
	r := nudgeResult{
		didExpandCalendarUnit: didExpand,
	}
	if rounded == ar2 {
		r.duration = InternalDuration{
			Date: w.endDuration,
		}
		r.nudgedEpochNs = w.endEpochNs
	} else {
		r.duration = InternalDuration{
			Date: w.startDuration,
		}
		r.nudgedEpochNs = w.startEpochNs
	}
	return r, total, nil
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// nudgeToZonedTime implements NudgeToZonedTime.
func nudgeToZonedTime(sign int, d InternalDuration, dt ISODateTime, cal string, tz TimeZone, increment int64, unit Unit, mode RoundingMode) (nudgeResult, error) {
	start, err := CalendarDateAdd(cal, dt.Date, d.Date, OverflowConstrain)
	if err != nil {
		return nudgeResult{}, err
	}
	startDT := ISODateTime{
		Date: start,
		Time: dt.Time,
	}
	endDT := ISODateTime{
		Date: AddDaysToISODate(start, int64(sign)),
		Time: dt.Time,
	}
	startNs, err := tz.EpochNanosecondsFor(startDT, DisambiguationCompatible)
	if err != nil {
		return nudgeResult{}, err
	}
	endNs, err := tz.EpochNanosecondsFor(endDT, DisambiguationCompatible)
	if err != nil {
		return nudgeResult{}, err
	}
	daySpan := endNs.Sub(startNs)
	if daySpan.Sign() != sign {
		return nudgeResult{}, rangeErrorf("the time zone has inconsistent offsets")
	}
	unitIncrement := Mul64(unit.Nanoseconds(), increment)
	rounded := RoundInt128ToIncrement(d.Time, unitIncrement, mode)
	if err := checkTimeDuration(rounded); err != nil {
		return nudgeResult{}, err
	}
	beyond := rounded.Sub(daySpan)
	didRoundBeyondDay := beyond.Sign() != -sign
	var dayDelta int64
	var nudged Int128
	if didRoundBeyondDay {
		dayDelta = int64(sign)
		rounded = RoundInt128ToIncrement(beyond, unitIncrement, mode)
		if err := checkTimeDuration(rounded); err != nil {
			return nudgeResult{}, err
		}
		nudged = endNs.Add(rounded)
	} else {
		nudged = startNs.Add(rounded)
	}
	dd, err := AdjustDateDurationRecord(d.Date, d.Date.Days+dayDelta, keep, keep)
	if err != nil {
		return nudgeResult{}, err
	}
	return nudgeResult{
		duration:              CombineDateAndTimeDuration(dd, rounded),
		nudgedEpochNs:         nudged,
		didExpandCalendarUnit: didRoundBeyondDay,
	}, nil
}

// nudgeToDayOrTime implements NudgeToDayOrTime.
func nudgeToDayOrTime(d InternalDuration, destEpochNs Int128, largestUnit Unit, increment int64, smallestUnit Unit, mode RoundingMode) (nudgeResult, error) {
	t, err := Add24HourDaysToTimeDuration(d.Time, d.Date.Days)
	if err != nil {
		return nudgeResult{}, err
	}
	rounded := RoundInt128ToIncrement(t, Mul64(increment, smallestUnit.Nanoseconds()), mode)
	if err := checkTimeDuration(rounded); err != nil {
		return nudgeResult{}, err
	}
	diff := rounded.Sub(t)
	wholeDays, _ := t.QuoRemInt64(NsPerDay)
	roundedWholeDays, _ := rounded.QuoRemInt64(NsPerDay)
	didExpandDays := roundedWholeDays.Abs().Cmp(wholeDays.Abs()) > 0
	nudged := destEpochNs.Add(diff)
	var days int64
	remainder := rounded
	if largestUnit.IsDateUnit() {
		days, _ = roundedWholeDays.Int64()
		remainder = rounded.Sub(Mul64(days, NsPerDay))
	}
	dd, err := AdjustDateDurationRecord(d.Date, days, keep, keep)
	if err != nil {
		return nudgeResult{}, err
	}
	return nudgeResult{
		duration:              CombineDateAndTimeDuration(dd, remainder),
		nudgedEpochNs:         nudged,
		didExpandCalendarUnit: didExpandDays,
	}, nil
}

// bubbleRelativeDuration implements BubbleRelativeDuration.
func bubbleRelativeDuration(sign int, d InternalDuration, nudgedEpochNs Int128, dt ISODateTime, cal string, tz *TimeZone, largestUnit, smallestUnit Unit) (InternalDuration, error) {
	if smallestUnit == largestUnit {
		return d, nil
	}
	s := int64(sign)
	for unit := smallestUnit - 1; unit >= largestUnit; unit-- {
		if unit == UnitWeek && largestUnit != UnitWeek {
			continue
		}
		var end DateDuration
		var err error
		switch unit {
		case UnitYear:
			end = DateDuration{
				Years: d.Date.Years + s,
			}
		case UnitMonth:
			if end, err = AdjustDateDurationRecord(d.Date, 0, 0, d.Date.Months+s); err != nil {
				return InternalDuration{}, err
			}
		case UnitWeek:
			if end, err = AdjustDateDurationRecord(d.Date, 0, d.Date.Weeks+s, keep); err != nil {
				return InternalDuration{}, err
			}
		default:
			continue
		}
		endDate, err := CalendarDateAdd(cal, dt.Date, end, OverflowConstrain)
		if err != nil {
			return InternalDuration{}, err
		}
		endNs, err := relativeEpochNs(ISODateTime{
			Date: endDate,
			Time: dt.Time,
		}, tz)
		if err != nil {
			return InternalDuration{}, err
		}
		if nudgedEpochNs.Cmp(endNs) != -sign {
			d = InternalDuration{
				Date: end,
			}
		} else {
			break
		}
	}
	return d, nil
}

// RoundRelativeDuration implements RoundRelativeDuration. A nil tz stands for unset.
func RoundRelativeDuration(d InternalDuration, originEpochNs, destEpochNs Int128, dt ISODateTime, cal string, tz *TimeZone, largestUnit Unit, increment int64, smallestUnit Unit, mode RoundingMode) (InternalDuration, error) {
	irregular := smallestUnit.IsCalendarUnit() || (tz != nil && smallestUnit == UnitDay)
	sign := 1
	if InternalDurationSign(d) < 0 {
		sign = -1
	}
	var r nudgeResult
	var err error
	switch {
	case irregular:
		r, _, err = nudgeToCalendarUnit(sign, d, originEpochNs, destEpochNs, dt, cal, tz, increment, smallestUnit, mode)
	case tz != nil:
		r, err = nudgeToZonedTime(sign, d, dt, cal, *tz, increment, smallestUnit, mode)
	default:
		r, err = nudgeToDayOrTime(d, destEpochNs, largestUnit, increment, smallestUnit, mode)
	}
	if err != nil {
		return InternalDuration{}, err
	}
	d = r.duration
	if r.didExpandCalendarUnit && smallestUnit != UnitWeek {
		return bubbleRelativeDuration(sign, d, r.nudgedEpochNs, dt, cal, tz, largestUnit, LargerOfTwoTemporalUnits(smallestUnit, UnitDay))
	}
	return d, nil
}

// TotalRelativeDuration implements TotalRelativeDuration.
func TotalRelativeDuration(d InternalDuration, originEpochNs, destEpochNs Int128, dt ISODateTime, cal string, tz *TimeZone, unit Unit) (float64, error) {
	if unit.IsCalendarUnit() || (tz != nil && unit == UnitDay) {
		sign := 1
		if InternalDurationSign(d) < 0 {
			sign = -1
		}
		_, total, err := nudgeToCalendarUnit(sign, d, originEpochNs, destEpochNs, dt, cal, tz, 1, unit, RoundTrunc)
		return total, err
	}
	t, err := Add24HourDaysToTimeDuration(d.Time, d.Date.Days)
	if err != nil {
		return 0, err
	}
	return TotalTimeDuration(t, unit), nil
}

// DifferencePlainDateTimeWithRounding implements DifferencePlainDateTimeWithRounding.
func DifferencePlainDateTimeWithRounding(dt1, dt2 ISODateTime, cal string, largestUnit Unit, increment int64, smallestUnit Unit, mode RoundingMode) (InternalDuration, error) {
	if CompareISODateTime(dt1, dt2) == 0 {
		return InternalDuration{}, nil
	}
	if !ISODateTimeWithinLimits(dt1) || !ISODateTimeWithinLimits(dt2) {
		return InternalDuration{}, rangeErrorf("date-time is out of range")
	}
	d, err := DifferenceISODateTime(dt1, dt2, cal, largestUnit)
	if err != nil {
		return InternalDuration{}, err
	}
	if smallestUnit == UnitNanosecond && increment == 1 {
		return d, nil
	}
	return RoundRelativeDuration(d, dt1.UTCEpochNanoseconds(), dt2.UTCEpochNanoseconds(), dt1, cal, nil, largestUnit, increment, smallestUnit, mode)
}

// DifferencePlainDateTimeWithTotal implements DifferencePlainDateTimeWithTotal.
func DifferencePlainDateTimeWithTotal(dt1, dt2 ISODateTime, cal string, unit Unit) (float64, error) {
	if CompareISODateTime(dt1, dt2) == 0 {
		return 0, nil
	}
	if !ISODateTimeWithinLimits(dt1) || !ISODateTimeWithinLimits(dt2) {
		return 0, rangeErrorf("date-time is out of range")
	}
	d, err := DifferenceISODateTime(dt1, dt2, cal, unit)
	if err != nil {
		return 0, err
	}
	if unit == UnitNanosecond {
		return d.Time.Float64(), nil
	}
	return TotalRelativeDuration(d, dt1.UTCEpochNanoseconds(), dt2.UTCEpochNanoseconds(), dt1, cal, nil, unit)
}

// DifferenceEpochNanoseconds implements DifferenceInstant.
func DifferenceEpochNanoseconds(ns1, ns2 Int128, increment int64, smallestUnit Unit, mode RoundingMode) (InternalDuration, error) {
	t, err := RoundTimeDuration(ns2.Sub(ns1), increment, smallestUnit, mode)
	if err != nil {
		return InternalDuration{}, err
	}
	return CombineDateAndTimeDuration(DateDuration{}, t), nil
}

// DifferenceZonedDateTimeWithRounding implements DifferenceZonedDateTimeWithRounding.
func DifferenceZonedDateTimeWithRounding(ns1, ns2 Int128, tz TimeZone, cal string, largestUnit Unit, increment int64, smallestUnit Unit, mode RoundingMode) (InternalDuration, error) {
	if largestUnit.IsTimeUnit() {
		return DifferenceEpochNanoseconds(ns1, ns2, increment, smallestUnit, mode)
	}
	d, err := DifferenceZonedDateTime(ns1, ns2, tz, cal, largestUnit)
	if err != nil {
		return InternalDuration{}, err
	}
	if smallestUnit == UnitNanosecond && increment == 1 {
		return d, nil
	}
	dt := tz.ISODateTimeFor(ns1)
	return RoundRelativeDuration(d, ns1, ns2, dt, cal, &tz, largestUnit, increment, smallestUnit, mode)
}

// DifferenceZonedDateTimeWithTotal implements DifferenceZonedDateTimeWithTotal.
func DifferenceZonedDateTimeWithTotal(ns1, ns2 Int128, tz TimeZone, cal string, unit Unit) (float64, error) {
	if unit.IsTimeUnit() {
		return TotalTimeDuration(ns2.Sub(ns1), unit), nil
	}
	d, err := DifferenceZonedDateTime(ns1, ns2, tz, cal, unit)
	if err != nil {
		return 0, err
	}
	dt := tz.ISODateTimeFor(ns1)
	return TotalRelativeDuration(d, ns1, ns2, dt, cal, &tz, unit)
}

// MatchBehaviour is how InterpretISODateTimeOffset matches an offset.
type MatchBehaviour int

const (
	MatchExactly MatchBehaviour = iota
	MatchMinutes
)

// InterpretISODateTimeOffset implements InterpretISODateTimeOffset. A startOfDay time is the
// start-of-day of the specification.
func InterpretISODateTimeOffset(d ISODate, t Time, startOfDay bool, behaviour OffsetBehaviour, offsetNs int64, tz TimeZone, disambiguation Disambiguation, option OffsetOption, match MatchBehaviour) (Int128, error) {
	if startOfDay {
		return tz.StartOfDay(d)
	}
	dt := ISODateTime{
		Date: d,
		Time: t,
	}
	if behaviour == OffsetBehaviourWall || option == OffsetIgnore {
		return tz.EpochNanosecondsFor(dt, disambiguation)
	}
	if behaviour == OffsetBehaviourExact || option == OffsetUse {
		balanced := BalanceISODateTime(d, int64(t.Hour), int64(t.Minute), int64(t.Second), int64(t.Millisecond), int64(t.Microsecond), int64(t.Nanosecond)-offsetNs)
		if err := CheckISODaysRange(balanced.Date); err != nil {
			return Int128{}, err
		}
		ns := balanced.UTCEpochNanoseconds()
		if !IsValidEpochNanoseconds(ns) {
			return Int128{}, rangeErrorf("date-time is out of range")
		}
		return ns, nil
	}
	if err := CheckISODaysRange(d); err != nil {
		return Int128{}, err
	}
	utc := dt.UTCEpochNanoseconds()
	possible, err := tz.PossibleEpochNanoseconds(dt)
	if err != nil {
		return Int128{}, err
	}
	for _, cand := range possible {
		off, _ := utc.Sub(cand).Int64()
		if off == offsetNs {
			return cand, nil
		}
		if match == MatchMinutes && RoundNumberToIncrement(off, NsPerMinute, RoundHalfExpand) == offsetNs {
			return cand, nil
		}
	}
	if option == OffsetReject {
		return Int128{}, rangeErrorf("the offset %s is not valid for the time zone %s", FormatUTCOffsetNanoseconds(offsetNs), tz.ID())
	}
	return tz.DisambiguatePossibleEpochNanoseconds(possible, dt, disambiguation)
}

// DateDurationDays implements DateDurationDays.
func DateDurationDays(dd DateDuration, relativeTo ISODate, cal string) (int64, error) {
	ymw, err := AdjustDateDurationRecord(dd, 0, keep, keep)
	if err != nil {
		return 0, err
	}
	if DateDurationSign(ymw) == 0 {
		return dd.Days, nil
	}
	later, err := CalendarDateAdd(cal, relativeTo, ymw, OverflowConstrain)
	if err != nil {
		return 0, err
	}
	return dd.Days + later.EpochDays() - relativeTo.EpochDays(), nil
}
