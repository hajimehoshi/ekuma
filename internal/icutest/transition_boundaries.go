// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"encoding/hex"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ICU's trans vector contains signed 32-bit epoch seconds. The type map
// also includes the transitions before that range, each stored as two words.
func boundaryOffsets(src intervalSource, zone string, at time.Time) (time.Duration, time.Duration) {
	block := src.from("/* " + zone + " */ :table {")
	vectors := map[string][]string{}
	var types []byte
	for _, line := range block.lines[1:] {
		if line == "}" {
			break
		}
		key, value, ok := strings.Cut(line, " { ")
		if !ok {
			continue
		}
		value = strings.TrimSuffix(value, " }")
		switch key {
		case "trans:intvector", "transPre32:intvector", "typeOffsets:intvector":
			vectors[key] = strings.Split(value, ", ")
		case "typeMap:bin":
			var err error
			types, err = hex.DecodeString(strings.Trim(value, `"`))
			if err != nil {
				src.t.Fatalf("%s:%d: %s: %v", src.path, block.line, zone, err)
			}
		}
	}
	index := slices.Index(vectors["trans:intvector"], strconv.FormatInt(at.Unix(), 10))
	if index < 0 {
		src.t.Fatalf("%s:%d: %s has no explicit transition at %v", src.path, block.line, zone, at)
	}
	index += len(vectors["transPre32:intvector"]) / 2
	offset := func(i int) time.Duration {
		offsets := vectors["typeOffsets:intvector"]
		if i < 0 || i >= len(types) || 2*int(types[i])+1 >= len(offsets) {
			src.t.Fatalf("%s:%d: %s: invalid offset type at transition %d", src.path, block.line, zone, i)
		}
		raw := int(types[i]) * 2
		return time.Duration(integer(offsets[raw])+integer(offsets[raw+1])) * time.Second
	}
	return offset(index - 1), offset(index)
}

var zoneMapping = regexp.MustCompile(`verifyMapping\(\*(tempcal|utccal), (\d+), Calendar::([A-Z]+), (\d+),\s*(\d+), (\d+)\.0\);`)
var stepwiseCall = regexp.MustCompile(`findBoundariesStepwise\((\d+), ONE_DAY, zone, (\d+)\);`)

func (s *transitionSuite) boundaries(src, rules intervalSource) {
	version := rules.from("TZVersion {")
	if got := intervalString(s.t, version.lines[0]); got != "2026e" {
		s.t.Fatalf("%s:%d: ICU tzdata %s, want 2026e", rules.path, version.line, got)
	}
	const pacific = "America/Los_Angeles" // Target of ICU's PST link in icuzones.
	block := src.from("TimeZoneBoundaryTest::TestBoundaries()")
	var mappings int
	for i, line := range block.lines {
		if !strings.HasPrefix(line, "verifyMapping(") {
			continue
		}
		m := zoneMapping.FindStringSubmatch(line)
		if m == nil {
			s.t.Fatalf("%s:%d: malformed mapping %s", src.path, block.line+i, line)
		}
		zone := pacific
		if m[1] == "utccal" {
			zone = "UTC"
		}
		month, err := time.Parse("January", m[3])
		if err != nil {
			s.t.Fatalf("%s:%d: %v", src.path, block.line+i, err)
		}
		wall := time.Date(integer(m[2]), month.Month(), integer(m[4]), integer(m[5]), 0, 0, 0, time.UTC)
		at := time.Unix(int64(integer(m[6]))*3600, 0).UTC()
		s.instant(block.line+i, zone, at, wall.Sub(at))
		s.check(block.line+i, "Temporal.ZonedDateTime.from", s.from, at.Format(time.RFC3339Nano), zone, wall.Format(wallLayout), "compatible")
		mappings++
	}
	if mappings == 0 {
		s.t.Fatalf("%s:%d: missing verifyMapping calls", src.path, block.line)
	}
	checkBoundary := func(row intervalSource, zone string) {
		ms, err := strconv.ParseInt(zoneMillis.FindString(row.lines[0]), 10, 64)
		if err != nil {
			s.t.Fatalf("%s:%d: %v", src.path, row.line, err)
		}
		at := time.UnixMilli(ms).UTC()
		before, after := boundaryOffsets(rules, zone, at)
		s.boundary(row.line, zone, at, before, after)
	}
	checkBoundary(src.from("::PST_1997_BEG ="), pacific)
	checkBoundary(src.from("::PST_1997_END ="), pacific)
	adelaide := block.from(`TimeZone* z = TimeZone::createTimeZone("Australia/Adelaide")`)
	adelaideZone := intervalString(s.t, adelaide.lines[0])
	checkBoundary(adelaide.from("findDaylightBoundaryUsingTimeZone(date(97, 0, 1)"), adelaideZone)
	checkBoundary(adelaide.from("findDaylightBoundaryUsingTimeZone(date(97, 6, 1)"), adelaideZone)

	custom := block.from("TimeZoneBoundaryTest::TestNewRules()")
	for _, line := range custom.lines {
		if strings.HasPrefix(line, "testUsingBinarySearch(tz,") {
			s.excluded["boundary requires an ICU SimpleTimeZone"]++
		}
	}
	var zone string
	stepwise := block.from("TimeZoneBoundaryTest::TestStepwise()")
	for i, line := range stepwise.lines {
		if strings.Contains(line, "TimeZone::createTimeZone(") {
			zone = intervalString(s.t, line)
		}
		if strings.HasPrefix(line, "findBoundariesStepwise(") {
			m := stepwiseCall.FindStringSubmatch(line)
			if m == nil || zone == "" {
				s.t.Fatalf("%s:%d: malformed stepwise case %s", src.path, stepwise.line+i, line)
			}
			s.transitionCount(stepwise.line+i, zone, integer(m[1]), m[2])
		}
	}
}

func (s *transitionSuite) transitionCount(line int, zone string, year int, want string) {
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	s.check(line, "getTimeZoneTransition(next)", s.count, want, zone, start.Format(time.RFC3339), start.AddDate(1, 0, 0).Format(time.RFC3339))
}

func (s *transitionSuite) javaStepwise(src intervalSource) {
	// The other named-zone cases also appear in the C++ boundary fixture.
	row := src.from("void TestStepwise()").from(`safeGetTimeZone("America/Phoenix")`)
	m := regexp.MustCompile(`(\d+), ONE_DAY, safeGetTimeZone\("[^"]+"\), (\d+)\);`).FindStringSubmatch(row.lines[0])
	if m == nil {
		s.t.Fatalf("%s:%d: malformed stepwise case %s", src.path, row.line, row.lines[0])
	}
	s.transitionCount(row.line, intervalString(s.t, row.lines[0]), integer(m[1]), m[2])
}

func (s *transitionSuite) historicalOffsets(src intervalSource) {
	for _, row := range src.from("::TestHistorical()").table("} DATA[]") {
		c := row.columns(s.t, 3)
		if c[0] == "nullptr" {
			break
		}
		at := time.Unix(int64(integer(c[1])), 0).UTC()
		offset := time.Duration(integer(strings.TrimSuffix(c[2], "*H"))) * time.Hour
		s.instant(row.line, intervalString(s.t, c[0]), at, offset)
	}
}
