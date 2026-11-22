// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package date

import (
	"strings"
)

// The calendar fields whose differences select interval patterns, from the largest.
const (
	calEra = iota
	calYear
	calMonth
	calDate
	calAmPm
	calHour
	calMinute
	calSecond
	calMillisecond
	calCount
)

// calendarLetters are the pattern letters of the calendar fields.
var calendarLetters = [calCount]byte{'G', 'y', 'M', 'd', 'a', 'h', 'm', 's', 'S'}

// calendarFieldOfLetter returns the calendar field of the letter of an interval format, or -1.
func calendarFieldOfLetter(c byte) int {
	switch c {
	case 'G':
		return calEra
	case 'y':
		return calYear
	case 'M':
		return calMonth
	case 'd':
		return calDate
	case 'a', 'b', 'B':
		return calAmPm
	case 'h', 'H', 'K', 'k':
		return calHour
	case 'm':
		return calMinute
	case 's':
		return calSecond
	}
	return -1
}

// intervalPattern is the pattern of a range whose largest different field is a calendar
// field.
type intervalPattern struct {
	// first and second are the parts of the pattern that format the start and the end. An
	// empty first means a fallback to the full pattern second.
	first, second string
}

func (p intervalPattern) isEmpty() bool {
	return p.first == "" && p.second == ""
}

// intervalInfo is the interval formats of a locale.
type intervalInfo struct {
	// formats maps skeletons to their interval patterns by calendar field.
	formats map[string]*[calCount]string
	// skeletons are the skeletons of formats, sorted.
	skeletons []string
	fallback  string
}

func newIntervalInfo(d *localeData) *intervalInfo {
	info := &intervalInfo{
		formats: map[string]*[calCount]string{},
	}
	fields := d.calendarFields("I")
	lang, _, _ := strings.Cut(d.chain[0], "-")
	for _, k := range sortedKeys(fields) {
		pattern := fields[k]
		if lang == "en" {
			// English date ranges use ordinary spaces around the en dash for web compatibility.
			pattern = strings.ReplaceAll(pattern, "\u2009–\u2009", " – ")
		}
		if k == "" {
			info.fallback = pattern
			continue
		}
		skel, letter, ok := strings.Cut(k, "/")
		if !ok || len(letter) != 1 {
			continue
		}
		f := calendarFieldOfLetter(letter[0])
		if f < 0 {
			continue
		}
		ps, ok := info.formats[skel]
		if !ok {
			ps = &[calCount]string{}
			info.formats[skel] = ps
			info.skeletons = append(info.skeletons, skel)
		}
		if ps[f] == "" {
			ps[f] = pattern
		}
	}
	if info.fallback == "" {
		info.fallback = "{0} – {1}"
	}
	return info
}

// pattern returns the interval pattern of the skeleton for the calendar field.
func (info *intervalInfo) pattern(skel string, field int) string {
	if ps, ok := info.formats[skel]; ok {
		return ps[field]
	}
	return ""
}

// skeletonWidths returns the number of each letter in the skeleton s.
func skeletonWidths(s string) [128]int {
	var w [128]int
	for i := range len(s) {
		if s[i] < 128 {
			w[s[i]]++
		}
	}
	return w
}

// bestSkeleton returns the skeleton of the interval formats nearest to skel and the
// difference: 0 for the same skeleton, 1 for a difference in widths, 2 for a difference in
// the letters of the time zone or the hour cycle, and -1 for a difference in fields.
func (info *intervalInfo) bestSkeleton(skel string) (string, int) {
	replaced := false
	input := skel
	for _, r := range [][2]string{{"z", "v"}, {"K", "h"}, {"k", "H"}, {"b", "a"}} {
		if strings.Contains(input, r[0]) {
			input = strings.ReplaceAll(input, r[0], r[1])
			replaced = true
		}
	}
	in := skeletonWidths(input)
	bestDistance := int(^uint(0) >> 1)
	best := ""
	diff := 0
	for _, s := range info.skeletons {
		w := skeletonWidths(s)
		distance := 0
		fieldDiff := 1
		for c := 'A'; c <= 'z'; c++ {
			a, b := in[c], w[c]
			if a == b {
				continue
			}
			switch {
			case a == 0 || b == 0:
				fieldDiff = -1
				distance += 0x1000
			case c == 'M' && (a <= 2 && b > 2 || a > 2 && b <= 2):
				distance += 0x100
			default:
				distance += abs(a - b)
			}
		}
		if distance < bestDistance {
			best = s
			bestDistance = distance
			diff = fieldDiff
		}
		if distance == 0 {
			diff = 0
			break
		}
	}
	if best == "" {
		return "", -1
	}
	if replaced && diff != -1 {
		diff = 2
	}
	return best, diff
}

