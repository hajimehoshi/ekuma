// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// This program generates chinaprojection.bin from lunar-go's astronomical
// new moons and solar terms. Run it with go generate in the calendar package.
package main

import (
	"encoding/binary"
	"log"
	"os"
	"time"

	"github.com/6tail/lunar-go/calendar"
)

// The first projected year follows the ICU4X table. The last ends exactly
// at the mean calendar's new year in 8486. This is the last such handoff
// before the projection has a new year earlier than January 19 in 8578.
const (
	firstYear = 2103
	lastYear  = 8485
)

func julianDay(year int, month time.Month, day int) int {
	const unixEpoch = 2440588
	return int(time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Unix()/86400) + unixEpoch
}

func main() {
	var data []byte
	nextDay := julianDay(firstYear, time.February, 7)
	for year := firstYear; year <= lastYear; year++ {
		offset := nextDay - julianDay(year, time.January, 19)
		if offset < 0 || offset >= 64 {
			log.Fatalf("year %d: new year offset %d is outside the packed range", year, offset)
		}
		record := uint32(offset) << 17
		months := calendar.NewLunarYear(year).GetMonthsInYear()
		var ordinal, regular, leap int
		for e := months.Front(); e != nil; e = e.Next() {
			m := e.Value.(*calendar.LunarMonth)
			ordinal++
			if m.GetFirstJulianDay() != float64(nextDay) {
				log.Fatalf("year %d month %d: discontinuous new moon", year, ordinal)
			}
			if m.IsLeap() {
				if regular == 0 || leap != 0 || m.GetMonth() != -regular {
					log.Fatalf("year %d month %d: invalid leap month %d", year, ordinal, m.GetMonth())
				}
				leap = ordinal
			} else {
				regular++
				if m.GetMonth() != regular {
					log.Fatalf("year %d month %d: invalid regular month %d", year, ordinal, m.GetMonth())
				}
			}
			switch m.GetDayCount() {
			case 29:
			case 30:
				record |= 1 << (ordinal - 1)
			default:
				log.Fatalf("year %d month %d: invalid length %d", year, ordinal, m.GetDayCount())
			}
			nextDay += m.GetDayCount()
		}
		if regular != 12 {
			log.Fatalf("year %d: got %d regular months", year, regular)
		}
		data = binary.LittleEndian.AppendUint32(data, record|uint32(leap)<<13)
	}
	if nextDay != julianDay(lastYear+1, time.February, 14) {
		log.Fatalf("last projected year ends at unexpected Julian day %d", nextDay-1)
	}
	if err := os.WriteFile("../chinaprojection.bin", data, 0o644); err != nil {
		log.Fatal(err)
	}
}
