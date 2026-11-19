// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package bytecode defines the instructions that the compiler emits and the interpreter executes.
package bytecode

import (
	"sort"

	"github.com/hajimehoshi/ekuma/internal/srcpos"
)

// Opcode is the operation of an instruction.
type Opcode uint8

// The comments of the opcodes give the meaning of the operands a, b and c, with these
// conventions:
//
//   - An operand is a register unless its comment says otherwise. An operand that may name no
//     register is -1 then.
//   - K is an index into [Func.Consts] and N an index into [Func.Keys].
//   - A site is an index into [Func.Keys] that also names an inline cache of its own.
//   - A flag is 0 or 1.
//   - An instruction writes the value that it produces to a. A jump has its target in a, or in
//     c when it also produces a value.
//   - A property read takes the object and the key in b and c, and a property store takes the
//     object, the key and the value in a, b and c. A variable store takes the value in a.
//   - An instruction that behaves differently in strict code has a variant for strict code,
//     whose name ends in Strict.
//   - An instruction whose operands do not fit in a, b and c is followed by an [OpExtra] with
//     the rest, written x.a and x.b.
const (
	// Loads and moves.
	OpLoadK     Opcode = iota // a = Consts[b]
	OpLoadInt                 // a = the integer b
	OpLoadUndef               // a = undefined
	OpLoadNull                // a = null
	OpLoadTrue                // a = true
	OpLoadFalse               // a = false
	OpLoadTDZ                 // a = the marker of an uninitialized binding
	OpMove                    // a = b

	// Checks and errors.
	OpCheckTDZ         // throw a ReferenceError naming Keys[b] when a is uninitialized
	OpRequireCoercible // throw a TypeError when a is undefined or null
	OpThrowConstAssign // throw the TypeError for assigning to the constant Keys[a]
	OpThrowError       // throw an error of the Throw kind a with the message Consts[b]

	// Global variables.
	OpGetGlobal              // a = the global variable Keys[b]; b is a site
	OpGetGlobalStrict        // like [OpGetGlobal], in strict code
	OpGetGlobalOrUndef       // like [OpGetGlobal], but undefined when the global does not exist
	OpGetGlobalOrUndefStrict // like [OpGetGlobalOrUndef], in strict code
	OpSetGlobal              // the global variable Keys[b] = a, creating it when it does not exist; b is a site
	OpSetGlobalStrict        // like [OpSetGlobal], but a ReferenceError when the global does not exist
	OpResolveGlobal          // a = whether the global variable Keys[b] exists; b is a site
	OpDeleteName             // a = delete the global variable Keys[b]
	OpDeclareGlobalVar       // declare the global variable Keys[a]; b is a set of GlobalVar flags, and c holds the function with GlobalVarFunc and is -1 otherwise
	OpDeclareGlobalLex       // declare the global lexical binding Keys[a], constant with the flag b
	OpInitGlobalLex          // initialize the global lexical binding Keys[b] with a
	OpCheckGlobalDecl        // throw when declaring Keys[a] at the top level would conflict; b is one of the Decl kinds
	OpDeclareAnnexB          // a = whether the global variable Keys[b] of a block-level function is declared; c is a set of GlobalVar flags

	// Upvalues and closures.
	OpGetUpval    // a = upvalue b
	OpSetUpval    // upvalue b = a
	OpCloseUpvals // detach the upvalues that refer to the registers from a on
	OpClosure     // a = closure of Protos[b]

	// The values that the running function provides.
	OpLoadThis       // a = this
	OpLoadThisSloppy // a = this, converted the way a sloppy function sees it
	OpLoadCallee     // a = the running function
	OpLoadNewTarget  // a = new.target
	OpLoadHome       // a = the home object of the running function
	OpArguments      // a = the arguments object, mapped to the parameters with the flag b
	OpRestArgs       // a = the arguments from index b on, as an array

	// Properties.
	OpGetProp          // a = b.Keys[c]; c is a site
	OpGetLength        // a = b.length; c is a site
	OpGetIndex         // a = b[c]
	OpGetIndexInt      // a = b[the integer c]
	OpSetProp          // a.Keys[b] = c; b is a site
	OpSetPropStrict    // like [OpSetProp], in strict code
	OpSetIndex         // a[b] = c
	OpSetIndexStrict   // like [OpSetIndex], in strict code
	OpDeleteProp       // a = delete b[c]
	OpDeletePropStrict // like [OpDeleteProp], in strict code
	OpIn               // a = b in c
	OpInstanceof       // a = b instanceof c
	OpToPropertyKey    // a = b converted to a property key. c is -1, or the object of a compound assignment to its property b: then a key that is an integer from 0 to 2^32-1 stays a number, and a key that needs converting first throws a TypeError when c is undefined or null

	// Super properties.
	OpSuperBase      // a = the super base of the home object b, which is its prototype
	OpGetSuper       // a = super[c] with the this value b; x.a is the home object, or with the flag x.b the super base, and then c is converted to a property key in place
	OpSetSuper       // super[b] = c with the this value a; x.a is the super base
	OpSetSuperStrict // like [OpSetSuper], in strict code

	// Private names.
	OpHasPrivate // a = the private name b is in c
	OpGetPrivate // a = b.#c, where c holds the private name
	OpSetPrivate // a.#b = c, where b holds the private name

	// Arithmetic.
	OpAdd    // a = b + c
	OpSub    // a = b - c
	OpMul    // a = b * c
	OpDiv    // a = b / c
	OpMod    // a = b % c
	OpExp    // a = b ** c
	OpBitOr  // a = b | c
	OpBitAnd // a = b & c
	OpBitXor // a = b ^ c
	OpShl    // a = b << c
	OpShr    // a = b >> c
	OpUShr   // a = b >>> c

	// Arithmetic with a constant number Consts[c], which is an int32 for a bitwise operator and
	// below 32 for a shift. [Opcode.RegOperand] gives the opcode that takes a register instead.
	OpAddK    // a = b + Consts[c]
	OpSubK    // a = b - Consts[c]
	OpMulK    // a = b * Consts[c]
	OpDivK    // a = b / Consts[c]
	OpModK    // a = b % Consts[c]
	OpBitOrK  // a = b | Consts[c]
	OpBitAndK // a = b & Consts[c]
	OpShlK    // a = b << Consts[c]
	OpShrK    // a = b >> Consts[c]
	OpUShrK   // a = b >>> Consts[c]

	// Unary operators and conversions.
	OpNeg       // a = -b
	OpPlus      // a = +b
	OpNot       // a = !b
	OpBitNot    // a = ~b
	OpTypeof    // a = typeof b
	OpInc       // a = b + 1, where b is already numeric
	OpDec       // a = b - 1, where b is already numeric
	OpToNumeric // a = b converted to a number or BigInt
	OpToString  // a = b converted to a string
	OpConcat    // a = the concatenation of the c strings from b on

	// Comparisons.
	OpLt       // a = b < c
	OpLe       // a = b <= c
	OpGt       // a = b > c
	OpGe       // a = b >= c
	OpEq       // a = b == c
	OpNe       // a = b != c
	OpStrictEq // a = b === c
	OpStrictNe // a = b !== c

	// Jumps.
	OpJump             // pc = a
	OpJumpIf           // if b then pc = a
	OpJumpIfNot        // if !b then pc = a
	OpJumpIfNullish    // if b is null or undefined then pc = a
	OpJumpIfNotNullish // if b is neither null nor undefined then pc = a
	OpJumpIfUndef      // if b is undefined then pc = a
	OpJumpIfNotUndef   // if b is not undefined then pc = a
	OpJumpIfInt        // if b is the integer c then pc = a

	// Jumps when a comparison fails. The constant Consts[c] of the K forms is a number.
	OpJumpNotLt   // if !(b < c) then pc = a
	OpJumpNotLe   // if !(b <= c) then pc = a
	OpJumpNotGt   // if !(b > c) then pc = a
	OpJumpNotGe   // if !(b >= c) then pc = a
	OpJumpNotEq   // if !(b == c) then pc = a
	OpJumpNotNe   // if !(b != c) then pc = a
	OpJumpNotSEq  // if !(b === c) then pc = a
	OpJumpNotSNe  // if !(b !== c) then pc = a
	OpJumpNotLtK  // if !(b < Consts[c]) then pc = a
	OpJumpNotLeK  // if !(b <= Consts[c]) then pc = a
	OpJumpNotGtK  // if !(b > Consts[c]) then pc = a
	OpJumpNotGeK  // if !(b >= Consts[c]) then pc = a
	OpJumpNotSEqK // if !(b === Consts[c]) then pc = a
	OpJumpNotSNeK // if !(b !== Consts[c]) then pc = a

	// Calls. The callee is in a and the this value in a+1, and the result replaces the callee.
	OpCall       // a = a(this a+1, args a+2...), b arguments
	OpCallCall   // a = a.call(...) where a+1 is the function and a+2... the b arguments of call, the first of which is the this value
	OpApplyFwd   // a = a.apply(...) where a+1 is the function, a+2 the this value, forwarding the arguments of the running function; the flag b says whether an arguments object made for them is mapped
	OpCallFwd    // a = a(this a+1, the arguments of the running function)
	OpCallSpread // a = a(this a+1, ...the array a+2)
	OpCallEval   // like [OpCall]; a direct eval when a is the eval function. c indexes [Func.EvalScopes]. With b = -1, a+2 is the array of the arguments: a direct eval skips the next instruction, which is the [OpCallSpread] for any other function
	OpSpreadArgs // a = an array of the elements of the iterable b, which is b itself when iterating b runs no JavaScript; only the spread call that follows reads it

	// The calls in tail position of strict functions. When the callee is a function written in
	// JavaScript and the running frame can be replaced, the callee's frame replaces it, and the
	// callee returns to the caller of the running function. A callee that is Function.prototype.call
	// stands for its this value. Otherwise they are the ordinary calls, and the next instructions
	// return the result.
	OpTailCall       // like [OpCall]
	OpTailCallSpread // like [OpCallSpread]
	OpTailCallEval   // like [OpCallEval], where the instruction after one with b = -1 is [OpTailCallSpread]
	OpTailApplyFwd   // like [OpApplyFwd]
	OpTailCallFwd    // like [OpCallFwd]

	// Constructor calls.
	OpNew             // a = new a(args a+2...), b arguments; c is a site for the prototype property of a
	OpNewSpread       // a = new a(...the array a+2)
	OpSuperCall       // a = super(args a+2...), b arguments, where a is the super constructor and a+1 new.target
	OpSuperSpread     // a = super(...the array a+2)
	OpCheckThisUninit // throw when a is initialized: super was called twice

	// Returns and exceptions.
	OpReturn        // return a
	OpReturnUndef   // return undefined
	OpReturnDerived // return a from a derived constructor whose this is in b
	OpThrow         // throw a
	OpRethrow       // throw a again, as the completion of a finally block; when a makes a generator return and b is not -1, throw b instead

	// Generators and async functions.
	OpInitialYield        // suspend a generator before its body
	OpYield               // suspend the generator with the value b; a receives the value sent on resume. The flag c, set for yield*, leaves every resume kind to the code that follows and passes the result b through
	OpAwait               // suspend until the promise b settles; a receives its value
	OpYieldDelegateResult // a = the value of the iterator result b; jump to c when it is not done, yielding b as it is
	OpGenResumeMode       // a = the Resume kind with which the generator was resumed

	// Literals.
	OpNewObject       // a = {}, with room for c properties
	OpNewArray        // a = [], with room for c elements
	OpArrayPush       // append b to the array a
	OpArrayHole       // append a hole to the array a
	OpArraySpread     // append the elements of the iterable b to the array a
	OpCopyProps       // copy the own enumerable properties of b to a, as an object spread
	OpCopyPropsExcept // a = an object with the own enumerable properties of b except the x.a keys from c on
	OpSetProto        // set the prototype of the object literal a to b when b is an object or null
	OpTemplate        // a = the template object b of the function
	OpRegExp          // a = a new RegExp object for the literal RegExps[b]
	OpRegExpArg       // a+2, or a+1 with the flag c, = the RegExp object for the literal RegExps[b] as the first argument or this value of the call of a that follows
	OpRegExpMethod    // a = the property Keys[c] of a RegExp object a+1 for the literal RegExps[b]; c is a site. [OpRegExpArg] with c = 1 replaces a+1 before the call

	// Definitions of properties and classes.
	OpDefineField  // define the enumerable data property b of a as c
	OpDefineFieldK // define the enumerable data property Keys[b] of a as c; b is a site
	OpDefineMethod // define the non-enumerable method b of a as c, setting its home object
	OpDefineGetter // define the getter c for the property b of a, enumerable with the flag x.a
	OpDefineSetter // define the setter c for the property b of a, enumerable with the flag x.a
	OpSetFuncName  // set the name of the anonymous function a to the property key b, with the NamePrefix kind c
	OpSetHome      // set the home object of the function a to b
	OpClass        // make the function a a class constructor with the parent c, or with no extends clause when c is -1; a+1 receives the prototype
	OpPrivateName  // a = a new private name described by Keys[b]; the flag c says whether it names a method
	OpInitFields   // define the instance elements of the class a on the instance b
	OpAddField     // add the class element with the key b and the value, initializer or initial value in c to the class a; x.a is an Elem kind
	OpAutoAccessor // a, a+1, a+2 = the backing private name, the getter and the setter of an auto-accessor with the key b
	OpDecorate     // a step of evaluating a class with decorators; x.a is the Dec kind, which gives the operands

	// Iteration.
	OpForIn           // a = the for-in iterator over b
	OpForInNext       // a = the next key of the for-in iterator b; jump to c when it is exhausted
	OpGetIterator     // a, a+1 = the iterator of b and its next method; c is one of the Iter kinds
	OpIterNext        // a = the next value of the iterator b, b+1; jump to c when it is done
	OpIterClose       // close the iterator a, a+1, and set a to undefined; b is -1 or holds the pending exception, which is kept unless it makes a generator return
	OpIterResult      // a = the iterator result object with the value b and the done flag c
	OpIterCall        // a, a+1 = the result of calling the method Keys[c] of the iterator b with the argument a and true, or undefined and false when there is no such method
	OpIterReturn      // a, a+1 = the result of calling the return method of the iterator b without arguments and true, or undefined and false when there is none; b becomes undefined
	OpIterCheckResult // throw a TypeError when the iterator result a is not an object
	OpIterResultValue // a = the value of the iterator result b; jump to c when it is done

	// Disposal of resources.
	OpUsing        // add the resource a with the Dispose hint c to the dispose capability b, which is created when b is undefined
	OpDisposeBegin // start disposing the capability a for the completion b, which is a throw when c is the integer 1
	OpDisposeStep  // a = the next value to await in disposing the capability b; jump to c when there is none
	OpDisposeError // record that disposing a resource of the capability a threw b
	OpDisposeEnd   // end disposing the capability a and throw its completion when it is a throw; a becomes undefined

	// Dynamic scopes, which with statements and direct evals open.
	OpWith            // push the object a as a with scope
	OpPopScope        // pop the innermost dynamic scope
	OpResolveName     // a = the innermost of the c innermost dynamic scopes that has the variable Keys[b], or undefined
	OpGetScoped       // a = the variable Keys[c] of the dynamic scope b
	OpGetScopedStrict // like [OpGetScoped], in strict code
	OpSetScoped       // the variable Keys[c] of the dynamic scope b = a
	OpSetScopedStrict // like [OpSetScoped], in strict code
	OpDeleteScoped    // a = delete the variable Keys[c] of the dynamic scope b
	OpCaptureScopes   // the closure a sees the dynamic scopes that are open
	OpEvalScope       // push a new scope for the variables that direct evals declare
	OpScopeThis       // a = the this value of a call of a function found in the dynamic scope b
	OpDeclareEvalVar  // declare the variable Keys[a] in the b-th innermost dynamic scope, a scope of [OpEvalScope]; it is a function when c is not -1, and c holds it

	// Modules.
	OpImport // a = the promise of a dynamic import of b with the options c in the [Phase] c+1, an integer; with b = -1, a = import.meta

	// Others.
	OpDebugger // do nothing, for a debugger statement
	OpExtra    // the operands of the instruction before it that do not fit in it; it is never executed
)

