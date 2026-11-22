// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package calendar_test

import (
	"errors"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/calendar"
	"github.com/hajimehoshi/ekuma/internal/temporal"
)

func epochDays(y, m, d int) int64 {
	return isoDate(y, m, d).EpochDays()
}

// The expected dates are those of ICU4X.
func TestDate(t *testing.T) {
	for _, tc := range []struct {
		cal  string
		day  int64
		want calendar.Date
	}{
		{
			cal: "buddhist",
			day: epochDays(2024, 3, 15),
			want: calendar.Date{
				Era:          "be",
				EraYear:      2567,
				Year:         2567,
				Month:        3,
				MonthCode:    calendar.MonthCode{Number: 3},
				Day:          15,
				DayOfYear:    75,
				DaysInMonth:  31,
				DaysInYear:   366,
				MonthsInYear: 12,
				InLeapYear:   true,
			},
		},
		{
			cal: "chinese",
			day: epochDays(2024, 3, 15),
			want: calendar.Date{
				Year:         2024,
				Month:        2,
				MonthCode:    calendar.MonthCode{Number: 2},
				Day:          6,
				DayOfYear:    35,
				DaysInMonth:  30,
				DaysInYear:   354,
				MonthsInYear: 12,
			},
		},
		// A leap month in a year of the mean approximation.
		{
			cal: "chinese",
			day: -62948,
			want: calendar.Date{
				Year:         1797,
				Month:        8,
				MonthCode:    calendar.MonthCode{Number: 7, Leap: true},
				Day:          5,
				DayOfYear:    212,
				DaysInMonth:  29,
				DaysInYear:   384,
				MonthsInYear: 13,
				InLeapYear:   true,
			},
		},
		// The last day of Temporal, far in the mean approximation.
		{
			cal: "chinese",
			day: 100000000,
			want: calendar.Date{
				Year:         275760,
				Month:        7,
				MonthCode:    calendar.MonthCode{Number: 7},
				Day:          30,
				DayOfYear:    207,
				DaysInMonth:  30,
				DaysInYear:   354,
				MonthsInYear: 12,
			},
		},
		// A year of the Qing dynasty.
		{
			cal: "dangi",
			day: epochDays(1905, 2, 4),
			want: calendar.Date{
				Year:         1905,
				Month:        1,
				MonthCode:    calendar.MonthCode{Number: 1},
				Day:          1,
				DayOfYear:    1,
				DaysInMonth:  30,
				DaysInYear:   355,
				MonthsInYear: 12,
			},
		},
		{
			cal: "dangi",
			day: 100000000,
			want: calendar.Date{
				Year:         275760,
				Month:        7,
				MonthCode:    calendar.MonthCode{Number: 7},
				Day:          30,
				DayOfYear:    207,
				DaysInMonth:  30,
				DaysInYear:   355,
				MonthsInYear: 12,
			},
		},
		{
			cal: "coptic",
			day: -62571,
			want: calendar.Date{
				Era:          "am",
				EraYear:      1514,
				Year:         1514,
				Month:        13,
				MonthCode:    calendar.MonthCode{Number: 13},
				Day:          5,
				DayOfYear:    365,
				DaysInMonth:  5,
				DaysInYear:   365,
				MonthsInYear: 13,
			},
		},
		{
			cal: "ethioaa",
			day: epochDays(2024, 3, 15),
			want: calendar.Date{
				Era:          "aa",
				EraYear:      7516,
				Year:         7516,
				Month:        7,
				MonthCode:    calendar.MonthCode{Number: 7},
				Day:          6,
				DayOfYear:    186,
				DaysInMonth:  30,
				DaysInYear:   365,
				MonthsInYear: 13,
			},
		},
		{
			cal: "ethiopic",
			day: epochDays(0, 1, 1),
			want: calendar.Date{
				Era:          "aa",
				EraYear:      5492,
				Year:         -8,
				Month:        5,
				MonthCode:    calendar.MonthCode{Number: 5},
				Day:          7,
				DayOfYear:    127,
				DaysInMonth:  30,
				DaysInYear:   365,
				MonthsInYear: 13,
			},
		},
		{
			cal: "gregory",
			day: epochDays(0, 1, 1),
			want: calendar.Date{
				Era:          "bce",
				EraYear:      1,
				Year:         0,
				Month:        1,
				MonthCode:    calendar.MonthCode{Number: 1},
				Day:          1,
				DayOfYear:    1,
				DaysInMonth:  31,
				DaysInYear:   366,
				MonthsInYear: 12,
				InLeapYear:   true,
			},
		},
		{
			cal: "hebrew",
			day: epochDays(2024, 3, 15),
			want: calendar.Date{
				Era:          "am",
				EraYear:      5784,
				Year:         5784,
				Month:        7,
				MonthCode:    calendar.MonthCode{Number: 6},
				Day:          5,
				DayOfYear:    182,
				DaysInMonth:  29,
				DaysInYear:   383,
				MonthsInYear: 13,
				InLeapYear:   true,
			},
		},
		{
			cal: "hebrew",
			day: -62415,
			want: calendar.Date{
				Era:          "am",
				EraYear:      5559,
				Year:         5559,
				Month:        6,
				MonthCode:    calendar.MonthCode{Number: 5, Leap: true},
				Day:          6,
				DayOfYear:    154,
				DaysInMonth:  30,
				DaysInYear:   384,
				MonthsInYear: 13,
				InLeapYear:   true,
			},
		},
		{
			cal: "indian",
			day: epochDays(2024, 3, 15),
			want: calendar.Date{
				Era:          "shaka",
				EraYear:      1945,
				Year:         1945,
				Month:        12,
				MonthCode:    calendar.MonthCode{Number: 12},
				Day:          25,
				DayOfYear:    360,
				DaysInMonth:  30,
				DaysInYear:   365,
				MonthsInYear: 12,
			},
		},
		{
			cal: "islamic-civil",
			day: epochDays(0, 1, 1),
			want: calendar.Date{
				Era:          "bh",
				EraYear:      642,
				Year:         -641,
				Month:        5,
				MonthCode:    calendar.MonthCode{Number: 5},
				Day:          6,
				DayOfYear:    124,
				DaysInMonth:  30,
				DaysInYear:   354,
				MonthsInYear: 12,
			},
		},
		{
			cal: "islamic-tbla",
			day: epochDays(2024, 3, 15),
			want: calendar.Date{
				Era:          "ah",
				EraYear:      1445,
				Year:         1445,
				Month:        9,
				MonthCode:    calendar.MonthCode{Number: 9},
				Day:          6,
				DayOfYear:    242,
				DaysInMonth:  30,
				DaysInYear:   355,
				MonthsInYear: 12,
				InLeapYear:   true,
			},
		},
		// A published year, which is not a leap year of the tabular calendar.
		{
			cal: "islamic-umalqura",
			day: epochDays(2024, 3, 15),
			want: calendar.Date{
				Era:          "ah",
				EraYear:      1445,
				Year:         1445,
				Month:        9,
				MonthCode:    calendar.MonthCode{Number: 9},
				Day:          5,
				DayOfYear:    241,
				DaysInMonth:  30,
				DaysInYear:   354,
				MonthsInYear: 12,
			},
		},
		{
			cal: "islamic-umalqura",
			day: -100000000,
			want: calendar.Date{
				Era:          "bh",
				EraYear:      280805,
				Year:         -280804,
				Month:        3,
				MonthCode:    calendar.MonthCode{Number: 3},
				Day:          22,
				DayOfYear:    81,
				DaysInMonth:  30,
				DaysInYear:   355,
				MonthsInYear: 12,
				InLeapYear:   true,
			},
		},
		{
			cal: "japanese",
			day: epochDays(2024, 3, 15),
			want: calendar.Date{
				Era:          "reiwa",
				EraYear:      6,
				Year:         2024,
				Month:        3,
				MonthCode:    calendar.MonthCode{Number: 3},
				Day:          15,
				DayOfYear:    75,
				DaysInMonth:  31,
				DaysInYear:   366,
				MonthsInYear: 12,
				InLeapYear:   true,
			},
		},
		{
			cal: "japanese",
			day: -63000,
			want: calendar.Date{
				Era:          "ce",
				EraYear:      1797,
				Year:         1797,
				Month:        7,
				MonthCode:    calendar.MonthCode{Number: 7},
				Day:          6,
				DayOfYear:    187,
				DaysInMonth:  31,
				DaysInYear:   365,
				MonthsInYear: 12,
			},
		},
		{
			cal: "persian",
			day: epochDays(2024, 3, 15),
			want: calendar.Date{
				Era:          "ap",
				EraYear:      1402,
				Year:         1402,
				Month:        12,
				MonthCode:    calendar.MonthCode{Number: 12},
				Day:          25,
				DayOfYear:    361,
				DaysInMonth:  29,
				DaysInYear:   365,
				MonthsInYear: 12,
			},
		},
		// 1502 is the first year that the 33-year rule makes a leap year but the equinox does
		// not, so 1503 starts a day earlier and is a leap year instead.
		{
			cal: "persian",
			day: epochDays(2124, 3, 20),
			want: calendar.Date{
				Era:          "ap",
				EraYear:      1503,
				Year:         1503,
				Month:        1,
				MonthCode:    calendar.MonthCode{Number: 1},
				Day:          1,
				DayOfYear:    1,
				DaysInMonth:  31,
				DaysInYear:   366,
				MonthsInYear: 12,
				InLeapYear:   true,
			},
		},
		{
			cal: "roc",
			day: -63000,
			want: calendar.Date{
				Era:          "broc",
				EraYear:      115,
				Year:         -114,
				Month:        7,
				MonthCode:    calendar.MonthCode{Number: 7},
				Day:          6,
				DayOfYear:    187,
				DaysInMonth:  31,
				DaysInYear:   365,
				MonthsInYear: 12,
			},
		},
	} {
		c := calendar.Lookup(tc.cal)
		got := c.Date(tc.day)
		if got != tc.want {
			t.Errorf("%s: Date(%d): got %+v, want %+v", tc.cal, tc.day, got, tc.want)
		}
		if d := c.EpochDays(got.Year, got.Month, got.Day); d != tc.day {
			t.Errorf("%s: EpochDays(%d, %d, %d): got %d, want %d", tc.cal, got.Year, got.Month, got.Day, d, tc.day)
		}
	}
}

