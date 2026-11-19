// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package compiler translates a syntax tree to bytecode.
package compiler

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hajimehoshi/ekuma/internal/ast"
	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/srcpos"
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

	// sym is the symbol of name, and scope is the scope that declares the binding. Both are nil
	// for a binding that stands for a variable of the call site of eval code.
	sym   *symbol
	scope *scope

	// shadowed is the binding of the same name that this one hides, while scope is open.
	shadowed *binding

	// capFn is the innermost function being compiled that has an upvalue for the binding, and
	// capIndex is the index of that upvalue.
	capFn    *fn
	capIndex int32

	// tdz is set while the binding may be read before its declaration was evaluated.
	tdz bool

	// alwaysCheck is set for a binding whose declaration can be skipped by a jump.
	alwaysCheck bool

	// unset is set while tdz is set and the register does not hold the TDZ marker,
	// so that every access of the binding is known to throw.
	unset bool

	// catch marks a parameter of a catch clause.
	catch bool

	// initialized is set once the code compiled from then on always runs after the binding
	// got its first value.
	initialized bool
}

// symbol is a variable name of the program being compiled.
type symbol struct {
	// top is the innermost binding of the name among the open scopes, or nil.
	top *binding
}

// scope is a block scope.
type scope struct {
	fn     *fn
	parent *scope

	// binds are the bindings that the scope declares, in declaration order.
	binds  []*binding
	inline [4]*binding

	// firstReg is the first register of the scope's own variables.
	firstReg int32

	// captured is set when a closure refers to a variable of the scope or a direct eval is
	// in it.
	captured bool

	// withs is the number of with statements that the scope is nested in, within the function.
	withs int

	// dispose is the register of the dispose capability of the using declarations of the scope,
	// when hasDispose is set.
	dispose    int32
	hasDispose bool

	// used is set once a using declaration of the scope was compiled.
	used bool
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

	// ctlFinalizer is the finally block of a try statement in code that has a completion value.
	ctlFinalizer
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

	// For ctlFinally: the scope whose using declarations it disposes, or nil for a try statement.
	disposes *scope

	// For ctlScope and ctlIter: the first register of the scope, or the iterator.
	// For ctlFinalizer: the register of the completion value of the finally block, and
	// valueReg is the register of the completion value of the code around the try statement.
	reg int32
}

// fn is the state of the function being compiled.
type fn struct {
	c      *compiler
	parent *fn
	f      *bytecode.Func
	node   *ast.Function

	scope *scope

	// varScope is the scope of the var and function declarations of the function or script.
	varScope *scope

	// depth is the number of functions around f.
	depth int

	// top is the first register that is not in use.
	top int32

	upvals []*binding

	// upvalSaves holds, for every binding in upvals, its capFn and capIndex before f captured it.
	upvalSaves []capture

	// numConsts and strConsts map the bits of the number constants and the strings of the string
	// constants to their indices. bigConsts does so for the digits of the BigInt constants.
	numConsts map[uint64]int32
	strConsts map[string]int32
	bigConsts map[string]int32
	keys      map[string]int32
	control   []*ctl
	withDepth int

	// outerWiths is the number of with statements and dynamic scopes of eval code around f,
	// outside its own code, which stays the same while f is compiled.
	outerWiths int

	// mutated holds the parameters that an arguments object can change.
	mutated map[string]bool

	// mapsPeak is the largest number of entries that one of the maps of this state held when a
	// function compiled with it finished.
	mapsPeak int

	isScript bool

	// module is set for the function of a module, whose top-level bindings are upvalues.
	module bool

	// completion is the register of the completion value of a script, or -1.
	completion int32

	// forwardsArgs is set when the function only ever passes its arguments on,
	// so that no arguments object is needed.
	forwardsArgs bool

	// chains holds, for every optional chain being compiled, the jumps of its short circuits.
	chains [][]int

	// classDepth is the number of class bodies around the code being compiled, within the function.
	classDepth int

	// lazyArgs is set when the variable arguments holds the TDZ marker until a direct eval
	// creates the mapped arguments object.
	lazyArgs bool

	// anyMutated is set when a direct eval can assign to any variable of the function.
	anyMutated bool

	// evalLevel is the withDepth at which the scope of OpEvalScope that the direct evals of
	// the code being compiled declare their variables in was pushed, or 0 when there is none.
	evalLevel int

	// eval describes the call site of the eval code being compiled, which is the outermost
	// function, and is nil otherwise.
	eval *evalOuter

	// annexBRegs holds, for every block-level function that the top-level code hoists to a
	// global variable only when it can be declared, the register that records whether it was.
	annexBRegs map[string]int32

	// catchDepth is the number of try blocks with a catch clause around the code being compiled.
	catchDepth int

	// tails are the calls in tail position of the expression being compiled.
	tails []ast.Expr
}