var regOperandOps = [...]Opcode{
	OpAddK - OpAddK:    OpAdd,
	OpSubK - OpAddK:    OpSub,
	OpMulK - OpAddK:    OpMul,
	OpDivK - OpAddK:    OpDiv,
	OpModK - OpAddK:    OpMod,
	OpBitOrK - OpAddK:  OpBitOr,
	OpBitAndK - OpAddK: OpBitAnd,
	OpShlK - OpAddK:    OpShl,
	OpShrK - OpAddK:    OpShr,
	OpUShrK - OpAddK:   OpUShr,
}

// RegOperand returns the opcode that takes a register in place of the constant of op, one of
// [OpAddK] through [OpUShrK].
func (op Opcode) RegOperand() Opcode {
	return regOperandOps[op-OpAddK]
}

// The kinds of error that [OpThrowError] throws.
const (
	ThrowTypeError = iota
	ThrowReferenceError
)

// The flags of [OpDeclareGlobalVar].
const (
	// GlobalVarFunc declares a function.
	GlobalVarFunc = 1 << iota

	// GlobalVarDeletable declares a binding of eval code, which can be deleted.
	GlobalVarDeletable
)

// The kinds of top-level declaration that [OpCheckGlobalDecl] checks.
const (
	DeclVar = iota
	DeclLexical
	DeclFunction
)

