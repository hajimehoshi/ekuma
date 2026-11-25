// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

func runDateIntervals(t *testing.T, root string, failures map[string]map[int]string) {
	const fieldTests = "cdateintervalformattest.c"
	t.Run(fieldTests, func(t *testing.T) {
		path := filepath.Join(root, "icu4c/source/test/cintltst", fieldTests)
		s := newIntervalSuite(t, path, failures)
		src := readIntervalSource(t, path)
		dates := intervalDates(src)
		s.checkZonedIntervals(src, dates)
		s.checkMinutePositions(src)
		s.checkGermanParts(src, dates)
	})
	const formatTests = "dtifmtts.cpp"
	t.Run(formatTests, func(t *testing.T) {
		path := filepath.Join(root, "icu4c/source/test/intltest", formatTests)
		s := newIntervalSuite(t, path, failures)
		src := readIntervalSource(t, path)
		header := readIntervalSource(t, filepath.Join(root, "icu4c/source/i18n/unicode/udat.h"))
		s.checkSkeletonIntervals(src, header)
		s.checkMilliseconds(src)
		s.checkHourCycles(src)
		s.checkCalendarIntervals(src)
		s.checkEnglishParts(src)
		s.checkSpanIdentity(src)
		s.countIntervalExclusions(src)
	})
	const javaFile = "JavaTimeFormatTest.java"
	t.Run(javaFile, func(t *testing.T) {
		path := filepath.Join(root, "icu4j/main/core/src/test/java/com/ibm/icu/dev/test/format", javaFile)
		s := newIntervalSuite(t, path, failures)
		s.checkFrenchDateRanges(readIntervalSource(t, path))
	})
}

const intervalPacific = "America/Los_Angeles"

type intervalInput struct {
	locale, skeleton, zone string
	start, end             int64
}

func intervalOptions(in intervalInput) (map[string]any, string) {
	if in.locale == "root" {
		return nil, "ICU root locale"
	}
	lang, _, _ := strings.Cut(in.locale, "-")
	calendar := "gregory"
	switch {
	case strings.Contains(in.locale, "-ca-japanese"):
		calendar = "japanese"
	case strings.Contains(in.locale, "-ca-roc"):
		calendar = "roc"
	case lang == "th":
		calendar = "buddhist"
	}
	opts := map[string]any{"timeZone": in.zone, "calendar": calendar}
	for skel := in.skeleton; len(skel) != 0; {
		ch := skel[0]
		n := 1
		for n < len(skel) && skel[n] == ch {
			n++
		}
		skel = skel[n:]
		width := "numeric"
		if n == 2 {
			width = "2-digit"
		}
		name := "short"
		switch n {
		case 4:
			name = "long"
		case 5:
			name = "narrow"
		}
		switch ch {
		case 'y':
			if n > 2 {
				return nil, "minimum year width above two digits"
			}
			opts["year"] = width
		case 'M':
			if n > 2 {
				width = name
			}
			opts["month"] = width
		case 'd':
			opts["day"] = width
		case 'E', 'e':
			opts["weekday"] = name
		case 'G':
			opts["era"] = name
		case 'H', 'h', 'K', 'k', 'j', 'J', 'C':
			cycle := intervalHourCycle(in.locale, ch)
			hour := "numeric"
			if cycle == "h23" || cycle == "h24" {
				if lang != "ja" {
					hour = "2-digit"
				}
			} else if ch == 'J' {
				return nil, "12-hour clock without a day period"
			} else if n > 2 {
				return nil, "explicit AM/PM width"
			}
			if ch == 'C' && lang == "hi" {
				opts["dayPeriod"] = "short"
			}
			opts["hourCycle"], opts["hour"] = cycle, hour
		case 'a':
			if n > 3 {
				return nil, "explicit AM/PM width"
			}
		case 'b':
			return nil, "noon/midnight day period"
		case 'B':
			opts["dayPeriod"] = name
		case 'm':
			opts["minute"] = width
		case 's':
			opts["second"] = width
		case 'S':
			opts["fractionalSecondDigits"] = n
		case 'z':
			opts["timeZoneName"] = name
		case 'v':
			opts["timeZoneName"] = name + "Generic"
		default:
			panic(fmt.Sprintf("icutest: unexpected ICU interval skeleton %q", in.skeleton))
		}
	}
	return opts, ""
}

