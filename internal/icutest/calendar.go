// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"cmp"
	"fmt"
	"strings"
	"testing"
)

func calendarLocale(id string) (string, string) {
	loc, cal, _ := strings.Cut(id, "@calendar=")
	if cal == "gregorian" {
		cal = "gregory"
	}
	return strings.ReplaceAll(loc, "_", "-"), cal
}

func icuFields(input string, inherit map[string]int) map[string]int {
	fields := map[string]int{}
	for pair := range strings.SplitSeq(input, ",") {
		key, v, _ := strings.Cut(pair, "=")
		switch v {
		case "":
			fields[key] = inherit[key]
		case "JANUARY":
			fields[key] = 0
		case "FEBRUARY":
			fields[key] = 1
		case "JUNE":
			fields[key] = 5
		case "AUGUST":
			fields[key] = 7
		case "OCTOBER":
			fields[key] = 9
		default:
			fields[key] = integer(v)
		}
	}
	return fields
}

func temporalFields(calendar string, fields map[string]int) map[string]any {
	bag := map[string]any{"calendar": calendar, "day": 1, "monthCode": "M01"}
	for key, v := range fields {
		switch key {
		case "YEAR":
			if _, ok := fields["ERA"]; ok {
				bag["eraYear"] = v
			} else {
				bag["year"] = v
			}
		case "ERA":
			names := [2]string{"bce", "ce"}
			if calendar == "roc" {
				names = [2]string{"broc", "roc"}
			}
			bag["era"] = names[v]
		case "EXTENDED_YEAR":
			bag["year"] = v
		case "MONTH":
			bag["monthCode"] = monthCode(v+1, fields["IS_LEAP_MONTH"])
		case "IS_LEAP_MONTH": // Included in monthCode.
		case "DATE":
			bag["day"] = v
		case "HOUR_OF_DAY":
			bag["hour"] = v
		case "MINUTE":
			bag["minute"] = v
		case "SECOND":
			bag["second"] = v
		default:
			panic("unknown ICU date field " + key)
		}
	}
	return bag
}

func monthCode(month, leap int) string {
	code := fmt.Sprintf("M%02d", month)
	if leap == 1 {
		code += "L"
	}
	return code
}

func dateResult(fields map[string]any) string {
	year := fmt.Sprint(fields["year"])
	if era, ok := fields["era"]; ok {
		year = fmt.Sprint(era, " ", fields["eraYear"])
	}
	return fmt.Sprintf("%s %s %d", year, fields["monthCode"], fields["day"])
}

func runCalendar(t *testing.T, path string, forward, reverse map[int]string) {
	f := newFormatFixture(t, path)
	f.track("Temporal.PlainDate convert_fwd", forward)
	f.track("Temporal.PlainDate convert_rev", reverse)
	convert := f.function(`(input, calendar, era, duration) => {
		let d = Temporal.PlainDate.from(input, {overflow: "reject"});
		if (duration) d = d.add(duration);
		d = d.withCalendar(calendar);
		return [era ? d.era+" "+d.eraYear : d.year, d.monthCode, d.day].join(" ");
	}`)
	addZoned := f.function(`(millis, calendar, zone, duration) => Temporal.Instant.fromEpochMilliseconds(Number(millis))
		.toZonedDateTimeISO(zone).withCalendar(calendar).add(duration).epochMilliseconds.toString()`)
	resourceCases(f, func(setting resource, row map[string]string, line int) {
		_, cal := calendarLocale(row["locale"])
		op := setting.child("Type").values[0]
		var duration any
		toCal := cal
		switch op {
		case "ops":
			if row["operation"] == "roll" {
				f.excluded["ICU Calendar.roll wraps one field without carrying into larger fields"]++
				return
			}
			if row["operation"] != "add" {
				t.Fatalf("%s:%d: unknown operation %q", path, line, row["operation"])
			}
			period := map[string]any{}
			for key, v := range icuFields(row["params"], nil) {
				unit := map[string]string{"DATE": "days", "MONTH": "months", "YEAR": "years"}[key]
				period[unit] = v
			}
			duration = period
			op = "add"
			if millis, ok := strings.CutPrefix(row["from"], "MILLIS="); ok {
				f.check(line, "Temporal.ZonedDateTime.add", addZoned, strings.TrimPrefix(row["to"], "MILLIS="), millis, cal, "America/Los_Angeles", duration)
				return
			}
		case "convert_fwd", "convert_rev":
			_, toCal = calendarLocale(setting.child("ToCalendar").values[0])
		default:
			t.Fatalf("%s:%d: unknown setting %s", path, line, op)
		}
		from := icuFields(row["from"], nil)
		to := icuFields(row["to"], from)
		if op == "convert_rev" {
			from, to = to, from
			cal, toCal = toCal, cal
		}
		bag, want := temporalFields(cal, from), temporalFields(toCal, to)
		_, withEra := want["era"]
		f.check(line, "Temporal.PlainDate "+op, convert, dateResult(want), bag, toCal, withEra, duration)
	})
}

