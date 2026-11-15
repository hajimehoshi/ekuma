// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package bytecode defines the instructions that the compiler emits and the interpreter executes.
package bytecode

import (
	"sort"
)

// Opcode is the operation of an instruction.
type Opcode uint8

// The comments give the operands. Registers are written a, b and c; K means an index into
// the constants, N an index into the property keys, and "cache" an index into the inline caches.
const (
	OpLoadK            Opcode = iota // a = Consts[b]
	OpLoadInt                        // a = the integer b
	OpLoadUndef                      // a = undefined
	OpLoadNull                       // a = null
	OpLoadTrue                       // a = true
	OpLoadFalse                      // a = false
	OpLoadTDZ                        // a = the marker of an uninitialized binding
	OpMove                           // a = b
	OpCheckTDZ                       // throw a ReferenceError naming Keys[b] when a is uninitialized
	OpThrowConstAssign               // throw the TypeError for assigning to the constant Keys[a]
	OpThrowError                     // throw an error of the Throw kind a with the message Consts[b]

	OpGetGlobal        // a = global Keys[b], cache c
	OpGetGlobalOrUndef // like [OpGetGlobal], but undefined when the global does not exist
	OpSetGlobal        // global Keys[b] = a, cache c; creates the global when it does not exist
	OpSetGlobalStrict  // like [OpSetGlobal], but a ReferenceError when the global does not exist
	OpDeclareGlobalVar // declare the global variable Keys[a]; b is a set of GlobalVar flags, and c holds the function
	OpDeclareGlobalLex // declare the global lexical binding Keys[a], constant when b is set
	OpInitGlobalLex    // initialize the global lexical binding Keys[b] with a
	OpCheckGlobalDecl  // throw when declaring Keys[a] at the top level would conflict; b is one of the Decl kinds
	OpDeclareAnnexB    // a = whether the global variable Keys[b] of a block-level function is declared; c is a set of GlobalVar flags

	OpGetUpval    // a = upvalue b
	OpSetUpval    // upvalue b = a
	OpCloseUpvals // detach the upvalues that refer to the registers from a on
	OpClosure     // a = closure of Protos[b]

	OpLoadThis       // a = this
	OpLoadThisSloppy // a = this, converted the way a sloppy function sees it
	OpLoadCallee     // a = the running function
	OpLoadNewTarget  // a = new.target
	OpLoadHome       // a = the home object of the running function
	OpArguments      // a = the arguments object; b says whether it is mapped to the parameters
	OpRestArgs       // a = the arguments from index b on, as an array

	OpGetProp // a = b.Keys[c], cache c
	OpSetProp // a.Keys[b] = c, cache b
	OpSetPropStrict
	OpGetLength // a = b.length, cache c
	OpGetIndex  // a = b[c]
	OpSetIndex  // a[b] = c
	OpSetIndexStrict
	OpGetIndexInt   // a = b[the integer c]
	OpSuperBase     // a = the super base of the home object b, which is its prototype
	OpGetSuper      // a = super[b], with this c; the next instruction's a is the home object, or the super base with the key converted in place when its b is nonzero
	OpSetSuper      // super[a] = b, with this c; the next instruction's a says whether the code is strict, and its b is the super base
	OpDeleteProp    // a = delete b[c]; the next instruction's a says whether the code is strict
	OpIn            // a = b in c
	OpInstanceof    // a = b instanceof c
	OpHasPrivate    // a = the private name b is in c
	OpGetPrivate    // a = b.#c, where c holds the private name
	OpSetPrivate    // a.#b = c, where b holds the private name
	OpToPropertyKey // a = b converted to a property key; when c is nonzero, a TypeError is thrown first if c-1 is undefined or null

	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpExp
	OpBitOr
	OpBitAnd
	OpBitXor
	OpShl
	OpShr
	OpUShr
	OpAddK // a = b + Consts[c], which is a number
	OpSubK
	OpMulK
	OpDivK
	OpModK
	OpBitOrK
	OpBitAndK
	OpShlK
	OpShrK
	OpUShrK
	OpNeg
	OpPlus // a = +b
	OpNot
	OpBitNot
	OpToNumeric // a = b converted to a number or BigInt
	OpToString  // a = b converted to a string
	OpTypeof
	OpInc // a = b + 1, where b is already numeric
	OpDec // a = b - 1, where b is already numeric

	OpLt
	OpLe
	OpGt
	OpGe
	OpEq
	OpNe
	OpStrictEq
	OpStrictNe

	OpJump          // pc = a
	OpJumpIf        // if b then pc = a
	OpJumpIfNot     // if !b then pc = a
	OpJumpIfNullish // if b is null or undefined then pc = a
	OpJumpIfNotNullish
	OpJumpIfUndef    // if b is undefined then pc = a
	OpJumpIfNotUndef // if b is not undefined then pc = a
	OpJumpNotLt      // if !(b < c) then pc = a
	OpJumpNotLe
	OpJumpNotGt
	OpJumpNotGe
	OpJumpNotEq
	OpJumpNotNe
	OpJumpNotSEq
	OpJumpNotSNe
	OpJumpNotLtK // if !(b < Consts[c]) then pc = a, where Consts[c] is a number
	OpJumpNotLeK
	OpJumpNotGtK
	OpJumpNotGeK
	OpJumpNotSEqK
	OpJumpNotSNeK
	OpJumpIfInt // if b is the integer c then pc = a

	OpCall          // a = a(this a+1, args a+2...), b arguments
	OpCallCall      // a = a.call(...) where a+1 is the function, a+2 the this value and a+3... b arguments
	OpApplyFwd      // a = a.apply(...) where a+1 is the function, a+2 the this value, forwarding the arguments of the running function; b says whether an arguments object made for them is mapped
	OpCallFwd       // a = a(this a+1, the arguments of the running function)
	OpCallSpread    // a = a(this a+1, ...the array a+2)
	OpCallEval      // like [OpCall]; a direct eval when a is the eval function. c indexes [Func.EvalScopes]. With b = -1, a+2 is the array of the arguments: a direct eval skips the next instruction, which is the [OpCallSpread] for any other function
	OpNew           // a = new a(args a+2...), b arguments, cache c
	OpNewSpread     // a = new a(...the array a+2)
	OpSuperCall     // a = super(args a+2...), b arguments, where a is the running constructor and a+1 new.target
	OpSuperSpread   // a = super(...the array a+2)
	OpReturn        // return a
	OpReturnUndef   // return undefined
	OpReturnDerived // return a from a derived constructor whose this is in b
	OpThrow         // throw a
	OpRethrow       // throw a again, as the completion of a finally block; when b is nonzero and a makes a generator return, throw b-1 instead

	OpInitialYield        // suspend a generator before its body
	OpYield               // suspend the generator with the value b; a receives the value sent on resume
	OpAwait               // suspend until the promise b settles; a receives its value
	OpYieldDelegateResult // a = the value of the iterator result b; jump to c when it is not done, yielding b as it is
	OpGenResumeMode       // a = the Resume kind with which the generator was resumed

	OpNewObject        // a = {}, with room for c properties
	OpNewArray         // a = [a+1 ... a+b]
	OpArrayPush        // append b to the array a
	OpArrayHole        // append a hole to the array a
	OpArraySpread      // append the elements of the iterable b to the array a
	OpRegExp           // a = a new RegExp object for the literal RegExps[b]
	OpRegExpArg        // a+2, or a+1 when c is 1, = the RegExp object for the literal RegExps[b] as the first argument or this value of the call of a that follows
	OpRegExpMethod     // a = the property Keys[c] of a RegExp object a+1 for the literal RegExps[b], cache c; [OpRegExpArg] with c = 1 replaces a+1 before the call
	OpTemplate         // a = the template object b of the function
	OpDefineField      // define the enumerable data property b of a as c
	OpDefineFieldK     // define the enumerable data property Keys[b] of a as c, cache b
	OpDefineMethod     // define the non-enumerable method b of a as c, setting its home object
	OpDefineGetter     // define the getter c for the property b of a; the next instruction's a says whether it is enumerable
	OpDefineSetter     // define the setter c for the property b of a
	OpSetFuncName      // set the name of the anonymous function a to the property key b, with the NamePrefix kind c
	OpSetHome          // set the home object of the function a to b
	OpCopyProps        // copy the own enumerable properties of b to a, as an object spread
	OpCopyPropsExcept  // a = an object with the own enumerable properties of b except the keys in c+1...c+N, N in the next instruction's a
	OpSetProto         // set the prototype of the object literal a to b when b is an object or null
	OpClass            // make the function a a class constructor with the parent c; a+1 receives the prototype. c is -1 without an extends clause
	OpPrivateName      // a = a new private name described by Keys[b]; c says whether it names a method
	OpInitFields       // define the instance elements of the class a on the instance b
	OpAddField         // add the class element with the key b and the value or initializer c to the class a; the next instruction's a is an Elem kind
	OpCheckThisUninit  // throw when a is initialized: super was called twice
	OpRequireCoercible // throw a TypeError when a is undefined or null

	OpForIn       // a = the for-in iterator over b
	OpForInNext   // a = the next key of the for-in iterator b, or jump to c when it is exhausted
	OpGetIterator // a, a+1 = the iterator of b and its next method; c is one of the Iter kinds
	OpIterNext    // a = the next value of the iterator b, b+1; jump to c when it is done
	OpIterClose   // close the iterator a, a+1; when b is nonzero, b-1 holds the pending exception, which is kept unless it makes a generator return; a becomes undefined
	OpIterResult  // a = the iterator result object with the value b and done c
	OpIterCall    // a = the result of calling the method Keys[c] of the iterator b with the argument a; a becomes undefined when there is none

	OpIterReturn      // a, a+1 = the result of calling the return method of the iterator b without arguments and true, or undefined and false when there is none; b becomes undefined
	OpIterCheckResult // throw a TypeError when the iterator result a is not an object
	OpIterResultValue // a = the value of the iterator result b; jump to c when it is done

	OpWith          // push the object a as a with scope
	OpPopScope      // pop the innermost dynamic scope
	OpResolveName   // a = the innermost of the c innermost dynamic scopes that has the variable Keys[b], or undefined
	OpGetScoped     // a = the variable Keys[c] of the dynamic scope b
	OpSetScoped     // the variable Keys[c] of the dynamic scope b = a
	OpDeleteScoped  // a = delete the variable Keys[c] of the dynamic scope b
	OpDeleteName    // a = delete the global variable Keys[b]
	OpCaptureScopes // the closure a sees the dynamic scopes that are open

	OpDebugger
	OpNop

	OpUsing        // add the resource a with the Dispose hint c to the dispose capability b, which is created when b is undefined
	OpDisposeBegin // start disposing the capability a for the completion b, which is a throw when c is the integer 1
	OpDisposeStep  // a = the next value to await in disposing the capability b; jump to c when there is none
	OpDisposeError // record that disposing a resource of the capability a threw b
	OpDisposeEnd   // end disposing the capability a and throw its completion when it is a throw; a becomes undefined

	OpEvalScope      // push a new scope for the variables that direct evals declare
	OpScopeThis      // a = the this value of a call of a function found in the dynamic scope b
	OpDeclareEvalVar // declare the variable Keys[a] in the b-th innermost dynamic scope, a scope of [OpEvalScope]; it is a function when c is not -1, and c holds it

	OpImport // a = the promise of a dynamic import of b with the options c in the [Phase] c+1, an integer; with b = -1, a = import.meta

	// The calls in tail position of strict functions. When the callee is a function written in
	// JavaScript and the running frame can be replaced, the callee's frame replaces it, and the
	// callee returns to the caller of the running function. Otherwise they are the ordinary calls,
	// and the next instructions return the result.
	OpTailCall       // like [OpCall]
	OpTailCallSpread // like [OpCallSpread]
	OpTailCallEval   // like [OpCallEval], where the instruction after one with b = -1 is [OpTailCallSpread]
)

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
// others.
const (
	ElemField = iota
	ElemMethod
	ElemGetter
	ElemSetter
	ElemStatic = 8
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

// Pos says that the instructions from [Pos.PC] on belong to the source position at [Pos.Line] and
// [Pos.Column].
type Pos struct {
	PC     int32
	Line   int32
	Column int32
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
	p := f.Positions[i]
	return int(p.Line), int(p.Column)
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
// Requests[Request], or its namespace object when [IndirectExport.All] is set.
type IndirectExport struct {
	Name       string
	Request    int32
	ImportName string
	All        bool
}

// ModuleFunc says that the closure of Protos[Proto] of the module function initializes Bindings[Binding].
type ModuleFunc struct {
	Proto   int32
	Binding int32
}
