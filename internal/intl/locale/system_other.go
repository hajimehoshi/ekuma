// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !darwin && !windows && !linux && !js

package locale

func systemLocale() string {
	return ""
}
