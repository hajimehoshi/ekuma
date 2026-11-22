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

// Run tests Intl.Segmenter and String.prototype.normalize against data in an ICU checkout.
func Run(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping ICU tests in short mode")
	}
	dir := os.Getenv("EKUMA_ICU")
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			t.Skip("no user cache directory; set EKUMA_ICU to an ICU checkout")
		}
		dir = filepath.Join(cache, "ekuma", "icu")
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			t.Skipf("no ICU checkout at %s; set EKUMA_ICU", dir)
		} else if err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"Grapheme", "Word", "Sentence"} {
		file := kind + "BreakTest.txt"
		t.Run(file, func(t *testing.T) {
			runBreakTest(t, filepath.Join(dir, "icu4c", "source", "test", "testdata", file), strings.ToLower(kind))
		})
	}
	t.Run("NormalizationTest.txt", func(t *testing.T) {
		runNormalizationTest(t, filepath.Join(dir, "icu4c", "source", "data", "unidata", "NormalizationTest.txt"))
	})
}

func runBreakTest(t *testing.T, path, granularity string) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var vm ekuma.VM
	script, err := vm.ParseScript("break-test.js", `
		const segmenter = new Intl.Segmenter("en", {granularity: `+strconv.Quote(granularity)+`});
		codePoints => {
			const input = String.fromCodePoint(...codePoints);
			const boundaries = [];
			let end = 0;
			for (const {index, segment} of segmenter.segment(input)) {
				boundaries.push(index);
				end = index + segment.length;
			}
			boundaries.push(end);
			return boundaries.join(" ");
		}
	`)
	if err != nil {
		t.Fatal(err)
	}
	boundaries, err := script.Evaluate(t.Context())
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

// parseBreakTest returns the code points and expected UTF-16 boundary indices for a row.
func parseBreakTest(fields []string) ([]any, string, error) {
	if len(fields)%2 != 1 || fields[0] != "÷" || fields[len(fields)-1] != "÷" {
		return nil, "", fmt.Errorf("expected alternating break markers and code points with a break at both ends")
	}
	var codePoints []any
	var offsets []string
	var offset int
	for i, field := range fields {
		if i%2 == 0 {
			switch field {
			case "÷":
				offsets = append(offsets, strconv.Itoa(offset))
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
	return codePoints, strings.Join(offsets, " "), nil
}
