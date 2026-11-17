// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime_test

import (
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma/internal/datetime"
)

// BenchmarkParse parses one fixed corpus per iteration.
func BenchmarkParse(b *testing.B) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		b.Fatal(err)
	}
	zone := datetime.LocationZone(loc)
	times := []float64{-8.64e15, -62198755200000, -62167219200000, -1e12, 0, 951868799000, 1615705199000, 1636264799000, 253402300800000, 8.64e15}
	corpora := map[string][]string{
		"ISO": {}, "UTC": {}, "Local": {},
		"Legacy":  {"Thursday January 2 2020 03:04:05 Z", "2 feb. 99 3:04 PM", "2020/1/2 03:04:05", "dEcEmBeR 31 1999 23:59:59 GMT-05:00", "Jan 2 2020 (nested (comment))"},
		"Invalid": {"Invalid Date", "Thu Jan 32 2020 03:04:05 GMT+0900", "Thu, 02 Jan 2020 25:00:00 GMT", "Jan Feb 2 2020", ""},
	}
	for _, tv := range times {
		corpora["ISO"] = append(corpora["ISO"], datetime.ISOString(tv))
		corpora["UTC"] = append(corpora["UTC"], datetime.UTCString(tv))
		corpora["Local"] = append(corpora["Local"], datetime.String(tv, zone))
	}
	for _, name := range []string{"ISO", "UTC", "Local", "Legacy", "Invalid"} {
		b.Run(name, func(b *testing.B) {
			inputs := corpora[name]
			b.ReportAllocs()
			for b.Loop() {
				for _, s := range inputs {
					datetime.Parse(s, zone)
				}
			}
		})
	}
}

func BenchmarkDateFormat(b *testing.B) {
	zone := datetime.LocationZone(time.FixedZone("Test", 9*3600))
	for b.Loop() {
		datetime.UTCString(1615705199000)
		datetime.String(1615705199000, zone)
	}
}
