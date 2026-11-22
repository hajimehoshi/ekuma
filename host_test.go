// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"math"
	"math/rand/v2"
	"strconv"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hajimehoshi/ekuma"
)

// run runs src in vm and returns the result as a string.
func run(t *testing.T, vm *ekuma.VM, src string) string {
	t.Helper()
	v, err := runScript(t.Context(), vm, "host", src)
	if err != nil {
		t.Fatalf("runScript: %v", err)
	}
	return toString(t, vm, v)
}

func TestRandomSource(t *testing.T) {
	var vm ekuma.VM
	vm.SetRandomSource(rand.NewPCG(1, 2))
	r := rand.New(rand.NewPCG(1, 2))
	for i := range 3 {
		got, err := strconv.ParseFloat(run(t, &vm, `Math.random()`), 64)
		if err != nil {
			t.Fatal(err)
		}
		if want := r.Float64(); got != want {
			t.Errorf("call %d: got %v, want %v", i, got, want)
		}
	}
}

// constSource is a [rand.Source] that returns its value.
type constSource uint64

func (s constSource) Uint64() uint64 {
	return uint64(s)
}

func TestRandomSourceBounds(t *testing.T) {
	var vm ekuma.VM
	for _, tc := range []struct {
		src  constSource
		want float64
	}{
		{
			src:  0,
			want: 0,
		},
		{
			src:  math.MaxUint64,
			want: 1 - 0x1p-53,
		},
	} {
		vm.SetRandomSource(tc.src)
		got, err := strconv.ParseFloat(run(t, &vm, `Math.random()`), 64)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("source %#x: got %v, want %v", uint64(tc.src), got, tc.want)
		}
	}
}

func TestRandomSourceNil(t *testing.T) {
	var vm ekuma.VM
	vm.SetRandomSource(constSource(0))
	vm.SetRandomSource(nil)
	// Two zeros in a row from a random source are practically impossible.
	if got := run(t, &vm, `var a = Math.random(), b = Math.random(); 0 <= a && a < 1 && 0 <= b && b < 1 && (a !== 0 || b !== 0)`); got != "true" {
		t.Errorf("got %s, want true", got)
	}
}

func TestTimeZone(t *testing.T) {
	var vm ekuma.VM
	vm.SetTimeZone(time.FixedZone("", 5*3600))
	if got, want := run(t, &vm, `new Date().getTimezoneOffset()`), "-300"; got != want {
		t.Errorf("fixed zone: got %q, want %q", got, want)
	}
	vm.SetTimeZone(nil)
	_, off := time.Now().Zone()
	if got, want := run(t, &vm, `new Date().getTimezoneOffset()`), strconv.Itoa(-off/60); got != want {
		t.Errorf("nil: got %q, want %q", got, want)
	}
}

func TestDateTimeZoneAfterArgumentConversion(t *testing.T) {
	var vm ekuma.VM
	changeTimeZone := vm.FuncOf("changeTimeZone", func(ekuma.Value, []ekuma.Value) (any, error) {
		vm.SetTimeZone(time.FixedZone("", 3600))
		return nil, nil
	})
	if err := vm.Global().Set("changeTimeZone", changeTimeZone); err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{
		`new Date({ valueOf() { changeTimeZone(); return 1970; } }, 0, 1).getTime()`,
		`new Date({ valueOf() { changeTimeZone(); return "1970-01-01T00:00:00"; } }).getTime()`,
	} {
		vm.SetTimeZone(time.UTC)
		if got, want := run(t, &vm, src), "-3600000"; got != want {
			t.Errorf("%s: got %s, want %s", src, got, want)
		}
	}
}

func TestDateTimeZoneAfterClockRead(t *testing.T) {
	var vm ekuma.VM
	vm.SetTimeZone(time.UTC)
	vm.SetClock(func() time.Time {
		vm.SetTimeZone(time.FixedZone("", 3600))
		return time.Unix(0, 0)
	})
	if got, want := run(t, &vm, `Date()`), run(t, &vm, `new Date(0).toString()`); got != want {
		t.Errorf("Date(): got %q, want %q", got, want)
	}
}

// TestTimeZoneName checks that Date, Temporal and Intl agree on a location whose name is a
// known time zone identifier with other rules.
func TestTimeZoneName(t *testing.T) {
	if _, err := time.LoadLocation("Europe/Berlin"); err != nil {
		t.Skipf("no time zone database: %v", err)
	}
	var vm ekuma.VM
	const instant = `var ms = Date.UTC(2026, 6, 1), id = Temporal.Now.timeZoneId();
		[id, new Date(ms).getTimezoneOffset(), Temporal.Instant.fromEpochMilliseconds(ms).toZonedDateTimeISO(id).offset,
			new Intl.DateTimeFormat("en", { timeZoneName: "longOffset" }).formatToParts(ms).find(p => p.type === "timeZoneName").value].join()`
	vm.SetTimeZone(time.FixedZone("Europe/Berlin", 3600))
	if got, want := run(t, &vm, instant), "Europe/Berlin,-120,+02:00,GMT+02:00"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestClock checks that Date and Temporal read the current time from the clock, the time
// value rounded down, and both clamped to the range of Date.
func TestClock(t *testing.T) {
	var vm ekuma.VM
	for _, tc := range []struct {
		now  time.Time
		want string
	}{
		{
			now:  time.Unix(0, -1),
			want: "-1 -1",
		},
		{
			now:  time.Unix(1e13, 0),
			want: "8640000000000000 8640000000000000000000",
		},
		{
			now:  time.Unix(-1e13, 0),
			want: "-8640000000000000 -8640000000000000000000",
		},
	} {
		vm.SetClock(func() time.Time {
			return tc.now
		})
		if got := run(t, &vm, `Date.now() + " " + Temporal.Now.instant().epochNanoseconds`); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.now, got, tc.want)
		}
	}
}

