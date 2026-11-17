// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !android

package locale

import (
	"os"
)

func systemLocale() string {
	for _, path := range []string{"/etc/locale.conf", "/etc/default/locale"} {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if l := localeFromConfig(string(b)); l != "" {
			return l
		}
	}
	return ""
}
