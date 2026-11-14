// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package datetime

import (
	"time"
)

// Local returns the time zone of time.Local.
func Local() TimeZone {
	return LocationZone(time.Local)
}