type compiler struct {
	prog *ast.Program
	file string

	// lines converts the offsets of prog into lines and columns, for the compiled code and for
	// errors.
	lines *srcpos.Table

	// isExpression holds the named function expressions, whose names are bound inside them.
	isExpression map[*ast.Function]bool

	// fieldNames holds the names that the anonymous functions of class field initializers get.
	fieldNames map[*ast.Function]string

	// fieldKeys holds the variables of the property keys whose names the anonymous functions
	// of class field initializers get, when the names are known only at run time.
	fieldKeys map[*ast.Function]string

	// dynamic is the function of a program from parser.ParseFunction, whose name is not bound inside it.
	dynamic *ast.Function

	scratch *scratch

	// evalCode is set while eval code is compiled, whose free names can be variables of
	// the call site.
	evalCode bool

	// lastName and lastSym are the most recent result of symbol.
	lastName string
	lastSym  *symbol

	// topDecls maps the names that the top level of a program with many declarations
	// declares to what their declarations are, once a lookup needed it.
	topDecls map[string]topDecl

	// poll, when not nil, is called every pollNodes nodes, and may stop the compilation by
	// panicking. nodes counts the nodes toward the next call.
	poll  func()
	nodes int
}

// topDecl tells what the declarations of a name at the top level of a program are.
type topDecl struct {
	// lexical reports a let, const or class declaration.
	lexical bool

	// plain reports a declaration that is not an Annex B var.
	plain bool
}

// lookupTopDecl returns what the declarations of name at the top level of the program are.
func (c *compiler) lookupTopDecl(name string) topDecl {
	decls := c.prog.Scope.Decls
	if len(decls) > 16 {
		if c.topDecls == nil {
			c.topDecls = make(map[string]topDecl, len(decls))
			for _, d := range decls {
				c.topDecls[d.Name] = c.topDecls[d.Name].with(d.Kind)
			}
		}
		return c.topDecls[name]
	}
	var t topDecl
	for _, d := range decls {
		if d.Name == name {
			t = t.with(d.Kind)
		}
	}
	return t
}

// with returns t with a declaration of the kind k added.
func (t topDecl) with(k ast.DeclKind) topDecl {
	switch k {
	case ast.DeclLet, ast.DeclConst, ast.DeclClass:
		t.lexical = true
	}
	if k != ast.DeclAnnexBVar {
		t.plain = true
	}
	return t
}

// pollNodes is the number of statements and expressions between two calls of the poll function
// of a compilation.
const pollNodes = 1 << 14

// step counts a statement or an expression toward the next call of the poll function.
func (c *compiler) step() {
	c.nodes++
	if c.nodes >= pollNodes {
		c.nodes = 0
		if c.poll != nil {
			c.poll()
		}
	}
}

// scratch is the storage that compilations reuse.
type scratch struct {
	// The free buffers that the functions being compiled build their code, positions, keys
	// and constants in. A finished function gets copies of exactly the used length.
	codeBufs  [][]bytecode.Instr
	posBufs   [][]bytecode.Pos
	keyBufs   [][]string
	constBufs [][]bytecode.Const

	// fns are the states of finished functions, with their maps emptied.
	fns []*fn

	// syms maps the names of the compilation to their symbols, which come from symChunks.
	// symsUsed is the number of symbols handed out, and symsPeak the most entries that syms
	// held when a compilation ended.
	syms      map[string]*symbol
	symChunks []*[chunkSize]symbol
	symsUsed  int
	symsPeak  int

	// bindChunks hold the bindings of the compilation, of which bindsUsed are handed out.
	bindChunks []*[chunkSize]binding
	bindsUsed  int
}

// Limits of what is kept for later compilations, so that compiling a large program does not keep
// its memory alive: the number of scratch sets in freeScratch, and what each set holds. A buffer
// limit counts the elements of all the buffers of its kind together.
const (
	maxFreeScratch   = 4
	maxFreeFns       = 16
	maxFreeMapLen    = 1024
	maxSparseMapLen  = 256
	maxFreeCode      = 1 << 15
	maxFreePositions = 1 << 15
	maxFreeKeys      = 1 << 14
	maxFreeConsts    = 1 << 12
	maxFreeChunks    = 4
)

// chunkSize is the number of symbols or bindings that one allocation holds.
const chunkSize = 256

// freeScratch holds scratch sets for reuse, which garbage collection does not take away. An
// empty slot is nil.
var freeScratch [maxFreeScratch]atomic.Pointer[scratch]

// scratchPool holds the scratch sets for reuse that do not fit in freeScratch.
var scratchPool sync.Pool

// getScratch returns a scratch set that no other compilation uses.
func getScratch() *scratch {
	for i := range freeScratch {
		if s := freeScratch[i].Load(); s != nil && freeScratch[i].CompareAndSwap(s, nil) {
			return s
		}
	}
	if s, ok := scratchPool.Get().(*scratch); ok {
		return s
	}
	return &scratch{}
}

