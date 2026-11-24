// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package bytecode

import (
	"errors"
	"fmt"
)

// The errors that [Func.Validate] reports in a [ValidationError].
var (
	// ErrOpcode is an opcode that is unknown or invalid for the function.
	ErrOpcode = errors.New("bytecode: invalid opcode")

	// ErrRegister is a register outside the registers of the function.
	ErrRegister = errors.New("bytecode: register out of range")

	// ErrIndex is an index outside the elements of the function that it indexes.
	ErrIndex = errors.New("bytecode: index out of range")

	// ErrValue is an operand with a value that its kind does not allow.
	ErrValue = errors.New("bytecode: invalid operand")

	// ErrTarget is a jump target outside the code or at an [OpExtra].
	ErrTarget = errors.New("bytecode: invalid jump target")

	// ErrExtra is an [OpExtra] that no instruction takes, or an instruction without its OpExtra.
	ErrExtra = errors.New("bytecode: misplaced OpExtra")

	// ErrSequence is an instruction without the instruction that has to follow it.
	ErrSequence = errors.New("bytecode: invalid instruction sequence")

	// ErrSite is a site or a global cache that two instructions share.
	ErrSite = errors.New("bytecode: shared site")

	// ErrHandler is an element of [Func.Handlers] outside the code or out of order.
	ErrHandler = errors.New("bytecode: invalid handler")

	// ErrPosition is an element of [Func.Positions] outside the code or out of order.
	ErrPosition = errors.New("bytecode: invalid position")
)

// ValidationError is an error that [Func.Validate] reports.
type ValidationError struct {
	// Func is the function with the error, which is the validated one or one nested in it.
	Func *Func

	// PC is the index of the instruction in [Func.Code], or -1 for an error outside the code.
	PC int

	// Op is the opcode at PC, when PC is not -1.
	Op Opcode

	// Err is one of the errors that Validate reports, such as [ErrRegister].
	Err error

	detail string
}

