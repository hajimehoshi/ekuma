// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package compiler translates a syntax tree to bytecode.
package compiler

import (
	"fmt"

	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
)

// Error is a construct that the compiler cannot translate.
type Error struct {
	// Offset is the byte offset of the construct in the source.
	Offset int

	// Line is the 1-based line of the construct.
	Line int

	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("line %d: %s", e.Line, e.Message)
}

type bindKind uint8

const (
	bindVar bindKind = iota
	bindLet
	bindConst
	bindClass

	// bindFuncName is the name of a named function expression inside that function.
	bindFuncName

	// bindPseudo is a value that the engine provides, such as this.
	bindPseudo
)

// binding is a variable of the function being compiled.
type binding struct {
	name string
	reg  int32
	kind bindKind

	// tdz is set while the binding may be read before its declaration was evaluated.
	tdz bool

	// alwaysCheck is set for a binding whose declaration can be skipped by a jump.
	alwaysCheck bool
}

// scope is a block scope.
type scope struct {
	parent *scope
	vars   map[string]*binding

	// firstReg is the first register of the scope's own variables.
	firstReg int32

	// captured is set when a closure refers to a variable of the scope.
	captured bool

	// withs is the number of with statements that the scope is nested in, within the function.
	withs int
}

type refKind uint8

const (
	refLocal refKind = iota
	refUpval
	refGlobal
)

// ref is a resolved variable reference.
type ref struct {
	kind refKind
	b    *binding

	// index is the upvalue index for refUpval.
	index int32
}

type ctlKind uint8

const (
	// ctlLoop is a loop or another statement that break or continue can target.
	ctlLoop ctlKind = iota

	// ctlFinally is a try statement with a finally block, while its try or catch block is compiled.
	ctlFinally

	// ctlScope is a block scope that has variables.
	ctlScope

	// ctlIter is a for-of loop whose iterator must be closed when the loop is left.
	ctlIter

	// ctlWith is a with statement.
	ctlWith
)

type exitKind uint8

const (
	exitBreak exitKind = iota
	exitContinue
	exitReturn
)

// pendingExit is a jump that left a try statement and continues after its finally block.
type pendingExit struct {
	id     int32
	kind   exitKind
	target *ctl
}

// ctl is an entry of the stack of constructs that a jump can target or has to unwind.
type ctl struct {
	kind ctlKind

	labels      []string
	isLoop      bool
	isSwitch    bool
	hasIter     bool
	asyncIter   bool
	breaks      []int
	continues   []int
	continuePC  int32
	hasContinue bool

	// For ctlFinally: the registers of the completion and the jumps to the finally block.
	kindReg  int32
	valueReg int32
	toFinal  []int
	pending  []pendingExit

	// For ctlScope and ctlIter: the first register of the scope, or the iterator.
	reg int32
}

// fn is the state of the function being compiled.
type fn struct {
	c      *compiler
	parent *fn
	f      *bytecode.Func
	node   *ast.Function

	scope *scope

	// top is the first register that is not in use.
	top int32

	upvals    []*binding
	consts    map[bytecode.Const]int32
	keys      map[string]int32
	control   []*ctl
	withDepth int

	// mutated holds the names that functions nested in this one assign to.
	mutated map[string]bool

	isScript bool

	// completion is the register of the completion value of a script, or -1.
	completion int32

	// forwardsArgs is set when the function only ever passes its arguments on,
	// so that no arguments object is needed.
	forwardsArgs bool

	// chains holds, for every optional chain being compiled, the jumps of its short circuits.
	chains [][]int

	line int32
}

type compiler struct {
	prog *ast.Program
	file string

	// isExpression holds the named function expressions, whose names are bound inside them.
	isExpression map[*ast.Function]bool

	// fieldNames holds the names that the anonymous functions of class field initializers get.
	fieldNames map[*ast.Function]string
}

// CompileScript compiles the script prog. file names the source in stack traces.
func CompileScript(prog *ast.Program, file string) (f *bytecode.Func, err error) {
	c := &compiler{
		prog:         prog,
		file:         file,
		isExpression: map[*ast.Function]bool{},
		fieldNames:   map[*ast.Function]string{},
	}
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*Error)
			if !ok {
				panic(r)
			}
			f, err = nil, e
		}
	}()
	return c.compileScript(), nil
}

// fail aborts the compilation with an error at the node n.
func (c *compiler) fail(n ast.Node, format string, args ...any) {
	line, _ := c.prog.Position(n.Start())
	panic(&Error{
		Offset:  int(n.Start()),
		Line:    line,
		Message: fmt.Sprintf(format, args...),
	})
}

