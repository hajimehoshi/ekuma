// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"
	"slices"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// execQuiet continues an eligible frame after a non-reentering operation succeeds.
// It returns the PC of the first instruction it leaves to regular dispatch.
func (ag *agent) execQuiet(cx *ExecutionContext, fp *storage.FuncProto, pc int, regs []storage.Value) int {
	for {
		if validateExec {
			ag.checkStep(fp, pc, slices.Clip(regs))
		}
		i := &fp.Code[pc]
		pc++
		switch i.Op {
		case bytecode.OpLoadK:
			regs[i.A] = fp.Consts[i.B]
		case bytecode.OpLoadInt:
			storage.SetNumber(&regs[i.A], float64(i.B))
		case bytecode.OpLoadUndef:
			regs[i.A] = storage.Undefined
		case bytecode.OpLoadNull:
			regs[i.A] = storage.Null
		case bytecode.OpLoadTrue:
			regs[i.A] = storage.True
		case bytecode.OpLoadFalse:
			regs[i.A] = storage.False
		case bytecode.OpMove:
			regs[i.A] = regs[i.B]
		case bytecode.OpSeal:
			regs[i.A].Seal()
		case bytecode.OpNewObject:
			regs[i.A] = storage.Value{O: cx.NewEmptyObjectWithRoom(int(i.C))}
		case bytecode.OpGetGlobal, bytecode.OpGetGlobalStrict, bytecode.OpGetGlobalOrUndef, bytecode.OpGetGlobalOrUndefStrict:
			c := &fp.Globals[i.C]
			if c.Holder == nil || c.Holder.Shape() != c.Shape {
				goto retry
			}
			if validateExec {
				cx.checkGlobalHit(fp, pc-1, fp.Keys[i.B], c, false)
			}
			regs[i.A] = *c.Ref
		case bytecode.OpGetProp, bytecode.OpGetPropChain, bytecode.OpGetMethod, bytecode.OpGetLength:
			v := regs[i.B]
			o := v.O
			if o == nil {
				goto retry
			}
			if i.Op == bytecode.OpGetLength {
				var n float64
				switch o.Kind {
				case storage.KindArray:
					n = float64(o.Length())
				case storage.KindString:
					n = float64(v.StringLength())
				default:
					goto cachedRead
				}
				if validateExec {
					cx.checkLengthHit(fp, pc-1, v, n)
				}
				storage.SetNumber(&regs[i.A], n)
				continue
			}
		cachedRead:
			if o == storage.NumberObject {
				o = cx.realm.Intrinsics[storage.IntrinsicNumberPrototype]
			}
			e := fp.Caches[i.C].Entry(o.Shape())
			if e == nil {
				goto retry
			}
			var value storage.Value
			switch {
			case e.Kind == storage.CacheOwn:
				value = e.OwnValue(o)
			case e.Kind == storage.CacheProto && e.Epoch == cx.ProtoEpoch:
				value = *e.Ref
			default:
				goto retry
			}
			if validateExec {
				key := cx.Key(storage.KeyLength)
				if i.Op != bytecode.OpGetLength {
					key = fp.Keys[i.C]
				}
				cx.checkEntryRead(fp, pc-1, v, o, key, e)
			}
			regs[i.A] = value
			if i.Op == bytecode.OpGetMethod {
				regs[i.A+1] = v
				n := &fp.Code[pc]
				if n.Op == bytecode.OpCall && n.B == i.A {
					if result, ok := cx.quietCall(value.O, v, regs, int(n.B)+2, int(n.C), pc+1); ok {
						regs[n.A] = result
						pc++
						continue
					}
					// The method read is complete. The call is the next unchecked instruction.
					return pc
				}
			}
		case bytecode.OpGetIndex, bytecode.OpGetIndexInt:
			j := int64(i.C)
			if i.Op == bytecode.OpGetIndex {
				k := regs[i.C]
				if k.O != storage.NumberObject || !numconv.InInt64Range(k.N) || float64(int64(k.N)) != k.N {
					goto retry
				}
				j = int64(k.N)
			}
			v := regs[i.B]
			if v.IsString() {
				if j < 0 || j >= int64(v.StringLength()) {
					goto retry
				}
				regs[i.A] = cx.UnitString(v.UnitAt(int(j)))
				continue
			}
			if !v.IsObject() {
				goto retry
			}
			if result, ok := storage.Element(v.O, j); ok {
				regs[i.A] = result
				continue
			}
			goto retry
		case bytecode.OpSetIndex, bytecode.OpSetIndexStrict, bytecode.OpSetIndexInt, bytecode.OpSetIndexIntStrict:
			j := int64(i.B)
			if i.Op == bytecode.OpSetIndex || i.Op == bytecode.OpSetIndexStrict {
				k := regs[i.B]
				if k.O != storage.NumberObject || !numconv.InInt64Range(k.N) || float64(int64(k.N)) != k.N {
					goto retry
				}
				j = int64(k.N)
			}
			o := regs[i.A].O
			if o == nil || o.Kind != storage.KindArray {
				goto retry
			}
			v := regs[i.C]
			// Changing the element storage can poll while copying existing elements.
			ag.frames[len(ag.frames)-1].pc = pc
			if o.SetElement(j, v) {
				continue
			}
			if j == int64(o.Length()) {
				if _, ok := cx.AppendElement(o, v); ok {
					continue
				}
			}
			goto retry
		case bytecode.OpAdd:
			x, y := regs[i.B], regs[i.C]
			if x.O == storage.NumberObject && y.O == storage.NumberObject {
				storage.SetNumber(&regs[i.A], x.N+y.N)
				continue
			}
			if x.IsBigInt() && y.IsBigInt() {
				ag.frames[len(ag.frames)-1].pc = pc
				regs[i.A] = cx.addSubBigInts(x, y, false, i.Accumulate)
				continue
			}
			if result, ok := cx.concatPrimitive(x, y, pc); ok {
				regs[i.A] = result
				continue
			}
			goto retry
		case bytecode.OpPrependK:
			x := regs[i.A]
			if i.C != 0 || !x.IsString() || x.O.Flags()&storage.ObjExclusive == 0 {
				goto retry
			}
			ag.frames[len(ag.frames)-1].pc = pc
			if !cx.prependInPlace(x, fp.Consts[i.B]) {
				goto retry
			}
		case bytecode.OpConcatK:
			x, suffix := regs[i.B], fp.Consts[i.C]
			if x.IsString() {
				ag.frames[len(ag.frames)-1].pc = pc
				regs[i.A] = cx.Concat(x, suffix)
				continue
			}
			if x.IsNumber() && suffix.StringByteLength() == 0 {
				regs[i.A] = cx.NumberToString(x.N)
				continue
			}
			goto retry
		case bytecode.OpMulAdd:
			if regs[i.B].IsBigInt() {
				if !cx.tryMulAddBigInts(regs, fp.Code, pc-1) {
					goto retry
				}
				pc++
				continue
			}
			fallthrough
		case bytecode.OpAddK, bytecode.OpSubK, bytecode.OpMulK, bytecode.OpDivK, bytecode.OpModK,
			bytecode.OpSub, bytecode.OpMul, bytecode.OpDiv, bytecode.OpMod:
			x := regs[i.B]
			if x.O != storage.NumberObject {
				goto retry
			}
			var y float64
			switch i.Op {
			case bytecode.OpAddK, bytecode.OpSubK, bytecode.OpMulK, bytecode.OpDivK, bytecode.OpModK:
				y = fp.Consts[i.C].N
			default:
				v := regs[i.C]
				if v.O != storage.NumberObject {
					goto retry
				}
				y = v.N
			}
			var result float64
			switch i.Op {
			case bytecode.OpAddK:
				result = x.N + y
			case bytecode.OpSub, bytecode.OpSubK:
				result = x.N - y
			case bytecode.OpMul, bytecode.OpMulAdd, bytecode.OpMulK:
				result = float64(x.N * y)
			case bytecode.OpDiv, bytecode.OpDivK:
				result = x.N / y
			default:
				if a, b := int64(x.N), int64(y); a > 0 && b > 0 && float64(a) == x.N && float64(b) == y {
					result = float64(a % b)
				} else {
					result = math.Mod(x.N, y)
				}
			}
			storage.SetNumber(&regs[i.A], result)
		case bytecode.OpBitAnd, bytecode.OpBitOr, bytecode.OpBitXor, bytecode.OpShl, bytecode.OpShr, bytecode.OpUShr,
			bytecode.OpBitAndK, bytecode.OpBitOrK, bytecode.OpShlK, bytecode.OpShrK, bytecode.OpUShrK:
			x := regs[i.B]
			if x.O != storage.NumberObject || !numconv.InInt64Range(x.N) {
				goto retry
			}
			var y float64
			switch i.Op {
			case bytecode.OpBitAndK, bytecode.OpBitOrK, bytecode.OpShlK, bytecode.OpShrK, bytecode.OpUShrK:
				y = fp.Consts[i.C].N
			default:
				v := regs[i.C]
				if v.O != storage.NumberObject || !numconv.InInt64Range(v.N) {
					goto retry
				}
				y = v.N
			}
			a, b := int32(int64(x.N)), int32(int64(y))
			var result float64
			switch i.Op {
			case bytecode.OpBitAnd, bytecode.OpBitAndK:
				result = float64(a & b)
			case bytecode.OpBitOr, bytecode.OpBitOrK:
				result = float64(a | b)
			case bytecode.OpBitXor:
				result = float64(a ^ b)
			case bytecode.OpShl, bytecode.OpShlK:
				result = float64(a << (uint32(b) & 31))
			case bytecode.OpShr, bytecode.OpShrK:
				result = float64(a >> (uint32(b) & 31))
			case bytecode.OpUShr, bytecode.OpUShrK:
				result = float64(uint32(a) >> (uint32(b) & 31))
			}
			storage.SetNumber(&regs[i.A], result)
		case bytecode.OpInc, bytecode.OpDec, bytecode.OpPostInc, bytecode.OpPostDec:
			x := regs[i.B]
			if x.O != storage.NumberObject {
				goto retry
			}
			delta := 1.0
			if i.Op == bytecode.OpDec || i.Op == bytecode.OpPostDec {
				delta = -1
			}
			if i.Op == bytecode.OpPostInc || i.Op == bytecode.OpPostDec {
				storage.SetNumber(&regs[i.A], x.N)
				storage.SetNumber(&regs[i.B], x.N+delta)
			} else {
				storage.SetNumber(&regs[i.A], x.N+delta)
			}
			if i.FuseNext {
				n := &fp.Code[pc]
				if bytecode.OpLoopLt <= n.Op && n.Op <= bytecode.OpLoopGeK {
					if validateExec {
						ag.checkStep(fp, pc, slices.Clip(regs))
					}
					i = n
					pc++
					goto loopCompare
				}
			}
		case bytecode.OpJump:
			if int(i.A) < pc && ag.InterruptFlag.Load() {
				goto retry
			}
			pc = int(i.A)
		case bytecode.OpJumpIf, bytecode.OpJumpIfNot, bytecode.OpLoopIf:
			if storage.ToBoolean(regs[i.B]) == (i.Op != bytecode.OpJumpIfNot) {
				if i.Op == bytecode.OpLoopIf && ag.InterruptFlag.Load() {
					goto retry
				}
				pc = int(i.A)
			}
		case bytecode.OpLoopLt, bytecode.OpLoopLe, bytecode.OpLoopGt, bytecode.OpLoopGe,
			bytecode.OpLoopLtK, bytecode.OpLoopLeK, bytecode.OpLoopGtK, bytecode.OpLoopGeK:
			goto loopCompare
		case bytecode.OpCall, bytecode.OpCallUndef:
			b := int(i.B)
			this := regs[b+1]
			if i.Op == bytecode.OpCallUndef {
				this = storage.Undefined
			}
			result, ok := cx.quietCall(regs[b].O, this, regs, b+2, int(i.C), pc)
			if !ok {
				goto retry
			}
			if i.Op == bytecode.OpCallUndef {
				regs[b+1] = this
			}
			regs[i.A] = result
		case bytecode.OpRegExpArg:
			site := &fp.RegExps[i.B]
			if site.State == nil {
				goto retry
			}
			cx.lendRegExpLiteral(slices.Clip(regs), i.A, i.C, site)
		case bytecode.OpRegExpMethod:
			site := &fp.RegExps[i.B]
			if site.State == nil {
				goto retry
			}
			m, o, ok := cx.regExpLiteralMethodFast(site, fp.Keys[i.C], &fp.Caches[i.C])
			if !ok {
				goto retry
			}
			regs[i.A], regs[i.A+1] = m, storage.Value{O: o}
		default:
			goto retry
		}
		continue
	loopCompare:
		{
			x := regs[i.B]
			if x.O != storage.NumberObject {
				goto retry
			}
			var y float64
			if i.Op >= bytecode.OpLoopLtK {
				y = fp.Consts[i.C].N
			} else {
				v := regs[i.C]
				if v.O != storage.NumberObject {
					goto retry
				}
				y = v.N
			}
			var taken bool
			switch i.Op {
			case bytecode.OpLoopLt, bytecode.OpLoopLtK:
				taken = x.N < y
			case bytecode.OpLoopLe, bytecode.OpLoopLeK:
				taken = x.N <= y
			case bytecode.OpLoopGt, bytecode.OpLoopGtK:
				taken = x.N > y
			case bytecode.OpLoopGe, bytecode.OpLoopGeK:
				taken = x.N >= y
			}
			if taken {
				if ag.InterruptFlag.Load() {
					goto retry
				}
				pc = int(i.A)
			}
		}
		continue
	retry:
		if validateExec {
			ag.enterChecks()
		}
		return pc - 1
	}
}

