// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// zoneRows joins the six-column format records which ICU wraps over several lines.
func zoneRows(src intervalSource, marker string) []intervalRow {
	var rows []intervalRow
	var record intervalRow
	for _, r := range src.table(marker) {
		if record.text == "" {
			record.line = r.line
		}
		record.text += " " + r.text
		if strings.HasSuffix(strings.TrimSuffix(r.text, ","), "}") {
			rows = append(rows, record)
			record = intervalRow{}
		}
	}
	if record.text != "" {
		src.t.Fatalf("%s:%d: incomplete format record %s", src.path, record.line, record.text)
	}
	return rows
}

var zoneMillis = regexp.MustCompile(`[0-9]{12,}`)

func instantInSource(src intervalSource, marker string) string {
	r := src.from(marker)
	ms, err := strconv.ParseInt(zoneMillis.FindString(r.lines[0]), 10, 64)
	if err != nil {
		src.t.Fatalf("%s:%d: invalid epoch milliseconds in %s", src.path, r.line, r.lines[0])
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
}

func (s *zoneSuite) formatRows(block intervalSource, table, jan, jul string) {
	dates := map[string]string{
		"dateJan": instantInSource(block, jan),
		"dateJul": instantInSource(block, jul),
	}
	for _, row := range zoneRows(block, table) {
		c := row.columns(s.t, 6)
		if c[0] == "nullptr" {
			break
		}
		zone := intervalString(s.t, c[1])
		want := intervalString(s.t, c[4])
		style := strings.TrimPrefix(strings.TrimPrefix(c[3], "UTZFMT_STYLE_"), "Style.")
		var name string
		switch style {
		case "GENERIC_LONG":
			name = "longGeneric"
		case "SPECIFIC_LONG":
			name = "long"
		case "ZONE_ID":
			s.identifier(row.line, zone, want)
			continue
		case "GENERIC_LOCATION", "EXEMPLAR_LOCATION", "ZONE_ID_SHORT":
			s.excluded["ICU display style "+style]++
			continue
		default:
			s.t.Fatalf("%s:%d: unknown ICU style %s", s.path, row.line, style)
		}
		s.display(row.line, intervalString(s.t, c[0]), zone, dates[c[2]], name, want)
	}
}

func (s *zoneSuite) zoneFormat() {
	src := readIntervalSource(s.t, s.path)
	s.formatRows(src.from("::TestFormat() {"), "FormatTestData DATA[]", "UDate dateJan =", "UDate dateJul =")
	s.excluded["TZDB abbreviation provider"] += len(zoneRows(src.from("::TestFormatTZDBNames() {"), "FormatTestData DATA[]")) - 1
	s.excluded["application-defined SimpleTimeZone"] += len(src.from("::TestFormatCustomZone() {").table("} TESTDATA[]")) - 1
}

func (s *zoneSuite) javaZoneFormat() {
	src := readIntervalSource(s.t, s.path)
	s.formatRows(src.from("void TestFormat()"), "Object[][] TESTDATA", "Date dateJan =", "Date dateJul =")
	block := src.from("void TestInheritedFormat()")
	zone := intervalString(s.t, block.from("TimeZone.getTimeZone(").lines[0])
	assertion := block.from("fmt.format(cal, sb, fp)").from("if (!sb.toString().equals(")
	s.display(assertion.line, "en", zone, instantInSource(block, "cal.setTimeInMillis("), "longOffset", intervalString(s.t, assertion.lines[0]))
	s.excluded["TimeZone object formatted at the current time"]++
	s.excluded["TZDB abbreviation provider"] += len(zoneRows(src.from("void TestFormatTZDBNames()"), "Object[][] TESTDATA"))
}

const (
	zoneJanuary = "2013-01-15T00:00:00Z"
	zoneJuly    = "2013-07-15T00:00:00Z"
)

var zoneComment = regexp.MustCompile(`/\*.*?\*/`)

func (s *zoneSuite) zoneTests() {
	src := readIntervalSource(s.t, s.path)
	// The C++ alias table includes identifiers absent from the Java resource.
	icuOnly := s.icuOnlyIDs()
	for _, row := range src.from("::TestAliasedNames()").table("} kData[]") {
		if strings.HasPrefix(row.text, "/*") {
			continue
		}
		for _, c := range row.columns(s.t, 2) {
			id := intervalString(s.t, c)
			s.alias(row.line, strings.ToLower(id), id, icuOnly)
		}
	}
	s.pacificNames(src.from("::TestDisplayName()"))
	s.metaZoneNames(src)
}

func (s *zoneSuite) pacificNames(block intervalSource) {
	names := map[string]string{
		"SHORT":               "short",
		"LONG":                "long",
		"SHORT_COMMONLY_USED": "short",
		"SHORT_GENERIC":       "shortGeneric",
		"LONG_GENERIC":        "longGeneric",
		"LONG_GMT":            "longOffset",
	}
	for _, row := range block.table("} kData[]") {
		c := row.columns(s.t, 3)
		want := intervalString(s.t, c[2])
		if want == "" {
			break
		}
		instant := zoneJanuary
		if c[0] == "true" {
			instant = zoneJuly
		}
		style := strings.TrimPrefix(c[1], "TimeZone::")
		if name, ok := names[style]; ok {
			s.display(row.line, "en", "America/Los_Angeles", instant, name, want)
			continue
		}
		switch style {
		case "SHORT_GMT":
			s.excluded["RFC 822 offset"]++
		case "GENERIC_LOCATION":
			s.excluded["ICU display style "+style]++
		default:
			s.t.Fatalf("%s:%d: unknown ICU style %s", s.path, row.line, style)
		}
	}
}

func (s *zoneSuite) metaZoneNames(src intervalSource) {
	for _, row := range src.table("zoneDisplayTestData []") {
		row.text = zoneComment.ReplaceAllString(row.text, "")
		c := row.columns(s.t, 5)
		if c[0] == "nullptr" {
			break
		}
		zone := intervalString(s.t, c[0])
		instant := zoneJanuary
		style := "short"
		if c[3] == "TimeZone::LONG" {
			style = "long"
		}
		if c[2] == "true" {
			instant = zoneJuly
			switch zone {
			case "Australia/Perth":
				if style == "short" {
					s.excluded["Perth forced daylight fallback uses a standard offset"]++
					continue
				}
				instant = "2007-01-15T00:00:00Z"
			case "Pacific/Honolulu":
				s.excluded["Honolulu forced daylight fallback uses a standard offset"]++
				continue
			}
		} else if zone == "America/Sao_Paulo" {
			instant = zoneJuly
		}
		s.display(row.line, intervalString(s.t, c[1]), zone, instant, style, intervalString(s.t, c[4]))
	}
}

func (s *zoneSuite) dateFormat() {
	src := readIntervalSource(s.t, s.path)
	for _, row := range src.table("*fallbackTests[][6]") {
		c := row.columns(s.t, 6)
		if c[0] == "nullptr" {
			break
		}
		loc := intervalString(s.t, c[0])
		zone := intervalString(s.t, c[1])
		pattern := intervalString(s.t, c[3])
		_, opts, reason := datePatternOptions(loc, pattern, zone)
		if reason != "" {
			s.excluded[reason]++
			continue
		}
		s.display(row.line, loc, zone, intervalString(s.t, c[2]), opts["timeZoneName"].(string), intervalString(s.t, c[4]))
	}
	s.fieldPositions(src)
	block := src.from("::TestLocaleDateFormat()")
	format := s.function(`(locale, timeZone, instant, options) =>
		new Intl.DateTimeFormat(locale, {...options, timeZone}).format(Date.parse(instant))`)
	for _, tc := range []struct {
		locale string
		marker string
	}{
		{
			locale: "fr",
			marker: "UnicodeString expectedFRENCH",
		},
		{
			locale: "en-US",
			marker: "UnicodeString expectedUS",
		},
	} {
		r := block.from(tc.marker)
		s.check(r.line, "format", format, intervalString(s.t, r.lines[0]), tc.locale, "America/Los_Angeles", "1997-09-15T07:00:00Z", map[string]any{"dateStyle": "full", "timeStyle": "full"})
	}
}

func (s *zoneSuite) fieldPositions(src intervalSource) {
	fields := listArray(src, "char* DATEFORMAT_FIELD_NAMES[]")
	block := src.from("::TestFieldPosition() {")
	instant := instantInSource(block, "UDate aug13 =")
	zone := intervalString(s.t, block.from("TimeZone* PT =").lines[0])
	// Each formatter contributes one result per field. The conditional final
	// time separator occupies one slot in either build of ICU.
	var n int
	expected := block.from("char* EXPECTED[]")
	var alternate bool
	for i, line := range expected.lines[1:] {
		switch line {
		case "};":
			if n != 4*len(fields) {
				s.t.Fatalf("%s:%d: expected four sets of %d fields, got %d values", s.path, expected.line, len(fields), n)
			}
			return
		case "#else":
			alternate = true
		case "#endif":
			alternate = false
		}
		if alternate {
			continue
		}
		for _, want := range listStrings(s.t, line) {
			group, field := n/len(fields), fields[n%len(fields)]
			n++
			if want == "" {
				continue
			}
			loc := "en-US"
			if group == 1 {
				loc = "fr-FR"
			}
			style := "long"
			if group == 2 {
				style = "short"
			}
			switch field {
			case "TIMEZONE_FIELD":
			case "GENERIC_TIMEZONE_FIELD":
				style += "Generic"
			case "TIMEZONE_LOCALIZED_GMT_OFFSET_FIELD":
				style += "Offset"
			default:
				continue
			}
			s.display(expected.line+i+1, loc, zone, instant, style, want)
		}
	}
	s.t.Fatalf("%s:%d: unterminated field positions", s.path, expected.line)
}