// putScratch gives back the scratch set s for reuse.
func putScratch(s *scratch) {
	s.trim()
	for i := range freeScratch {
		if freeScratch[i].Load() == nil && freeScratch[i].CompareAndSwap(nil, s) {
			return
		}
	}
	scratchPool.Put(s)
}

// capture is the upvalue of a binding in the innermost function that has one.
type capture struct {
	fn    *fn
	index int32
}

// symbol returns the symbol of name.
func (c *compiler) symbol(name string) *symbol {
	if c.lastSym != nil && name == c.lastName {
		return c.lastSym
	}
	sc := c.scratch
	s, ok := sc.syms[name]
	if !ok {
		i := sc.symsUsed
		if i/chunkSize == len(sc.symChunks) {
			sc.symChunks = append(sc.symChunks, new([chunkSize]symbol))
		}
		s = &sc.symChunks[i/chunkSize][i%chunkSize]
		sc.symsUsed++
		if sc.syms == nil {
			sc.syms = map[string]*symbol{}
		}
		sc.syms[name] = s
	}
	c.lastName = name
	c.lastSym = s
	return s
}

// newBinding returns a zero binding that lives until the compilation ends.
func (c *compiler) newBinding() *binding {
	sc := c.scratch
	i := sc.bindsUsed
	if i/chunkSize == len(sc.bindChunks) {
		sc.bindChunks = append(sc.bindChunks, new([chunkSize]binding))
	}
	b := &sc.bindChunks[i/chunkSize][i%chunkSize]
	sc.bindsUsed++
	return b
}

// trimChunks zeroes the first n elements of chunks, and drops the chunks beyond maxFreeChunks.
func trimChunks[T any](chunks []*[chunkSize]T, n int) []*[chunkSize]T {
	for _, ch := range chunks {
		if n <= 0 {
			break
		}
		clear(ch[:min(n, len(ch))])
		n -= len(ch)
	}
	if len(chunks) > maxFreeChunks {
		clear(chunks[maxFreeChunks:])
		chunks = chunks[:maxFreeChunks]
	}
	return chunks
}

// trim empties s for reuse and drops what it holds beyond the limits.
func (s *scratch) trim() {
	if n := len(s.syms); keepMaps(n, s.symsPeak) {
		clear(s.syms)
		s.symsPeak = max(s.symsPeak, n)
	} else {
		s.syms = nil
		s.symsPeak = 0
	}
	s.symChunks = trimChunks(s.symChunks, s.symsUsed)
	s.bindChunks = trimChunks(s.bindChunks, s.bindsUsed)
	s.symsUsed = 0
	s.bindsUsed = 0
	s.codeBufs = trimBufs(s.codeBufs, maxFreeCode)
	s.posBufs = trimBufs(s.posBufs, maxFreePositions)
	s.keyBufs = trimBufs(s.keyBufs, maxFreeKeys)
	s.constBufs = trimBufs(s.constBufs, maxFreeConsts)
	if len(s.fns) > maxFreeFns {
		clear(s.fns[maxFreeFns:])
		s.fns = s.fns[:maxFreeFns]
	}
}

// trimBufs drops buffers from bufs so that their capacities add up to at most limit.
func trimBufs[T any](bufs [][]T, limit int) [][]T {
	kept := bufs[:0]
	var total int
	for _, b := range bufs {
		if total+cap(b) > limit {
			continue
		}
		total += cap(b)
		kept = append(kept, b)
	}
	clear(bufs[len(kept):])
	return kept
}

// keepMaps reports whether maps are kept for reuse when the fullest of them holds n entries and
// once held peak.
func keepMaps(n, peak int) bool {
	// A cleared map keeps its capacity. Clearing it takes time in proportion to that capacity,
	// unless it is empty.
	return n == 0 || (n <= maxFreeMapLen && peak <= max(4*n, maxSparseMapLen))
}

// takeBuf removes a scratch buffer from bufs and returns it empty, or returns nil when bufs has none.
func takeBuf[T any](bufs *[][]T) []T {
	n := len(*bufs)
	if n == 0 {
		return nil
	}
	b := (*bufs)[n-1]
	*bufs = (*bufs)[:n-1]
	return b
}

// releaseBuf returns a copy of work with no spare capacity, or nil when work is empty, and
// puts work back into bufs for reuse.
func releaseBuf[T any](bufs *[][]T, work []T) []T {
	if cap(work) == 0 {
		return nil
	}
	var out []T
	if len(work) > 0 {
		out = make([]T, len(work))
		copy(out, work)
	}
	clear(work)
	*bufs = append(*bufs, work[:0])
	return out
}

// CompileScript compiles the script prog. file names the source in stack traces.
func CompileScript(prog *ast.Program, file string) (*bytecode.Func, error) {
	return compile(prog, file, nil, nil)
}

