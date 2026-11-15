// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package datetime

import (
	"bytes"
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

// findZone looks up name in the directories of the time zone database, and then in the
// sources of the time package.
func findZone(name string) loadedZone {
	dirs := zoneDirs
	if z := os.Getenv("ZONEINFO"); z != "" {
		dirs = append([]string{z}, dirs...)
	}
	if runtime.GOOS != "windows" {
		for _, dir := range dirs {
			if l, ok := findZoneInDir(dir, name); ok {
				return l
			}
		}
	}
	loc, err := time.LoadLocation(name)
	if err != nil || loc.String() == "Local" {
		return loadedZone{}
	}
	return loadedZone{
		id:      name,
		primary: primaryOfUTC(name),
		zone: locationZone{
			loc: loc,
		},
		ok: true,
	}
}

// findZoneInDir finds the file of name in dir, matching each path element without regard to
// ASCII case.
func findZoneInDir(dir, name string) (loadedZone, bool) {
	path := dir
	var elems []string
	for elem := range strings.SplitSeq(name, "/") {
		entries, err := os.ReadDir(path)
		if err != nil {
			return loadedZone{}, false
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
			return loadedZone{}, false
		}
		elems = append(elems, found)
		path = filepath.Join(path, found)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return loadedZone{}, false
	}
	id := strings.Join(elems, "/")
	loc, err := time.LoadLocationFromTZData(id, data)
	if err != nil {
		return loadedZone{}, false
	}
	// The files of links are symbolic links, hard links or copies depending on the system, so
	// zones with the same data are taken to be links to one primary zone, which the data stands
	// for. The leading NUL keeps it apart from identifiers.
	primary := primaryOfUTC(id)
	if primary != "UTC" {
		if utc, err := os.ReadFile(filepath.Join(dir, "Etc", "UTC")); err == nil && bytes.Equal(utc, data) {
			primary = "UTC"
		} else {
			primary = "\x00" + string(data)
		}
	}
	return loadedZone{
		id:      id,
		primary: primary,
		zone: locationZone{
			loc: loc,
		},
		ok: true,
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