// The ways a generator is resumed, as [OpGenResumeMode] reports them.
const (
	ResumeNext = iota
	ResumeThrow
	ResumeReturn
)

// The prefixes that [OpSetFuncName] adds to a function name.
const (
	NamePrefixNone = iota
	NamePrefixGet  // "get "
	NamePrefixSet  // "set "
)

// The kinds of class element that [OpAddField] adds. [ElemStatic] may be combined with any of the
// others, and [ElemValue] with [ElemField].
const (
	ElemField = iota
	ElemMethod
	ElemGetter
	ElemSetter
	ElemStatic = 8
	// ElemValue makes c the initial value of the field instead of its initializer.
	ElemValue = 16
)

// The kinds of class element that only classes with decorators add with [OpDecorate].
const (
	ElemAccessor = ElemSetter + 1 + iota
	ElemStaticBlock
)

// The steps of [OpDecorate]. a holds the state of the class evaluation.
const (
	// DecBegin sets c to a new state for the class with the name b.
	DecBegin = iota
	// DecAdd adds the decorator b with the receiver c to the decorators of the next element.
	DecAdd
	// DecClass makes the decorators added so far the decorators of the class.
	DecClass
	// DecElement adds the class element with the key b and the value or initializer c,
	// which is -1 when there is none; x.b is its Elem kind.
	DecElement
	// DecApply applies the decorators of the elements, defines the elements on the class b
	// and its prototype, and sets c to the result of the decorators of the class.
	DecApply
	// DecFinish runs the static elements and the initializers of the class b.
	DecFinish
)

