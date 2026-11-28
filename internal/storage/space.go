// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"github.com/hajimehoshi/ekuma/internal/regexp"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Space is the state of an agent that its objects and values need: the property keys and the
// strings, the symbols, the shapes that belong to no realm, the epoch and the IDs that the inline
// caches rely on, and the objects that WeakRefs keep alive. The objects of an agent lead to its
// space through their shapes. It also holds what operations on them need besides: the state of
// polling, whether there is a host clock, and the RegExp matcher.
//
// It must not refer to the agent, a realm, a frame or an execution context, except that a shape
// records its realm. HostDefinedValue belongs to the host and is opaque to the engine.
type Space struct {
	// Poller comes first, as the interpreter loop reads its flag often.
	Poller

	Keys keys

	Strings stringTable

	// MissHolder holds undefined in its one slot, where the cache entries of properties that
	// were not found point.
	MissHolder *Object

	// ProtoEpoch changes whenever the properties or the prototype of a prototype object change,
	// or its slots move.
	ProtoEpoch uint32

	// ProtoElemsDirty is set if a prototype may have had a stored index or changed its own
	// prototype.
	ProtoElemsDirty bool

	lastReadShapeID uint32

	sparseIndices sparseIndexCache

	// IndexStrings holds the strings of the smallest array indices that were made, by index,
	// and nil for the others.
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

	// HasHostTimeZone is set if the host configured the local time zone.
	HasHostTimeZone bool

	// RegExpMatcher matches regular expressions. Nothing that a match runs starts another one.
	RegExpMatcher regexp.Matcher

	// HostDefinedValue is the value that [Space.HostDefined] returns.
	HostDefinedValue any
}

// HostDefined returns the host-defined value of the agent.
func (sp *Space) HostDefined() any {
	return sp.HostDefinedValue
}

// HostDefined returns the host-defined value of the agent that owns o, or nil for shared values.
func (o *Object) HostDefined() any {
	if o.shape == nil {
		return nil
	}
	return o.shape.Space.HostDefinedValue
}

// Owns reports whether o belongs to the agent of sp or is shared by all agents.
func (sp *Space) Owns(o *Object) bool {
	return o.shape == nil || o.shape.Space == sp
}

// InitMissHolder creates the object that MissHolder describes, with the shape s.
func (sp *Space) InitMissHolder(s *Shape) {
	sp.MissHolder = (&PropertyObject{
		Kind:  KindObject,
		shape: s,
		slots: []Value{Undefined},
	}).Init()
}
