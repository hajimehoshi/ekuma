// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
	"github.com/hajimehoshi/ekuma/internal/intl/date"
	"github.com/hajimehoshi/ekuma/internal/temporal"
)

type datePatternSuite struct {
	*formatFixture
	observe ekuma.Value
}

func runDatePatterns(t *testing.T, root string, failures map[string]map[int]string) {
	for _, path := range []string{
		"icu4c/source/test/intltest/dtptngts.cpp",
		"icu4c/source/test/cintltst/udatpg_test.c",
		"icu4j/main/core/src/test/java/com/ibm/icu/dev/test/format/DateTimeGeneratorTest.java",
	} {
		file := filepath.Base(path)
		t.Run(file, func(t *testing.T) {
			s := &datePatternSuite{
				formatFixture: newFormatFixture(t, filepath.Join(root, path)),
			}
			s.trackOperations(failures)
			s.observe = s.function(`(locale, options, ms, skeleton, mode, fields) => {
				const fmt = new Intl.DateTimeFormat(locale, options);
				const parts = fmt.formatToParts(ms);
				const text = fmt.format(ms);
				if (parts.some(p => !p.value.length) || parts.map(p => p.value).join("") !== text)
					return "inconsistent parts: " + JSON.stringify(parts) + " for " + text;
				const resolved = fmt.resolvedOptions();
				// CreateDateTimeFormat resolves the legacy islamic option to islamic-tbla.
				const calendar = options.calendar === "islamic" ? "islamic-tbla" : options.calendar;
				if (resolved.calendar !== calendar || resolved.numberingSystem !== options.numberingSystem)
					return "resolved calendar " + resolved.calendar + ", numbering system " + resolved.numberingSystem + ": " + JSON.stringify(parts);
				switch (mode) {
				case "text": return text;
				case "fields":
					return parts.filter(p => p.type !== "literal").map(p =>
						fields.includes(p.type) ? p.type + " " + p.value : p.type
					).sort().join("\n");
				case "pattern":
					return parts.map(p => {
						let value = p.value;
						if (fields.includes(p.type)) value = resolved[p.type] || "*";
						if (p.type === "hour") value += " " + resolved.hourCycle;
						return p.type + " " + value;
					}).join("\n");
				case "includes":
					return parts.some(p => fields.includes(p.type)) ? "present" : parts.map(p => p.type).join(" ");
				case "fraction": return parts.filter(p => p.type === "fractionalSecond").map(p => p.value).join(",");
				case "hour": return parts.filter(p => p.type === "hour").map(p => Number(p.value)).join(",");
				case "periodChange": {
					const first = parts.find(p => p.type === "dayPeriod")?.value;
					const second = fmt.formatToParts(fields).find(p => p.type === "dayPeriod")?.value;
					return first && second && first !== second ? "distinct" : JSON.stringify([first, second]);
				}
				case "kind":
					if (!parts.some(p => p.type === fields) || !resolved[fields]) return JSON.stringify(parts);
					if (fields === "dayPeriod") return "flexible";
					return resolved.timeZoneName.replace(/^(short|long)/, "") || "specific";
				case "clock": {
					const hour = parts.find(p => p.type === "hour");
					return resolved.hourCycle + " " + (hour && hour.value.replace(/^[0٠](?=.)/, ""));
				}
				case "iso":
					return /├|Minute/.test(text) ? text : "localized";
				}
			}`)
			src := readIntervalSource(t, s.path)
			switch file {
			case "dtptngts.cpp":
				s.cppDatePatterns(src)
			case "udatpg_test.c":
				s.cDatePatterns(src)
			case "DateTimeGeneratorTest.java":
				s.javaDatePatterns(src)
			}
		})
	}
}

func dateNameWidth(n int) string {
	switch n {
	case 4:
		return "long"
	case 5:
		return "narrow"
	}
	return "short"
}

