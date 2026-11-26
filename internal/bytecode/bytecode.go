// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package bytecode defines the instructions that the compiler emits and the interpreter executes.
package bytecode

import (
	"sort"

	"github.com/hajimehoshi/ekuma/internal/srcpos"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// Opcode is the operation of an instruction.
type Opcode uint8

// The comments of the opcodes give the meaning of the operands a, b and c, which
// [Opcode.Operands] describes as data, with these conventions:
//
//   - An operand is a register unless its comment says otherwise. An operand that may name no
//     register is -1 then.
//   - Consts[x] and Keys[x] are the elements of [Func.Consts] and [Func.Keys] at the index x.
//   - A site is an index into [Func.Keys] that also names an inline cache of its own.
//   - A global cache is an index of an inline cache of a global variable, which only its
//     instruction uses.
//   - A flag is 0 or 1.
//   - An instruction writes the value that it produces to a. A jump has its target in a, or in
//     c when it also produces a value. Some instructions also update registers that they read,
//     such as the variable of [OpPostInc] and the iterator of [OpIterNext].
//   - A property read takes the object and the key in b and c, and a property store takes the
//     object, the key and the value in a, b and c. A variable store takes the value in a.
//   - An instruction that behaves differently in strict code has a variant for strict code,
//     whose name ends in Strict. [OpLoadThisSloppy] is the other way around: the variant of
//     [OpLoadThis] for non-strict functions.
//   - An instruction whose operands do not fit in a, b and c is followed by an [OpExtra] with
//     the rest, written x.a and x.b.
//   - Some instructions work together with the next instruction. [OpResolveGlobal] and
//     [OpCopyPropToGlobal] may run a following [OpGetProp]. [OpGetPropChain] may run the
//     next named read and [OpGetMethod] may run the following [OpCall]. [OpIterNextPair]
//     may supply the value of the following [OpIterNext]. [OpRegExpArg] and [OpSpreadArgs]
//     prepare the call that follows them.
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
	OpGetGlobal              // a = the global variable Keys[b]; c is a global cache
	OpGetGlobalStrict        // like [OpGetGlobal], in strict code
	OpGetGlobalOrUndef       // like [OpGetGlobal], but undefined when the global does not exist
	OpGetGlobalOrUndefStrict // like [OpGetGlobalOrUndef], in strict code
	OpSetGlobal              // the global variable Keys[b] = a, creating it when it does not exist; c is a global cache
	OpSetGlobalStrict        // like [OpSetGlobal], but a ReferenceError when the global does not exist
	OpResolveGlobal          // a = undefined if the global variable Keys[b] exists, null otherwise; c is a global cache
	OpCopyPropToGlobal       // a = resolution of the global store at c-1. Cached data slots copy the following named read of b through a+1 to the global and jump to c
	OpDeleteName             // a = delete the global variable Keys[b]
	OpDeclareGlobalVar       // declare the global variable Keys[a]; b is a set of GlobalVar flags, and c holds the function with GlobalVarFunc and is -1 otherwise
	OpDeclareGlobalLex       // declare the global lexical binding Keys[a], constant with the flag b
	OpInitGlobalLex          // initialize the global lexical binding Keys[b] with a
	OpCheckGlobalDecl        // throw when declaring Keys[a] at the top level would conflict; b is one of the Decl kinds
	OpDeclareAnnexB          // a = whether the global variable Keys[b] of a block-level function is declared; c is a set of GlobalVar flags

	// Upvalues and closures.
	OpGetUpval        // a = upvalue b
	OpGetUpvalChecked // a = upvalue b, throwing a ReferenceError naming Keys[c] if it is uninitialized
	OpSetUpval        // upvalue b = a
	OpCloseUpvals     // detach the upvalues that refer to the registers from a on
	OpClosure         // a = closure of Protos[b]

	// The values that the running function provides.
	OpLoadThis        // a = this
	OpLoadThisSloppy  // a = this, converted the way a sloppy function sees it
	OpLoadCallee      // a = the running function
	OpLoadNewTarget   // a = new.target
	OpLoadHome        // a = the home object of the running function
	OpArguments       // a = the arguments object, mapped to the parameters with the flag b
	OpRestArgs        // a = the arguments from index b on, as an array
	OpGetArgsLength   // a = arguments.length; c is a site. b holds the arguments object once materialized
	OpGetArgsIndex    // a = arguments[c]. b holds the arguments object once materialized
	OpGetArgsIndexInt // a = arguments[the integer c]. b holds the arguments object once materialized

	// Properties.
	OpGetProp           // a = b.Keys[c]; c is a site
	OpGetPropChain      // like [OpGetProp], followed by an [OpGetProp] or [OpGetPropChain] of a, which it may run too
	OpGetMethod         // a, a+1 = b.Keys[c], b; c is a site
	OpGetLength         // a = b.length; c is a site
	OpGetIndex          // a = b[c]
	OpGetIndexInt       // a = b[the integer c]
	OpSetProp           // a.Keys[b] = c; b is a site
	OpSetPropStrict     // like [OpSetProp], in strict code
	OpSetIndex          // a[b] = c
	OpSetIndexStrict    // like [OpSetIndex], in strict code
	OpSetIndexInt       // a[the integer b] = c
	OpSetIndexIntStrict // like [OpSetIndexInt], in strict code
	OpDeleteProp        // a = delete b[c]
	OpDeletePropStrict  // like [OpDeleteProp], in strict code
	OpIn                // a = c in b
	OpInstanceof        // a = b instanceof c; x.a is a site for the prototype property of c
	OpToPropertyKey     // a = b converted to a property key. c is -1, or the object of a compound assignment to its property b: then a key that is an integer from 0 to 2^32-1 stays a number, and a key that needs converting first throws a TypeError when c is undefined or null

	// Super properties.
	OpSuperBase      // a = the super base of the home object b, which is its prototype
	OpGetSuper       // a = super[c] with the this value b; x.a is the home object, or with the flag x.b the super base, and then c is converted to a property key in place
	OpSetSuper       // super[b] = c with the this value a; x.a is the super base
	OpSetSuperStrict // like [OpSetSuper], in strict code

	// Private names.
	OpHasPrivate // a = the private name c is in b
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
	OpPostInc   // a = b converted to a number or BigInt, then b = a + 1
	OpPostDec   // a = b converted to a number or BigInt, then b = a - 1
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
	OpJumpIfNullish    // if b is null or undefined then pc = a; with the flag c, also if b has [[IsHTMLDDA]], as b == null
	OpJumpIfNotNullish // if b is neither null nor undefined then pc = a; with the flag c, also not with [[IsHTMLDDA]], as b != null
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
	OpJumpNotSEqP // if !(b === c) then pc = a, where c is one of [PrimUndefined] through [PrimTrue]
	OpJumpNotSNeP // if !(b !== c) then pc = a, where c is one of [PrimUndefined] through [PrimTrue]

	// Jumps back to the start of a loop when its condition holds. Like a backward [OpJump], they
	// check for an interruption. The constant Consts[c] of the K forms is a number.
	OpLoopIf         // if b then pc = a
	OpLoopLt         // if b < c then pc = a
	OpLoopLe         // if b <= c then pc = a
	OpLoopGt         // if b > c then pc = a
	OpLoopGe         // if b >= c then pc = a
	OpLoopLtK        // if b < Consts[c] then pc = a
	OpLoopLeK        // if b <= Consts[c] then pc = a
	OpLoopGtK        // if b > Consts[c] then pc = a
	OpLoopGeK        // if b >= Consts[c] then pc = a
	OpLoopSEqP       // if b === c then pc = a, where c is one of [PrimUndefined] through [PrimTrue]
	OpLoopSNeP       // if b !== c then pc = a, where c is one of [PrimUndefined] through [PrimTrue]
	OpLoopNullish    // if b is null or undefined then pc = a; with the flag c, also if b has [[IsHTMLDDA]], as b == null
	OpLoopNotNullish // if b is neither null nor undefined then pc = a; with the flag c, also not with [[IsHTMLDDA]], as b != null

	// Calls. The callee is in b and the this value in b+1. The result goes to a, which may be b,
	// once the call returns: a call that throws leaves a as it is. The frame of the callee can
	// start at b+2, so a call can overwrite every register from there on; [Operands.Writes]
	// says which calls can.
	OpCall           // a = b(this b+1, args b+2...), c arguments
	OpCallUndef      // like [OpCall], with the this value undefined instead of b+1
	OpCallCall       // a = b.call(...) where b+1 is the function and b+2... the c arguments of call, the first of which is the this value
	OpApplyFwd       // a = b(this b+1, c, arguments). x.a holds the arguments object once materialized. An ordinary call passes c and the object in b+2 and b+3
	OpCallFwd        // a = b(this b+1, ...arguments). c holds the arguments object once materialized
	OpCallSpread     // a = b(this b+1, ...the array b+2)
	OpCallEval       // like [OpCall]; a direct eval when b is the eval function. x.a indexes [Func.EvalScopes]
	OpCallEvalSpread // like [OpCallSpread]; a direct eval when b is the eval function. x.a indexes [Func.EvalScopes]
	OpSpreadArgs     // a = an array of the values from iterable b, possibly b itself for a dense array with ordinary iteration; only the following spread call reads it

	// The calls in tail position of strict functions. When the callee is a function written in
	// JavaScript and the running frame can be replaced, the callee's frame replaces it, and the
	// callee returns to the caller of the running function. A bound function stands for its target
	// with its bound arguments, Function.prototype.call for its this value, and
	// Function.prototype.apply for its this value when its argument list can be made without
	// running JavaScript. Otherwise they are the ordinary calls, and the next instructions return
	// the result.
	OpTailCall           // like [OpCall]
	OpTailCallUndef      // like [OpCallUndef]
	OpTailCallSpread     // like [OpCallSpread]
	OpTailCallEval       // like [OpCallEval]
	OpTailCallEvalSpread // like [OpCallEvalSpread]
	OpTailApplyFwd       // like [OpApplyFwd]
	OpTailCallFwd        // like [OpCallFwd]

	// Constructor calls, which take their operands as the calls do.
	OpNew             // a = new b(args b+2...), c arguments; x.a is a site for the prototype property of b
	OpNewSpread       // a = new b(...the array b+2)
	OpSuperCall       // a = super(args b+2...), c arguments, where b is the super constructor and b+1 new.target
	OpSuperSpread     // a = super(...the array b+2)
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
	OpAwait               // suspend until the value b, as a promise, settles; a receives its value
	OpYieldDelegateResult // a = the value of the iterator result b; jump to c when it is not done, yielding b as it is
	OpGenResumeMode       // a = the Resume kind with which the generator was resumed
	OpGenFinally          // enter a finally handler with the exception in a+1: a = 1 for a throw, or a = b and a+1 = the returned value for a generator return

	// Literals.
	OpNewObject       // a = {}, with room for c properties
	OpNewArray        // a = [], with room for c elements
	OpNewArrayK       // a = a new array of the c constants from Consts[b] on
	OpArrayPush       // append b to the array a
	OpArrayHole       // append a hole to the array a
	OpArraySpread     // append the elements of the iterable b to the array a
	OpCopyProps       // copy the own enumerable properties of b to a, as an object spread
	OpCopyPropsExcept // a = an object with the own enumerable properties of b except the x.a keys from c on
	OpSetProto        // set the prototype of the object literal a to b when b is an object or null
	OpTemplate        // a = the template object b of the function
	OpRegExp          // a = a new RegExp object for the literal RegExps[b]
	OpRegExpArg       // a = the RegExp object for the literal RegExps[b] as the this value, when a is c+1, or the first argument, when a is c+2, of the call of c that follows
	OpRegExpMethod    // a = the property Keys[c] of a RegExp object a+1 for the literal RegExps[b]; c is a site. An [OpRegExpArg] for the this value replaces a+1 before the call

	// Definitions of properties and classes.
	OpDefineField  // define the enumerable data property b of a as c
	OpDefineFieldK // define the enumerable data property Keys[b] of a as c; b is a site
	OpDefineMethod // define the non-enumerable method b of a as c, setting its home object
	OpDefineGetter // define the getter c for the property b of a, enumerable with the flag x.a
	OpDefineSetter // define the setter c for the property b of a, enumerable with the flag x.a
	OpSetFuncName  // set the name of the anonymous function a to the property key b, with the NamePrefix kind c
	OpSetHome      // set the home object of the function a to b
	OpClass        // a = the prototype of the function b, which becomes a class constructor with the parent c, or with no extends clause when c is -1
	OpPrivateName  // a = a new private name described by Keys[b]; the flag c says whether it names a method
	OpInitFields   // define the instance elements of the class a on the instance b
	OpAddField     // add the class element with the key b and the value, initializer or initial value in c to the class a; x.a is an Elem kind
	OpAutoAccessor // a, a+1, a+2 = the backing private name, the getter and the setter of an auto-accessor with the key b

	// The steps of evaluating a class with decorators, which share the state of the evaluation.
	OpDecorateBegin   // a = a new state for the class with the name b
	OpDecorateAdd     // add the decorator b with the receiver c to the decorators of the next element of the state a
	OpDecorateClass   // make the decorators that the state a has so far the decorators of the class
	OpDecorateElement // add to the state a the class element with the key b and the value or initializer c, each of which may be -1; x.a is its Elem kind
	OpDecorateApply   // apply the decorators of the elements of the state b, define the elements on the class c and its prototype, and set a to the result of the decorators of the class
	OpDecorateFinish  // run the static elements and the initializers of the class b of the state a

	// Iteration.
	OpForIn           // a = the for-in iterator over b
	OpForInNext       // a = the next key of the for-in iterator b; jump to c when it is exhausted
	OpGetIterator     // c selects the Iter kind. a, a+1 receive the iterator of b and its next property, or the next index and the array for indexed iteration
	OpIterNext        // a = the next value of the iterator b, b+1; a = undefined and jump to c when it is done
	OpIterClose       // close the iterator a, a+1, and set a to undefined; b is -1 or holds the pending exception, which is kept unless it makes a generator return
	OpIterCall        // a, a+1 = the result of calling the method Keys[c] of the iterator b with the argument a and true, or undefined and false when there is no such method
	OpIterReturn      // a, a+1 = the result of calling the return method of the iterator b without arguments and true, or undefined and false when there is none; b becomes undefined
	OpIterCheckResult // throw a TypeError when the iterator result a is not an object
	OpIterResultValue // a = the value of the iterator result b; jump to c when it is done
	OpIterNextPair    // step b, b+1 when it yields a pair, writing its elements to a, x.a and jumping to c when destructuring is plain; otherwise write the pair as the value of the following OpIterNext and skip it, or leave it to step the iterator; b becomes undefined when the iterator is done

	// Disposal of resources.
	OpUsing        // add the resource b with the Dispose hint c to the dispose capability a, which is created when a is undefined
	OpDisposeBegin // start disposing the capability a for the completion b, which is a throw when c holds the integer 1
	OpDisposeStep  // a = the next value to await in disposing the capability b; jump to c when there is none
	OpDisposeError // record that disposing a resource of the capability a threw b
	OpDisposeEnd   // end disposing the capability a and throw its completion when it is a throw; a becomes undefined

	// Dynamic scopes, which with statements and direct evals open.
	OpWith            // push the object a as a with scope
	OpPopScope        // pop the innermost dynamic scope
	OpResolveName     // a = the innermost of the c innermost dynamic scopes that has the variable Keys[b], or undefined
	OpGetScoped       // a = the variable Keys[c] of the dynamic scope b
	OpGetScopedStrict // like [OpGetScoped], in strict code
	OpSetScoped       // assign a to Keys[c] of dynamic scope b, or to an unresolvable name if b is null
	OpSetScopedStrict // like [OpSetScoped], in strict code
	OpDeleteScoped    // a = delete the variable Keys[c] of the dynamic scope b
	OpCaptureScopes   // the closure a sees the dynamic scopes that are open
	OpEvalScope       // push a new scope for the variables that direct evals declare
	OpScopeThis       // a = the this value of a call of a function found in the dynamic scope b
	OpDeclareEvalVar  // declare the variable Keys[a] in the b-th innermost dynamic scope, a scope of [OpEvalScope]; it is a function when c is not -1, and c holds it

	// Modules.
	OpImport     // a = the promise of a dynamic import of b with the options c in the [Phase] c+1, an integer
	OpImportMeta // a = import.meta

	// Others.
	OpExtra // the operands of the instruction before it that do not fit in it; it is never executed
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

// The values that [OpJumpNotSEqP], [OpJumpNotSNeP], [OpLoopSEqP] and [OpLoopSNeP] compare with.
const (
	PrimUndefined = iota
	PrimNull
	PrimFalse
	PrimTrue
)

// The flags of [OpDeclareGlobalVar] and [OpDeclareAnnexB].
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

// The kinds of class element that only classes with decorators add with [OpDecorateElement].
const (
	ElemAccessor = ElemSetter + 1 + iota
	ElemStaticBlock
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
	ConstUndefined ConstKind = iota
	ConstNull
	ConstFalse
	ConstTrue
	ConstNumber
	ConstString
	ConstBigInt
)

// Const is a constant of a function. A string is in [Const.Str], and so are the decimal digits of
// a BigInt. The kinds before [ConstNumber] have no other fields.
type Const struct {
	Kind ConstKind
	Num  float64
	Str  wtf8.String
}

// Template is the site of a tagged template. A cooked string is absent where [Template.HasCooked]
// is false.
type Template struct {
	Cooked    []wtf8.String
	HasCooked []bool
	Raw       []wtf8.String
}

// RegExp is a regular expression literal.
type RegExp struct {
	Pattern wtf8.String
	Flags   wtf8.String
}

// Func is the compiled code of a function.
type Func struct {
	Name wtf8.String
	File string

	// Source is the text of the function.
	Source wtf8.String

	Code     []Instr
	Consts   []Const
	Handlers []Handler
	Upvals   []UpvalDesc
	Protos   []*Func

	// Keys are the property names and variable names that instructions refer to.
	Keys []wtf8.String

	// NumCaches is the number of inline caches that the instructions refer to.
	NumCaches int

	// NumGlobalCaches is the number of inline caches of global variables that the
	// instructions refer to.
	NumGlobalCaches int

	Templates []Template

	RegExps []RegExp

	// EvalScopes describe the call sites of direct evals, which [OpCallEval] and its spread and tail-call forms refer to.
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
	EvalVarImport

	// EvalVarFuncName is the name of a named function expression inside that function.
	EvalVarFuncName

	// EvalVarPseudo is a value that the engine provides, such as this, or a private name.
	EvalVarPseudo
)

// EvalVar is a variable that eval code sees.
type EvalVar struct {
	Name string
	Kind EvalVarKind

	// InVarEnv says whether this binding belongs to the variable environment of non-strict eval.
	InVarEnv bool

	// BlocksVar says whether a lexical declaration between the call and its variable
	// environment forbids var declarations of this name, even if a catch parameter hides it.
	BlocksVar bool

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
	offset := f.SourceOffset(pc)
	if offset < 0 {
		return 0, 0
	}
	return f.Lines.Position(offset)
}

// SourceOffset returns the byte offset in the source for the instruction at pc, or -1 if unknown.
func (f *Func) SourceOffset(pc int) int32 {
	i := sort.Search(len(f.Positions), func(i int) bool {
		return int(f.Positions[i].PC) > pc
	}) - 1
	if i < 0 {
		return -1
	}
	return f.Positions[i].Offset
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
	Specifier  wtf8.String
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
	Key   wtf8.String
	Value wtf8.String
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
