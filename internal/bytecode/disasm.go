// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package bytecode

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Disassemble writes the code of f and of the functions nested in it to w as text.
func (f *Func) Disassemble(w io.Writer) error {
	return disassemble(w, f, "", nil)
}

// disassemble writes f and the functions nested in it to w. path is the path of indices into
// [Func.Protos] that leads to f, and buf is scratch space for the text of one function.
func disassemble(w io.Writer, f *Func, path string, buf []byte) error {
	d := disassembler{
		f:    f,
		path: path,
		b:    buf[:0],
	}
	d.function()
	if _, err := w.Write(d.b); err != nil {
		return err
	}
	for i := range f.Protos {
		if err := disassemble(w, f.Protos[i], protoPath(path, int32(i)), d.b); err != nil {
			return err
		}
	}
	return nil
}

// protoPath returns the path of Protos[index] of the function at path.
func protoPath(path string, index int32) string {
	if path == "" {
		return strconv.Itoa(int(index))
	}
	return path + "/" + strconv.Itoa(int(index))
}

// disassembler appends the text of one function to b. An operand that is out of range is
// written as its value followed by a question mark.
type disassembler struct {
	f    *Func
	path string
	b    []byte
}

func (d *disassembler) function() {
	f := d.f
	if d.path != "" {
		d.b = append(d.b, '\n')
	}
	d.b = append(d.b, "function "...)
	d.b = append(d.b, funcName(f)...)
	if d.path != "" {
		d.b = append(d.b, " ["...)
		d.b = append(d.b, d.path...)
		d.b = append(d.b, ']')
	}
	if f.File != "" {
		d.b = append(d.b, " ("...)
		d.b = append(d.b, f.File...)
		d.b = append(d.b, ')')
	}
	d.b = append(d.b, ": "...)
	d.b = appendCount(d.b, f.NumRegs, "register")
	d.b = append(d.b, ", "...)
	d.b = appendCount(d.b, f.NumParams, "parameter")
	d.b = append(d.b, '\n')

	for i, u := range f.Upvals {
		d.b = fmt.Appendf(d.b, "  upvalue u%d = ", i)
		if u.Local {
			d.b = fmt.Appendf(d.b, "r%d", u.Index)
		} else {
			d.b = fmt.Appendf(d.b, "u%d", u.Index)
		}
		if u.ByValue {
			d.b = append(d.b, ", by value"...)
		}
		d.b = append(d.b, '\n')
	}

	d.code()

	for _, h := range f.Handlers {
		d.b = fmt.Appendf(d.b, "  handler [%d, %d) -> %d, ", h.Start, h.End, h.Target)
		d.reg(h.Reg)
		if h.Finally {
			d.b = append(d.b, ", finally"...)
		}
		if h.Scopes != 0 {
			d.b = append(d.b, ", "...)
			d.b = appendCount(d.b, int(h.Scopes), "scope")
		}
		d.b = append(d.b, '\n')
	}
}

func funcName(f *Func) string {
	if f.Name == "" {
		return "<anonymous>"
	}
	return f.Name
}

func appendCount(b []byte, n int, noun string) []byte {
	b = strconv.AppendInt(b, int64(n), 10)
	b = append(b, ' ')
	b = append(b, noun...)
	if n != 1 {
		b = append(b, 's')
	}
	return b
}

// next returns the index of the instruction after the one at pc, skipping its OpExtra.
func (d *disassembler) next(pc int) int {
	code := d.f.Code
	if op := code[pc].Op; int(op) < len(opcodes) && opcodes[op].operands.Extra && pc+1 < len(code) && code[pc+1].Op == OpExtra {
		return pc + 2
	}
	return pc + 1
}

