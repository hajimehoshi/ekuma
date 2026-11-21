// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:generate go run gen_zonelinks.go

package datetime

import (
	"path/filepath"
	"strings"
	"time"
)

// Zone is a [TimeZone] whose offsets are known to the second, along with the instants at
// which they change.
type Zone interface {
	TimeZone

	// OffsetSeconds returns the offset from UTC in seconds at the Unix time sec.
	OffsetSeconds(sec int64) int

	// IsDST reports whether the zone observes daylight saving time at Unix time sec.
	IsDST(sec int64) bool

	// Transition returns the first Unix time after sec at which the offset changes when next
	// is true, or the last such time before sec when next is false. It reports false when
	// there is none.
	Transition(sec int64, next bool) (int64, bool)
}

// maxTransitionSteps bounds the zone periods that a search for a transition walks through.
// Periods whose offset does not change only change the abbreviation, and zones have few.
const maxTransitionSteps = 1000

func (z locationZone) OffsetSeconds(sec int64) int {
	sec = max(-maxSeconds, min(maxSeconds, sec))
	_, off := time.Unix(sec, 0).In(z.loc).Zone()
	return off
}

func (z locationZone) IsDST(sec int64) bool {
	return time.Unix(sec, 0).In(z.loc).IsDST()
}

func (z locationZone) Transition(sec int64, next bool) (int64, bool) {
	if next {
		off := z.OffsetSeconds(sec)
		t := sec
		for range maxTransitionSteps {
			_, end := time.Unix(t, 0).In(z.loc).ZoneBounds()
			if end.IsZero() {
				return 0, false
			}
			if end.Unix() <= t {
				// After the last recurring transition of a leap year, ZoneBounds
				// can give December 31 00:00 UTC as the year end. The next year's
				// rules start on January 1.
				end = end.Add(24 * time.Hour)
			}
			t = end.Unix()
			if t > maxSeconds {
				return 0, false
			}
			if z.OffsetSeconds(t) != off {
				return t, true
			}
		}
		return 0, false
	}
	t := sec - 1
	off := z.OffsetSeconds(t)
	for range maxTransitionSteps {
		start, _ := time.Unix(t, 0).In(z.loc).ZoneBounds()
		if start.IsZero() {
			return 0, false
		}
		s := start.Unix()
		if s < -maxSeconds {
			return 0, false
		}
		if z.OffsetSeconds(s-1) != off {
			return s, true
		}
		t = s - 1
	}
	return 0, false
}

// LoadZone returns the time zone that the IANA time zone identifier name stands for, matched
// without regard to ASCII case, together with the identifier in the case of the time zone
// database and the primary identifier that name is a link to. It reports false when the
// platform has no such time zone.
func LoadZone(name string) (id, primary string, z Zone, ok bool) {
	if !isZoneName(name) {
		return "", "", nil, false
	}
	return loadZone(name)
}

// isZoneName reports whether name has the form of an identifier of the IANA time zone database:
// at most maxZoneNameLength bytes of path components separated by slashes, each of at most
// maxZoneComponentLength ASCII letters, digits, '.', '-', '_' or '+', and not starting with
// '.' or '-'.
func isZoneName(name string) bool {
	if name == "" || len(name) > maxZoneNameLength {
		return false
	}
	for c := range strings.SplitSeq(name, "/") {
		if c == "" || len(c) > maxZoneComponentLength || c[0] == '.' || c[0] == '-' {
			return false
		}
		for i := range len(c) {
			switch b := c[i]; {
			case 'a' <= b && b <= 'z', 'A' <= b && b <= 'Z', '0' <= b && b <= '9':
			case b == '.', b == '-', b == '_', b == '+':
			default:
				return false
			}
		}
	}
	return true
}

const (
	// maxZoneComponentLength is the longest path component of an identifier that the theory
	// file of the IANA time zone database allows.
	maxZoneComponentLength = 14

	// maxZoneNameLength bounds the length of an identifier well above the longest one in the
	// database, America/Argentina/ComodRivadavia.
	maxZoneNameLength = 64
)

// ZoneName returns the identifier of tz, or an empty string if it is unknown.
func ZoneName(tz TimeZone) string {
	if tz, ok := tz.(locationZone); ok {
		name := tz.loc.String()
		// Only the host's placeholder and zoneinfo paths need platform resolution.
		if tz.loc == time.Local && (name == "Local" || filepath.IsAbs(name)) {
			return localZoneName()
		}
		return name
	}
	return platformZoneName(tz)
}

// ZoneOf returns the rules of tz, with its identifier when known.
func ZoneOf(tz TimeZone) (id string, z Zone, ok bool) {
	id = ZoneName(tz)
	if z, ok = tz.(Zone); !ok {
		_, _, z, ok = LoadZone(id)
	}
	return id, z, ok
}

// primaryOfUTC returns the primary identifier for the identifiers that ECMA-262 treats as UTC.
func primaryOfUTC(id string) string {
	switch id {
	case "Etc/UTC", "Etc/GMT", "GMT":
		return "UTC"
	}
	return id
}

// zoneTabEntry is a zone of the IANA time zone database.
type zoneTabEntry struct {
	// zone is the primary identifier of the zone.
	zone string
	// country is the ISO 3166 code of the country that zone.tab gives the zone, or empty.
	country string
}

// PrimaryZones returns the primary identifiers of the zones of the IANA time zone database,
// sorted.
func PrimaryZones() []string {
	zones := make([]string, len(zoneTab))
	for i, e := range zoneTab {
		zones[i] = e.zone
	}
	return zones
}

// ZonesOfCountry returns the primary identifiers of the zones that the IANA time zone
// database gives the country with the ISO 3166 code country, sorted.
func ZonesOfCountry(country string) []string {
	var zones []string
	for _, e := range zoneTab {
		if e.country == country {
			zones = append(zones, e.zone)
		}
	}
	return zones
}

// CountryOfZone returns the ISO 3166 code of the country that the IANA time zone database
// gives the zone with the primary identifier zone, or an empty string when it gives none.
func CountryOfZone(zone string) string {
	for _, e := range zoneTab {
		if e.zone == zone {
			return e.country
		}
	}
	return ""
}
