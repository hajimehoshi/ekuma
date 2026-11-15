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
	gen *generator

	// scopes are the dynamic scopes entered by with statements, innermost last.
	scopes []*Object
}

// thrownErrorKinds maps the error kinds of OpThrowError to the VM's.
var thrownErrorKinds = [...]ErrorKind{
	bytecode.ThrowTypeError:      ErrorKindType,
	bytecode.ThrowReferenceError: ErrorKindReference,
}

// maxFrames is the call depth at which a RangeError is thrown.
const maxFrames = 10000

var tdzObject = &Object{kind: kindHole}

// tdz is the content of a binding that is not initialized yet.
var tdz = Value{o: tdzObject}

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

// stackHeadroom is the number of values that [VM.ensureStack] keeps free above what it is asked for.
const stackHeadroom = 64

func (vm *VM) ensureStack(n int) {
	vm.stackHigh = max(vm.stackHigh, n)
	if n+stackHeadroom <= len(vm.stack) {
		return
	}
	s := make([]Value, max(n+stackHeadroom, len(vm.stack)*2))
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

// clearStale clears the stack slots and frames above the ones in use, which the calls since the
// outermost call into the engine started may have left values in.
func (vm *VM) clearStale() {
	clear(vm.stack[vm.sp:min(vm.stackHigh+stackHeadroom, len(vm.stack))])
	vm.stackHigh = vm.sp
	n := len(vm.frames)
	clear(vm.frames[n:min(vm.framesHigh, cap(vm.frames))])
	vm.framesHigh = n
}

// pushFrame starts the activation of the closure f, whose argc arguments are already at base.
// It returns the stack index of register 0 of the new frame.
func (vm *VM) pushFrame(f *Object, this Value, base, argc int, ret int32, isNew bool, newTarget *Object) int {
	fp := f.fn.code
	fp.prepare()
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
	vm.framesHigh = max(vm.framesHigh, len(vm.frames))
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
		closing := vm.thrown.o == closeSignal
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
			vm.dropNatives(n)
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
		vm.ThrowRangeError("maximum call stack size exceeded")
	}
	vm.depth++
	// The frames may belong to other realms than the code that started the loop.
	saved, outer := vm.curRealm, vm.callerRealm
	vm.callerRealm = saved
	for {
		v, st := vm.exec(stop)
		switch st {
		case execReturn, execYield:
			vm.depth--
			vm.callerRealm = outer
			if vm.curRealm != saved {
				vm.switchRealm(saved)
			}
			return v
		case execPanic:
			if vm.unwind(stop) {
				continue
			}
		}
		vm.depth--
		vm.callerRealm = outer
		if vm.curRealm != saved {
			vm.switchRealm(saved)
		}
		panic(errThrow)
	}
}

// enterCallerRealm makes the realm of the caller of the frame at index n current, where stop
// is the frame that the running interpreter loop was started for.
func (vm *VM) enterCallerRealm(stop, n int) {
	r := vm.callerRealm
	if n > stop {
		r = vm.frames[n-1].fp.realm
	}
	if r != vm.curRealm {
		vm.switchRealm(r)
	}
}

