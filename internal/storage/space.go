// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Space is the state of an agent that its objects and values need: the property keys and the
// strings, the symbols, the shapes that belong to no realm, the epochs and IDs that the inline
// caches rely on, and the objects that WeakRefs keep alive. The objects of an agent lead to its
// space through their shapes. It also holds what operations on them need besides: the state of
// polling, the clock and the RegExp matcher.
//
// It must not refer to the agent, a realm, a frame or an execution context, except that a shape
// records its realm. hostDefined belongs to the host and is opaque to the engine.
type Space struct {
	// poller comes first, as the interpreter loop reads its flag often.
	Poller

	Keys keys

	Strings stringTable

	// MissHolder holds undefined in its one slot, where the cache entries of properties that
	// were not found point.
	MissHolder *Object

	// ProtoEpoch changes whenever the properties or the prototype of a prototype object change,
	// or its slots move.
	ProtoEpoch uint32

	lastReadShapeID uint32

	sparseIndices sparseIndexCache

	// IndexStrings holds the strings of the array indices below indexStringCount that were
	// made, by index, and nil for the others.
	IndexStrings []*Object

	// SymbolShape is the shape of the symbols of the agent. It records the space that they belong
	// to, and no lookup starts from it.
	SymbolShape *Shape

	// Typeofs are the interned results of the typeof operator, indexed by the typeof constants.
	Typeofs [TypeofCount]Value

	// matchResultShapes are the shapes of RegExp match results of the realm that made them last.
	matchResultShapes matchResultShapes

	Builtins builtinLayout

	// MaxBigIntResultBits is the bit limit for checked BigInt arithmetic results.
	// Tests lower it to exercise overflow with small operands.
	MaxBigIntResultBits int64

	// SymbolRegistry is the GlobalSymbolRegistry List, by key.
	SymbolRegistry map[wtf8.String]*Object

	// keptObjects is the [[KeptAlive]] list.
	keptObjects keptObjects

	// HasHostClock is set if a host function gives the current time.
	HasHostClock bool

	// RegExpMatcher matches regular expressions. Nothing that a match runs starts another one.
	RegExpMatcher regexp.Matcher

	// HostDefinedValue is the value that [Space.HostDefined] returns.
	HostDefinedValue any
}

// HostDefined returns the value that NewExecutionContext was given.
func (sp *Space) HostDefined() any {
	return sp.HostDefinedValue
}

// HostDefined returns the host-defined value of the agent that owns o, or nil for shared values.
func (o *Object) HostDefined() any {
	if o.Shape == nil {
		return nil
	}
	return o.Shape.Space.HostDefinedValue
}

// Owns reports whether o belongs to the agent of sp or is shared by all agents.
func (sp *Space) Owns(o *Object) bool {
	return o.Shape == nil || o.Shape.Space == sp
}
