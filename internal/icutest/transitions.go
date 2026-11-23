// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

type transitionSuite struct {
	*formatFixture
	at, from, count ekuma.Value
}

func runTransitions(t *testing.T, root string) {
	for _, fixture := range []struct {
		path string
		run  func(*transitionSuite, intervalSource)
	}{
		{
			path: "icu4c/source/test/intltest/tzbdtest.cpp",
			run: func(s *transitionSuite, src intervalSource) {
				s.boundaries(src, readIntervalSource(s.t, filepath.Join(root, "icu4c/source/data/misc/zoneinfo64.txt")))
			},
		},
		{
			path: "icu4c/source/test/intltest/tzoffloc.cpp",
			run:  (*transitionSuite).offsetFromLocal,
		},
		{
			path: "icu4c/source/test/intltest/caltest.cpp",
			run:  (*transitionSuite).calendarWallTimes,
		},
		{
			path: "icu4c/source/test/intltest/tztest.cpp",
			run:  (*transitionSuite).historicalOffsets,
		},
		{
			path: "icu4j/main/core/src/test/java/com/ibm/icu/dev/test/timezone/TimeZoneBoundaryTest.java",
			run:  (*transitionSuite).javaStepwise,
		},
	} {
		t.Run(filepath.Base(fixture.path), func(t *testing.T) {
			if !inPinnedZoneProcess(t) {
				return
			}
			f := newFormatFixture(t, filepath.Join(root, fixture.path))
			var vm ekuma.VM
			s := transitionSuite{
				formatFixture: f,
				at: f.functionIn(&vm, `(zone, instant) => {
					const z = Temporal.Instant.from(instant).toZonedDateTimeISO(zone);
					return z.offset + " " + z.toPlainDateTime().toString();
				}`),
				from: f.functionIn(&vm, `(zone, wall, disambiguation) => {
					try {
						return Temporal.ZonedDateTime.from(wall + "[" + zone + "]", {disambiguation}).toInstant().toString();
					} catch (e) {
						if (e instanceof RangeError) return "RangeError";
						throw e;
					}
				}`),
				count: f.functionIn(&vm, `(zone, start, end) => {
					let z = Temporal.Instant.from(start).toZonedDateTimeISO(zone);
					const until = Temporal.Instant.from(end).epochNanoseconds;
					for (let n = 0; n <= 366; n++) {
						const next = z.getTimeZoneTransition("next");
						if (!next || next.epochNanoseconds >= until) return String(n);
						if (next.epochNanoseconds <= z.epochNanoseconds) throw new Error("non-advancing transition " + next);
						z = next;
					}
					throw new Error("more than 366 transitions in a year");
				}`),
			}
			fixture.run(&s, readIntervalSource(t, f.path))
		})
	}
}

// Go remembers ZONEINFO on first use and Ekuma caches named zones across VMs.
func inPinnedZoneProcess(t *testing.T) bool {
	t.Helper()
	const child = "EKUMA_ICU_TRANSITION_CHILD"
	if os.Getenv(child) == t.Name() {
		archive := os.Getenv("ZONEINFO")
		// time.LoadLocation recognizes a zip source by its filename suffix.
		if !strings.HasSuffix(archive, ".zip") {
			t.Fatalf("ZONEINFO=%q: expected a .zip path", archive)
		}
		data, err := os.ReadFile(archive)
		if err != nil {
			t.Fatal(err)
		}
		const digest = "b2d18a7c8fa8142097a48c99609fb3c92db5ee98bc740294e57eab8ae9f94779"
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != digest {
			t.Fatalf("%s: SHA-256 %s, want %s; set EKUMA_ICU_ZONEINFO to Go 1.27.4's zoneinfo.zip", archive, got, digest)
		}
		t.Logf("ZONEINFO=%s (tzdata 2026c)", archive)
		return true
	}
	archive := os.Getenv("EKUMA_ICU_ZONEINFO")
	if archive == "" {
		archive = filepath.Join(runtime.GOROOT(), "lib", "time", "zoneinfo.zip")
	}
	archive, err := filepath.Abs(archive)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	names := strings.Split(t.Name(), "/")
	for i, name := range names {
		names[i] = "^" + regexp.QuoteMeta(name) + "$"
	}
	cmd := exec.CommandContext(t.Context(), exe, "-test.run="+strings.Join(names, "/"), "-test.v")
	cmd.Env = append(os.Environ(), child+"="+t.Name(), "ZONEINFO="+archive, "TZ=UTC")
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	if err != nil {
		t.Fatalf("transition test subprocess: %v", err)
	}
	return false
}

const wallLayout = "2006-01-02T15:04:05.999999999"

func (s *transitionSuite) instant(line int, zone string, at time.Time, offset time.Duration) {
	want := at.In(time.FixedZone("", int(offset.Seconds())))
	s.check(line, "offset and local time", s.at, want.Format("-07:00 "+wallLayout), zone, at.Format(time.RFC3339Nano))
}

func (s *transitionSuite) boundary(line int, zone string, at time.Time, before, after time.Duration) {
	s.instant(line, zone, at.Add(-time.Nanosecond), before)
	s.instant(line, zone, at, after)
}

func (s *transitionSuite) wallTime(line int, zone string, wall, earlier, later time.Time, gap bool) {
	compatible := earlier
	if gap {
		compatible = later
	}
	reject := "RangeError"
	if earlier.Equal(later) {
		reject = earlier.Format(time.RFC3339Nano)
	}
	check := func(mode, want string) {
		s.check(line, "Temporal.ZonedDateTime.from", s.from, want, zone, wall.Format(wallLayout), mode)
	}
	check("earlier", earlier.Format(time.RFC3339Nano))
	check("later", later.Format(time.RFC3339Nano))
	check("compatible", compatible.Format(time.RFC3339Nano))
	check("reject", reject)
}