// quietCall runs a fixed-arity native kernel on arguments already in place.
func (cx *ExecutionContext) quietCall(f *storage.Object, this storage.Value, args []storage.Value, first, argc, pc int) (storage.Value, bool) {
	if argc > 2 || f == nil || f.Fn == nil || cx.InterruptFlag.Load() {
		return storage.Value{}, false
	}
	info := f.Fn.Info
	if info == nil || !info.Native.HasFast() || info.Realm != cx.realmPart || info.Extremum != ExtremumNone ||
		f == cx.realm.Intrinsics[storage.IntrinsicArrayPrototypePush] || f == cx.realm.Intrinsics[storage.IntrinsicArrayPrototypePop] {
		return storage.Value{}, false
	}
	k := nativeFastOf(info.Native)
	if k.contract.Frame != FrameUnobservable || (k.contract.Pace != PaceShort && k.contract.Pace != PacePolled) {
		return storage.Value{}, false
	}
	// A polled kernel needs the continuation PC of the call itself.
	cx.frames[len(cx.frames)-1].pc = pc
	switch argc {
	case 0:
		if k.method0 != nil {
			return k.method0(cx.Quiet(), this)
		}
	case 1:
		x := args[first]
		if k.number1 != nil && x.O == storage.NumberObject {
			return storage.Number(k.number1(x.N)), true
		}
		if k.method1 != nil {
			return k.method1(cx.Quiet(), this, x)
		}
	case 2:
		x, y := args[first], args[first+1]
		if k.number2 != nil && x.O == storage.NumberObject && y.O == storage.NumberObject {
			return storage.Number(k.number2(x.N, y.N)), true
		}
		if k.method2 != nil {
			return k.method2(cx.Quiet(), this, x, y)
		}
	}
	return storage.Value{}, false
}

// concatPrimitive concatenates two strings or formats a number next to an empty string.
func (cx *ExecutionContext) concatPrimitive(x, y storage.Value, pc int) (storage.Value, bool) {
	if x.IsString() {
		if y.IsString() {
			// A concatenation that exceeds the string length limit throws at this instruction.
			cx.frames[len(cx.frames)-1].pc = pc
			return cx.Concat(x, y), true
		}
		if y.IsNumber() && x.StringByteLength() == 0 {
			return cx.NumberToString(y.N), true
		}
	}
	if x.IsNumber() && y.IsString() && y.StringByteLength() == 0 {
		return cx.NumberToString(x.N), true
	}
	return storage.Value{}, false
}
