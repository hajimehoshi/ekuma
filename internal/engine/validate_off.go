// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !ekumavalidate

package engine

// validateExec says whether the interpreter checks the code that it runs, as validate_on.go
// describes.
const validateExec = false

type execChecks struct{}

func (ag *agent) enterChecks() {}

func (ag *agent) leaveChecks() {}

func (ag *agent) checkStep(fp *funcProto, pc int, regs []Value) {}

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

func (cx *ExecutionContext) checkInstanceShape(*funcProto, int, *Object, *realm, *shape) {}

func (ag *agent) checkArrayIteration(*realm) {}

func (ag *agent) checkNoIteratorReturn(*realm) {}

func (cx *ExecutionContext) checkPlainArray(*Object) {}

func (cx *ExecutionContext) checkUnmodifiedRegExp(*Object) {}

func (cx *ExecutionContext) checkRegExpShape(*Object) {}

func (s *shape) checkEnumerable() {}

func (c *sparseIndexCache) checkSparseIndices(*shape) {}

func (c *funcProto) checkReturnedProperty(Value, *cacheEntry) {}
