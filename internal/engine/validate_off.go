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

func (ag *agent) checkStep(fp *storage.FuncProto, pc int, regs []storage.Value) {}

func (ag *agent) checkActive(fp *storage.FuncProto) {}

func (ag *agent) moveStack() {}

func (ag *agent) abandonStack() {}

func (ag *agent) checkIdleStack() {}

func (ag *agent) holdStack() {}

func (ag *agent) holdArgs(args []storage.Value) int {
	return 0
}

func (ag *agent) releaseArgs(n int) {}

func (c *execChecks) observeSteps(func(*storage.FuncProto, int)) {}

func (cx *ExecutionContext) checkReadHit(*storage.FuncProto, int, storage.Value, *storage.Object, *storage.Value) {
}

func (cx *ExecutionContext) checkEntryRead(*storage.FuncProto, int, storage.Value, *storage.Object, *storage.Object, *storage.CacheEntry) {
}

func (cx *ExecutionContext) checkRememberedRead(storage.Value, *storage.Object, *storage.Object, storage.Prop) {
}

func (cx *ExecutionContext) checkGetterHit(*storage.FuncProto, int, storage.Value, *storage.Object, *storage.Object) {
}

func (cx *ExecutionContext) checkOwnGetterHit(*storage.FuncProto, int, storage.Value, *storage.Object, *storage.Object, int32) {
}

func (cx *ExecutionContext) checkLengthHit(*storage.FuncProto, int, storage.Value, float64) {}

func (cx *ExecutionContext) checkTypedArrayLengthHit(*storage.FuncProto, int, storage.Value, float64) {
}

func (cx *ExecutionContext) checkStoreHit(*storage.FuncProto, int, storage.Value, *storage.Object, *storage.Object, int32) {
}

func (cx *ExecutionContext) checkAddHit(*storage.FuncProto, int, *storage.Object, *storage.Object, *storage.Shape) {
}

func (cx *ExecutionContext) checkDefineHit(*storage.FuncProto, int, *storage.Object, *storage.Object, *storage.Shape) {
}

func (cx *ExecutionContext) checkSetterHit(*storage.FuncProto, int, storage.Value, *storage.Object, *storage.Object) {
}

func (cx *ExecutionContext) checkGlobalExists(*storage.FuncProto, int, *storage.Object, *storage.GlobalCache) {
}

func (cx *ExecutionContext) checkGlobalHit(*storage.FuncProto, int, *storage.Object, *storage.GlobalCache, bool) {
}

func (cx *ExecutionContext) checkInstanceofHit(*storage.FuncProto, int, storage.Value, *storage.Object, int32, bool) {
}

func (cx *ExecutionContext) checkInstanceShape(*storage.FuncProto, int, *storage.Object, *storage.RealmPart, *storage.Shape) {
}

func (cx *ExecutionContext) checkReturnedProperty(*storage.FuncProto, storage.Value, *storage.CacheEntry) {
}

func (cx *ExecutionContext) checkAppend(*storage.Object) {}

func (cx *ExecutionContext) checkObjectAppend(*storage.Object) {}

const validateExec = storage.ValidateExec
