// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package date

import (
	"math/bits"
	"slices"
	"strings"
	"sync"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
	"github.com/hajimehoshi/ekuma/internal/unicode/cldr"
)

// genPattern is a pattern of a generator with its skeleton.
type genPattern struct {
	skel    *skeleton
	base    string
	pattern string
	// specified is the skeleton that the pattern was given with, or nil when the skeleton
	// is that of the pattern.
	specified *skeleton
}

// generator chooses the patterns for skeletons as ICU's DateTimePatternGenerator does.
type generator struct {
	data *localeData
	// patterns are in the order of the search for the best pattern.
	patterns []*genPattern
	// defaultHour is the hour letter of the locale.
	defaultHour byte
	// allowedHours are the hour formats that the locale allows, most preferred first.
	allowedHours []string
	// dateTime are the patterns that combine a date and a time, by style from full to short.
	dateTime [4]string
	// appendItems are the patterns that append a field to a pattern, by field.
	appendItems [fieldCount]string
	// fieldNames are the display names of the fields, by field.
	fieldNames [fieldCount]string
	// decimal is the decimal separator.
	decimal string
}

// generatorKey is the key of the cache of generators.
type generatorKey struct {
	locale, calendar string
}

var (
	generatorsMu sync.Mutex
	generators   = map[generatorKey]*generator{}
)

// generatorFor returns the generator of the available locale l, whose Unicode extension
// keywords other than "hc", "rg" and "nu" are ignored, for the calendar cal.
func generatorFor(l, cal string) *generator {
	generatorsMu.Lock()
	defer generatorsMu.Unlock()
	k := generatorKey{
		locale:   l,
		calendar: cal,
	}
	if g, ok := generators[k]; ok {
		return g
	}
	g := newGenerator(l, cal)
	generators[k] = g
	return g
}

// canonicalItems are the patterns of single fields that every generator has.
var canonicalItems = []string{"G", "y", "Q", "M", "w", "W", "E", "D", "F", "d", "a", "H", "m", "s", "S", "v"}

// appendItemNames and fieldNameKeys are the CLDR names of the fields for their append items
// and display names.
var (
	appendItemNames = [fieldCount]string{"Era", "Year", "Quarter", "Month", "Week", "", "Day-Of-Week", "", "", "Day", "", "Hour", "Minute", "Second", "", "Timezone"}
	fieldNameKeys   = [fieldCount]string{"era", "year", "quarter", "month", "week", "weekOfMonth", "weekday", "dayOfYear", "weekdayOfMonth", "day", "dayperiod", "hour", "minute", "second", "", "zone"}
)

// styleLetters are the letters of the records for the styles from full to short.
var styleLetters = [4]string{"f", "l", "m", "s"}

func newGenerator(l, cal string) *generator {
	t, _ := locale.Parse(l)
	d := newLocaleData(t.BaseName(), cal)
	g := &generator{
		data: d,
	}
	for _, p := range canonicalItems {
		g.add(p, "", false)
	}
	for _, s := range styleLetters {
		if p, ok := d.calendar("F" + s); ok {
			g.add(p, "", false)
		}
		if p, ok := d.calendar("T" + s); ok {
			g.add(p, "", false)
		}
	}
	formats := d.calendarFields("S")
	for _, k := range sortedKeys(formats) {
		g.add(formats[k], k, true)
	}
	// The patterns are searched by the first letter of their base, upper case first, and
	// then in the order they were added.
	slices.SortStableFunc(g.patterns, func(a, b *genPattern) int {
		return int(a.base[0]) - int(b.base[0])
	})

	for i, s := range styleLetters {
		p, ok := d.calendar("A" + s)
		if !ok {
			p, _ = d.calendar("C" + s)
		}
		g.dateTime[i] = p
	}
	for f, n := range appendItemNames {
		if n == "" {
			continue
		}
		if p, ok := d.calendar("Y" + n); ok {
			g.appendItems[f] = p
		}
	}
	for f, k := range fieldNameKeys {
		if k == "" {
			continue
		}
		if n, ok := d.lookup(cldr.DateFieldNames, k); ok {
			g.fieldNames[f] = n
		}
	}
	nu := locale.NumberingSystemOf(locale.DataLocale(t.BaseName()))
	if v, ok := t.Keyword("nu"); ok {
		nu = v
	}
	g.decimal = d.numberSymbol(nu, "d")

	g.allowedHours, g.defaultHour = allowedHours(t)
	switch hc, _ := t.Keyword("hc"); hc {
	case "h11":
		g.defaultHour = 'K'
	case "h12":
		g.defaultHour = 'h'
	case "h23":
		g.defaultHour = 'H'
	case "h24":
		g.defaultHour = 'k'
	}
	return g
}

