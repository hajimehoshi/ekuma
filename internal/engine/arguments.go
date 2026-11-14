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
	o := &Object{
		kind:  kindObject,
		class: classArguments,
		shape: vm.shapes.arguments,
		elems: vm.frameArgs(fr, regs),
	}
	o.slots = []Value{Number(float64(fr.argc)), {o: vm.FnArrayValues}}
	if !mapped {
		vm.add(o, vm.K.callee, newAccessor(vm.ThrowTypeErrorFunc, vm.ThrowTypeErrorFunc), FlagAccessor)
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
	vm.add(o, vm.K.callee, Value{o: fr.cl}, FlagsHidden)
	return o
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
	readOnly := d.Has&HasWritable != 0 && d.Flags&FlagWritable == 0
	if d.IsData() && d.Has&HasValue == 0 && readOnly {
		d.Value = *u.p
		d.Has |= HasValue
	}
	m.params[i] = nil
	if !vm.defineOwnProperty(o, PropKey{idx: i}, d) {
		m.params[i] = u
		return false
	}
	if d.IsAccessor() {
		return true
	}
	if d.Has&HasValue != 0 {
		*u.p = d.Value
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
