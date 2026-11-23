// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"regexp"
	"strings"
	"time"
)

func fixtureHours(expr string) time.Duration {
	return time.Duration(integer(strings.TrimSuffix(expr, "*HOUR"))) * time.Hour
}

func (s *transitionSuite) offsetFromLocal(src intervalSource) {
	block := src.from("::TestGetOffsetAroundTransition()")
	zone := intervalString(s.t, block.from("TESTZONES[0] =").lines[0])
	dates := block.table(" DATES[NUM_DATES][6] =")
	standardTime := block.table(" OFFSETS1[NUM_DATES] =")
	standard := block.table(" OFFSETS2[NUM_DATES][2] =")
	daylight := block.table(" OFFSETS3[][2] =")
	if len(dates) != len(standardTime) || len(dates) != len(standard) || len(dates) != len(daylight) {
		s.t.Fatalf("%s:%d: offset tables do not match %d date rows", src.path, block.line, len(dates))
	}
	var previousOffset time.Duration
	var previousWasUnique bool
	for i, row := range dates {
		c := row.columns(s.t, 6)
		month, err := time.Parse("January", strings.TrimPrefix(c[1], "UCAL_"))
		if err != nil {
			s.t.Fatalf("%s:%d: %v", src.path, row.line, err)
		}
		wall := time.Date(integer(c[0]), month.Month(), integer(c[2]), integer(c[3]), integer(c[4]), 0, 0, time.UTC)
		std := standard[i].columns(s.t, 2)
		dst := daylight[i].columns(s.t, 2)
		raw := fixtureHours(std[0])
		// OFFSETS1 takes local standard time, even during daylight saving time.
		hours, minutes, hasMinutes := strings.Cut(c[5], "+")
		fromMidnight := fixtureHours(hours)
		if hasMinutes {
			fromMidnight += time.Duration(integer(strings.TrimSuffix(minutes, "*MINUTE"))) * time.Minute
		}
		midnight := time.Date(wall.Year(), wall.Month(), wall.Day(), 0, 0, 0, 0, time.UTC)
		s.instant(standardTime[i].line, zone, midnight.Add(fromMidnight-raw), fixtureHours(strings.TrimSuffix(standardTime[i].text, ",")))

		// The two wall-time policies select the standard and daylight offsets
		// respectively when the clock skips or repeats. Subtracting the larger
		// offset gives the earlier instant in either case.
		stdOffset := raw + fixtureHours(std[1])
		dstOffset := fixtureHours(dst[0]) + fixtureHours(dst[1])
		earlier := wall.Add(-max(stdOffset, dstOffset))
		later := wall.Add(-min(stdOffset, dstOffset))
		unique := stdOffset == dstOffset
		gap := !unique && previousOffset == min(stdOffset, dstOffset)
		s.wallTime(row.line, zone, wall, earlier, later, gap)
		// DATES samples the first skipped or repeated minute at each transition.
		if previousWasUnique && !unique {
			after := stdOffset
			if previousOffset == stdOffset {
				after = dstOffset
			}
			s.boundary(row.line, zone, later, previousOffset, after)
		}
		if unique {
			previousOffset = stdOffset
		}
		previousWasUnique = unique
	}
	s.excluded["wall-time row requires an ICU SimpleTimeZone"] += len(dates)
	s.excluded["wall-time row requires an ICU RuleBasedTimeZone"] += len(dates)
}

var calFields = regexp.MustCompile(`CalFields\((\d+),(\d+),(\d+),(\d+),(\d+),(\d+)\)`)

func (s *transitionSuite) calendarWallTimes(src intervalSource) {
	for _, table := range []string{" RPDATA[] =", " SKDATA[] ="} {
		skipped := table == " SKDATA[] ="
		var previousWall time.Time
		for _, row := range src.table(table) {
			if strings.HasPrefix(row.text, "{nullptr") {
				break
			}
			fields := calFields.FindAllStringSubmatch(row.text, -1)
			if (!skipped && len(fields) != 3) || (skipped && len(fields) != 4) {
				s.t.Fatalf("%s:%d: malformed wall-time row %s", src.path, row.line, row.text)
			}
			utc := func(c []string) time.Time {
				return time.Date(integer(c[1]), time.Month(integer(c[2])), integer(c[3]), integer(c[4]), integer(c[5]), integer(c[6]), 0, time.UTC)
			}
			zone := intervalString(s.t, row.text)
			wall, later, earlier := utc(fields[0]), utc(fields[1]), utc(fields[2])
			s.wallTime(row.line, zone, wall, earlier, later, skipped)
			// Each table gives the last unique second and the first skipped or
			// repeated second. The latter's later candidate is the transition.
			if !earlier.Equal(later) && wall.Sub(previousWall) == time.Second {
				before, after := wall.Sub(earlier), wall.Sub(later)
				if skipped {
					before, after = after, before
				}
				s.boundary(row.line, zone, later, before, after)
			}
			if earlier.Equal(later) {
				previousWall = wall
			}
			if skipped {
				s.excluded["ICU WALLTIME_NEXT_VALID"]++
			}
		}
	}
}
