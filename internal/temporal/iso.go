// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package temporal implements the abstract operations of the Temporal built-in objects:
// the ISO 8601 calendar, epoch nanoseconds, durations and their rounding, time zones, and
// the RFC 9557 string format.
package temporal

import (
	"math"
)

// Nanoseconds in the time units.
const (
	NsPerMicrosecond = 1_000
	NsPerMillisecond = 1_000_000
	NsPerSecond      = 1_000_000_000
	NsPerMinute      = 60 * NsPerSecond
	NsPerHour        = 60 * NsPerMinute
	NsPerDay         = 24 * NsPerHour
)

// maxEpochDays is the number of days from the epoch to the limits of instants.
const maxEpochDays = 100_000_000

var (
	// nsMaxInstant and nsMinInstant are the limits of epoch nanoseconds.
	nsMaxInstant = Mul64(maxEpochDays, NsPerDay)
	nsMinInstant = nsMaxInstant.Neg()

	// The limits of the epoch nanoseconds of ISO date-times, one day less a nanosecond beyond
	// the limits of instants.
	nsMaxDateTime = nsMaxInstant.Add(I128(NsPerDay - 1))
	nsMinDateTime = nsMinInstant.Sub(I128(NsPerDay - 1))
)

// ISODate is an ISO Date Record.
type ISODate struct {
	Year  int
	Month int
	Day   int
}

// Time is a Time Record without its [[Days]] field.
type Time struct {
	Hour        int
	Minute      int
	Second      int
	Millisecond int
	Microsecond int
	Nanosecond  int
}

// ISODateTime is an ISO Date-Time Record.
type ISODateTime struct {
	Date ISODate
	Time Time
}

// MidnightTime is the result of MidnightTimeRecord.
var MidnightTime = Time{}

// floorDiv returns floor(a / b) for b > 0.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b < 0 {
		q--
	}
	return q
}

