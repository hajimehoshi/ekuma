// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

// centralEurope2021 returns a time zone that is CET (UTC+1) except during the daylight saving
// time of 2021, when it is CEST (UTC+2). The data is built here so that the test does not
// depend on the time zone database of the machine.
func centralEurope2021(t *testing.T) *time.Location {
	t.Helper()
	be := binary.BigEndian
	var b []byte
	b = append(b, "TZif"...)
	b = append(b, make([]byte, 16)...)
	// The counts of UT indicators, standard indicators, leap seconds, transitions, types and abbreviation bytes.
	for _, n := range []uint32{0, 0, 0, 2, 2, 9} {
		b = be.AppendUint32(b, n)
	}
	b = be.AppendUint32(b, 1616893200) // 2021-03-28T01:00:00Z
	b = be.AppendUint32(b, 1635642000) // 2021-10-31T01:00:00Z
	b = append(b, 1, 0)
	b = be.AppendUint32(b, 1*3600)
	b = append(b, 0, 0)
	b = be.AppendUint32(b, 2*3600)
	b = append(b, 1, 4)
	b = append(b, "CET\x00CEST\x00"...)
	loc, err := time.LoadLocationFromTZData("Test/CentralEurope", b)
	if err != nil {
		t.Fatalf("LoadLocationFromTZData: %v", err)
	}
	return loc
}

// TestDateOffsetTransitions checks local times at the transitions of a time zone:
// a skipped local time takes the offset before the transition, a repeated one is the
// earlier instant, and the offset stays defined at the limits of the time values.
func TestDateOffsetTransitions(t *testing.T) {
	vm := ekuma.NewVM()
	ekuma.SetTimeZone(vm, centralEurope2021(t))
	tests := []struct {
		src  string
		want string
	}{
		{
			src:  `new Date(2021, 2, 28, 2, 30).toISOString()`,
			want: "2021-03-28T01:30:00.000Z",
		},
		{
			src:  `new Date("2021-03-28T02:30").getHours()`,
			want: "3",
		},
		{
			src:  `new Date(2021, 9, 31, 2, 30).toISOString()`,
			want: "2021-10-31T00:30:00.000Z",
		},
		{
			src:  `new Date(Date.UTC(2021, 9, 31, 1, 30)).toString()`,
			want: "Sun Oct 31 2021 02:30:00 GMT+0100 (CET)",
		},
		{
			src:  `var d = new Date(Date.UTC(2021, 9, 31, 1, 30)); Date.parse(d.toString()) === d.getTime()`,
			want: "true",
		},
		{
			src:  `[new Date(-8.64e15).getTimezoneOffset(), new Date(8.64e15).getTimezoneOffset()].join()`,
			want: "-60,-60",
		},
	}
	for _, tc := range tests {
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
