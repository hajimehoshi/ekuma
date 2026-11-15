// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package date_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma/internal/intl/date"
	"github.com/hajimehoshi/ekuma/internal/temporal"
)

func TestPatterns(t *testing.T) {
	for _, tc := range []struct {
		locale string
		hc     string
		c      date.Components
		want   string
	}{
		{
			locale: "en-US",
			hc:     "h12",
			c: date.Components{
				Year:  "numeric",
				Month: "numeric",
				Day:   "numeric",
			},
			want: "M/d/y",
		},
		// A long month combines the date and the time with the "at time" pattern.
		{
			locale: "en-US",
			hc:     "h12",
			c: date.Components{
				Year:   "numeric",
				Month:  "long",
				Day:    "numeric",
				Hour:   "numeric",
				Minute: "numeric",
			},
			want: "MMMM d, y 'at' h:mm a",
		},
		// Fractional seconds are appended to the seconds with the decimal separator.
		{
			locale: "de",
			hc:     "h23",
			c: date.Components{
				Minute:                 "numeric",
				Second:                 "numeric",
				FractionalSecondDigits: 3,
			},
			want: "mm:ss,SSS",
		},
		{
			locale: "en",
			hc:     "h12",
			c: date.Components{
				DayPeriod: "long",
				Hour:      "numeric",
				Minute:    "numeric",
			},
			want: "h:mm BBBB",
		},
		// A 24-hour cycle drops the day period.
		{
			locale: "en",
			hc:     "h23",
			c: date.Components{
				DayPeriod: "long",
				Hour:      "numeric",
			},
			want: "HH",
		},
		{
			locale: "en",
			hc:     "h11",
			c: date.Components{
				Hour:   "numeric",
				Minute: "numeric",
			},
			want: "K:mm a",
		},
		{
			locale: "ja",
			hc:     "h11",
			c: date.Components{
				Hour:   "numeric",
				Minute: "numeric",
			},
			want: "aK:mm",
		},
		// A time zone that no pattern has is appended.
		{
			locale: "en",
			hc:     "h12",
			c: date.Components{
				Hour:         "numeric",
				TimeZoneName: "longGeneric",
			},
			want: "h a vvvv",
		},
	} {
		if got := date.NewFormat(tc.locale, "gregory", tc.hc, tc.c).Pattern(); got != tc.want {
			t.Errorf("%s %s %+v: got %q, want %q", tc.locale, tc.hc, tc.c, got, tc.want)
		}
	}
}

func TestStylePatterns(t *testing.T) {
	for _, tc := range []struct {
		hc                   string
		dateStyle, timeStyle string
		want                 string
	}{
		{
			hc:        "h12",
			dateStyle: "full",
			timeStyle: "short",
			want:      "EEEE, MMMM d, y 'at' h:mm a",
		},
		// An hour cycle other than that of the style makes a pattern from the skeleton.
		{
			hc:        "h23",
			timeStyle: "long",
			want:      "HH:mm:ss z",
		},
	} {
		if got := date.NewStyleFormat("en-US", "gregory", tc.hc, tc.dateStyle, tc.timeStyle).Pattern(); got != tc.want {
			t.Errorf("%s %s %s: got %q, want %q", tc.hc, tc.dateStyle, tc.timeStyle, got, tc.want)
		}
	}
}

func zone(t *testing.T, id string) temporal.TimeZone {
	t.Helper()
	z, ok := temporal.GetAvailableNamedTimeZone(id, temporal.UTC)
	if !ok {
		t.Fatalf("no time zone %s", id)
	}
	return z
}

func join(parts []date.Part) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Value)
	}
	return b.String()
}

func TestTimeZoneNames(t *testing.T) {
	winter := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC).UnixMilli()
	for _, tc := range []struct {
		locale string
		zone   string
		name   string
		want   string
	}{
		{"en-US", "America/Los_Angeles", "short", "PST"},
		{"en-US", "America/Los_Angeles", "long", "Pacific Standard Time"},
		{"en-US", "America/Los_Angeles", "shortGeneric", "PT"},
		{"en-US", "America/Los_Angeles", "longOffset", "GMT-08:00"},
		{"en-US", "Asia/Kolkata", "shortOffset", "GMT+5:30"},
		// A specific name that the locale lacks is the GMT format.
		{"en-US", "Asia/Kolkata", "short", "GMT+5:30"},
		// A generic name that the locale lacks is the name of the region of the zone.
		{"en-US", "Europe/London", "shortGeneric", "United Kingdom Time"},
		{"en-US", "UTC", "long", "Coordinated Universal Time"},
	} {
		f := date.NewFormat(tc.locale, "gregory", "h23", date.Components{TimeZoneName: tc.name})
		parts := date.NewFormatter(f, "latn", zone(t, tc.zone)).Format(winter, false)
		var got string
		for _, p := range parts {
			if p.Type == "timeZoneName" {
				got = p.Value
			}
		}
		if got != tc.want {
			t.Errorf("%s %s %s: got %q, want %q", tc.locale, tc.zone, tc.name, got, tc.want)
		}
	}
}

