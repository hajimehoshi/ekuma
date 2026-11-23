// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build ignore

// This program checks the ICU inventory against the two pinned Git trees.
package main

import (
	"embed"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

//go:embed upstream-*.tsv testdata/*-revision.txt
var inventory embed.FS

func readTSV(name string) []map[string]string {
	f, err := inventory.Open(name)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Comma = '\t'
	header, err := r.Read()
	if err != nil {
		log.Fatalf("%s: %v", name, err)
	}
	records, err := r.ReadAll()
	if err != nil {
		log.Fatalf("%s: %v", name, err)
	}
	rows := make([]map[string]string, 0, len(records))
	for _, record := range records {
		row := make(map[string]string, len(header))
		for i, column := range header {
			row[column] = record[i]
		}
		rows = append(rows, row)
	}
	return rows
}

func checkSubtests(rows []map[string]string) error {
	adapters := map[string]bool{}
	for _, row := range readTSV("upstream-adapters.tsv") {
		adapters[row["TestICU subtest"]] = true
	}
	referenced := map[string]bool{}
	for _, row := range rows {
		for name := range strings.SplitSeq(row["TestICU subtests"], ", ") {
			if name != "-" {
				referenced[name] = true
			}
		}
	}
	for name := range referenced {
		if adapters[name] {
			delete(adapters, name)
		} else {
			adapters[name] = true
		}
	}
	if len(adapters) > 0 {
		return fmt.Errorf("unmatched TestICU subtests: %s", strings.Join(slices.Sorted(maps.Keys(adapters)), ", "))
	}
	return nil
}

var testPath = regexp.MustCompile(`(?i)/[^/]*(?:tests?|tst)(?:/|[._-]|$)|/[^/]*Test\.java$`)

func needsClassification(name string) bool {
	for _, dir := range []string{
		"icu4c/source/test/",
		"icu4c/source/data/unidata/",
		"icu4c/source/python/icutools/databuilder/test/",
		"icu4c/source/tools/ctestfw/",
		"icu4c/source/tools/gentest/",
		"icu4j/perf-tests/",
	} {
		if strings.HasPrefix(name, dir) {
			return true
		}
	}
	if strings.HasPrefix(name, "icu4j/") && strings.Contains(name, "/src/test/") {
		return true
	}
	return (strings.HasPrefix(name, "icu4c/") || strings.HasPrefix(name, "icu4j/")) && testPath.MatchString(name)
}

func readTree(repo, pin string) (map[string]string, error) {
	cmd := exec.Command("git", "-C", repo, "ls-tree", "-rz", pin)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	blobs := map[string]string{}
	for entry := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		meta, name, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("invalid ls-tree entry %q", entry)
		}
		blobs[name] = fields[2]
	}
	return blobs, nil
}

func checkTree(repo, version, pinFile string, rows []map[string]string) error {
	data, err := inventory.ReadFile("testdata/" + pinFile)
	if err != nil {
		return err
	}
	pin := strings.TrimSpace(string(data))
	blobs, err := readTree(repo, pin)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	fixtureBlobs := map[string]bool{}
	var suiteSources, fixtures int
	var problems []error
	for _, row := range rows {
		pattern := row["ICU path"]
		var matches []string
		if strings.Contains(pattern, "*") {
			for name := range blobs {
				match, err := path.Match(pattern, name)
				if err != nil {
					return err
				}
				if match {
					matches = append(matches, name)
				}
			}
			slices.Sort(matches)
		} else if _, ok := blobs[pattern]; ok {
			matches = append(matches, pattern)
		}
		want, err := strconv.Atoi(row[version+" paths"])
		if err != nil {
			return fmt.Errorf("%s: %w", pattern, err)
		}
		if len(matches) != want {
			problems = append(problems, fmt.Errorf("%s: expected %d paths, found %d", pattern, want, len(matches)))
		}
		for _, name := range matches {
			if seen[name] {
				problems = append(problems, fmt.Errorf("%s: classified twice", name))
			}
			seen[name] = true
			switch row["kind"] {
			case "suite source":
				suiteSources++
			case "fixture":
				fixtures++
				fixtureBlobs[blobs[name]] = true
			}
			other := row["registration or corresponding path"]
			switch row["disposition"] {
			case "same blob", version + " same blob":
				if blobs[name] != blobs[other] {
					problems = append(problems, fmt.Errorf("%s: blob differs from %s", name, other))
				}
			case "same rows", "compiled":
				if _, ok := blobs[other]; !ok {
					problems = append(problems, fmt.Errorf("%s: missing counterpart %s", name, other))
				}
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(blobs)) {
		if !seen[name] && needsClassification(name) {
			problems = append(problems, fmt.Errorf("%s: unclassified", name))
		}
	}
	fmt.Printf("ICU %s %s: %d tree entries, %d indexed, %d suite sources, %d fixture paths, %d distinct fixture blobs\n",
		version, pin, len(blobs), len(seen), suiteSources, fixtures, len(fixtureBlobs))
	return errors.Join(problems...)
}

func main() {
	log.SetFlags(0)
	if len(os.Args) != 3 {
		log.Fatal("usage: go run verify_inventory.go ICU_REPO ICU_CLDR_REPO")
	}
	rows := readTSV("upstream-paths.tsv")
	if err := checkSubtests(rows); err != nil {
		log.Fatal(err)
	}
	err := errors.Join(
		checkTree(os.Args[1], "78.3", "icu-revision.txt", rows),
		checkTree(os.Args[2], "79.1", "icu-cldr-revision.txt", rows),
	)
	if err != nil {
		log.Fatal(err)
	}
}
