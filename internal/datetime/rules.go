// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:generate go run gen_zonelinks.go

package datetime

import (
	"strings"
	"time"
)

// Zone is a [TimeZone] whose offsets are known to the second, along with the instants at
// which they change.
type Zone interface {
	TimeZone

	// OffsetSeconds returns the offset from UTC in seconds at the Unix time sec.
	OffsetSeconds(sec int64) int

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

func (z locationZone) Transition(sec int64, next bool) (int64, bool) {
	if next {
		off := z.OffsetSeconds(sec)
		t := sec
		for range maxTransitionSteps {
			_, end := time.Unix(t, 0).In(z.loc).ZoneBounds()
			if end.IsZero() {
				return 0, false
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
	if name == "" || name[0] == '/' || name[0] == '.' || strings.Contains(name, "..") || strings.Contains(name, "\\") {
		return "", "", nil, false
	}
	return loadZone(name)
}

// ZoneOf returns tz as a [Zone], with its IANA time zone identifier when it is known.
func ZoneOf(tz TimeZone) (id string, z Zone, ok bool) {
	switch tz := tz.(type) {
	case locationZone:
		if tz.loc == time.Local {
			return localZoneName(), tz, true
		}
		return tz.loc.String(), tz, true
	case Zone:
		return zoneName(tz), tz, true
	}
	return "", nil, false
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
