// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
	_ "github.com/hajimehoshi/ekuma/intl"
)

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
			name: "array toLocaleString",
			src:  `[1234.5, 1n].toLocaleString("de") + " " + new Float64Array([0.5]).toLocaleString("en", {style: "percent"})`,
			want: "1.234,5,1 50%",
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
			name: "relative time",
			src:  `[new Intl.RelativeTimeFormat("ja", {numeric: "auto"}).format(-1, "day"), new Intl.RelativeTimeFormat("ar-u-nu-arab").format(3, "hours")].join("; ")`,
			want: "昨日; خلال ٣ ساعات",
		},
		{
			// The name of a dialect is used only when it covers every subtag that it
			// stands for.
			name: "language names",
			src:  `["en-US", "zh-Hant-TW", "en-Latn-US", "de-1996"].map(c => new Intl.DisplayNames("en", {type: "language"}).of(c)).concat([new Intl.DisplayNames("en", {type: "language", style: "short"}).of("en-GB"), new Intl.DisplayNames("en", {type: "language", languageDisplay: "standard"}).of("en-US"), new Intl.DisplayNames("ja", {type: "language"}).of("zh-Hans-SG")]).join("; ")`,
			want: "American English; Chinese (Traditional, Taiwan); English (Latin, United States); German (German orthography of 1996); UK English; English (United States); 中国語 (簡体字、シンガポール)",
		},
		{
			name: "language without a name",
			src:  `new Intl.DisplayNames("en", {type: "language"}).of("en-XY") + " " + new Intl.DisplayNames("en", {type: "language", fallback: "none"}).of("en-XY")`,
			want: "en-XY undefined",
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
			// A run of marks longer than a step is uppercased as a whole: the first 30
			// Greek modifiers after a letter are removed.
			name: "Greek uppercasing of a long run of marks",
			src:  `String(("\u03b1" + "\u0301\u0308".repeat(2 ** 16)).toLocaleUpperCase("el") === "\u0391" + "\u0301\u0308".repeat(2 ** 16 - 15))`,
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
			src:  `("\u03b1" + "\u0301\u0308".repeat(2 ** 22)).toLocaleUpperCase("el")`,
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
