// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !ekumavalidate

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

type execChecks struct{}

func (ag *agent) enterChecks() {}

func (ag *agent) leaveChecks() {}

func (ag *agent) checkStep(fp *funcProto, pc int, regs []Value) {}

func (ag *agent) checkActive(fp *funcProto) {}

func (ag *agent) moveStack() {}

func (ag *agent) abandonStack() {}

func (ag *agent) checkIdleStack() {}

func (ag *agent) holdStack() {}

func (ag *agent) holdArgs(args []Value) int {
	return 0
}

func (ag *agent) releaseArgs(n int) {}

func (c *execChecks) observeSteps(func(*funcProto, int)) {}

func (cx *ExecutionContext) checkReadHit(*funcProto, int, Value, *Object, *Value) {}

func (cx *ExecutionContext) checkEntryRead(*funcProto, int, Value, *Object, *Object, *cacheEntry) {}

func (cx *ExecutionContext) checkRememberedRead(Value, *Object, *Object, prop) {}

func (cx *ExecutionContext) checkGetterHit(*funcProto, int, Value, *Object, *Object) {}

func (cx *ExecutionContext) checkOwnGetterHit(*funcProto, int, Value, *Object, *Value) {}

func (cx *ExecutionContext) checkLengthHit(*funcProto, int, Value, float64) {}

func (cx *ExecutionContext) checkTypedArrayLengthHit(*funcProto, int, Value, float64) {}

func (cx *ExecutionContext) checkStoreHit(*funcProto, int, Value, *Object, *Value) {}

func (cx *ExecutionContext) checkAddHit(*funcProto, int, *Object, *Object, *shape) {}

func (cx *ExecutionContext) checkDefineHit(*funcProto, int, *Object, *Object, *shape) {}

func (cx *ExecutionContext) checkSetterHit(*funcProto, int, Value, *Object, *Object) {}

func (cx *ExecutionContext) checkGlobalExists(*funcProto, int, *Object, *globalCache) {}

func (cx *ExecutionContext) checkGlobalHit(*funcProto, int, *Object, *globalCache, bool) {}

func (cx *ExecutionContext) checkInstanceofHit(*funcProto, int, Value, *Object, *Value, bool) {}

func (cx *ExecutionContext) checkInstanceShape(*funcProto, int, *Object, *realmPart, *shape) {}

func (cx *ExecutionContext) checkReturnedProperty(*funcProto, Value, *cacheEntry) {}

func (cx *ExecutionContext) checkAppend(*Object) {}

const validateExec = storage.ValidateExec