// rangeFormat formats ranges of dates as ICU's DateIntervalFormat does.
type rangeFormat struct {
	gen  *generator
	info *intervalInfo
	// full is the pattern for an entire endpoint.
	full     string
	patterns [calCount]intervalPattern
	// datePattern and timePattern are the patterns of the date and time parts for a fallback
	// that formats the date once, or empty.
	datePattern string
	timePattern string
	// dateTime combines a date and a time.
	dateTime string
}

// normalizeHours replaces the hour field of the skeleton s by a single hour letter of the
// locale, with a day period for a 12-hour cycle.
func (r *rangeFormat) normalizeHours(s string) string {
	var hourChar, periodChar byte
	hourStart, hourLen, periodStart, periodLen := 0, 0, 0, 0
scan:
	for i := range len(s) {
		c := s[i]
		switch c {
		case 'j', 'J', 'C', 'h', 'H', 'k', 'K':
			if hourChar == 0 {
				hourChar = c
				hourStart = i
			}
			hourLen++
		case 'a', 'b', 'B':
			if periodChar == 0 {
				periodChar = c
				periodStart = i
			}
			periodLen++
		default:
			if hourChar != 0 && periodChar != 0 {
				break scan
			}
		}
	}
	if hourChar == 0 {
		return s
	}
	converted := r.gen.bestPattern(string(hourChar), matchOptions{})
	var letters strings.Builder
	for _, it := range parsePattern(converted) {
		if it.ch != 0 {
			letters.WriteString(strings.Repeat(string(it.ch), it.n))
		}
	}
	cp := letters.String()
	h := byte('H')
	switch {
	case strings.Contains(cp, "h"):
		h = 'h'
	case strings.Contains(cp, "K"):
		h = 'K'
	case strings.Contains(cp, "k"):
		h = 'k'
	}
	switch {
	case strings.Contains(cp, "B"):
		periodChar = 'B'
	case strings.Contains(cp, "b"):
		periodChar = 'b'
	case periodChar == 0:
		periodChar = 'a'
	}
	repl := string(h)
	if h != 'H' && h != 'k' {
		n := 1
		switch {
		case periodLen >= 5 || hourLen >= 5:
			n = 5
		case periodLen >= 3 || hourLen >= 3:
			n = 3
		}
		repl += strings.Repeat(string(periodChar), n)
	}
	out := s[:hourStart] + repl + s[hourStart+hourLen:]
	if periodLen > 0 {
		if periodStart > hourStart {
			periodStart += len(repl) - hourLen
		}
		out = out[:periodStart] + out[periodStart+periodLen:]
	}
	return out
}

