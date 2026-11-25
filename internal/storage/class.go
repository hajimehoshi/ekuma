// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// ClassElement is an element that the constructor of a class adds to every instance.
type ClassElement struct {
	Kind int
	Key  Value
	Val  *Object

	// Init is the initial value of a field without an initializer function.
	Init Value

	// Dec is what the decorators of a field add to it, or nil.
	Dec *FieldDecoration

	// Cache is the shape transition that defining a public field takes.
	Cache PropCache
}

// FieldDecoration is what the decorators of a field or an auto-accessor add to it.
type FieldDecoration struct {
	// Initializers are the initializers that the decorators returned, in the order they were applied.
	Initializers []*Object
	// Extra are the initializers that the decorators added with addInitializer.
	Extra []*Object
}
