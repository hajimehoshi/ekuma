// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// DefaultLocale returns the configured default locale, or empty for the host default.
func (ag *agent) DefaultLocale() string {
	return ag.defaultLocale
}

// SetDefaultLocale sets the default locale to a well-formed language tag or empty.
func (ag *agent) SetDefaultLocale(tag string) {
	ag.defaultLocale = tag
	ag.availableDefaultLocale = ""
}

// AvailableDefaultLocale returns the available locale of the default locale, or empty when it
// is not known.
func (ag *agent) AvailableDefaultLocale() string {
	return ag.availableDefaultLocale
}

// SetAvailableDefaultLocale sets the available locale of the default locale, until
// [agent.SetDefaultLocale] changes that.
func (ag *agent) SetAvailableDefaultLocale(locale string) {
	ag.availableDefaultLocale = locale
}

// IntlFallbackSymbol returns the [[FallbackSymbol]] of %Intl% of the execution realm.
func (cx *ExecutionContext) IntlFallbackSymbol() *storage.Object {
	r := cx.realm
	if r.intlFallbackSymbol == nil {
		r.intlFallbackSymbol = cx.NewSymbol("IntlLegacyConstructedSymbol", true)
	}
	return r.intlFallbackSymbol
}
