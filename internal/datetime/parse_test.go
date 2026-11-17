// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime_test

import (
	"math"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma/internal/datetime"
)

func TestParseFormattedRoundTrip(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, loc := range []*time.Location{time.UTC, time.FixedZone("Test", 19800), loc} {
		zone := datetime.LocationZone(loc)
		values := []float64{-8.64e15, 8.64e15, -62198755200000, -62167219200000, 253402300800000, 1615705199000, 1615705200000, 1636264799000, 1636264800000}
		// A month at a time with changing weekdays and times of day.
		for month := time.January; month <= time.December; month++ {
			values = append(values, float64(time.Date(2020, month, int(month), int(month), 34, 56, 0, time.UTC).UnixMilli()))
		}
		for _, tv := range values {
			for _, s := range []string{datetime.UTCString(tv), datetime.String(tv, zone)} {
				if got := datetime.Parse(s, zone); got != tv {
					t.Errorf("%s: Parse(%q) = %v, want %v", loc, s, got, tv)
				}
			}
		}
	}
}

func TestParseFormattedFallback(t *testing.T) {
	zone := datetime.LocationZone(time.FixedZone("Test", 9*3600))
	for _, tc := range []struct{ input, equivalent string }{
		{
			input:      "Thu, 32 Jan 0005 00:00:00 GMT",
			equivalent: "2032-01-05T00:00:00Z",
		},
		{
			input:      "thu, 02 jAn 2020 03:04:05 gmt",
			equivalent: "2020-01-02T03:04:05Z",
		},
		{
			input:      "Thursday, 02 January 2020 03:04:05 GMT",
			equivalent: "2020-01-02T03:04:05Z",
		},
		{
			input:      "Thu Jan 02 2020 03:04:05 GMT+0900 (outer (inner)) ",
			equivalent: "2020-01-01T18:04:05Z",
		},
		{
			input:      "Thu Jan 02 2020 03:04:05",
			equivalent: "2020-01-02T03:04:05",
		},
		{
			input:      "Thu, 02 Jan 20 03:04:05 GMT",
			equivalent: "2020-01-02T03:04:05Z",
		},
		{
			input:      "Thu Jan 02 2020 03:04:05 GMT+01:30",
			equivalent: "2020-01-02T01:34:05Z",
		},
		{
			input:      "Thu, 02 Jan 2020 03:04:05.123 GMT",
			equivalent: "2020-01-02T03:04:05.123Z",
		},
		{
			input:      "Thu, 31 Feb 2020 24:00:00 GMT",
			equivalent: "2020-03-03T00:00:00Z",
		},
		{
			input:      "Thu Jan 02 2020 03:04:05 GMT+2460",
			equivalent: "2020-01-01T02:04:05Z",
		},
	} {
		if got, want := datetime.Parse(tc.input, zone), datetime.Parse(tc.equivalent, zone); got != want {
			t.Errorf("Parse(%q) = %v, want %v", tc.input, got, want)
		}
	}
	for _, s := range []string{
		"Thu, 02 Jan 2020 03:04:05 GMT garbage",
		"Thu Jan 02 2020 03:04:05 GMT+09000",
		"Thu Jan 02 2020 03:04:05 GMT+0900 (unclosed",
		"Thu, 02 Jan 2020 24:00:01 GMT",
		"Thu, 02 Jan 2020 03:60:00 GMT",
		"Thu, 02 Jan 2020 03:04:60 GMT",
		"Thu, 00 Jan 2020 03:04:05 GMT",
		"Thu, 02 Xxx 2020 03:04:05 GMT",
		"Sat, 13 Sep 275760 00:00:01 GMT",
	} {
		if got := datetime.Parse(s, zone); !math.IsNaN(got) {
			t.Errorf("Parse(%q) = %v, want NaN", s, got)
		}
	}
}

func TestParseSpaceSeparated(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	zone := datetime.LocationZone(loc)
	for _, tc := range []struct{ input, equivalent string }{
		{
			input:      "2021-3-14 2:30",
			equivalent: "2021-03-14T02:30:00",
		},
		{
			input:      "-000001-1-2 3:4:5.678+0130",
			equivalent: "-000001-01-02T03:04:05.678+01:30",
		},
		{
			input:      "1997-3-8 1:1:1-07",
			equivalent: "1997-03-08T01:01:01-07:00",
		},
		{
			input:      "1997-3-8 1:1Z",
			equivalent: "1997-03-08T01:01:00Z",
		},
	} {
		if got, want := datetime.Parse(tc.input, zone), datetime.Parse(tc.equivalent, zone); got != want {
			t.Errorf("Parse(%q) = %v, want %v", tc.input, got, want)
		}
	}
	for _, s := range []string{
		"1997-003-8 1:1",
		"1997-3-008 1:1",
		"1997-3 1:1",
		"1997-3-8 1:001",
		"1997-3-8 1:1+0160",
	} {
		if got := datetime.Parse(s, zone); !math.IsNaN(got) {
			t.Errorf("Parse(%q) = %v, want NaN", s, got)
		}
	}
}