// The hints of the resources that [OpUsing] adds.
const (
	DisposeSync = iota
	DisposeAsync
)

// The kinds of iterator that [OpGetIterator] gets.
const (
	IterSync     = iota // a sync iterator, or a plain array to be iterated by index
	IterAsync           // an async iterator
	IterSyncNext        // a sync iterator whose next method can be called directly
)

// Instr is one instruction.
type Instr struct {
	Op      Opcode
	A, B, C int32
}

// Handler is a region of code whose exceptions are caught.
type Handler struct {
	Start, End int32
	Target     int32

	// Reg receives the thrown value.
	Reg int32

	// Finally is set for a handler that also intercepts a generator being closed.
	Finally bool

	// Scopes is the number of dynamic scopes that are open at the target.
	Scopes int32
}

// UpvalDesc says where a closure finds a variable of an enclosing function.
type UpvalDesc struct {
	// Local says whether [UpvalDesc.Index] is a register of the enclosing function or one of its
	// upvalues.
	Local bool
	Index int32

	// ByValue says that the register [UpvalDesc.Index] of the enclosing function holds the
	// final value of the variable whenever the closure is created, so that the closure can keep
	// a copy instead of sharing the variable. It is only set with [UpvalDesc.Local].
	ByValue bool
}

// Pos says that the instructions from [Pos.PC] on belong to the source position at the byte offset
// [Pos.Offset] of the text of [Func.Lines].
type Pos struct {
	PC     int32
	Offset int32
}

