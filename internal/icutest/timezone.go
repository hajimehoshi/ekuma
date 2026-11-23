// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

type zoneSuite struct {
	*formatFixture
	root                     string
	resolved, temporal, name ekuma.Value
}

func runTimeZones(t *testing.T, root string, failures map[string]map[int]string) {
	for _, fixture := range []struct {
		path string
		run  func(*zoneSuite)
	}{
		{
			path: "icu4j/main/core/src/test/resources/com/ibm/icu/dev/test/format/TimeZoneAliases.txt",
			run:  (*zoneSuite).aliases,
		},
		{
			path: "icu4c/source/test/cintltst/ccaltst.c",
			run:  (*zoneSuite).ianaIDs,
		},
		{
			path: "icu4c/source/test/intltest/tztest.cpp",
			run:  (*zoneSuite).zoneTests,
		},
		{
			path: "icu4c/source/test/intltest/tzfmttst.cpp",
			run:  (*zoneSuite).zoneFormat,
		},
		{
			path: "icu4c/source/test/intltest/dtfmttst.cpp",
			run:  (*zoneSuite).dateFormat,
		},
		{
			path: "icu4j/main/common_tests/src/test/java/com/ibm/icu/dev/test/format/TimeZoneFormatTest.java",
			run:  (*zoneSuite).javaZoneFormat,
		},
	} {
		t.Run(filepath.Base(fixture.path), func(t *testing.T) {
			f := newFormatFixture(t, filepath.Join(root, fixture.path))
			f.trackOperations(failures)
			var vm ekuma.VM
			idFunction := func(expr string) ekuma.Value {
				return f.functionIn(&vm, `timeZone => {
					try { return `+expr+`; }
					catch (e) { if (e instanceof RangeError) return "RangeError"; throw e; }
				}`)
			}
			s := zoneSuite{
				formatFixture: f,
				root:          root,
				resolved:      idFunction(`new Intl.DateTimeFormat("en", {timeZone}).resolvedOptions().timeZone`),
				temporal:      idFunction(`new Temporal.ZonedDateTime(0n, timeZone).timeZoneId`),
				name: f.functionIn(&vm, `(locale, timeZone, instant, timeZoneName) => {
					const f = new Intl.DateTimeFormat(locale, {timeZone, timeZoneName});
					const ms = Date.parse(instant);
					const text = f.format(ms);
					const parts = f.formatToParts(ms);
					const zones = parts.filter(p => p.type === "timeZoneName");
					if (zones.length !== 1 || parts.map(p => p.value).join("") !== text)
						throw new Error(JSON.stringify({text, parts}));
					return zones[0].value;
				}`),
			}
			fixture.run(&s)
		})
	}
}

func (s *zoneSuite) identifier(line int, input, want string) {
	s.check(line, "Intl.DateTimeFormat(en).resolvedOptions().timeZone", s.resolved, want, input)
	s.check(line, "Temporal.ZonedDateTime(0n).timeZoneId", s.temporal, want, input)
}

// icuOnlyIDs reads the zones and links ICU adds to its IANA input.
func (s *zoneSuite) icuOnlyIDs() map[string]bool {
	path := filepath.Join(s.root, "icu4c/source/tools/tzcode/icuzones")
	ids := map[string]bool{}
	fixtureLines(s.t, path, func(line int, row string) {
		row, _, _ = strings.Cut(row, "#")
		fields := strings.Fields(row)
		if len(fields) == 0 {
			return
		}
		switch fields[0] {
		case "Zone":
			if len(fields) < 5 {
				s.t.Fatalf("%s:%d: incomplete Zone entry", path, line)
			}
			ids[fields[1]] = true
		case "Link":
			if len(fields) != 3 {
				s.t.Fatalf("%s:%d: malformed Link entry", path, line)
			}
			ids[fields[2]] = true
		}
	})
	return ids
}

func (s *zoneSuite) alias(line int, input, id string, icuOnly map[string]bool) {
	want := id
	if icuOnly[id] {
		want = "RangeError"
	}
	s.identifier(line, input, want)
}

func (s *zoneSuite) aliases() {
	icuOnly := s.icuOnlyIDs()
	forEachDataRow(s.t, s.path, 2, func(line int, ids []string) {
		for _, id := range ids {
			s.alias(line, id, id, icuOnly)
		}
	})
}

func (s *zoneSuite) ianaIDs() {
	src := readIntervalSource(s.t, s.path)
	for _, row := range src.table("IanaTimeZoneIDTestData TESTDATA[]") {
		c := row.columns(s.t, 2)
		if c[1] == "0" {
			break
		}
		if c[0] == "0" {
			s.excluded["null C pointer"]++
			continue
		}
		id := "Etc/Unknown"
		if c[0] != "UNKNOWN" {
			id = intervalString(s.t, c[0])
		}
		want := id
		if c[1] == "UNKNOWN" {
			want = "RangeError"
		}
		s.identifier(row.line, strings.ToLower(id), want)
	}
}

func (s *zoneSuite) display(line int, locale, zone, instant, style, want string) {
	s.check(line, "timeZoneName", s.name, want, strings.ReplaceAll(locale, "_", "-"), zone, instant, style)
}