// add adds the pattern p with the skeleton skel, or with its own skeleton when skel is
// empty. A pattern whose skeleton the generator already has replaces it only when override
// is true.
func (g *generator) add(p, skel string, override bool) {
	var s *skeleton
	if skel != "" {
		s = newSkeleton(skel)
	} else {
		s = newSkeleton(p)
	}
	base := s.base()
	for _, e := range g.patterns {
		if e.base == base && (e.specified == nil || (skel != "" && !override)) {
			if !override {
				return
			}
			break
		}
	}
	for _, e := range g.patterns {
		if e.base == base && e.skel.types == s.types {
			if !override || (skel != "" && e.specified != nil) {
				return
			}
			e.pattern = p
			if skel != "" {
				e.specified = s
			} else {
				e.specified = nil
			}
			return
		}
	}
	e := &genPattern{
		skel:    s,
		base:    base,
		pattern: p,
	}
	if skel != "" {
		e.specified = s
	}
	g.patterns = append(g.patterns, e)
}

// bestRaw returns the pattern whose skeleton is the nearest to req in the fields of mask.
func (g *generator) bestRaw(req *skeleton, mask int, info *distanceInfo) *genPattern {
	bestDistance := int(^uint(0) >> 1)
	bestMissing := -1
	var best *genPattern
	var tmp distanceInfo
	for _, e := range g.patterns {
		// A two-digit year pattern can include a literal marking an abbreviated year.
		numericYears := req.types[fieldYear] > 0 && e.skel.types[fieldYear] > 0
		if numericYears && req.lens[fieldYear] != 2 && e.skel.lens[fieldYear] == 2 {
			continue
		}
		d := req.distance(e.skel, mask, &tmp)
		if d < bestDistance || d == bestDistance && bestMissing < tmp.missing {
			bestDistance = d
			bestMissing = tmp.missing
			best = e
			*info = tmp
			if d == 0 {
				break
			}
		}
	}
	return best
}

// mapHourLetters replaces the hour letters "j", "C" and "J" of the skeleton s, and reports
// whether s has "J".
func (g *generator) mapHourLetters(s string) (string, bool) {
	var b strings.Builder
	usesJ := false
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' {
			inQuote = !inQuote
			b.WriteByte(c)
			continue
		}
		if inQuote {
			b.WriteByte(c)
			continue
		}
		switch c {
		case 'j', 'C':
			extra := 0
			for i+1 < len(s) && s[i+1] == c {
				extra++
				i++
			}
			hourLen := 1 + extra&1
			periodLen := 1
			if extra >= 2 {
				periodLen = 3 + extra>>1
			}
			hour := byte('h')
			period := byte('a')
			if c == 'j' {
				hour = g.defaultHour
			} else {
				best := g.allowedHours[0]
				switch best[0] {
				case 'H':
					hour = 'H'
				case 'K':
					hour = 'K'
				case 'k':
					hour = 'k'
				}
				switch {
				case strings.HasSuffix(best, "B"):
					period = 'B'
				case strings.HasSuffix(best, "b"):
					period = 'b'
				}
			}
			if hour == 'H' || hour == 'k' {
				periodLen = 0
			}
			for range periodLen {
				b.WriteByte(period)
			}
			for range hourLen {
				b.WriteByte(hour)
			}
		case 'J':
			b.WriteByte('H')
			usesJ = true
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), usesJ
}

// PatternForSkeleton returns the locale's pattern for an ICU skeleton, optionally matching the requested hour width.
func PatternForSkeleton(l, cal, skeleton string, matchHourWidth bool) string {
	return generatorFor(l, cal).bestPattern(skeleton, matchOptions{
		hourLength: matchHourWidth,
	})
}

// matchOptions are the options of the adjustment of field lengths.
type matchOptions struct {
	// hourLength keeps the length of the requested hour field.
	hourLength bool
}

// bestPattern returns the pattern for the skeleton s.
func (g *generator) bestPattern(s string, opts matchOptions) string {
	mapped, usesJ := g.mapHourLetters(s)
	req := newSkeleton(mapped)
	var info distanceInfo
	best := g.bestRaw(req, -1, &info)
	if info.missing == 0 && info.extra == 0 {
		return g.adjust(best.pattern, best.specified, req, usesJ, false, opts)
	}
	needed := req.fieldMask()
	datePattern := g.bestAppending(req, needed&dateFieldMask, usesJ, opts)
	timePattern := g.bestAppending(req, needed&timeFieldMask, usesJ, opts)
	if datePattern == "" {
		return timePattern
	}
	if timePattern == "" {
		return datePattern
	}
	style := 3
	switch req.baseLens[fieldMonth] {
	case 4:
		if req.baseLens[fieldWeekday] > 0 {
			style = 0
		} else {
			style = 1
		}
	case 3:
		style = 2
	}
	return formatPattern(g.dateTime[style], timePattern, datePattern)
}

