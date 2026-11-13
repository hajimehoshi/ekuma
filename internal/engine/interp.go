// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/numconv"
)

// frame is the activation of a function written in JavaScript.
type frame struct {
	fp *funcProto
	cl *Object
	pc int

	// base is the stack index of register 0. For a generator, whose registers are not on the stack,
	// it is the stack index from which the frame may place the arguments of its calls.
	base int

	this Value

	// ret is the register of the calling frame that receives the result.
	ret int32

	// isNew is set when the function was called as a constructor that creates its own this.
	isNew bool

	argc int

	// extraBase is the stack index of the arguments beyond the parameters, which stay below
	// the registers when the function needs them, and -1 otherwise.
	extraBase int

	newTarget *Object

	// gen is the generator that the frame is an activation of.
	gen *Generator

	// scopes are the dynamic scopes entered by with statements, innermost last.
	scopes []*Object
}

const (
	// maxFrames is the call depth at which a RangeError is thrown.
	maxFrames = 10000

	// maxDepth is the number of nested interpreter loops at which a RangeError is thrown.
	maxDepth = 400
)

var tdzObject = &Object{Kind: kindHole}

// tdz is the content of a binding that is not initialized yet.
var tdz = Value{O: tdzObject}

type execStatus uint8

const (
	// execReturn: the frame the loop was started for returned.
	execReturn execStatus = iota

	// execThrow: an exception left the frame the loop was started for.
	execThrow

	// execPanic: Go code called by the loop threw. The frames are still in place.
	execPanic

	// execYield: the generator the loop was started for suspended.
	execYield
)

func (vm *VM) ensureStack(n int) {
	if n+64 <= len(vm.stack) {
		return
	}
	s := make([]Value, max(n+64, len(vm.stack)*2))
	copy(s, vm.stack)
	vm.stack = s
	// The open upvalues point into the old stack.
	for _, u := range vm.openUpvals {
		u.p = &s[u.index]
	}
}

// top returns the first stack index that is not in use.
func (vm *VM) top() int {
	return vm.sp
}

// pushFrame starts the activation of the closure f, whose argc arguments are already at base.
// It returns the stack index of register 0 of the new frame.
func (vm *VM) pushFrame(f *Object, this Value, base, argc int, ret int32, isNew bool, newTarget *Object) int {
	fp := f.Fn.Code
	extraBase := -1
	if argc > fp.nparams && fp.needsExtra {
		// The surplus arguments stay where they are, and the registers start above them.
		extraBase = base + fp.nparams
		newBase := base + argc
		vm.ensureStack(newBase + fp.nregs + 1)
		copy(vm.stack[newBase:newBase+fp.nparams], vm.stack[base:base+fp.nparams])
		base = newBase
	} else {
		vm.ensureStack(base + max(argc, fp.nregs+1))
	}
	stack := vm.stack
	for i := min(argc, fp.nparams); i < fp.nclear; i++ {
		stack[base+i] = Undefined
	}
	vm.frames = append(vm.frames, frame{
		fp:        fp,
		cl:        f,
		base:      base,
		this:      this,
		ret:       ret,
		isNew:     isNew,
		argc:      argc,
		extraBase: extraBase,
		newTarget: newTarget,
	})
	vm.sp = base + fp.nregs + 1
	return base
}

// extraArgs returns the arguments of the frame fr beyond its parameters.
// The result is only valid until the stack grows.
func (vm *VM) extraArgs(fr *frame) []Value {
	if fr.gen != nil {
		return fr.gen.extra
	}
	if fr.extraBase < 0 {
		return nil
	}
	return vm.stack[fr.extraBase : fr.extraBase+fr.argc-fr.fp.nparams]
}

// findUpval returns the upvalue for the stack slot index.
func (vm *VM) findUpval(index int) *upvalue {
	i := len(vm.openUpvals)
	for i > 0 && vm.openUpvals[i-1].index > index {
		i--
	}
	if i > 0 && vm.openUpvals[i-1].index == index {
		return vm.openUpvals[i-1]
	}
	u := &upvalue{
		p:     &vm.stack[index],
		index: index,
	}
	vm.openUpvals = append(vm.openUpvals, nil)
	copy(vm.openUpvals[i+1:], vm.openUpvals[i:])
	vm.openUpvals[i] = u
	return u
}

// closeUpvals detaches the upvalues that refer to the stack from index from on.
func (vm *VM) closeUpvals(from int) {
	n := len(vm.openUpvals)
	for n > 0 && vm.openUpvals[n-1].index >= from {
		u := vm.openUpvals[n-1]
		u.val = *u.p
		u.p = &u.val
		vm.openUpvals[n-1] = nil
		n--
	}
	vm.openUpvals = vm.openUpvals[:n]
}

// unwind finds the handler for the pending exception, dropping the frames that have none.
// It reports false when the frame at index stop was dropped without a handler.
func (vm *VM) unwind(stop int) bool {
	for {
		n := len(vm.frames) - 1
		fr := &vm.frames[n]
		pc := int32(fr.pc - 1)
		closing := vm.thrown.O == closeSignal
		for _, h := range fr.fp.Handlers {
			if pc < h.Start || pc >= h.End || (closing && !h.Finally) {
				continue
			}
			var regs []Value
			if fr.gen != nil {
				regs = fr.gen.regs
			} else {
				regs = vm.stack[fr.base:]
			}
			regs[h.Reg] = vm.thrown
			vm.thrown = Undefined
			fr.pc = int(h.Target)
			fr.scopes = fr.scopes[:min(len(fr.scopes), int(h.Scopes))]
			vm.sp = fr.top()
			return true
		}
		if fr.gen != nil {
			fr.gen.state = genCompleted
			fr.gen.regs = nil
		} else if len(vm.openUpvals) > 0 {
			vm.closeUpvals(fr.base)
		}
		*fr = frame{}
		vm.frames = vm.frames[:n]
		if n == stop {
			return false
		}
	}
}

// top returns the first stack index above the registers of the frame.
func (fr *frame) top() int {
	if fr.gen != nil {
		return fr.base
	}
	return fr.base + fr.fp.nregs + 1
}

// run executes the top frame until the frame at index stop returns, and returns its value.
// An exception that leaves that frame is thrown to the Go caller.
func (vm *VM) run(stop int) Value {
	if vm.depth >= maxDepth {
		vm.unwindTo(stop)
		vm.ThrowRangeError("Maximum call stack size exceeded")
	}
	vm.depth++
	for {
		v, st := vm.exec(stop)
		switch st {
		case execReturn, execYield:
			vm.depth--
			return v
		case execPanic:
			if vm.unwind(stop) {
				continue
			}
		}
		vm.depth--
		panic(errThrow)
	}
}

// arguments returns the arguments of the frame fr as a fresh slice.
func (vm *VM) frameArgs(fr *frame, regs []Value) []Value {
	n := min(fr.argc, fr.fp.nparams)
	args := make([]Value, 0, fr.argc)
	args = append(args, regs[:n]...)
	args = append(args, vm.extraArgs(fr)...)
	return args
}