// datePatternOptions translates the request independently of the expected pattern.
func datePatternOptions(id, skeleton, zone string) (string, map[string]any, string) {
	loc, keywords, _ := strings.Cut(id, "@")
	loc = strings.ReplaceAll(loc, "_", "-")
	cal, numbers := "gregory", "latn"
	switch loc {
	case "root":
		return "", nil, "ICU root locale"
	case "cn-ZH", "und-IN":
		return "", nil, "ICU fallback for an unspecified or unavailable language"
	case "ja-TRADITIONAL":
		return "", nil, "legacy ICU variant outside BCP 47"
	case "ar-EG":
		numbers = "arab"
	case "ars":
		cal, numbers = "islamic-umalqura", "arab"
	case "ckb-IR":
		cal, numbers = "persian", "arab"
	case "de-AT-u-ca-iso8601", "de-CH-u-ca-iso8601":
		cal = "iso8601"
	}
	opts := map[string]any{"timeZone": zone, "numberingSystem": numbers}
	for kv := range strings.SplitSeq(keywords, ";") {
		key, val, _ := strings.Cut(kv, "=")
		switch key {
		case "":
		case "calendar":
			cal = val
		case "hours":
			opts["hourCycle"] = val
		case "rg":
			return "", nil, "ICU region override is outside DateTimeFormat's locale keys"
		default:
			panic("unexpected ICU locale keyword " + kv)
		}
	}
	opts["calendar"] = cal
	for rest := skeleton; len(rest) > 0; {
		ch := rest[0]
		n := len(rest) - len(strings.TrimLeft(rest, string(ch)))
		rest = rest[n:]
		width := "numeric"
		if n == 2 {
			width = "2-digit"
		}
		name := dateNameWidth(n)
		switch ch {
		case 'y':
			opts["year"] = width
		case 'M', 'L':
			if n > 2 {
				width = name
			}
			opts["month"] = width
		case 'd':
			if n > 2 {
				return "", nil, "ordinal day of month"
			}
			opts["day"] = width
		case 'e', 'c', 'E':
			if ch != 'E' && n < 3 {
				return "", nil, "numeric local weekday"
			}
			if n == 6 {
				return "", nil, "ICU six-letter weekday width"
			}
			opts["weekday"] = name
		case 'G':
			opts["era"] = name
		case 'h', 'K', 'H', 'k', 'j', 'J', 'C':
			switch ch {
			case 'H', 'k', 'h', 'K':
				opts["hour12"] = ch == 'h' || ch == 'K'
			case 'J':
				if strings.HasPrefix(loc, "en") || loc == "zh-TW" {
					return "", nil, "12-hour clock with suppressed day period"
				}
			case 'C':
				if n%2 == 0 {
					width = "2-digit"
				}
				switch loc {
				case "zh-TW", "gu-IN":
					opts["dayPeriod"] = []string{"short", "long", "narrow"}[(n-1)/2]
				case "en-BN":
					return "", nil, "AM/PM with noon or midnight"
				case "en":
					if n > 2 {
						return "", nil, "explicit AM/PM name width"
					}
				}
			}
			opts["hour"] = width
		case 'm':
			opts["minute"] = width
		case 's':
			opts["second"] = width
		case 'S':
			if n > 3 {
				return "", nil, "more than three fractional second digits"
			}
			opts["fractionalSecondDigits"] = n
		case 'B':
			opts["dayPeriod"] = name
		case 'a', 'b':
			return "", nil, "fixed AM/PM or noon/midnight day period"
		case 'v', 'z', 'O', 'Z':
			if ch == 'Z' && n != 4 {
				return "", nil, "RFC 822 or ISO zone offset"
			}
			if ch == 'v' {
				name += "Generic"
			} else if ch != 'z' {
				name += "Offset"
			}
			opts["timeZoneName"] = name
		case 'V', 'X', 'x':
			return "", nil, "location identifier, location name or ISO zone offset"
		case 'q', 'Q':
			return "", nil, "quarter"
		case 'Y', 'u', 'U':
			return "", nil, "explicit week-based, extended or cyclic year"
		case 'w', 'W', 'D', 'F', 'g', 'A':
			return "", nil, "week number, ordinal day, Julian day or milliseconds in day"
		default:
			panic("unknown ICU skeleton field in " + skeleton)
		}
	}
	return loc, opts, ""
}

// ICU's fixed-output matrices give strings without field positions. The
// instant identifies the numeric fields, and the text supplies their padding.
var dateDigits = regexp.MustCompile(`[0-9]+`)

