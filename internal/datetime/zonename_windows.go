// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime

import (
	"slices"
	"sync"
	"syscall"
)

//go:generate go run gen_windowszones.go

func init() {
	// The time package reads the time zone of Windows once, so time.Local keeps it while a
	// program runs. The identifier is read once to match it.
	windowsZoneName = sync.OnceValue(registryZoneName)
}

// registryZoneName returns the IANA time zone identifier of the time zone that the registry of
// Windows names, or an empty string when it is not known.
func registryZoneName() string {
	path, err := syscall.UTF16PtrFromString(`SYSTEM\CurrentControlSet\Control\TimeZoneInformation`)
	if err != nil {
		return ""
	}
	var key syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, path, 0, syscall.KEY_READ, &key); err != nil {
		return ""
	}
	defer syscall.RegCloseKey(key)
	value, err := syscall.UTF16PtrFromString("TimeZoneKeyName")
	if err != nil {
		return ""
	}
	// The value is a UTF-16 string in little-endian byte order.
	buf := make([]byte, 512)
	n := uint32(len(buf))
	var typ uint32
	if err := syscall.RegQueryValueEx(key, value, nil, &typ, &buf[0], &n); err != nil || typ != syscall.REG_SZ {
		return ""
	}
	units := make([]uint16, n/2)
	for i := range units {
		units[i] = uint16(buf[2*i]) | uint16(buf[2*i+1])<<8
	}
	zone, _ := ianaZoneOfWindows(syscall.UTF16ToString(units))
	return zone
}

// ianaZoneOfWindows returns the IANA time zone identifier of the time zone of Windows whose ID
// is id, and reports whether there is one.
func ianaZoneOfWindows(id string) (string, bool) {
	if i, ok := slices.BinarySearch(windowsZoneIDs, id); ok {
		return windowsIANAZones[i], true
	}
	return "", false
}
