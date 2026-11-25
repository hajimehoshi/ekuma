// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// FieldDecoration is what the decorators of a field or an auto-accessor add to it.
type FieldDecoration struct {
	// Initializers are the Initializers that the decorators returned, in the order they were applied.
	Initializers []*Object
	// Extra are the initializers that the decorators added with addInitializer.
	Extra []*Object
}
