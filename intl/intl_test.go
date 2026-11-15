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
