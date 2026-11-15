// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"fmt"
	"strconv"
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

// The scripts map a time value to a result. localTimeSrc reads the UTC components of its
// argument as a local date and time. Both truncate offsets to whole minutes as Ekuma's offsets
// are, which differs from the host only for local mean time.
const (
	timezoneOffsetSrc = `function (t) { return Math.trunc(new Date(t).getTimezoneOffset()); }`
	localTimeSrc      = `function (t) {
	var d = new Date(t);
	var u = new Date(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate(), d.getUTCHours(), d.getUTCMinutes()).getTime();
	return t + Math.trunc((u - t) / 60000) * 60000;
}`
)

// compareWithHost runs f, a JavaScript function expression, for each of values both in vm and
// in the JavaScript host, reports the results that differ, and returns the host's results.
func compareWithHost(t *testing.T, vm *ekuma.VM, f string, values []float64) []string {
	t.Helper()
	var src strings.Builder
	src.WriteString("[")
	for i, v := range values {
		if i > 0 {
			src.WriteString(",")
		}
		src.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
	}
	fmt.Fprintf(&src, "].map(%s).join()", f)

	v, err := runScript(t.Context(), vm, "date", src.String())
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	got := strings.Split(v.String(), ",")
	want := strings.Split(js.Global().Call("eval", src.String()).String(), ",")
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d", len(got), len(want))
	}
	var n int
	for i := range got {
		if got[i] == want[i] {
			continue
		}
		if n++; n > 10 {
			t.Errorf("more results differ")
			break
		}
		t.Errorf("%s with %s (%s): got %s, want %s", f, strconv.FormatFloat(values[i], 'f', -1, 64), time.UnixMilli(int64(values[i])).UTC().Format(time.RFC3339), got[i], want[i])
	}
	return want
}

// TestDateHostTimeZone compares local time with the Date of the JavaScript host in the
// host's time zone, at instants over a year and around the instants where the offset changes.
func TestDateHostTimeZone(t *testing.T) {
	var vm ekuma.VM
	const (
		year   = 2025
		minute = 60 * 1000
		// step is an odd number of minutes, so the scan meets every minute of the hour, and
		// shorter than any period of one offset.
		step = 97 * minute
	)
	start := float64(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	end := float64(time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	var scan []float64
	for v := start; v < end; v += step {
		scan = append(scan, v)
	}

	offsets := compareWithHost(t, &vm, timezoneOffsetSrc, scan)
	// Around each change of the offset, the instants a minute apart and the local times a minute
	// apart from two hours before to two hours after the change.
	var instants, localTimes []float64
	for i := 1; i < len(scan); i++ {
		if offsets[i] == offsets[i-1] {
			continue
		}
		if len(instants) == 0 {
			t.Logf("the host's offset changes from %s to %s minutes between %s and %s", offsets[i-1], offsets[i], time.UnixMilli(int64(scan[i-1])).UTC(), time.UnixMilli(int64(scan[i])).UTC())
		}
		for v := scan[i-1]; v <= scan[i]; v += minute {
			instants = append(instants, v)
		}
		before, err := strconv.ParseFloat(offsets[i-1], 64)
		if err != nil {
			t.Fatalf("ParseFloat: %v", err)
		}
		after, err := strconv.ParseFloat(offsets[i], 64)
		if err != nil {
			t.Fatalf("ParseFloat: %v", err)
		}
		for v := scan[i-1] - before*minute - 120*minute; v <= scan[i]-after*minute+120*minute; v += minute {
			localTimes = append(localTimes, v)
		}
	}
	if len(instants) == 0 {
		t.Logf("the host's offset does not change in %d, so daylight saving time is not tested", year)
	}

	// Instants before 1970 and at the limits of the time values.
	far := []float64{
		-8.64e15,
		float64(time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()),
		float64(time.Date(1960, 7, 1, 12, 0, 0, 0, time.UTC).UnixMilli()),
		8.64e15,
	}
	compareWithHost(t, &vm, timezoneOffsetSrc, append(instants, far...))
	compareWithHost(t, &vm, localTimeSrc, append(append(localTimes, scan...), far...))
}
