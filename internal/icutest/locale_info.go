// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const icuData = "icu4c/source/data/"

func runLocaleInfo(t *testing.T, root string, failures map[string]map[int]string) {
	// keyTypeData maps ICU's legacy calendar and collation names to BCP 47.
	path := filepath.Join(root, icuData, "misc/keyTypeData.txt")
	types := readResource(t, path).child("typeMap")
	calendars := types.child("calendar")
	collations := types.child("collation")
	run := func(path string, check func(*formatFixture)) {
		t.Run(path, func(t *testing.T) {
			check(newFormatFixture(t, filepath.Join(root, path)))
		})
	}
	run(icuData+"misc/keyTypeData.txt", func(f *formatFixture) {
		var names []string
		for _, entry := range calendars.children {
			if calendarAvailable(entry.name) {
				names = append(names, unicodeType(f, calendars, entry.name))
			} else {
				f.excluded["observational Islamic calendar outside the Intl.Era-monthcode calendar table"]++
			}
		}
		checkSupportedValues(f, calendars.line, "calendar", names)
	})
	run(icuData+"misc/supplementalData.txt", func(f *formatFixture) {
		data := readResource(f.t, f.path)
		f.trackOperations(failures)
		checkWeekInfo(f, data.child("weekData"))
		checkLanguageHourCycles(f, data.child("timeData"))
		checkCalendarPreferences(f, data.child("calendarPreferenceData"), calendars)
	})
	run(icuData+"coll/root.txt", func(f *formatFixture) {
		checkCollationResources(f, collations)
	})
	run(icuData+"misc/numberingSystems.txt", checkNumberingSystemValues)
	for _, file := range []string{"ar_EG", "ar_MA", "bn"} {
		run(icuData+"locales/"+file+".txt", func(f *formatFixture) {
			entry := readResource(f.t, f.path).child("NumberElements").child("default")
			want := resourceValue(f, entry)
			locale := strings.ReplaceAll(file, "_", "-")
			fn := f.function(`tag => new Intl.Locale(tag).getNumberingSystems().join(" ")`)
			f.check(entry.line, "getNumberingSystems", fn, want, locale)
			if file == "bn" {
				f.check(entry.line, "getNumberingSystems", fn, want, "bn-BD-1996")
				f.check(entry.line, "getNumberingSystems", fn, want, "ar-EG-u-nu-"+want)
			}
		})
	}
	run(icuData+"curr/en.txt", func(f *formatFixture) {
		currencies := readResource(f.t, f.path).child("Currencies")
		var names []string
		for _, entry := range currencies.children {
			names = append(names, entry.name)
		}
		checkSupportedValues(f, currencies.line, "currency", names)
	})
	run(icuData+"unit/en.txt", checkUnitValues)
	run(icuData+"misc/timezoneTypes.txt", checkZoneValues)
	run("icu4c/source/test/cintltst/udatpg_test.c", func(f *formatFixture) {
		src := readIntervalSource(f.t, f.path).from("TestGetDefaultHourCycle(void) {")
		fn := f.function(`tag => new Intl.Locale(tag).getHourCycles()[0]`)
		for _, row := range src.table("HourCycleData testData[]") {
			if strings.HasPrefix(row.text, "/*") {
				continue
			}
			cols := row.columns(f.t, 2)
			tag := strings.ReplaceAll(intervalString(f.t, cols[0]), "_", "-")
			cycle := strings.TrimPrefix(cols[1], "UDAT_HOUR_CYCLE_")
			f.check(row.line, "getHourCycles()[0]", fn, "h"+cycle, tag)
		}
	})
	run("icu4j/main/core/src/test/java/com/ibm/icu/dev/test/format/DateTimeGeneratorTest.java", checkHourCycleOverrides)
	run("icu4j/main/common_tests/src/test/java/com/ibm/icu/dev/test/util/ULocaleTest.java", checkTextDirection)
}

func resourceValue(f *formatFixture, r resource) string {
	f.t.Helper()
	if len(r.values) != 1 {
		f.t.Fatalf("%s:%d: expected one value for %q, got %q", f.path, r.line, r.name, r.values)
	}
	return r.values[0]
}

func unicodeType(f *formatFixture, mapping resource, name string) string {
	f.t.Helper()
	if mapped := resourceValue(f, mapping.child(name)); mapped != "" {
		return mapped
	}
	return name
}

func calendarAvailable(name string) bool {
	return name != "islamic" && name != "islamic-rgsa"
}

func checkCalendarPreferences(f *formatFixture, prefs, mapping resource) {
	fn := f.function(`tag => new Intl.Locale(tag).getCalendars().join(" ")`)
	for _, entry := range prefs.children {
		var names []string
		for _, name := range entry.values {
			if !calendarAvailable(name) {
				f.excluded["regional preference for an observational Islamic calendar"]++
				continue
			}
			names = append(names, unicodeType(f, mapping, name))
		}
		want := strings.Join(names, " ")
		f.check(entry.line, "getCalendars", fn, want, "und-"+entry.name)
		switch entry.name {
		case "TH":
			for _, tag := range []string{"th", "en-US-u-rg-thzzzz", "en-u-sd-th10", "th-TH-u-rg-uszzzz"} {
				f.check(entry.line, "getCalendars", fn, want, tag)
			}
			f.check(entry.line, "getCalendars", fn, "gregory", "th-u-ca-gregory")
		case "001":
			f.check(entry.line, "getCalendars", fn, want, "en-US")
		}
	}
}

