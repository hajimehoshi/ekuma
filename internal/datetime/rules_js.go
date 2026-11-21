// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime

import (
	"strings"
	"sync"
	"syscall/js"
	"time"
	_ "time/tzdata" // Supply zone rules on hosts without a zoneinfo filesystem.
)

// intlZone holds the rules of a zone identified by the JavaScript host.
type intlZone struct {
	locationZone
}

func (z intlZone) Offset(t float64) (float64, string) {
	off, _ := z.locationZone.Offset(t)
	// Wasm Date strings omit the zone abbreviation.
	return off, ""
}

// hostZoneID resolves name through Intl.DateTimeFormat, or the host's default zone if name is empty.
func hostZoneID(name string) (id string) {
	intl := js.Global().Get("Intl")
	if intl.Type() != js.TypeObject {
		return ""
	}
	f := intl.Get("DateTimeFormat")
	if f.Type() != js.TypeFunction {
		return ""
	}
	opts := map[string]any{}
	if name != "" {
		opts["timeZone"] = name
	}
	// Intl.DateTimeFormat throws for an unrecognized identifier.
	defer func() {
		if recover() != nil {
			id = ""
		}
	}()
	v := f.New("en-US", opts).Call("resolvedOptions").Get("timeZone")
	if v.Type() != js.TypeString {
		return ""
	}
	return v.String()
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

func loadZone(name string) (string, string, Zone, bool) {
	key := strings.ToLower(name)
	if v, ok := loadedZones.Load(key); ok {
		l := v.(loadedZone)
		return l.id, l.primary, l.zone, l.ok
	}
	l := findZone(name)
	if !l.ok {
		return "", "", nil, false
	}
	v, _ := loadedZones.LoadOrStore(key, l)
	l = v.(loadedZone)
	return l.id, l.primary, l.zone, l.ok
}

// findZone looks name up in the time zones of the JavaScript host.
func findZone(name string) loadedZone {
	primary := hostZoneID(name)
	if primary == "" || primary == "Local" {
		return loadedZone{}
	}
	// The host resolves a name to its own primary identifier, which can be another name, so the
	// identifier keeps the given name in the case of the database.
	id, ok := zoneIDByLowerCase()[strings.ToLower(name)]
	if !ok {
		id = name
		if strings.EqualFold(primary, name) {
			id = primary
		}
	}
	// Offsets and the boundaries of their periods must come from the same rules.
	loc, err := time.LoadLocation(primary)
	if err != nil {
		return loadedZone{}
	}
	return loadedZone{
		id:      id,
		primary: primaryOfUTC(primary),
		zone: intlZone{
			locationZone: locationZone{
				loc: loc,
			},
		},
		ok: true,
	}
}

// zoneIDByLowerCase maps the lower-case form of each time zone identifier that the IANA link
// table or the JavaScript host lists to the identifier.
var zoneIDByLowerCase = sync.OnceValue(func() map[string]string {
	m := map[string]string{}
	for link, zone := range zoneLinks {
		m[strings.ToLower(link)] = link
		m[strings.ToLower(zone)] = zone
	}
	intl := js.Global().Get("Intl")
	if intl.Type() == js.TypeObject && intl.Get("supportedValuesOf").Type() == js.TypeFunction {
		list := intl.Call("supportedValuesOf", "timeZone")
		for i := range list.Length() {
			id := list.Index(i).String()
			m[strings.ToLower(id)] = id
		}
	}
	return m
})

// localZoneName returns the IANA time zone identifier of time.Local, which is not the
// zone of the host on GOOS=js.
func localZoneName() string {
	return "UTC"
}

func platformZoneName(tz TimeZone) string {
	switch z := tz.(type) {
	case intlZone:
		return z.loc.String()
	case jsZone:
		return hostZoneID("")
	}
	return ""
}
