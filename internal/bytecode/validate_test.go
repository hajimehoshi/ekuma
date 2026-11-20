// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package bytecode_test

import (
	"errors"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/srcpos"
)

// validFunc returns a valid function, which the cases of TestValidate break.
func validFunc() *bytecode.Func {
	return &bytecode.Func{
		Name: "f",
		File: "test.js",
		Code: []bytecode.Instr{
			{Op: bytecode.OpLoadInt, A: 0, B: 1},
			{Op: bytecode.OpJumpIfNot, A: 6, B: 0},
			{Op: bytecode.OpGetPropChain, A: 1, B: 0, C: 0},
			{Op: bytecode.OpGetProp, A: 1, B: 1, C: 1},
			{Op: bytecode.OpInstanceof, A: 1, B: 1, C: 0},
			{Op: bytecode.OpExtra, A: 2},
			{Op: bytecode.OpCall, A: 1, B: 1, C: 1},
			{Op: bytecode.OpReturn, A: 1},
		},
		Keys:      []string{"x", "y", "prototype"},
		NumCaches: 3,
		NumRegs:   4,
		Handlers: []bytecode.Handler{
			{
				Start:  2,
				End:    4,
				Target: 7,
				Reg:    1,
			},
			{
				Start:  0,
				End:    6,
				Target: 7,
				Reg:    1,
			},
		},
		Positions: []bytecode.Pos{
			{
				PC:     0,
				Offset: 0,
			},
			{
				PC:     2,
				Offset: 4,
			},
		},
		Lines: srcpos.NewTable("f(); x.y;"),
		Protos: []*bytecode.Func{
			{
				Code: []bytecode.Instr{
					{Op: bytecode.OpReturnUndef},
				},
			},
		},
	}
}

func TestValidate(t *testing.T) {
	testCases := []struct {
		name string

		// breakFunc breaks the valid function f.
		breakFunc func(f *bytecode.Func)

		// err is the error, and pc the instruction, that Validate reports; nested says that
		// the error is in the nested function.
		err    error
		pc     int
		nested bool
	}{
		{
			name:      "valid",
			breakFunc: func(f *bytecode.Func) {},
		},
		{
			name: "register",
			breakFunc: func(f *bytecode.Func) {
				f.Code[3].A = 4
			},
			err: bytecode.ErrRegister,
			pc:  3,
		},
		{
			name: "call arguments past the registers",
			breakFunc: func(f *bytecode.Func) {
				f.Code[6].C = 2
			},
			err: bytecode.ErrRegister,
			pc:  6,
		},
		{
			name: "key",
			breakFunc: func(f *bytecode.Func) {
				f.Code[3].C = 3
			},
			err: bytecode.ErrIndex,
			pc:  3,
		},
		{
			name: "flag",
			breakFunc: func(f *bytecode.Func) {
				f.Code[1] = bytecode.Instr{Op: bytecode.OpJumpIfNullish, A: 6, B: 0, C: 2}
			},
			err: bytecode.ErrValue,
			pc:  1,
		},
		{
			name: "unused operand",
			breakFunc: func(f *bytecode.Func) {
				f.Code[7].B = 1
			},
			err: bytecode.ErrValue,
			pc:  7,
		},
		{
			name: "jump to an extra",
			breakFunc: func(f *bytecode.Func) {
				f.Code[1].A = 5
			},
			err: bytecode.ErrTarget,
			pc:  1,
		},
		{
			name: "extra without an instruction",
			breakFunc: func(f *bytecode.Func) {
				f.Code[4] = bytecode.Instr{Op: bytecode.OpStrictEq, A: 1, B: 1, C: 0}
			},
			err: bytecode.ErrExtra,
			pc:  5,
		},
		{
			name: "RegExp argument outside the call",
			breakFunc: func(f *bytecode.Func) {
				f.RegExps = []bytecode.RegExp{
					{
						Pattern: "a",
					},
				}
				f.Code[0] = bytecode.Instr{Op: bytecode.OpRegExpArg, A: 3, B: 0, C: 0}
			},
			err: bytecode.ErrValue,
			pc:  0,
		},
		{
			name: "constant run past the constants",
			breakFunc: func(f *bytecode.Func) {
				f.Consts = []bytecode.Const{
					{
						Kind: bytecode.ConstNull,
					},
				}
				f.Code[0] = bytecode.Instr{Op: bytecode.OpNewArrayK, A: 0, B: 0, C: 2}
			},
			err: bytecode.ErrIndex,
			pc:  0,
		},
		{
			name: "chain without its next read",
			breakFunc: func(f *bytecode.Func) {
				f.Code[3].B = 0
			},
			err: bytecode.ErrSequence,
			pc:  2,
		},
		{
			name: "shared site",
			breakFunc: func(f *bytecode.Func) {
				f.Code[3].C = 0
			},
			err: bytecode.ErrSite,
			pc:  3,
		},
		{
			name: "handler after the handler around it",
			breakFunc: func(f *bytecode.Func) {
				f.Handlers[0], f.Handlers[1] = f.Handlers[1], f.Handlers[0]
			},
			err: bytecode.ErrHandler,
			pc:  -1,
		},
		{
			name: "positions out of order",
			breakFunc: func(f *bytecode.Func) {
				f.Positions[1].PC = 0
			},
			err: bytecode.ErrPosition,
			pc:  -1,
		},
		{
			name: "nested function",
			breakFunc: func(f *bytecode.Func) {
				f.Protos[0].Code[0].A = 1
			},
			err:    bytecode.ErrValue,
			pc:     0,
			nested: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := validFunc()
			tc.breakFunc(f)
			err := f.Validate()
			if !errors.Is(err, tc.err) {
				t.Fatalf("Validate: got %v, want %v", err, tc.err)
			}
			if tc.err == nil {
				return
			}
			var e *bytecode.ValidationError
			if !errors.As(err, &e) {
				t.Fatalf("Validate: got %T, want a *bytecode.ValidationError", err)
			}
			if e.PC != tc.pc {
				t.Errorf("PC: got %d, want %d", e.PC, tc.pc)
			}
			if want := f.Protos[0]; (e.Func == want) != tc.nested {
				t.Errorf("Func: got %p, want the nested function %p: %t", e.Func, want, tc.nested)
			}
			if tc.pc >= 0 {
				if got, want := e.Op, e.Func.Code[tc.pc].Op; got != want {
					t.Errorf("Op: got %v, want %v", got, want)
				}
			}
		})
	}
}