func (e *ValidationError) Error() string {
	name := e.Func.Name
	if name == "" {
		name = "<anonymous>"
	}
	where := fmt.Sprintf("function %s of %s", name, e.Func.File)
	if e.PC >= 0 {
		where += fmt.Sprintf(" at %d (%s)", e.PC, e.Op)
	}
	return fmt.Sprintf("%v: %s: %s", e.Err, where, e.detail)
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

// Validate reports whether the code of f and of the functions nested in it uses its operands as
// [Opcode.Operands] describes them. It returns nil or a [*ValidationError].
func (f *Func) Validate() error {
	return validate(f, nil)
}

func validate(f, parent *Func) error {
	if err := validateFunc(f, parent); err != nil {
		return err
	}
	for _, p := range f.Protos {
		if err := validate(p, f); err != nil {
			return err
		}
	}
	return nil
}

// validator validates the code of one function.
type validator struct {
	f *Func

	// extra marks the indices in f.Code of the OpExtra words that follow their instructions.
	extra []bool

	// siteUsers maps a site to the index of the instruction that uses it.
	siteUsers map[int32]int

	// globalCacheUsers maps a global cache to the index of the instruction that uses it.
	globalCacheUsers map[int32]int
}

func validateFunc(f, parent *Func) error {
	v := &validator{
		f:     f,
		extra: make([]bool, len(f.Code)),
	}
	if err := v.funcFields(parent); err != nil {
		return err
	}
	for pc := 0; pc < len(f.Code); pc++ {
		op := f.Code[pc].Op
		if int(op) >= len(opcodes) || opcodes[op].name == "" {
			return v.errorf(pc, ErrOpcode, "opcode %d", op)
		}
		if op == OpExtra {
			return v.errorf(pc, ErrExtra, "no instruction takes it")
		}
		if !opcodes[op].operands.Extra {
			continue
		}
		if pc+1 >= len(f.Code) || f.Code[pc+1].Op != OpExtra {
			return v.errorf(pc, ErrExtra, "the instruction is not followed by its OpExtra")
		}
		pc++
		v.extra[pc] = true
	}
	for pc, in := range f.Code {
		if v.extra[pc] {
			continue
		}
		if err := v.instr(pc, in); err != nil {
			return err
		}
	}
	if err := v.handlers(); err != nil {
		return err
	}
	return v.positions()
}

func (v *validator) errorf(pc int, err error, format string, args ...any) error {
	e := &ValidationError{
		Func:   v.f,
		PC:     pc,
		Err:    err,
		detail: fmt.Sprintf(format, args...),
	}
	if pc >= 0 {
		e.Op = v.f.Code[pc].Op
	}
	return e
}

// funcFields validates the fields of the function other than its code.
func (v *validator) funcFields(parent *Func) error {
	f := v.f
	if f.NumParams < 0 || f.NumParams > f.NumRegs {
		return v.errorf(-1, ErrRegister, "%d parameters with %d registers", f.NumParams, f.NumRegs)
	}
	if f.NumClear < 0 || f.NumClear > f.NumRegs {
		return v.errorf(-1, ErrRegister, "%d registers to clear with %d registers", f.NumClear, f.NumRegs)
	}
	if f.NumCaches < 0 || f.NumCaches > len(f.Keys) {
		return v.errorf(-1, ErrIndex, "%d inline caches with %d keys", f.NumCaches, len(f.Keys))
	}
	if f.NumGlobalCaches < 0 {
		return v.errorf(-1, ErrIndex, "negative global cache count %d", f.NumGlobalCaches)
	}
	if parent == nil {
		return nil
	}
	for i, u := range f.Upvals {
		switch {
		case u.Local && (u.Index < 0 || int(u.Index) >= parent.NumRegs):
			return v.errorf(-1, ErrRegister, "upvalue %d is register %d of a function with %d registers", i, u.Index, parent.NumRegs)
		case !u.Local && (u.Index < 0 || int(u.Index) >= len(parent.Upvals)):
			return v.errorf(-1, ErrIndex, "upvalue %d is upvalue %d of a function with %d upvalues", i, u.Index, len(parent.Upvals))
		case u.ByValue && !u.Local:
			return v.errorf(-1, ErrValue, "upvalue %d is by value but not local", i)
		}
	}
	return nil
}

// operand returns the operand field of the instruction in, followed by x when it takes an
// OpExtra.
func operand(in, x Instr, field Field) int32 {
	switch field {
	case FieldA:
		return in.A
	case FieldB:
		return in.B
	case FieldC:
		return in.C
	case FieldXA:
		return x.A
	default:
		return x.B
	}
}

// kind returns the kind of the operand field in ops.
func (ops *Operands) kind(field Field) OperandKind {
	switch field {
	case FieldA:
		return ops.A
	case FieldB:
		return ops.B
	case FieldC:
		return ops.C
	case FieldXA:
		return ops.XA
	default:
		return ops.XB
	}
}

var fieldNames = [...]string{
	FieldA:  "a",
	FieldB:  "b",
	FieldC:  "c",
	FieldXA: "x.a",
	FieldXB: "x.b",
}

// instr validates the instruction in at pc.
func (v *validator) instr(pc int, in Instr) error {
	ops := &opcodes[in.Op].operands
	var x Instr
	if ops.Extra {
		x = v.f.Code[pc+1]
		if x.C != 0 {
			return v.errorf(pc, ErrValue, "x.c is %d, not 0", x.C)
		}
	}
	for field := FieldA; field <= FieldXB; field++ {
		if !ops.Extra && field >= FieldXA {
			break
		}
		if err := v.operand(pc, ops.kind(field), field, operand(in, x, field)); err != nil {
			return err
		}
	}
	for _, r := range ops.Reads {
		if err := v.regs(pc, in, x, ops, r); err != nil {
			return err
		}
	}
	for _, r := range ops.Writes {
		if err := v.regs(pc, in, x, ops, r); err != nil {
			return err
		}
	}
	return v.special(pc, in)
}

// operand validates the value of the operand field of the kind k of the instruction at pc.
func (v *validator) operand(pc int, k OperandKind, field Field, val int32) error {
	f := v.f
	name := fieldNames[field]
	inRange := func(n int, what string) error {
		if val < 0 || int(val) >= n {
			return v.errorf(pc, ErrIndex, "%s is %d with %d %s", name, val, n, what)
		}
		return nil
	}
	inEnum := func(max int32) error {
		if val < 0 || val > max {
			return v.errorf(pc, ErrValue, "%s is %d, not from 0 to %d", name, val, max)
		}
		return nil
	}
	switch k {
	case OperandUnused:
		if val != 0 {
			return v.errorf(pc, ErrValue, "%s is unused but %d", name, val)
		}
	case OperandReg:
		if val < 0 || int(val) >= f.NumRegs {
			return v.errorf(pc, ErrRegister, "%s is %d with %d registers", name, val, f.NumRegs)
		}
	case OperandOptReg:
		if val < -1 || int(val) >= f.NumRegs {
			return v.errorf(pc, ErrRegister, "%s is %d with %d registers", name, val, f.NumRegs)
		}
	case OperandConst:
		return inRange(len(f.Consts), "constants")
	case OperandConstRun:
		n := f.Code[pc].C
		if val < 0 || n < 0 || int64(val)+int64(n) > int64(len(f.Consts)) {
			return v.errorf(pc, ErrIndex, "%s is %d, the start of a run of %d with %d constants", name, val, n, len(f.Consts))
		}
	case OperandKey:
		return inRange(len(f.Keys), "keys")
	case OperandSite:
		if err := inRange(min(len(f.Keys), f.NumCaches), "sites"); err != nil {
			return err
		}
		if v.siteUsers == nil {
			v.siteUsers = map[int32]int{}
		}
		if other, ok := v.siteUsers[val]; ok {
			return v.errorf(pc, ErrSite, "%s is the site %d of the instruction at %d too", name, val, other)
		}
		v.siteUsers[val] = pc
	case OperandGlobalCache:
		if err := inRange(f.NumGlobalCaches, "global caches"); err != nil {
			return err
		}
		if v.globalCacheUsers == nil {
			v.globalCacheUsers = map[int32]int{}
		}
		if other, ok := v.globalCacheUsers[val]; ok {
			return v.errorf(pc, ErrSite, "%s is the global cache %d of the instruction at %d too", name, val, other)
		}
		v.globalCacheUsers[val] = pc
	case OperandTarget:
		if val < 0 || int(val) >= len(f.Code) {
			return v.errorf(pc, ErrTarget, "%s is %d with %d instructions", name, val, len(f.Code))
		}
		if v.extra[val] {
			return v.errorf(pc, ErrTarget, "%s is %d, an OpExtra", name, val)
		}
	case OperandInt:
	case OperandCount:
		if val < 0 {
			return v.errorf(pc, ErrValue, "%s is the count %d", name, val)
		}
	case OperandFlag:
		return inEnum(1)
	case OperandUpval:
		return inRange(len(f.Upvals), "upvalues")
	case OperandProto:
		return inRange(len(f.Protos), "prototypes")
	case OperandRegExp:
		return inRange(len(f.RegExps), "regular expressions")
	case OperandTemplate:
		return inRange(len(f.Templates), "templates")
	case OperandEvalScope:
		return inRange(len(f.EvalScopes), "eval scopes")
	case OperandThrow:
		return inEnum(ThrowReferenceError)
	case OperandDecl:
		return inEnum(DeclFunction)
	case OperandIter:
		return inEnum(IterSyncNext)
	case OperandElem:
		base := val &^ (ElemStatic | ElemValue)
		if val < 0 || base > ElemStaticBlock || val&ElemValue != 0 && base != ElemField {
			return v.errorf(pc, ErrValue, "%s is the Elem kind %d", name, val)
		}
	case OperandDispose:
		return inEnum(DisposeAsync)
	case OperandNamePrefix:
		return inEnum(NamePrefixSet)
	case OperandPrim:
		return inEnum(PrimTrue)
	case OperandGlobalVar:
		return inEnum(GlobalVarFunc | GlobalVarDeletable)
	default:
		panic("bytecode: unknown OperandKind")
	}
	return nil
}

// regs validates the run of registers r of the instruction in at pc, followed by x.
func (v *validator) regs(pc int, in, x Instr, ops *Operands, r Regs) error {
	start := operand(in, x, r.At)
	if start == -1 && ops.kind(r.At) == OperandOptReg {
		return nil
	}
	first := int(start) + r.Offset
	n := r.Len
	switch {
	case r.Len == RegsToEnd:
		n = v.f.NumRegs - first
	case r.Counted:
		n += ops.count(in, x)
	}
	if n < 0 || first < 0 || first+n > v.f.NumRegs {
		return v.errorf(pc, ErrRegister, "%d registers from %d with %d registers", n, first, v.f.NumRegs)
	}
	return nil
}

// special validates what is particular to the instruction in at pc.
func (v *validator) special(pc int, in Instr) error {
	code := v.f.Code
	switch in.Op {
	case OpGetArgsIndex:
		if v.f.IsGenerator || v.f.IsAsync {
			return v.errorf(pc, ErrOpcode, "indexed arguments read requires a stack frame with the surplus arguments")
		}
	case OpGetPropChain:
		// The instruction may run the next one.
		if pc+1 >= len(code) || code[pc+1].Op != OpGetProp && code[pc+1].Op != OpGetPropChain || code[pc+1].B != in.A {
			return v.errorf(pc, ErrSequence, "not followed by an OpGetProp of a")
		}
	case OpCopyPropToGlobal:
		if int(in.C) < pc+3 {
			return v.errorf(pc, ErrSequence, "missing global property assignment")
		}
		get, set := code[pc+1], code[in.C-1]
		if in.A == in.B || get.Op != OpGetProp || get.A != in.A+1 || get.B != in.B ||
			(set.Op != OpSetGlobal && set.Op != OpSetGlobalStrict) || set.A != get.A {
			return v.errorf(pc, ErrSequence, "fallback must read the source property and store it to the global")
		}
	case OpIterNextPair:
		if pc+2 >= len(code) || code[pc+2].Op != OpIterNext || code[pc+2].B != in.B {
			return v.errorf(pc, ErrSequence, "not followed by an OpIterNext of b")
		}
	case OpRegExpArg:
		// The RegExp object is the this value or the first argument of the call.
		if in.A != in.C+1 && in.A != in.C+2 {
			return v.errorf(pc, ErrValue, "a is %d for the call of %d", in.A, in.C)
		}
	}
	return nil
}

// boundary reports whether pc is the index of an instruction or the end of the code.
func (v *validator) boundary(pc int32) bool {
	return pc >= 0 && int(pc) <= len(v.f.Code) && (int(pc) == len(v.f.Code) || !v.extra[pc])
}

func (v *validator) handlers() error {
	f := v.f
	for i, h := range f.Handlers {
		switch {
		case !v.boundary(h.Start) || !v.boundary(h.End) || h.Start > h.End:
			return v.errorf(-1, ErrHandler, "handler %d covers %d to %d of %d instructions", i, h.Start, h.End, len(f.Code))
		case !v.boundary(h.Target) || int(h.Target) == len(f.Code):
			return v.errorf(-1, ErrHandler, "handler %d jumps to %d", i, h.Target)
		case h.Reg < 0 || int(h.Reg) >= f.NumRegs:
			return v.errorf(-1, ErrRegister, "handler %d receives the exception in register %d with %d registers", i, h.Reg, f.NumRegs)
		case h.Scopes < 0:
			return v.errorf(-1, ErrHandler, "handler %d keeps %d scopes", i, h.Scopes)
		}
		// The first handler that covers an instruction catches its exceptions, so an inner
		// handler comes before the handlers around it.
		for j, g := range f.Handlers[:i] {
			if g.Start < h.End && h.Start < g.End && (g.Start < h.Start || g.End > h.End) {
				return v.errorf(-1, ErrHandler, "handler %d covers %d to %d, which overlaps handler %d from %d to %d without being in it", j, g.Start, g.End, i, h.Start, h.End)
			}
		}
	}
	return nil
}

func (v *validator) positions() error {
	f := v.f
	if len(f.Positions) > 0 && f.Lines == nil {
		return v.errorf(-1, ErrPosition, "%d positions without lines", len(f.Positions))
	}
	for i, p := range f.Positions {
		switch {
		case !v.boundary(p.PC) || int(p.PC) == len(f.Code):
			return v.errorf(-1, ErrPosition, "position %d is at %d of %d instructions", i, p.PC, len(f.Code))
		case i > 0 && p.PC <= f.Positions[i-1].PC:
			return v.errorf(-1, ErrPosition, "position %d is at %d, after position %d at %d", i, p.PC, i-1, f.Positions[i-1].PC)
		case p.Offset < 0:
			return v.errorf(-1, ErrPosition, "position %d has the offset %d", i, p.Offset)
		}
	}
	return nil
}