// ConstKind is the type of a constant.
type ConstKind uint8

const (
	ConstNumber ConstKind = iota
	ConstString
	ConstBigInt
)

// Const is a constant of a function. Strings are in WTF-8, and a BigInt is its decimal digits in
// [Const.Str].
type Const struct {
	Kind ConstKind
	Num  float64
	Str  string
}

// Template is the site of a tagged template. A cooked string is absent where [Template.HasCooked]
// is false.
type Template struct {
	Cooked    []string
	HasCooked []bool
	Raw       []string
}

// RegExp is a regular expression literal.
type RegExp struct {
	Pattern string
	Flags   string
}

// Func is the compiled code of a function.
type Func struct {
	Name string
	File string

	// Source is the text of the function.
	Source string

	Code     []Instr
	Consts   []Const
	Handlers []Handler
	Upvals   []UpvalDesc
	Protos   []*Func

	// Keys are the property names and variable names that instructions refer to, in WTF-8.
	Keys []string

	// NumCaches is the number of inline caches that the instructions refer to.
	NumCaches int

	Templates []Template

	RegExps []RegExp

	// EvalScopes describe the call sites of direct evals, which [OpCallEval] refers to.
	EvalScopes []EvalScope

	// Positions maps instructions to source positions, ordered by PC.
	Positions []Pos

	// Lines converts the offsets of [Func.Positions] into lines and columns. It may be nil when
	// there are no positions.
	Lines *srcpos.Table

	// Length is the value of the length property of the function.
	Length int

	// NumParams is the number of leading registers that receive arguments.
	NumParams int

	// ShadowedParams marks, by index, the parameters whose name a later parameter repeats.
	// It is nil when no name repeats.
	ShadowedParams []bool

	// NumClear is the number of leading registers that must not hold stale values when the function starts.
	NumClear int
	NumRegs  int

	Strict      bool
	IsArrow     bool
	IsEval      bool
	IsGenerator bool
	IsAsync     bool
	IsMethod    bool

	// IsScript says whether the code is the top level of a script.
	IsScript bool

	// IsConstructor says whether closures of the function can be called with new.
	IsConstructor      bool
	IsClassConstructor bool
	IsDerived          bool

	// NeedsExtra is set when the function reads the arguments beyond its parameters.
	NeedsExtra bool
}

