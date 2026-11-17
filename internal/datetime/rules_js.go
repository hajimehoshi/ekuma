// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime

import (
	"strings"
	"sync"
	"syscall/js"
	"time"
)

func (jsZone) OffsetSeconds(sec int64) int {
	t := max(-maxTime, min(maxTime, float64(sec)*msPerSecond))
	m := jsDate.New(t).Call("getTimezoneOffset").Float()
	return -int(m * 60)
}

func (z jsZone) Transition(sec int64, next bool) (int64, bool) {
	return searchTransition(z.OffsetSeconds, sec, next)
}

// intlZone is a time zone of the Intl.DateTimeFormat of the JavaScript host.
type intlZone struct {
	id     string
	format js.Value
}

func newIntlZone(id string) (intlZone, bool) {
	intl := js.Global().Get("Intl")
	if intl.Type() != js.TypeObject {
		return intlZone{}, false
	}
	opts := map[string]any{
		"timeZone":  id,
		"hourCycle": "h23",
		"era":       "short",
		"year":      "numeric",
		"month":     "numeric",
		"day":       "numeric",
		"hour":      "numeric",
		"minute":    "numeric",
		"second":    "numeric",
	}
	var f js.Value
	ok := func() (ok bool) {
		defer func() {
			if recover() != nil {
				ok = false
			}
		}()
		f = intl.Get("DateTimeFormat").New("en-US", opts)
		return true
	}()
	if !ok {
		return intlZone{}, false
	}
	return intlZone{
		id:     f.Call("resolvedOptions").Get("timeZone").String(),
		format: f,
	}, true
}

func (z intlZone) Offset(t float64) (float64, string) {
	sec := int64(t / msPerSecond)
	if t < 0 && float64(sec)*msPerSecond != t {
		sec--
	}
	return float64(z.OffsetSeconds(sec)/60) * msPerMinute, ""
}

func (z intlZone) OffsetSeconds(sec int64) int {
	t := max(-maxTime, min(maxTime, float64(sec)*msPerSecond))
	parts := z.format.Call("formatToParts", t)
	var year, month, day, hour, minute, second int
	bc := false
	for i := range parts.Length() {
		p := parts.Index(i)
		v := p.Get("value").String()
		n := 0
		for _, c := range v {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		switch p.Get("type").String() {
		case "era":
			bc = strings.HasPrefix(v, "B") || strings.HasPrefix(v, "b")
		case "year":
			year = n
		case "month":
			month = n
		case "day":
			day = n
		case "hour":
			hour = n % 24
		case "minute":
			minute = n
		case "second":
			second = n
		}
	}
	if bc {
		year = 1 - year
	}
	days := MakeDay(float64(year), float64(month-1), float64(day))
	local := int64(days)*86400 + int64(hour)*3600 + int64(minute)*60 + int64(second)
	return int(local - int64(t/msPerSecond))
}

func (z intlZone) Transition(sec int64, next bool) (int64, bool) {
	return searchTransition(z.OffsetSeconds, sec, next)
}

// Limits of the search for transitions by bisection.
const (
	// beforeFirstDST is 1847-01-01T00:00:00Z. There are no transitions to find before it.
	beforeFirstDST = -3944678400
	// searchWindow is the step of the search, shorter than the shortest period of an offset.
	searchWindow = 19 * 86400
	// searchAhead is how far past the present the search looks before it decides that there
	// are no more transitions.
	searchAhead = 3 * 366 * 86400
)

// searchTransition finds the transition after or before sec of a zone whose offsets offsetAt
// returns, by bisection.
func searchTransition(offsetAt func(int64) int, sec int64, next bool) (int64, bool) {
	now := time.Now().Unix()
	if next {
		left := max(sec, beforeFirstDST)
		leftOff := offsetAt(left)
		limit := max(left, now) + searchAhead
		for left < limit {
			right := left + searchWindow
			if right > maxSeconds {
				return 0, false
			}
			if rightOff := offsetAt(right); rightOff != leftOff {
				return bisect(offsetAt, left, right, leftOff), true
			}
			left = right
		}
		return 0, false
	}
	if sec > now+searchAhead {
		t, ok := searchTransition(offsetAt, now+searchAhead, false)
		if !ok || t < now {
			return t, ok
		}
	}
	right := sec - 1
	rightOff := offsetAt(right)
	for right > beforeFirstDST {
		left := right - searchWindow
		if leftOff := offsetAt(left); leftOff != rightOff {
			return bisect(offsetAt, left, right, leftOff), true
		}
		right = left
	}
	return 0, false
}

// bisect returns the first second in (left, right] whose offset differs from leftOff, the
// offset at left.
func bisect(offsetAt func(int64) int, left, right int64, leftOff int) int64 {
	for right-left > 1 {
		mid := left + (right-left)/2
		if offsetAt(mid) == leftOff {
			left = mid
		} else {
			right = mid
		}
	}
	return right
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
	z, ok := newIntlZone(name)
	if !ok {
		return loadedZone{}
	}
	// The host resolves a name to its own primary identifier, which can be another name, so the
	// identifier keeps the given name in the case of the database.
	id, ok := zoneIDByLowerCase()[strings.ToLower(name)]
	if !ok {
		id = name
		if strings.EqualFold(z.id, name) {
			id = z.id
		}
	}
	return loadedZone{
		id:      id,
		primary: primaryOfUTC(z.id),
		zone:    z,
		ok:      true,
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

// zoneName returns the identifier of a Zone that is not a locationZone.
func zoneName(z Zone) string {
	switch z := z.(type) {
	case intlZone:
		return z.id
	case jsZone:
		intl := js.Global().Get("Intl")
		if intl.Type() != js.TypeObject {
			return ""
		}
		return intl.Get("DateTimeFormat").New().Call("resolvedOptions").Get("timeZone").String()
	}
	return ""
}