func (c *compiler) newFn(parent *fn, node *ast.Function, name string) *fn {
	f := &fn{
		c:      c,
		parent: parent,
		node:   node,
		f: &bytecode.Func{
			Name: name,
			File: c.file,
		},
		consts:     map[bytecode.Const]int32{},
		keys:       map[string]int32{},
		completion: -1,
	}
	f.scope = &scope{vars: map[string]*binding{}}
	return f
}

func (f *fn) emit(op bytecode.Opcode, a, b, c int32) int {
	f.f.Code = append(f.f.Code, bytecode.Instr{
		Op: op,
		A:  a,
		B:  b,
		C:  c,
	})
	return len(f.f.Code) - 1
}

// pc returns the index of the next instruction.
func (f *fn) pc() int32 {
	return int32(len(f.f.Code))
}

// patch makes the jump instruction at index i jump to the next instruction.
func (f *fn) patch(i int) {
	f.f.Code[i].A = f.pc()
}

func (f *fn) patchAll(list []int) {
	for _, i := range list {
		f.patch(i)
	}
}

// patchC is patch for the instructions that jump to their C operand.
func (f *fn) patchC(i int) {
	f.f.Code[i].C = f.pc()
}

func (f *fn) jump() int {
	return f.emit(bytecode.OpJump, 0, 0, 0)
}

func (f *fn) jumpTo(pc int32) {
	f.emit(bytecode.OpJump, pc, 0, 0)
}

// setLine records the source line of the instructions that follow.
func (f *fn) setLine(n ast.Node) {
	line, _ := f.c.prog.Position(n.Start())
	if int32(line) == f.line {
		return
	}
	f.line = int32(line)
	pc := f.pc()
	if k := len(f.f.Lines); k > 0 && f.f.Lines[k-1].PC == pc {
		f.f.Lines[k-1].Line = int32(line)
		return
	}
	f.f.Lines = append(f.f.Lines, bytecode.LinePos{
		PC:   pc,
		Line: int32(line),
	})
}

// newReg reserves the next register.
func (f *fn) newReg() int32 {
	r := f.top
	f.top++
	if int(f.top) > f.f.NumRegs {
		f.f.NumRegs = int(f.top)
	}
	return r
}

// newRegs reserves n consecutive registers and returns the first.
func (f *fn) newRegs(n int) int32 {
	r := f.top
	f.top += int32(n)
	if int(f.top) > f.f.NumRegs {
		f.f.NumRegs = int(f.top)
	}
	return r
}

// free releases the registers from mark on.
func (f *fn) free(mark int32) {
	f.top = mark
}

func (f *fn) constNum(n float64) int32 {
	return f.constant(bytecode.Const{
		Kind: bytecode.ConstNumber,
		Num:  n,
	})
}

func (f *fn) constStr(s string) int32 {
	return f.constant(bytecode.Const{
		Kind: bytecode.ConstString,
		Str:  s,
	})
}

func (f *fn) constant(c bytecode.Const) int32 {
	// NaN is not equal to itself, and -0 equals 0: neither is worth sharing through the map.
	if c.Kind == bytecode.ConstNumber && (c.Num != c.Num || c.Num == 0) {
		f.f.Consts = append(f.f.Consts, c)
		return int32(len(f.f.Consts) - 1)
	}
	if i, ok := f.consts[c]; ok {
		return i
	}
	i := int32(len(f.f.Consts))
	f.f.Consts = append(f.f.Consts, c)
	f.consts[c] = i
	return i
}

// key returns the index of the property or variable name s.
func (f *fn) key(s string) int32 {
	if i, ok := f.keys[s]; ok {
		return i
	}
	i := f.site(s)
	f.keys[s] = i
	return i
}

// site returns a fresh index for the name s. The index also names an inline cache,
// so every instruction that caches gets a site of its own.
func (f *fn) site(s string) int32 {
	i := int32(len(f.f.Keys))
	f.f.Keys = append(f.f.Keys, s)
	f.f.NumCaches = len(f.f.Keys)
	return i
}

// declare creates a variable in the current scope and gives it a register.
func (f *fn) declare(name string, kind bindKind) *binding {
	if b, ok := f.scope.vars[name]; ok {
		return b
	}
	b := &binding{
		name: name,
		reg:  f.newReg(),
		kind: kind,
	}
	f.scope.vars[name] = b
	return b
}

// declareAt creates a variable in the current scope that lives in the existing register reg.
func (f *fn) declareAt(name string, kind bindKind, reg int32) *binding {
	b := &binding{
		name: name,
		reg:  reg,
		kind: kind,
	}
	f.scope.vars[name] = b
	return b
}

