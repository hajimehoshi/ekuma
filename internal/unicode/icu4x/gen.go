// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build ignore

// This program generates the tables of package icu4x from a release of ICU4X's icu_calendar
// crate, and LICENSE, the license of that data. Run it with go generate.
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
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	crateVersion = "2.4.0"
	crateSHA256  = "a68ffb801cc7acb8dcae62da7c692c47934698d2dd4f479ddee1eb442763dea1"
)

func get(url string) []byte {
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
	return b
}

// crateFiles returns the files of the icu_calendar crate by their paths in the crate.
func crateFiles() map[string][]byte {
	url := "https://static.crates.io/crates/icu_calendar/icu_calendar-" + crateVersion + ".crate"
	b := get(url)
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != crateSHA256 {
		log.Fatalf("%s: unexpected SHA-256 %x", url, sum)
	}
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

func (o *output) eastAsian(files map[string][]byte, path, name, doc string) {
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
		offset := newYear - epochDays(year, 1, 19)
		if offset < 0 || offset >= 64 || leap > 13 {
			log.Fatalf("%s: year %d does not fit", path, year)
		}
		fmt.Fprintf(&o.b, "\t%#08x, // %d\n", uint32(bits)|uint32(leap)<<13|uint32(offset)<<17, year)
	}
	o.b.WriteString("}\n")
}

func (o *output) hijri(files map[string][]byte, path string) {
	src := string(files[path])
	start := atoi(startingYear.FindStringSubmatch(src)[1])
	rows := hijriRow.FindAllStringSubmatch(src, -1)
	if len(rows) == 0 {
		log.Fatalf("%s: no rows", path)
	}
	fmt.Fprintf(&o.b, "\n// UmalquraStart is the first year of UmalquraYears.\nconst UmalquraStart = %d\n", start)
	o.comment("UmalquraYears holds the published Umm al-Qura years from UmalquraStart on. Bits 0 to 11 are set for the months with 30 days, and the bits from 12 on are the new year in days from 1970-01-01.")
	o.b.WriteString("var UmalquraYears = [...]int32{\n")
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

// copyright returns the copyright line of the license of the crate.
func copyright(license string) string {
	for line := range strings.SplitSeq(license, "\n") {
		if s, ok := strings.CutPrefix(strings.TrimSpace(line), "Copyright © "); ok {
			return s
		}
	}
	log.Fatal("no copyright in the license of the crate")
	return ""
}

func main() {
	files := crateFiles()
	license := string(files["LICENSE"])
	if !strings.HasPrefix(license, "UNICODE LICENSE V3") {
		log.Fatal("the license of the crate is not the Unicode License v3")
	}
	var o output
	fmt.Fprintf(&o.b, "// SPDX-License-Identifier: Unicode-3.0\n// SPDX-FileCopyrightText: %s\n\n", copyright(license))
	fmt.Fprintf(&o.b, "// Code generated by gen.go from icu_calendar %s. DO NOT EDIT.\n\npackage icu4x\n", crateVersion)
	o.eastAsian(files, "src/cal/east_asian_traditional/china_data.rs", "China", "holds the Chinese years computed by the rules of GB/T 33661-2017")
	o.eastAsian(files, "src/cal/east_asian_traditional/korea_data.rs", "Korea", "holds the Korean years confirmed by KASI")
	o.eastAsian(files, "src/cal/east_asian_traditional/qing_data.rs", "Qing", "holds the years of the late Qing dynasty, for both the Chinese and the Korean calendar")
	o.hijri(files, "src/cal/hijri/ummalqura_data.rs")
	b, err := format.Source(o.b.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("years.go", b, 0o644); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("LICENSE", []byte(license), 0o644); err != nil {
		log.Fatal(err)
	}
}
