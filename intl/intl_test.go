// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hajimehoshi/ekuma"
	_ "github.com/hajimehoshi/ekuma/intl"
)

func TestDateTimeFormatRejectsPosixrules(t *testing.T) {
	dir := t.TempDir()
	// A TZif v1 file with one UTC time type and no transitions.
	data := make([]byte, 50)
	copy(data, "TZif")
	data[39] = 1 // Number of local time types.
	data[43] = 4 // Number of bytes in the abbreviation table.
	data = append(data, "UTC\x00"...)
	if err := os.WriteFile(filepath.Join(dir, "posixrules"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZONEINFO", dir)

	var vm ekuma.VM
	got := evalLocale(t, vm.InitialRealm(), `
		try {
			new Intl.DateTimeFormat("en", {timeZone: "posixrules"}).resolvedOptions().timeZone;
		} catch (e) {
			String(e instanceof RangeError);
		}
	`)
	if got != "true" {
		t.Errorf("DateTimeFormat with posixrules: got %s, want RangeError", got)
	}
}

func TestParseLocalizedDateString(t *testing.T) {
	var vm ekuma.VM
	if err := vm.SetLocale("wo"); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	vm.SetTimeZone(loc)
	// Wolof's daylight name contains an unmatched opening parenthesis.
	got := evalLocale(t, vm.InitialRealm(), `
		const date = new Date(Date.UTC(2020, 6, 1));
		new Date(Date.parse(date.toString())).toUTCString();
	`)
	if want := "Wed, 01 Jul 2020 00:00:00 GMT"; got != want {
		t.Errorf("parsing the localized Date string: got %q, want %q", got, want)
	}
}

func TestDateStringWithCustomLocalZone(t *testing.T) {
	t.Setenv("TZ", "UTC")
	original := time.Local
	t.Cleanup(func() { time.Local = original })
	var vm ekuma.VM
	for _, tc := range []struct {
		name   string
		offset int
		want   string
	}{
		{
			name: "Office",
			want: "00:00:00 GMT+0000 (Office)",
		},
		{
			name:   "Europe/Berlin",
			offset: 3 * 60 * 60,
			want:   "03:00:00 GMT+0300 (Europe/Berlin)",
		},
	} {
		time.Local = time.FixedZone(tc.name, tc.offset)
		vm.SetTimeZone(time.Local)
		got := evalLocale(t, vm.InitialRealm(), `new Date(0).toTimeString()`)
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTemporalToLocaleString(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "plain date",
			src:  `new Temporal.PlainDate(2024, 3, 1).toLocaleString("de")`,
			want: "1.3.2024",
		},
		{
			name: "zoned date-time",
			src:  `Temporal.ZonedDateTime.from("2024-03-01T10:00[Europe/Paris]").toLocaleString("en-US")`,
			want: "3/1/2024, 10:00:00 AM GMT+1",
		},
		// Date.prototype.toTemporalInstant creates an Instant before Temporal is used.
		{
			name: "instant from a date",
			src:  `new Date(0).toTemporalInstant().toLocaleString("en-US", {timeZone: "UTC"})`,
			want: "1/1/1970, 12:00:00 AM",
		},
		{
			name: "namespace",
			src:  `Object.getOwnPropertyNames(Temporal).join() + " " + Object.prototype.toString.call(Temporal)`,
			want: "Instant,Duration,PlainDate,PlainTime,PlainDateTime,PlainYearMonth,PlainMonthDay,ZonedDateTime,Now [object Temporal]",
		},
	} {
		var vm ekuma.VM
		s, err := vm.ParseScript("test.js", tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		v, err := s.Evaluate(context.Background())
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got := v.String(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestLocaleSensitiveMethods(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "localeCompare",
			src:  `["a".localeCompare("B"), "ä".localeCompare("z", "sv"), "a".localeCompare("á", "en", {sensitivity: "base"})].join()`,
			want: "-1,1,0",
		},
		{
			// The chunks of a long string end at 65536 bytes, inside the last run here.
			name: "collation of long runs of combining marks",
			src: `var below = "̖", acute = "́", grave = "̀", x = "x".repeat(65500);
				var c = new Intl.Collator("en");
				var cmp = (a, b) => [a.localeCompare(b), c.compare(a, b)].join("/");
				[
					cmp("e" + below.repeat(30) + acute, "é" + below.repeat(30)),
					cmp("e" + below.repeat(31) + acute, "é" + below.repeat(31)),
					cmp("e" + (acute + below).repeat(20), "é" + below.repeat(20) + acute.repeat(19)),
					cmp("e" + (acute + below).repeat(2000), "é" + below.repeat(2000) + acute.repeat(1999)),
					cmp("e" + (acute + below).repeat(2000), "é" + below.repeat(2000) + acute.repeat(1998)),
					cmp(x + "e" + (acute + below).repeat(100), x + "é" + below.repeat(100) + acute.repeat(99)),
					cmp(x + "e" + (grave + below).repeat(100), x + "é" + below.repeat(100) + acute.repeat(99)),
					new Intl.Collator("en", {sensitivity: "accent"}).compare("E" + below.repeat(30) + acute, "é" + below.repeat(30)),
					new Intl.Collator("en", {ignorePunctuation: true}).compare("-e" + below.repeat(30) + acute, "é" + below.repeat(30)),
				].join()`,
			want: "0/0,0/0,0/0,0/0,1/1,0/0,1/1,0,0",
		},
		{
			name: "case mapping",
			src:  `"i".toLocaleUpperCase("tr") + "I".toLocaleLowerCase("az-Latn") + "i".toLocaleUpperCase("en")`,
			want: "İıI",
		},
		{
			// Lithuanian removes the dot above of i without composing the rest.
			name: "Lithuanian uppercasing",
			src:  `"i\u0307\u0301".toLocaleUpperCase("lt")`,
			want: "I\u0301",
		},
		{
			// U+0345 is case-ignorable, so the sigma after it is not final.
			name: "final sigma with a language",
			src:  `"\u0345Σ".toLocaleLowerCase("tr") + "\u0345Σ".toLocaleLowerCase("el")`,
			want: "\u0345σ\u0345σ",
		},
		{
			// The uppercase of SpecialCasing.txt adds the breathing, which Greek removes.
			name: "Greek uppercasing",
			src:  `"ὐ".toLocaleUpperCase("el")`,
			want: "Υ",
		},
		{
			name: "Greek diaeresis follows the form of the preceding accent",
			src:  `"α\u0301ί άι\u0301".toLocaleUpperCase("el")`,
			want: "ΑΙ\u0308 ΑΪ",
		},
		{
			name: "Greek eta with case-ignorable neighbors",
			src:  `"η\u0301 η\u0301'α α'ή".toLocaleUpperCase("el")`,
			want: "Η\u0301 Η'Α Α'Η",
		},
		{
			// A string longer than 65536 bytes is mapped in parts, and the language rules
			// need the context of the characters around a part's end.
			name: "case mapping around the end of a part",
			src: `var failed = [];
				for (var n = 65528; n < 65544; n++) {
					var p = "a".repeat(n);
					for (var [lang, c, upper] of [
						["tr", "İ", false], ["az", "İ", false], ["tr", "İ", false],
						["lt", "i̇", true], ["lt", "İ̀", false],
						["el", "ᾳ", true], ["el", "Ά", true], ["el", "άι", true],
					]) {
						var map = s => upper ? s.toLocaleUpperCase(lang) : s.toLocaleLowerCase(lang);
						if (map(p + c).slice(n) !== map(c)) {
							failed.push(lang + " " + n);
						}
					}
				}
				failed.join()`,
			want: "",
		},
		{
			name: "array toLocaleString",
			src:  `[1234.5, 1n].toLocaleString("de") + " " + new Float64Array([0.5]).toLocaleString("en", {style: "percent"})`,
			want: "1.234,5,1 50%",
		},
		{
			name: "Thai and Chinese word iteration",
			src: `Array.from(new Intl.Segmenter(undefined, {granularity: "word"}).segment(
				"😀เด็กๆกำลังเล่น，我研究生命起源。"), s => s.index + " " + s.segment).join("|")`,
			want: "0 😀|2 เด็กๆ|7 กำลัง|12 เล่น|16 ，|17 我|18 研究|20 生命|22 起源|24 。",
		},
		{
			name: "segment of a surrogate pair",
			src: `(() => {
				const s = new Intl.Segmenter("en", {granularity: "word"}).segment("a \u{1F600}b");
				const c = s.containing(3);
				return [c.segment, c.index, c.isWordLike, [...s].length].join();
			})()`,
			want: "\U0001F600,2,false,4",
		},
	} {
		var vm ekuma.VM
		s, err := vm.ParseScript("test.js", tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		v, err := s.Evaluate(context.Background())
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got := v.String(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestFormatters(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "list contextual conjunctions",
			src:  `[new Intl.ListFormat("es").format(["agua", "iglesia"]), new Intl.ListFormat("es", {type: "disjunction"}).format(["siete", "ocho"]), new Intl.ListFormat("he").format(["שלום", "abc"])].join("; ")`,
			want: "agua e iglesia; siete u ocho; שלום ו-abc",
		},
		{
			// The elements are a megabyte each, and together longer than a string may be.
			name: "list longer than the string length limit",
			src:  `var s = "x".repeat(1 << 20); try { new Intl.ListFormat("en").format(Array(1100).fill(s)); "no error"; } catch (e) { String(e instanceof RangeError); }`,
			want: "true",
		},
		{
			// The variants are not compared in pairs.
			name: "many variants",
			src:  `var v = []; for (let i = 0; i < 100000; i++) v.push("v" + i.toString(36).padStart(4, "0")); String(Intl.getCanonicalLocales("en-" + v.join("-"))[0].length)`,
			want: "600002",
		},
		{
			// The parts of each element are made once, however many elements follow.
			name: "long list",
			src:  `String(new Intl.ListFormat("en").format(Array(100000).fill("a")).length)`,
			want: "300002",
		},
		{
			name: "regional date style clears inherited day numbering",
			src:  `new Intl.DateTimeFormat("zh-Hant-MO", {calendar: "chinese", dateStyle: "long", timeZone: "UTC"}).formatToParts(Date.UTC(2020, 4, 23)).find(p => p.type === "day").value`,
			want: "1",
		},
		{
			name: "relative time",
			src:  `[new Intl.RelativeTimeFormat("ja", {numeric: "auto"}).format(-1, "day"), new Intl.RelativeTimeFormat("ar-u-nu-arab").format(3, "hours")].join("; ")`,
			want: "昨日; خلال ٣ ساعات",
		},
		{
			name: "language names",
			src:  `["en-US", "zh-Hant-TW", "en-Latn-US", "de-1996"].map(c => new Intl.DisplayNames("en", {type: "language"}).of(c)).concat([new Intl.DisplayNames("en", {type: "language", style: "short"}).of("en-GB"), new Intl.DisplayNames("en", {type: "language", languageDisplay: "standard"}).of("en-US"), new Intl.DisplayNames("ja", {type: "language"}).of("zh-Hans-SG")]).join("; ")`,
			want: "American English; Traditional Chinese (Taiwan); American English (Latin); German (German orthography of 1996); UK English; English (United States); 簡体中国語 (シンガポール)",
		},
		{
			name: "language without a name",
			src:  `new Intl.DisplayNames("en", {type: "language"}).of("en-XY") + " " + new Intl.DisplayNames("en", {type: "language", fallback: "none"}).of("en-XY")`,
			want: "en-XY undefined",
		},
		{
			name: "untranslated variant with fallback none",
			src:  `const names = new Intl.DisplayNames("en", {type: "language", fallback: "none"}); [names.of("hmn-PAHAWH3"), names.of("hmn-unlisted")].map(String).join("; ")`,
			want: "Hmong (pahawh3); undefined",
		},
		{
			name: "other display names",
			src:  `[new Intl.DisplayNames("fr", {type: "region"}).of("de"), new Intl.DisplayNames("en", {type: "region", style: "short"}).of("GB"), new Intl.DisplayNames("en", {type: "script"}).of("hans"), new Intl.DisplayNames("de", {type: "currency", style: "narrow"}).of("usd"), new Intl.DisplayNames("en", {type: "calendar"}).of("islamic-civil"), new Intl.DisplayNames("en", {type: "dateTimeField", style: "short"}).of("month")].join("; ")`,
			want: "Allemagne; UK; Simplified; US-Dollar; Hijri Calendar (tabular, civil epoch); mo.",
		},
		{
			// The Arabic name of one year has no number, and Serbian separates the
			// fields of a numeric duration with periods.
			name: "durations",
			src:  `[new Intl.DurationFormat("ar", {style: "long"}).format({years: 1, months: 2}), new Intl.DurationFormat("sr", {style: "digital"}).format({hours: 1, minutes: 2, seconds: 3}), new Intl.DurationFormat("fr", {style: "long"}).format({hours: 1, minutes: 2, seconds: 3})].join("; ")`,
			want: "سنة وشهران; 1.02.03; 1\u00a0heure, 2 minutes et 3\u00a0secondes",
		},
		{
			name: "compact percent",
			src:  `[12345, 1.5].map(x => new Intl.NumberFormat("de", {style: "percent", notation: "compact"}).format(x)).join("; ")`,
			want: "1,2\u00A0Mio.\u00A0%; 150\u00A0%",
		},
		{
			// Swahili has compact patterns with a negative subpattern.
			name: "compact negative subpatterns",
			src:  `[12345, -12345].map(x => new Intl.NumberFormat("sw", {notation: "compact"}).format(x)).join("; ")`,
			want: "elfu\u00A012; elfu\u00A0-12",
		},
		{
			// The exponent is chosen with the rounding direction of the sign.
			name: "exponent of negative numbers under directional rounding",
			src: `var f = (o, x) => new Intl.NumberFormat("en", o).format(x), s = m => ({notation: "scientific", maximumFractionDigits: 0, roundingMode: m}); ` +
				`[f(s("floor"), -9.6), f(s("ceil"), -9.6), f(s("halfFloor"), -9.5), f(s("halfCeil"), -9.5), ` +
				`f({notation: "compact", roundingMode: "floor"}, -999999), f({notation: "compact", roundingMode: "floor"}, -999.5), ` +
				`f({notation: "engineering", maximumFractionDigits: 0, roundingMode: "floor"}, -999.5), ` +
				`new Intl.NumberFormat("en", {notation: "scientific", maximumFractionDigits: 0, roundingMode: "floor"}).formatToParts(-9.6).map(p => p.value).join(""), ` +
				`new Intl.NumberFormat("en", {notation: "scientific", maximumFractionDigits: 0, roundingMode: "floor"}).formatRange(-9.6, -9.6)].join("; ")`,
			want: "-1E1; -9E0; -1E1; -9E0; -1M; -1K; -1E3; -1E1; ~-1E1",
		},
		{
			// All the marks belong to the same vowel, even across interruption checks.
			name: "Greek uppercasing of a long run of marks",
			src:  `String(("α" + "\u0301\u0308".repeat(2 ** 16)).toLocaleUpperCase("el") === "Α\u0308")`,
			want: "true",
		},
		{
			name: "compact magnitudes above 10^14",
			src:  `new Intl.NumberFormat("ja", {notation: "compact"}).format(1e16)`,
			want: "1京",
		},
		{
			name: "plural of scientific notation",
			src:  `[1000, 0.002].map(x => new Intl.NumberFormat("ru", {style: "unit", unit: "meter", unitDisplay: "long", notation: "scientific"}).format(x)).join("; ")`,
			want: "1E3 метров; 2E-3 метра",
		},
		{
			name: "plural of infinity and not-a-number",
			src:  `[new Intl.NumberFormat("ru", {style: "unit", unit: "meter", unitDisplay: "long"}).format(Infinity), new Intl.NumberFormat("fr", {style: "currency", currency: "USD", currencyDisplay: "name"}).format(NaN)].join("; ")`,
			want: "∞ метра; NaN dollars des États-Unis",
		},
		{
			name: "duration toLocaleString",
			src:  `Temporal.Duration.from("PT1H30M").toLocaleString("de", {style: "long"})`,
			want: "1 Stunde, 30 Minuten",
		},
	} {
		var vm ekuma.VM
		s, err := vm.ParseScript("test.js", tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		v, err := s.Evaluate(context.Background())
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got := v.String(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestCancelFormatting checks that a context stops formatting or mapping a long value or a long
// list.
func TestCancelFormatting(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "BigInt.prototype.toLocaleString",
			src:  `((1n << (1n << 23n)) - 1n).toLocaleString()`,
		},
		{
			name: "Intl.NumberFormat.prototype.format with a BigInt",
			src:  `new Intl.NumberFormat().format((1n << (1n << 23n)) - 1n)`,
		},
		{
			name: "a list of locales without elements",
			src:  `Intl.getCanonicalLocales({length: 2 ** 53})`,
		},
		{
			name: "Array.prototype.toLocaleString of holes",
			src:  `var a = []; a.length = 2 ** 32 - 1; a.toLocaleString()`,
		},
		{
			name: "Greek uppercasing",
			src:  `"I\u0300a".repeat(2 ** 23).toLocaleUpperCase("el")`,
		},
		{
			name: "Lithuanian lowercasing",
			src:  `"I\u0300a".repeat(2 ** 23).toLocaleLowerCase("lt")`,
		},
		{
			// The marks have no boundary of a normalization form between them.
			name: "Lithuanian uppercasing of a run of marks",
			src:  `("a" + "\u0327\u0328\u0301".repeat(2 ** 22)).toLocaleUpperCase("lt")`,
		},
		{
			name: "Greek uppercasing of a run of marks",
			src:  `("\u03b1" + "\u0301\u0308".repeat(2 ** 24)).toLocaleUpperCase("el")`,
		},
		{
			name: "collation of a run of marks",
			src:  `new Intl.Collator().compare("a" + "\u0327\u0328\u0301".repeat(2 ** 22), "b")`,
		},
	} {
		var vm ekuma.VM
		s, err := vm.ParseScript("test.js", tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		if _, err := s.Evaluate(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: got %v, want context.DeadlineExceeded", tc.name, err)
		}
		cancel()
	}
}

// TestCancelElementLookups checks that a loop over the elements of an array-like object stops at the
// next element when its context is done.
func TestCancelElementLookups(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "locales",
			src:  `Intl.getCanonicalLocales({ get 0() { cancel(); return "en"; }, 1: "en", length: 2 });`,
		},
		{
			name: "toLocaleString",
			src:  `Array.prototype.toLocaleString.call({ get 0() { cancel(); }, length: 2 });`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var vm ekuma.VM
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if err := vm.Global().Set("cancel", vm.FuncOf("cancel", func(ekuma.Value, []ekuma.Value) (any, error) {
					cancel()
					// Wait until the VM has received the cancellation.
					synctest.Wait()
					return nil, nil
				})); err != nil {
					t.Fatal(err)
				}
				s, err := vm.ParseScript("test.js", tc.src)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.Evaluate(ctx); !errors.Is(err, context.Canceled) {
					t.Errorf("got %v, want context.Canceled", err)
				}
			})
		})
	}
}