func runLunarMapping(t *testing.T, path string) {
	f := newFormatFixture(t, path)
	convert := f.function(`(bag, target, zone) => {
		const d = Temporal.ZonedDateTime.from({...bag, timeZone: zone, hour: 8}, {overflow: "reject"}).withCalendar(target);
		return [d.year, d.monthCode, d.day].join(" ");
	}`)
	var active, terminated bool
	fixtureLines(t, path, func(line int, row string) {
		row = strings.TrimSpace(row)
		if row == "static const GregoToLunar gregoToLunar[] = {" {
			active = true
			return
		}
		if !active {
			return
		}
		if strings.HasPrefix(row, "//#") {
			f.excluded["mapping disabled in ICU's table"]++
			return
		}
		row, _, _ = strings.Cut(row, "//")
		row = strings.TrimSpace(row)
		if row == "" {
			return
		}
		values := strings.Split(strings.Trim(row, "{}, "), ",")
		if len(values) != 7 {
			t.Fatalf("%s:%d: invalid mapping %q", path, line, row)
		}
		var n [7]int
		for i, v := range values {
			n[i] = integer(strings.TrimSpace(v))
		}
		if n == [7]int{} {
			active = false
			terminated = true
			return
		}
		// A lunar year starts in Gregorian January or February. The
		// sexagenary cycle starting in 1864 precedes all fixture dates.
		year := n[0]
		if (year-1864)%60+1 != n[3] {
			year--
		}
		if (year-1864)%60+1 != n[3] {
			t.Fatalf("%s:%d: invalid cyclic year %d", path, line, n[3])
		}
		g := map[string]any{"calendar": "gregory", "year": n[0], "monthCode": monthCode(n[1], 0), "day": n[2]}
		c := map[string]any{"calendar": "chinese", "year": year, "monthCode": monthCode(n[4], n[5]), "day": n[6]}
		// TestChineseCalendarMapping passes "China" to createTimeZone.
		// ICU 79.1 resolves this unknown ID to a fixed zero offset.
		f.check(line, "Temporal.ZonedDateTime.withCalendar", convert, dateResult(c), g, "chinese", "UTC")
		f.check(line, "Temporal.ZonedDateTime.withCalendar", convert, dateResult(g), c, "gregory", "UTC")
	})
	if !terminated {
		t.Fatalf("%s: missing lunar table terminator", path)
	}
}

func runDateFormat(t *testing.T, path string, known map[int]string) {
	f := newFormatFixture(t, path)
	f.track("Intl.DateTimeFormat.format", known)
	format := f.function(`(locale, options, date, pattern) => {
		const ms = typeof date === "string" ? Number(date) :
			Temporal.ZonedDateTime.from({...date, timeZone: options.timeZone}).epochMilliseconds;
		const fmt = new Intl.DateTimeFormat(locale, options);
		if (!pattern) return fmt.format(ms);
		const parts = fmt.formatToParts(ms);
		return ["era", "year"].map(k => parts.find(p => p.type === k).value).join(" ");
	}`)
	resourceCases(f, func(setting resource, row map[string]string, line int) {
		if setting.child("Type").values[0] == "date_parse" {
			f.excluded["Intl.DateTimeFormat has no parsing operation"]++
			return
		}
		loc, cal := calendarLocale(row["locale"])
		zone := cmp.Or(row["zone"], "America/Los_Angeles")
		opts := map[string]any{"calendar": cmp.Or(cal, "gregory"), "timeZone": zone}
		spec, date := row["spec"], row["date"]
		var pattern string
		if p, ok := strings.CutPrefix(spec, "PATTERN="); ok {
			pattern = p
			switch p {
			case "G y":
				opts["year"] = "numeric"
			case "G yy":
				opts["year"] = "2-digit"
			default:
				f.excluded["Intl year width is numeric or two-digit; ICU pattern requests extra padding"]++
				return
			}
			opts["era"] = "short"
		} else {
			for kv := range strings.SplitSeq(spec, ",") {
				k, v, _ := strings.Cut(kv, "=")
				if strings.HasPrefix(v, "RELATIVE_") {
					if strings.HasPrefix(date, "RELATIVE_") || strings.Contains(spec, ",") {
						f.excluded["ICU relative-date formatter supplies relative words and combination patterns"]++
						return
					}
					v = strings.TrimPrefix(v, "RELATIVE_")
				}
				opts[strings.ToLower(k)+"Style"] = strings.ToLower(v)
			}
		}
		var input any
		if ms, ok := strings.CutPrefix(date, "MILLIS="); ok {
			input = ms
		} else if relative, ok := strings.CutPrefix(date, "RELATIVE_ADD:"); ok {
			// Only the explicit clock fields are displayed by this time-only case.
			date = relative
			fields := icuFields(date, nil)
			fields["YEAR"] = 2000
			fields["DATE"] = 1
			input = temporalFields(cal, fields)
		} else {
			input = temporalFields(cal, icuFields(date, nil))
		}
		op := "Intl.DateTimeFormat.format"
		if pattern != "" {
			op = "Intl.DateTimeFormat.formatToParts"
		}
		f.check(line, op, format, row["str"], loc, opts, input, pattern)
	})
}