// floorMod returns a modulo b for b > 0.
func floorMod(a, b int64) int64 {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

// IsLeapYear reports whether year is a leap year in the ISO 8601 calendar.
func IsLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// DaysInYear returns the number of days in year.
func DaysInYear(year int) int {
	if IsLeapYear(year) {
		return 366
	}
	return 365
}

// ISODaysInMonth implements ISODaysInMonth.
func ISODaysInMonth(year, month int) int {
	switch month {
	case 2:
		if IsLeapYear(year) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

// IsValidISODate implements IsValidISODate.
func IsValidISODate(year, month, day int) bool {
	return month >= 1 && month <= 12 && day >= 1 && day <= ISODaysInMonth(year, month)
}

// IsValidTime implements IsValidTime.
func IsValidTime(t Time) bool {
	return t.Hour >= 0 && t.Hour <= 23 &&
		t.Minute >= 0 && t.Minute <= 59 &&
		t.Second >= 0 && t.Second <= 59 &&
		t.Millisecond >= 0 && t.Millisecond <= 999 &&
		t.Microsecond >= 0 && t.Microsecond <= 999 &&
		t.Nanosecond >= 0 && t.Nanosecond <= 999
}

// BalanceISOYearMonth implements BalanceISOYearMonth.
func BalanceISOYearMonth(year, month int64) (int64, int) {
	m := month - 1
	return year + floorDiv(m, 12), int(floorMod(m, 12)) + 1
}

// ISODateToEpochDays implements ISODateToEpochDays for a month in any range and a day of
// any value.
func ISODateToEpochDays(year, month, day int64) int64 {
	y, m := BalanceISOYearMonth(year, month)
	// Days from 0000-03-01, with years starting in March so that the leap day is last.
	if m <= 2 {
		y--
	}
	era := floorDiv(y, 400)
	yoe := y - era*400
	mp := (int64(m) + 9) % 12
	doy := (153*mp+2)/5 + day - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

// EpochDaysToISODate returns the ISO date of the day days after the epoch.
func EpochDaysToISODate(days int64) ISODate {
	z := days + 719468
	era := floorDiv(z, 146097)
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	m := mp + 3
	if m > 12 {
		m -= 12
	}
	if m <= 2 {
		y++
	}
	return ISODate{
		Year:  int(y),
		Month: int(m),
		Day:   int(d),
	}
}

// EpochDays returns the number of days from the epoch to d.
func (d ISODate) EpochDays() int64 {
	return ISODateToEpochDays(int64(d.Year), int64(d.Month), int64(d.Day))
}

// AddDaysToISODate implements AddDaysToISODate.
func AddDaysToISODate(d ISODate, days int64) ISODate {
	return EpochDaysToISODate(d.EpochDays() + days)
}

// CompareISODate implements CompareISODate.
func CompareISODate(a, b ISODate) int {
	switch {
	case a.Year != b.Year:
		return sign(a.Year - b.Year)
	case a.Month != b.Month:
		return sign(a.Month - b.Month)
	}
	return sign(a.Day - b.Day)
}

// CompareTimeRecord implements CompareTimeRecord.
func CompareTimeRecord(a, b Time) int {
	return sign64(a.NanosecondOfDay() - b.NanosecondOfDay())
}

// CompareISODateTime implements CompareISODateTime.
func CompareISODateTime(a, b ISODateTime) int {
	if c := CompareISODate(a.Date, b.Date); c != 0 {
		return c
	}
	return CompareTimeRecord(a.Time, b.Time)
}

func sign(x int) int {
	switch {
	case x < 0:
		return -1
	case x > 0:
		return 1
	}
	return 0
}

func sign64(x int64) int {
	switch {
	case x < 0:
		return -1
	case x > 0:
		return 1
	}
	return 0
}

// NanosecondOfDay returns the nanoseconds from midnight to t.
func (t Time) NanosecondOfDay() int64 {
	return int64(t.Hour)*NsPerHour + int64(t.Minute)*NsPerMinute + int64(t.Second)*NsPerSecond +
		int64(t.Millisecond)*NsPerMillisecond + int64(t.Microsecond)*NsPerMicrosecond + int64(t.Nanosecond)
}

// SubSecondNanoseconds returns the nanoseconds of t past its second.
func (t Time) SubSecondNanoseconds() int {
	return t.Millisecond*NsPerMillisecond + t.Microsecond*NsPerMicrosecond + t.Nanosecond
}

// timeFromNanosecondOfDay returns the time ns nanoseconds after midnight, 0 ≤ ns < NsPerDay.
func timeFromNanosecondOfDay(ns int64) Time {
	return Time{
		Hour:        int(ns / NsPerHour),
		Minute:      int(ns / NsPerMinute % 60),
		Second:      int(ns / NsPerSecond % 60),
		Millisecond: int(ns / NsPerMillisecond % 1000),
		Microsecond: int(ns / NsPerMicrosecond % 1000),
		Nanosecond:  int(ns % 1000),
	}
}

// BalanceTime implements BalanceTime. It returns the time and the number of days beyond it.
// The total of the components in nanoseconds must fit in an Int128.
func BalanceTime(hour, minute, second, millisecond, microsecond, nanosecond int64) (Time, int64) {
	total := Mul64(hour, NsPerHour).
		Add(Mul64(minute, NsPerMinute)).
		Add(Mul64(second, NsPerSecond)).
		Add(Mul64(millisecond, NsPerMillisecond)).
		Add(Mul64(microsecond, NsPerMicrosecond)).
		Add(I128(nanosecond))
	return balanceTimeNs(total)
}

// balanceTimeNs returns the time of the day ns nanoseconds after a midnight, and the number of
// days from that midnight.
func balanceTimeNs(ns Int128) (Time, int64) {
	days, rem := ns.FloorDivInt64(NsPerDay)
	d, _ := days.Int64()
	return timeFromNanosecondOfDay(rem), d
}

// BalanceISODateTime implements BalanceISODateTime.
func BalanceISODateTime(d ISODate, hour, minute, second, millisecond, microsecond, nanosecond int64) ISODateTime {
	t, days := BalanceTime(hour, minute, second, millisecond, microsecond, nanosecond)
	return ISODateTime{
		Date: AddDaysToISODate(d, days),
		Time: t,
	}
}

// RegulateISODate implements RegulateISODate. year, month and day are mathematical values.
func RegulateISODate(year, month, day float64, overflow Overflow) (ISODate, error) {
	if overflow == OverflowConstrain {
		month = max(1, min(12, month))
	} else if month < 1 || month > 12 {
		return ISODate{}, rangeErrorf("month %v is out of range", month)
	}
	// Years far beyond the limits are rejected later anyway, so they are clamped to keep the
	// arithmetic in range.
	y := int(max(-1e9, min(1e9, year)))
	dim := float64(ISODaysInMonth(y, int(month)))
	if month == 2 {
		// The year may be beyond the range of int, but whether it is a leap year is exact.
		dim = 28
		if m4, m100, m400 := math.Mod(year, 4), math.Mod(year, 100), math.Mod(year, 400); m4 == 0 && (m100 != 0 || m400 == 0) {
			dim = 29
		}
	}
	if overflow == OverflowConstrain {
		day = max(1, min(dim, day))
	} else if day < 1 || day > dim {
		return ISODate{}, rangeErrorf("day %v is out of range", day)
	}
	return ISODate{
		Year:  y,
		Month: int(month),
		Day:   int(day),
	}, nil
}

// RegulateTime implements RegulateTime. The components are mathematical values.
func RegulateTime(hour, minute, second, millisecond, microsecond, nanosecond float64, overflow Overflow) (Time, error) {
	if overflow == OverflowConstrain {
		return Time{
			Hour:        int(max(0, min(23, hour))),
			Minute:      int(max(0, min(59, minute))),
			Second:      int(max(0, min(59, second))),
			Millisecond: int(max(0, min(999, millisecond))),
			Microsecond: int(max(0, min(999, microsecond))),
			Nanosecond:  int(max(0, min(999, nanosecond))),
		}, nil
	}
	return CreateTimeRecord(hour, minute, second, millisecond, microsecond, nanosecond)
}

// CreateTimeRecord implements CreateTimeRecord with a validity check of the components.
func CreateTimeRecord(hour, minute, second, millisecond, microsecond, nanosecond float64) (Time, error) {
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 || second < 0 || second > 59 ||
		millisecond < 0 || millisecond > 999 || microsecond < 0 || microsecond > 999 ||
		nanosecond < 0 || nanosecond > 999 {
		return Time{}, rangeErrorf("time is out of range")
	}
	return Time{
		Hour:        int(hour),
		Minute:      int(minute),
		Second:      int(second),
		Millisecond: int(millisecond),
		Microsecond: int(microsecond),
		Nanosecond:  int(nanosecond),
	}, nil
}

// CreateISODateRecord returns the ISO date of the mathematical values, or a RangeError when
// they do not make a valid date.
func CreateISODateRecord(year, month, day float64) (ISODate, error) {
	if month < 1 || month > 12 || day < 1 || day > 31 || year < -1e9 || year > 1e9 {
		return ISODate{}, rangeErrorf("date is out of range")
	}
	d := ISODate{
		Year:  int(year),
		Month: int(month),
		Day:   int(day),
	}
	if !IsValidISODate(d.Year, d.Month, d.Day) {
		return ISODate{}, rangeErrorf("date is out of range")
	}
	return d, nil
}

// UTCEpochNanoseconds implements GetUTCEpochNanoseconds.
func (dt ISODateTime) UTCEpochNanoseconds() Int128 {
	return Mul64(dt.Date.EpochDays(), NsPerDay).Add(I128(dt.Time.NanosecondOfDay()))
}

// ISODateTimeFromEpochNanoseconds implements GetISOPartsFromEpoch.
func ISODateTimeFromEpochNanoseconds(ns Int128) ISODateTime {
	days, rem := ns.FloorDivInt64(NsPerDay)
	d, _ := days.Int64()
	return ISODateTime{
		Date: EpochDaysToISODate(d),
		Time: timeFromNanosecondOfDay(rem),
	}
}

// IsValidEpochNanoseconds implements IsValidEpochNanoseconds.
func IsValidEpochNanoseconds(ns Int128) bool {
	return ns.Cmp(nsMinInstant) >= 0 && ns.Cmp(nsMaxInstant) <= 0
}

// ISODateTimeWithinLimits implements ISODateTimeWithinLimits.
func ISODateTimeWithinLimits(dt ISODateTime) bool {
	if dt.Date.Year < -300000 || dt.Date.Year > 300000 {
		return false
	}
	ns := dt.UTCEpochNanoseconds()
	return ns.Cmp(nsMinDateTime) >= 0 && ns.Cmp(nsMaxDateTime) <= 0
}

// ISODateWithinLimits reports whether noon of d is within the limits of ISO date-times.
func ISODateWithinLimits(d ISODate) bool {
	return ISODateTimeWithinLimits(ISODateTime{
		Date: d,
		Time: Time{Hour: 12},
	})
}

// ISOYearMonthWithinLimits implements ISOYearMonthWithinLimits.
func ISOYearMonthWithinLimits(d ISODate) bool {
	switch {
	case d.Year < -271821 || d.Year > 275760:
		return false
	case d.Year == -271821 && d.Month < 4:
		return false
	case d.Year == 275760 && d.Month > 9:
		return false
	}
	return true
}

// CheckISODaysRange implements CheckISODaysRange.
func CheckISODaysRange(d ISODate) error {
	if days := d.EpochDays(); days < -maxEpochDays || days > maxEpochDays {
		return rangeErrorf("date is out of range")
	}
	return nil
}

// DayOfWeek returns the ISO day of the week of d, 1 for Monday to 7 for Sunday.
func (d ISODate) DayOfWeek() int {
	// 1970-01-01 was a Thursday.
	return int(floorMod(d.EpochDays()+3, 7)) + 1
}

// DayOfYear returns the ordinal day of d in its year.
func (d ISODate) DayOfYear() int {
	return int(d.EpochDays()-ISODateToEpochDays(int64(d.Year), 1, 1)) + 1
}

// WeekOfYear returns the ISO 8601 week number of d and the year that the week belongs to.
func (d ISODate) WeekOfYear() (week, year int) {
	week = (d.DayOfYear() - d.DayOfWeek() + 10) / 7
	year = d.Year
	if week < 1 {
		year--
		return weeksInYear(year), year
	}
	if week > weeksInYear(year) {
		return 1, year + 1
	}
	return week, year
}

// weeksInYear returns the number of ISO weeks in year.
func weeksInYear(year int) int {
	jan1 := ISODate{
		Year:  year,
		Month: 1,
		Day:   1,
	}.DayOfWeek()
	if jan1 == 4 || (jan1 == 3 && IsLeapYear(year)) {
		return 53
	}
	return 52
}
