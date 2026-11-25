// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build ekumavalidate

package storage

import (
	"math"
)

// ValidateExec enables the interpreter's frame and bytecode checks. The registers must belong
// to the running frame. Each instruction must write only the registers in [bytecode.Opcode.Operands]
// and continue where [bytecode.Opcode.Flow] or a jump target allows. A cache that answers for a
// property lookup must answer as the lookup does (see validate_cache_on.go). Every request for room
// on the stack moves the stack, so that registers that are not reloaded after one are stale.
const ValidateExec = true

// StalePoison fills the stack storage that the interpreter has moved away from. It has no shape,
// so that most uses as an object fail at once.
var StalePoison = Value{
	N: math.Inf(-1),
	O: &Object{
		Kind: KindObject,
	},
}

// IsStale reports whether v was read from storage that the stack has moved away from.
func IsStale(v Value) bool {
	return v.O == StalePoison.O
}

// CheckValue checks that v was not read from stale registers.
func CheckValue(v Value) {
	if IsStale(v) {
		panic("ekumavalidate: a value was read from stale registers")
	}
}

// checkArrayProtoPlain checks that the prototypes of plain arrays have no index property and run no
// code.
func (r *RealmPart) checkArrayProtoPlain() {
	a := r.Intrinsic(IntrinsicArrayPrototype)
	if a.Kind != KindArray || a.Flags&ObjExotic != 0 || len(a.Elems) != 0 || !a.InheritsNoIndex() {
		panic("ekumavalidate: the prototypes of plain arrays are not plain")
	}
}