// CompileFunction compiles the program prog from parser.ParseFunction. The returned script
// evaluates to the function, which runs in the global scope and does not see its own name.
// poll, when not nil, is called now and then during a long compilation, and may stop it by
// panicking.
func CompileFunction(prog *ast.Program, file string, poll func()) (*bytecode.Func, error) {
	return compile(prog, file, prog.Body[0].(*ast.ExprStmt).Expr.(*ast.Function), poll)
}

func compile(prog *ast.Program, file string, dynamic *ast.Function, poll func()) (f *bytecode.Func, err error) {
	c := newCompiler(prog, file)
	defer c.release()
	c.dynamic = dynamic
	c.poll = poll
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

// newCompiler returns a compiler of prog. The compiler must be released when it is done.
func newCompiler(prog *ast.Program, file string) *compiler {
	return &compiler{
		prog:    prog,
		file:    file,
		lines:   srcpos.NewTable(prog.Source),
		scratch: getScratch(),
	}
}

// nameField records name as the name that the anonymous function of the class field
// initializer init gets.
func (c *compiler) nameField(init *ast.Function, name string) {
	if c.fieldNames == nil {
		c.fieldNames = map[*ast.Function]string{}
	}
	c.fieldNames[init] = name
}

// release gives back the storage of c for reuse.
func (c *compiler) release() {
	putScratch(c.scratch)
	c.scratch = nil
	c.lastSym = nil
	c.lastName = ""
}

// fail aborts the compilation with an error at the node n.
func (c *compiler) fail(n ast.Node, format string, args ...any) {
	line, _ := c.lines.Position(n.Start())
	panic(&Error{
		Offset:  int(n.Start()),
		Line:    line,
		Message: fmt.Sprintf(format, args...),
	})
}

func (c *compiler) newFn(parent *fn, node *ast.Function, name string) *fn {
	sc := c.scratch
	var f *fn
	if n := len(sc.fns); n > 0 {
		f = sc.fns[n-1]
		sc.fns = sc.fns[:n-1]
		*f = fn{
			numConsts:  f.numConsts,
			strConsts:  f.strConsts,
			bigConsts:  f.bigConsts,
			keys:       f.keys,
			mutated:    f.mutated,
			scope:      f.scope,
			varScope:   f.scope,
			upvals:     f.upvals,
			upvalSaves: f.upvalSaves,
			mapsPeak:   f.mapsPeak,
		}
	} else {
		f = &fn{
			numConsts: map[uint64]int32{},
			strConsts: map[string]int32{},
			bigConsts: map[string]int32{},
			keys:      map[string]int32{},
			mutated:   map[string]bool{},
			scope:     &scope{},
		}
		f.varScope = f.scope
	}
	f.scope.fn = f
	f.c = c
	f.parent = parent
	if parent != nil {
		f.depth = parent.depth + 1
		f.outerWiths = parent.withsAround()
	}
	f.node = node
	f.completion = -1
	f.f = &bytecode.Func{
		Name:      name,
		File:      c.file,
		Code:      takeBuf(&sc.codeBufs),
		Positions: takeBuf(&sc.posBufs),
		Lines:     c.lines,
		Keys:      takeBuf(&sc.keyBufs),
		Consts:    takeBuf(&sc.constBufs),
	}
	return f
}

// finish ends the compilation of f and returns its code. f must not be used afterwards.
func (f *fn) finish() *bytecode.Func {
	b := f.f
	sc := f.c.scratch
	b.Code = releaseBuf(&sc.codeBufs, b.Code)
	b.Positions = releaseBuf(&sc.posBufs, b.Positions)
	b.Keys = releaseBuf(&sc.keyBufs, b.Keys)
	b.Consts = releaseBuf(&sc.constBufs, b.Consts)

	// Give the upvalues of the bindings of outer functions back to those functions.
	for i, u := range slices.Backward(f.upvals) {
		if u.capFn == f {
			s := f.upvalSaves[i]
			u.capFn = s.fn
			u.capIndex = s.index
		}
	}

	// The scope that newFn created is the outermost one of the function.
	root := f.scope
	for {
		root.close()
		if root.parent == nil {
			break
		}
		root = root.parent
	}
	n := max(len(root.binds), len(f.numConsts), len(f.strConsts), len(f.bigConsts), len(f.keys), len(f.mutated))
	if !keepMaps(n, f.mapsPeak) {
		return b
	}
	clear(root.binds)
	*root = scope{
		binds: root.binds[:0],
	}
	clear(f.numConsts)
	clear(f.strConsts)
	clear(f.bigConsts)
	clear(f.keys)
	clear(f.mutated)
	clear(f.upvals)
	clear(f.upvalSaves)
	*f = fn{
		numConsts:  f.numConsts,
		strConsts:  f.strConsts,
		bigConsts:  f.bigConsts,
		keys:       f.keys,
		mutated:    f.mutated,
		scope:      root,
		upvals:     f.upvals[:0],
		upvalSaves: f.upvalSaves[:0],
		mapsPeak:   max(f.mapsPeak, n),
	}
	sc.fns = append(sc.fns, f)
	return b
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

// setPos records the source position at offset for the instructions that follow.
func (f *fn) setPos(offset int32) {
	pos := bytecode.Pos{
		PC:     f.pc(),
		Offset: offset,
	}
	k := len(f.f.Positions)
	if k > 0 && f.f.Positions[k-1].Offset == pos.Offset {
		return
	}
	if k > 0 && f.f.Positions[k-1].PC == pos.PC {
		f.f.Positions[k-1] = pos
		return
	}
	f.f.Positions = append(f.f.Positions, pos)
}

// setRefPos records the position of n, a reference to the variable name, when using the
// reference may throw.
func (f *fn) setRefPos(n ast.Node, name string) {
	r := f.resolve(name)
	if r.kind == refGlobal || r.needsCheck() || (r.kind == refLocal && r.b.unset) || f.dynDepth(name) > 0 {
		f.setPos(n.Start())
	}
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
	var m map[string]int32
	var k string
	switch c.Kind {
	case bytecode.ConstNumber:
		// NaN is not equal to itself, and -0 equals 0: neither is worth sharing through the map.
		if c.Num != c.Num || c.Num == 0 {
			f.f.Consts = append(f.f.Consts, c)
			return int32(len(f.f.Consts) - 1)
		}
		bits := math.Float64bits(c.Num)
		if i, ok := f.numConsts[bits]; ok {
			return i
		}
		i := int32(len(f.f.Consts))
		f.f.Consts = append(f.f.Consts, c)
		f.numConsts[bits] = i
		return i
	case bytecode.ConstString:
		m, k = f.strConsts, c.Str
	default:
		m, k = f.bigConsts, c.Str
	}
	if i, ok := m[k]; ok {
		return i
	}
	i := int32(len(f.f.Consts))
	f.f.Consts = append(f.f.Consts, c)
	m[k] = i
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
	sym := f.c.symbol(name)
	if b := sym.top; b != nil && b.scope == f.scope {
		return b
	}
	b := f.c.newBinding()
	b.name = name
	b.reg = f.newReg()
	b.kind = kind
	f.scope.bind(sym, b)
	return b
}

// declareAt creates a variable in the current scope that lives in the existing register reg.
// It replaces a variable of the same name in the scope.
func (f *fn) declareAt(name string, kind bindKind, reg int32) *binding {
	sym := f.c.symbol(name)
	if old := sym.top; old != nil && old.scope == f.scope {
		f.scope.unbind(old)
	}
	b := f.c.newBinding()
	b.name = name
	b.reg = reg
	b.kind = kind
	f.scope.bind(sym, b)
	return b
}

// bind adds the binding b of the symbol sym to s. Every scope nested in s that is open must not
// have a binding of the same name.
func (s *scope) bind(sym *symbol, b *binding) {
	b.sym = sym
	b.scope = s
	b.shadowed = sym.top
	sym.top = b
	if s.binds == nil {
		s.binds = s.inline[:0]
	}
	s.binds = append(s.binds, b)
}

// unbind removes the binding b from s.
func (s *scope) unbind(b *binding) {
	b.unlink()
	i := len(s.binds) - 1
	for s.binds[i] != b {
		i--
	}
	s.binds = slices.Delete(s.binds, i, i+1)
}

// unlink removes the binding b from the bindings of its name.
func (b *binding) unlink() {
	sym := b.sym
	if sym.top == b {
		sym.top = b.shadowed
		return
	}
	p := sym.top
	for p.shadowed != b {
		p = p.shadowed
	}
	p.shadowed = b.shadowed
}

// close hides the bindings of s, whose code was compiled.
func (s *scope) close() {
	for _, b := range slices.Backward(s.binds) {
		b.unlink()
	}
}

// reopen makes the bindings of s visible again after close. Every scope nested in s that is
// open must not have a binding of a name of s.
func (s *scope) reopen() {
	for _, b := range s.binds {
		b.shadowed = b.sym.top
		b.sym.top = b
	}
}

// lookup returns the binding of name that s declares, or nil. s must be open.
func (s *scope) lookup(name string) *binding {
	for b := s.fn.c.symbol(name).top; b != nil; b = b.shadowed {
		if b.scope == s {
			return b
		}
		if b.scope.fn.depth < s.fn.depth {
			break
		}
	}
	return nil
}

// sortedBinds returns the bindings of s sorted by name.
func (s *scope) sortedBinds() []*binding {
	bs := slices.Clone(s.binds)
	slices.SortFunc(bs, func(a, b *binding) int {
		return strings.Compare(a.name, b.name)
	})
	return bs
}

func (f *fn) pushScope() *scope {
	s := &scope{
		fn:       f,
		parent:   f.scope,
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
	s.close()
	f.scope = s.parent
	f.free(s.firstReg)
}

// lookupLocal finds the variable name in the scopes of f.
func (f *fn) lookupLocal(name string) (*binding, *scope) {
	for b := f.c.symbol(name).top; b != nil; b = b.shadowed {
		g := b.scope.fn
		if g == f {
			return b, b.scope
		}
		if g.depth < f.depth {
			break
		}
	}
	return nil, nil
}

// resolve finds the variable that name refers to.
func (f *fn) resolve(name string) ref {
	return f.resolveSym(f.c.symbol(name), name)
}

// resolveSym is resolve for the symbol sym of name. f must be the innermost function being
// compiled, or one that the innermost function's resolveSym asks.
func (f *fn) resolveSym(sym *symbol, name string) ref {
	b := sym.top
	switch {
	case b != nil && b.scope.fn == f:
		return ref{
			kind: refLocal,
			b:    b,
		}
	case b != nil && b.capFn == f:
		return ref{
			kind:  refUpval,
			b:     b,
			index: b.capIndex,
		}
	case b == nil && !f.c.evalCode:
		return ref{kind: refGlobal}
	}
	if f.parent == nil {
		if f.eval != nil {
			if r, ok := f.eval.resolve(f, name); ok {
				return r
			}
		}
		return ref{kind: refGlobal}
	}
	r := f.parent.resolveSym(sym, name)
	if r.kind == refGlobal {
		return r
	}
	if i, ok := f.upvalIndex(r.b); ok {
		return ref{
			kind:  refUpval,
			b:     r.b,
			index: i,
		}
	}
	d := bytecode.UpvalDesc{Index: r.index}
	if r.kind == refLocal {
		d.Local = true
		d.Index = r.b.reg
		if f.parent.isFinal(r.b) {
			d.ByValue = true
		} else {
			r.b.scope.captured = true
		}
	}
	return ref{
		kind:  refUpval,
		b:     r.b,
		index: f.addUpval(r.b, d),
	}
}

// upvalIndex returns the index of the upvalue of f for the binding b, if f has one.
func (f *fn) upvalIndex(b *binding) (int32, bool) {
	// The upvalues of b form a chain from the innermost function that has one outwards.
	g, i := b.capFn, b.capIndex
	for g != nil && g.depth > f.depth {
		s := g.upvalSaves[i]
		g, i = s.fn, s.index
	}
	if g == f {
		return i, true
	}
	return 0, false
}

// addUpval adds an upvalue described by d for the binding b to f, and returns its index.
func (f *fn) addUpval(b *binding, d bytecode.UpvalDesc) int32 {
	i := int32(len(f.upvals))
	f.upvals = append(f.upvals, b)
	f.upvalSaves = append(f.upvalSaves, capture{
		fn:    b.capFn,
		index: b.capIndex,
	})
	f.f.Upvals = append(f.f.Upvals, d)
	b.capFn = f
	b.capIndex = i
	return i
}

// assigns reports whether the code of the function f, or of a function nested in it, assigns
// to the variable name other than by a declaration, as recorded in [ast.Program.Assigned]. It is
// false for a script, a module or eval code.
func (f *fn) assigns(name string) bool {
	n := f.node
	return n != nil && f.c.assignedIn(name, n.Order-n.Nested, n.Order)
}

// nestedAssigns reports whether the code of a function nested in the function f assigns to the
// variable name. It is false for a script, a module or eval code.
func (f *fn) nestedAssigns(name string) bool {
	n := f.node
	return n != nil && f.c.assignedIn(name, n.Order-n.Nested, n.Order-1)
}

// assignedIn reports whether the own code of a function whose [ast.Function.Order] is from lo
// to hi assigns to the variable name.
func (c *compiler) assignedIn(name string, lo, hi int32) bool {
	orders := c.prog.Assigned[name]
	i, _ := slices.BinarySearch(orders, lo)
	return i < len(orders) && orders[i] <= hi
}

// isFinal reports whether the local binding b of f holds its final value from now on, so that
// a closure created at this point can copy it.
func (f *fn) isFinal(b *binding) bool {
	if f.node == nil || f.anyMutated || !b.initialized || b.alwaysCheck {
		return false
	}
	if f.assigns(b.name) || f.mutated[b.name] {
		return false
	}
	// A parameter can also be declared by var or function declarations, and a catch parameter
	// by var declarations in the catch block (Annex B.3.4), which assign to it.
	if (b.kind == bindVar || b.catch) && f.redeclares(b.name) {
		return false
	}
	return true
}

// redeclares reports whether the function scope of f has a declaration of name other than a
// parameter.
func (f *fn) redeclares(name string) bool {
	for _, d := range f.node.Scope.Decls {
		if d.Name == name && d.Kind != ast.DeclParam {
			return true
		}
	}
	return false
}

// withsAround returns the number of with statements and dynamic scopes of eval code around
// the code being compiled, including those around the enclosing functions.
func (f *fn) withsAround() int {
	n := f.withDepth + f.outerWiths
	if f.eval != nil {
		n += int(f.eval.scope.Depth)
	}
	return n
}

// evalScopesAround reports whether a scope of OpEvalScope can be among the dynamic scopes
// around the code being compiled.
func (f *fn) evalScopesAround() bool {
	for g := f; g != nil; g = g.parent {
		if g.evalLevel > 0 || g.eval != nil && g.eval.scope.VarDepth > 0 {
			return true
		}
	}
	return false
}

// dynDepth returns the number of the innermost dynamic scopes that a reference to the variable
// name searches before its binding.
func (f *fn) dynDepth(name string) int32 {
	if f.withsAround() == 0 || name == "this" || name == "new.target" || name[0] == '#' || name[0] == '%' {
		return 0
	}
	var n int32
	for g := f; g != nil; g = g.parent {
		if _, s := g.lookupLocal(name); s != nil {
			return n + int32(g.withDepth-s.withs)
		}
		n += int32(g.withDepth)
		if g.eval != nil {
			if v, ok := g.eval.vars[name]; ok {
				return n + v.Depth
			}
			return n + g.eval.scope.Depth
		}
	}
	return n
}

// resolveName emits the search of the dynamic scopes for the variable name, and returns the
// register that holds the scope found or undefined. It returns -1 when no dynamic scope can
// hold the variable.
func (f *fn) resolveName(name string) int32 {
	d := f.dynDepth(name)
	if d == 0 {
		return -1
	}
	s := f.newReg()
	f.emit(bytecode.OpResolveName, s, f.key(name), d)
	return s
}

// captureScopes emits what makes the closure dst see the dynamic scopes that are open.
func (f *fn) captureScopes(dst int32) {
	if f.withsAround() > 0 {
		f.emit(bytecode.OpCaptureScopes, dst, 0, 0)
	}
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

// loadVar emits the read of the variable name into dst. orUndef makes an unresolvable
// reference read as undefined.
func (f *fn) loadVar(name string, dst int32, orUndef bool) {
	mark := f.top
	f.loadVarFrom(name, dst, orUndef, f.resolveName(name))
	f.free(mark)
}

// getScopedOp returns the opcode that reads a variable of a dynamic scope in the code being compiled.
func (f *fn) getScopedOp() bytecode.Opcode {
	if f.strict() {
		return bytecode.OpGetScopedStrict
	}
	return bytecode.OpGetScoped
}

// loadVarFrom is loadVar for the variable whose dynamic scope resolveName put in the register scope.
func (f *fn) loadVarFrom(name string, dst int32, orUndef bool, scope int32) {
	end := -1
	if scope >= 0 {
		skip := f.emit(bytecode.OpJumpIfUndef, 0, scope, 0)
		f.emit(f.getScopedOp(), dst, scope, f.key(name))
		end = f.jump()
		f.patch(skip)
	}
	f.loadStatic(name, dst, orUndef)
	if end >= 0 {
		f.patch(end)
	}
}

// loadStatic emits the read of the binding of the variable name outside the dynamic scopes into dst.
func (f *fn) loadStatic(name string, dst int32, orUndef bool) {
	r := f.resolve(name)
	switch r.kind {
	case refLocal:
		if r.b.unset {
			f.checkTDZ(r, name)
			break
		}
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
		switch {
		case orUndef && f.strict():
			op = bytecode.OpGetGlobalOrUndefStrict
		case orUndef:
			op = bytecode.OpGetGlobalOrUndef
		case f.strict():
			op = bytecode.OpGetGlobalStrict
		}
		k := f.site(name)
		f.emit(op, dst, k, 0)
	}
}

// storeVar emits the assignment of the register src to the variable name.
// init marks the store that initializes a declaration, which is allowed for a constant.
func (f *fn) storeVar(n ast.Node, name string, src int32, init bool) {
	mark := f.top
	scope := int32(-1)
	if !init {
		scope = f.resolveName(name)
	}
	f.storeVarTo(n, name, src, init, scope)
	f.free(mark)
}

// storeVarTo is storeVar for the variable whose dynamic scope resolveName put in the register scope.
func (f *fn) storeVarTo(n ast.Node, name string, src int32, init bool, scope int32) {
	if !init {
		f.setRefPos(n, name)
	}
	end := -1
	if scope >= 0 {
		skip := f.emit(bytecode.OpJumpIfUndef, 0, scope, 0)
		op := bytecode.OpSetScoped
		if f.strict() {
			op = bytecode.OpSetScopedStrict
		}
		f.emit(op, src, scope, f.key(name))
		end = f.jump()
		f.patch(skip)
	}
	f.storeStatic(n, name, src, init)
	if end >= 0 {
		f.patch(end)
	}
}

// storeStatic emits the assignment of the register src to the binding of the variable name
// outside the dynamic scopes.
func (f *fn) storeStatic(n ast.Node, name string, src int32, init bool) {
	r := f.resolve(name)
	if r.b != nil && !init {
		switch r.b.kind {
		case bindConst:
			if r.needsCheck() {
				f.checkTDZ(r, name)
			}
			f.emit(bytecode.OpThrowConstAssign, f.key(name), 0, 0)
			return
		case bindFuncName:
			// Assigning to the name of a function expression is an error in strict code only.
			if f.strict() {
				f.emit(bytecode.OpThrowConstAssign, f.key(name), 0, 0)
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
		case f.strict():
			k := f.site(name)
			f.emit(bytecode.OpSetGlobalStrict, src, k, 0)
		default:
			k := f.site(name)
			f.emit(bytecode.OpSetGlobal, src, k, 0)
		}
	}
}

// checkTDZ emits the check that the binding of r is initialized.
func (f *fn) checkTDZ(r ref, name string) {
	switch r.kind {
	case refLocal:
		if r.b.unset {
			t := f.newReg()
			f.emit(bytecode.OpLoadTDZ, t, 0, 0)
			f.emit(bytecode.OpCheckTDZ, t, f.key(name), 0)
			f.free(t)
			break
		}
		f.emit(bytecode.OpCheckTDZ, r.b.reg, f.key(name), 0)
	case refUpval:
		t := f.newReg()
		f.emit(bytecode.OpGetUpval, t, r.index, 0)
		f.emit(bytecode.OpCheckTDZ, t, f.key(name), 0)
		f.free(t)
	}
}

// undeclaredGlobal reports whether name refers to a global variable that the top level of
// the program does not declare.
func (f *fn) undeclaredGlobal(name string) bool {
	if f.resolve(name).kind != refGlobal {
		return false
	}
	return !f.c.lookupTopDecl(name).plain
}

// globalLexical reports whether name is a let, const or class declaration of the top level of the script.
func (c *compiler) globalLexical(name string) bool {
	return c.lookupTopDecl(name).lexical
}

// localReg returns the register of the variable name when it is a local of f that can be
// read without a check, and -1 otherwise.
func (f *fn) localReg(name string) int32 {
	if f.dynDepth(name) > 0 {
		return -1
	}
	b, _ := f.lookupLocal(name)
	if b == nil || b.tdz || b.alwaysCheck {
		return -1
	}
	return b.reg
}

// declareAnnexBGlobal declares the global variable name of a block-level function when it can,
// and records in a register whether evaluating the declaration assigns the function to it.
// flags is a set of the bytecode GlobalVar flags.
func (f *fn) declareAnnexBGlobal(name string, flags int32) {
	r := f.newReg()
	f.emit(bytecode.OpDeclareAnnexB, r, f.key(name), flags)
	if f.annexBRegs == nil {
		f.annexBRegs = map[string]int32{}
	}
	f.annexBRegs[name] = r
}

func (c *compiler) compileScript() *bytecode.Func {
	prog := c.prog
	f := c.newFn(nil, nil, "")
	f.isScript = true
	f.f.IsScript = true
	f.f.Strict = prog.Strict
	f.f.Source = prog.Source
	f.anyMutated = prog.Scope.ContainsDirectEval

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
			f.emit(bytecode.OpCheckGlobalDecl, f.key(d.Name), bytecode.DeclLexical, 0)
		case ast.DeclFunction:
			if _, ok := funcs[d.Name]; !ok {
				funcOrder = append(funcOrder, d.Name)
			}
			funcs[d.Name] = d.Func
		}
	}
	for _, name := range funcOrder {
		f.emit(bytecode.OpCheckGlobalDecl, f.key(name), bytecode.DeclFunction, 0)
	}
	vars := map[string]bool{}
	for _, d := range prog.Scope.Decls {
		if d.Kind == ast.DeclVar {
			vars[d.Name] = true
			if _, ok := funcs[d.Name]; !ok {
				f.emit(bytecode.OpCheckGlobalDecl, f.key(d.Name), bytecode.DeclVar, 0)
			}
		}
	}
	for _, d := range prog.Scope.Decls {
		if d.Kind == ast.DeclAnnexBVar && funcs[d.Name] == nil && !vars[d.Name] {
			f.declareAnnexBGlobal(d.Name, 0)
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
		f.emit(bytecode.OpDeclareGlobalVar, f.key(name), bytecode.GlobalVarFunc, t)
		f.free(t)
	}
	for _, d := range prog.Scope.Decls {
		if d.Kind == ast.DeclVar {
			if _, ok := funcs[d.Name]; !ok {
				f.emit(bytecode.OpDeclareGlobalVar, f.key(d.Name), 0, 0)
			}
		}
	}

	f.stmts(prog.Body)
	f.emit(bytecode.OpReturn, f.completion, 0, 0)
	f.f.NumClear = 0
	return f.finish()
}