// exec is the interpreter loop. See run.
func (vm *VM) exec(stop int) (result Value, status execStatus) {
	defer func() {
		if r := recover(); r != nil {
			if r != any(errThrow) {
				panic(r)
			}
			vm.sp = vm.frames[len(vm.frames)-1].top()
			status = execPanic
		}
	}()

	fr := &vm.frames[len(vm.frames)-1]
	fp := fr.fp
	cl := fr.cl
	code := fp.code
	base := fr.base
	pc := fr.pc
	var regs []Value
	onHeap := fr.gen != nil
	if onHeap {
		regs = fr.gen.regs
	} else {
		regs = vm.stack[base : base+fp.nregs+1]
	}
	// The state of a call in preparation, shared by every instruction that ends in a call.
	var (
		f         *Object
		callThis  Value
		argBase   int
		argc      int
		ret       int32
		isNew     bool
		newTarget *Object
	)

	for {
		i := &code[pc]
		pc++
		switch i.Op {
		case bytecode.OpLoadK:
			regs[i.A] = fp.consts[i.B]
		case bytecode.OpLoadInt:
			regs[i.A] = Value{N: float64(i.B)}
		case bytecode.OpLoadUndef:
			regs[i.A] = Undefined
		case bytecode.OpLoadNull:
			regs[i.A] = Null
		case bytecode.OpLoadTrue:
			regs[i.A] = True
		case bytecode.OpLoadFalse:
			regs[i.A] = False
		case bytecode.OpLoadTDZ:
			regs[i.A] = tdz
		case bytecode.OpMove:
			regs[i.A] = regs[i.B]
		case bytecode.OpCheckTDZ:
			if regs[i.A].O == tdzObject {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.throwTDZ(fp.keys[i.B])
			}
		case bytecode.OpThrowConstAssign:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.ThrowTypeError("Assignment to constant variable '" + fp.keys[i.A].Str + "'")
		case bytecode.OpThrowError:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.throwError(ErrorKind(i.A), fp.consts[i.B].O.Str)

		case bytecode.OpGetGlobal, bytecode.OpGetGlobalOrUndef:
			c := &fp.caches[i.C]
			if h := c.holder; h != nil && h.Shape == c.shape && c.epoch == vm.globalEpoch {
				if v := h.Slots[c.slot]; v.O != tdzObject {
					regs[i.A] = v
					continue
				}
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.getGlobal(fp.keys[i.B], c, i.Op == bytecode.OpGetGlobalOrUndef)
			regs[i.A] = v
		case bytecode.OpSetGlobal, bytecode.OpSetGlobalStrict:
			c := &fp.caches[i.C]
			if h := c.holder; h != nil && h.Shape == c.shape && c.epoch == vm.globalEpoch && c.kind == cacheOwn {
				if h.Slots[c.slot].O != tdzObject {
					h.Slots[c.slot] = regs[i.A]
					continue
				}
			}
			vm.frames[len(vm.frames)-1].pc = pc
			vm.setGlobal(fp.keys[i.B], regs[i.A], c, i.Op == bytecode.OpSetGlobalStrict)
		case bytecode.OpDeclareGlobalVar:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.declareGlobalVar(fp.keys[i.A], i.B != 0, regs[i.C])
		case bytecode.OpDeclareGlobalLex:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.declareGlobalLex(fp.keys[i.A], i.B != 0)
		case bytecode.OpInitGlobalLex:
			vm.initGlobalLex(fp.keys[i.B], regs[i.A])
		case bytecode.OpCheckGlobalDecl:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.checkGlobalDecl(fp.keys[i.A], int(i.B))

		case bytecode.OpGetUpval:
			regs[i.A] = *cl.Fn.upvals[i.B].p
		case bytecode.OpSetUpval:
			*cl.Fn.upvals[i.B].p = regs[i.A]
		case bytecode.OpCloseUpvals:
			if onHeap {
				vm.frames[len(vm.frames)-1].gen.closeUpvals(int(i.A))
			} else if len(vm.openUpvals) > 0 {
				vm.closeUpvals(base + int(i.A))
			}
		case bytecode.OpClosure:
			p := fp.protos[i.B]
			fn := &Function{
				Code:        p,
				Constructor: p.isConstructor,
			}
			fn.classConstructor = p.isClassConstructor
			fn.derived = p.isDerived
			o := &Object{
				Kind:  KindFunction,
				Class: ClassFunction,
				Flags: ObjExotic,
				Shape: vm.FunctionShape,
				Fn:    fn,
			}
			if p.isGenerator || p.isAsync {
				o.Shape = vm.ShapeFor(vm.functionProtoFor(p))
			}
			if n := len(p.Upvals); n > 0 {
				fn.upvals = make([]*upvalue, n)
				for j, d := range p.Upvals {
					switch {
					case !d.Local:
						fn.upvals[j] = cl.Fn.upvals[d.Index]
					case onHeap:
						fn.upvals[j] = vm.frames[len(vm.frames)-1].gen.findUpval(int(d.Index))
					default:
						fn.upvals[j] = vm.findUpval(base + int(d.Index))
					}
				}
			}
			regs[i.A] = Value{O: o}

		case bytecode.OpLoadThis:
			regs[i.A] = vm.frames[len(vm.frames)-1].this
		case bytecode.OpLoadThisSloppy:
			t := vm.frames[len(vm.frames)-1].this
			if t.O == nil || t.O.Kind < kindObject {
				if t.IsNullish() {
					t = Value{O: vm.Global}
				} else {
					t = Value{O: vm.ToObject(t)}
				}
			}
			regs[i.A] = t
		case bytecode.OpLoadCallee:
			regs[i.A] = Value{O: cl}
		case bytecode.OpLoadNewTarget:
			if nt := vm.frames[len(vm.frames)-1].newTarget; nt != nil {
				regs[i.A] = Value{O: nt}
			} else {
				regs[i.A] = Undefined
			}
		case bytecode.OpLoadHome:
			if h := cl.Fn.home; h != nil {
				regs[i.A] = Value{O: h}
			} else {
				regs[i.A] = Undefined
			}
		case bytecode.OpArguments:
			cur := &vm.frames[len(vm.frames)-1]
			regs[i.A] = Value{O: vm.newArguments(cur, regs, i.B != 0)}
		case bytecode.OpRestArgs:
			cur := &vm.frames[len(vm.frames)-1]
			var rest []Value
			if int(i.B) < cur.argc {
				all := vm.frameArgs(cur, regs)
				rest = all[i.B:]
			}
			regs[i.A] = Value{O: vm.NewArray(rest)}

		case bytecode.OpGetProp:
			v := regs[i.B]
			c := &fp.caches[i.C]
			if o := v.O; o != nil {
				e := &c.cacheEntry
				if o.Shape != e.shape {
					e = &c.second
				}
				if o.Shape == e.shape {
					if e.kind == cacheOwn {
						regs[i.A] = o.Slots[e.slot]
						continue
					}
					if e.epoch == vm.protoEpoch {
						if e.kind == cacheProto {
							regs[i.A] = e.holder.Slots[e.slot]
							continue
						}
						if e.kind == cacheGetter {
							f, callThis, argc, ret = e.holder, v, 0, i.A
							goto callAbove
						}
					}
				}
			}
			vm.frames[len(vm.frames)-1].pc = pc
			val, getter := vm.get(v, fp.keys[i.C], c)
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpSetProp, bytecode.OpSetPropStrict:
			v := regs[i.A]
			c := &fp.caches[i.B]
			var setter *Object
			if o := v.O; o != nil {
				e := &c.cacheEntry
				if o.Shape != e.shape {
					e = &c.second
				}
				if o.Shape == e.shape {
					switch e.kind {
					case cacheOwn:
						o.Slots[e.slot] = regs[i.C]
						continue
					case cacheAdd:
						if e.epoch == vm.protoEpoch && o.Flags&objIsProto == 0 {
							o.Shape = e.next
							o.Slots = append(o.Slots, regs[i.C])
							continue
						}
					case cacheSetter:
						if e.epoch == vm.protoEpoch {
							setter = e.holder
						}
					}
				}
			}
			if setter == nil {
				vm.frames[len(vm.frames)-1].pc = pc
				var ok bool
				setter, ok = vm.set(v, fp.keys[i.B], regs[i.C], c)
				if !ok && i.Op == bytecode.OpSetPropStrict {
					vm.throwReadOnly(v, PropKey{Key: fp.keys[i.B]})
				}
			}
			if setter != nil {
				val := regs[i.C]
				argBase = vm.frames[len(vm.frames)-1].top()
				vm.ensureStack(argBase + 1)
				vm.stack[argBase] = val
				f, callThis, argc, ret, isNew, newTarget = setter, v, 1, int32(fp.nregs), false, nil
				goto call
			}
		case bytecode.OpGetLength:
			v := regs[i.B]
			if o := v.O; o != nil && o.Kind == KindArray {
				regs[i.A] = Value{N: float64(o.Length)}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			val, getter := vm.get(v, vm.K.Length, &fp.caches[i.C])
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpGetIndex:
			v, k := regs[i.B], regs[i.C]
			if o := v.O; o != nil && k.O == nil {
				if j := int(k.N); float64(j) == k.N && uint(j) < uint(len(o.Elems)) {
					if e := o.Elems[j]; e.O != holeObject {
						regs[i.A] = e
						continue
					}
				}
			}
			vm.frames[len(vm.frames)-1].pc = pc
			val, getter := vm.getValue(v, k)
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpGetIndexInt:
			v := regs[i.B]
			if o := v.O; o != nil && uint(i.C) < uint(len(o.Elems)) {
				if e := o.Elems[i.C]; e.O != holeObject {
					regs[i.A] = e
					continue
				}
			}
			vm.frames[len(vm.frames)-1].pc = pc
			val, getter := vm.getIndex(v, uint32(i.C))
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpSetIndex, bytecode.OpSetIndexStrict:
			v, k := regs[i.A], regs[i.B]
			if o := v.O; o != nil && k.O == nil {
				if j := int(k.N); float64(j) == k.N && uint(j) < uint(len(o.Elems)) {
					if o.Elems[j].O != holeObject {
						o.Elems[j] = regs[i.C]
						continue
					}
				} else if j == len(o.Elems) && o.Kind == KindArray && uint32(j) == o.Length && o.Flags == 0 && float64(j) == k.N && o.Shape == vm.ArrayShape && vm.ArrayProtoPlain() {
					// Appending to a plain array.
					o.Elems = append(o.Elems, regs[i.C])
					o.Length++
					continue
				}
			}
			vm.frames[len(vm.frames)-1].pc = pc
			setter, ok := vm.setValue(v, k, regs[i.C])
			if !ok && i.Op == bytecode.OpSetIndexStrict {
				vm.throwReadOnly(v, vm.ToPropertyKey(k))
			}
			if setter != nil {
				val := regs[i.C]
				argBase = vm.frames[len(vm.frames)-1].top()
				vm.ensureStack(argBase + 1)
				vm.stack[argBase] = val
				f, callThis, argc, ret, isNew, newTarget = setter, v, 1, int32(fp.nregs), false, nil
				goto call
			}
		case bytecode.OpGetSuper:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			home := regs[code[pc].A]
			pc++
			v := vm.getSuper(home, regs[i.B], regs[i.C])
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpSetSuper:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			strict := code[pc].A != 0
			home := regs[code[pc].B]
			pc++
			vm.setSuper(home, regs[i.A], regs[i.B], regs[i.C], strict)
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpDeleteProp:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			strict := code[pc].A != 0
			pc++
			v := vm.deleteValue(regs[i.B], regs[i.C], strict)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpIn:
			vm.frames[len(vm.frames)-1].pc = pc
			c := regs[i.C]
			if !c.IsObject() {
				vm.ThrowTypeError("Cannot use 'in' operator to search for a key in a non-object")
			}
			r := vm.HasProperty(c.O, vm.ToPropertyKey(regs[i.B]))
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Bool(r)
		case bytecode.OpInstanceof:
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.instanceOf(regs[i.B], regs[i.C])
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Bool(r)
		case bytecode.OpHasPrivate:
			vm.frames[len(vm.frames)-1].pc = pc
			c := regs[i.C]
			if !c.IsObject() {
				vm.ThrowTypeError("Cannot use 'in' operator to search for a private field in a non-object")
			}
			_, ok := c.O.Shape.Lookup(regs[i.B].O)
			regs[i.A] = Bool(ok)
		case bytecode.OpGetPrivate:
			v := regs[i.B]
			vm.frames[len(vm.frames)-1].pc = pc
			val, getter := vm.getPrivate(v, regs[i.C].O)
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpSetPrivate:
			v := regs[i.A]
			vm.frames[len(vm.frames)-1].pc = pc
			if setter := vm.setPrivate(v, regs[i.B].O, regs[i.C]); setter != nil {
				val := regs[i.C]
				argBase = vm.frames[len(vm.frames)-1].top()
				vm.ensureStack(argBase + 1)
				vm.stack[argBase] = val
				f, callThis, argc, ret, isNew, newTarget = setter, v, 1, int32(fp.nregs), false, nil
				goto call
			}
		case bytecode.OpDefinePrivate:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.definePrivate(regs[i.A], regs[i.B].O, regs[i.C])
		case bytecode.OpToPropertyKey:
			v := regs[i.B]
			if v.O != nil && (v.O.Kind == KindString || v.O.Kind == kindSymbol) {
				regs[i.A] = v
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			k := vm.KeyValue(vm.ToPropertyKey(v))
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = k

		case bytecode.OpAdd:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Value{N: x.N + y.N}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			if x.O != nil && y.O != nil && x.O.Kind == KindString && y.O.Kind == KindString {
				regs[i.A] = vm.concat(x, x.O.Str, y, y.O.Str)
				continue
			}
			v := vm.addValues(x, y)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpSub:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Value{N: x.N - y.N}
				continue
			}
			goto arith
		case bytecode.OpMul:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Value{N: x.N * y.N}
				continue
			}
			goto arith
		case bytecode.OpDiv:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Value{N: x.N / y.N}
				continue
			}
			goto arith
		case bytecode.OpMod:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				xi, yi := int32(x.N), int32(y.N)
				if xi >= 0 && yi > 0 && float64(xi) == x.N && float64(yi) == y.N {
					regs[i.A] = Value{N: float64(xi % yi)}
					continue
				}
				regs[i.A] = Value{N: math.Mod(x.N, y.N)}
				continue
			}
			goto arith
		case bytecode.OpBitOr:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Value{N: float64(numconv.ToInt32(x.N) | numconv.ToInt32(y.N))}
				continue
			}
			goto arith
		case bytecode.OpBitAnd:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Value{N: float64(numconv.ToInt32(x.N) & numconv.ToInt32(y.N))}
				continue
			}
			goto arith
		case bytecode.OpBitXor:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Value{N: float64(numconv.ToInt32(x.N) ^ numconv.ToInt32(y.N))}
				continue
			}
			goto arith
		case bytecode.OpShl:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Value{N: float64(numconv.ToInt32(x.N) << (numconv.ToUint32(y.N) & 31))}
				continue
			}
			goto arith
		case bytecode.OpShr:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Value{N: float64(numconv.ToInt32(x.N) >> (numconv.ToUint32(y.N) & 31))}
				continue
			}
			goto arith
		case bytecode.OpUShr:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Value{N: float64(numconv.ToUint32(x.N) >> (numconv.ToUint32(y.N) & 31))}
				continue
			}
			goto arith
		case bytecode.OpExp:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Value{N: NumberPow(x.N, y.N)}
				continue
			}
			goto arith
		case bytecode.OpAddK:
			x := regs[i.B]
			if x.O == nil {
				regs[i.A] = Value{N: x.N + fp.consts[i.C].N}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.addValues(x, fp.consts[i.C])
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpSubK:
			x := regs[i.B]
			if x.O == nil {
				regs[i.A] = Value{N: x.N - fp.consts[i.C].N}
				continue
			}
			goto arithK
		case bytecode.OpMulK:
			x := regs[i.B]
			if x.O == nil {
				regs[i.A] = Value{N: x.N * fp.consts[i.C].N}
				continue
			}
			goto arithK
		case bytecode.OpDivK:
			x := regs[i.B]
			if x.O == nil {
				regs[i.A] = Value{N: x.N / fp.consts[i.C].N}
				continue
			}
			goto arithK
		case bytecode.OpModK:
			x, y := regs[i.B], fp.consts[i.C]
			if x.O == nil {
				xi, yi := int32(x.N), int32(y.N)
				if xi >= 0 && yi > 0 && float64(xi) == x.N && float64(yi) == y.N {
					regs[i.A] = Value{N: float64(xi % yi)}
					continue
				}
				regs[i.A] = Value{N: math.Mod(x.N, y.N)}
				continue
			}
			goto arithK
		case bytecode.OpBitOrK:
			x := regs[i.B]
			if x.O == nil {
				regs[i.A] = Value{N: float64(numconv.ToInt32(x.N) | int32(fp.consts[i.C].N))}
				continue
			}
			goto arithK
		case bytecode.OpBitAndK:
			x := regs[i.B]
			if x.O == nil {
				regs[i.A] = Value{N: float64(numconv.ToInt32(x.N) & int32(fp.consts[i.C].N))}
				continue
			}
			goto arithK
		case bytecode.OpShlK:
			x := regs[i.B]
			if x.O == nil {
				regs[i.A] = Value{N: float64(numconv.ToInt32(x.N) << (uint32(fp.consts[i.C].N) & 31))}
				continue
			}
			goto arithK
		case bytecode.OpShrK:
			x := regs[i.B]
			if x.O == nil {
				regs[i.A] = Value{N: float64(numconv.ToInt32(x.N) >> (uint32(fp.consts[i.C].N) & 31))}
				continue
			}
			goto arithK
		case bytecode.OpUShrK:
			x := regs[i.B]
			if x.O == nil {
				regs[i.A] = Value{N: float64(numconv.ToUint32(x.N) >> (uint32(fp.consts[i.C].N) & 31))}
				continue
			}
			goto arithK
		case bytecode.OpNeg:
			x := regs[i.B]
			if x.O == nil {
				regs[i.A] = Value{N: -x.N}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.negate(x)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpPlus:
			x := regs[i.B]
			if x.O == nil {
				regs[i.A] = x
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			n := vm.ToNumber(x)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Value{N: n}
		case bytecode.OpNot:
			regs[i.A] = Bool(!Truthy(regs[i.B]))
		case bytecode.OpBitNot:
			x := regs[i.B]
			if x.O == nil {
				regs[i.A] = Value{N: float64(^numconv.ToInt32(x.N))}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.bitNot(x)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpToNumeric:
			x := regs[i.B]
			if x.O == nil {
				regs[i.A] = x
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.ToNumeric(x)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpToString:
			x := regs[i.B]
			if x.O != nil && x.O.Kind == KindString {
				regs[i.A] = x
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.ToStringValue(x)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpTypeof:
			regs[i.A] = vm.typeofValue(regs[i.B])
		case bytecode.OpInc, bytecode.OpDec:
			x := regs[i.B]
			if x.O == nil {
				if i.Op == bytecode.OpInc {
					regs[i.A] = Value{N: x.N + 1}
				} else {
					regs[i.A] = Value{N: x.N - 1}
				}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			d := int64(1)
			if i.Op == bytecode.OpDec {
				d = -1
			}
			v := vm.step(x, d)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v

		case bytecode.OpLt:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Bool(x.N < y.N)
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.compare(x, y, true) == cmpLess
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Bool(r)
		case bytecode.OpLe:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Bool(x.N <= y.N)
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			c := vm.compare(y, x, false)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Bool(c == cmpGreater || c == cmpEqual)
		case bytecode.OpGt:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Bool(x.N > y.N)
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.compare(y, x, false) == cmpLess
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Bool(r)
		case bytecode.OpGe:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				regs[i.A] = Bool(x.N >= y.N)
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			c := vm.compare(x, y, true)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Bool(c == cmpGreater || c == cmpEqual)
		case bytecode.OpEq, bytecode.OpNe:
			x, y := regs[i.B], regs[i.C]
			var r bool
			if x.O == nil && y.O == nil {
				r = x.N == y.N
			} else {
				vm.frames[len(vm.frames)-1].pc = pc
				r = vm.looseEquals(x, y)
				regs = vm.reload(regs, onHeap, base, fp)
			}
			regs[i.A] = Bool(r == (i.Op == bytecode.OpEq))
		case bytecode.OpStrictEq:
			regs[i.A] = Bool(StrictEquals(regs[i.B], regs[i.C]))
		case bytecode.OpStrictNe:
			regs[i.A] = Bool(!StrictEquals(regs[i.B], regs[i.C]))

		case bytecode.OpJump:
			if int(i.A) < pc && vm.interrupt.Load() {
				// Every loop jumps backwards with this instruction.
				vm.frames[len(vm.frames)-1].pc = pc
				vm.Poll()
			}
			pc = int(i.A)
		case bytecode.OpJumpIf:
			if Truthy(regs[i.B]) {
				pc = int(i.A)
			}
		case bytecode.OpJumpIfNot:
			if !Truthy(regs[i.B]) {
				pc = int(i.A)
			}
		case bytecode.OpJumpIfNullish:
			if regs[i.B].IsNullish() {
				pc = int(i.A)
			}
		case bytecode.OpJumpIfNotNullish:
			if !regs[i.B].IsNullish() {
				pc = int(i.A)
			}
		case bytecode.OpJumpIfUndef:
			if regs[i.B].O == undefinedObject {
				pc = int(i.A)
			}
		case bytecode.OpJumpIfNotUndef:
			if regs[i.B].O != undefinedObject {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLt:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				if !(x.N < y.N) {
					pc = int(i.A)
				}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.compare(x, y, true) == cmpLess
			regs = vm.reload(regs, onHeap, base, fp)
			if !r {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLe:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				if !(x.N <= y.N) {
					pc = int(i.A)
				}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			c := vm.compare(y, x, false)
			regs = vm.reload(regs, onHeap, base, fp)
			if !(c == cmpGreater || c == cmpEqual) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGt:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				if !(x.N > y.N) {
					pc = int(i.A)
				}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.compare(y, x, false) == cmpLess
			regs = vm.reload(regs, onHeap, base, fp)
			if !r {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGe:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				if !(x.N >= y.N) {
					pc = int(i.A)
				}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			c := vm.compare(x, y, true)
			regs = vm.reload(regs, onHeap, base, fp)
			if !(c == cmpGreater || c == cmpEqual) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotEq, bytecode.OpJumpNotNe:
			x, y := regs[i.B], regs[i.C]
			var r bool
			if x.O == nil && y.O == nil {
				r = x.N == y.N
			} else {
				vm.frames[len(vm.frames)-1].pc = pc
				r = vm.looseEquals(x, y)
				regs = vm.reload(regs, onHeap, base, fp)
			}
			if r != (i.Op == bytecode.OpJumpNotEq) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotSEq:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				if x.N != y.N {
					pc = int(i.A)
				}
				continue
			}
			if !StrictEquals(x, y) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotSNe:
			x, y := regs[i.B], regs[i.C]
			if x.O == nil && y.O == nil {
				if x.N == y.N {
					pc = int(i.A)
				}
				continue
			}
			if StrictEquals(x, y) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLtK:
			x := regs[i.B]
			if x.O != nil {
				vm.frames[len(vm.frames)-1].pc = pc
				x = vm.ToNumeric(vm.toPrimitive(x, hintNumber))
				regs = vm.reload(regs, onHeap, base, fp)
				if x.O != nil {
					if compareBigIntMixed(x, fp.consts[i.C]) != cmpLess {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.N < fp.consts[i.C].N) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLeK:
			x := regs[i.B]
			if x.O != nil {
				vm.frames[len(vm.frames)-1].pc = pc
				x = vm.ToNumeric(vm.toPrimitive(x, hintNumber))
				regs = vm.reload(regs, onHeap, base, fp)
				if x.O != nil {
					if c := compareBigIntMixed(x, fp.consts[i.C]); c != cmpLess && c != cmpEqual {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.N <= fp.consts[i.C].N) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGtK:
			x := regs[i.B]
			if x.O != nil {
				vm.frames[len(vm.frames)-1].pc = pc
				x = vm.ToNumeric(vm.toPrimitive(x, hintNumber))
				regs = vm.reload(regs, onHeap, base, fp)
				if x.O != nil {
					if compareBigIntMixed(x, fp.consts[i.C]) != cmpGreater {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.N > fp.consts[i.C].N) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGeK:
			x := regs[i.B]
			if x.O != nil {
				vm.frames[len(vm.frames)-1].pc = pc
				x = vm.ToNumeric(vm.toPrimitive(x, hintNumber))
				regs = vm.reload(regs, onHeap, base, fp)
				if x.O != nil {
					if c := compareBigIntMixed(x, fp.consts[i.C]); c != cmpGreater && c != cmpEqual {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.N >= fp.consts[i.C].N) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotSEqK:
			if x := regs[i.B]; x.O != nil || x.N != fp.consts[i.C].N {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotSNeK:
			if x := regs[i.B]; x.O == nil && x.N == fp.consts[i.C].N {
				pc = int(i.A)
			}
		case bytecode.OpJumpIfInt:
			if x := regs[i.B]; x.O == nil && x.N == float64(i.C) {
				pc = int(i.A)
			}

		case bytecode.OpCall:
			a := int(i.A)
			f, callThis, argBase, argc, ret, isNew, newTarget = regs[a].O, regs[a+1], a+2, int(i.B), i.A, false, nil
			goto callRegs
		case bytecode.OpCallCall:
			a := int(i.A)
			if regs[a].O == vm.FnCall {
				f, callThis, argBase, argc, ret, isNew, newTarget = regs[a+1].O, regs[a+2], a+3, int(i.B)-1, i.A, false, nil
				if argc < 0 {
					callThis, argc = Undefined, 0
				}
			} else {
				f, callThis, argBase, argc, ret, isNew, newTarget = regs[a].O, regs[a+1], a+2, int(i.B), i.A, false, nil
			}
			goto callRegs
		case bytecode.OpApplyFwd:
			a := int(i.A)
			cur := &vm.frames[len(vm.frames)-1]
			if regs[a].O != vm.FnApply {
				// Something else is called apply here: pass it a real arguments object.
				regs[a+3] = Value{O: vm.newArguments(cur, regs, false)}
				f, callThis, argBase, argc, ret, isNew, newTarget = regs[a].O, regs[a+1], a+2, 2, i.A, false, nil
				goto callRegs
			}
			f, callThis, argc, ret, isNew, newTarget = regs[a+1].O, regs[a+2], cur.argc, i.A, false, nil
			argBase = cur.top()
			vm.ensureStack(argBase + argc)
			n := min(argc, fp.nparams)
			copy(vm.stack[argBase:], regs[:n])
			copy(vm.stack[argBase+n:], vm.extraArgs(&vm.frames[len(vm.frames)-1]))
			goto call
		case bytecode.OpCallFwd:
			a := int(i.A)
			cur := &vm.frames[len(vm.frames)-1]
			f, callThis, argc, ret, isNew, newTarget = regs[a].O, regs[a+1], cur.argc, i.A, false, nil
			argBase = cur.top()
			vm.ensureStack(argBase + argc)
			n := min(argc, fp.nparams)
			copy(vm.stack[argBase:], regs[:n])
			copy(vm.stack[argBase+n:], vm.extraArgs(&vm.frames[len(vm.frames)-1]))
			goto call
		case bytecode.OpCallSpread, bytecode.OpNewSpread, bytecode.OpSuperSpread:
			a := int(i.A)
			f, callThis, ret, isNew, newTarget = regs[a].O, regs[a+1], i.A, false, nil
			args := regs[a+2].O.Elems
			argc = len(args)
			argBase = vm.frames[len(vm.frames)-1].top()
			vm.ensureStack(argBase + argc)
			copy(vm.stack[argBase:], args)
			switch i.Op {
			case bytecode.OpNewSpread:
				goto construct
			case bytecode.OpSuperSpread:
				newTarget = regs[a+1].O
				goto superCall
			}
			goto call
		case bytecode.OpCallEval:
			a := int(i.A)
			if regs[a].O == vm.FnEval {
				vm.frames[len(vm.frames)-1].pc = pc
				var src Value = Undefined
				if i.B > 0 {
					src = regs[a+2]
				}
				v := vm.directEval(src, int(i.C), regs)
				regs = vm.reload(regs, onHeap, base, fp)
				regs[a] = v
				continue
			}
			f, callThis, argBase, argc, ret, isNew, newTarget = regs[a].O, regs[a+1], a+2, int(i.B), i.A, false, nil
			goto callRegs
		case bytecode.OpNew:
			a := int(i.A)
			f, argBase, argc, ret = regs[a].O, a+2, int(i.B), i.A
			if onHeap {
				top := vm.frames[len(vm.frames)-1].top()
				vm.ensureStack(top + argc)
				copy(vm.stack[top:], regs[argBase:argBase+argc])
				argBase = top
			} else {
				argBase += base
			}
			if f != nil && f.Fn != nil && f.Fn.Code != nil && f.Fn.Constructor && !f.Fn.derived {
				// The common case: a plain constructor function, whose prototype property is cached.
				c := &fp.caches[i.C]
				var proto Value
				if f.Shape == c.shape && c.kind == cacheOwn {
					proto = f.Slots[c.slot]
				} else {
					vm.frames[len(vm.frames)-1].pc = pc
					proto, _ = vm.get(Value{O: f}, vm.K.Prototype, c)
				}
				if proto.O == nil || proto.O.Kind < kindObject {
					proto = Value{O: vm.ObjectProto}
				}
				var shape *Shape
				if proto.O == c.holder && c.next != nil {
					shape = c.next
				} else {
					shape = vm.ShapeFor(proto.O)
					if c.kind == cacheOwn {
						c.holder, c.next = proto.O, shape
					}
				}
				callThis = Value{
					O: &Object{
						Kind:  kindObject,
						Shape: shape,
					},
				}
				isNew, newTarget = true, f
				goto call
			}
			goto construct
		case bytecode.OpSuperCall:
			a := int(i.A)
			argBase, argc, ret = a+2, int(i.B), i.A
			if onHeap {
				top := vm.frames[len(vm.frames)-1].top()
				vm.ensureStack(top + argc)
				copy(vm.stack[top:], regs[argBase:argBase+argc])
				argBase = top
			} else {
				argBase += base
			}
			f, newTarget = regs[a].O, regs[a+1].O
			goto superCall

		case bytecode.OpReturn, bytecode.OpReturnUndef, bytecode.OpReturnDerived:
			r := Undefined
			if i.Op != bytecode.OpReturnUndef {
				r = regs[i.A]
			}
			n := len(vm.frames) - 1
			done := &vm.frames[n]
			if i.Op == bytecode.OpReturnDerived {
				if !r.IsObject() {
					if !r.IsUndefined() {
						done.pc = pc
						vm.ThrowTypeError("Derived constructors may only return object or undefined")
					}
					r = regs[i.B]
					if r.O == tdzObject {
						done.pc = pc
						vm.throwReferenceError("Must call super constructor in derived class before accessing 'this' or returning from derived constructor")
					}
				}
			} else if done.isNew && (r.O == nil || r.O.Kind < kindObject) {
				r = done.this
			}
			if g := done.gen; g != nil {
				g.state = genCompleted
				g.regs = nil
			} else if len(vm.openUpvals) > 0 {
				vm.closeUpvals(base)
			}
			ret = done.ret
			vm.frames = vm.frames[:n]
			if n == stop {
				return r, execReturn
			}
			fr := &vm.frames[n-1]
			fp = fr.fp
			cl = fr.cl
			code = fp.code
			base = fr.base
			pc = fr.pc
			onHeap = fr.gen != nil
			if onHeap {
				regs = fr.gen.regs
				vm.sp = base
			} else {
				regs = vm.stack[base : base+fp.nregs+1]
				vm.sp = base + fp.nregs + 1
			}
			regs[ret] = r
		case bytecode.OpThrow:
			vm.thrown = regs[i.A]
			goto throw
		case bytecode.OpRethrow:
			vm.thrown = regs[i.A]
			goto throw

		case bytecode.OpInitialYield:
			n := len(vm.frames) - 1
			cur := &vm.frames[n]
			g := cur.gen
			g.pc = pc
			g.state = genSuspendedStart
			ret = cur.ret
			*cur = frame{}
			vm.frames = vm.frames[:n]
			if n == stop {
				return Value{O: g.object}, execReturn
			}
			fr := &vm.frames[n-1]
			fp = fr.fp
			cl = fr.cl
			code = fp.code
			base = fr.base
			pc = fr.pc
			onHeap = fr.gen != nil
			if onHeap {
				regs = fr.gen.regs
				vm.sp = base
			} else {
				regs = vm.stack[base : base+fp.nregs+1]
				vm.sp = base + fp.nregs + 1
			}
			regs[ret] = Value{O: g.object}
		case bytecode.OpYield, bytecode.OpAwait:
			n := len(vm.frames) - 1
			cur := &vm.frames[n]
			g := cur.gen
			v := regs[i.B]
			g.resumeReg = i.A
			g.pc = pc
			g.scopes = cur.scopes
			g.state = genSuspendedYield
			g.awaiting = i.Op == bytecode.OpAwait
			g.Delegating = i.C != 0
			*cur = frame{}
			vm.frames = vm.frames[:n]
			// A generator only ever runs as the frame its loop was started for.
			return v, execYield
		case bytecode.OpGenResumeMode:
			g := vm.frames[len(vm.frames)-1].gen
			regs[i.A] = Value{N: float64(g.mode)}
			g.mode = ResumeNext
		case bytecode.OpYieldDelegateResult:
			vm.frames[len(vm.frames)-1].pc = pc
			r := regs[i.B]
			if !r.IsObject() {
				vm.ThrowTypeError("Iterator result " + vm.DisplayString(r) + " is not an object")
			}
			done := Truthy(vm.GetV(r, vm.K.done))
			regs = vm.reload(regs, onHeap, base, fp)
			if !done {
				pc = int(i.C)
				continue
			}
			v := vm.GetV(r, vm.K.Value)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v

		case bytecode.OpNewObject:
			regs[i.A] = Value{
				O: &Object{
					Kind:  kindObject,
					Shape: vm.objectShape,
				},
			}
		case bytecode.OpNewArray:
			elems := make([]Value, i.B, max(i.B, i.C))
			copy(elems, regs[i.A+1:])
			regs[i.A] = Value{O: vm.NewArray(elems)}
		case bytecode.OpArrayPush:
			o := regs[i.A].O
			o.Elems = append(o.Elems, regs[i.B])
			o.Length = uint32(len(o.Elems))
		case bytecode.OpArrayHole:
			o := regs[i.A].O
			o.Elems = append(o.Elems, hole)
			o.Length = uint32(len(o.Elems))
		case bytecode.OpArraySpread:
			vm.frames[len(vm.frames)-1].pc = pc
			o := regs[i.A].O
			vm.appendIterable(o, regs[i.B])
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpRegExp:
			vm.frames[len(vm.frames)-1].pc = pc
			regs[i.A] = vm.newRegExpLiteral(fp.consts[i.B], fp.consts[i.C])
		case bytecode.OpTemplate:
			regs[i.A] = Value{O: vm.templateObject(&fp.templates[i.B])}
		case bytecode.OpDefineField:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.defineField(regs[i.A].O, regs[i.B], regs[i.C])
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpDefineFieldK:
			o := regs[i.A].O
			c := &fp.caches[i.B]
			if o.Shape == c.shape && c.kind == cacheAdd {
				o.Shape = c.next
				o.Slots = append(o.Slots, regs[i.C])
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			vm.defineFieldK(o, fp.keys[i.B], regs[i.C], c)
		case bytecode.OpDefineMethod:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.defineMethod(regs[i.A].O, regs[i.B], regs[i.C])
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpDefineGetter, bytecode.OpDefineSetter:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			enumerable := code[pc].A != 0
			pc++
			vm.defineAccessor(regs[i.A].O, regs[i.B], regs[i.C].O, i.Op == bytecode.OpDefineSetter, enumerable)
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpSetFuncName:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.setFuncName(regs[i.A], regs[i.B], int(i.C))
		case bytecode.OpSetHome:
			regs[i.A].O.Fn.home = regs[i.B].O
		case bytecode.OpCopyProps:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.copyDataProperties(regs[i.A].O, regs[i.B], nil)
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpCopyPropsExcept:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			n := int(code[pc].A)
			pc++
			o := vm.NewObject(vm.ObjectProto)
			excluded := append([]Value(nil), regs[int(i.C)+1:int(i.C)+1+n]...)
			vm.copyDataProperties(o, regs[i.B], excluded)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Value{O: o}
		case bytecode.OpSetProto:
			if p := regs[i.B]; p.IsObject() {
				vm.SetProto(regs[i.A].O, p.O)
			} else if p.IsNull() {
				vm.SetProto(regs[i.A].O, nil)
			}
		case bytecode.OpClass:
			vm.frames[len(vm.frames)-1].pc = pc
			parent := hole
			if i.C >= 0 {
				parent = regs[i.C]
			}
			proto := vm.setupClass(regs[i.A].O, parent)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A+1] = Value{O: proto}
		case bytecode.OpPrivateName:
			name := &Object{
				Kind:  kindSymbol,
				Class: ClassPrivateName,
				Str:   fp.keys[i.B].Str,
			}
			if i.C != 0 {
				name.Flags |= ObjFrozenLength
			}
			regs[i.A] = Value{O: name}
		case bytecode.OpInitFields:
			if regs[i.A].O.Fn.fields != nil {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.initFields(regs[i.A].O, regs[i.B])
				regs = vm.reload(regs, onHeap, base, fp)
			}
		case bytecode.OpAddField:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			kind := int(code[pc].A)
			pc++
			var init *Object
			if i.C >= 0 {
				init = regs[i.C].O
			}
			vm.addClassElement(regs[i.A].O, regs[i.B], init, kind)
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpCheckThisUninit:
			if regs[i.A].O != tdzObject {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.throwReferenceError("Super constructor may only be called once")
			}
		case bytecode.OpRequireCoercible:
			if regs[i.A].IsNullish() {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.ThrowTypeError("Cannot destructure '" + vm.DisplayString(regs[i.A]) + "' as it is " + vm.DisplayString(regs[i.A]))
			}

		case bytecode.OpForIn:
			vm.frames[len(vm.frames)-1].pc = pc
			it := vm.newForIn(regs[i.B])
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = it
		case bytecode.OpForInNext:
			vm.frames[len(vm.frames)-1].pc = pc
			k, ok := vm.forInNext(regs[i.B].O)
			regs = vm.reload(regs, onHeap, base, fp)
			if !ok {
				pc = int(i.C)
				continue
			}
			regs[i.A] = k
		case bytecode.OpGetIterator:
			v := regs[i.B]
			if i.C == 0 && v.O != nil && v.O.Kind == KindArray && vm.arrayIterationPlain(v.O) {
				// Iterate a plain array by index: the next method slot holds the index.
				regs[i.A] = v
				regs[i.A+1] = Value{N: 0}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			it, next := vm.getIterator(v, i.C != 0)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = it
			regs[i.A+1] = next
		case bytecode.OpIterNext:
			if regs[i.B].O == undefinedObject {
				// The iterator finished earlier.
				pc = int(i.C)
				continue
			}
			if idx := regs[i.B+1]; idx.O == nil {
				o := regs[i.B].O
				j := uint32(idx.N)
				if j >= o.Length {
					// Mark the iterator as finished, so that closing it does nothing.
					regs[i.B] = Undefined
					pc = int(i.C)
					continue
				}
				regs[i.B+1] = Value{N: idx.N + 1}
				if int(j) < len(o.Elems) && o.Elems[j].O != holeObject {
					regs[i.A] = o.Elems[j]
					continue
				}
				vm.frames[len(vm.frames)-1].pc = pc
				v := vm.GetKey(Value{O: o}, PropKey{Idx: j})
				regs = vm.reload(regs, onHeap, base, fp)
				regs[i.A] = v
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			{
				it, next := regs[i.B], regs[i.B+1]
				r := vm.CallValue(next, it)
				regs = vm.reload(regs, onHeap, base, fp)
				if !r.IsObject() {
					regs[i.B] = Undefined
					vm.ThrowTypeError("Iterator result " + vm.DisplayString(r) + " is not an object")
				}
				var done bool
				var v Value
				if ok := vm.protect(func() {
					done = Truthy(vm.GetV(r, vm.K.done))
					if !done {
						v = vm.GetV(r, vm.K.Value)
					}
				}); !ok {
					// An iterator whose result cannot be read counts as finished.
					regs = vm.reload(regs, onHeap, base, fp)
					regs[i.B] = Undefined
					goto throw
				}
				regs = vm.reload(regs, onHeap, base, fp)
				if done {
					regs[i.B] = Undefined
					pc = int(i.C)
					continue
				}
				regs[i.A] = v
			}
		case bytecode.OpIterClose:
			it := regs[i.A]
			if it.IsUndefined() || regs[i.A+1].O == nil {
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			vm.iteratorClose(it, i.B != 0)
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpIterResult:
			regs[i.A] = Value{O: vm.NewIterResult(regs[i.B], i.C != 0)}
		case bytecode.OpIterCall:
			vm.frames[len(vm.frames)-1].pc = pc
			{
				it := regs[i.B]
				m := vm.GetMethod(it, fp.keys[i.C])
				regs = vm.reload(regs, onHeap, base, fp)
				if m == nil {
					regs[i.A] = Undefined
					regs[i.A+1] = False
					continue
				}
				r := vm.Call(m, it, regs[i.A])
				regs = vm.reload(regs, onHeap, base, fp)
				regs[i.A] = r
				regs[i.A+1] = True
			}

		case bytecode.OpWith:
			vm.frames[len(vm.frames)-1].pc = pc
			o := vm.ToObject(regs[i.A])
			cur := &vm.frames[len(vm.frames)-1]
			cur.scopes = append(cur.scopes, o)
		case bytecode.OpPopScope:
			cur := &vm.frames[len(vm.frames)-1]
			cur.scopes = cur.scopes[:len(cur.scopes)-1]
		case bytecode.OpGetName, bytecode.OpTypeofName:
			vm.frames[len(vm.frames)-1].pc = pc
			v, found := vm.getName(fp.keys[i.B])
			regs = vm.reload(regs, onHeap, base, fp)
			if !found {
				// The name is not in a dynamic scope: the following instruction reads the static binding.
				continue
			}
			if i.Op == bytecode.OpTypeofName {
				v = vm.typeofValue(v)
			}
			regs[i.A] = v
			pc = int(i.C)
		case bytecode.OpSetName:
			vm.frames[len(vm.frames)-1].pc = pc
			found := vm.setName(fp.keys[i.B], regs[i.A], fp.strict)
			regs = vm.reload(regs, onHeap, base, fp)
			if found {
				pc = int(i.C)
			}
		case bytecode.OpDeleteName:
			vm.frames[len(vm.frames)-1].pc = pc
			regs[i.A] = Bool(vm.deleteName(fp.keys[i.B]))
		case bytecode.OpCallName:
			vm.frames[len(vm.frames)-1].pc = pc
			fn, this, found := vm.callName(fp.keys[i.B])
			regs = vm.reload(regs, onHeap, base, fp)
			if found {
				regs[i.A] = fn
				regs[i.A+1] = this
				pc = int(i.C)
			}

		case bytecode.OpDebugger, bytecode.OpNop:
		default:
			panic("ekuma: unknown bytecode.Opcode")
		}
		continue

	arith:
		{
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.arith(i.Op, regs[i.B], regs[i.C])
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
			continue
		}

	arithK:
		{
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.arith(i.Op-bytecode.OpAddK+bytecode.OpAdd, regs[i.B], fp.consts[i.C])
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
			continue
		}

	callAbove:
		// A call without arguments, such as a getter.
		argBase = vm.frames[len(vm.frames)-1].top()
		isNew, newTarget = false, nil
		goto call

	callRegs:
		// The arguments are in the registers from argBase on.
		if onHeap {
			top := vm.frames[len(vm.frames)-1].top()
			vm.ensureStack(top + argc)
			copy(vm.stack[top:], regs[argBase:argBase+argc])
			argBase = top
		} else {
			argBase += base
		}

	call:
		// The arguments are on the stack from argBase on.
		{
			if f == nil || f.Fn == nil {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.throwNotCallable(fp, pc-1, regs)
			}
			fn := f.Fn
			if c := fn.Code; c != nil && !c.isGenerator && !c.isAsync && (!fn.classConstructor || isNew || newTarget != nil) {
				if len(vm.frames) >= maxFrames {
					vm.frames[len(vm.frames)-1].pc = pc
					vm.ThrowRangeError("Maximum call stack size exceeded")
				}
				vm.frames[len(vm.frames)-1].pc = pc
				base = vm.pushFrame(f, callThis, argBase, argc, ret, isNew, newTarget)
				fp = c
				cl = f
				code = c.code
				onHeap = false
				regs = vm.stack[base : base+c.nregs+1]
				pc = 0
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			if c := fn.Code; c != nil && c.isGenerator && !c.isAsync && fn.Bound == nil {
				// Run the generator up to its initial suspension as an ordinary frame.
				vm.pushGenerator(f, callThis, vm.stack[argBase:argBase+argc], ret)
				fr := &vm.frames[len(vm.frames)-1]
				fp = c
				cl = f
				code = c.code
				base = fr.base
				onHeap = true
				regs = fr.gen.regs
				pc = 0
				continue
			}
			vm.ensureStack(argBase + argc)
			saved := vm.sp
			vm.sp = argBase + argc
			r := vm.CallSlow(f, callThis, vm.stack[argBase:argBase+argc:argBase+argc], isNew, newTarget)
			vm.sp = saved
			regs = vm.reload(regs, onHeap, base, fp)
			regs[ret] = r
			continue
		}

	construct:
		// new f(...) for anything but a plain constructor function.
		{
			vm.frames[len(vm.frames)-1].pc = pc
			if f == nil || f.Fn == nil || !vm.IsConstructor(f) {
				vm.throwNotConstructor(fp, pc-1, regs)
			}
			vm.ensureStack(argBase + argc)
			saved := vm.sp
			vm.sp = argBase + argc
			r := vm.Construct(f, vm.stack[argBase:argBase+argc:argBase+argc], f)
			vm.sp = saved
			regs = vm.reload(regs, onHeap, base, fp)
			regs[ret] = Value{O: r}
			continue
		}

	superCall:
		// super(...): f is the running constructor and newTarget its new.target.
		{
			vm.frames[len(vm.frames)-1].pc = pc
			parent := f.Shape.proto
			if parent == nil || parent.Fn == nil || !vm.IsConstructor(parent) {
				vm.ThrowTypeError("Super constructor is not a constructor")
			}
			vm.ensureStack(argBase + argc)
			saved := vm.sp
			vm.sp = argBase + argc
			r := vm.Construct(parent, vm.stack[argBase:argBase+argc:argBase+argc], newTarget)
			vm.sp = saved
			regs = vm.reload(regs, onHeap, base, fp)
			regs[ret] = Value{O: r}
			continue
		}

	throw:
		vm.frames[len(vm.frames)-1].pc = pc
		if !vm.unwind(stop) {
			return Undefined, execThrow
		}
		{
			fr := &vm.frames[len(vm.frames)-1]
			fp = fr.fp
			cl = fr.cl
			code = fp.code
			base = fr.base
			pc = fr.pc
			onHeap = fr.gen != nil
			if onHeap {
				regs = fr.gen.regs
			} else {
				regs = vm.stack[base : base+fp.nregs+1]
			}
		}
	}
}

// reload returns the registers of the running frame after a call that may have moved the stack.
func (vm *VM) reload(regs []Value, onHeap bool, base int, fp *funcProto) []Value {
	if onHeap {
		return regs
	}
	return vm.stack[base : base+fp.nregs+1]
}

func NumberPow(x, y float64) float64 {
	// The exponents NaN and ±Infinity with a base of ±1 give NaN, unlike in IEEE 754.
	if y != y || (math.IsInf(y, 0) && (x == 1 || x == -1)) {
		return math.NaN()
	}
	return math.Pow(x, y)
}
