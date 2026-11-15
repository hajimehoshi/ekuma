// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package locale

import (
	"os"
)

// hostLocale returns the locale that the POSIX environment variables name for messages, which
// may be empty.
func hostLocale() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
