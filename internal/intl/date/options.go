// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package date

import (
	"strings"
)

// Components are the date and time components of a format, with the values of the options
// of Intl.DateTimeFormat. An empty string or a zero FractionalSecondDigits is an absent
// component.
type Components struct {
	Weekday                string
	Era                    string
	Year                   string
	Month                  string
	Day                    string
	DayPeriod              string
	Hour                   string
	Minute                 string
	Second                 string
	FractionalSecondDigits int
	TimeZoneName           string
}

// Format is a pattern for formatting dates and times in a locale and a calendar.
type Format struct {
	// locale is the available locale, with its Unicode extension keywords.
	locale   string
	calendar string
	// hourCycle is the hour cycle of the pattern, or empty when it has no hour.
	hourCycle string
	pattern   string
	items     []item
}

// hourLetters maps the hour cycles to their pattern letters.
var hourLetters = map[string]byte{
	"h11": 'K',
	"h12": 'h',
	"h23": 'H',
	"h24": 'k',
}

// hourCycleOf returns the hour cycle of the first hour field of the pattern p, or an empty
// string when p has none.
func hourCycleOf(items []item) string {
	for _, it := range items {
		switch it.ch {
		case 'K':
			return "h11"
		case 'h':
			return "h12"
		case 'H':
			return "h23"
		case 'k':
			return "h24"
		}
	}
	return ""
}

// replaceHourCycle replaces the hour letters of the pattern p with that of the hour cycle hc,
// putting a space between a day and an hour that follows it immediately.
func replaceHourCycle(p, hc string) string {
	c, ok := hourLetters[hc]
	if !ok {
		return p
	}
	var b strings.Builder
	replace := true
	var last byte
	for i := 0; i < len(p); i++ {
		ch := p[i]
		switch ch {
		case '\'':
			replace = !replace
			b.WriteByte(ch)
		case 'H', 'h', 'K', 'k':
			if replace && last == 'd' {
				b.WriteByte(' ')
			}
			if replace {
				b.WriteByte(c)
			} else {
				b.WriteByte(ch)
			}
		default:
			b.WriteByte(ch)
		}
		last = ch
	}
	return b.String()
}

// skeletonFor returns the skeleton for the components c with the hour cycle hc.
func skeletonFor(c Components, hc string) string {
	var b strings.Builder
	add := func(v string, values map[string]string) {
		b.WriteString(values[v])
	}
	add(c.Weekday, map[string]string{"narrow": "EEEEE", "long": "EEEE", "short": "EEE"})
	add(c.Era, map[string]string{"narrow": "GGGGG", "long": "GGGG", "short": "GGG"})
	add(c.Year, map[string]string{"2-digit": "yy", "numeric": "y"})
	add(c.Month, map[string]string{"narrow": "MMMMM", "long": "MMMM", "short": "MMM", "2-digit": "MM", "numeric": "M"})
	add(c.Day, map[string]string{"2-digit": "dd", "numeric": "d"})
	add(c.DayPeriod, map[string]string{"narrow": "BBBBB", "long": "BBBB", "short": "B"})
	if h := string(hourLetters[hc]); c.Hour != "" {
		add(c.Hour, map[string]string{"2-digit": h + h, "numeric": h})
	}
	add(c.Minute, map[string]string{"2-digit": "mm", "numeric": "m"})
	add(c.Second, map[string]string{"2-digit": "ss", "numeric": "s"})
	for range c.FractionalSecondDigits {
		b.WriteByte('S')
	}
	add(c.TimeZoneName, map[string]string{"long": "zzzz", "short": "z", "longOffset": "OOOO", "shortOffset": "O", "longGeneric": "vvvv", "shortGeneric": "v"})
	return b.String()
}

// NewFormat returns the format for the components c in the available locale l, whose Unicode
// extension keywords apply, and the calendar cal, with the hour cycle hc.
func NewFormat(l, cal, hc string, c Components) *Format {
	g := generatorFor(l, cal)
	p := g.bestPattern(skeletonFor(c, hc), matchOptions{
		hourLength: true,
	})
	return newFormat(l, cal, replaceHourCycle(p, hc))
}

