// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package dates

import (
	"strconv"
	"strings"
	"sync"

	"github.com/hajimehoshi/ekuma/internal/datetime"
	"github.com/hajimehoshi/ekuma/internal/intl/cldr"
	"github.com/hajimehoshi/ekuma/internal/intl/number"
	"github.com/hajimehoshi/ekuma/internal/temporal"
)

// msPerDay is the number of milliseconds in a day.
const msPerDay = 86400000

// cldrZoneID returns the CLDR identifier of the zone with the identifier id.
func cldrZoneID(id string) string {
	if c, ok := cldr.ZoneAliases.Lookup(id); ok {
		return c
	}
	return id
}

var (
	zoneCountriesOnce sync.Once
	// zoneCountries maps the CLDR identifiers of zones to their countries.
	zoneCountries map[string]string
	// countryZones counts the zones of each country.
	countryZones map[string]int
)

// countryOf returns the country of the zone with the CLDR identifier id, and reports whether
// it is the primary zone of the country.
func countryOf(id string) (country string, primary bool) {
	zoneCountriesOnce.Do(func() {
		zoneCountries = map[string]string{}
		countryZones = map[string]int{}
		for _, z := range datetime.PrimaryZones() {
			c := datetime.CountryOfZone(z)
			if c == "" {
				continue
			}
			zoneCountries[cldrZoneID(z)] = c
			countryZones[c]++
		}
	})
	country = zoneCountries[id]
	if country == "" {
		return "", false
	}
	if countryZones[country] == 1 {
		return country, true
	}
	p, _ := cldr.PrimaryZones.Lookup(country)
	return country, p == id
}

// metazoneOf returns the metazone that the zone with the CLDR identifier id uses at the Unix
// time sec.
func metazoneOf(id string, sec int64) string {
	v, ok := cldr.MetaZoneUsages.Lookup(id)
	if !ok {
		return ""
	}
	for u := range strings.SplitSeq(v, ";") {
		f := strings.Split(u, " ")
		if f[1] != "" {
			if from, _ := strconv.ParseInt(f[1], 10, 64); sec < from {
				continue
			}
		}
		if f[2] != "" {
			if to, _ := strconv.ParseInt(f[2], 10, 64); sec >= to {
				continue
			}
		}
		return f[0]
	}
	return ""
}

// zoneAt is a time zone at an instant.
type zoneAt struct {
	tz temporal.TimeZone
	// t is the instant in milliseconds since the epoch.
	t int64
	// offset is the offset of tz at t in milliseconds.
	offset int64
}

// epochNs returns t milliseconds since the epoch in nanoseconds.
func epochNs(t int64) temporal.Int128 {
	return temporal.Mul64(t, 1e6)
}

// offsetAt returns the offset of tz at t milliseconds since the epoch in milliseconds.
func offsetAt(tz temporal.TimeZone, t int64) int64 {
	return tz.OffsetNanosecondsFor(epochNs(t)) / 1e6
}

// transitionAt returns the time of the first transition of tz after t when next is true, or of
// the last one at or before t when it is false, in milliseconds since the epoch, and reports
// false when there is none.
func transitionAt(tz temporal.TimeZone, t int64, next bool) (int64, bool) {
	ns, ok := tz.Transition(epochNs(t), next)
	if !ok {
		return 0, false
	}
	q, _ := ns.FloorDivInt64(1e6)
	ms, _ := q.Int64()
	return ms, true
}

// isDST reports whether the offset of tz at t is daylight saving time: it is greater than the
// offsets of the periods before and after it.
func isDST(tz temporal.TimeZone, t int64) bool {
	if tz.IsOffset() {
		return false
	}
	off := offsetAt(tz, t)
	prev, ok := transitionAt(tz, t+1, false)
	if !ok {
		return false
	}
	next, ok := transitionAt(tz, t, true)
	if !ok {
		return false
	}
	return off > offsetAt(tz, prev-1) && off > offsetAt(tz, next)
}