// splitSkeleton splits the skeleton s into its date and time parts, each also in the
// normalized form of ICU's DateIntervalFormat.
func splitSkeleton(s string) (date, normDate, time, normTime string) {
	var d, nd, t, nt strings.Builder
	var e, dd, mm, y, m, v, z int
	var hourChar byte
	for i := range len(s) {
		c := s[i]
		switch c {
		case 'E':
			d.WriteByte(c)
			e++
		case 'd':
			d.WriteByte(c)
			dd++
		case 'M':
			d.WriteByte(c)
			mm++
		case 'y':
			d.WriteByte(c)
			y++
		case 'G', 'Y', 'u', 'Q', 'q', 'L', 'l', 'W', 'w', 'D', 'F', 'g', 'e', 'c', 'U', 'r':
			nd.WriteByte(c)
			d.WriteByte(c)
		case 'h', 'H', 'k', 'K':
			t.WriteByte(c)
			if hourChar == 0 {
				hourChar = c
			}
		case 'm':
			t.WriteByte(c)
			m++
		case 'z':
			z++
			t.WriteByte(c)
		case 'v':
			v++
			t.WriteByte(c)
		case 'a':
			// The day period goes with the hour.
			t.WriteByte(c)
		case 'V', 'Z', 'j', 's', 'S', 'A', 'b', 'B':
			t.WriteByte(c)
			nt.WriteByte(c)
		}
	}
	nd.WriteString(strings.Repeat("y", y))
	switch {
	case mm == 0:
	case mm < 3:
		nd.WriteByte('M')
	default:
		nd.WriteString(strings.Repeat("M", min(mm, 5)))
	}
	switch {
	case e == 0:
	case e <= 3:
		nd.WriteByte('E')
	default:
		nd.WriteString(strings.Repeat("E", min(e, 6)))
	}
	if dd > 0 {
		nd.WriteByte('d')
	}
	if hourChar != 0 {
		nt.WriteByte(hourChar)
	}
	if m > 0 {
		nt.WriteByte('m')
	}
	if z > 0 {
		nt.WriteByte('z')
	}
	if v > 0 {
		nt.WriteByte('v')
	}
	return d.String(), nd.String(), t.String(), nt.String()
}

// splitInterval splits the interval pattern p at the first field whose letter repeats.
func splitInterval(p string) intervalPattern {
	var seen [128]bool
	inQuote := false
	var prev byte
	count := 0
	found := false
	i := 0
	for ; i < len(p); i++ {
		c := p[i]
		if c != prev && count > 0 {
			if !seen[prev] {
				seen[prev] = true
			} else {
				found = true
				break
			}
			count = 0
		}
		if c == '\'' {
			if i+1 < len(p) && p[i+1] == '\'' {
				i++
			} else {
				inQuote = !inQuote
			}
		} else if !inQuote && isLetter(c) {
			prev = c
			count++
		}
	}
	if count > 0 && !found && !seen[prev] {
		count = 0
	}
	at := i - count
	return intervalPattern{
		first:  p[:at],
		second: p[at:],
	}
}

// adjustWidths lengthens the fields of the interval pattern p, found for the skeleton best, to
// those of the skeleton input.
func adjustWidths(input, best, p string, diff int) string {
	in := skeletonWidths(input)
	bw := skeletonWidths(best)
	if diff == 2 {
		for _, r := range [][2]string{{"z", "v"}, {"K", "h"}, {"k", "H"}, {"b", "a"}} {
			if strings.Contains(input, r[0]) {
				p = strings.ReplaceAll(p, r[1], r[0])
			}
		}
	}
	if strings.Contains(p, "a") && bw['a'] == 0 {
		bw['a'] = 1
	}
	if strings.Contains(p, "b") && bw['b'] == 0 {
		bw['b'] = 1
	}
	var b strings.Builder
	inQuote := false
	var prev byte
	count := 0
	flush := func() {
		sc := prev
		if sc == 'L' {
			sc = 'M'
		}
		if n, w := bw[sc], in[sc]; n == count && w > n {
			b.WriteString(strings.Repeat(string(prev), w-n))
		}
		count = 0
	}
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c != prev && count > 0 {
			flush()
		}
		b.WriteByte(c)
		if c == '\'' {
			if i+1 < len(p) && p[i+1] == '\'' {
				i++
				b.WriteByte('\'')
			} else {
				inQuote = !inQuote
			}
		} else if !inQuote && isLetter(c) {
			prev = c
			count++
		}
	}
	if count > 0 {
		flush()
	}
	return b.String()
}

// fieldInSkeleton reports whether the skeleton s has the calendar field.
func fieldInSkeleton(field int, s string) bool {
	return strings.IndexByte(s, calendarLetters[field]) >= 0
}

