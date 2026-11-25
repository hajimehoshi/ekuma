// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// HostDefined returns the value that NewExecutionContext was given.
func (sp *Space) HostDefined() any {
	return sp.HostDefinedValue
}