// Masks of the seconds and fractional seconds fields.
const (
	fractionalMask           = 1 << fieldFractionalSecond
	secondAndFractionalMasks = 1<<fieldSecond | fractionalMask
)

// bestAppending returns the pattern for the fields of mask of req, appending the fields that
// no pattern has.
func (g *generator) bestAppending(req *skeleton, mask int, usesJ bool, opts matchOptions) string {
	if mask == 0 {
		return ""
	}
	var info distanceInfo
	e := g.bestRaw(req, mask, &info)
	result := g.adjust(e.pattern, e.specified, req, usesJ, false, opts)
	if info.missing == 0 && info.extra == 0 {
		return result
	}
	last := 0
	for info.missing != 0 {
		if last == info.missing {
			break
		}
		if info.missing&secondAndFractionalMasks == fractionalMask && mask&secondAndFractionalMasks == secondAndFractionalMasks {
			result = g.adjust(result, e.specified, req, usesJ, true, opts)
			info.missing &^= fractionalMask
			continue
		}
		start := info.missing
		e = g.bestRaw(req, info.missing, &info)
		tmp := g.adjust(e.pattern, e.specified, req, usesJ, false, opts)
		found := start &^ info.missing
		top := bits.Len(uint(found)) - 1
		if top >= 0 {
			format := g.appendItems[top]
			if format == "" {
				format = "{0} ├{2}: {1}┤"
			}
			result = formatPattern(format, result, tmp, "'"+g.fieldNames[top]+"'")
		}
		last = info.missing
	}
	return result
}

// adjust adjusts the field letters and lengths of the pattern p, found for the request req,
// to those of req. specified is the skeleton that p was given with, or nil. fixFractional
// appends the fractional seconds of req to the seconds.
func (g *generator) adjust(p string, specified, req *skeleton, usesJ, fixFractional bool, opts matchOptions) string {
	items := parsePattern(p)
	for i, it := range items {
		if it.ch == 0 {
			continue
		}
		ci := canonicalIndex(it.ch, it.n)
		if ci < 0 {
			continue
		}
		row := fieldTypes[ci]
		f := row.field
		if fixFractional && f == fieldSecond {
			var s strings.Builder
			s.WriteString(patternString([]item{it}) + quoteLiteral(g.decimal))
			for range req.lens[fieldFractionalSecond] {
				s.WriteString(string(req.chars[fieldFractionalSecond]))
			}
			items[i] = item{lit: "\x00" + s.String()}
			continue
		}
		if req.types[f] == 0 {
			continue
		}
		reqChar := req.chars[f]
		reqLen := req.lens[f]
		if reqChar == 'E' && reqLen < 3 {
			reqLen = 3
		}
		adjLen := reqLen
		switch {
		case f == fieldHour && !opts.hourLength, f == fieldMinute, f == fieldSecond:
			adjLen = it.n
		case specified != nil && reqChar != 'c' && reqChar != 'e':
			skelLen := specified.lens[f]
			patNumeric := row.typ > 0
			reqNumeric := req.types[f] > 0
			if skelLen == reqLen || patNumeric != reqNumeric {
				adjLen = it.n
			}
		}
		c := reqChar
		if f == fieldHour || f == fieldMonth || f == fieldWeekday || f == fieldYear && reqChar != 'Y' {
			c = it.ch
		}
		if c == 'E' && adjLen < 3 {
			c = 'e'
		}
		if f == fieldHour && g.defaultHour != 0 {
			switch {
			case usesJ || reqChar == g.defaultHour:
				c = g.defaultHour
			case reqChar == 'h' && g.defaultHour == 'K':
				c = 'K'
			case reqChar == 'H' && g.defaultHour == 'k':
				c = 'k'
			case reqChar == 'k' && g.defaultHour == 'H':
				c = 'H'
			case reqChar == 'K' && g.defaultHour == 'h':
				c = 'h'
			}
		}
		items[i] = item{
			ch: c,
			n:  adjLen,
		}
	}
	var b strings.Builder
	for _, it := range items {
		if s, ok := strings.CutPrefix(it.lit, "\x00"); ok {
			b.WriteString(s)
			continue
		}
		b.WriteString(patternString([]item{it}))
	}
	return b.String()
}
