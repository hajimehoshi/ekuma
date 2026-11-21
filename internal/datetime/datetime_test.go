// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime_test

import (
	"math"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma/internal/datetime"
)

// TestRangeLimits checks the date arithmetic at the limits of the time values and just beyond them.
func TestRangeLimits(t *testing.T) {
	tests := []struct {
		tv                float64
		year, month, date float64
		weekDay, ms       float64
	}{
		{
			tv:      -8.64e15,
			year:    -271821,
			month:   3,
			date:    20,
			weekDay: 2,
			ms:      0,
		},
		{
			tv:      8.64e15,
			year:    275760,
			month:   8,
			date:    13,
			weekDay: 6,
			ms:      0,
		},
		{
			tv:      -1,
			year:    1969,
			month:   11,
			date:    31,
			weekDay: 3,
			ms:      999,
		},
	}
	for _, tc := range tests {
		if got := datetime.YearFromTime(tc.tv); got != tc.year {
			t.Errorf("YearFromTime(%v): got %v, want %v", tc.tv, got, tc.year)
		}
		if got := datetime.MonthFromTime(tc.tv); got != tc.month {
			t.Errorf("MonthFromTime(%v): got %v, want %v", tc.tv, got, tc.month)
		}
		if got := datetime.DateFromTime(tc.tv); got != tc.date {
			t.Errorf("DateFromTime(%v): got %v, want %v", tc.tv, got, tc.date)
		}
		if got := datetime.WeekDay(tc.tv); got != tc.weekDay {
			t.Errorf("WeekDay(%v): got %v, want %v", tc.tv, got, tc.weekDay)
		}
		if got := datetime.MsFromTime(tc.tv); got != tc.ms {
			t.Errorf("MsFromTime(%v): got %v, want %v", tc.tv, got, tc.ms)
		}
		day := datetime.MakeDay(tc.year, tc.month, tc.date)
		if got := datetime.MakeDate(day, datetime.TimeWithinDay(tc.tv)); got != tc.tv {
			t.Errorf("MakeDate of the components of %v: got %v", tc.tv, got)
		}
	}

	if got := datetime.TimeClip(8.64e15 + 1); !math.IsNaN(got) {
		t.Errorf("TimeClip(8.64e15 + 1): got %v, want NaN", got)
	}
	if got := datetime.TimeClip(math.Copysign(0, -1)); got != 0 || math.Signbit(got) {
		t.Errorf("TimeClip(-0): got %v, want +0", got)
	}
	if got := datetime.MakeDay(1e300, 0, 1); !math.IsNaN(got) {
		t.Errorf("MakeDay(1e300, 0, 1): got %v, want NaN", got)
	}
	if got := datetime.MakeDate(1e305, 0); !math.IsNaN(got) {
		t.Errorf("MakeDate(1e305, 0): got %v, want NaN", got)
	}
	// Months beyond a year carry into the year, and negative months borrow from it.
	if got, want := datetime.MakeDay(2000, -1, 1), datetime.MakeDay(1999, 11, 1); got != want {
		t.Errorf("MakeDay(2000, -1, 1): got %v, want %v", got, want)
	}
}

// TestParseLegacy checks the formats that Parse accepts besides the date time string format.
func TestParseLegacy(t *testing.T) {
	loc := datetime.LocationZone(time.FixedZone("Test", 9*3600))
	utc := func(y, mo, d, h, mi, s float64) float64 {
		return datetime.MakeDate(datetime.MakeDay(y, mo-1, d), datetime.MakeTime(h, mi, s, 0))
	}
	const local = -9 * 3600 * 1000
	nan := math.NaN()
	tests := []struct {
		in   string
		want float64
	}{
		{
			in:   "2020/01/02",
			want: utc(2020, 1, 2, 0, 0, 0) + local,
		},
		{
			in:   "2020-01-2",
			want: utc(2020, 1, 2, 0, 0, 0) + local,
		},
		{
			in:   "2020/1/2 03:04:05",
			want: utc(2020, 1, 2, 3, 4, 5) + local,
		},
		{
			in:   "1/2/2020",
			want: utc(2020, 1, 2, 0, 0, 0) + local,
		},
		{
			in:   "Jan 2, 2020",
			want: utc(2020, 1, 2, 0, 0, 0) + local,
		},
		{
			in:   "January 2, 2020 03:04:05",
			want: utc(2020, 1, 2, 3, 4, 5) + local,
		},
		{
			in:   "2 Jan 2020 03:04:05 GMT",
			want: utc(2020, 1, 2, 3, 4, 5),
		},
		{
			in:   "Thu, 02 Jan 2020 03:04:05 UTC",
			want: utc(2020, 1, 2, 3, 4, 5),
		},
		{
			in:   "Thursday January 2 2020 03:04:05 Z",
			want: utc(2020, 1, 2, 3, 4, 5),
		},
		{
			in:   "Jan 2 2020 03:04:05 +0130",
			want: utc(2020, 1, 2, 1, 34, 5),
		},
		{
			in:   "Jan 2 2020 03:04:05 GMT-05:00",
			want: utc(2020, 1, 2, 8, 4, 5),
		},
		{
			in:   "Jan 2 2020 3:04 PM",
			want: utc(2020, 1, 2, 15, 4, 0) + local,
		},
		{
			in:   "Jan 2 2020 12:04 AM",
			want: utc(2020, 1, 2, 0, 4, 0) + local,
		},
		{
			in:   "Thu Jan 02 2020 03:04:05 GMT+0900 (Japan Standard Time)",
			want: utc(2020, 1, 1, 18, 4, 5),
		},
		{
			in:   "Fri Dec 31 -0001 00:00:00 GMT+0000",
			want: utc(-1, 12, 31, 0, 0, 0),
		},
		{
			in:   "Jan 2 99",
			want: utc(1999, 1, 2, 0, 0, 0) + local,
		},
		{
			in:   "Jan 2 2020 25:00",
			want: nan,
		},
		{
			in:   "Jan 32 2020",
			want: nan,
		},
		{
			in:   "Jan Feb 2 2020",
			want: nan,
		},
		{
			in:   "2020/01/02 Jan",
			want: nan,
		},
		{
			in:   "Jan 2 2020 03:04:05 +0100 +0200",
			want: nan,
		},
		{
			in:   "Jan 2 2020 (unclosed",
			want: nan,
		},
		{
			in:   "hello",
			want: nan,
		},
		{
			in:   "",
			want: nan,
		},
	}
	for _, tc := range tests {
		got := datetime.Parse(tc.in, loc)
		if got != tc.want && !(math.IsNaN(got) && math.IsNaN(tc.want)) {
			t.Errorf("Parse(%q): got %v, want %v", tc.in, got, tc.want)
		}
	}
}