// EvalScope is what eval code sees at the call site of a direct eval.
type EvalScope struct {
	// Strict says whether the calling code is strict.
	Strict bool

	// The constructs that the calling context allows in eval code.
	NewTarget     bool
	SuperProperty bool
	SuperCall     bool
	InFieldInit   bool

	// Vars are the variables visible at the call, one for each name, sorted by name.
	Vars []EvalVar

	// Depth is the number of dynamic scopes open at the call.
	Depth int32

	// LazyArguments says whether the variable arguments, when it is a register of the calling
	// function, holds the TDZ marker until a direct eval that refers to it creates the mapped
	// arguments object of the calling function.
	LazyArguments bool

	// VarDepth says which of the dynamic scopes, counted from the innermost one starting at 1,
	// is the scope of [OpEvalScope] that the var declarations of non-strict eval code go to.
	// It is 0 when they go to the global object.
	VarDepth int32
}

// EvalVarKind is the kind of a variable that eval code sees.
type EvalVarKind uint8

const (
	EvalVarVar EvalVarKind = iota
	EvalVarLet
	EvalVarConst
	EvalVarClass

	// EvalVarFuncName is the name of a named function expression inside that function.
	EvalVarFuncName

	// EvalVarPseudo is a value that the engine provides, such as this, or a private name.
	EvalVarPseudo
)

// EvalPlace says where a variable is relative to the variable environment of non-strict eval code.
type EvalPlace uint8

const (
	// EvalPlaceOuter is a variable outside the variable environment.
	EvalPlaceOuter EvalPlace = iota

	// EvalPlaceVarEnv is a var-scoped variable of the variable environment itself.
	EvalPlaceVarEnv

	// EvalPlaceInner is a variable of a scope between the call and the variable environment.
	EvalPlaceInner

	// EvalPlaceCatch is a catch parameter between the call and the variable environment.
	EvalPlaceCatch
)

