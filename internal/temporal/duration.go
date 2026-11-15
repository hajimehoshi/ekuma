// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package temporal

import (
	"math"
)

// Duration holds the fields of a Temporal.Duration, which are integral Numbers.
type Duration struct {
	Years        float64
	Months       float64
	Weeks        float64
	Days         float64
	Hours        float64
	Minutes      float64
	Seconds      float64
	Milliseconds float64
	Microseconds float64
	Nanoseconds  float64
}

// Fields returns the fields of d from years to nanoseconds.
func (d *Duration) Fields() [10]float64 {
	return [10]float64{d.Years, d.Months, d.Weeks, d.Days, d.Hours, d.Minutes, d.Seconds, d.Milliseconds, d.Microseconds, d.Nanoseconds}
}

// DurationFromFields returns the duration of the fields from years to nanoseconds.
func DurationFromFields(f [10]float64) Duration {
	return Duration{
		Years:        f[0],
		Months:       f[1],
		Weeks:        f[2],
		Days:         f[3],
		Hours:        f[4],
		Minutes:      f[5],
		Seconds:      f[6],
		Milliseconds: f[7],
		Microseconds: f[8],
		Nanoseconds:  f[9],
	}
}

// DateDuration is a Date Duration Record.
type DateDuration struct {
	Years  int64
	Months int64
	Weeks  int64
	Days   int64
}

// InternalDuration is an Internal Duration Record. Time is a time duration in nanoseconds.
type InternalDuration struct {
	Date DateDuration
	Time Int128
}

// maxTimeDuration is the largest magnitude of a time duration, 2⁵³ seconds less a nanosecond.
var maxTimeDuration = Mul64(1<<53, NsPerSecond).Sub(I128(1))

// secondsPerUnit are the lengths of the days and time units of the fields of a duration, in
// seconds.
var secondsPerUnit = [10]float64{0, 0, 0, 86400, 3600, 60, 1, 1e-3, 1e-6, 1e-9}

// nsPerField are the lengths of the days and time units of the fields of a duration, in
// nanoseconds.
var nsPerField = [10]int64{0, 0, 0, NsPerDay, NsPerHour, NsPerMinute, NsPerSecond, NsPerMillisecond, NsPerMicrosecond, 1}

// int128FromFloat returns the integral Number f, |f| < 2¹²⁶, as an Int128.
func int128FromFloat(f float64) Int128 {
	if f > -1<<62 && f < 1<<62 {
		return I128(int64(f))
	}
	frac, exp := math.Frexp(math.Abs(f))
	mant := int64(math.Ldexp(frac, 53))
	x := I128(mant).lsh(uint(exp - 53))
	if f < 0 {
		return x.Neg()
	}
	return x
}

// lsh returns x << n for a non-negative x.
func (x Int128) lsh(n uint) Int128 {
	if n >= 64 {
		return Int128{
			hi: int64(x.lo << (n - 64)),
		}
	}
	if n == 0 {
		return x
	}
	return Int128{
		hi: x.hi<<n | int64(x.lo>>(64-n)),
		lo: x.lo << n,
	}
}

// IsValidDuration implements IsValidDuration.
func IsValidDuration(d *Duration) bool {
	f := d.Fields()
	s := 0
	for _, v := range f {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
		if v < 0 {
			if s > 0 {
				return false
			}
			s = -1
		} else if v > 0 {
			if s < 0 {
				return false
			}
			s = 1
		}
	}
	if math.Abs(d.Years) >= 1<<32 || math.Abs(d.Months) >= 1<<32 || math.Abs(d.Weeks) >= 1<<32 {
		return false
	}
	// The fields have the same sign, so one of them reaching the limit alone is enough.
	for i := 3; i < 10; i++ {
		if math.Abs(f[i])*secondsPerUnit[i] >= 1<<54 {
			return false
		}
	}
	return timeNanoseconds(f).Abs().Cmp(maxTimeDuration) <= 0
}

// timeNanoseconds returns the days and time fields of f in nanoseconds. Each field must be
// below 2⁵⁴ seconds.
func timeNanoseconds(f [10]float64) Int128 {
	var total Int128
	for i := 3; i < 10; i++ {
		if f[i] == 0 {
			continue
		}
		// A field below 2⁵⁴ seconds that is not a safe integer is a number of milliseconds or
		// smaller units, whose nanoseconds still fit in an Int128.
		total = total.Add(int128FromFloat(f[i]).MulInt64(nsPerField[i]))
	}
	return total
}

// DurationSign implements DurationSign.
func DurationSign(d *Duration) int {
	for _, v := range d.Fields() {
		if v < 0 {
			return -1
		}
		if v > 0 {
			return 1
		}
	}
	return 0
}

// DateDurationSign implements DateDurationSign.
func DateDurationSign(d DateDuration) int {
	for _, v := range [4]int64{d.Years, d.Months, d.Weeks, d.Days} {
		if v < 0 {
			return -1
		}
		if v > 0 {
			return 1
		}
	}
	return 0
}

// InternalDurationSign implements InternalDurationSign.
func InternalDurationSign(d InternalDuration) int {
	if s := DateDurationSign(d.Date); s != 0 {
		return s
	}
	return d.Time.Sign()
}

