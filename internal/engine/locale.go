// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// DefaultLocale returns the configured default locale, or empty for the host default.
func (ag *agent) DefaultLocale() string {
	return ag.defaultLocale
}

// SetDefaultLocale sets the default locale to a well-formed language tag or empty.
func (ag *agent) SetDefaultLocale(tag string) {
	ag.defaultLocale = tag
}