func TestClockNil(t *testing.T) {
	var vm ekuma.VM
	vm.SetClock(func() time.Time {
		return time.Unix(0, 0)
	})
	vm.SetClock(nil)
	before := time.Now().UnixMilli()
	got, err := strconv.ParseFloat(run(t, &vm, `Date.now()`), 64)
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now().UnixMilli()
	if got < float64(before) || got > float64(after) {
		t.Errorf("got %v, want a time in [%d, %d]", got, before, after)
	}
}

// TestClockSystemTimeZone checks that a system time zone without an identifier becomes the
// offset time zone of its offset at the time of the clock.
func TestClockSystemTimeZone(t *testing.T) {
	var vm ekuma.VM
	vm.SetTimeZone(namedDaylightSavingZone(t, "", zoneType{offset: 1 * 3600, abbr: "CET"}, zoneType{offset: 2 * 3600, abbr: "CEST"},
		1616893200, // 2021-03-28T01:00:00Z
		1635642000, // 2021-10-31T01:00:00Z
	))
	for _, tc := range []struct {
		now  time.Time
		want string
	}{
		{
			now:  time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
			want: "+01:00",
		},
		{
			now:  time.Date(2021, 7, 1, 0, 0, 0, 0, time.UTC),
			want: "+02:00",
		},
	} {
		vm.SetClock(func() time.Time {
			return tc.now
		})
		if got := run(t, &vm, `Temporal.Now.timeZoneId()`); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.now, got, tc.want)
		}
	}
}

// TestClockReads checks that Temporal.Now reads the clock once when the system time zone has
// an identifier.
func TestClockReads(t *testing.T) {
	var vm ekuma.VM
	vm.SetTimeZone(time.UTC)
	var reads int
	vm.SetClock(func() time.Time {
		reads++
		return time.Unix(0, 0)
	})
	run(t, &vm, `void Temporal.Now.plainDateTimeISO()`)
	if reads != 1 {
		t.Errorf("reads: got %d, want 1", reads)
	}
}

// TestClockAtomicsWait checks that a synchronous wait lasts until the clock passes its
// timeout, however long the real time it takes.
func TestClockAtomicsWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var ms atomic.Int64
		var vm ekuma.VM
		vm.SetCanBlock(true)
		vm.SetClock(func() time.Time {
			return time.UnixMilli(ms.Load())
		})
		start := time.Now()
		go func() {
			time.Sleep(time.Second)
			ms.Store(10)
		}()
		if got, want := run(t, &vm, `Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 10)`), "timed-out"; got != want {
			t.Errorf("Atomics.wait: got %q, want %q", got, want)
		}
		if d := time.Since(start); d < time.Second {
			t.Errorf("Atomics.wait returned after %v, before the clock reached its timeout", d)
		}
	})
}

// TestClockAtomicsWaitAsync checks that the timeout of an asynchronous wait ends when the
// clock passes it, both when the jobs run and in [ekuma.VM.AwaitJobs].
func TestClockAtomicsWaitAsync(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var ms atomic.Int64
		var vm ekuma.VM
		vm.SetClock(func() time.Time {
			return time.UnixMilli(ms.Load())
		})
		run(t, &vm, `
			var log = [];
			Atomics.waitAsync(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 10).value.then(r => log.push(r));
		`)
		// The real time passes the timeout, and the clock does not.
		time.Sleep(time.Second)
		if got, want := run(t, &vm, `log.join()`), ""; got != want {
			t.Errorf("log before the clock reached the timeout: got %q, want %q", got, want)
		}
		start := time.Now()
		go func() {
			time.Sleep(time.Second)
			ms.Store(10)
		}()
		if ok, err := vm.AwaitJobs(t.Context()); err != nil || !ok {
			t.Errorf("AwaitJobs: got %t, %v, want true, nil", ok, err)
		}
		if d := time.Since(start); d < time.Second {
			t.Errorf("AwaitJobs returned after %v, before the clock reached the timeout", d)
		}
		if got, want := run(t, &vm, `log.join()`), "timed-out"; got != want {
			t.Errorf("log: got %q, want %q", got, want)
		}
	})
}
