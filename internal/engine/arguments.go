// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// argumentsMap is the parameter map of a mapped arguments object.
type argumentsMap struct {
	// params holds, by index, the storage of the parameter that the index property is mapped to,
	// or nil for a property that is not mapped. The slot of a mapped property holds undefined, so
	// that it does not keep a value alive that the parameter no longer has.
	params []*upvalue
}

// forwardedArguments returns the arguments object shared by forwarding calls in the running frame.
func (cx *ExecutionContext) forwardedArguments(argsReg int32) Value {
	fr := &cx.frames[len(cx.frames)-1]
	regs := cx.frameRegs(fr)
	if regs[argsReg].IsUndefined() {
		// Only functions with a simple parameter list forward their arguments.
		regs[argsReg] = Value{O: cx.newArguments(fr, !fr.fp.Strict)}
	}
	return regs[argsReg]
}

// newArguments returns the arguments object of the frame fr.
// A mapped object links the indices below both the argument count and the parameter count to the parameters.
func (cx *ExecutionContext) newArguments(fr *frame, mapped bool) *Object {
	o := cx.NewArgumentsObject(cx.frameRegs(fr)[:min(fr.argc, fr.fp.Nparams)], cx.extraArgs(fr))
	if !mapped {
		if cx.realm.StrictCallee.O == nil {
			// An accessor pair is never changed in place, so the objects share it.
			throwTypeError := cx.Intrinsic(IntrinsicThrowTypeError)
			cx.realm.StrictCallee = storage.NewAccessor(throwTypeError, throwTypeError)
		}
		cx.Add(o, cx.Key(keyCallee), cx.realm.StrictCallee, FlagAccessor)
		return o
	}
	if n := min(fr.argc, fr.fp.Nparams); n > 0 {
		m := &argumentsMap{
			params: make([]*upvalue, n),
		}
		shadowed := fr.fp.ShadowedParams
		for i := range n {
			if i < len(shadowed) && shadowed[i] {
				continue
			}
			m.params[i] = cx.captureRegister(fr, i)
			// A mapped property stays out of elems, so that every access to it takes a path that consults the map.
			cx.Add(o, cx.IndexName(uint32(i)), storage.Undefined, FlagsData)
			o.Elems[i] = storage.Hole
		}
		o.Flags |= objSparse
		o.SetExtState(m)
	}
	cx.Add(o, cx.Key(keyCallee), Value{O: fr.cl}, FlagsHidden)
	return o
}

// mappedParam returns the storage of the parameter that the index property i of o is mapped to,
// or nil when o is not an arguments object that maps i.
func mappedParam(o *Object, i uint32) *upvalue {
	if o.Class != classArguments {
		return nil
	}
	m, _ := o.ExtState().(*argumentsMap)
	if m == nil || int64(i) >= int64(len(m.params)) {
		return nil
	}
	return m.params[i]
}

// defineMappedArgument implements [[DefineOwnProperty]] of an arguments exotic object
// for the index i, which is mapped to the parameter u.
func (cx *ExecutionContext) defineMappedArgument(o *Object, i uint32, u *upvalue, d PropDesc) bool {
	m := o.ExtState().(*argumentsMap)
	// The ordinary definition validates against the value in the slot.
	p, _ := o.Shape.Lookup(cx.IndexName(i))
	o.Slots[p.Slot] = *u.P
	readOnly := d.HasFalse(HasWritable, FlagWritable)
	if d.IsData() && !d.HasField(HasValue) && readOnly {
		d.SetValue(*u.P)
	}
	m.params[i] = nil
	if !cx.defineOwnProperty(o, storage.IndexKey(i), d) {
		m.params[i] = u
		o.Slots[p.Slot] = storage.Undefined
		return false
	}
	if d.IsAccessor() {
		return true
	}
	if d.HasField(HasValue) {
		*u.P = d.Value()
	}
	if !readOnly {
		m.params[i] = u
		o.Slots[p.Slot] = storage.Undefined
	}
	return true
}

// deleteMappedArgument implements [[Delete]] of an arguments exotic object for the index i,
// which is mapped to a parameter.
func (cx *ExecutionContext) deleteMappedArgument(o *Object, i uint32) bool {
	key := cx.IndexName(i)
	if p, _ := o.Shape.Lookup(key); p.Flags&FlagConfigurable == 0 {
		return false
	}
	cx.remove(o, key)
	o.ExtState().(*argumentsMap).params[i] = nil
	return true
}