// dstCheckRange is the range around an instant in which a transition into or out of daylight
// saving time makes a generic name preferred over a standard name.
const dstCheckRange = 184 * msPerDay

// usesDSTAround reports whether tz is in daylight saving time around t.
func usesDSTAround(tz temporal.TimeZone, t int64) bool {
	if prev, ok := transitionAt(tz, t+1, false); ok && t-prev < dstCheckRange && isDST(tz, prev-1) {
		return true
	}
	if next, ok := transitionAt(tz, t, true); ok && next-t < dstCheckRange && isDST(tz, next) {
		return true
	}
	return false
}

// zoneFormatter formats time zones in a locale.
type zoneFormatter struct {
	data   *localeData
	region string
	digits []string
}

// name returns the field key of the time zone names.
func (z *zoneFormatter) name(key string) string {
	v, _ := z.data.lookup(cldr.TimeZoneNames, key)
	return v
}

// gmt returns the localized GMT format of the offset in milliseconds.
func (z *zoneFormatter) gmt(offset int64, short bool) string {
	if offset == 0 {
		if s := z.name("0"); s != "" {
			return s
		}
		return "GMT"
	}
	hf := z.name("h")
	if hf == "" {
		hf = "+HH:mm;-HH:mm"
	}
	pos, neg, _ := strings.Cut(hf, ";")
	p := pos
	if offset < 0 {
		p = neg
		offset = -offset
	}
	sec := offset / 1000
	h, m, s := sec/3600, sec/60%60, sec%60
	var b strings.Builder
	for _, it := range parsePattern(p) {
		switch it.ch {
		case 0:
			b.WriteString(it.lit)
		case 'H':
			if short {
				b.WriteString(strconv.FormatInt(h, 10))
			} else {
				b.WriteString(pad(h, 2))
			}
		case 'm':
			if short && m == 0 && s == 0 {
				// The short format leaves out zero minutes with their separator.
				str := b.String()
				b.Reset()
				b.WriteString(strings.TrimRight(str, ":."))
				return z.wrapGMT(b.String())
			}
			b.WriteString(pad(m, 2))
			if s != 0 {
				b.WriteString(":" + pad(s, 2))
			}
		}
	}
	return z.wrapGMT(b.String())
}

// wrapGMT returns the offset s in the GMT format of the locale.
func (z *zoneFormatter) wrapGMT(s string) string {
	g := z.name("g")
	if g == "" {
		g = "GMT{0}"
	}
	return formatPattern(g, number.Transliterate(z.digits, s))
}

// pad returns n in decimal with at least w digits.
func pad(n int64, w int) string {
	s := strconv.FormatInt(n, 10)
	for len(s) < w {
		s = "0" + s
	}
	return s
}

// exemplarCity returns the exemplar city of the zone with the CLDR identifier id.
func (z *zoneFormatter) exemplarCity(id string) string {
	if s := z.name("z" + id + "|e"); s != "" {
		return s
	}
	if strings.HasPrefix(id, "Etc/") || strings.HasPrefix(id, "SystemV/") {
		return ""
	}
	i := strings.LastIndexByte(id, '/')
	if i < 0 {
		return ""
	}
	return strings.ReplaceAll(id[i+1:], "_", " ")
}

// territoryName returns the name of the region r.
func (z *zoneFormatter) territoryName(r string) string {
	v, _ := z.data.lookup(cldr.TerritoryNames, r)
	return v
}

// specific returns the specific name of the zone id at sec, long or short, or an empty
// string.
func (z *zoneFormatter) specific(id string, sec int64, long, daylight bool) string {
	key := "s"
	if long {
		key = "l"
	}
	if daylight {
		key += "d"
	} else {
		key += "s"
	}
	if s := z.name("z" + id + "|" + key); s != "" {
		return s
	}
	if mz := metazoneOf(id, sec); mz != "" {
		return z.name("m" + mz + "|" + key)
	}
	return ""
}

