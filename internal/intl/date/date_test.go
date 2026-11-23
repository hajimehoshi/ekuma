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

func TestISO8601DateTimeStyle(t *testing.T) {
	for _, tc := range []struct {
		locale string
		want   string
	}{
		{
			locale: "fr",
			want:   "y MMMM d, EEEE 'à' HH:mm",
		},
		// Basque has a standard Gregorian separator and no atTime resource.
		{
			locale: "eu",
			want:   "y MMMM d, EEEE HH:mm",
		},
	} {
		f := date.NewStyleFormat(tc.locale, "iso8601", "h23", "full", "short")
		if got := f.Pattern(); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.locale, got, tc.want)
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

func TestTimeZoneNameAtInstant(t *testing.T) {
	for _, tc := range []struct {
		zone string
		at   time.Time
		name string
		want string
	}{
		{
			zone: "Africa/Casablanca",
			at:   time.Date(2020, 5, 25, 0, 0, 0, 0, time.UTC),
			name: "short",
			want: "GMT+0",
		},
		{
			// Whitehorse stayed at UTC-7 when Pacific daylight time ended.
			zone: "America/Whitehorse",
			at:   time.Date(2020, 3, 9, 0, 0, 0, 0, time.UTC),
			name: "short",
			want: "PDT",
		},
		{
			// CLDR specifies the daylight offset even on TZif builds with negative DST.
			zone: "Europe/Dublin",
			at:   time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
			name: "long",
			want: "Irish Standard Time",
		},
	} {
		f := date.NewFormat("en", "gregory", "h23", date.Components{
			TimeZoneName: tc.name,
		})
		got := join(date.NewFormatter(f, "latn", zone(t, tc.zone)).Format(tc.at.UnixMilli(), false))
		if got != tc.want {
			t.Errorf("%s at %s: got %q, want %q", tc.zone, tc.at, got, tc.want)
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

func TestCalendarFormat(t *testing.T) {
	day := func(y, m, d int) int64 {
		return time.Date(y, time.Month(m), d, 12, 0, 0, 0, time.UTC).UnixMilli()
	}
	ymd := date.Components{
		Year:  "numeric",
		Month: "numeric",
		Day:   "numeric",
	}
	yMMMMd := date.Components{
		Year:  "numeric",
		Month: "long",
		Day:   "numeric",
	}
	for _, tc := range []struct {
		locale string
		cal    string
		c      date.Components
		t      int64
		want   string
	}{
		// A two-digit week-year keeps the sign when its leading digits are omitted.
		{
			locale: "te",
			cal:    "buddhist",
			c: date.Components{
				Year:  "2-digit",
				Month: "long",
			},
			t:    day(-101, 8, 17),
			want: "ఆగస్టు -01",
		},
		// Adar of a leap year is Adar II.
		{
			locale: "en",
			cal:    "hebrew",
			c:      yMMMMd,
			t:      day(2024, 3, 15),
			want:   "5 Adar II 5784",
		},
		// A numeric Hebrew month is the ordinal month: Tamuz of a common year is 10.
		{
			locale: "ja",
			cal:    "hebrew",
			c:      ymd,
			t:      day(2023, 6, 20),
			want:   "AM5783/10/1",
		},
		{
			locale: "en",
			cal:    "chinese",
			c:      yMMMMd,
			t:      day(2023, 3, 22),
			want:   "Second Monthbis 1, 2023(gui-mao)",
		},
		{
			locale: "en",
			cal:    "chinese",
			c:      ymd,
			t:      day(2023, 3, 22),
			want:   "2bis/1/2023",
		},
		{
			locale: "en",
			cal:    "japanese",
			c: date.Components{
				Era:   "short",
				Year:  "numeric",
				Month: "long",
				Day:   "numeric",
			},
			t:    day(2024, 3, 15),
			want: "March 15, 6 Reiwa",
		},
		// Before Meiji, the Japanese calendar has the eras of the Gregorian calendar.
		{
			locale: "en",
			cal:    "japanese",
			c: date.Components{
				Era:  "short",
				Year: "numeric",
			},
			t:    day(1850, 1, 1),
			want: "1850 AD",
		},
	} {
		f := date.NewFormat(tc.locale, tc.cal, "h12", tc.c)
		if got := join(date.NewFormatter(f, "latn", temporal.UTC).Format(tc.t, false)); got != tc.want {
			t.Errorf("%s %s %+v: got %q, want %q", tc.locale, tc.cal, tc.c, got, tc.want)
		}
	}
}

func TestFormatRange(t *testing.T) {
	day := func(y, m, d, h int) int64 {
		return time.Date(y, time.Month(m), d, h, 0, 0, 0, time.UTC).UnixMilli()
	}
	for _, tc := range []struct {
		locale string
		c      date.Components
		x, y   int64
		want   string
	}{
		// Only the fields that differ come from both dates.
		{
			locale: "en-US",
			c: date.Components{
				Year:  "numeric",
				Month: "short",
				Day:   "numeric",
			},
			x:    day(2019, 1, 3, 0),
			y:    day(2019, 1, 5, 0),
			want: "Jan [3] – (5), 2019",
		},
		// A time range on the same day has the date once.
		{
			locale: "en-US",
			c: date.Components{
				Year:   "numeric",
				Month:  "short",
				Day:    "numeric",
				Hour:   "numeric",
				Minute: "numeric",
			},
			x:    day(2019, 1, 3, 10),
			y:    day(2019, 1, 3, 11),
			want: "Jan 3, 2019, [10:00] – (11:00) AM",
		},
		// A date and a time without an interval format fall back to the whole pattern, era
		// included.
		{
			locale: "en-US",
			c: date.Components{
				Era:    "narrow",
				Year:   "numeric",
				Month:  "numeric",
				Day:    "numeric",
				Hour:   "numeric",
				Minute: "numeric",
				Second: "numeric",
			},
			x:    day(2025, 11, 4, 14),
			y:    day(2025, 11, 15, 14),
			want: "[11/4/2025 A, 2:00:00 PM] – (11/15/2025 A, 2:00:00 PM",
		},
		{
			locale: "pt",
			c: date.Components{
				Year: "numeric",
			},
			x:    day(2019, 1, 1, 0),
			y:    day(2020, 1, 1, 0),
			want: "[2019]\u2009–\u2009(2020",
		},
		// Dates that do not differ in the fields of the format are one date.
		{
			locale: "en-US",
			c: date.Components{
				Year:  "numeric",
				Month: "numeric",
			},
			x:    day(2019, 1, 3, 0),
			y:    day(2019, 1, 5, 0),
			want: "1/2019",
		},
	} {
		f := date.NewFormat(tc.locale, "gregory", "h12", tc.c)
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
			t.Errorf("%s %+v: got %q, want %q", tc.locale, tc.c, got, tc.want)
		}
	}
}

// A zoned date-time has a time zone name by default, but not with a component of its own.
func TestZonedDateTimeComponents(t *testing.T) {
	for _, tc := range []struct {
		c    date.Components
		want string
	}{
		{
			c:    date.Components{},
			want: "short",
		},
		{
			c: date.Components{
				Year: "numeric",
			},
			want: "",
		},
	} {
		got, ok := date.TemporalComponents(tc.c, date.Any, date.ZonedDateTime, date.InheritAll)
		if !ok || got.TimeZoneName != tc.want {
			t.Errorf("TemporalComponents(%+v): got %+v, %t, want the time zone name %q", tc.c, got, ok, tc.want)
		}
	}
}
