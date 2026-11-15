// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl_test

import (
	"context"
	"testing"

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
			name: "case mapping",
			src:  `"i".toLocaleUpperCase("tr") + "I".toLocaleLowerCase("az-Latn") + "i".toLocaleUpperCase("en")`,
			want: "İıI",
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
