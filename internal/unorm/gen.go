// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build ignore

// This program builds Unicode normalization tables from the UCD in the pinned ICU release.
package main

import (
	"bytes"
	"encoding/binary"
	"log"
	"maps"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/unicode/icu"
)

func read(name string) string {
	b, err := icu.ReadFile("icu4c/source/data/unidata/" + name)
	if err != nil {
		log.Fatal(err)
	}
	return string(b)
}

func number(s string, base int) uint32 {
	n, err := strconv.ParseUint(s, base, 32)
	if err != nil {
		log.Fatal(err)
	}
	return uint32(n)
}

func readDerivedProperties(properties []uint32) map[uint32]bool {
	// Low byte: CCC. Next four bits: non-Yes quick checks for NFC, NFD,
	// NFKC, NFKD. Bit 12: compatibility mapping. High half: mapping offset.
	excluded := map[uint32]bool{}
	checks := map[string]uint32{
		"NFC_QC":  1 << 8,
		"NFD_QC":  1 << 9,
		"NFKC_QC": 1 << 10,
		"NFKD_QC": 1 << 11,
	}
	for line := range strings.Lines(read("DerivedNormalizationProps.txt")) {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Split(line, ";")
		if len(fields) < 2 {
			continue
		}
		property := strings.TrimSpace(fields[1])
		mask := checks[property]
		if mask == 0 && property != "Full_Composition_Exclusion" {
			continue
		}
		lo, hi, ranged := strings.Cut(strings.TrimSpace(fields[0]), "..")
		if !ranged {
			hi = lo
		}
		if mask != 0 && (len(fields) < 3 || (strings.TrimSpace(fields[2]) != "M" && strings.TrimSpace(fields[2]) != "N")) {
			log.Fatalf("unexpected quick check: %s", line)
		}
		for r, end := number(lo, 16), number(hi, 16); r <= end; r++ {
			if mask != 0 {
				properties[r] |= mask
			} else {
				excluded[r] = true
			}
		}
	}
	return excluded
}

func readMappings(properties []uint32, excluded map[uint32]bool) ([]byte, map[uint64]uint32) {
	mappings := []byte{0}
	offsets := map[string]uint32{}
	compositions := map[uint64]uint32{}
	for line := range strings.Lines(read("UnicodeData.txt")) {
		fields := strings.Split(line, ";")
		r := number(fields[0], 16)
		properties[r] |= number(fields[3], 10)
		parts := strings.Fields(fields[5])
		if len(parts) == 0 {
			continue
		}
		compat := strings.HasPrefix(parts[0], "<")
		if compat {
			properties[r] |= 1 << 12
			parts = parts[1:]
		}
		var mapping []rune
		for _, part := range parts {
			mapping = append(mapping, rune(number(part, 16)))
		}
		s := string(mapping)
		offset, ok := offsets[s]
		if !ok {
			if len(s) > math.MaxUint8 || len(mappings) > math.MaxUint16 {
				log.Fatal("decomposition mapping exceeds table format")
			}
			offset = uint32(len(mappings))
			offsets[s] = offset
			mappings = append(mappings, byte(len(s)))
			mappings = append(mappings, s...)
		}
		properties[r] |= offset << 16
		if !compat && len(mapping) == 2 && !excluded[r] {
			key := uint64(mapping[0])<<21 | uint64(mapping[1])
			compositions[key] = r
		}
	}
	return mappings, compositions
}

func encodeTables(properties []uint32, mappings []byte, compositions map[uint64]uint32) []byte {
	// Each page of 256 code points refers to a shared block of properties.
	var index, blocks []byte
	blockIDs := map[[256]uint32]uint16{}
	for block := range slices.Chunk(properties, 256) {
		key := [256]uint32(block)
		id, ok := blockIDs[key]
		if !ok {
			id = uint16(len(blockIDs))
			blockIDs[key] = id
			for _, prop := range block {
				blocks = binary.LittleEndian.AppendUint32(blocks, prop)
			}
		}
		index = binary.LittleEndian.AppendUint16(index, id)
	}
	var pairs []byte
	for _, key := range slices.Sorted(maps.Keys(compositions)) {
		pairs = binary.LittleEndian.AppendUint64(pairs, key)
		pairs = binary.LittleEndian.AppendUint32(pairs, compositions[key])
	}
	// Header: byte offsets of the mapping strings and composition pairs.
	var header []byte
	start := 8 + len(index) + len(blocks)
	header = binary.LittleEndian.AppendUint32(header, uint32(start))
	header = binary.LittleEndian.AppendUint32(header, uint32(start+len(mappings)))
	return bytes.Join([][]byte{header, index, blocks, mappings, pairs}, nil)
}

func main() {
	properties := make([]uint32, utf8.MaxRune+1)
	excluded := readDerivedProperties(properties)
	mappings, compositions := readMappings(properties, excluded)
	data := encodeTables(properties, mappings, compositions)
	if err := os.WriteFile("tables.bin", data, 0o644); err != nil {
		log.Fatal(err)
	}
}
