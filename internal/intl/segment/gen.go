// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build ignore

// This program generates tables.go and properties.bin: the break properties of UAX #29 from
// the Unicode Character Database. Run it with go generate.
package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"go/format"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const version = "18.0.0"

const base = "https://www.unicode.org/Public/" + version + "/ucd/"

func fetch(path string) []string {
	resp, err := http.Get(base + path)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("%s: %s", path, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		l := sc.Text()
		if i := strings.IndexByte(l, '#'); i >= 0 {
			l = l[:i]
		}
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if err := sc.Err(); err != nil {
		log.Fatal(err)
	}
	return lines
}

func codeRange(s string) (rune, rune) {
	lo, hi, ok := strings.Cut(s, "..")
	if !ok {
		hi = lo
	}
	a, err := strconv.ParseUint(lo, 16, 32)
	if err != nil {
		log.Fatal(err)
	}
	b, err := strconv.ParseUint(hi, 16, 32)
	if err != nil {
		log.Fatal(err)
	}
	return rune(a), rune(b)
}

// The values of the properties, in the order of the constants of package segment.
var (
	graphemeValues = []string{"Other", "CR", "LF", "Control", "Extend", "ZWJ", "Regional_Indicator", "Prepend", "SpacingMark", "L", "V", "T", "LV", "LVT"}
	wordValues     = []string{"Other", "CR", "LF", "Newline", "Extend", "ZWJ", "Regional_Indicator", "Format", "Katakana", "Hebrew_Letter", "ALetter", "Single_Quote", "Double_Quote", "MidNumLet", "MidLetter", "MidNum", "Numeric", "ExtendNumLet", "WSegSpace"}
	sentenceValues = []string{"Other", "CR", "LF", "Extend", "Sep", "Format", "Sp", "Lower", "Upper", "OLetter", "Numeric", "ATerm", "SContinue", "STerm", "Close"}
	incbValues     = []string{"None", "Consonant", "Linker", "Extend"}
)

func index(values []string, v string) uint32 {
	for i, s := range values {
		if s == v {
			return uint32(i)
		}
	}
	log.Fatalf("unknown value %s", v)
	return 0
}

func main() {
	props := make([]uint32, 0x110000)
	set := func(path string, shift int, values []string, field int, filter string) {
		for _, l := range fetch(path) {
			f := strings.Split(l, ";")
			for i := range f {
				f[i] = strings.TrimSpace(f[i])
			}
			if filter != "" && (len(f) < 2 || f[1] != filter) {
				continue
			}
			if len(f) <= field {
				continue
			}
			v := index(values, f[field])
			lo, hi := codeRange(f[0])
			for c := lo; c <= hi; c++ {
				props[c] |= v << shift
			}
		}
	}
	set("auxiliary/GraphemeBreakProperty.txt", 0, graphemeValues, 1, "")
	set("auxiliary/WordBreakProperty.txt", 4, wordValues, 1, "")
	set("auxiliary/SentenceBreakProperty.txt", 9, sentenceValues, 1, "")
	for _, l := range fetch("emoji/emoji-data.txt") {
		f := strings.Split(l, ";")
		if strings.TrimSpace(f[1]) != "Extended_Pictographic" {
			continue
		}
		lo, hi := codeRange(strings.TrimSpace(f[0]))
		for c := lo; c <= hi; c++ {
			props[c] |= 1 << 13
		}
	}
	set("DerivedCoreProperties.txt", 14, incbValues, 2, "InCB")

	// Word-like status and dictionary selection also depend on category and script.
	category := make([]byte, len(props))
	for _, l := range fetch("extracted/DerivedGeneralCategory.txt") {
		codes, value, _ := strings.Cut(l, ";")
		group := strings.TrimSpace(value)[0]
		lo, hi := codeRange(strings.TrimSpace(codes))
		for c := lo; c <= hi; c++ {
			category[c] = group
			if group == 'L' || group == 'N' {
				props[c] |= 1 << 16
			}
		}
	}
	for _, l := range fetch("Scripts.txt") {
		codes, value, _ := strings.Cut(l, ";")
		script := strings.TrimSpace(value)
		if script != "Thai" && script != "Han" {
			continue
		}
		lo, hi := codeRange(strings.TrimSpace(codes))
		for c := lo; c <= hi; c++ {
			switch script {
			case "Thai":
				props[c] |= 1 << 17
				if 0x0e01 <= c && c <= 0x0e4e && category[c] == 'L' {
					props[c] |= 1 << 18
				}
			case "Han":
				if category[c] != 'M' {
					props[c] |= 2 << 18
				}
			}
		}
	}

	// Each leaf entry indexes one distinct combination of properties.
	values := []uint32{0}
	valueIndex := map[uint32]byte{0: 0}
	indices := make([]byte, len(props))
	for c, p := range props {
		i, ok := valueIndex[p]
		if !ok {
			if len(values) >= 256 {
				log.Fatal("too many combinations of break properties")
			}
			i = byte(len(values))
			valueIndex[p] = i
			values = append(values, p)
		}
		indices[c] = i
	}

	// A trie: stage 1 by code point >> 10, stage 2 blocks of 32 by (code point >> 5) & 31,
	// and leaves of 32 values.
	var leaves [][32]byte
	leafIndex := map[[32]byte]uint16{}
	var mids [][32]uint16
	midIndex := map[[32]uint16]uint16{}
	var stage1 []uint16
	for hi := 0; hi < 0x110000; hi += 1 << 10 {
		var mid [32]uint16
		for m := range 32 {
			var leaf [32]byte
			copy(leaf[:], indices[hi+m<<5:hi+m<<5+32])
			li, ok := leafIndex[leaf]
			if !ok {
				li = uint16(len(leaves))
				leaves = append(leaves, leaf)
				leafIndex[leaf] = li
			}
			mid[m] = li
		}
		mi, ok := midIndex[mid]
		if !ok {
			mi = uint16(len(mids))
			mids = append(mids, mid)
			midIndex[mid] = mi
		}
		stage1 = append(stage1, mi)
	}
	var bin []byte
	for _, v := range stage1 {
		bin = binary.LittleEndian.AppendUint16(bin, v)
	}
	midOff := len(bin)
	for _, m := range mids {
		for _, v := range m {
			bin = binary.LittleEndian.AppendUint16(bin, v)
		}
	}
	leafOff := len(bin)
	for _, l := range leaves {
		bin = append(bin, l[:]...)
	}
	if err := os.WriteFile("properties.bin", bin, 0o644); err != nil {
		log.Fatal(err)
	}

	var b bytes.Buffer
	b.WriteString("// SPDX-License-Identifier: 0BSD\n// SPDX-FileCopyrightText: 2026 Hajime Hoshi\n\n")
	b.WriteString("// Code generated by gen.go from the Unicode Character Database " + version + ". DO NOT EDIT.\n\n")
	b.WriteString("package segment\n\nimport _ \"embed\"\n\n")
	b.WriteString("// properties holds the trie of the break properties: the uint16 stage 1 at offset 0, the\n")
	b.WriteString("// uint16 stage 2 at propertiesStage2, and the byte indices into propertyValues at propertiesLeaves.\n")
	b.WriteString("//\n//go:embed properties.bin\nvar properties string\n\n")
	fmt.Fprintf(&b, "const (\n\tpropertiesStage2 = %d\n\tpropertiesLeaves = %d\n)\n", midOff, leafOff)
	b.WriteString("\nvar propertyValues = [...]uint32{\n")
	for _, v := range values {
		fmt.Fprintf(&b, "%#x,\n", v)
	}
	b.WriteString("}\n")
	src, err := format.Source(b.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("tables.go", src, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("%d bytes (%d leaves, %d mids)", len(bin), len(leaves), len(mids))
}
