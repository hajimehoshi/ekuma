// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// RealmPart is the storage state of a realm, which the realm embeds.
type RealmPart struct {
	Intrinsics [intrinsicCount]*Object

	Shapes shapeTable

	ArrayIter arrayIterCache

	regExpOriginals regExpOriginals

	Global *Object

	// GlobalThis is the global this value, which a host can set to a global proxy.
	GlobalThis *Object

	// GlobalLex holds the top-level let, const and class declarations.
	GlobalLex *Object

	// ObjectTagStrings are results of Object.prototype.toString, by tag.
	ObjectTagStrings map[wtf8.String]Value

	// StrictCallee is the callee property of unmapped arguments objects, or the zero value
	// until it is needed.
	StrictCallee Value

	// RegExpFlagStrings holds the value of the flags getter for each set of the 8 flags, once
	// computed. It is nil until it is needed.
	RegExpFlagStrings *[1 << 8]Value

	// AsyncFromSyncProto is %AsyncFromSyncIteratorPrototype%, or nil until it is needed.
	AsyncFromSyncProto *Object

	LegacyRegExp legacyRegExpStatics

	// space is the space of the agent of the realm.
	*Space

	// Owner is the execution context of the realm. Only executionContextOf and the
	// interpreter loop read it.
	Owner any
}

func (o *Object) CreationRealm() *RealmPart {
	if o.Class == ClassProxy {
		st := o.ExtState().(*ProxyState)
		if st.IsGlobal() {
			if st.Target == nil {
				return nil
			}
			o = st.Target
		}
	}
	return o.shape.Realm
}
