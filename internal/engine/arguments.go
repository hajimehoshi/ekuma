// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// argumentsMap is the parameter map of a mapped arguments object.
type argumentsMap struct {
	// params holds, by index, the storage of the parameter that the index property is mapped to,
	// or nil for a property that is not mapped.
	params []*upvalue
}

// forwardedArguments returns the arguments object shared by forwarding calls in the running frame.
func (vm *VM) forwardedArguments(regs []Value, argsReg int32) Value {
	if regs[argsReg].IsUndefined() {
		fr := &vm.frames[len(vm.frames)-1]
		// Only functions with a simple parameter list forward their arguments.
		regs[argsReg] = Value{o: vm.newArguments(fr, regs, !fr.fp.strict)}
	}
	return regs[argsReg]
}

// newArguments returns the arguments object of the frame fr, whose registers are regs.
// A mapped object links the indices below both the argument count and the parameter count to the parameters.
func (vm *VM) newArguments(fr *frame, regs []Value, mapped bool) *Object {
	o := vm.newArgumentsObject(fr, regs)
	if !mapped {
		if vm.strictCallee.o == nil {
			// An accessor pair is never changed in place, so the objects share it.
			vm.strictCallee = newAccessor(vm.Intrinsic(IntrinsicThrowTypeError), vm.Intrinsic(IntrinsicThrowTypeError))
		}
		vm.add(o, vm.Key(keyCallee), vm.strictCallee, FlagAccessor)
		return o
	}
	if n := min(fr.argc, fr.fp.nparams); n > 0 {
		m := &argumentsMap{
			params: make([]*upvalue, n),
		}
		shadowed := fr.fp.ShadowedParams
		for i := range n {
			if i < len(shadowed) && shadowed[i] {
				continue
			}
			if fr.gen != nil {
				m.params[i] = fr.gen.findUpval(i)
			} else {
				m.params[i] = vm.findUpval(fr.base + i)
			}
			// A mapped property stays out of elems, so that every access to it takes a path that consults the map.
			vm.add(o, vm.indexName(uint32(i)), o.elems[i], FlagsData)
			o.elems[i] = hole
		}
		o.flags |= objSparse
		o.setExtState(m)
	}
	vm.add(o, vm.Key(keyCallee), Value{o: fr.cl}, FlagsHidden)
	return o
}

// newArgumentsObject returns an arguments object without callee whose elements are the arguments
// of the frame fr, whose registers are regs.
func (vm *VM) newArgumentsObject(fr *frame, regs []Value) *Object {
	var a *argumentsObject
	var elems []Value
	switch fr.argc {
	case 0:
		a, elems = &argumentsObject{}, []Value{}
	case 1:
		w := &argumentsObjectWith[[1]Value]{}
		a, elems = &w.argumentsObject, w.elems[:0]
	case 2:
		w := &argumentsObjectWith[[2]Value]{}
		a, elems = &w.argumentsObject, w.elems[:0]
	case 3:
		w := &argumentsObjectWith[[3]Value]{}
		a, elems = &w.argumentsObject, w.elems[:0]
	case 4:
		w := &argumentsObjectWith[[4]Value]{}
		a, elems = &w.argumentsObject, w.elems[:0]
	default:
		a, elems = &argumentsObject{}, make([]Value, 0, fr.argc)
	}
	n := min(fr.argc, fr.fp.nparams)
	elems = append(elems, regs[:n]...)
	elems = append(elems, vm.extraArgs(fr)...)
	a.kind = kindObject
	a.class = classArguments
	a.shape = vm.shapes.arguments
	a.elems = elems
	a.storage = [3]Value{Number(float64(fr.argc)), {o: vm.Intrinsic(IntrinsicArrayPrototypeValues)}}
	a.slots = a.storage[:2]
	return a.init()
}

// argumentsObjectWith is an arguments object together with the storage S of its elements.
type argumentsObjectWith[S any] struct {
	argumentsObject
	elems S
}

// argumentsObject is an arguments object together with the storage of its length,
// its iterator and its callee, so that creating one is a single allocation.
type argumentsObject struct {
	propertyObject
	storage [3]Value
}

// mappedParam returns the storage of the parameter that the index property i of o is mapped to,
// or nil when o is not an arguments object that maps i.
func mappedParam(o *Object, i uint32) *upvalue {
	if o.class != classArguments {
		return nil
	}
	m, _ := o.extState().(*argumentsMap)
	if m == nil || int64(i) >= int64(len(m.params)) {
		return nil
	}
	return m.params[i]
}

// defineMappedArgument implements [[DefineOwnProperty]] of an arguments exotic object
// for the index i, which is mapped to the parameter u.
func (vm *VM) defineMappedArgument(o *Object, i uint32, u *upvalue, d PropDesc) bool {
	m := o.extState().(*argumentsMap)
	// The stored value is brought up to date, as the ordinary definition validates against it.
	p, _ := o.shape.lookup(vm.indexName(i))
	o.slots[p.slot] = *u.p
	readOnly := d.has&HasWritable != 0 && d.flags&FlagWritable == 0
	if d.IsData() && d.has&HasValue == 0 && readOnly {
		d.value = *u.p
		d.has |= HasValue
	}
	m.params[i] = nil
	if !vm.defineOwnProperty(o, PropKey{idx: i}, d) {
		m.params[i] = u
		return false
	}
	if d.IsAccessor() {
		return true
	}
	if d.has&HasValue != 0 {
		*u.p = d.value
	}
	if !readOnly {
		m.params[i] = u
	}
	return true
}

// deleteMappedArgument implements [[Delete]] of an arguments exotic object for the index i,
// which is mapped to a parameter.
func (vm *VM) deleteMappedArgument(o *Object, i uint32) bool {
	key := vm.indexName(i)
	if p, _ := o.shape.lookup(key); p.flags&FlagConfigurable == 0 {
		return false
	}
	vm.remove(o, key)
	o.extState().(*argumentsMap).params[i] = nil
	return true
}
