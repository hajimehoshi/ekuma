// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build ignore

// This program generates the year tables of the Chinese, Korean and Umm al-Qura calendars
// from ICU4X and the historical Korean calendar table from .NET. Run it with go generate.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/format"
	"io"
	"log"
	"math/bits"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	crateVersion         = "2.4.0"
	crateSHA256          = "a68ffb801cc7acb8dcae62da7c692c47934698d2dd4f479ddee1eb442763dea1"
	dotnetVersion        = "10.0.0"
	koreanCalendarSHA256 = "cd281054d039459ac7ddfd0401d61f2ae514dd4871d12284e326a9c782187768"
)

func get(url, wantSHA256 string) []byte {
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != wantSHA256 {
		log.Fatalf("%s: unexpected SHA-256 %x", url, sum)
	}
	return b
}

// crateFiles returns the files of the icu_calendar crate by their paths in the crate.
func crateFiles() map[string][]byte {
	url := "https://static.crates.io/crates/icu_calendar/icu_calendar-" + crateVersion + ".crate"
	b := get(url, crateSHA256)
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		log.Fatal(err)
	}
	tr := tar.NewReader(gz)
	files := map[string][]byte{}
	prefix := "icu_calendar-" + crateVersion + "/"
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			log.Fatal(err)
		}
		files[strings.TrimPrefix(h.Name, prefix)] = b
	}
	return files
}

// epochDays returns the days from 1970-01-01 to the proleptic Gregorian date y-m-d.
func epochDays(y, m, d int64) int64 {
	if m <= 2 {
		y--
	}
	era := y / 400
	if y < 0 && y%400 != 0 {
		era--
	}
	yoe := y - era*400
	mp := (m + 9) % 12
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

var (
	eastAsianRow = regexp.MustCompile(`PackedEastAsianTraditionalYearData::new\((\d+), \[([ls, ]+)\], (None|Some\((\d+)\)), gregorian\((\d+), (\d+), (\d+)\)\)`)
	hijriRow     = regexp.MustCompile(`PackedHijriYearData::try_new\((\d+), \[([ls, ]+)\], gregorian\((\d+), (\d+), (\d+)\)\)`)
	startingYear = regexp.MustCompile(`pub const STARTING_YEAR: i32 = (\d+);`)
)

func atoi(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		log.Fatal(err)
	}
	return n
}

// lengths returns the bits of the months with 30 days in a Rust list of l and s.
func lengths(s string) (uint32, int) {
	var bits uint32
	n := 0
	for i, e := range strings.Split(s, ",") {
		switch strings.TrimSpace(e) {
		case "l":
			bits |= 1 << i
		case "s":
		default:
			log.Fatalf("unexpected month length %q", e)
		}
		n++
	}
	return bits, n
}

type output struct {
	b bytes.Buffer
}

func (o *output) write(path string) {
	b, err := format.Source(o.b.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		log.Fatal(err)
	}
}

// comment writes the doc comment text, wrapped at 100 columns, after a blank line.
func (o *output) comment(text string) {
	o.b.WriteString("\n")
	line := "//"
	for _, w := range strings.Fields(text) {
		if len(line)+1+len(w) > 100 {
			o.b.WriteString(line + "\n")
			line = "//"
		}
		line += " " + w
	}
	o.b.WriteString(line + "\n")
}

// eastAsian writes a year table and returns its starting ISO year.
func (o *output) eastAsian(files map[string][]byte, path, name, doc string) int64 {
	src := string(files[path])
	start := atoi(startingYear.FindStringSubmatch(src)[1])
	rows := eastAsianRow.FindAllStringSubmatch(src, -1)
	if len(rows) == 0 {
		log.Fatalf("%s: no rows", path)
	}
	fmt.Fprintf(&o.b, "\n// %sStart is the first year of %sYears.\nconst %sStart = %d\n", name, name, name, start)
	o.comment(fmt.Sprintf("%sYears %s, from %sStart on. Bits 0 to 12 are set for the months with 30 days, bits 13 to 16 are the ordinal of the leap month or 0, and bits 17 to 22 are the new year in days after January 19 of the year.", name, doc, name))
	fmt.Fprintf(&o.b, "var %sYears = [...]uint32{\n", name)
	for i, r := range rows {
		year := atoi(r[1])
		if year != start+int64(i) {
			log.Fatalf("%s: year %d is not in order", path, year)
		}
		bits, n := lengths(r[2])
		if n != 13 {
			log.Fatalf("%s: year %d has %d months", path, year, n)
		}
		var leap int64
		if r[3] != "None" {
			leap = atoi(r[4])
		}
		newYear := epochDays(atoi(r[5]), atoi(r[6]), atoi(r[7]))
		o.eastAsianYear(year, newYear, leap, bits)
	}
	o.b.WriteString("}\n")
	return start
}

func (o *output) eastAsianYear(year, newYear, leap int64, months uint32) {
	offset := newYear - epochDays(year, 1, 19)
	if offset < 0 || offset >= 64 || leap > 13 {
		log.Fatalf("year %d does not fit", year)
	}
	fmt.Fprintf(&o.b, "\t%#08x, // %d\n", months|uint32(leap)<<13|uint32(offset)<<17, year)
}