func TestCanonicalize(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want string
		ok   bool
	}{
		{
			id:   "islamicc",
			want: "islamic-civil",
			ok:   true,
		},
		{
			id:   "ethiopic-amete-alem",
			want: "ethioaa",
			ok:   true,
		},
		{
			id:   "islamic",
			want: "",
			ok:   false,
		},
	} {
		got, ok := calendar.Canonicalize(tc.id)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Canonicalize(%q): got %q, %t, want %q, %t", tc.id, got, ok, tc.want, tc.ok)
		}
	}
}

func isoDate(y, m, d int) temporal.ISODate {
	return temporal.ISODate{
		Year:  y,
		Month: m,
		Day:   d,
	}
}

func errorKind(err error) (temporal.ErrorKind, bool) {
	var e *temporal.Error
	if !errors.As(err, &e) {
		return 0, false
	}
	return e.Kind, true
}

func TestResolveFields(t *testing.T) {
	for _, tc := range []struct {
		cal      string
		f        temporal.CalendarFields
		wantYear float64
		wantErr  temporal.ErrorKind
		err      bool
	}{
		{
			cal: "gregory",
			f: temporal.CalendarFields{
				Era:        "bc",
				HasEra:     true,
				EraYear:    1,
				HasEraYear: true,
				Month:      1,
				HasMonth:   true,
				Day:        1,
				HasDay:     true,
			},
			wantYear: 0,
		},
		// An era year beyond the end of the era is accepted.
		{
			cal: "japanese",
			f: temporal.CalendarFields{
				Era:        "heisei",
				HasEra:     true,
				EraYear:    32,
				HasEraYear: true,
				Month:      1,
				HasMonth:   true,
				Day:        1,
				HasDay:     true,
			},
			wantYear: 2020,
		},
		{
			cal: "gregory",
			f: temporal.CalendarFields{
				Era:        "ce",
				HasEra:     true,
				EraYear:    1,
				HasEraYear: true,
				Year:       2,
				HasYear:    true,
				Month:      1,
				HasMonth:   true,
				Day:        1,
				HasDay:     true,
			},
			wantErr: temporal.RangeError,
			err:     true,
		},
		{
			cal: "roc",
			f: temporal.CalendarFields{
				Era:        "ce",
				HasEra:     true,
				EraYear:    1,
				HasEraYear: true,
				Month:      1,
				HasMonth:   true,
				Day:        1,
				HasDay:     true,
			},
			wantErr: temporal.RangeError,
			err:     true,
		},
		{
			cal: "gregory",
			f: temporal.CalendarFields{
				Era:      "ce",
				HasEra:   true,
				Year:     1,
				HasYear:  true,
				Month:    1,
				HasMonth: true,
				Day:      1,
				HasDay:   true,
			},
			wantErr: temporal.TypeError,
			err:     true,
		},
		// A calendar without eras needs a year.
		{
			cal: "chinese",
			f: temporal.CalendarFields{
				Era:        "ce",
				HasEra:     true,
				EraYear:    1,
				HasEraYear: true,
				Month:      1,
				HasMonth:   true,
				Day:        1,
				HasDay:     true,
			},
			wantErr: temporal.TypeError,
			err:     true,
		},
		{
			cal: "gregory",
			f: temporal.CalendarFields{
				Year:         2024,
				HasYear:      true,
				MonthCode:    "M05L",
				HasMonthCode: true,
				Day:          1,
				HasDay:       true,
			},
			wantErr: temporal.RangeError,
			err:     true,
		},
	} {
		f := tc.f
		err := calendar.Temporal.Lookup(tc.cal).ResolveFields(&f, temporal.FieldsDate)
		if tc.err {
			if k, ok := errorKind(err); !ok || k != tc.wantErr {
				t.Errorf("%s: ResolveFields(%+v): got %v, want an error of kind %d", tc.cal, tc.f, err, tc.wantErr)
			}
			continue
		}
		if err != nil || f.Year != tc.wantYear || f.HasEra || f.HasEraYear {
			t.Errorf("%s: ResolveFields(%+v): got %+v, %v, want year %v", tc.cal, tc.f, f, err, tc.wantYear)
		}
	}
}

