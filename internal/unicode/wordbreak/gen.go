// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build ignore

// This program generates the Thai and Han word tries from ICU4C.
package main

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/unicode/datafile"
)

type node struct {
	cost     byte
	children map[rune]*node
}

// encodeTrie numbers nodes breadth-first, with each node's children ordered by rune.
// The children of adjacent parents then occupy consecutive ranges.
func encodeTrie(root *node) ([]byte, error) {
	nodes := []*node{root}
	var labels, costs, counts []byte
	labels = binary.LittleEndian.AppendUint16(labels, 0)
	for i := 0; i < len(nodes); i++ {
		n := nodes[i]
		costs = append(costs, n.cost)
		counts = binary.LittleEndian.AppendUint16(counts, uint16(len(n.children)))
		for _, r := range slices.Sorted(maps.Keys(n.children)) {
			if r > 0xffff {
				return nil, fmt.Errorf("dictionary edge outside the BMP: %U", r)
			}
			labels = binary.LittleEndian.AppendUint16(labels, uint16(r))
			nodes = append(nodes, n.children[r])
		}
	}

	var out bytes.Buffer
	w, err := flate.NewWriter(&out, flate.BestCompression)
	if err != nil {
		return nil, err
	}
	for _, column := range [][]byte{labels, costs, counts} {
		if _, err := w.Write(column); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func generate(filename, out string, weighted bool) error {
	src, err := datafile.ReadICU("icu4c/source/data/brkitr/dictionaries/" + filename)
	if err != nil {
		return err
	}
	var root node
	var words int
	for line := range strings.Lines(string(bytes.TrimPrefix(src, []byte("\uFEFF")))) {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		word := fields[0]
		cost := uint64(1)
		if weighted {
			if strings.IndexFunc(word, func(r rune) bool { return !unicode.Is(unicode.Han, r) }) >= 0 {
				continue
			}
			cost, err = strconv.ParseUint(fields[1], 10, 8)
			if err != nil || cost == 0 {
				return fmt.Errorf("invalid word cost in %q", line)
			}
		}
		if utf8.RuneCountInString(word) > 255 {
			return fmt.Errorf("word too long: %q", word)
		}
		n := &root
		for _, r := range word {
			if n.children == nil {
				n.children = make(map[rune]*node)
			}
			if n.children[r] == nil {
				n.children[r] = new(node)
			}
			n = n.children[r]
		}
		n.cost = byte(cost)
		words++
	}
	data, err := encodeTrie(&root)
	if err != nil {
		return err
	}
	log.Printf("%s: %d words, %d bytes", filename, words, len(data))
	return os.WriteFile(out, data, 0o644)
}

func main() {
	if err := generate("thaidict.txt", "thai.deflate", false); err != nil {
		log.Fatal(err)
	}
	if err := generate("cjdict.txt", "han.deflate", true); err != nil {
		log.Fatal(err)
	}
}
