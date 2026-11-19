// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package bytecode_test

import (
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/srcpos"
)

// TestDisassemble tests the text of a small function.
func TestDisassemble(t *testing.T) {
	src := "function f(o) {\n  return o.x;\n}"
	f := &bytecode.Func{
		Name: "f",
		File: "test.js",
		Code: []bytecode.Instr{
			{Op: bytecode.OpLoadK, A: 1, B: 0},
			{Op: bytecode.OpGetProp, A: 2, B: 0, C: 0},
			{Op: bytecode.OpJumpIfNot, A: 6, B: 2},
			{Op: bytecode.OpNew, A: 2, B: 2, C: 0},
			{Op: bytecode.OpExtra, A: 1},
			{Op: bytecode.OpClosure, A: 1, B: 0},
			{Op: bytecode.OpReturn, A: 1},
			{Op: bytecode.OpReturnUndef},
		},
		Consts: []bytecode.Const{
			{
				Kind: bytecode.ConstString,
				Str:  "a\n",
			},
		},
		Keys:      []string{"x", "prototype"},
		NumCaches: 2,
		Handlers: []bytecode.Handler{
			{
				Start:   1,
				End:     5,
				Target:  6,
				Reg:     1,
				Finally: true,
				Scopes:  1,
			},
		},
		Protos: []*bytecode.Func{
			{
				Name: "g",
				File: "test.js",
				Code: []bytecode.Instr{
					{Op: bytecode.OpGetUpval, A: 0, B: 0},
					{Op: bytecode.OpReturn, A: 0},
				},
				Upvals: []bytecode.UpvalDesc{
					{
						Local:   true,
						Index:   1,
						ByValue: true,
					},
				},
				NumRegs: 1,
			},
		},
		Positions: []bytecode.Pos{
			{
				PC:     0,
				Offset: 0,
			},
			{
				PC:     1,
				Offset: 25,
			},
		},
		Lines:     srcpos.NewTable(src),
		NumParams: 1,
		NumRegs:   3,
	}
	want := `function f (test.js): 3 registers, 1 parameter
     0  1:1   OpLoadK        r1, "a\n"
     1  2:10  OpGetProp      r2, r0, "x"
     2        OpJumpIfNot    ->6, r2
     3        OpNew          r2, r2, 0, "prototype"
     5        OpClosure      r1, [0] g
     6        OpReturn       r1
     7        OpReturnUndef
  handler [1, 5) -> 6, r1, finally, 1 scope

function g [0] (test.js): 1 register, 0 parameters
  upvalue u0 = r1, by value
     0  OpGetUpval  r0, u0
     1  OpReturn    r0
`
	var b strings.Builder
	if err := f.Disassemble(&b); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestDisassembleOpcodes tests that an instruction of every opcode disassembles as one line
// with its name, given operands in range.
func TestDisassembleOpcodes(t *testing.T) {
	f := &bytecode.Func{
		Consts: []bytecode.Const{
			{
				Kind: bytecode.ConstNumber,
			},
		},
		Keys:      []string{"k"},
		NumCaches: 1,
		Upvals:    []bytecode.UpvalDesc{{}},
		Protos:    []*bytecode.Func{{}},
		Templates: []bytecode.Template{
			{
				Raw: []string{""},
			},
		},
		RegExps:    []bytecode.RegExp{{}},
		EvalScopes: []bytecode.EvalScope{{}},
		NumRegs:    1,
	}
	var ops []bytecode.Opcode
	for op := bytecode.Opcode(0); op <= bytecode.OpExtra; op++ {
		ops = append(ops, op)
		f.Code = append(f.Code, bytecode.Instr{Op: op})
		if op.Operands().Extra {
			f.Code = append(f.Code, bytecode.Instr{Op: bytecode.OpExtra})
		}
	}
	var b strings.Builder
	if err := f.Disassemble(&b); err != nil {
		t.Fatal(err)
	}
	// The lines of the instructions come after the header and the upvalue, and before the
	// nested function.
	lines := strings.Split(b.String(), "\n")[2:]
	for i, op := range ops {
		if i >= len(lines) {
			t.Fatalf("%d lines for %d opcodes", i, len(ops))
		}
		line := lines[i]
		if fields := strings.Fields(line); len(fields) < 2 || fields[1] != op.String() {
			t.Errorf("line %q for %s", line, op)
		}
		if strings.Contains(line, "?") {
			t.Errorf("operand out of range: %q", line)
		}
	}
	if len(lines) <= len(ops) || lines[len(ops)] != "" {
		t.Errorf("more instruction lines than opcodes:\n%s", b.String())
	}
}
