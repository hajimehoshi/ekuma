// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"slices"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

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

// TraceShortcut records the distance to the next dispatched instruction of f after each
// execution of its single op site. bypass forces that site to take its generic path.
func TraceShortcut(t *testing.T, f Value, op bytecode.Opcode, bypass bool, distances *[]int) {
	t.Helper()
	fp := f.o.fn.code
	site := -1
	for pc, i := range fp.code {
		if i.Op == op {
			if site >= 0 {
				t.Fatalf("multiple %v sites in %s", op, fp.name)
			}
			site = pc
		}
	}
	if site < 0 {
		t.Fatalf("no %v site in %s", op, fp.name)
	}

	switch op {
	case bytecode.OpGetPropChain:
		if fp.code[site+1].Op != bytecode.OpGetProp {
			t.Fatal("expected a second named read after OpGetPropChain")
		}
	case bytecode.OpIterNextPair:
		if fp.code[site+1].Op != bytecode.OpExtra || fp.code[site+2].Op != bytecode.OpIterNext {
			t.Fatal("expected the extra word and ordinary iterator step after OpIterNextPair")
		}
		if bypass {
			// Give this VM its own copy and keep the offsets of handlers and jumps.
			fp.code = slices.Clone(fp.code)
			jump := bytecode.Instr{
				Op: bytecode.OpJump,
				A:  int32(site + 2),
			}
			fp.code[site], fp.code[site+1] = jump, jump
		}
	default:
		t.Fatalf("unsupported shortcut %v", op)
	}

	var stepping bool
	fp.context.checks.observeSteps(func(current *funcProto, pc int) {
		if current != fp {
			return
		}
		if stepping {
			*distances = append(*distances, pc-site)
		}
		stepping = pc == site
		if stepping && bypass && op == bytecode.OpGetPropChain {
			// A cold first read does the full named lookup. The second read keeps its cache.
			fp.caches[fp.code[site].C] = propCache{}
		}
	})
}

// CheckDispatchSequence checks successive instruction offsets in a frame of f.
func CheckDispatchSequence(f *bytecode.Func, pcs ...int) {
	fp := &funcProto{
		Func:  f,
		code:  f.Code,
		nregs: f.NumRegs,
	}
	ag := &agent{
		frames: []frame{{
			fp: fp,
		}},
		stack: make([]Value, fp.nregs+1),
	}
	for _, pc := range pcs {
		ag.checkStep(fp, pc, ag.stack)
	}
}