func newFormat(l, cal, p string) *Format {
	items := parsePattern(p)
	return &Format{
		locale:    l,
		calendar:  cal,
		hourCycle: hourCycleOf(items),
		pattern:   p,
		items:     items,
	}
}

// styleLetter returns the letter of the records for the style s.
func styleLetter(s string) string {
	switch s {
	case "full":
		return "f"
	case "long":
		return "l"
	case "medium":
		return "m"
	}
	return "s"
}

// NewStyleFormat returns the format for the date style dateStyle and the time style
// timeStyle, either of which may be empty, in the available locale l and the calendar cal,
// with the hour cycle hc.
func NewStyleFormat(l, cal, hc, dateStyle, timeStyle string) *Format {
	g := generatorFor(l, cal)
	d := g.data
	var p string
	switch {
	case dateStyle != "" && timeStyle != "":
		dp, _ := d.calendar("F" + styleLetter(dateStyle))
		tp, _ := d.calendar("T" + styleLetter(timeStyle))
		glue, ok := d.calendar("A" + styleLetter(dateStyle))
		if !ok {
			glue, _ = d.calendar("C" + styleLetter(dateStyle))
		}
		p = formatPattern(glue, tp, dp)
	case dateStyle != "":
		p, _ = d.calendar("F" + styleLetter(dateStyle))
	default:
		p, _ = d.calendar("T" + styleLetter(timeStyle))
	}
	if timeStyle != "" && hourCycleOf(parsePattern(p)) != hc {
		s := newSkeleton(p).String()
		c := hourLetters[hc]
		var b strings.Builder
		for i := range len(s) {
			switch s[i] {
			case 'a', 'b', 'B':
			case 'h', 'H', 'K', 'k':
				b.WriteByte(c)
			default:
				b.WriteByte(s[i])
			}
		}
		p = replaceHourCycle(g.bestPattern(b.String(), matchOptions{
			hourLength: true,
		}), hc)
	}
	return newFormat(l, cal, p)
}

// Pattern returns the pattern of f.
func (f *Format) Pattern() string {
	return f.pattern
}

// Components returns the components of f.
func (f *Format) Components() Components {
	var c Components
	width := func(n int) string {
		switch {
		case n == 4:
			return "long"
		case n == 5:
			return "narrow"
		}
		return "short"
	}
	digits := func(n int) string {
		if n == 2 {
			return "2-digit"
		}
		return "numeric"
	}
	for _, it := range f.items {
		switch it.ch {
		case 'E', 'c':
			if it.ch == 'c' && it.n < 3 {
				continue
			}
			c.Weekday = width(it.n)
		case 'G':
			c.Era = width(it.n)
		case 'y', 'u', 'U', 'r':
			if it.n == 2 && it.ch == 'y' {
				c.Year = "2-digit"
			} else {
				c.Year = "numeric"
			}
		case 'M', 'L':
			if it.n <= 2 {
				c.Month = digits(it.n)
			} else {
				c.Month = width(it.n)
			}
		case 'd':
			c.Day = digits(it.n)
		case 'b', 'B':
			c.DayPeriod = width(it.n)
		case 'h', 'H', 'K', 'k':
			c.Hour = digits(it.n)
		case 'm':
			c.Minute = digits(it.n)
		case 's':
			c.Second = digits(it.n)
		case 'S':
			c.FractionalSecondDigits = it.n
		case 'z':
			if it.n == 4 {
				c.TimeZoneName = "long"
			} else {
				c.TimeZoneName = "short"
			}
		case 'O':
			if it.n == 4 {
				c.TimeZoneName = "longOffset"
			} else {
				c.TimeZoneName = "shortOffset"
			}
		case 'v':
			if it.n == 4 {
				c.TimeZoneName = "longGeneric"
			} else {
				c.TimeZoneName = "shortGeneric"
			}
		}
	}
	return c
}