// genericLocation returns the generic location name of the zone id.
func (z *zoneFormatter) genericLocation(id string) string {
	country, primary := countryOf(id)
	if country == "" {
		return ""
	}
	var loc string
	if primary {
		loc = z.territoryName(country)
	} else {
		loc = z.exemplarCity(id)
	}
	if loc == "" {
		return ""
	}
	return formatPattern(z.name("r"), loc)
}

// goldenZone returns the zone that stands for the metazone mz in the region.
func goldenZone(mz, region string) string {
	if g, ok := cldr.MetaZoneGoldenZones.Lookup(mz + "/" + region); ok {
		return g
	}
	g, _ := cldr.MetaZoneGoldenZones.Lookup(mz + "/001")
	return g
}

// partialLocation returns the name mzName of a metazone qualified with the location of the
// zone id.
func (z *zoneFormatter) partialLocation(id, mz, mzName string) string {
	var loc string
	if country, _ := countryOf(id); country != "" {
		if goldenZone(mz, country) == id {
			loc = z.territoryName(country)
		} else {
			loc = z.exemplarCity(id)
		}
	} else {
		loc = z.exemplarCity(id)
		if loc == "" {
			loc = id
		}
	}
	f := z.name("f")
	if f == "" {
		f = "{1} ({0})"
	}
	return formatPattern(f, loc, mzName)
}

// genericNonLocation returns the generic name of the zone of za other than its location name,
// or an empty string.
func (z *zoneFormatter) genericNonLocation(id string, za zoneAt, long bool) string {
	key := "sg"
	std := "ss"
	if long {
		key = "lg"
		std = "ls"
	}
	if s := z.name("z" + id + "|" + key); s != "" {
		return s
	}
	sec := floorDiv(za.t, 1000)
	mz := metazoneOf(id, sec)
	if mz == "" {
		return ""
	}
	mzGeneric := z.name("m" + mz + "|" + key)
	if !isDST(za.tz, za.t) && !usesDSTAround(za.tz, za.t) {
		// A zone that uses no daylight saving time around the instant has the standard name.
		s := z.name("z" + id + "|" + std)
		if s == "" {
			s = z.name("m" + mz + "|" + std)
		}
		if s != "" && !strings.EqualFold(s, mzGeneric) {
			return s
		}
	}
	if mzGeneric == "" {
		return ""
	}
	golden := goldenZone(mz, z.region)
	if golden == "" || golden == id {
		return mzGeneric
	}
	gz, ok := temporal.GetAvailableNamedTimeZone(golden, temporal.UTC)
	if !ok {
		return mzGeneric
	}
	off := za.offset
	dst := isDST(za.tz, za.t)
	// The offsets of the golden zone are taken at the same wall time.
	gt := za.t + off
	goff := offsetAt(gz, gt)
	gt = za.t + off - goff
	goff = offsetAt(gz, gt)
	if goff != off || isDST(gz, gt) != dst {
		return z.partialLocation(id, mz, mzGeneric)
	}
	return mzGeneric
}

// format returns the time zone of za formatted with the pattern field of the letter c and the
// length n.
func (z *zoneFormatter) format(c byte, n int, za zoneAt) string {
	var id string
	if !za.tz.IsOffset() {
		id = cldrZoneID(za.tz.ID())
	}
	sec := floorDiv(za.t, 1000)
	long := n >= 4
	var s string
	switch c {
	case 'z':
		if id != "" {
			s = z.specific(id, sec, long, isDST(za.tz, za.t))
		}
	case 'v':
		if id != "" {
			s = z.genericNonLocation(id, za, long)
			if s == "" {
				s = z.genericLocation(id)
			}
		}
	case 'V':
		if id != "" && n == 4 {
			s = z.genericLocation(id)
		}
	case 'O':
	}
	if s == "" {
		return z.gmt(za.offset, !long)
	}
	return s
}

// floorDiv returns x divided by y rounded toward negative infinity.
func floorDiv(x, y int64) int64 {
	q := x / y
	if x%y != 0 && (x < 0) != (y < 0) {
		q--
	}
	return q
}