// DefaultTemporalLargestUnit implements DefaultTemporalLargestUnit.
func DefaultTemporalLargestUnit(d *Duration) Unit {
	for i, v := range d.Fields() {
		if v != 0 {
			return Unit(i)
		}
	}
	return UnitNanosecond
}

// TimeDurationFromComponents implements TimeDurationFromComponents for the fields of a valid
// duration.
func TimeDurationFromComponents(hours, minutes, seconds, milliseconds, microseconds, nanoseconds float64) Int128 {
	return timeNanoseconds([10]float64{
		4: hours,
		5: minutes,
		6: seconds,
		7: milliseconds,
		8: microseconds,
		9: nanoseconds,
	})
}

// checkTimeDuration returns a RangeError when the time duration d is beyond its limits.
func checkTimeDuration(d Int128) error {
	if d.Abs().Cmp(maxTimeDuration) > 0 {
		return rangeErrorf("time duration is out of range")
	}
	return nil
}

// Add24HourDaysToTimeDuration implements Add24HourDaysToTimeDuration.
func Add24HourDaysToTimeDuration(d Int128, days int64) (Int128, error) {
	r := d.Add(Mul64(days, NsPerDay))
	if err := checkTimeDuration(r); err != nil {
		return Int128{}, err
	}
	return r, nil
}

// AddTimeDuration implements AddTimeDuration.
func AddTimeDuration(a, b Int128) (Int128, error) {
	r := a.Add(b)
	if err := checkTimeDuration(r); err != nil {
		return Int128{}, err
	}
	return r, nil
}

// ToInternalDurationRecord implements ToInternalDurationRecord for a valid duration.
func ToInternalDurationRecord(d *Duration) InternalDuration {
	return InternalDuration{
		Date: DateDuration{
			Years:  int64(d.Years),
			Months: int64(d.Months),
			Weeks:  int64(d.Weeks),
			Days:   int64(d.Days),
		},
		Time: TimeDurationFromComponents(d.Hours, d.Minutes, d.Seconds, d.Milliseconds, d.Microseconds, d.Nanoseconds),
	}
}

// ToInternalDurationRecordWith24HourDays implements ToInternalDurationRecordWith24HourDays for
// a valid duration.
func ToInternalDurationRecordWith24HourDays(d *Duration) InternalDuration {
	t := TimeDurationFromComponents(d.Hours, d.Minutes, d.Seconds, d.Milliseconds, d.Microseconds, d.Nanoseconds)
	// A valid duration's days and time fields total less than 2⁵³ seconds.
	t = t.Add(Mul64(int64(d.Days), NsPerDay))
	return InternalDuration{
		Date: DateDuration{
			Years:  int64(d.Years),
			Months: int64(d.Months),
			Weeks:  int64(d.Weeks),
		},
		Time: t,
	}
}

// ToDateDurationRecordWithoutTime implements ToDateDurationRecordWithoutTime.
func ToDateDurationRecordWithoutTime(d *Duration) (DateDuration, error) {
	id := ToInternalDurationRecordWith24HourDays(d)
	days, _ := id.Time.QuoRemInt64(NsPerDay)
	dd, _ := days.Int64()
	return CreateDateDurationRecord(id.Date.Years, id.Date.Months, id.Date.Weeks, dd)
}

// CreateDateDurationRecord implements CreateDateDurationRecord.
func CreateDateDurationRecord(years, months, weeks, days int64) (DateDuration, error) {
	d := Duration{
		Years:  float64(years),
		Months: float64(months),
		Weeks:  float64(weeks),
		Days:   float64(days),
	}
	if !IsValidDuration(&d) {
		return DateDuration{}, rangeErrorf("duration is out of range")
	}
	return DateDuration{
		Years:  years,
		Months: months,
		Weeks:  weeks,
		Days:   days,
	}, nil
}

// AdjustDateDurationRecord implements AdjustDateDurationRecord. A negative weeks or months
// of -1 << 63 keeps the field of d.
func AdjustDateDurationRecord(d DateDuration, days, weeks, months int64) (DateDuration, error) {
	if weeks == keep {
		weeks = d.Weeks
	}
	if months == keep {
		months = d.Months
	}
	return CreateDateDurationRecord(d.Years, months, weeks, days)
}

// keep is the argument of AdjustDateDurationRecord that keeps a field.
const keep = -1 << 63

// CombineDateAndTimeDuration implements CombineDateAndTimeDuration.
func CombineDateAndTimeDuration(d DateDuration, t Int128) InternalDuration {
	return InternalDuration{
		Date: d,
		Time: t,
	}
}