func (s *datePatternSuite) fixed(line int, id, skeleton string, instant time.Time, zone, want string) {
	loc, opts, reason := datePatternOptions(id, skeleton, zone)
	if reason != "" {
		s.excluded[reason]++
		return
	}
	tz, ok := temporal.GetAvailableNamedTimeZone(zone, temporal.UTC)
	if !ok {
		s.t.Fatalf("%s:%d: unknown time zone %q", s.path, line, zone)
	}
	s.executed++
	got := date.FormatSkeleton(loc, opts["calendar"].(string), opts["numberingSystem"].(string), skeleton, tz, instant.UnixMilli())
	if got != want {
		s.t.Errorf("%s:%d: skeleton %q (%s %v, %s): got %q, want %q", s.path, line, skeleton, loc, opts, instant, got, want)
	}
	want = strings.ReplaceAll(want, "\u202f", " ")
	s.check(line, "format", s.observe, want, loc, opts, instant.UnixMilli(), skeleton, "text")
	numbers := dateDigits.FindAllString(want, -1)
	var values []any
	var fields []string
	addValue := func(key, value string) {
		fields = append(fields, key+" "+value)
		values = append(values, key)
	}
	addNumber := func(key string, n int) {
		for _, v := range numbers {
			if integer(v) == n {
				addValue(key, v)
				return
			}
		}
		s.t.Fatalf("%s:%d: %s %s: missing %s %d in %q", s.path, line, id, skeleton, key, n, want)
	}
	cal := opts["calendar"]
	english := loc == "en" || strings.HasPrefix(loc, "en-")
	// The Japanese and ROC era names and AM/PM markers are outside the
	// numeric fields in these fixtures. Chinese and Japanese put them first.
	outerName := func(prefix bool) string {
		spans := dateDigits.FindAllStringIndex(want, -1)
		if prefix {
			return strings.TrimSpace(want[:spans[0][0]])
		}
		return strings.TrimSpace(want[spans[len(spans)-1][1]:])
	}
	for _, key := range []string{"year", "month", "day", "weekday", "hour", "minute", "second", "fractionalSecondDigits"} {
		if _, ok := opts[key]; !ok {
			continue
		}
		switch key {
		case "year":
			y := instant.Year()
			switch cal {
			case "japanese":
				y -= 1988 // The matrix date falls in Heisei.
				addValue("era", outerName(!english))
			case "roc":
				y -= 1911
				addValue("era", outerName(!english))
			case "chinese":
				// January 13 precedes Chinese New Year in 1999.
				y--
				key = "relatedYear"
				if !strings.Contains(skeleton, "d") {
					fields = append(fields, "yearName")
				}
			}
			if opts["year"] == "2-digit" {
				y %= 100
			}
			addNumber(key, y)
		case "day":
			d := instant.Day()
			if cal == "chinese" {
				d = 26
			}
			addNumber(key, d)
		case "hour":
			h := instant.Hour()
			if !slices.ContainsFunc(numbers, func(v string) bool { return integer(v) == h }) || strings.Contains(skeleton, "h") {
				h = (h+11)%12 + 1
				addValue("dayPeriod", outerName(strings.HasPrefix(loc, "zh") || strings.HasPrefix(loc, "ja")))
			}
			addNumber(key, h)
		case "month":
			if english && (opts[key] == "short" || opts[key] == "long" || opts[key] == "narrow") {
				addValue(key, englishDateName(instant.Month().String(), opts[key].(string)))
			} else if (opts[key] == "numeric" || opts[key] == "2-digit") && !strings.Contains(want, "月") {
				m := int(instant.Month())
				if cal == "chinese" {
					m = 11
				}
				addNumber(key, m)
			} else {
				fields = append(fields, key)
			}
		case "weekday":
			if english {
				addValue(key, englishDateName(instant.Weekday().String(), opts[key].(string)))
			} else {
				fields = append(fields, key)
			}
		case "minute":
			addNumber(key, instant.Minute())
		case "second":
			addNumber(key, instant.Second())
		case "fractionalSecondDigits":
			addNumber("fractionalSecond", instant.Nanosecond()/1e6)
		}
	}
	slices.Sort(fields)
	s.check(line, "formatToParts/fields", s.observe, strings.Join(fields, "\n"), loc, opts, instant.UnixMilli(), skeleton, "fields", values)
}

func englishDateName(name, width string) string {
	switch width {
	case "narrow":
		return name[:1]
	case "short":
		return name[:3]
	}
	return name
}

// A single-digit month, day, hour, minute and second distinguish padding.
// The year and fraction distinguish truncation from numeric formatting.
var patternInstant = time.Date(2006, time.March, 4, 1, 5, 7, 891e6, time.UTC)

func patternFraction(n int) string {
	return fmt.Sprintf("%03d", patternInstant.Nanosecond()/1e6)[:n]
}