func TestFormat(t *testing.T) {
	afternoon := time.Date(2024, 1, 5, 15, 4, 5, 6e6, time.UTC).UnixMilli()
	noon := time.Date(2024, 1, 5, 12, 0, 0, 0, time.UTC).UnixMilli()
	for _, tc := range []struct {
		locale string
		nu     string
		c      date.Components
		t      int64
		want   string
	}{
		// The narrow no-break space of CLDR before the day period is a space.
		{
			locale: "en",
			nu:     "latn",
			c: date.Components{
				Hour:   "numeric",
				Minute: "2-digit",
			},
			t:    afternoon,
			want: "3:04 PM",
		},
		{
			locale: "en",
			nu:     "latn",
			c: date.Components{
				DayPeriod: "long",
				Hour:      "numeric",
			},
			t:    afternoon,
			want: "3 in the afternoon",
		},
		{
			locale: "en",
			nu:     "latn",
			c: date.Components{
				DayPeriod: "short",
				Hour:      "numeric",
				Minute:    "2-digit",
			},
			t:    noon,
			want: "12:00 noon",
		},
		{
			locale: "en-u-nu-arab",
			nu:     "arab",
			c: date.Components{
				Minute:                 "2-digit",
				Second:                 "2-digit",
				FractionalSecondDigits: 2,
			},
			t:    afternoon,
			want: "٠٤:٠٥٫٠٠",
		},
		{
			locale: "en",
			nu:     "latn",
			c: date.Components{
				Era:  "short",
				Year: "numeric",
			},
			t:    time.Date(-43, 3, 15, 0, 0, 0, 0, time.UTC).UnixMilli(),
			want: "44 BC",
		},
	} {
		f := date.NewFormat(tc.locale, "gregory", "h12", tc.c)
		if got := join(date.NewFormatter(f, tc.nu, temporal.UTC).Format(tc.t, false)); got != tc.want {
			t.Errorf("%s %+v: got %q, want %q", tc.locale, tc.c, got, tc.want)
		}
	}
}

func TestFormatRange(t *testing.T) {
	day := func(y, m, d, h int) int64 {
		return time.Date(y, time.Month(m), d, h, 0, 0, 0, time.UTC).UnixMilli()
	}
	for _, tc := range []struct {
		c    date.Components
		x, y int64
		want string
	}{
		// Only the fields that differ come from both dates.
		{
			c: date.Components{
				Year:  "numeric",
				Month: "short",
				Day:   "numeric",
			},
			x:    day(2019, 1, 3, 0),
			y:    day(2019, 1, 5, 0),
			want: "Jan [3] – (5), 2019",
		},
		// A time range on the same day has the date once.
		{
			c: date.Components{
				Year:   "numeric",
				Month:  "short",
				Day:    "numeric",
				Hour:   "numeric",
				Minute: "numeric",
			},
			x:    day(2019, 1, 3, 10),
			y:    day(2019, 1, 3, 11),
			want: "Jan 3, 2019, [10:00] – (11:00) AM",
		},
		// Dates that do not differ in the fields of the format are one date.
		{
			c: date.Components{
				Year:  "numeric",
				Month: "numeric",
			},
			x:    day(2019, 1, 3, 0),
			y:    day(2019, 1, 5, 0),
			want: "1/2019",
		},
	} {
		f := date.NewFormat("en-US", "gregory", "h12", tc.c)
		var b strings.Builder
		src := "shared"
		for _, p := range date.NewFormatter(f, "latn", temporal.UTC).FormatRange(tc.x, tc.y, false) {
			if p.Source != src {
				switch src {
				case "startRange":
					b.WriteString("]")
				case "endRange":
					b.WriteString(")")
				}
				switch p.Source {
				case "startRange":
					b.WriteString("[")
				case "endRange":
					b.WriteString("(")
				}
				src = p.Source
			}
			b.WriteString(p.Value)
		}
		if got := b.String(); got != tc.want {
			t.Errorf("%+v: got %q, want %q", tc.c, got, tc.want)
		}
	}
}
