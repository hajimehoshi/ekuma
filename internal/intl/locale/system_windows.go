// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale

import (
	"syscall"
	"unsafe"
)

var getUserDefaultLocaleName = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")

func systemLocale() string {
	var name [85]uint16 // LOCALE_NAME_MAX_LENGTH, including the terminating null.
	n, _, _ := getUserDefaultLocaleName.Call(uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(name[:])
}
