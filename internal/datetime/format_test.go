// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime_test

import (
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma/internal/datetime"
)

func BenchmarkDateConversion(b *testing.B) {
	zone := datetime.LocationZone(time.FixedZone("Test", -5*3600))
	values := [...]float64{-8.64e15, -62167219200000, -1, 0, 951782400000, 1700000000123, 253402300800000, 8.64e15}
	for _, name := range []string{"ISO", "UTC", "Local", "LocaleDate", "Month", "Date"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				v := values[i%len(values)]
				switch name {
				case "Month":
					datetime.MonthFromTime(v)
				case "Date":
					datetime.DateFromTime(v)
				case "ISO":
					datetime.ISOString(v)
				case "UTC":
					datetime.UTCString(v)
				case "Local":
					offset, name := zone.Offset(v)
					datetime.String(v, offset, name)
				case "LocaleDate":
					datetime.LocaleDateString(v, zone)
				}
			}
		})
	}
}

func TestFormatCalendar(t *testing.T) {
	zone := datetime.LocationZone(time.UTC)
	for _, tc := range []struct {
		iso    string
		utc    string
		local  string
		locale string
	}{
		{
			iso:    "-271821-04-20T00:00:00.000Z",
			utc:    "Tue, 20 Apr -271821 00:00:00 GMT",
			local:  "Tue Apr 20 -271821",
			locale: "4/20/-271821",
		},
		{
			iso:    "-000001-12-31T00:00:00.000Z",
			utc:    "Fri, 31 Dec -0001 00:00:00 GMT",
			local:  "Fri Dec 31 -0001",
			locale: "12/31/-1",
		},
		{
			iso:    "0000-02-29T00:00:00.000Z",
			utc:    "Tue, 29 Feb 0000 00:00:00 GMT",
			local:  "Tue Feb 29 0000",
			locale: "2/29/0",
		},
		{
			iso:    "1900-03-01T00:00:00.000Z",
			utc:    "Thu, 01 Mar 1900 00:00:00 GMT",
			local:  "Thu Mar 01 1900",
			locale: "3/1/1900",
		},
		{
			iso:    "+010000-01-01T00:00:00.000Z",
			utc:    "Sat, 01 Jan 10000 00:00:00 GMT",
			local:  "Sat Jan 01 10000",
			locale: "1/1/10000",
		},
		{
			iso:    "+275760-09-13T00:00:00.000Z",
			utc:    "Sat, 13 Sep 275760 00:00:00 GMT",
			local:  "Sat Sep 13 275760",
			locale: "9/13/275760",
		},
	} {
		t.Run(tc.iso, func(t *testing.T) {
			tv := datetime.Parse(tc.iso, zone)
			if got := datetime.ISOString(tv); got != tc.iso {
				t.Errorf("ISOString: got %q, want %q", got, tc.iso)
			}
			if got := datetime.UTCString(tv); got != tc.utc {
				t.Errorf("UTCString: got %q, want %q", got, tc.utc)
			}
			if got := datetime.DateString(tv, zone); got != tc.local {
				t.Errorf("DateString: got %q, want %q", got, tc.local)
			}
			if got := datetime.LocaleDateString(tv, zone); got != tc.locale {
				t.Errorf("LocaleDateString: got %q, want %q", got, tc.locale)
			}
		})
	}
}