// ICU chooses a clock family from the skeleton, then applies a compatible locale override.
func intervalHourCycle(locale string, ch byte) string {
	lang, _, _ := strings.Cut(locale, "-")
	_, override, _ := strings.Cut(locale, "-hc-")
	use24 := lang == "de"
	switch ch {
	case 'H', 'k':
		use24 = true
	case 'h', 'K':
		use24 = false
	case 'j', 'J':
		switch override {
		case "h11", "h12", "h23", "h24":
			return override
		}
	}
	if use24 {
		if override == "h24" {
			return "h24"
		}
		return "h23"
	}
	if override == "h11" || lang == "ja" {
		return "h11"
	}
	return "h12"
}

type intervalSuite struct {
	*formatFixture
	format  ekuma.Value
	pacific *time.Location
}

func newIntervalSuite(t *testing.T, path string, failures map[string]map[int]string) *intervalSuite {
	f := newFormatFixture(t, path)
	s := &intervalSuite{
		formatFixture: f,
	}
	var err error
	s.pacific, err = time.LoadLocation(intervalPacific)
	if err != nil {
		t.Fatal(err)
	}
	f.trackOperations(failures)
	s.format = f.function(`(method, locale, options, start, end) => {
		const fmt = new Intl.DateTimeFormat(locale, options);
		if (method.split("/")[0] === "formatRange") return fmt.formatRange(start, end);
		const parts = fmt.formatRangeToParts(start, end);
		if (method === "sources") return [...new Set(parts.map(p => p.source))].sort().join(" ");
		if (method === "minutePosition") {
			let offset = 0;
			for (const part of parts) {
				if (part.type === "minute") return [offset, offset + part.value.length, part.value].join(":");
				offset += part.value.length;
			}
			return "0:0:";
		}
		if (method === "startSpan") {
			let offset = 0;
			const spans = [];
			for (const part of parts) {
				const end = offset + part.value.length;
				if (part.source === "startRange") {
					const last = spans.at(-1);
					if (last && last[1] === offset) last[1] = end;
					else spans.push([offset, end]);
				}
				offset = end;
			}
			return spans.map(span => span.join(":")).join(",");
		}
		return parts.map(p => [p.source, p.type, p.value].join(" ")).join("\n");
	}`)
	return s
}

func (s *intervalSuite) checkInterval(line int, group, op string, in intervalInput, want string) {
	opts, reason := intervalOptions(in)
	if reason != "" {
		s.excluded[reason]++
		return
	}
	if entries := s.failures[op]; entries[line] == "web-spaces" {
		known := strings.ReplaceAll(want, "\u202f", " ")
		if lang, _, _ := strings.Cut(in.locale, "-"); lang == "en" {
			known = strings.ReplaceAll(known, "\u2009–\u2009", " – ")
		}
		entries[line] = known
	}
	s.check(line, op, s.format, want, op, in.locale, opts, in.start, in.end, group+" "+in.skeleton)
}

func (s *intervalSuite) checkFrenchDateRanges(src intervalSource) {
	block := src.from("void testDateIntervalFormat()")
	in := intervalInput{
		locale:   "fr",
		skeleton: intervalString(s.t, block.from("String intervalSkeleton =").lines[0]),
		zone:     "UTC",
	}
	for i, line := range block.lines {
		switch {
		case strings.Contains(line, "= LocalDate.of("):
			assignment, args, _ := strings.Cut(line, "= LocalDate.of(")
			row := intervalRow{
				line: block.line + i,
				text: strings.TrimSuffix(args, ");"),
			}
			c := row.columns(s.t, 3)
			month := fixtureMonth(s.t, strings.TrimPrefix(c[1], "Month."))
			ms := time.Date(integer(c[0]), time.Month(month), integer(c[2]), 0, 0, 0, 0, time.UTC).UnixMilli()
			if strings.HasSuffix(assignment, "from ") {
				in.start = ms
			} else {
				in.end = ms
			}
		case strings.HasPrefix(line, "di.format(from, to,"):
			s.checkInterval(block.line+i-1, "testDateIntervalFormat", "formatRange", in, intervalString(s.t, block.lines[i-1]))
		case strings.HasPrefix(line, "LocalDateTime fromDt"):
			return
		}
	}
	s.t.Fatalf("%s:%d: missing end of LocalDate cases", src.path, block.line)
}
