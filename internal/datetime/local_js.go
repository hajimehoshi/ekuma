// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime

import (
	"math"
	"syscall/js"
)

// Local returns the time zone of the JavaScript host.
func Local() TimeZone {
	return jsZone{}
}

// jsZone is the time zone of the Date objects of the JavaScript host.
type jsZone struct{}

var jsDate = js.Global().Get("Date")

func (jsZone) Offset(t float64) (float64, string) {
	t = math.Max(-maxTime, math.Min(maxTime, t))
	m := jsDate.New(t).Call("getTimezoneOffset").Float()
	// getTimezoneOffset is positive west of UTC, and may have a fraction for local mean time.
	return -math.Trunc(m)*msPerMinute + 0, ""
}
