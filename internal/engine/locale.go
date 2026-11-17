// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// DefaultLocale returns the configured default locale, or empty for the host default.
func (vm *VM) DefaultLocale() string {
	return vm.defaultLocale
}

// SetDefaultLocale sets the default locale to a well-formed language tag or empty.
func (vm *VM) SetDefaultLocale(tag string) {
	vm.defaultLocale = tag
}