// throwReturnError throws an error of the given kind for the return at pc of the frame at index n,
// where stop is as for enterCallerRealm. No handler of that frame catches it, and the error belongs
// to the realm of the caller.
func (vm *VM) throwReturnError(stop, n, pc int, kind ErrorKind, msg string) {
	vm.frames[n].pc = pc
	vm.enterCallerRealm(stop, n)
	e := vm.newError(kind, msg)
	// The frame has finished running, so it unwinds from pc 0, which lies before every handler.
	vm.frames[n].pc = 0
	vm.Throw(Value{o: e})
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
	if fp.realm != vm.curRealm {
		vm.switchRealm(fp.realm)
	}
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
			regs[i.A] = Value{n: float64(i.B)}
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
			if regs[i.A].o == tdzObject {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.throwTDZ(fp.keys[i.B])
			}
		case bytecode.OpThrowConstAssign:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.ThrowTypeError("assignment to constant variable '" + fp.keys[i.A].str + "'")
		case bytecode.OpThrowError:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.throwError(thrownErrorKinds[i.A], fp.consts[i.B].o.str)

		case bytecode.OpGetGlobal, bytecode.OpGetGlobalOrUndef:
			c := &fp.caches[i.C]
			if h := c.holder; h != nil && h.shape == c.shape && c.epoch == vm.globalEpoch {
				if v := h.slots[c.slot]; v.o != tdzObject {
					regs[i.A] = v
					continue
				}
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.getGlobal(fp.keys[i.B], c, i.Op == bytecode.OpGetGlobalOrUndef)
			regs[i.A] = v
		case bytecode.OpSetGlobal, bytecode.OpSetGlobalStrict:
			c := &fp.caches[i.C]
			if h := c.holder; h != nil && h.shape == c.shape && c.epoch == vm.globalEpoch && c.kind == cacheOwn {
				if h.slots[c.slot].o != tdzObject {
					h.slots[c.slot] = regs[i.A]
					continue
				}
			}
			vm.frames[len(vm.frames)-1].pc = pc
			vm.setGlobal(fp.keys[i.B], regs[i.A], c, i.Op == bytecode.OpSetGlobalStrict)
		case bytecode.OpResolveGlobal:
			c := &fp.caches[i.C]
			if h := c.holder; h != nil && h.shape == c.shape && c.epoch == vm.globalEpoch {
				regs[i.A] = True
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.resolveGlobal(fp.keys[i.B], c)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpDeclareGlobalVar:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.declareGlobalVar(fp.keys[i.A], int(i.B), regs[i.C])
		case bytecode.OpDeclareGlobalLex:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.declareGlobalLex(fp.keys[i.A], i.B != 0)
		case bytecode.OpInitGlobalLex:
			vm.initGlobalLex(fp.keys[i.B], regs[i.A])
		case bytecode.OpCheckGlobalDecl:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.checkGlobalDecl(fp.keys[i.A], int(i.B))
		case bytecode.OpDeclareAnnexB:
			vm.frames[len(vm.frames)-1].pc = pc
			regs[i.A] = Bool(vm.declareGlobalAnnexB(fp.keys[i.B], int(i.C)))

		case bytecode.OpGetUpval:
			regs[i.A] = *cl.fn.upvals[i.B].p
		case bytecode.OpSetUpval:
			*cl.fn.upvals[i.B].p = regs[i.A]
		case bytecode.OpCloseUpvals:
			if onHeap {
				vm.frames[len(vm.frames)-1].gen.closeUpvals(int(i.A))
			} else if len(vm.openUpvals) > 0 {
				vm.closeUpvals(base + int(i.A))
			}
		case bytecode.OpClosure:
			p := fp.protos[i.B]
			o, copies := newClosureObject(len(p.Upvals), p.ncopied)
			fn := o.fn
			fn.code = p
			fn.constructor = p.isConstructor
			fn.classConstructor = p.isClassConstructor
			fn.derived = p.isDerived
			o.kind = kindFunction
			o.class = classFunction
			o.flags = objExotic
			o.shape = vm.shapes.function
			if p.isGenerator || p.isAsync {
				o.shape = vm.functionShapeFor(vm.functionProtoFor(p))
			}
			for j, d := range p.Upvals {
				switch {
				case !d.Local:
					fn.upvals[j] = cl.fn.upvals[d.Index]
				case d.ByValue:
					u := &copies[0]
					copies = copies[1:]
					u.val = regs[d.Index]
					u.p = &u.val
					fn.upvals[j] = u
				case onHeap:
					fn.upvals[j] = vm.frames[len(vm.frames)-1].gen.findUpval(int(d.Index))
				default:
					fn.upvals[j] = vm.findUpval(base + int(d.Index))
				}
			}
			regs[i.A] = Value{o: o}

		case bytecode.OpLoadThis:
			regs[i.A] = vm.frames[len(vm.frames)-1].this
		case bytecode.OpLoadThisSloppy:
			t := vm.frames[len(vm.frames)-1].this
			if t.o == nil || t.o.kind < kindFirstObject {
				if t.IsNullish() {
					t = Value{o: vm.global}
				} else {
					t = Value{o: vm.ToObject(t)}
				}
			}
			regs[i.A] = t
		case bytecode.OpLoadCallee:
			regs[i.A] = Value{o: cl}
		case bytecode.OpLoadNewTarget:
			if nt := vm.frames[len(vm.frames)-1].newTarget; nt != nil {
				regs[i.A] = Value{o: nt}
			} else {
				regs[i.A] = Undefined
			}
		case bytecode.OpLoadHome:
			if h := cl.fn.home; h != nil {
				regs[i.A] = Value{o: h}
			} else {
				regs[i.A] = Undefined
			}
		case bytecode.OpArguments:
			cur := &vm.frames[len(vm.frames)-1]
			regs[i.A] = Value{o: vm.newArguments(cur, regs, i.B != 0)}
		case bytecode.OpRestArgs:
			cur := &vm.frames[len(vm.frames)-1]
			var rest []Value
			if int(i.B) < cur.argc {
				all := vm.frameArgs(cur, regs)
				rest = all[i.B:]
			}
			regs[i.A] = Value{o: vm.NewArray(rest)}

		case bytecode.OpGetProp:
			v := regs[i.B]
			c := &fp.caches[i.C]
			if o := v.o; o != nil {
				e := &c.cacheEntry
				if o.shape != e.shape {
					e = &c.second
				}
				if o.shape == e.shape {
					if e.kind == cacheOwn {
						regs[i.A] = o.slots[e.slot]
						continue
					}
					if e.epoch == vm.protoEpoch {
						if e.kind == cacheProto {
							regs[i.A] = e.holder.slots[e.slot]
							continue
						}
						if e.kind == cacheGetter {
							f, callThis, argc, ret = e.holder, v, 0, i.A
							goto callAbove
						}
					}
					if e.kind == cacheOwnGetter {
						g := o.slots[e.slot].accessor().getter
						if g == nil {
							regs[i.A] = Undefined
							continue
						}
						f, callThis, argc, ret = g, v, 0, i.A
						goto callAbove
					}
				}
			}
			vm.frames[len(vm.frames)-1].pc = pc
			val, getter := vm.getNamed(v, fp.keys[i.C], c)
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpSetProp, bytecode.OpSetPropStrict:
			v := regs[i.A]
			c := &fp.caches[i.B]
			var setter *Object
			if o := v.o; o != nil {
				e := &c.cacheEntry
				if o.shape != e.shape {
					e = &c.second
				}
				if o.shape == e.shape {
					switch e.kind {
					case cacheOwn:
						o.slots[e.slot] = regs[i.C]
						continue
					case cacheAdd:
						if e.epoch == vm.protoEpoch && o.flags&objIsProto == 0 {
							o.shape = e.next
							o.slots = append(o.slots, regs[i.C])
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
				setter, ok = vm.setNamed(v, fp.keys[i.B], regs[i.C], c)
				if !ok && i.Op == bytecode.OpSetPropStrict {
					vm.throwReadOnly(v, PropKey{key: fp.keys[i.B]})
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
			if o := v.o; o != nil && o.kind == kindArray {
				regs[i.A] = Value{n: float64(o.length)}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			val, getter := vm.getNamed(v, vm.Key(KeyLength), &fp.caches[i.C])
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpGetIndex:
			v, k := regs[i.B], regs[i.C]
			if o := v.o; o != nil && k.o == nil {
				if j := int(k.n); float64(j) == k.n && uint(j) < uint(len(o.elems)) {
					if e := o.elems[j]; e.o != holeObject {
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
			if o := v.o; o != nil && uint(i.C) < uint(len(o.elems)) {
				if e := o.elems[i.C]; e.o != holeObject {
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
			if o := v.o; o != nil && k.o == nil {
				if j := int(k.n); float64(j) == k.n && uint(j) < uint(len(o.elems)) {
					if o.elems[j].o != holeObject {
						o.elems[j] = regs[i.C]
						continue
					}
				} else if j == len(o.elems) && o.kind == kindArray && uint32(j) == o.length && o.flags == 0 && float64(j) == k.n && o.shape == vm.shapes.array && vm.arrayProtoPlain() {
					// Appending to a plain array.
					o.elems = appendElem(o.elems, regs[i.C])
					o.length++
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
		case bytecode.OpSuperBase:
			vm.frames[len(vm.frames)-1].pc = pc
			regs[i.A] = vm.superBase(regs[i.B])
		case bytecode.OpGetSuper:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			ext := code[pc]
			pc++
			if ext.B == 0 {
				v, _ := vm.getSuper(vm.superBase(regs[ext.A]), regs[i.B], regs[i.C])
				regs = vm.reload(regs, onHeap, base, fp)
				regs[i.A] = v
				continue
			}
			v, key := vm.getSuper(regs[ext.A], regs[i.B], regs[i.C])
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.B] = vm.KeyValue(key)
			regs[i.A] = v
		case bytecode.OpSetSuper:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			strict := code[pc].A != 0
			superBase := regs[code[pc].B]
			pc++
			vm.setSuper(superBase, regs[i.A], regs[i.B], regs[i.C], strict)
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
				vm.ThrowTypeError("cannot use 'in' operator to search for a key in a non-object")
			}
			r := vm.HasProperty(c.o, vm.QueryKey(regs[i.B]))
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
				vm.ThrowTypeError("cannot use 'in' operator to search for a private field in a non-object")
			}
			_, ok := c.o.shape.lookup(regs[i.B].o)
			regs[i.A] = Bool(ok)
		case bytecode.OpGetPrivate:
			v := regs[i.B]
			vm.frames[len(vm.frames)-1].pc = pc
			val, getter := vm.getPrivate(v, regs[i.C].o)
			if getter != nil {
				f, callThis, argc, ret = getter, v, 0, i.A
				goto callAbove
			}
			regs[i.A] = val
		case bytecode.OpSetPrivate:
			v := regs[i.A]
			vm.frames[len(vm.frames)-1].pc = pc
			if setter := vm.setPrivate(v, regs[i.B].o, regs[i.C]); setter != nil {
				val := regs[i.C]
				argBase = vm.frames[len(vm.frames)-1].top()
				vm.ensureStack(argBase + 1)
				vm.stack[argBase] = val
				f, callThis, argc, ret, isNew, newTarget = setter, v, 1, int32(fp.nregs), false, nil
				goto call
			}
		case bytecode.OpToPropertyKey:
			v := regs[i.B]
			if v.o != nil && (v.o.kind == kindString || v.o.kind == kindSymbol) {
				regs[i.A] = v
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			if i.C != 0 {
				if b := regs[i.C-1]; b.IsNullish() {
					vm.ThrowTypeError("cannot read properties of " + vm.DisplayString(b) + " (reading '" + vm.DisplayString(v) + "')")
				}
			}
			k := vm.KeyValue(vm.ToPropertyKey(v))
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = k

		case bytecode.OpAdd:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Value{n: x.n + y.n}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			if x.o != nil && y.o != nil && x.o.kind == kindString && y.o.kind == kindString {
				regs[i.A] = vm.concat(x, x.o.str, y, y.o.str)
				continue
			}
			v := vm.addValues(x, y)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpSub:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Value{n: x.n - y.n}
				continue
			}
			goto arith
		case bytecode.OpMul:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Value{n: x.n * y.n}
				continue
			}
			goto arith
		case bytecode.OpDiv:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Value{n: x.n / y.n}
				continue
			}
			goto arith
		case bytecode.OpMod:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				xi, yi := int32(x.n), int32(y.n)
				if xi > 0 && yi > 0 && float64(xi) == x.n && float64(yi) == y.n {
					regs[i.A] = Value{n: float64(xi % yi)}
					continue
				}
				regs[i.A] = Value{n: math.Mod(x.n, y.n)}
				continue
			}
			goto arith
		case bytecode.OpBitOr:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Value{n: float64(numconv.ToInt32(x.n) | numconv.ToInt32(y.n))}
				continue
			}
			goto arith
		case bytecode.OpBitAnd:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Value{n: float64(numconv.ToInt32(x.n) & numconv.ToInt32(y.n))}
				continue
			}
			goto arith
		case bytecode.OpBitXor:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Value{n: float64(numconv.ToInt32(x.n) ^ numconv.ToInt32(y.n))}
				continue
			}
			goto arith
		case bytecode.OpShl:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Value{n: float64(numconv.ToInt32(x.n) << (numconv.ToUint32(y.n) & 31))}
				continue
			}
			goto arith
		case bytecode.OpShr:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Value{n: float64(numconv.ToInt32(x.n) >> (numconv.ToUint32(y.n) & 31))}
				continue
			}
			goto arith
		case bytecode.OpUShr:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Value{n: float64(numconv.ToUint32(x.n) >> (numconv.ToUint32(y.n) & 31))}
				continue
			}
			goto arith
		case bytecode.OpExp:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Value{n: NumberPow(x.n, y.n)}
				continue
			}
			goto arith
		case bytecode.OpAddK:
			x := regs[i.B]
			if x.o == nil {
				regs[i.A] = Value{n: x.n + fp.consts[i.C].n}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.addValues(x, fp.consts[i.C])
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpSubK:
			x := regs[i.B]
			if x.o == nil {
				regs[i.A] = Value{n: x.n - fp.consts[i.C].n}
				continue
			}
			goto arithK
		case bytecode.OpMulK:
			x := regs[i.B]
			if x.o == nil {
				regs[i.A] = Value{n: x.n * fp.consts[i.C].n}
				continue
			}
			goto arithK
		case bytecode.OpDivK:
			x := regs[i.B]
			if x.o == nil {
				regs[i.A] = Value{n: x.n / fp.consts[i.C].n}
				continue
			}
			goto arithK
		case bytecode.OpModK:
			x, y := regs[i.B], fp.consts[i.C]
			if x.o == nil {
				xi, yi := int32(x.n), int32(y.n)
				if xi > 0 && yi > 0 && float64(xi) == x.n && float64(yi) == y.n {
					regs[i.A] = Value{n: float64(xi % yi)}
					continue
				}
				regs[i.A] = Value{n: math.Mod(x.n, y.n)}
				continue
			}
			goto arithK
		case bytecode.OpBitOrK:
			x := regs[i.B]
			if x.o == nil {
				regs[i.A] = Value{n: float64(numconv.ToInt32(x.n) | int32(fp.consts[i.C].n))}
				continue
			}
			goto arithK
		case bytecode.OpBitAndK:
			x := regs[i.B]
			if x.o == nil {
				regs[i.A] = Value{n: float64(numconv.ToInt32(x.n) & int32(fp.consts[i.C].n))}
				continue
			}
			goto arithK
		case bytecode.OpShlK:
			x := regs[i.B]
			if x.o == nil {
				regs[i.A] = Value{n: float64(numconv.ToInt32(x.n) << (uint32(fp.consts[i.C].n) & 31))}
				continue
			}
			goto arithK
		case bytecode.OpShrK:
			x := regs[i.B]
			if x.o == nil {
				regs[i.A] = Value{n: float64(numconv.ToInt32(x.n) >> (uint32(fp.consts[i.C].n) & 31))}
				continue
			}
			goto arithK
		case bytecode.OpUShrK:
			x := regs[i.B]
			if x.o == nil {
				regs[i.A] = Value{n: float64(numconv.ToUint32(x.n) >> (uint32(fp.consts[i.C].n) & 31))}
				continue
			}
			goto arithK
		case bytecode.OpNeg:
			x := regs[i.B]
			if x.o == nil {
				regs[i.A] = Value{n: -x.n}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.negate(x)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpPlus:
			x := regs[i.B]
			if x.o == nil {
				regs[i.A] = x
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			n := vm.ToNumber(x)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Value{n: n}
		case bytecode.OpNot:
			regs[i.A] = Bool(!ToBoolean(regs[i.B]))
		case bytecode.OpBitNot:
			x := regs[i.B]
			if x.o == nil {
				regs[i.A] = Value{n: float64(^numconv.ToInt32(x.n))}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.bitNot(x)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpToNumeric:
			x := regs[i.B]
			if x.o == nil {
				regs[i.A] = x
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.ToNumeric(x)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpToString:
			x := regs[i.B]
			if x.o != nil && x.o.kind == kindString {
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
			if x.o == nil {
				if i.Op == bytecode.OpInc {
					regs[i.A] = Value{n: x.n + 1}
				} else {
					regs[i.A] = Value{n: x.n - 1}
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
			if x.o == nil && y.o == nil {
				regs[i.A] = Bool(x.n < y.n)
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.compare(x, y, true) == cmpLess
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Bool(r)
		case bytecode.OpLe:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Bool(x.n <= y.n)
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			c := vm.compare(y, x, false)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Bool(c == cmpGreater || c == cmpEqual)
		case bytecode.OpGt:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Bool(x.n > y.n)
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			r := vm.compare(y, x, false) == cmpLess
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Bool(r)
		case bytecode.OpGe:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				regs[i.A] = Bool(x.n >= y.n)
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			c := vm.compare(x, y, true)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Bool(c == cmpGreater || c == cmpEqual)
		case bytecode.OpEq, bytecode.OpNe:
			x, y := regs[i.B], regs[i.C]
			var r bool
			if x.o == nil && y.o == nil {
				r = x.n == y.n
			} else {
				vm.frames[len(vm.frames)-1].pc = pc
				r = vm.looseEquals(x, y)
				regs = vm.reload(regs, onHeap, base, fp)
			}
			regs[i.A] = Bool(r == (i.Op == bytecode.OpEq))
		case bytecode.OpStrictEq:
			regs[i.A] = Bool(IsStrictlyEqual(regs[i.B], regs[i.C]))
		case bytecode.OpStrictNe:
			regs[i.A] = Bool(!IsStrictlyEqual(regs[i.B], regs[i.C]))

		case bytecode.OpJump:
			if int(i.A) < pc && vm.interrupt.Load() {
				// Every loop jumps backwards with this instruction.
				vm.frames[len(vm.frames)-1].pc = pc
				vm.Poll()
			}
			pc = int(i.A)
		case bytecode.OpJumpIf, bytecode.OpJumpIfNot:
			// ToBoolean, with the common cases inlined.
			v := regs[i.B]
			var t bool
			if o := v.o; o == nil {
				t = v.n != 0 && v.n == v.n
			} else if k := o.kind; k == kindTrue {
				t = true
			} else if k <= kindFalse {
				t = false
			} else if k >= kindObject {
				// kindHTMLDDA is below kindObject and takes the slow path.
				t = true
			} else {
				t = ToBoolean(v)
			}
			if t == (i.Op == bytecode.OpJumpIf) {
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
			if regs[i.B].o == undefinedObject {
				pc = int(i.A)
			}
		case bytecode.OpJumpIfNotUndef:
			if regs[i.B].o != undefinedObject {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLt:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				if !(x.n < y.n) {
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
			if x.o == nil && y.o == nil {
				if !(x.n <= y.n) {
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
			if x.o == nil && y.o == nil {
				if !(x.n > y.n) {
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
			if x.o == nil && y.o == nil {
				if !(x.n >= y.n) {
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
			if x.o == nil && y.o == nil {
				r = x.n == y.n
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
			if x.o == nil && y.o == nil {
				if x.n != y.n {
					pc = int(i.A)
				}
				continue
			}
			if !IsStrictlyEqual(x, y) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotSNe:
			x, y := regs[i.B], regs[i.C]
			if x.o == nil && y.o == nil {
				if x.n == y.n {
					pc = int(i.A)
				}
				continue
			}
			if IsStrictlyEqual(x, y) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLtK:
			x := regs[i.B]
			if x.o != nil {
				vm.frames[len(vm.frames)-1].pc = pc
				x = vm.ToNumeric(vm.ToPrimitive(x, HintNumber))
				regs = vm.reload(regs, onHeap, base, fp)
				if x.o != nil {
					if compareBigIntMixed(x, fp.consts[i.C]) != cmpLess {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.n < fp.consts[i.C].n) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotLeK:
			x := regs[i.B]
			if x.o != nil {
				vm.frames[len(vm.frames)-1].pc = pc
				x = vm.ToNumeric(vm.ToPrimitive(x, HintNumber))
				regs = vm.reload(regs, onHeap, base, fp)
				if x.o != nil {
					if c := compareBigIntMixed(x, fp.consts[i.C]); c != cmpLess && c != cmpEqual {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.n <= fp.consts[i.C].n) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGtK:
			x := regs[i.B]
			if x.o != nil {
				vm.frames[len(vm.frames)-1].pc = pc
				x = vm.ToNumeric(vm.ToPrimitive(x, HintNumber))
				regs = vm.reload(regs, onHeap, base, fp)
				if x.o != nil {
					if compareBigIntMixed(x, fp.consts[i.C]) != cmpGreater {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.n > fp.consts[i.C].n) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotGeK:
			x := regs[i.B]
			if x.o != nil {
				vm.frames[len(vm.frames)-1].pc = pc
				x = vm.ToNumeric(vm.ToPrimitive(x, HintNumber))
				regs = vm.reload(regs, onHeap, base, fp)
				if x.o != nil {
					if c := compareBigIntMixed(x, fp.consts[i.C]); c != cmpGreater && c != cmpEqual {
						pc = int(i.A)
					}
					continue
				}
			}
			if !(x.n >= fp.consts[i.C].n) {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotSEqK:
			if x := regs[i.B]; x.o != nil || x.n != fp.consts[i.C].n {
				pc = int(i.A)
			}
		case bytecode.OpJumpNotSNeK:
			if x := regs[i.B]; x.o == nil && x.n == fp.consts[i.C].n {
				pc = int(i.A)
			}
		case bytecode.OpJumpIfInt:
			if x := regs[i.B]; x.o == nil && x.n == float64(i.C) {
				pc = int(i.A)
			}

		case bytecode.OpCall:
			a := int(i.A)
			f, callThis, argBase, argc, ret, isNew, newTarget = regs[a].o, regs[a+1], a+2, int(i.B), i.A, false, nil
			goto callRegs
		case bytecode.OpCallCall:
			a := int(i.A)
			if regs[a].o == vm.Intrinsic(IntrinsicFunctionPrototypeCall) {
				f, callThis, argBase, argc, ret, isNew, newTarget = regs[a+1].o, regs[a+2], a+3, int(i.B)-1, i.A, false, nil
				if argc < 0 {
					callThis, argc = Undefined, 0
				}
			} else {
				f, callThis, argBase, argc, ret, isNew, newTarget = regs[a].o, regs[a+1], a+2, int(i.B), i.A, false, nil
			}
			goto callRegs
		case bytecode.OpApplyFwd, bytecode.OpTailApplyFwd:
			a := int(i.A)
			if regs[a].o != vm.Intrinsic(IntrinsicFunctionPrototypeApply) {
				// Something else is called apply here: pass it a real arguments object.
				regs[a+3] = Value{o: vm.newArguments(&vm.frames[len(vm.frames)-1], regs, i.B != 0)}
				f, callThis, argBase, argc, ret, isNew, newTarget = regs[a].o, regs[a+1], a+2, 2, i.A, false, nil
				if i.Op == bytecode.OpTailApplyFwd {
					argBase += base
					goto tailCall
				}
				goto callRegs
			}
			f, callThis = regs[a+1].o, regs[a+2]
			goto forward
		case bytecode.OpCallFwd, bytecode.OpTailCallFwd:
			a := int(i.A)
			f, callThis = regs[a].o, regs[a+1]
			goto forward
		case bytecode.OpCallSpread, bytecode.OpNewSpread, bytecode.OpSuperSpread:
			a := int(i.A)
			f, callThis, ret, isNew, newTarget = regs[a].o, regs[a+1], i.A, false, nil
			args := regs[a+2].o.elems
			argc = len(args)
			argBase = vm.frames[len(vm.frames)-1].top()
			vm.ensureStack(argBase + argc)
			copy(vm.stack[argBase:], args)
			switch i.Op {
			case bytecode.OpNewSpread:
				goto construct
			case bytecode.OpSuperSpread:
				newTarget = regs[a+1].o
				goto superCall
			}
			goto call
		case bytecode.OpCallEval, bytecode.OpTailCallEval:
			a := int(i.A)
			if regs[a].o == vm.Intrinsic(IntrinsicEval) {
				vm.frames[len(vm.frames)-1].pc = pc
				v := vm.directEval(evalArg(regs[a+2:], int(i.B)), int(i.C))
				regs = vm.reload(regs, onHeap, base, fp)
				regs[a] = v
				if i.B < 0 {
					pc++
				}
				continue
			}
			if i.B < 0 {
				continue
			}
			f, callThis, argBase, argc, ret, isNew, newTarget = regs[a].o, regs[a+1], a+2, int(i.B), i.A, false, nil
			if i.Op == bytecode.OpTailCallEval {
				argBase += base
				goto tailCall
			}
			goto callRegs
		case bytecode.OpTailCall:
			a := int(i.A)
			f, callThis, argBase, argc, ret, isNew, newTarget = regs[a].o, regs[a+1], base+a+2, int(i.B), i.A, false, nil
			goto tailCall
		case bytecode.OpTailCallSpread:
			a := int(i.A)
			f, callThis, ret, isNew, newTarget = regs[a].o, regs[a+1], i.A, false, nil
			args := regs[a+2].o.elems
			argc = len(args)
			argBase = vm.sp
			vm.ensureStack(argBase + argc)
			copy(vm.stack[argBase:], args)
			goto tailCall
		case bytecode.OpNew:
			a := int(i.A)
			f, argBase, argc, ret = regs[a].o, a+2, int(i.B), i.A
			if onHeap {
				top := vm.frames[len(vm.frames)-1].top()
				vm.ensureStack(top + argc)
				copy(vm.stack[top:], regs[argBase:argBase+argc])
				argBase = top
			} else {
				argBase += base
			}
			if f != nil && f.fn != nil && f.fn.code != nil && f.fn.constructor && !f.fn.derived {
				// The common case: a plain constructor function, whose prototype property is cached.
				c := &fp.caches[i.C]
				var proto Value
				if f.shape == c.shape && c.kind == cacheOwn {
					proto = f.slots[c.slot]
				} else {
					vm.frames[len(vm.frames)-1].pc = pc
					proto, _ = vm.getNamed(Value{o: f}, vm.Key(KeyPrototype), c)
				}
				if proto.o == nil || proto.o.kind < kindFirstObject {
					proto = Value{o: vm.realmIntrinsic(f.fn.code.realm, IntrinsicObjectPrototype)}
				}
				var shape *shape
				if proto.o == c.holder && c.next != nil {
					shape = c.next
				} else {
					shape = vm.shapeFor(proto.o)
					if c.kind == cacheOwn {
						c.holder, c.next = proto.o, shape
					}
				}
				callThis = Value{
					o: &Object{
						kind:  kindObject,
						shape: shape,
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
			f, newTarget = regs[a].o, regs[a+1].o
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
						vm.throwReturnError(stop, n, pc, ErrorKindType, "derived constructors may only return object or undefined")
					}
					r = regs[i.B]
					if r.o == tdzObject {
						vm.throwReturnError(stop, n, pc, ErrorKindReference, "must call super constructor in derived class before accessing 'this' or returning from derived constructor")
					}
				}
			} else if done.isNew && (r.o == nil || r.o.kind < kindFirstObject) {
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
			if fp.realm != vm.curRealm {
				vm.switchRealm(fp.realm)
			}
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
			if i.B != 0 && vm.thrown.o == closeSignal {
				vm.thrown = regs[i.B-1]
			}
			goto throw

		case bytecode.OpInitialYield:
			n := len(vm.frames) - 1
			cur := &vm.frames[n]
			g := cur.gen
			g.pc = pc
			g.scopes = cur.scopes
			g.state = genSuspendedStart
			o := vm.newGeneratorObject(g)
			ret = cur.ret
			*cur = frame{}
			vm.frames = vm.frames[:n]
			if n == stop {
				return Value{o: o}, execReturn
			}
			fr := &vm.frames[n-1]
			fp = fr.fp
			if fp.realm != vm.curRealm {
				vm.switchRealm(fp.realm)
			}
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
			regs[ret] = Value{o: o}
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
			g.delegating = i.C != 0
			*cur = frame{}
			vm.frames = vm.frames[:n]
			// A generator only ever runs as the frame its loop was started for.
			return v, execYield
		case bytecode.OpGenResumeMode:
			g := vm.frames[len(vm.frames)-1].gen
			regs[i.A] = Value{n: float64(g.mode)}
			g.mode = ResumeNext
		case bytecode.OpYieldDelegateResult:
			vm.frames[len(vm.frames)-1].pc = pc
			r := regs[i.B]
			if !r.IsObject() {
				vm.ThrowTypeError("iterator result " + vm.DisplayString(r) + " is not an object")
			}
			done := ToBoolean(vm.GetV(r, vm.Key(keyDone)))
			regs = vm.reload(regs, onHeap, base, fp)
			if !done {
				pc = int(i.C)
				continue
			}
			v := vm.GetV(r, vm.Key(KeyValue))
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v

		case bytecode.OpNewObject:
			o, slots := newObjectWithStorage(int(i.C))
			o.kind = kindObject
			o.shape = vm.shapes.object
			o.slots = slots[:0]
			regs[i.A] = Value{o: o}
		case bytecode.OpNewArray:
			o, elems := newObjectWithStorage(int(max(i.B, i.C)))
			elems = elems[:i.B]
			copy(elems, regs[i.A+1:])
			o.kind = kindArray
			o.class = classArray
			o.shape = vm.shapes.array
			o.elems = elems
			o.length = uint32(len(elems))
			regs[i.A] = Value{o: o}
		case bytecode.OpArrayPush:
			o := regs[i.A].o
			o.elems = appendElem(o.elems, regs[i.B])
			o.length = uint32(len(o.elems))
		case bytecode.OpArrayHole:
			o := regs[i.A].o
			o.elems = appendElem(o.elems, hole)
			o.length = uint32(len(o.elems))
		case bytecode.OpArraySpread:
			vm.frames[len(vm.frames)-1].pc = pc
			o := regs[i.A].o
			vm.appendIterable(o, regs[i.B])
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpRegExp:
			vm.frames[len(vm.frames)-1].pc = pc
			regs[i.A] = Value{o: vm.newRegExpLiteral(&fp.regExps[i.B])}
		case bytecode.OpRegExpArg:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.lendRegExpLiteral(regs[i.A:], &fp.regExps[i.B], i.C != 0)
		case bytecode.OpRegExpMethod:
			vm.frames[len(vm.frames)-1].pc = pc
			m, this := vm.regExpLiteralMethod(&fp.regExps[i.B], fp.keys[i.C], &fp.caches[i.C])
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A], regs[i.A+1] = m, Value{o: this}
		case bytecode.OpTemplate:
			regs[i.A] = Value{o: vm.templateObject(&fp.templates[i.B])}
		case bytecode.OpDefineField:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.defineField(regs[i.A].o, regs[i.B], regs[i.C])
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpDefineFieldK:
			o := regs[i.A].o
			c := &fp.caches[i.B]
			if o.shape == c.shape && c.kind == cacheAdd {
				o.shape = c.next
				o.slots = append(o.slots, regs[i.C])
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			vm.defineFieldK(o, fp.keys[i.B], regs[i.C], c)
		case bytecode.OpDefineMethod:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.defineMethod(regs[i.A].o, regs[i.B], regs[i.C])
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpDefineGetter, bytecode.OpDefineSetter:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			enumerable := code[pc].A != 0
			pc++
			vm.defineAccessor(regs[i.A].o, regs[i.B], regs[i.C].o, i.Op == bytecode.OpDefineSetter, enumerable)
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpSetFuncName:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.setFuncName(regs[i.A], regs[i.B], int(i.C))
		case bytecode.OpSetHome:
			regs[i.A].o.fn.home = regs[i.B].o
		case bytecode.OpCopyProps:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.copyDataProperties(regs[i.A].o, regs[i.B], nil)
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpCopyPropsExcept:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			n := int(code[pc].A)
			pc++
			o := vm.NewOrdinaryObject()
			excluded := append([]Value(nil), regs[int(i.C)+1:int(i.C)+1+n]...)
			vm.copyDataProperties(o, regs[i.B], excluded)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Value{o: o}
		case bytecode.OpSetProto:
			if p := regs[i.B]; p.IsObject() {
				vm.setProto(regs[i.A].o, p.o)
			} else if p.IsNull() {
				vm.setProto(regs[i.A].o, nil)
			}
		case bytecode.OpClass:
			vm.frames[len(vm.frames)-1].pc = pc
			parent := hole
			if i.C >= 0 {
				parent = regs[i.C]
			}
			proto := vm.setupClass(regs[i.A].o, parent)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A+1] = Value{o: proto}
		case bytecode.OpPrivateName:
			name := &Object{
				kind:  kindSymbol,
				class: classPrivateName,
				str:   fp.keys[i.B].str,
			}
			if i.C != 0 {
				name.flags |= objFrozenLength
			}
			regs[i.A] = Value{o: name}
		case bytecode.OpInitFields:
			if regs[i.A].o.fn.fields != nil {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.initFields(regs[i.A].o, regs[i.B])
				regs = vm.reload(regs, onHeap, base, fp)
			}
		case bytecode.OpAddField:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			kind := int(code[pc].A)
			pc++
			var init *Object
			if i.C >= 0 {
				init = regs[i.C].o
			}
			vm.addClassElement(regs[i.A].o, regs[i.B], init, kind)
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpCheckThisUninit:
			if regs[i.A].o != tdzObject {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.ThrowReferenceError("super constructor may only be called once")
			}
		case bytecode.OpRequireCoercible:
			if regs[i.A].IsNullish() {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.ThrowTypeError("cannot destructure '" + vm.DisplayString(regs[i.A]) + "' as it is " + vm.DisplayString(regs[i.A]))
			}

		case bytecode.OpForIn:
			vm.frames[len(vm.frames)-1].pc = pc
			it := vm.newForIn(regs[i.B])
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = it
		case bytecode.OpForInNext:
			vm.frames[len(vm.frames)-1].pc = pc
			k, ok := vm.forInNext(regs[i.B].o)
			regs = vm.reload(regs, onHeap, base, fp)
			if !ok {
				pc = int(i.C)
				continue
			}
			regs[i.A] = k
		case bytecode.OpGetIterator:
			v := regs[i.B]
			if i.C == bytecode.IterSync && v.o != nil && v.o.kind == kindArray && vm.arrayIterationPlain(v.o) {
				// Iterate a plain array by index: the next method slot holds the index.
				regs[i.A] = v
				regs[i.A+1] = Value{n: 0}
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			it, next := vm.getIterator(v, i.C == bytecode.IterAsync)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = it
			regs[i.A+1] = next
		case bytecode.OpIterNext:
			if regs[i.B].o == undefinedObject {
				// The iterator finished earlier.
				pc = int(i.C)
				continue
			}
			if idx := regs[i.B+1]; idx.o == nil {
				o := regs[i.B].o
				j := uint32(idx.n)
				if j >= o.length {
					// Mark the iterator as finished, so that closing it does nothing.
					regs[i.B] = Undefined
					pc = int(i.C)
					continue
				}
				regs[i.B+1] = Value{n: idx.n + 1}
				if int(j) < len(o.elems) && o.elems[j].o != holeObject {
					regs[i.A] = o.elems[j]
					continue
				}
				vm.frames[len(vm.frames)-1].pc = pc
				v := vm.Index(Value{o: o}, j)
				regs = vm.reload(regs, onHeap, base, fp)
				regs[i.A] = v
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			{
				// An iterator whose step throws counts as finished, so that it is not closed.
				it := regs[i.B]
				regs[i.B] = Undefined
				v, st := vm.iterNext(it, regs[i.B+1])
				regs = vm.reload(regs, onHeap, base, fp)
				switch st {
				case iterValue:
					regs[i.B] = it
					regs[i.A] = v
				case iterDone:
					pc = int(i.C)
				default:
					goto throw
				}
			}
		case bytecode.OpIterClose:
			it := regs[i.A]
			if it.IsUndefined() || regs[i.A+1].o == nil {
				continue
			}
			regs[i.A] = Undefined
			vm.frames[len(vm.frames)-1].pc = pc
			vm.iteratorClose(it, i.B != 0 && regs[i.B-1].o != closeSignal)
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpIterResult:
			regs[i.A] = vm.CreateIterResultObject(regs[i.B], i.C != 0)
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
		case bytecode.OpIterReturn:
			vm.frames[len(vm.frames)-1].pc = pc
			{
				it := regs[i.B]
				regs[i.B] = Undefined
				r, ok := vm.iterReturn(it)
				regs = vm.reload(regs, onHeap, base, fp)
				regs[i.A] = r
				regs[i.A+1] = Bool(ok)
			}
		case bytecode.OpIterCheckResult:
			if !regs[i.A].IsObject() {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.throwNotIterResult(regs[i.A])
			}
		case bytecode.OpIterResultValue:
			vm.frames[len(vm.frames)-1].pc = pc
			{
				v, done := vm.iterResultValue(regs[i.B])
				regs = vm.reload(regs, onHeap, base, fp)
				if done {
					pc = int(i.C)
					continue
				}
				regs[i.A] = v
			}

		case bytecode.OpUsing:
			vm.frames[len(vm.frames)-1].pc = pc
			{
				dc := vm.addDisposableOp(regs[i.A], regs[i.B], DisposeHint(i.C))
				regs = vm.reload(regs, onHeap, base, fp)
				regs[i.B] = dc
			}
		case bytecode.OpDisposeBegin:
			beginDisposeOp(regs[i.A], regs[i.B], regs[i.C])
		case bytecode.OpDisposeStep:
			vm.frames[len(vm.frames)-1].pc = pc
			{
				v, await := vm.disposeStepOp(regs[i.B])
				regs = vm.reload(regs, onHeap, base, fp)
				if !await {
					pc = int(i.C)
					continue
				}
				regs[i.A] = v
			}
		case bytecode.OpDisposeError:
			vm.disposeError(regs[i.A].o.ext.(*DisposeCapability), regs[i.B])
		case bytecode.OpDisposeEnd:
			if v, thrown := endDisposeOp(regs[i.A]); thrown {
				regs[i.A] = Undefined
				vm.thrown = v
				goto throw
			}
			regs[i.A] = Undefined

		case bytecode.OpWith:
			vm.frames[len(vm.frames)-1].pc = pc
			o := vm.ToObject(regs[i.A])
			cur := &vm.frames[len(vm.frames)-1]
			cur.scopes = append(cur.scopes, o)
		case bytecode.OpPopScope:
			cur := &vm.frames[len(vm.frames)-1]
			cur.scopes = cur.scopes[:len(cur.scopes)-1]
		case bytecode.OpResolveName:
			vm.frames[len(vm.frames)-1].pc = pc
			o := vm.findScope(cl, fp.keys[i.B], int(i.C))
			regs = vm.reload(regs, onHeap, base, fp)
			if o != nil {
				regs[i.A] = Value{o: o}
			} else {
				regs[i.A] = Undefined
			}
		case bytecode.OpGetScoped:
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.getScoped(regs[i.B].o, fp.keys[i.C], fp.strict)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpSetScoped:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.setScoped(regs[i.B].o, fp.keys[i.C], regs[i.A], fp.strict)
			regs = vm.reload(regs, onHeap, base, fp)
		case bytecode.OpDeleteScoped:
			vm.frames[len(vm.frames)-1].pc = pc
			ok := vm.DeleteProp(regs[i.B].o, PropKey{key: fp.keys[i.C]})
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = Bool(ok)
		case bytecode.OpDeleteName:
			vm.frames[len(vm.frames)-1].pc = pc
			regs[i.A] = Bool(vm.deleteName(fp.keys[i.B]))
		case bytecode.OpCaptureScopes:
			regs[i.A].o.fn.scopes = vm.openScopes(cl)
		case bytecode.OpEvalScope:
			vm.pushEvalScope()
		case bytecode.OpScopeThis:
			regs[i.A] = scopeThis(regs[i.B])
		case bytecode.OpDeclareEvalVar:
			vm.frames[len(vm.frames)-1].pc = pc
			vm.declareEvalVar(cl, fp.keys[i.A], int(i.B), regs, int(i.C))
		case bytecode.OpImport:
			vm.frames[len(vm.frames)-1].pc = pc
			v := vm.importOp(regs, i.B, i.C)
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
		case bytecode.OpAutoAccessor:
			vm.autoAccessorOp(regs, i)
		case bytecode.OpDecorate:
			vm.frames[len(vm.frames)-1].pc = pc + 1
			pc++
			if v, ok := vm.decorateOp(regs, i, &code[pc-1]); ok {
				regs = vm.reload(regs, onHeap, base, fp)
				regs[i.C] = v
			} else {
				regs = vm.reload(regs, onHeap, base, fp)
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
			v := vm.arith(i.Op.RegOperand(), regs[i.B], fp.consts[i.C])
			regs = vm.reload(regs, onHeap, base, fp)
			regs[i.A] = v
			continue
		}

	callAbove:
		// A call without arguments, such as a getter.
		argBase = vm.frames[len(vm.frames)-1].top()
		isNew, newTarget = false, nil
		goto call

	forward:
		// A call with the arguments of the running function.
		{
			cur := &vm.frames[len(vm.frames)-1]
			argc, ret, isNew, newTarget = cur.argc, i.A, false, nil
			argBase = cur.top()
			vm.ensureStack(argBase + argc)
			n := min(argc, fp.nparams)
			copy(vm.stack[argBase:], regs[:n])
			copy(vm.stack[argBase+n:], vm.extraArgs(&vm.frames[len(vm.frames)-1]))
			if i.Op >= bytecode.OpTailApplyFwd {
				goto tailCall
			}
			goto call
		}

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
			if f == nil || f.fn == nil {
				vm.frames[len(vm.frames)-1].pc = pc
				vm.throwNotCallable(fp, pc-1, regs)
			}
			fn := f.fn
			if c := fn.code; c != nil && !c.isGenerator && !c.isAsync && (!fn.classConstructor || isNew || newTarget != nil) {
				if len(vm.frames) >= maxFrames {
					vm.frames[len(vm.frames)-1].pc = pc
					vm.ThrowRangeError("maximum call stack size exceeded")
				}
				vm.frames[len(vm.frames)-1].pc = pc
				base = vm.pushFrame(f, callThis, argBase, argc, ret, isNew, newTarget)
				if c.realm != vm.curRealm {
					vm.switchRealm(c.realm)
				}
				fp = c
				cl = f
				code = c.code
				onHeap = false
				regs = vm.stack[base : base+c.nregs+1]
				pc = 0
				continue
			}
			vm.frames[len(vm.frames)-1].pc = pc
			if c := fn.code; c != nil && c.isGenerator && !c.isAsync && fn.bound == nil {
				// Run the generator up to its initial suspension as an ordinary frame.
				vm.pushGenerator(f, callThis, vm.stack[argBase:argBase+argc], ret)
				if c.realm != vm.curRealm {
					vm.switchRealm(c.realm)
				}
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
			r := vm.callSlow(f, callThis, vm.stack[argBase:argBase+argc:argBase+argc], isNew, newTarget)
			vm.sp = saved
			regs = vm.reload(regs, onHeap, base, fp)
			regs[ret] = r
			continue
		}

	tailCall:
		// A call in tail position, whose arguments are on the stack from argBase on.
		vm.frames[len(vm.frames)-1].pc = pc
		if f, callThis, argBase, argc = vm.unbind(f, callThis, argBase, argc); !vm.replaceFrame(f, callThis, argBase, argc) {
			goto call
		}
		{
			fr := &vm.frames[len(vm.frames)-1]
			fp = fr.fp
			if fp.realm != vm.curRealm {
				vm.switchRealm(fp.realm)
			}
			cl = f
			code = fp.code
			base = fr.base
			regs = vm.stack[base : base+fp.nregs+1]
			pc = 0
			continue
		}

	construct:
		// new f(...) for anything but a plain constructor function.
		{
			vm.frames[len(vm.frames)-1].pc = pc
			if f == nil || f.fn == nil || !vm.IsConstructor(f) {
				vm.throwNotConstructor(fp, pc-1, regs)
			}
			vm.ensureStack(argBase + argc)
			saved := vm.sp
			vm.sp = argBase + argc
			r := vm.Construct(f, vm.stack[argBase:argBase+argc:argBase+argc], f)
			vm.sp = saved
			regs = vm.reload(regs, onHeap, base, fp)
			regs[ret] = Value{o: r}
			continue
		}

	superCall:
		// super(...): f is the running constructor and newTarget its new.target.
		{
			vm.frames[len(vm.frames)-1].pc = pc
			parent := f.shape.proto
			if parent == nil || parent.fn == nil || !vm.IsConstructor(parent) {
				vm.ThrowTypeError("super constructor is not a constructor")
			}
			vm.ensureStack(argBase + argc)
			saved := vm.sp
			vm.sp = argBase + argc
			r := vm.Construct(parent, vm.stack[argBase:argBase+argc:argBase+argc], newTarget)
			vm.sp = saved
			regs = vm.reload(regs, onHeap, base, fp)
			regs[ret] = Value{o: r}
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
			if fp.realm != vm.curRealm {
				vm.switchRealm(fp.realm)
			}
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

// unbind returns the function that a tail call of f ends up calling, with its this value and
// the stack index and count of its arguments, for the argc arguments at argBase on the stack.
// A bound function stands for its target, with the bound arguments inserted before the others,
// Function.prototype.call of the running realm for its callable this value, and
// Function.prototype.apply of the running realm for its callable this value when its argument
// list can be made without running JavaScript.
func (vm *VM) unbind(f *Object, this Value, argBase, argc int) (*Object, Value, int, int) {
	for f != nil && f.fn != nil {
		if b := f.fn.bound; b != nil {
			if k := len(b.args); k > 0 {
				vm.ensureStack(argBase + argc + k)
				copy(vm.stack[argBase+k:], vm.stack[argBase:argBase+argc])
				copy(vm.stack[argBase:], b.args)
				argc += k
			}
			f, this = b.target, b.this
			continue
		}
		if this.o == nil || this.o.fn == nil {
			break
		}
		// Function.prototype.call and apply end in a tail call of their this value.
		switch f {
		case vm.Intrinsic(IntrinsicFunctionPrototypeCall):
			f, this = this.o, Undefined
			if argc > 0 {
				this = vm.stack[argBase]
				argBase++
				argc--
			}
			continue
		case vm.Intrinsic(IntrinsicFunctionPrototypeApply):
			var list []Value
			if argc > 1 {
				if a := vm.stack[argBase+1]; !a.IsNullish() {
					var ok bool
					if list, ok = a.o.plainElems(); !ok {
						return f, this, argBase, argc
					}
				}
			}
			f, this = this.o, Undefined
			if argc > 0 {
				this = vm.stack[argBase]
			}
			// The list goes above the arguments, which may be in the registers of the running frame.
			argBase = max(vm.sp, argBase+argc)
			argc = len(list)
			vm.ensureStack(argBase + argc)
			copy(vm.stack[argBase:], list)
			continue
		}
		break
	}
	return f, this, argBase, argc
}

// replaceFrame replaces the running frame with a frame of f for the argc arguments at argBase on
// the stack, which returns to the caller of the running frame. It reports false, changing nothing,
// when f is not an ordinary function written in JavaScript or the running frame constructs an object.
func (vm *VM) replaceFrame(f *Object, this Value, argBase, argc int) bool {
	n := len(vm.frames) - 1
	cur := &vm.frames[n]
	if f == nil || f.fn == nil || cur.isNew || cur.newTarget != nil {
		return false
	}
	if c := f.fn.code; c == nil || c.isGenerator || c.isAsync || f.fn.classConstructor {
		return false
	}
	if vm.interrupt.Load() {
		vm.Poll()
	}
	// The arguments go where the running frame's were.
	start := cur.base
	if cur.extraBase >= 0 {
		start = cur.extraBase - cur.fp.nparams
	}
	if len(vm.openUpvals) > 0 {
		vm.closeUpvals(start)
	}
	end := max(vm.sp, argBase+argc)
	copy(vm.stack[start:], vm.stack[argBase:argBase+argc])
	ret := cur.ret
	*cur = frame{}
	vm.frames = vm.frames[:n]
	vm.pushFrame(f, this, start, argc, ret, false, nil)
	// Nothing above the new frame is in use any more.
	if vm.sp < end {
		clear(vm.stack[vm.sp:end])
	}
	return true
}

// reload returns the registers of the running frame after a call that may have moved the stack.
func (vm *VM) reload(regs []Value, onHeap bool, base int, fp *funcProto) []Value {
	if onHeap {
		return regs
	}
	return vm.stack[base : base+fp.nregs+1]
}

// NumberPow implements Number::exponentiate.
func NumberPow(x, y float64) float64 {
	// The exponents NaN and ±Infinity with a base of ±1 give NaN, unlike in IEEE 754.
	if y != y || (math.IsInf(y, 0) && (x == 1 || x == -1)) {
		return math.NaN()
	}
	return math.Pow(x, y)
}