// TemporalDurationFromInternal implements TemporalDurationFromInternal.
func TemporalDurationFromInternal(id InternalDuration, largestUnit Unit) (Duration, error) {
	s := id.Time.Sign()
	abs := id.Time.Abs()
	secsI, nsRem := abs.QuoRemInt64(NsPerSecond)
	// secs < 2⁵³ for a time duration within its limits.
	secs, _ := secsI.Int64()
	ns := nsRem
	var days, hours, minutes, ms, us int64
	var fms, fus, fns float64
	switch largestUnit {
	case UnitYear, UnitMonth, UnitWeek, UnitDay, UnitHour, UnitMinute, UnitSecond:
		us, ns = ns/1000, ns%1000
		ms, us = us/1000, us%1000
		secs += ms / 1000
		ms %= 1000
		if largestUnit <= UnitMinute {
			minutes, secs = secs/60, secs%60
		}
		if largestUnit <= UnitHour {
			hours, minutes = minutes/60, minutes%60
		}
		if largestUnit <= UnitDay {
			days, hours = hours/24, hours%24
		}
		fms, fus, fns = float64(ms), float64(us), float64(ns)
	case UnitMillisecond:
		fms = I128(secs).MulInt64(1000).Add(I128(ns / NsPerMillisecond)).Float64()
		fus = float64(ns / 1000 % 1000)
		fns = float64(ns % 1000)
		secs = 0
	case UnitMicrosecond:
		fus = I128(secs).MulInt64(1_000_000).Add(I128(ns / 1000)).Float64()
		fns = float64(ns % 1000)
		secs = 0
	case UnitNanosecond:
		fns = abs.Float64()
		secs = 0
	}
	fs := float64(s)
	d := Duration{
		Years:        float64(id.Date.Years),
		Months:       float64(id.Date.Months),
		Weeks:        float64(id.Date.Weeks),
		Days:         float64(id.Date.Days) + fs*float64(days),
		Hours:        fs * float64(hours),
		Minutes:      fs * float64(minutes),
		Seconds:      fs * float64(secs),
		Milliseconds: fs * fms,
		Microseconds: fs * fus,
		Nanoseconds:  fs * fns,
	}
	d.normalizeZeros()
	if !IsValidDuration(&d) {
		return Duration{}, rangeErrorf("duration is out of range")
	}
	return d, nil
}

// normalizeZeros turns the negative zeros of d into positive zeros.
func (d *Duration) normalizeZeros() {
	f := d.Fields()
	for i := range f {
		f[i] += 0
	}
	*d = DurationFromFields(f)
}

// Negated returns the negation of d, without negative zeros.
func (d *Duration) Negated() Duration {
	f := d.Fields()
	for i := range f {
		f[i] = -f[i] + 0
	}
	return DurationFromFields(f)
}

// RoundTimeDuration implements RoundTimeDuration.
func RoundTimeDuration(d Int128, increment int64, unit Unit, mode RoundingMode) (Int128, error) {
	r := RoundInt128ToIncrement(d, Mul64(increment, unit.Nanoseconds()), mode)
	if err := checkTimeDuration(r); err != nil {
		return Int128{}, err
	}
	return r, nil
}

// TotalTimeDuration implements TotalTimeDuration.
func TotalTimeDuration(d Int128, unit Unit) float64 {
	return DivToFloat64(d, I128(unit.Nanoseconds()))
}

// roundInt128ToIncrement implements RoundNumberToIncrement for integers in an Int128.
func RoundInt128ToIncrement(x, increment Int128, mode RoundingMode) Int128 {
	q, r := x.QuoRem(increment)
	if r.IsZero() {
		return x
	}
	negative := x.Sign() < 0
	r1 := q.Abs()
	r2 := r1.Add(I128(1))
	cmp := r.Abs().Add(r.Abs()).Cmp(increment)
	even := r1.lo&1 == 0
	rounded := r1
	if mode.unsigned(negative).apply(cmp, even) {
		rounded = r2
	}
	if negative {
		rounded = rounded.Neg()
	}
	return rounded.mul(increment)
}

// mul returns x × y for a product that fits in an Int128.
func (x Int128) mul(y Int128) Int128 {
	if v, ok := y.Int64(); ok {
		return x.MulInt64(v)
	}
	r, _ := Int128FromBig(x.Big().Mul(x.Big(), y.Big()))
	return r
}

// RoundNumberToIncrement implements RoundNumberToIncrement for integers.
func RoundNumberToIncrement(x, increment int64, mode RoundingMode) int64 {
	q, r := x/increment, x%increment
	if r == 0 {
		return x
	}
	negative := x < 0
	r1 := q
	if r1 < 0 {
		r1 = -r1
	}
	ar := r
	if ar < 0 {
		ar = -ar
	}
	cmp := sign64(2*ar - increment)
	rounded := r1
	if mode.unsigned(negative).apply(cmp, r1%2 == 0) {
		rounded = r1 + 1
	}
	if negative {
		rounded = -rounded
	}
	return rounded * increment
}

// RoundNumberToIncrementAsIfPositive implements RoundNumberToIncrementAsIfPositive.
func RoundNumberToIncrementAsIfPositive(x Int128, increment int64, mode RoundingMode) Int128 {
	q, r := x.FloorDivInt64(increment)
	if r == 0 {
		return x
	}
	cmp := sign64(2*r - increment)
	even := q.lo&1 == 0
	if mode.unsigned(false).apply(cmp, even) {
		q = q.Add(I128(1))
	}
	return q.MulInt64(increment)
}
