// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// NewExecutionContextWithBigIntResultLimit limits checked BigInt results to maxBits bits.
func NewExecutionContextWithBigIntResultLimit(maxBits int64) *ExecutionContext {
	cx, _ := NewExecutionContext(nil)
	cx.maxBigIntResultBits = maxBits
	return cx
}

// InternedCount returns the number of entries in the table of interned strings of cx.
func InternedCount(cx *ExecutionContext) int {
	return len(cx.strings.interned.entries)
}

// ObjectTransitionCount returns the number of transitions from the empty shape of the objects
// whose prototype is %Object.prototype%.
func ObjectTransitionCount(cx *ExecutionContext) int {
	return cx.realm.shapes.object.transitions.len()
}

// ElementStorage returns the number of values that the storage of the elements of the object
// v has room for, including those that removing its first elements left unused.
func ElementStorage(v Value) int {
	return v.o.elemsStorage()
}

// StackSize returns the number of slots of the stack of cx.
func StackSize(cx *ExecutionContext) int {
	return len(cx.stack)
}

// JobStorage returns the number of jobs that the storage of the job queue of cx has room for.
func JobStorage(cx *ExecutionContext) int {
	return cap(cx.jobs.jobs)
}

// MaxNatives is the number of calls of native functions that can run at once.
const MaxNatives = maxNatives
