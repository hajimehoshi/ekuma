// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

// zoneType is a local time type of a time zone.
type zoneType struct {
	// offset is the offset from UTC in seconds.
	offset int32
	abbr   string
}

// daylightSavingZone returns a time zone that is std except from start until end, two instants
// in Unix seconds, when it is dst. The data is built here so that the test does not depend on
// the time zone database of the machine.
func daylightSavingZone(t *testing.T, std, dst zoneType, start, end uint32) *time.Location {
	t.Helper()
	be := binary.BigEndian
	var b []byte
	b = append(b, "TZif"...)
	b = append(b, make([]byte, 16)...)
	// The counts of UT indicators, standard indicators, leap seconds, transitions, types and abbreviation bytes.
	for _, n := range []uint32{0, 0, 0, 2, 2, uint32(len(std.abbr) + len(dst.abbr) + 2)} {
		b = be.AppendUint32(b, n)
	}
	b = be.AppendUint32(b, start)
	b = be.AppendUint32(b, end)
	b = append(b, 1, 0)
	b = be.AppendUint32(b, uint32(std.offset))
	b = append(b, 0, 0)
	b = be.AppendUint32(b, uint32(dst.offset))
	b = append(b, 1, byte(len(std.abbr)+1))
	b = append(b, std.abbr+"\x00"+dst.abbr+"\x00"...)
	loc, err := time.LoadLocationFromTZData("Test/"+std.abbr, b)
	if err != nil {
		t.Fatalf("LoadLocationFromTZData: %v", err)
	}
	return loc
}

// TestDateOffsetTransitions checks local times at the transitions of time zones:
// a skipped local time takes the offset before the transition, a repeated one is the
// earlier instant, and the offset stays defined at the limits of the time values.
func TestDateOffsetTransitions(t *testing.T) {
	vm := ekuma.NewVM()
	// Central Europe in 2021.
	centralEurope := daylightSavingZone(t, zoneType{offset: 1 * 3600, abbr: "CET"}, zoneType{offset: 2 * 3600, abbr: "CEST"},
		1616893200, // 2021-03-28T01:00:00Z
		1635642000, // 2021-10-31T01:00:00Z
	)
	// Lord Howe Island from 2020 to 2021, whose daylight saving time shifts by 30 minutes.
	lordHowe := daylightSavingZone(t, zoneType{offset: 10*3600 + 30*60, abbr: "+1030"}, zoneType{offset: 11 * 3600, abbr: "+11"},
		1601739000, // 2020-10-03T15:30:00Z
		1617462000, // 2021-04-03T15:00:00Z
	)
	// Cuba in 2021, whose transitions are at local midnight.
	cuba := daylightSavingZone(t, zoneType{offset: -5 * 3600, abbr: "CST"}, zoneType{offset: -4 * 3600, abbr: "CDT"},
		1615698000, // 2021-03-14T05:00:00Z
		1636261200, // 2021-11-07T05:00:00Z
	)
	tests := []struct {
		zone *time.Location
		src  string
		want string
	}{
		{
			zone: centralEurope,
			src:  `new Date(2021, 2, 28, 2, 30).toISOString()`,
			want: "2021-03-28T01:30:00.000Z",
		},
		{
			zone: centralEurope,
			src:  `new Date("2021-03-28T02:30").getHours()`,
			want: "3",
		},
		{
			zone: centralEurope,
			src:  `new Date(2021, 9, 31, 2, 30).toISOString()`,
			want: "2021-10-31T00:30:00.000Z",
		},
		{
			zone: centralEurope,
			src:  `new Date(Date.UTC(2021, 9, 31, 1, 30)).toString()`,
			want: "Sun Oct 31 2021 02:30:00 GMT+0100 (CET)",
		},
		{
			zone: centralEurope,
			src:  `var d = new Date(Date.UTC(2021, 9, 31, 1, 30)); Date.parse(d.toString()) === d.getTime()`,
			want: "true",
		},
		{
			zone: centralEurope,
			src:  `[new Date(-8.64e15).getTimezoneOffset(), new Date(8.64e15).getTimezoneOffset()].join()`,
			want: "-60,-60",
		},
		{
			zone: lordHowe,
			src:  `new Date(2020, 9, 4, 2, 15).toISOString()`,
			want: "2020-10-03T15:45:00.000Z",
		},
		{
			zone: lordHowe,
			src:  `new Date(2021, 3, 4, 1, 45).toISOString()`,
			want: "2021-04-03T14:45:00.000Z",
		},
		{
			zone: cuba,
			src:  `new Date(2021, 2, 14).toISOString()`,
			want: "2021-03-14T05:00:00.000Z",
		},
		{
			zone: cuba,
			src:  `new Date(2021, 10, 7).toISOString()`,
			want: "2021-11-07T04:00:00.000Z",
		},
	}
	for _, tc := range tests {
		ekuma.SetTimeZone(vm, tc.zone)
		v, err := vm.RunScript(t.Context(), "date", tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.src, err)
			continue
		}
		if got := toString(t, vm, v); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.src, got, tc.want)
		}
	}
}
