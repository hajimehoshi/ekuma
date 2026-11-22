// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package bytecode_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// opcodeNames returns the names of the opcodes as bytecode.go declares them, in order.
func opcodeNames(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "bytecode.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range file.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST || len(g.Specs) == 0 {
			continue
		}
		if typ, ok := g.Specs[0].(*ast.ValueSpec).Type.(*ast.Ident); !ok || typ.Name != "Opcode" {
			continue
		}
		for _, s := range g.Specs {
			for _, n := range s.(*ast.ValueSpec).Names {
				names = append(names, n.Name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("no opcodes found in bytecode.go")
	}
	return names
}

// TestOpcodeTable tests that the table has an entry with a flow for every opcode and no other.
func TestOpcodeTable(t *testing.T) {
	names := opcodeNames(t)
	for i, name := range names {
		op := bytecode.Opcode(i)
		if got := op.String(); got != name {
			t.Errorf("opcode %d: the table has %q, want %q", i, got, name)
		}
		if op.Flow() == 0 {
			t.Errorf("%s: the table has no flow", name)
		}
	}
	if n := len(names); n < 256 {
		if got, want := bytecode.Opcode(n).String(), "Opcode("; !strings.HasPrefix(got, want) {
			t.Errorf("opcode %d past the last one: the table has %q", n, got)
		}
	}
}

// operandsOf returns the kinds of the operands of ops, indexed by Field.
func operandsOf(ops bytecode.Operands) []bytecode.OperandKind {
	return []bytecode.OperandKind{
		bytecode.FieldA:  ops.A,
		bytecode.FieldB:  ops.B,
		bytecode.FieldC:  ops.C,
		bytecode.FieldXA: ops.XA,
		bytecode.FieldXB: ops.XB,
	}
}

func isReg(k bytecode.OperandKind) bool {
	return k == bytecode.OperandReg || k == bytecode.OperandOptReg
}

// TestOperandRegs tests that the runs of registers of every opcode start at its register
// operands, and that a run counts with the count operand of the opcode.
func TestOperandRegs(t *testing.T) {
	for i, name := range opcodeNames(t) {
		ops := bytecode.Opcode(i).Operands()
		kinds := operandsOf(ops)
		if !ops.Extra && (ops.XA != bytecode.OperandUnused || ops.XB != bytecode.OperandUnused) {
			t.Errorf("%s: x.a or x.b without an OpExtra", name)
		}
		counts := 0
		for _, k := range kinds {
			if k == bytecode.OperandCount {
				counts++
			}
		}
		if counts > 1 {
			t.Errorf("%s: %d count operands", name, counts)
		}
		runs := slices.Concat(ops.Reads, ops.Writes)
		counted := false
		for _, r := range runs {
			if !isReg(kinds[r.At]) {
				t.Errorf("%s: a run starts at the operand %d of the kind %d", name, r.At, kinds[r.At])
			}
			if r.Offset < 0 || r.Len < 0 && r.Len != bytecode.RegsToEnd || r.Len == 0 && !r.Counted {
				t.Errorf("%s: the run %+v", name, r)
			}
			counted = counted || r.Counted
		}
		if counted != (counts == 1) {
			t.Errorf("%s: %d count operands, but a counted run is %t", name, counts, counted)
		}
		for field, k := range kinds {
			if isReg(k) && !slices.ContainsFunc(runs, func(r bytecode.Regs) bool { return r.At == bytecode.Field(field) }) {
				t.Errorf("%s: the register operand %d starts no run", name, field)
			}
		}
	}
}

// writesA reports whether ops writes the register a.
func writesA(ops bytecode.Operands) bool {
	return slices.ContainsFunc(ops.Writes, func(r bytecode.Regs) bool {
		return r.At == bytecode.FieldA && r.Offset == 0
	})
}

// TestJumpTargetConvention tests that a jump has its target in a, or in c when it writes a.
func TestJumpTargetConvention(t *testing.T) {
	for i, name := range opcodeNames(t) {
		ops := bytecode.Opcode(i).Operands()
		for field, k := range operandsOf(ops) {
			if k != bytecode.OperandTarget {
				continue
			}
			switch bytecode.Field(field) {
			case bytecode.FieldA:
				if len(ops.Writes) > 0 {
					t.Errorf("%s: the target is in a, but the instruction writes registers", name)
				}
			case bytecode.FieldC:
				if !writesA(ops) {
					t.Errorf("%s: the target is in c, but the instruction does not write a", name)
				}
			default:
				t.Errorf("%s: the target is in the operand %d", name, field)
			}
		}
	}
}

// TestResultConvention tests that an instruction that writes a register writes its value to a,
// and writes no other register than the ones from a on, apart from the frames of callees.
func TestResultConvention(t *testing.T) {
	// alsoOthers are the opcodes that write a and other registers as well.
	alsoOthers := map[string]bool{
		"OpGetSuper":      true,
		"OpPostInc":       true,
		"OpPostDec":       true,
		"OpCallUndef":     true,
		"OpTailCallUndef": true,
		"OpApplyFwd":      true,
		"OpTailApplyFwd":  true,
		"OpCallFwd":       true,
		"OpGetArgsLength": true,
		"OpGetArgsIndex":  true,
		"OpTailCallFwd":   true,
		"OpIterNext":      true,
		"OpIterNextPair":  true,
		"OpIterReturn":    true,
	}
	for i, name := range opcodeNames(t) {
		ops := bytecode.Opcode(i).Operands()
		if len(ops.Writes) == 0 {
			continue
		}
		if !writesA(ops) {
			t.Errorf("%s: writes registers but not a", name)
		}
		others := slices.ContainsFunc(ops.Writes, func(r bytecode.Regs) bool {
			// The frame of a callee takes the place of the registers from its arguments on.
			if r.Len == bytecode.RegsToEnd && r.Offset == 2 {
				return false
			}
			return r.At != bytecode.FieldA
		})
		if others != alsoOthers[name] {
			t.Errorf("%s: writes other registers than a: %t, want %t", name, others, alsoOthers[name])
		}
	}
}

// TestStrictConvention tests that every opcode for strict code has an opcode for other code with
// the same operands, and that no opcode is named for sloppy code.
func TestStrictConvention(t *testing.T) {
	// sloppy are the opcodes for non-strict code whose strict variant has no Strict in its name.
	sloppy := map[string]bool{
		"OpLoadThisSloppy": true,
	}
	names := opcodeNames(t)
	for i, name := range names {
		if strings.HasSuffix(name, "Sloppy") != sloppy[name] {
			t.Errorf("%s: an opcode for sloppy code: %t, want %t", name, !sloppy[name], sloppy[name])
		}
		base, ok := strings.CutSuffix(name, "Strict")
		if !ok {
			continue
		}
		j := slices.Index(names, base)
		if j < 0 {
			t.Errorf("%s: no %s", name, base)
			continue
		}
		if got, want := bytecode.Opcode(i).Operands(), bytecode.Opcode(j).Operands(); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: the operands %+v differ from those of %s, %+v", name, got, base, want)
		}
	}
}