func (f *fn) pushScope() *scope {
	s := &scope{
		parent:   f.scope,
		vars:     map[string]*binding{},
		firstReg: f.top,
		withs:    f.withDepth,
	}
	f.scope = s
	return s
}

// popScope leaves the current scope, detaching the closures that captured its variables.
func (f *fn) popScope() {
	s := f.scope
	if s.captured {
		f.emit(bytecode.OpCloseUpvals, s.firstReg, 0, 0)
	}
	f.scope = s.parent
	f.free(s.firstReg)
}

// lookupLocal finds the variable name in the scopes of f.
func (f *fn) lookupLocal(name string) (*binding, *scope) {
	for s := f.scope; s != nil; s = s.parent {
		if b, ok := s.vars[name]; ok {
			return b, s
		}
	}
	return nil, nil
}

// resolve finds the variable that name refers to.
func (f *fn) resolve(name string) ref {
	if b, _ := f.lookupLocal(name); b != nil {
		return ref{
			kind: refLocal,
			b:    b,
		}
	}
	if f.parent == nil {
		return ref{kind: refGlobal}
	}
	r := f.parent.resolve(name)
	if r.kind == refGlobal {
		return r
	}
	for i, b := range f.upvals {
		if b == r.b {
			return ref{
				kind:  refUpval,
				b:     r.b,
				index: int32(i),
			}
		}
	}
	d := bytecode.UpvalDesc{Index: r.index}
	if r.kind == refLocal {
		d.Local = true
		d.Index = r.b.reg
		_, s := f.parent.lookupLocal(name)
		s.captured = true
	}
	f.upvals = append(f.upvals, r.b)
	f.f.Upvals = append(f.f.Upvals, d)
	return ref{
		kind:  refUpval,
		b:     r.b,
		index: int32(len(f.upvals) - 1),
	}
}

// dynamic reports whether name has to be looked up in with scopes first.
func (f *fn) dynamic() bool {
	for g := f; g != nil; g = g.parent {
		if g.withDepth > 0 {
			return true
		}
	}
	return false
}

// needsCheck reports whether a read of the binding of r can see it uninitialized.
func (r ref) needsCheck() bool {
	if r.b == nil {
		return false
	}
	if r.kind == refUpval {
		return r.b.kind == bindLet || r.b.kind == bindConst || r.b.kind == bindClass || r.b.tdz
	}
	return r.b.tdz || r.b.alwaysCheck
}

// loadVar emits the read of the variable name into dst.
func (f *fn) loadVar(name string, dst int32, orUndef bool) {
	var skip int = -1
	if f.dynamic() {
		op := bytecode.OpGetName
		if orUndef {
			op = bytecode.OpTypeofName
		}
		skip = f.emit(op, dst, f.key(name), 0)
	}
	r := f.resolve(name)
	switch r.kind {
	case refLocal:
		if r.needsCheck() {
			f.emit(bytecode.OpCheckTDZ, r.b.reg, f.key(name), 0)
		}
		if r.b.reg != dst {
			f.emit(bytecode.OpMove, dst, r.b.reg, 0)
		}
	case refUpval:
		f.emit(bytecode.OpGetUpval, dst, r.index, 0)
		if r.needsCheck() {
			f.emit(bytecode.OpCheckTDZ, dst, f.key(name), 0)
		}
	case refGlobal:
		op := bytecode.OpGetGlobal
		if orUndef {
			op = bytecode.OpGetGlobalOrUndef
		}
		k := f.site(name)
		f.emit(op, dst, k, k)
	}
	if skip >= 0 {
		if orUndef {
			f.emit(bytecode.OpTypeof, dst, dst, 0)
		}
		f.patchC(skip)
	}
}

// storeVar emits the assignment of the register src to the variable name.
// init marks the store that initializes a declaration, which is allowed for a constant.
func (f *fn) storeVar(n ast.Node, name string, src int32, init bool) {
	var skip int = -1
	if !init && f.dynamic() {
		skip = f.emit(bytecode.OpSetName, src, f.key(name), 0)
	}
	r := f.resolve(name)
	if r.b != nil && !init {
		switch r.b.kind {
		case bindConst:
			if r.needsCheck() {
				f.checkTDZ(r, name)
			}
			f.emit(bytecode.OpThrowConstAssign, f.key(name), 0, 0)
			if skip >= 0 {
				f.patchC(skip)
			}
			return
		case bindFuncName:
			// Assigning to the name of a function expression is an error in strict code only.
			if f.f.Strict {
				f.emit(bytecode.OpThrowConstAssign, f.key(name), 0, 0)
			}
			if skip >= 0 {
				f.patchC(skip)
			}
			return
		}
		if r.needsCheck() {
			f.checkTDZ(r, name)
		}
	}
	switch r.kind {
	case refLocal:
		if r.b.reg != src {
			f.emit(bytecode.OpMove, r.b.reg, src, 0)
		}
	case refUpval:
		f.emit(bytecode.OpSetUpval, src, r.index, 0)
	case refGlobal:
		switch {
		case init && f.isScript && f.c.globalLexical(name):
			f.emit(bytecode.OpInitGlobalLex, src, f.key(name), 0)
		case f.f.Strict:
			k := f.site(name)
			f.emit(bytecode.OpSetGlobalStrict, src, k, k)
		default:
			k := f.site(name)
			f.emit(bytecode.OpSetGlobal, src, k, k)
		}
	}
	if skip >= 0 {
		f.patchC(skip)
	}
}

