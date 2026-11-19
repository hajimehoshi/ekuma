// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package temporal

import "github.com/hajimehoshi/ekuma/internal/datetime"

// NamedTimeZone returns the named time zone id with the rules of z.
func NamedTimeZone(id string, z datetime.Zone) TimeZone {
	return TimeZone{
		id:      id,
		primary: id,
		zone:    z,
	}
}
