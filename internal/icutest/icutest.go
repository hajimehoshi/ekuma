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
	dir := checkoutDir(t, "EKUMA_ICU", "icu")
	cldrDir := checkoutDir(t, "EKUMA_ICU_CLDR", "icu-cldr")
	if dir == "" && cldrDir == "" {
		t.Skip("no ICU checkout available")
	}
	locales, fixtures := readFailures(t)
	if dir != "" {
		testdata := filepath.Join(dir, "icu4c", "source", "test", "testdata")
		unidata := filepath.Join(dir, "icu4c", "source", "data", "unidata")
		runBreakTests(t, testdata)
		t.Run("casing.txt", func(t *testing.T) {
			runCasingTest(t, filepath.Join(testdata, "casing.txt"))
		})
		t.Run("strcase.cpp", func(t *testing.T) {
			runArmenianUpper(t, filepath.Join(dir, "icu4c/source/test/intltest/strcase.cpp"))
		})
		for _, file := range []string{"UnicodeData.txt", "SpecialCasing.txt"} {
			t.Run(file, func(t *testing.T) {
				runUnicodeCasing(t, unidata, file)
			})
		}
		t.Run("rbbitst.txt", func(t *testing.T) {
			runRBBITest(t, filepath.Join(testdata, "rbbitst.txt"), filepath.Join(unidata, "UnicodeData.txt"), fixtures["rbbitst.txt"])
		})
		t.Run("CaseFolding.txt", func(t *testing.T) {
			runCaseFoldingTest(t, filepath.Join(unidata, "CaseFolding.txt"))
		})
		t.Run("regextst.txt", func(t *testing.T) {
			runTaggedCaptures(t, filepath.Join(testdata, "regextst.txt"))
		})
		t.Run("re_tests.txt", func(t *testing.T) {
			runPerlCaptures(t, filepath.Join(testdata, "re_tests.txt"))
		})
		t.Run("DerivedCoreProperties.txt", func(t *testing.T) {
			runIdentifierBoundaries(t, filepath.Join(unidata, "DerivedCoreProperties.txt"))
		})
		for _, file := range []string{"emoji-sequences.txt", "emoji-zwj-sequences.txt"} {
			t.Run(file, func(t *testing.T) {
				runEmojiTest(t, filepath.Join(unidata, file))
			})
		}
		currentNormalization := filepath.Join(unidata, "NormalizationTest.txt")
		t.Run("NormalizationTest.txt", func(t *testing.T) {
			runNormalizationTest(t, currentNormalization, nil)
		})
		t.Run("NormalizationTest-3.2.0.txt", func(t *testing.T) {
			seen := map[[5]string]bool{}
			forEachNormalizationRow(t, currentNormalization, func(_ int, columns [5]string) {
				seen[columns] = true
			})
			runNormalizationTest(t, filepath.Join(testdata, "NormalizationTest-3.2.0.txt"), seen)
		})
		for _, alternate := range []string{"NON_IGNORABLE", "SHIFTED"} {
			file := "CollationTest_" + alternate + "_SHORT.txt"
			t.Run(file, func(t *testing.T) {
				runOrderedCollation(t, filepath.Join(testdata, file), alternate == "SHIFTED", fixtures[file])
			})
		}
		t.Run("locale-collation", func(t *testing.T) {
			runLocaleCollation(t, dir, fixtures)
		})
		const file = "collationtest.txt"
		t.Run(file, func(t *testing.T) {
			runCollationTest(t, filepath.Join(testdata, file), fixtures[file])
		})
	}
	if cldrDir != "" {
		testdata := filepath.Join(cldrDir, "icu4c", "source", "test", "testdata")
		localeDir := filepath.Join(testdata, "cldr", "localeIdentifiers")
		misc := filepath.Join(cldrDir, "icu4c", "source", "data", "misc")
		t.Run("numberpermutationtest.txt", func(t *testing.T) {
			runNumberPermutations(t, filepath.Join(testdata, "numberpermutationtest.txt"))
		})
		t.Run("CompactDecimalFormatTest.java", func(t *testing.T) {
			runCompactSerbian(t, filepath.Join(cldrDir, "icu4j/main/common_tests/src/test/java/com/ibm/icu/dev/test/format/CompactDecimalFormatTest.java"))
		})
		t.Run("currtest.c", func(t *testing.T) {
			runCurrencyFractionDigits(t, filepath.Join(cldrDir, "icu4c/source/test/cintltst/currtest.c"))
		})
		t.Run("NumberFormatTestCases.txt", func(t *testing.T) {
			runNumberCases(t, filepath.Join(testdata, "NumberFormatTestCases.txt"))
		})
		t.Run("dcfmtest.txt", func(t *testing.T) {
			runDecimalFormat(t, filepath.Join(testdata, "dcfmtest.txt"))
		})
		t.Run("calendar.txt", func(t *testing.T) {
			runCalendar(t, filepath.Join(testdata, "calendar.txt"))
		})
		t.Run("format.txt", func(t *testing.T) {
			runDateFormat(t, filepath.Join(testdata, "format.txt"), fixtures["format.txt"])
		})
		t.Run("numberformattestspecification.txt", func(t *testing.T) {
			runNumberSpecification(t, filepath.Join(testdata, "numberformattestspecification.txt"), fixtures["numberformattestspecification.txt"])
		})
		javaFormat := filepath.Join(cldrDir, "icu4j/main/core/src/test/resources/com/ibm/icu/dev/test/format")
		t.Run("icu4j-plurals.txt", func(t *testing.T) {
			runPluralPhrases(t, filepath.Join(javaFormat, "plurals.txt"), filepath.Join(misc, "plurals.txt"))
		})
		t.Run("icu4j-NumberFormatTestCases.txt", func(t *testing.T) {
			runNumberCases(t, filepath.Join(javaFormat, "NumberFormatTestCases.txt"))
		})
		t.Run("localeDisplayName.txt", func(t *testing.T) {
			runDisplayNames(t, filepath.Join(localeDir, "localeDisplayName.txt"))
		})
		t.Run("display-names", func(t *testing.T) {
			runDisplayNameComponents(t, cldrDir)
		})
		t.Run("plurals.txt", func(t *testing.T) {
			runPluralSamples(t, filepath.Join(misc, "plurals.txt"))
		})
		t.Run("pluralRanges.txt", func(t *testing.T) {
			runPluralRanges(t, filepath.Join(misc, "pluralRanges.txt"), filepath.Join(misc, "plurals.txt"))
		})
		t.Run("transitions", func(t *testing.T) {
			runTransitions(t, cldrDir)
		})
		t.Run("locale-info", func(t *testing.T) {
			runLocaleInfo(t, cldrDir)
		})
		t.Run("calendar-arithmetic", func(t *testing.T) {
			runCalendarArithmetic(t, cldrDir, fixtures)
		})
		runTimeZones(t, cldrDir, fixtures)
		runDatePatterns(t, cldrDir, fixtures)
		runDateIntervals(t, cldrDir, fixtures)
		t.Run("number-parts", func(t *testing.T) {
			runNumberParts(t, cldrDir)
		})
		runNumberRanges(t, cldrDir)
		runListFormats(t, cldrDir)
		t.Run("duration", func(t *testing.T) {
			runDurationFormats(t, cldrDir)
		})
		runRelativeTimeFormats(t, cldrDir)
		t.Run("caltestdata.h", func(t *testing.T) {
			runLunarMapping(t, filepath.Join(cldrDir, "icu4c/source/test/intltest/caltestdata.h"))
		})
		t.Run("localeMatcherTest.txt", func(t *testing.T) {
			runLocaleMatcher(t, filepath.Join(testdata, "localeMatcherTest.txt"), fixtures["localeMatcherTest.txt"])
		})
		t.Run("likelySubtags.txt", func(t *testing.T) {
			runLikelySubtagsTest(t, filepath.Join(localeDir, "likelySubtags.txt"), locales)
		})
		t.Run("localeCanonicalization.txt", func(t *testing.T) {
			runLocaleCanonicalizationTest(t, filepath.Join(localeDir, "localeCanonicalization.txt"), locales)
		})
	}
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
		"localeMatcherTest.txt":                 {},
		"rbbitst.txt":                           {},
		"format.txt":                            {},
		"numberformattestspecification.txt":     {},
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
		if len(fields) < 3 {
			t.Fatalf("%s:%d: expected an operation or fixture, a case and a known result, got %q", failuresFile, i+1, row)
		}
		name, key := fields[0], fields[1]
		tail := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(row), name))
		result := strings.TrimSpace(strings.TrimPrefix(tail, key))
		if strings.HasPrefix(result, "\"") {
			var err error
			result, err = strconv.Unquote(result)
			if err != nil {
				t.Fatalf("%s:%d: %v", failuresFile, i+1, err)
			}
		} else if len(fields) != 3 {
			t.Fatalf("%s:%d: expected one result, got %q", failuresFile, i+1, result)
		}
		if entries, ok := locales[name]; ok {
			if _, ok := entries[key]; ok {
				t.Fatalf("%s:%d: duplicate entry for %s(%q)", failuresFile, i+1, name, key)
			}
			entries[key] = result
			continue
		}
		entries, ok := fixtures[name]
		if fixture, _, hasOp := strings.Cut(name, "/"); hasOp && !ok {
			switch fixture {
			case "cmsccoll.c", "cdateintervalformattest.c", "dtifmtts.cpp", "dtptngts.cpp", "udatpg_test.c", "DateTimeGeneratorTest.java",
				"dtfmttst.cpp", "tztest.cpp", "TimeZoneFormatTest.java",
				"OrdinalMonthTest.java", "caltest.cpp", "incaltst.cpp", "supplementalData.txt":
				entries = map[int]string{}
				fixtures[name] = entries
				ok = true
			}
		}
		if !ok {
			t.Fatalf("%s:%d: unknown operation or fixture %q", failuresFile, i+1, name)
		}
		for k := range strings.SplitSeq(key, ",") {
			line, err := strconv.Atoi(k)
			if err != nil || line <= 0 {
				t.Fatalf("%s:%d: invalid line %q for %s", failuresFile, i+1, key, name)
			}
			if _, ok := entries[line]; ok {
				t.Fatalf("%s:%d: duplicate entry for %s:%d", failuresFile, i+1, name, line)
			}
			entries[line] = result
		}
	}
	return locales, fixtures
}