// newRangeFormat returns the range format for the requested skeleton.
func newRangeFormat(g *generator, skeleton string) *rangeFormat {
	r := &rangeFormat{
		gen:  g,
		info: newIntervalInfo(g.data),
	}
	r.full = g.bestPattern(skeleton, matchOptions{})
	skel := r.normalizeHours(skeleton)
	date, normDate, time, normTime := splitSkeleton(skel)
	if time != "" && date != "" {
		r.dateTime, _ = g.data.calendar("Cm")
	}
	found := r.setSeparate(normDate, normTime, date, time)
	switch {
	case !found && (time == "" || date != ""):
		// Every field falls back to the full pattern.
		return r
	case time != "" && date == "":
		full := g.bestPattern("yMd"+time, matchOptions{})
		for _, f := range []int{calDate, calMonth, calYear} {
			r.patterns[f] = intervalPattern{
				second: full,
			}
		}
		r.patterns[calEra] = intervalPattern{
			second: g.bestPattern("GyMd"+time, matchOptions{}),
		}
	case time != "" && date != "":
		s := skeleton
		for _, f := range []struct {
			field  int
			letter string
		}{{calDate, "d"}, {calMonth, "M"}, {calYear, "y"}, {calEra, "G"}} {
			if !fieldInSkeleton(f.field, date) {
				s = f.letter + s
				r.patterns[f.field] = intervalPattern{
					second: g.bestPattern(s, matchOptions{}),
				}
			}
		}
		datePattern := g.bestPattern(date, matchOptions{})
		for _, f := range []int{calAmPm, calHour, calMinute} {
			ip := r.patterns[f]
			if ip.first == "" {
				continue
			}
			r.patterns[f] = splitInterval(formatPattern(r.dateTime, ip.first+ip.second, datePattern))
		}
	}
	return r
}

// setSeparate sets the interval patterns of the date or the time skeleton, and reports
// whether the interval formats have a skeleton near them.
func (r *rangeFormat) setSeparate(normDate, normTime, date, time string) bool {
	skel := normDate
	if normTime != "" {
		skel = normTime
	}
	best, diff := r.info.bestSkeleton(skel)
	if best == "" {
		return false
	}
	if date != "" {
		r.datePattern = r.gen.bestPattern(date, matchOptions{})
	}
	if time != "" {
		r.timePattern = r.gen.bestPattern(time, matchOptions{})
	}
	if diff == -1 {
		return false
	}
	if normTime == "" {
		r.setInterval(calDate, skel, best, diff, false)
		ext, extBest, ok := r.setInterval(calMonth, skel, best, diff, true)
		if ok {
			skel, best = ext, extBest
		}
		r.setInterval(calYear, skel, best, diff, true)
		r.setInterval(calEra, skel, best, diff, true)
	} else {
		r.setInterval(calMinute, skel, best, diff, false)
		r.setInterval(calHour, skel, best, diff, false)
		r.setInterval(calAmPm, skel, best, diff, false)
	}
	return true
}

// isFieldIgnored reports whether the pattern p has no field smaller than or equal to the
// calendar field.
func isFieldIgnored(p string, field int) bool {
	for _, it := range parsePattern(p) {
		if it.ch == 0 {
			continue
		}
		f := calendarFieldOfPattern(it.ch)
		if f >= 0 && f >= field {
			return false
		}
	}
	return true
}

// calendarFieldOfPattern returns the calendar field of the pattern letter c, or -1.
func calendarFieldOfPattern(c byte) int {
	switch c {
	case 'G':
		return calEra
	case 'y', 'Y', 'u', 'U', 'r':
		return calYear
	case 'M', 'L', 'Q', 'q', 'w', 'W':
		return calMonth
	case 'd', 'D', 'E', 'c', 'e', 'F', 'g':
		return calDate
	case 'a', 'b', 'B':
		return calAmPm
	case 'h', 'H', 'k', 'K':
		return calHour
	case 'm':
		return calMinute
	case 's':
		return calSecond
	case 'S', 'A':
		return calMillisecond
	}
	return -1
}