func (s *datePatternSuite) pattern(line int, id, skeleton, pattern string) {
	loc, opts, reason := datePatternOptions(id, skeleton, "UTC")
	if reason != "" {
		s.excluded[reason]++
		return
	}
	var names []any
	var parts []string
	var literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			// Intl renders ICU's narrow no-break space as a plain space
			// for web compatibility. Fixed-output fixtures supply their own text.
			parts = append(parts, "literal "+strings.ReplaceAll(literal.String(), "\u202f", " "))
			literal.Reset()
		}
	}
	var quoted bool
	for p := pattern; len(p) > 0; {
		ch := p[0]
		if ch == '\'' {
			p = p[1:]
			if strings.HasPrefix(p, "'") {
				literal.WriteByte(ch)
				p = p[1:]
			} else {
				quoted = !quoted
			}
			continue
		}
		if quoted || !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z') {
			literal.WriteByte(ch)
			p = p[1:]
			continue
		}
		flush()
		n := len(p) - len(strings.TrimLeft(p, string(ch)))
		p = p[n:]
		key, value, widthOnly := patternField(ch, n, loc, opts)
		if widthOnly {
			names = append(names, key)
		}
		parts = append(parts, key+" "+value)
	}
	if quoted {
		s.t.Fatalf("%s:%d: unterminated quote in pattern %q", s.path, line, pattern)
	}
	flush()
	s.check(line, "formatToParts/pattern", s.observe, strings.Join(parts, "\n"), loc, opts, patternInstant.UnixMilli(), skeleton, "pattern", names, pattern)
	if opts["dayPeriod"] != nil && (loc == "zh-TW" || loc == "gu-IN") {
		// Both locales distinguish night and morning within AM. A fixed
		// AM/PM value at both instants would hide a lost B field.
		s.check(line, "formatToParts/dayPeriod", s.observe, "distinct", loc, opts, patternInstant.UnixMilli(), skeleton, "periodChange", patternInstant.Add(9*time.Hour).UnixMilli())
	}
}

func patternField(ch byte, n int, loc string, opts map[string]any) (key, value string, widthOnly bool) {
	date := patternInstant
	english := loc == "en" || strings.HasPrefix(loc, "en-")
	num := func(v int) string {
		s := fmt.Sprintf("%0*d", n, v)
		if opts["numberingSystem"] == "arab" {
			return strings.Map(func(r rune) rune { return '٠' + r - '0' }, s)
		}
		return s
	}
	switch ch {
	case 'y', 'r':
		y := date.Year()
		switch opts["calendar"] {
		case "japanese":
			y -= 1988
		case "buddhist":
			y += 543
		case "coptic":
			y -= 284
		}
		if n == 2 {
			y %= 100
		}
		if ch == 'r' {
			return "relatedYear", num(y), false
		}
		return "year", num(y), false
	case 'U':
		return "yearName", "*", true
	case 'G':
		return "era", dateNameWidth(n), true
	case 'M', 'L':
		if n > 2 {
			if english && (opts["calendar"] == "gregory" || opts["calendar"] == "iso8601") {
				return "month", englishDateName(date.Month().String(), dateNameWidth(n)), false
			}
			return "month", dateNameWidth(n), true
		}
		m := int(date.Month())
		if opts["calendar"] == "coptic" {
			m = 6 // Amshir 25, 1722
		}
		return "month", num(m), false
	case 'd':
		d := date.Day()
		if opts["calendar"] == "coptic" {
			d = 25
		}
		return "day", num(d), false
	case 'E', 'e', 'c':
		if english {
			return "weekday", englishDateName(date.Weekday().String(), dateNameWidth(n)), false
		}
		return "weekday", dateNameWidth(n), true
	case 'h', 'H', 'k', 'K':
		cycle := map[byte]string{'h': "h12", 'H': "h23", 'k': "h24", 'K': "h11"}[ch]
		return "hour", num(date.Hour()) + " " + cycle, false
	case 'm':
		return "minute", num(date.Minute()), false
	case 's':
		return "second", num(date.Second()), false
	case 'S':
		return "fractionalSecond", num(integer(patternFraction(n))), false
	case 'a':
		if loc == "en" || loc == "en-US" {
			return "dayPeriod", date.Format("PM"), false
		}
		// ECMA-402 exposes a dayPeriod width only for flexible names.
		return "dayPeriod", "*", true
	case 'B':
		return "dayPeriod", dateNameWidth(n), true
	case 'v':
		return "timeZoneName", dateNameWidth(n) + "Generic", true
	default:
		panic(fmt.Sprintf("unhandled expected date pattern field %c", ch))
	}
}
