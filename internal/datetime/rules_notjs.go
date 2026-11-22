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

// loadedZones caches the time zones that loadZone found by the lower-case name, as the results
// must not change while a program runs. Names that were not found are not cached, so that the
// cache is bounded by the size of the time zone database.
var loadedZones sync.Map

// zoneIDByLowerCase maps IANA zone and link names to their database spelling.
var zoneIDByLowerCase = sync.OnceValue(func() map[string]string {
	ids := map[string]string{
		"factory": "Factory", // zoneTab omits Factory.
	}
	for _, e := range zoneTab {
		ids[strings.ToLower(e.zone)] = e.zone
	}
	for link, target := range zoneLinks {
		ids[strings.ToLower(link)] = link
		ids[strings.ToLower(target)] = target
	}
	return ids
})

func loadZone(name string) (string, string, Zone, bool) {
	key := strings.ToLower(name)
	if v, ok := loadedZones.Load(key); ok {
		l := v.(loadedZone)
		return l.id, l.primary, l.zone, l.ok
	}
	id, ok := zoneIDByLowerCase()[key]
	if !ok {
		return "", "", nil, false
	}
	l := findZone(id)
	if !l.ok {
		return "", "", nil, false
	}
	v, _ := loadedZones.LoadOrStore(key, l)
	l = v.(loadedZone)
	return l.id, l.primary, l.zone, l.ok
}

// findZone loads the rules for an IANA identifier in its database spelling.
func findZone(id string) loadedZone {
	target, isLink := zoneLinks[id]
	if !isLink {
		target = id
	}
	z, ok := findZoneInSources(target)
	if !ok && isLink {
		// An older database may have rules only under the link name.
		z, ok = findZoneInSources(id)
	}
	if !ok {
		return loadedZone{}
	}
	return loadedZone{
		id:      id,
		primary: primaryOfUTC(target),
		zone:    z,
		ok:      true,
	}
}

// findZoneInSources looks up the rules for name in zoneinfo directories and Go's time sources.
func findZoneInSources(name string) (Zone, bool) {
	if runtime.GOOS != "windows" {
		dirs := zoneDirs
		if z := os.Getenv("ZONEINFO"); z != "" {
			dirs = append([]string{z}, dirs...)
		}
		for _, dir := range dirs {
			if z, ok := findZoneInDir(dir, name); ok {
				return z, true
			}
		}
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, false
	}
	return locationZone{
		loc: loc,
	}, true
}

// findZoneInDir loads rules from dir, matching path components without regard to ASCII case.
func findZoneInDir(dir, name string) (Zone, bool) {
	path := dir
	for elem := range strings.SplitSeq(name, "/") {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, false
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
			return nil, false
		}
		path = filepath.Join(path, found)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	loc, err := time.LoadLocationFromTZData(name, data)
	if err != nil {
		return nil, false
	}
	return locationZone{
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
		return windowsZoneName()
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

func platformZoneName(tz TimeZone) string {
	return ""
}

// windowsZoneName returns the IANA time zone identifier of the time zone that Windows is set
// to, or an empty string when it is not known. It is set on Windows only.
var windowsZoneName = func() string {
	return ""
}