// setInterval sets the interval pattern of the calendar field from the interval formats of
// best, adjusted to skel. When extend is true and best has no pattern for the field, it looks
// for one with the letter of the field added, and returns the extended skeletons.
func (r *rangeFormat) setInterval(field int, skel, best string, diff int, extend bool) (string, string, bool) {
	p := r.info.pattern(best, field)
	var extSkel, extBest string
	if p == "" {
		if isFieldIgnored(best, field) {
			return "", "", false
		}
		if field == calAmPm {
			if p = r.info.pattern(best, calHour); p != "" {
				r.patterns[field] = splitInterval(adjustWidths(skel, best, p, diff))
			}
			return "", "", false
		}
		if extend {
			letter := string(calendarLetters[field])
			extSkel = letter + skel
			extBest = letter + best
			p = r.info.pattern(extBest, field)
			if p == "" && diff == 0 {
				if b, d := r.info.bestSkeleton(extBest); b != "" && d != -1 {
					p = r.info.pattern(b, field)
					best = b
				}
			}
			if p != "" && best != extBest && r.info.pattern(extBest, field) != "" {
				best = extBest
			}
		}
	}
	if p != "" {
		r.patterns[field] = splitInterval(adjustWidths(skel, best, p, diff))
	}
	if extend && extSkel != "" {
		return extSkel, extBest, true
	}
	return "", "", false
}

// RangePart is a part of a formatted range of dates.
type RangePart struct {
	Type   string
	Value  string
	Source string
}

// largestDifference returns the largest calendar field in which a and b differ, or calCount.
func largestDifference(a, b localTime) int {
	switch {
	case a.era != b.era || a.gregorianEra != b.gregorianEra:
		return calEra
	case a.year != b.year:
		return calYear
	case a.month != b.month || a.leapMonth != b.leapMonth:
		return calMonth
	case a.day != b.day:
		return calDate
	case (a.hour < 12) != (b.hour < 12):
		return calAmPm
	case a.hour != b.hour:
		return calHour
	case a.minute != b.minute:
		return calMinute
	case a.second != b.second:
		return calSecond
	case a.millisecond != b.millisecond:
		return calMillisecond
	}
	return calCount
}

// fieldPart is a part of a formatted range with the letter of its field.
type fieldPart struct {
	Part
	letter byte
}

// FormatRange returns the parts of the range of dates from x to y, in milliseconds since the
// epoch.
func (fm *Formatter) FormatRange(x, y int64, plain bool) []RangePart {
	r := fm.rangeFormat()
	ax := fm.localTime(x + fm.zoneAt(x, plain).offset)
	ay := fm.localTime(y + fm.zoneAt(y, plain).offset)
	field := largestDifference(ax, ay)
	shared := func() []RangePart {
		var out []RangePart
		for _, p := range fm.Format(x, plain) {
			out = append(out, RangePart{
				Type:   p.Type,
				Value:  p.Value,
				Source: "shared",
			})
		}
		return out
	}
	if field == calCount {
		return shared()
	}
	var parts []fieldPart
	ip := r.patterns[field]
	switch {
	case ip.isEmpty():
		if isFieldIgnored(r.full, field) {
			return shared()
		}
		parts = fm.fallback(r, r.full, x, y, plain, field >= calAmPm)
	case ip.first == "":
		parts = fm.fallback(r, ip.second, x, y, plain, field >= calAmPm)
	default:
		parts = fm.fieldParts(ip.first, x, plain)
		parts = append(parts, fm.fieldParts(ip.second, y, plain)...)
	}
	return withSources(mergeLiterals(parts))
}

// rangeFormat returns the range format of the formatter.
func (fm *Formatter) rangeFormat() *rangeFormat {
	if fm.ranges == nil {
		l := fm.f.locale
		if hc := fm.f.hourCycle; hc != "" {
			l = withKeyword(l, "hc", hc)
		}
		fm.ranges = newRangeFormat(generatorFor(l, fm.f.calendar), fm.f.rangeSkeleton())
	}
	return fm.ranges
}

// withKeyword returns the language tag l with the Unicode extension keyword key set to value.
func withKeyword(l, key, value string) string {
	base, ext, ok := strings.Cut(l, "-u-")
	if !ok {
		return l + "-u-" + key + "-" + value
	}
	subtags := strings.Split(ext, "-")
	var out []string
	for i := 0; i < len(subtags); i++ {
		if subtags[i] == key {
			i++
			for i < len(subtags) && len(subtags[i]) > 2 {
				i++
			}
			i--
			continue
		}
		out = append(out, subtags[i])
	}
	out = append(out, key, value)
	return base + "-u-" + strings.Join(out, "-")
}