// code appends one line for each instruction, with the source position that starts there.
func (d *disassembler) code() {
	f := d.f

	// The columns are as wide as their widest entry.
	posLabels := make([]string, len(f.Code))
	var posWidth, nameWidth int
	var posIndex int
	for pc := 0; pc < len(f.Code); pc = d.next(pc) {
		for posIndex < len(f.Positions) && int(f.Positions[posIndex].PC) <= pc {
			posLabels[pc] = d.position(f.Positions[posIndex])
			posIndex++
		}
		posWidth = max(posWidth, len(posLabels[pc]))
		nameWidth = max(nameWidth, len(f.Code[pc].Op.String()))
	}
	pcWidth := max(4, len(strconv.Itoa(len(f.Code)-1)))

	for pc := 0; pc < len(f.Code); pc = d.next(pc) {
		d.b = fmt.Appendf(d.b, "  %*d  ", pcWidth, pc)
		if posWidth > 0 {
			d.b = fmt.Appendf(d.b, "%-*s  ", posWidth, posLabels[pc])
		}
		name := f.Code[pc].Op.String()
		d.b = fmt.Appendf(d.b, "%-*s  ", nameWidth, name)
		end := len(d.b)
		d.operands(pc)
		if len(d.b) == end {
			// Drop the padding of an instruction without operands.
			d.b = d.b[:end-(nameWidth-len(name))-2]
		}
		d.b = append(d.b, '\n')
	}
}

// position returns the line and the column of p.
func (d *disassembler) position(p Pos) string {
	if d.f.Lines == nil {
		return strconv.Itoa(int(p.Offset)) + "?"
	}
	line, column := d.f.Lines.Position(p.Offset)
	return strconv.Itoa(line) + ":" + strconv.Itoa(column)
}

// operands appends the operands of the instruction at pc and of its OpExtra.
func (d *disassembler) operands(pc int) {
	code := d.f.Code
	in := code[pc]
	if int(in.Op) >= len(opcodes) || opcodes[in.Op].name == "" {
		d.b = fmt.Appendf(d.b, "%d, %d, %d", in.A, in.B, in.C)
		return
	}
	ops := &opcodes[in.Op].operands
	var x Instr
	hasExtra := ops.Extra && pc+1 < len(code) && code[pc+1].Op == OpExtra
	if hasExtra {
		x = code[pc+1]
	}
	start := len(d.b)
	for field := FieldA; field <= FieldXB; field++ {
		if !ops.Extra && field >= FieldXA {
			break
		}
		k := ops.kind(field)
		v := operand(in, x, field)
		if k == OperandUnused && v == 0 {
			continue
		}
		if field == FieldC && ops.B == OperandConstRun {
			// The run of b shows its length.
			continue
		}
		if len(d.b) > start {
			d.b = append(d.b, ", "...)
		}
		if field >= FieldXA && !hasExtra {
			// The OpExtra is missing.
			d.b = append(d.b, '?')
			continue
		}
		d.operand(k, v, in)
	}
}

