// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale

import (
	"syscall/js"
)

// hostLocale returns the locale of the host environment, which may be empty.
func hostLocale() string {
	nav := js.Global().Get("navigator")
	if nav.Type() != js.TypeObject {
		return ""
	}
	l := nav.Get("language")
	if l.Type() != js.TypeString {
		return ""
	}
	return l.String()
}