func writeHistoricalKorea(modernStart int64) {
	const path = "/src/libraries/System.Private.CoreLib/src/System/Globalization/KoreanLunisolarCalendar.cs"
	src := get("https://raw.githubusercontent.com/dotnet/runtime/v"+dotnetVersion+path, koreanCalendarSHA256)
	row := regexp.MustCompile(`(?m)^(\d{4}) +\*/ \{ (\d+),\s+(\d+),\s+(\d+),\s+0b([01]{16}) \}`)

	// .NET documents agreement with KASI from 1391 onwards.
	const first = 1391
	var o output
	o.b.WriteString("// SPDX-License-Identifier: MIT\n// SPDX-FileCopyrightText: .NET Foundation and Contributors\n\n")
	fmt.Fprintf(&o.b, "// Code generated by gen_years.go from .NET %s KoreanLunisolarCalendar.cs. DO NOT EDIT.\n\npackage calendar\n\n", dotnetVersion)
	fmt.Fprintf(&o.b, "const historicalKoreaStart = %d\n\n", first)
	o.comment("historicalKoreaYears holds the Korean calendar years from historicalKoreaStart, encoded as eastAsianYear.")
	o.b.WriteString("var historicalKoreaYears = [...]uint32{\n")
	expectedYear := int64(first)
	var nextNewYear int64
	for _, r := range row.FindAllStringSubmatch(string(src), -1) {
		year := atoi(r[1])
		if year < first {
			continue
		}
		newYear := epochDays(year, atoi(r[3]), atoi(r[4]))
		if year != expectedYear || (year > first && newYear != nextNewYear) {
			log.Fatalf("discontinuous Korean calendar at year %d", year)
		}
		if year == modernStart {
			o.b.WriteString("}\n")
			o.write("koreanhistory.go")
			return
		}
		// .NET names the regular month preceding a leap month. Its high bit
		// records the length of the first ordinal month.
		leap := atoi(r[2])
		numMonths := 12
		if leap != 0 {
			leap++
			numMonths++
		}
		mask, err := strconv.ParseUint(r[5], 2, 16)
		if err != nil {
			log.Fatal(err)
		}
		months := bits.Reverse16(uint16(mask))
		if months>>numMonths != 0 {
			log.Fatalf("unexpected month bits in Korean year %d", year)
		}
		o.eastAsianYear(year, newYear, leap, uint32(months))
		expectedYear++
		nextNewYear = newYear + int64(29*numMonths+bits.OnesCount16(months))
	}
	log.Fatal("incomplete historical Korean calendar")
}

func (o *output) hijri(files map[string][]byte, path string) {
	src := string(files[path])
	start := atoi(startingYear.FindStringSubmatch(src)[1])
	rows := hijriRow.FindAllStringSubmatch(src, -1)
	if len(rows) == 0 {
		log.Fatalf("%s: no rows", path)
	}
	fmt.Fprintf(&o.b, "\n// umalquraStart is the first year of umalquraYears.\nconst umalquraStart = %d\n", start)
	o.comment("umalquraYears holds the published Umm al-Qura years from umalquraStart on. Bits 0 to 11 are set for the months with 30 days, and the bits from 12 on are the new year in days from 1970-01-01.")
	o.b.WriteString("var umalquraYears = [...]int32{\n")
	for i, r := range rows {
		year := atoi(r[1])
		if year != start+int64(i) {
			log.Fatalf("%s: year %d is not in order", path, year)
		}
		bits, n := lengths(r[2])
		if n != 12 {
			log.Fatalf("%s: year %d has %d months", path, year, n)
		}
		newYear := epochDays(atoi(r[3]), atoi(r[4]), atoi(r[5]))
		fmt.Fprintf(&o.b, "\t%d<<12 | %#03x, // %d\n", newYear, bits, year)
	}
	o.b.WriteString("}\n")
}

func main() {
	files := crateFiles()
	var o output
	o.b.WriteString("// SPDX-License-Identifier: 0BSD\n// SPDX-FileCopyrightText: 2026 Hajime Hoshi\n\n")
	fmt.Fprintf(&o.b, "// Code generated by gen_years.go from icu_calendar %s. DO NOT EDIT.\n\npackage calendar\n", crateVersion)
	o.eastAsian(files, "src/cal/east_asian_traditional/china_data.rs", "china", "holds the Chinese years computed by the rules of GB/T 33661-2017")
	modernKoreaStart := o.eastAsian(files, "src/cal/east_asian_traditional/korea_data.rs", "korea", "holds the Korean years confirmed by KASI")
	o.eastAsian(files, "src/cal/east_asian_traditional/qing_data.rs", "qing", "holds the Chinese calendar years of the late Qing dynasty")
	o.hijri(files, "src/cal/hijri/ummalqura_data.rs")
	o.write("years.go")
	writeHistoricalKorea(modernKoreaStart)
}
