// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build (android && !cgo) || (!darwin && !js && !linux && !windows)

package locale

func systemLocale() string {
	// The host environment supplies the locale on these platforms.
	// hostLocale has already checked it.
	return ""
}