func TestDateFromFields(t *testing.T) {
	for _, tc := range []struct {
		cal      string
		f        temporal.CalendarFields
		overflow temporal.Overflow
		want     temporal.ISODate
		err      bool
	}{
		// Adar I in a common year is Adar.
		{
			cal: "hebrew",
			f: temporal.CalendarFields{
				Year:         5783,
				HasYear:      true,
				MonthCode:    "M05L",
				HasMonthCode: true,
				Day:          1,
				HasDay:       true,
			},
			overflow: temporal.OverflowConstrain,
			want:     isoDate(2023, 2, 22),
		},
		{
			cal: "hebrew",
			f: temporal.CalendarFields{
				Year:         5783,
				HasYear:      true,
				MonthCode:    "M05L",
				HasMonthCode: true,
				Day:          1,
				HasDay:       true,
			},
			overflow: temporal.OverflowReject,
			err:      true,
		},
		// The last day of the intercalary third month in the Korean calendar.
		{
			cal: "dangi",
			f: temporal.CalendarFields{
				Year:         1879,
				HasYear:      true,
				MonthCode:    "M03L",
				HasMonthCode: true,
				Day:          30,
				HasDay:       true,
			},
			overflow: temporal.OverflowReject,
			want:     isoDate(1879, 5, 20),
		},
		// A missing leap month of the Chinese calendar is its common month.
		{
			cal: "chinese",
			f: temporal.CalendarFields{
				Year:         2024,
				HasYear:      true,
				MonthCode:    "M02L",
				HasMonthCode: true,
				Day:          1,
				HasDay:       true,
			},
			overflow: temporal.OverflowConstrain,
			want:     isoDate(2024, 3, 10),
		},
	} {
		got, err := temporal.CalendarDateFromFields(tc.cal, tc.f, tc.overflow)
		if tc.err {
			if k, ok := errorKind(err); !ok || k != temporal.RangeError {
				t.Errorf("%s: CalendarDateFromFields(%+v): got %v, want a RangeError", tc.cal, tc.f, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: CalendarDateFromFields(%+v): got %v, %v, want %v", tc.cal, tc.f, got, err, tc.want)
		}
	}
}

// The expected reference dates are those of ICU4X.
func TestMonthDayToISOReferenceDate(t *testing.T) {
	for _, tc := range []struct {
		cal  string
		code string
		day  float64
		want temporal.ISODate
	}{
		// Adar I did not occur in 1971 and 1972.
		{
			cal:  "hebrew",
			code: "M05L",
			day:  1,
			want: isoDate(1970, 2, 7),
		},
		{
			cal:  "chinese",
			code: "M09L",
			day:  1,
			want: isoDate(2014, 10, 24),
		},
		// A leap month not known to occur is constrained to its common month.
		{
			cal:  "chinese",
			code: "M01L",
			day:  1,
			want: isoDate(1972, 2, 15),
		},
		{
			cal:  "islamic-umalqura",
			code: "M02",
			day:  30,
			want: isoDate(1970, 5, 6),
		},
		{
			cal:  "coptic",
			code: "M13",
			day:  6,
			want: isoDate(1971, 9, 11),
		},
	} {
		f := temporal.CalendarFields{
			MonthCode:    tc.code,
			HasMonthCode: true,
			Day:          tc.day,
			HasDay:       true,
		}
		got, err := temporal.CalendarMonthDayFromFields(tc.cal, f, temporal.OverflowConstrain)
		if err != nil || got != tc.want {
			t.Errorf("%s: CalendarMonthDayFromFields(%s, %v): got %v, %v, want %v", tc.cal, tc.code, tc.day, got, err, tc.want)
		}
	}
}

func TestDateAdd(t *testing.T) {
	// Adar I 30, 5784, a year later is the last day of Adar, 5785.
	from := isoDate(2024, 3, 10)
	got, err := temporal.CalendarDateAdd("hebrew", from, temporal.DateDuration{
		Years: 1,
	}, temporal.OverflowConstrain)
	if want := isoDate(2025, 3, 29); err != nil || got != want {
		t.Errorf("CalendarDateAdd: got %v, %v, want %v", got, err, want)
	}
	if _, err := temporal.CalendarDateAdd("hebrew", from, temporal.DateDuration{
		Years: 1,
	}, temporal.OverflowReject); err == nil {
		t.Errorf("CalendarDateAdd with reject: got no error")
	}
	if _, err := temporal.CalendarDateAdd("chinese", from, temporal.DateDuration{
		Years: 300000,
	}, temporal.OverflowConstrain); err == nil {
		t.Errorf("CalendarDateAdd beyond the limits: got no error")
	}
}

func TestDateUntil(t *testing.T) {
	// From 2023 M02 1 to 2024 M02 1 of the Chinese calendar, with M02L between.
	one, two := isoDate(2023, 2, 20), isoDate(2024, 3, 10)
	for _, tc := range []struct {
		unit temporal.Unit
		want temporal.DateDuration
	}{
		{
			unit: temporal.UnitYear,
			want: temporal.DateDuration{
				Years: 1,
			},
		},
		{
			unit: temporal.UnitMonth,
			want: temporal.DateDuration{
				Months: 13,
			},
		},
		{
			unit: temporal.UnitWeek,
			want: temporal.DateDuration{
				Weeks: 54,
				Days:  6,
			},
		},
	} {
		if got := temporal.CalendarDateUntil("chinese", one, two, tc.unit); got != tc.want {
			t.Errorf("CalendarDateUntil(%v): got %+v, want %+v", tc.unit, got, tc.want)
		}
	}
	// A difference over the whole range counts its months without overflowing.
	got := temporal.CalendarDateUntil("chinese", isoDate(-271821, 4, 19), isoDate(275760, 9, 13), temporal.UnitMonth)
	if got.Months < 6700000 || got.Months > 6800000 {
		t.Errorf("CalendarDateUntil over the range: got %+v", got)
	}
}

func TestMain(m *testing.M) {
	temporal.SetCalendars(calendar.Temporal)
	m.Run()
}
