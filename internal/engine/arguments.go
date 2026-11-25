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
	params []*storage.Upvalue
}

// forwardedArguments returns the arguments object shared by forwarding calls in the running frame.
func (cx *ExecutionContext) forwardedArguments(argsReg int32) storage.Value {
	fr := &cx.frames[len(cx.frames)-1]
	regs := cx.frameRegs(fr)
	if regs[argsReg].IsUndefined() {
		// Only functions with a simple parameter list forward their arguments.
		regs[argsReg] = storage.Value{O: cx.newArguments(fr, !fr.fp.Strict)}
	}
	return regs[argsReg]
}

// newArguments returns the arguments object of the frame fr.
// A mapped object links the indices below both the argument count and the parameter count to the parameters.
func (cx *ExecutionContext) newArguments(fr *frame, mapped bool) *storage.Object {
	o := cx.NewArgumentsObject(cx.frameRegs(fr)[:min(fr.argc, fr.fp.Nparams)], cx.extraArgs(fr))
	if !mapped {
		if cx.realm.StrictCallee.O == nil {
			// An accessor pair is never changed in place, so the objects share it.
			throwTypeError := cx.Intrinsic(storage.IntrinsicThrowTypeError)
			cx.realm.StrictCallee = storage.NewAccessor(throwTypeError, throwTypeError)
		}
		cx.Add(o, cx.Key(storage.KeyCallee), cx.realm.StrictCallee, storage.FlagAccessor)
		return o
	}
	if n := min(fr.argc, fr.fp.Nparams); n > 0 {
		m := &argumentsMap{
			params: make([]*storage.Upvalue, n),
		}
		shadowed := fr.fp.ShadowedParams
		for i := range n {
			if i < len(shadowed) && shadowed[i] {
				continue
			}
			m.params[i] = cx.captureRegister(fr, i)
			cx.AddMappedElement(o, uint32(i))
		}
		o.SetExtState(m)
	}
	cx.Add(o, cx.Key(storage.KeyCallee), storage.Value{O: fr.cl}, storage.FlagsHidden)
	return o
}

// mappedParam returns the storage of the parameter that the index property i of o is mapped to,
// or nil when o is not an arguments object that maps i.
func mappedParam(o *storage.Object, i uint32) *storage.Upvalue {
	if o.Class != storage.ClassArguments {
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
func (cx *ExecutionContext) defineMappedArgument(o *storage.Object, i uint32, u *storage.Upvalue, d storage.PropDesc) bool {
	m := o.ExtState().(*argumentsMap)
	// The ordinary definition validates against the value in the slot.
	p, _ := o.Shape().Lookup(cx.IndexName(i))
	o.StoreMapped(p, *u.P)
	readOnly := d.HasFalse(storage.HasWritable, storage.FlagWritable)
	if d.IsData() && !d.HasField(storage.HasValue) && readOnly {
		d.SetValue(*u.P)
	}
	m.params[i] = nil
	if !cx.defineOwnProperty(o, storage.IndexKey(i), d) {
		m.params[i] = u
		o.StoreMapped(p, storage.Undefined)
		return false
	}
	if d.IsAccessor() {
		return true
	}
	if d.HasField(storage.HasValue) {
		*u.P = d.Value()
	}
	if !readOnly {
		m.params[i] = u
		o.StoreMapped(p, storage.Undefined)
	}
	return true
}

// deleteMappedArgument implements [[Delete]] of an arguments exotic object for the index i,
// which is mapped to a parameter.
func (cx *ExecutionContext) deleteMappedArgument(o *storage.Object, i uint32) bool {
	key := cx.IndexName(i)
	if p, _ := o.Shape().Lookup(key); p.Flags&storage.FlagConfigurable == 0 {
		return false
	}
	cx.Remove(o, key)
	o.ExtState().(*argumentsMap).params[i] = nil
	return true
}