// operand appends the operand v of the kind k of the instruction in.
func (d *disassembler) operand(k OperandKind, v int32, in Instr) {
	f := d.f
	switch k {
	case OperandUnused:
		d.bad(v)
	case OperandReg:
		d.reg(v)
	case OperandOptReg:
		if v == -1 {
			d.b = append(d.b, '-')
			return
		}
		d.reg(v)
	case OperandConst:
		if !inRange(v, len(f.Consts)) {
			d.bad(v)
			return
		}
		d.constant(f.Consts[v], v)
	case OperandConstRun:
		if v < 0 || in.C < 0 || int64(v)+int64(in.C) > int64(len(f.Consts)) {
			d.bad(v)
			return
		}
		d.b = append(d.b, '[')
		for i := v; i < v+in.C; i++ {
			if i > v {
				d.b = append(d.b, ", "...)
			}
			d.constant(f.Consts[i], i)
		}
		d.b = append(d.b, ']')
	case OperandKey:
		if !inRange(v, len(f.Keys)) {
			d.bad(v)
			return
		}
		d.b = appendQuoted(d.b, f.Keys[v])
	case OperandSite:
		if !inRange(v, min(len(f.Keys), f.NumCaches)) {
			d.bad(v)
			return
		}
		d.b = appendQuoted(d.b, f.Keys[v])
	case OperandTarget:
		d.b = append(d.b, "->"...)
		d.b = strconv.AppendInt(d.b, int64(v), 10)
		if !inRange(v, len(f.Code)) {
			d.b = append(d.b, '?')
		}
	case OperandInt:
		d.b = strconv.AppendInt(d.b, int64(v), 10)
	case OperandCount:
		if v < 0 {
			d.bad(v)
			return
		}
		d.b = strconv.AppendInt(d.b, int64(v), 10)
	case OperandFlag:
		switch v {
		case 0:
			d.b = append(d.b, "false"...)
		case 1:
			d.b = append(d.b, "true"...)
		default:
			d.bad(v)
		}
	case OperandUpval:
		d.b = fmt.Appendf(d.b, "u%d", v)
		if !inRange(v, len(f.Upvals)) {
			d.b = append(d.b, '?')
		}
	case OperandProto:
		if !inRange(v, len(f.Protos)) {
			d.bad(v)
			return
		}
		d.b = append(d.b, '[')
		d.b = append(d.b, protoPath(d.path, v)...)
		d.b = append(d.b, ']')
		if name := f.Protos[v].Name; name != "" {
			d.b = append(d.b, ' ')
			d.b = append(d.b, name...)
		}
	case OperandRegExp:
		if !inRange(v, len(f.RegExps)) {
			d.bad(v)
			return
		}
		r := f.RegExps[v]
		d.b = append(d.b, '/')
		d.b = appendEscaped(d.b, r.Pattern, false)
		d.b = append(d.b, '/')
		d.b = append(d.b, r.Flags...)
	case OperandTemplate:
		if !inRange(v, len(f.Templates)) {
			d.bad(v)
			return
		}
		d.b = append(d.b, '`')
		for i, s := range f.Templates[v].Raw {
			if i > 0 {
				d.b = append(d.b, "${}"...)
			}
			d.b = appendEscaped(d.b, s, false)
		}
		d.b = append(d.b, '`')
	case OperandEvalScope:
		d.b = fmt.Appendf(d.b, "evalscope %d", v)
		if !inRange(v, len(f.EvalScopes)) {
			d.b = append(d.b, '?')
		}
	case OperandGlobalCache:
		d.b = fmt.Appendf(d.b, "globalcache %d", v)
		if !inRange(v, f.NumGlobalCaches) {
			d.b = append(d.b, '?')
		}
	case OperandThrow:
		d.enum(throwNames[:], v)
	case OperandDecl:
		d.enum(declNames[:], v)
	case OperandIter:
		d.enum(iterNames[:], v)
	case OperandElem:
		base := v &^ (ElemStatic | ElemValue)
		if !inRange(base, len(elemNames)) || v&ElemValue != 0 && base != ElemField {
			d.bad(v)
			return
		}
		d.b = append(d.b, elemNames[base]...)
		if v&ElemStatic != 0 {
			d.b = append(d.b, "|ElemStatic"...)
		}
		if v&ElemValue != 0 {
			d.b = append(d.b, "|ElemValue"...)
		}
	case OperandDispose:
		d.enum(disposeNames[:], v)
	case OperandNamePrefix:
		d.enum(namePrefixNames[:], v)
	case OperandPrim:
		d.enum(primNames[:], v)
	case OperandGlobalVar:
		switch v {
		case 0:
			d.b = append(d.b, '0')
		case GlobalVarFunc:
			d.b = append(d.b, "GlobalVarFunc"...)
		case GlobalVarDeletable:
			d.b = append(d.b, "GlobalVarDeletable"...)
		case GlobalVarFunc | GlobalVarDeletable:
			d.b = append(d.b, "GlobalVarFunc|GlobalVarDeletable"...)
		default:
			d.bad(v)
		}
	default:
		panic("bytecode: unknown OperandKind")
	}
}

func inRange(v int32, n int) bool {
	return v >= 0 && int(v) < n
}