// checkTDZ emits the check that the binding of r is initialized.
func (f *fn) checkTDZ(r ref, name string) {
	switch r.kind {
	case refLocal:
		f.emit(bytecode.OpCheckTDZ, r.b.reg, f.key(name), 0)
	case refUpval:
		t := f.newReg()
		f.emit(bytecode.OpGetUpval, t, r.index, 0)
		f.emit(bytecode.OpCheckTDZ, t, f.key(name), 0)
		f.free(t)
	}
}

// globalLexical reports whether name is a let, const or class declaration of the top level of the script.
func (c *compiler) globalLexical(name string) bool {
	for _, d := range c.prog.Scope.Decls {
		if d.Name == name {
			switch d.Kind {
			case ast.DeclLet, ast.DeclConst, ast.DeclClass:
				return true
			}
		}
	}
	return false
}

// localReg returns the register of the variable name when it is a local of f that can be
// read without a check, and -1 otherwise.
func (f *fn) localReg(name string) int32 {
	if f.dynamic() {
		return -1
	}
	b, _ := f.lookupLocal(name)
	if b == nil || b.tdz || b.alwaysCheck {
		return -1
	}
	return b.reg
}

func (c *compiler) compileScript() *bytecode.Func {
	prog := c.prog
	f := c.newFn(nil, nil, "")
	f.isScript = true
	f.f.Strict = prog.Strict
	f.f.Source = prog.Source
	f.mutated = map[string]bool{}

	// Register 0 holds this, and register 1 the completion value.
	this := f.declare("this", bindPseudo)
	f.emit(bytecode.OpLoadThis, this.reg, 0, 0)
	f.completion = f.newReg()
	f.emit(bytecode.OpLoadUndef, f.completion, 0, 0)

	// GlobalDeclarationInstantiation: check everything before declaring anything.
	funcs := map[string]*ast.Function{}
	var funcOrder []string
	for _, d := range prog.Scope.Decls {
		switch d.Kind {
		case ast.DeclLet, ast.DeclConst, ast.DeclClass:
			f.emit(bytecode.OpCheckGlobalDecl, f.key(d.Name), 1, 0)
		case ast.DeclFunction:
			if _, ok := funcs[d.Name]; !ok {
				funcOrder = append(funcOrder, d.Name)
			}
			funcs[d.Name] = d.Func
		}
	}
	for _, name := range funcOrder {
		f.emit(bytecode.OpCheckGlobalDecl, f.key(name), 2, 0)
	}
	for _, d := range prog.Scope.Decls {
		if d.Kind == ast.DeclVar || d.Kind == ast.DeclAnnexBVar {
			if _, ok := funcs[d.Name]; !ok {
				f.emit(bytecode.OpCheckGlobalDecl, f.key(d.Name), 0, 0)
			}
		}
	}
	for _, d := range prog.Scope.Decls {
		switch d.Kind {
		case ast.DeclLet, ast.DeclClass:
			f.emit(bytecode.OpDeclareGlobalLex, f.key(d.Name), 0, 0)
		case ast.DeclConst:
			f.emit(bytecode.OpDeclareGlobalLex, f.key(d.Name), 1, 0)
		}
	}
	for _, name := range funcOrder {
		t := f.newReg()
		f.closure(funcs[name], t, name)
		f.emit(bytecode.OpDeclareGlobalVar, f.key(name), 1, t)
		f.free(t)
	}
	for _, d := range prog.Scope.Decls {
		if d.Kind == ast.DeclVar || d.Kind == ast.DeclAnnexBVar {
			if _, ok := funcs[d.Name]; !ok {
				f.emit(bytecode.OpDeclareGlobalVar, f.key(d.Name), 0, 0)
			}
		}
	}

	f.stmts(prog.Body)
	f.emit(bytecode.OpReturn, f.completion, 0, 0)
	f.f.NumClear = 0
	return f.f
}
