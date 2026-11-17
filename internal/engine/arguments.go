// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// argumentsMap is the parameter map of a mapped arguments object.
type argumentsMap struct {
	// params holds, by index, the storage of the parameter that the index property is mapped to,
	// or nil for a property that is not mapped.
	params []*upvalue
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
		o.ext = m
	}
	vm.add(o, vm.Key(keyCallee), Value{o: fr.cl}, FlagsHidden)
	return o
}

// newArgumentsObject returns an arguments object without callee whose elements are the arguments
// of the frame fr, whose registers are regs.
func (vm *VM) newArgumentsObject(fr *frame, regs []Value) *Object {
	a := &argumentsObject{
		kind:    kindObject,
		class:   classArguments,
		shape:   vm.shapes.arguments,
		elems:   vm.frameArgs(fr, regs),
		storage: [3]Value{Number(float64(fr.argc)), {o: vm.Intrinsic(IntrinsicArrayPrototypeValues)}},
	}
	a.slots = a.storage[:2]
	return a.init()
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
	m, _ := o.ext.(*argumentsMap)
	if m == nil || int64(i) >= int64(len(m.params)) {
		return nil
	}
	return m.params[i]
}

// defineMappedArgument implements [[DefineOwnProperty]] of an arguments exotic object
// for the index i, which is mapped to the parameter u.
func (vm *VM) defineMappedArgument(o *Object, i uint32, u *upvalue, d PropDesc) bool {
	m := o.ext.(*argumentsMap)
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
	o.ext.(*argumentsMap).params[i] = nil
	return true
}