func checkWeekInfo(f *formatFixture, weeks resource) {
	fn := f.function(`tag => {
		const {firstDay, weekend} = new Intl.Locale(tag).getWeekInfo();
		return JSON.stringify([firstDay, weekend]);
	}`)
	for _, entry := range weeks.children {
		region, ok := strings.CutSuffix(entry.name, ":intvector")
		if !ok || len(entry.values) != 6 {
			f.t.Fatalf("%s:%d: invalid week record %s %v", f.path, entry.line, entry.name, entry.values)
		}
		if entry.values[3] != "0" || entry.values[5] != "86400000" {
			f.excluded["partial-day weekend boundary"]++
			continue
		}
		// ICU numbers Sunday as 1. Intl numbers Monday as 1.
		weekday := func(value string) int {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 7 {
				f.t.Fatalf("%s:%d: invalid weekday %q", f.path, entry.line, value)
			}
			return (n+5)%7 + 1
		}
		first := weekday(entry.values[0])
		start, end := weekday(entry.values[2]), weekday(entry.values[4])
		var weekend []string
		for day := 1; day <= 7; day++ {
			if (day-start+7)%7 <= (end-start+7)%7 {
				weekend = append(weekend, strconv.Itoa(day))
			}
		}
		check := func(tag string, first int) {
			f.check(entry.line, "getWeekInfo", fn, fmt.Sprintf("[%d,[%s]]", first, strings.Join(weekend, ",")), tag)
		}
		check("und-"+region, first)
		if region == "AF" {
			check("en-US-u-rg-afzzzz", first)
			check("en-US-u-fw-tue-rg-afzzzz", 2)
		}
		if region == "IR" {
			check("fa", first)
		}
	}
}

func checkLanguageHourCycles(f *formatFixture, timeData resource) {
	fn := f.function(`tag => new Intl.Locale(tag).getHourCycles()[0]`)
	cycles := map[string]string{
		"h": "h12",
		"H": "h23",
		"K": "h11",
		"k": "h24",
	}
	for _, entry := range timeData.children {
		language, region, ok := strings.Cut(entry.name, "_")
		if !ok {
			continue
		}
		preferred := entry.child("preferred")
		symbol := resourceValue(f, preferred)
		want, ok := cycles[symbol]
		if !ok {
			f.t.Fatalf("%s:%d: unknown preferred hour symbol %q", f.path, preferred.line, symbol)
		}
		f.check(preferred.line, "getHourCycles()[0]", fn, want, language+"-"+region)
	}
}

func checkHourCycleOverrides(f *formatFixture) {
	src := readIntervalSource(f.t, f.path).from("void testRegionOverride()")
	preferred := f.function(`tag => new Intl.Locale(tag).getHourCycles()[0]`)
	restricted := f.function(`tag => new Intl.Locale(tag).getHourCycles().join(" ")`)
	for _, row := range src.table("String[][] testCases") {
		cols := row.columns(f.t, 3)
		base, keyword, _ := strings.Cut(intervalString(f.t, cols[0]), "@")
		tag := strings.ReplaceAll(base, "_", "-")
		want := "h" + strings.TrimPrefix(intervalString(f.t, cols[2]), "HOUR_CYCLE_")
		key, value, _ := strings.Cut(keyword, "=")
		switch key {
		case "":
		case "rg":
			tag += "-u-rg-" + strings.ToLower(value)
		case "hours":
			f.check(row.line, "getHourCycles", restricted, want, tag+"-u-hc-"+value)
			continue
		default:
			f.t.Fatalf("%s:%d: unknown hour-cycle keyword %q", f.path, row.line, keyword)
		}
		f.check(row.line, "getHourCycles()[0]", preferred, want, tag)
	}
}

func checkTextDirection(f *formatFixture) {
	src := readIntervalSource(f.t, f.path).from("void TestIsRightToLeft()")
	fn := f.function(`tag => String(new Intl.Locale(tag).getTextInfo().direction)`)
	for i, row := range src.lines[1:] {
		if row == "}" {
			break
		}
		var tag string
		switch {
		case strings.Contains(row, "ULocale.ROOT."):
			tag = "und"
		case strings.Contains(row, "ULocale.CHINESE."):
			tag = "zh"
		default:
			_, arg, ok := strings.Cut(row, "new ULocale(")
			if !ok {
				f.t.Fatalf("%s:%d: unknown locale in %s", f.path, src.line+i+1, row)
			}
			tag = intervalString(f.t, arg)
		}
		want := "ltr"
		switch {
		case strings.HasPrefix(row, "assertTrue("):
			want = "rtl"
		case tag == "he-Zyxw":
			// ICU's boolean also returns false for an unregistered script.
			want = "undefined"
		case !strings.HasPrefix(row, "assertFalse("):
			f.t.Fatalf("%s:%d: unknown direction assertion %s", f.path, src.line+i+1, row)
		}
		f.check(src.line+i+1, "getTextInfo().direction", fn, want, tag)
	}
}

func checkSupportedValues(f *formatFixture, line int, key string, names []string) {
	f.t.Helper()
	if len(names) == 0 {
		f.t.Fatalf("%s:%d: no %s identifiers", f.path, line, key)
	}
	slices.Sort(names)
	fn := f.function(`key => Intl.supportedValuesOf(key).join(" ")`)
	f.check(line, "Intl.supportedValuesOf", fn, strings.Join(names, " "), key)
}
