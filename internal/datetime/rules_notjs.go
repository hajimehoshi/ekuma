// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package datetime

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// zoneDirs are the directories of the time zone database that the time package reads too.
var zoneDirs = []string{
	"/usr/share/zoneinfo/",
	"/usr/share/lib/zoneinfo/",
	"/usr/lib/locale/TZ/",
	"/etc/zoneinfo/",
}

// loadedZone is a time zone that loadZone found, or not.
type loadedZone struct {
	id, primary string
	zone        Zone
	ok          bool
}

// loadedZones caches the results of loadZone by the lower-case name, as the results must not
// change while a program runs.
var loadedZones sync.Map

func loadZone(name string) (string, string, Zone, bool) {
	key := strings.ToLower(name)
	if v, ok := loadedZones.Load(key); ok {
		l := v.(loadedZone)
		return l.id, l.primary, l.zone, l.ok
	}
	l := findZone(name)
	v, _ := loadedZones.LoadOrStore(key, l)
	l = v.(loadedZone)
	return l.id, l.primary, l.zone, l.ok
}

// findZone finds the time zone that name stands for, resolving a link to its zone.
func findZone(name string) loadedZone {
	link, target, isLink := findZoneLink(name)
	if !isLink {
		id, z, ok := findZoneInSources(name)
		if !ok {
			return loadedZone{}
		}
		return loadedZone{
			id:      id,
			primary: primaryOfUTC(id),
			zone:    z,
			ok:      true,
		}
	}
	// The sources might not have the link, which some systems put in an optional package. A
	// database older than the table might have the link but not the zone that it resolves to.
	z, ok := findZoneOfLink(target, link)
	if !ok {
		return loadedZone{}
	}
	return loadedZone{
		id:      link,
		primary: primaryOfUTC(target),
		zone:    z,
		ok:      true,
	}
}

// findZoneLink finds the link of the IANA time zone database that name matches without regard
// to ASCII case, and the zone that it resolves to.
func findZoneLink(name string) (link, zone string, ok bool) {
	if zone, ok := zoneLinks[name]; ok {
		return name, zone, true
	}
	for link, zone := range zoneLinks {
		if strings.EqualFold(link, name) {
			return link, zone, true
		}
	}
	return "", "", false
}

// findZoneOfLink finds the zone of a link in the sources by the name of the zone that the link
// resolves to, or else by the name of the link.
func findZoneOfLink(target, link string) (Zone, bool) {
	if _, z, ok := findZoneInSources(target); ok {
		return z, true
	}
	if _, z, ok := findZoneInSources(link); ok {
		return z, true
	}
	return nil, false
}

// findZoneInSources looks up name in the directories of the time zone database, and then in
// the sources of the time package. It returns the identifier in the case of the source.
func findZoneInSources(name string) (string, Zone, bool) {
	dirs := zoneDirs
	if z := os.Getenv("ZONEINFO"); z != "" {
		dirs = append([]string{z}, dirs...)
	}
	if runtime.GOOS == "windows" {
		// The sources of the time package match names in their case only.
		name = zoneIDInCase(name)
	} else {
		for _, dir := range dirs {
			if id, z, ok := findZoneInDir(dir, name); ok {
				return id, z, true
			}
		}
	}
	loc, err := time.LoadLocation(name)
	if err != nil || loc.String() == "Local" {
		return "", nil, false
	}
	return name, locationZone{
		loc: loc,
	}, true
}

// zoneIDInCase returns the identifier of the zone of the IANA time zone database that name
// matches without regard to ASCII case, or name when there is none.
func zoneIDInCase(name string) string {
	for _, e := range zoneTab {
		if strings.EqualFold(e.zone, name) {
			return e.zone
		}
	}
	// zoneTab has UTC for these zones.
	for _, id := range []string{"Etc/UTC", "Etc/GMT"} {
		if strings.EqualFold(id, name) {
			return id
		}
	}
	return name
}

// findZoneInDir finds the file of name in dir, matching each path element without regard to
// ASCII case. It returns the identifier in the case of the file.
func findZoneInDir(dir, name string) (string, Zone, bool) {
	path := dir
	var elems []string
	for elem := range strings.SplitSeq(name, "/") {
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", nil, false
		}
		found := ""
		for _, e := range entries {
			if e.Name() == elem {
				found = elem
				break
			}
			if found == "" && strings.EqualFold(e.Name(), elem) {
				found = e.Name()
			}
		}
		if found == "" {
			return "", nil, false
		}
		elems = append(elems, found)
		path = filepath.Join(path, found)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, false
	}
	id := strings.Join(elems, "/")
	loc, err := time.LoadLocationFromTZData(id, data)
	if err != nil {
		return "", nil, false
	}
	return id, locationZone{
		loc: loc,
	}, true
}

// localZoneName returns the IANA time zone identifier of [time.Local], or an empty string
// when it is not known.
func localZoneName() string {
	tz, ok := os.LookupEnv("TZ")
	if ok {
		tz = strings.TrimPrefix(tz, ":")
		if tz == "" {
			return "UTC"
		}
		if !filepath.IsAbs(tz) {
			return tz
		}
		return zoneNameFromPath(tz)
	}
	if runtime.GOOS == "windows" {
		return ""
	}
	target, err := os.Readlink("/etc/localtime")
	if err != nil {
		return ""
	}
	return zoneNameFromPath(target)
}

// zoneNameFromPath returns the identifier of the time zone file at path, which is the part
// after a directory named zoneinfo.
func zoneNameFromPath(path string) string {
	path = filepath.ToSlash(path)
	i := strings.LastIndex(path, "zoneinfo/")
	if i < 0 {
		return ""
	}
	return path[i+len("zoneinfo/"):]
}

// zoneName returns the identifier of a Zone that is not a locationZone.
func zoneName(z Zone) string {
	return ""
}