func checkoutDir(t *testing.T, env, name string) string {
	t.Helper()
	if dir := os.Getenv(env); dir != "" {
		return dir
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Logf("no user cache directory; set %s to an ICU checkout", env)
		return ""
	}
	dir := filepath.Join(cache, "ekuma", name)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		t.Logf("no ICU checkout at %s; set %s", dir, env)
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
	t.Run("emoji-test.txt", func(t *testing.T) {
		runEmojiBreakTest(t, filepath.Join(dir, "emoji-test.txt"))
	})
}

func runBreakTest(t *testing.T, path, granularity string) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var vm ekuma.VM
	check, err := segmenterFactory(t, &vm).Invoke(t.Context(), "en", granularity)
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
		if err := checkBreakRecords(t, check, codePoints, want); err != nil {
			t.Errorf("%s:%d: %s: %v", path, line, granularity, err)
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
			return (codePoints, offsets, wordLike) => {
				const input = String.fromCodePoint(...codePoints);
				if (offsets[0] !== 0 || offsets.at(-1) !== input.length) {
					throw new Error("fixture lacks an initial or final UTF-16 boundary");
				}
				const segments = segmenter.segment(input);
				const iterator = segments[Symbol.iterator]();
				// Look up at most three segment starts and two interior code units per row.
				const middle = Math.floor((offsets.length - 1) / 2);
				const lookups = new Set([0, offsets[middle], offsets.at(-2)]);
				const interiorEnd = offsets.find((end, i) => i > 0 && end - offsets[i - 1] > 1);
				if (interiorEnd !== undefined) lookups.add(interiorEnd - 1);
				const pair = input.search(/[\uD800-\uDBFF][\uDC00-\uDFFF]/);
				if (pair >= 0) lookups.add(pair + 1);

				function check(operation, index, got, want) {
					if (got && got.index === want.index && got.segment === want.segment &&
						got.input === want.input && (want.isWordLike === undefined ||
						got.isWordLike === want.isWordLike)) {
						return;
					}
					throw new Error(operation + " at UTF-16 index " + index + ": got " +
						JSON.stringify(got) + ", want " + JSON.stringify(want));
				}
				for (let i = 1; i < offsets.length; i++) {
					const start = offsets[i - 1], end = offsets[i];
					const want = {segment: input.slice(start, end), index: start, input};
					if (wordLike[i] !== null) want.isWordLike = wordLike[i];
					const step = iterator.next();
					check("iterator", start, step.done ? undefined : step.value, want);
					for (const index of lookups) {
						if (start <= index && index < end) {
							check("containing", index, segments.containing(index), want);
						}
					}
				}
				const extra = iterator.next();
				if (!extra.done) {
					throw new Error("iterator at UTF-16 index " + input.length +
						": got " + JSON.stringify(extra.value) + ", want end of iterator");
				}
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
func parseBreakTest(fields []string) ([]any, []breakBoundary, error) {
	if len(fields)%2 != 1 || fields[0] != "÷" || fields[len(fields)-1] != "÷" {
		return nil, nil, fmt.Errorf("expected alternating break markers and code points with a break at both ends")
	}
	var codePoints []any
	var boundaries []breakBoundary
	var offset int
	for i, field := range fields {
		if i%2 == 0 {
			switch field {
			case "÷":
				boundaries = append(boundaries, breakBoundary{
					offset: offset,
				})
			case "×":
			default:
				return nil, nil, fmt.Errorf("invalid break marker %q", field)
			}
			continue
		}
		cp, err := strconv.ParseUint(field, 16, 32)
		if err != nil || cp > unicode.MaxRune {
			return nil, nil, fmt.Errorf("invalid code point %q", field)
		}
		codePoints = append(codePoints, uint32(cp))
		offset++
		if cp > 0xffff {
			offset++
		}
	}
	return codePoints, boundaries, nil
}

type breakBoundary struct {
	offset int
	// wordLike is the ICU word classification of the segment ending here.
	// A nil value means the fixture supplies no equivalent classification.
	wordLike *bool
}

func checkBreakRecords(t *testing.T, check ekuma.Value, points []any, boundaries []breakBoundary) error {
	offsets := make([]any, len(boundaries))
	wordLike := make([]any, len(boundaries))
	for i, b := range boundaries {
		offsets[i] = b.offset
		if b.wordLike != nil {
			wordLike[i] = *b.wordLike
		}
	}
	_, err := check.Invoke(t.Context(), points, offsets, wordLike)
	return err
}