// bad appends the operand v that is out of range.
func (d *disassembler) bad(v int32) {
	d.b = strconv.AppendInt(d.b, int64(v), 10)
	d.b = append(d.b, '?')
}

func (d *disassembler) reg(v int32) {
	d.b = fmt.Appendf(d.b, "r%d", v)
	if !inRange(v, d.f.NumRegs) {
		d.b = append(d.b, '?')
	}
}

// constant appends the value of the constant c at the index v.
func (d *disassembler) constant(c Const, v int32) {
	switch c.Kind {
	case ConstUndefined:
		d.b = append(d.b, "undefined"...)
	case ConstNull:
		d.b = append(d.b, "null"...)
	case ConstFalse:
		d.b = append(d.b, "false"...)
	case ConstTrue:
		d.b = append(d.b, "true"...)
	case ConstNumber:
		if c.Num == 0 && math.Signbit(c.Num) {
			d.b = append(d.b, "-0"...)
			return
		}
		d.b = numconv.AppendNumber(d.b, c.Num)
	case ConstString:
		d.b = appendQuoted(d.b, c.Str)
	case ConstBigInt:
		d.b = append(d.b, c.Str...)
		d.b = append(d.b, 'n')
	default:
		d.bad(v)
	}
}

// enum appends the name of the value v of the constants names.
func (d *disassembler) enum(names []string, v int32) {
	if !inRange(v, len(names)) {
		d.bad(v)
		return
	}
	d.b = append(d.b, names[v]...)
}

var (
	throwNames = [...]string{
		ThrowTypeError:      "ThrowTypeError",
		ThrowReferenceError: "ThrowReferenceError",
	}
	declNames = [...]string{
		DeclVar:      "DeclVar",
		DeclLexical:  "DeclLexical",
		DeclFunction: "DeclFunction",
	}
	iterNames = [...]string{
		IterSync:     "IterSync",
		IterAsync:    "IterAsync",
		IterSyncNext: "IterSyncNext",
	}
	elemNames = [...]string{
		ElemField:       "ElemField",
		ElemMethod:      "ElemMethod",
		ElemGetter:      "ElemGetter",
		ElemSetter:      "ElemSetter",
		ElemAccessor:    "ElemAccessor",
		ElemStaticBlock: "ElemStaticBlock",
	}
	disposeNames = [...]string{
		DisposeSync:  "DisposeSync",
		DisposeAsync: "DisposeAsync",
	}
	namePrefixNames = [...]string{
		NamePrefixNone: "NamePrefixNone",
		NamePrefixGet:  "NamePrefixGet",
		NamePrefixSet:  "NamePrefixSet",
	}
	primNames = [...]string{
		PrimUndefined: "PrimUndefined",
		PrimNull:      "PrimNull",
		PrimFalse:     "PrimFalse",
		PrimTrue:      "PrimTrue",
	}
)

// appendQuoted appends the WTF-8 string s to b as a double-quoted string.
func appendQuoted(b []byte, s string) []byte {
	b = append(b, '"')
	b = appendEscaped(b, s, true)
	return append(b, '"')
}

// appendEscaped appends the WTF-8 string s to b, escaping the code points that are not graphic
// and the lone surrogates. With quoted, it also escapes double quotes and backslashes.
func appendEscaped(b []byte, s string, quoted bool) []byte {
	for len(s) > 0 {
		r, n := wtf8.DecodeRune(s)
		switch {
		case wtf8.IsHighSurrogate(r) || wtf8.IsLowSurrogate(r):
			b = fmt.Appendf(b, `\u%04x`, r)
		case r == utf8.RuneError && n == 1:
			b = fmt.Appendf(b, `\x%02x`, s[0])
		case quoted && (r == '"' || r == '\\'):
			b = append(b, '\\', byte(r))
		case strconv.IsGraphic(r):
			b = append(b, s[:n]...)
		default:
			// The quoted rune without its single quotes, such as \n or  .
			q := strconv.QuoteRune(r)
			b = append(b, q[1:len(q)-1]...)
		}
		s = s[n:]
	}
	return b
}