// EvalVar is a variable that eval code sees.
type EvalVar struct {
	Name  string
	Kind  EvalVarKind
	Place EvalPlace

	// Check says whether a read can find the variable uninitialized.
	Check bool

	// Local and [EvalVar.Index] say where the variable is, as in [UpvalDesc].
	Local bool
	Index int32

	// Depth is the number of dynamic scopes that a reference searches before the variable.
	Depth int32
}

// Position returns the 1-based source line and column of the instruction at pc,
// or zeros when it has none.
func (f *Func) Position(pc int) (line, column int) {
	i := sort.Search(len(f.Positions), func(i int) bool {
		return int(f.Positions[i].PC) > pc
	}) - 1
	if i < 0 {
		return 0, 0
	}
	return f.Lines.Position(f.Positions[i].Offset)
}

// Module describes what a module imports and exports, and the bindings of its top level.
//
// The compiled code of a module is a function that reaches every binding of the top level as an
// upvalue: an [UpvalDesc] of the module function with Local set refers to [Module.Bindings] by
// index.
type Module struct {
	// Requests are the [[RequestedModules]], in source order without duplicates.
	Requests []ModuleRequest

	// Bindings are the bindings of the top level, including the imported ones.
	Bindings []ModuleBinding

	// Imports are the import entries, except those of namespace imports.
	Imports []ImportEntry

	// LocalExports, [Module.IndirectExports] and [Module.StarExports] are the export entries.
	LocalExports    []LocalExport
	IndirectExports []IndirectExport
	StarExports     []int32

	// Funcs are the function declarations of the top level, which are instantiated before the
	// module is evaluated.
	Funcs []ModuleFunc

	// HasTLA says whether the top level contains await.
	HasTLA bool
}

// ModuleRequest is a ModuleRequest Record. [ModuleRequest.Attributes] are sorted by key.
type ModuleRequest struct {
	Specifier  string
	Attributes []ImportAttribute
	Phase      Phase
}

// Phase is the phase of an import.
type Phase uint8

const (
	PhaseEvaluation Phase = iota
	PhaseDefer
	PhaseSource
)

// ImportAttribute is an ImportAttribute Record.
type ImportAttribute struct {
	Key   string
	Value string
}

// ModuleBindingKind is the kind of a binding of the top level of a module.
type ModuleBindingKind uint8

const (
	// ModuleVar is a var declaration, initialized to undefined before evaluation.
	ModuleVar ModuleBindingKind = iota

	// ModuleLexical is a let, const, class or using declaration, or the default export of an
	// expression, which is uninitialized until its declaration is evaluated.
	ModuleLexical

	// ModuleFunction is a function declaration.
	ModuleFunction

	// ModuleImport is a binding of the binding that another module exports.
	ModuleImport

	// ModuleNamespace is a namespace import, initialized to the namespace object of Request.
	ModuleNamespace

	// ModuleDeferredNamespace is a deferred namespace import, initialized to the deferred
	// namespace object of Request.
	ModuleDeferredNamespace

	// ModuleSource is a source phase import, initialized to the module source of Request.
	ModuleSource
)

// ModuleBinding is a binding of the top level of a module.
type ModuleBinding struct {
	Name string
	Kind ModuleBindingKind

	// Request indexes [Module.Requests] for [ModuleNamespace], [ModuleDeferredNamespace] and
	// [ModuleSource].
	Request int32
}

// ImportEntry is an ImportEntry Record of a named or default import, which binds Bindings[Binding].
type ImportEntry struct {
	Request    int32
	ImportName string
	Binding    int32
}

// LocalExport exports Bindings[Binding] as [LocalExport.Name].
type LocalExport struct {
	Name    string
	Binding int32
}

// IndirectExport exports as [IndirectExport.Name] the export [IndirectExport.ImportName] of
// Requests[Request], its namespace object when [IndirectExport.All] is set, or its module
// source when [IndirectExport.Source] is set.
type IndirectExport struct {
	Name       string
	Request    int32
	ImportName string
	All        bool
	Source     bool
}

// ModuleFunc says that the closure of Protos[Proto] of the module function initializes Bindings[Binding].
type ModuleFunc struct {
	Proto   int32
	Binding int32
}