// fallback returns the parts of the range from x to y with the fallback pattern, formatting
// the date once when both are on the same day.
func (fm *Formatter) fallback(r *rangeFormat, full string, x, y int64, plain, sameDay bool) []fieldPart {
	pattern := func(p string, z int64) []fieldPart {
		return fm.fieldParts(p, z, plain)
	}
	formatRange := func(p string) []fieldPart {
		var out []fieldPart
		for _, s := range splitMessage(r.info.fallback) {
			switch s {
			case "{0}":
				out = append(out, pattern(p, x)...)
			case "{1}":
				out = append(out, pattern(p, y)...)
			default:
				out = append(out, fieldPart{Type: "literal", Value: s})
			}
		}
		return out
	}
	if sameDay && r.datePattern != "" && r.timePattern != "" {
		var out []fieldPart
		for _, s := range splitMessage(r.dateTime) {
			switch s {
			case "{0}":
				out = append(out, formatRange(r.timePattern)...)
			case "{1}":
				out = append(out, pattern(r.datePattern, x)...)
			default:
				for _, it := range parsePattern(s) {
					out = append(out, fieldPart{Type: "literal", Value: it.lit})
				}
			}
		}
		return out
	}
	return formatRange(full)
}

// splitMessage splits the message pattern p into its arguments and the texts between them.
func splitMessage(p string) []string {
	var out []string
	for len(p) > 0 {
		i := strings.IndexByte(p, '{')
		if i < 0 || i+2 >= len(p) || p[i+2] != '}' {
			out = append(out, p)
			break
		}
		if i > 0 {
			out = append(out, p[:i])
		}
		out = append(out, p[i:i+3])
		p = p[i+3:]
	}
	return out
}

// fieldParts returns the parts of t formatted with the pattern p, with their letters.
func (fm *Formatter) fieldParts(p string, t int64, plain bool) []fieldPart {
	items := parsePattern(p)
	var out []fieldPart
	letters := []byte{}
	for _, it := range items {
		if it.ch != 0 {
			letters = append(letters, it.ch)
		}
	}
	li := 0
	for _, part := range fm.formatItems(items, t, plain, fm.f.patternNumbers(p)) {
		fp := fieldPart{Part: part}
		if part.Type != "literal" {
			fp.letter = letters[li]
			li++
		}
		out = append(out, fp)
	}
	return out
}

// mergeLiterals merges adjacent literal parts.
func mergeLiterals(parts []fieldPart) []fieldPart {
	var out []fieldPart
	for _, p := range parts {
		if p.Value == "" && p.Type == "literal" {
			continue
		}
		if n := len(out); n > 0 && p.Type == "literal" && out[n-1].Type == "literal" {
			out[n-1].Value += p.Value
			continue
		}
		out = append(out, p)
	}
	return out
}

// withSources returns parts with the sources of ICU's spans: the fields that appear twice,
// and what is between them, come from the start and the end.
func withSources(parts []fieldPart) []RangePart {
	s1a, s1b, s2a, s2b := len(parts), -1, len(parts), -1
	for i, p := range parts {
		if p.letter == 0 {
			continue
		}
		for j := i + 1; j < len(parts); j++ {
			if parts[j].letter != p.letter {
				continue
			}
			s1a = min(s1a, i)
			s1b = max(s1b, i)
			s2a = min(s2a, j)
			s2b = max(s2b, j)
			break
		}
	}
	out := make([]RangePart, len(parts))
	for i, p := range parts {
		src := "shared"
		switch {
		case s1a <= i && i <= s1b:
			src = "startRange"
		case s2a <= i && i <= s2b:
			src = "endRange"
		}
		out[i] = RangePart{
			Type:   p.Type,
			Value:  webCompatible(p.Value),
			Source: src,
		}
	}
	return out
}
