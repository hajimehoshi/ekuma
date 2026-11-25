// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Native identifies a registered native function. The zero value identifies none.
type Native struct {
	Index uint16

	// FastIndex is the index of what callers use instead of calling the function, or 0.
	FastIndex uint8
}

// HasFast reports whether fast returns something.
func (n Native) HasFast() bool {
	// Widening the index as nativeFastOf does lets the compiler share its register.
	return uint(n.FastIndex) != 0
}

// NativeInfo is the description of a native function.
type NativeInfo struct {
	// Native is the Native function.
	Native Native

	// Name and length are the initial values of the properties Name and length.
	Name   wtf8.String
	Length int32

	// Extremum says that the function is Math.min or Math.max, which the interpreter computes
	// for two numbers.
	Extremum Extremum

	// Frameless says that calls of the function show no frame, as agent.SetFrameless describes.
	Frameless bool

	// Prototype is the intrinsic of realm that the property Prototype of the function holds,
	// when FixedPrototype is set. The property can then be neither changed nor deleted.
	Prototype      Intrinsic
	FixedPrototype bool

	// Realm is the [[Realm]] of the function, or nil for a proxy.
	Realm *RealmPart
}

// ProxyInfo describes every callable proxy. It must not be modified.
var ProxyInfo NativeInfo

// Extremum identifies Math.min or Math.max for agent.SetExtremum.
type Extremum uint8
