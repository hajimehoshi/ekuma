// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package icutest runs ICU test data against Ekuma.
package icutest

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/hajimehoshi/ekuma"
	_ "github.com/hajimehoshi/ekuma/intl"
)

// Run tests Ekuma against the available ICU fixtures.
func Run(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping ICU tests in short mode")
	}
	dir := checkoutDir(t)
	if dir == "" {
		t.Skip("no ICU checkout available")
	}
	locales, fixtures := readFailures(t)
	testdata := filepath.Join(dir, "icu4c", "source", "test", "testdata")
	unidata := filepath.Join(dir, "icu4c", "source", "data", "unidata")
	runBreakTests(t, testdata)
	t.Run("casing.txt", func(t *testing.T) {
		runCasingTest(t, filepath.Join(testdata, "casing.txt"))
	})
	t.Run("rbbitst.txt", func(t *testing.T) {
		runRBBITest(t, filepath.Join(testdata, "rbbitst.txt"), filepath.Join(unidata, "UnicodeData.txt"), fixtures["rbbitst.txt"])
	})
	t.Run("CaseFolding.txt", func(t *testing.T) {
		runCaseFoldingTest(t, filepath.Join(unidata, "CaseFolding.txt"))
	})
	for _, file := range []string{"emoji-sequences.txt", "emoji-zwj-sequences.txt"} {
		t.Run(file, func(t *testing.T) {
			runEmojiTest(t, filepath.Join(unidata, file))
		})
	}
	t.Run("NormalizationTest.txt", func(t *testing.T) {
		runNormalizationTest(t, filepath.Join(unidata, "NormalizationTest.txt"))
	})
	localeDir := filepath.Join(testdata, "cldr", "localeIdentifiers")
	t.Run("likelySubtags.txt", func(t *testing.T) {
		runLikelySubtagsTest(t, filepath.Join(localeDir, "likelySubtags.txt"), locales)
	})
	t.Run("localeCanonicalization.txt", func(t *testing.T) {
		runLocaleCanonicalizationTest(t, filepath.Join(localeDir, "localeCanonicalization.txt"), locales)
	})
	for _, alternate := range []string{"NON_IGNORABLE", "SHIFTED"} {
		file := "CollationTest_" + alternate + "_SHORT.txt"
		t.Run(file, func(t *testing.T) {
			runOrderedCollation(t, filepath.Join(testdata, file), alternate == "SHIFTED", fixtures[file])
		})
	}
	const file = "collationtest.txt"
	t.Run(file, func(t *testing.T) {
		runCollationTest(t, filepath.Join(testdata, file), fixtures[file])
	})
}

const failuresFile = "internal/icutest/testdata/icu-failures.txt"

func readFailures(t *testing.T) (localeFailures, map[string]map[int]string) {
	t.Helper()
	data, err := os.ReadFile(failuresFile)
	if err != nil {
		t.Fatal(err)
	}
	locales := localeFailures{
		"maximize":                 {},
		"minimize":                 {},
		"Intl.getCanonicalLocales": {},
	}
	fixtures := map[string]map[int]string{
		"rbbitst.txt":                           {},
		"CollationTest_NON_IGNORABLE_SHORT.txt": {},
		"CollationTest_SHIFTED_SHORT.txt":       {},
		"collationtest.txt":                     {},
	}
	for i, row := range strings.Split(string(data), "\n") {
		row, _, _ = strings.Cut(row, "#")
		fields := strings.Fields(row)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 {
			t.Fatalf("%s:%d: expected an operation or fixture, a case and a known result, got %q", failuresFile, i+1, row)
		}
		name, key, result := fields[0], fields[1], fields[2]
		if entries, ok := locales[name]; ok {
			if _, ok := entries[key]; ok {
				t.Fatalf("%s:%d: duplicate entry for %s(%q)", failuresFile, i+1, name, key)
			}
			entries[key] = result
			continue
		}
		entries, ok := fixtures[name]
		if !ok {
			t.Fatalf("%s:%d: unknown operation or fixture %q", failuresFile, i+1, name)
		}
		line, err := strconv.Atoi(key)
		if err != nil || line <= 0 {
			t.Fatalf("%s:%d: invalid line %q for %s", failuresFile, i+1, key, name)
		}
		if _, ok := entries[line]; ok {
			t.Fatalf("%s:%d: duplicate entry for %s:%d", failuresFile, i+1, name, line)
		}
		entries[line] = result
	}
	return locales, fixtures
}

func checkoutDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("EKUMA_ICU"); dir != "" {
		return dir
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Logf("no user cache directory; set EKUMA_ICU to an ICU checkout")
		return ""
	}
	dir := filepath.Join(cache, "ekuma", "icu")
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		t.Logf("no ICU checkout at %s; set EKUMA_ICU", dir)
		return ""
	} else if err != nil {
		t.Fatal(err)
	}
	return dir
}

func runBreakTests(t *testing.T, dir string) {
	for _, kind := range []string{"Grapheme", "Word", "Sentence"} {
		file := kind + "BreakTest.txt"
		t.Run(file, func(t *testing.T) {
			runBreakTest(t, filepath.Join(dir, file), strings.ToLower(kind))
		})
	}
}

func runBreakTest(t *testing.T, path, granularity string) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var vm ekuma.VM
	boundaries, err := segmenterFactory(t, &vm).Invoke(t.Context(), "en", granularity)
	if err != nil {
		t.Fatal(err)
	}

	var cases, line int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line++
		data, _, _ := strings.Cut(scanner.Text(), "#")
		fields := strings.Fields(data)
		if len(fields) == 0 {
			continue
		}
		codePoints, want, err := parseBreakTest(fields)
		if err != nil {
			t.Fatalf("%s:%d: %v", path, line, err)
		}
		got, err := boundaries.Invoke(t.Context(), codePoints)
		if err != nil {
			t.Fatalf("%s:%d: %v", path, line, err)
		}
		if got.String() != want {
			t.Errorf("%s:%d: %s: got [%s], want [%s]", path, line, data, got.String(), want)
		}
		cases++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if cases == 0 {
		t.Fatalf("%s contains no break tests", path)
	}
	t.Logf("%d cases", cases)
}

func segmenterFactory(t *testing.T, vm *ekuma.VM) ekuma.Value {
	t.Helper()
	script, err := vm.ParseScript("break-test.js", `
		(locale, granularity) => {
			const segmenter = new Intl.Segmenter(locale, {granularity});
			return codePoints => {
				const input = String.fromCodePoint(...codePoints);
				const boundaries = [];
				let end = 0;
				for (const {index, segment} of segmenter.segment(input)) {
					boundaries.push(index);
					end = index + segment.length;
				}
				boundaries.push(end);
				return boundaries.join(" ");
			};
		}
	`)
	if err != nil {
		t.Fatal(err)
	}
	factory, err := script.Evaluate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return factory
}

// parseBreakTest returns the code points and expected UTF-16 boundary indices for a row.
func parseBreakTest(fields []string) ([]any, string, error) {
	if len(fields)%2 != 1 || fields[0] != "÷" || fields[len(fields)-1] != "÷" {
		return nil, "", fmt.Errorf("expected alternating break markers and code points with a break at both ends")
	}
	var codePoints []any
	var offsets []int
	var offset int
	for i, field := range fields {
		if i%2 == 0 {
			switch field {
			case "÷":
				offsets = append(offsets, offset)
			case "×":
			default:
				return nil, "", fmt.Errorf("invalid break marker %q", field)
			}
			continue
		}
		cp, err := strconv.ParseUint(field, 16, 32)
		if err != nil || cp > unicode.MaxRune {
			return nil, "", fmt.Errorf("invalid code point %q", field)
		}
		codePoints = append(codePoints, uint32(cp))
		offset++
		if cp > 0xffff {
			offset++
		}
	}
	return codePoints, formatBreakOffsets(offsets), nil
}

func formatBreakOffsets(offsets []int) string {
	var b strings.Builder
	for i, offset := range offsets {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strconv.Itoa(offset))
	}
	return b.String()
}
