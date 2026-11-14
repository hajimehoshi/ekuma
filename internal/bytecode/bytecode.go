// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package bytecode defines the instructions that the compiler emits and the interpreter executes.
package bytecode

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
	OpThrowError                     // throw an error of kind a with the message Consts[b]

	OpGetGlobal        // a = global Keys[b], cache c
	OpGetGlobalOrUndef // like OpGetGlobal, but undefined when the global does not exist
	OpSetGlobal        // global Keys[b] = a, cache c; creates the global when it does not exist
	OpSetGlobalStrict  // like OpSetGlobal, but a ReferenceError when the global does not exist
	OpDeclareGlobalVar // declare the global variable Keys[a]; b says whether it is a function, which c holds
	OpDeclareGlobalLex // declare the global lexical binding Keys[a], constant when b is set
	OpInitGlobalLex    // initialize the global lexical binding Keys[b] with a
	OpCheckGlobalDecl  // throw when declaring Keys[a] at the top level would conflict; b: 0 var, 1 lexical, 2 function

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
	OpDefinePrivate // define the private field b of a as c
	OpToPropertyKey // a = b converted to a property key

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
	OpApplyFwd      // a = a.apply(...) where a+1 is the function, a+2 the this value, forwarding the arguments of the running function
	OpCallFwd       // a = a(this a+1, the arguments of the running function)
	OpCallSpread    // a = a(this a+1, ...the array a+2)
	OpCallEval      // like OpCall; a direct eval when a is the eval function. c is the index of the scope description
	OpNew           // a = new a(args a+2...), b arguments, cache c
	OpNewSpread     // a = new a(...the array a+2)
	OpSuperCall     // a = super(args a+2...), b arguments, where a is the running constructor and a+1 new.target
	OpSuperSpread   // a = super(...the array a+2)
	OpReturn        // return a
	OpReturnUndef   // return undefined
	OpReturnDerived // return a from a derived constructor whose this is in b
	OpThrow         // throw a
	OpRethrow       // throw a again, as the completion of a finally block

	OpInitialYield        // suspend a generator before its body
	OpYield               // suspend the generator with the value b; a receives the value sent on resume
	OpAwait               // suspend until the promise b settles; a receives its value
	OpYieldDelegateResult // a = the value of the iterator result b; jump to c when it is not done, yielding b as it is
	OpGenResumeMode       // a = how the generator was resumed: 0 next, 1 throw, 2 return

	OpNewObject        // a = {}
	OpNewArray         // a = [a+1 ... a+b]
	OpArrayPush        // append b to the array a
	OpArrayHole        // append a hole to the array a
	OpArraySpread      // append the elements of the iterable b to the array a
	OpRegExp           // a = new RegExp(Consts[b], Consts[c])
	OpTemplate         // a = the template object b of the function
	OpDefineField      // define the enumerable data property b of a as c
	OpDefineFieldK     // define the enumerable data property Keys[b] of a as c, cache b
	OpDefineMethod     // define the non-enumerable method b of a as c, setting its home object
	OpDefineGetter     // define the getter c for the property b of a; the next instruction's a says whether it is enumerable
	OpDefineSetter     // define the setter c for the property b of a
	OpSetFuncName      // set the name of the anonymous function a to the property key b, with the prefix kind c
	OpSetHome          // set the home object of the function a to b
	OpCopyProps        // copy the own enumerable properties of b to a, as an object spread
	OpCopyPropsExcept  // a = an object with the own enumerable properties of b except the keys in c+1...c+N, N in the next instruction's a
	OpSetProto         // set the prototype of the object literal a to b when b is an object or null
	OpClass            // make the function a a class constructor with the parent c; a+1 receives the prototype. c is -1 without an extends clause
	OpPrivateName      // a = a new private name described by Keys[b]; c says whether it names a method
	OpInitFields       // define the instance elements of the class a on the instance b
	OpAddField         // add the class element with the key b and the value or initializer c to the class a; the next instruction's a is the kind
	OpCheckThisUninit  // throw when a is initialized: super was called twice
	OpRequireCoercible // throw a TypeError when a is undefined or null

	OpForIn       // a = the for-in iterator over b
	OpForInNext   // a = the next key of the for-in iterator b, or jump to c when it is exhausted
	OpGetIterator // a, a+1 = the iterator of b and its next method; c selects sync (0) or async (1)
	OpIterNext    // a = the next value of the iterator b, b+1; jump to c when it is done
	OpIterClose   // close the iterator a, a+1; b says whether an exception is pending, which is then kept
	OpIterResult  // a = the iterator result object with the value b and done c
	OpIterCall    // a = the result of calling the method Keys[c] of the iterator b with the argument a; a becomes undefined when there is none

	OpWith       // push the object a as a with scope
	OpPopScope   // pop the innermost dynamic scope
	OpGetName    // a = the variable Keys[b] looked up dynamically; c is the fallback instruction kind
	OpSetName    // the variable Keys[b] looked up dynamically = a
	OpDeleteName // a = delete the variable Keys[b]
	OpTypeofName // a = typeof the variable Keys[b], looked up dynamically
	OpCallName   // a, a+1 = the function Keys[b] looked up dynamically and its this value

	OpDebugger
	OpNop
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
	// Local says whether Index is a register of the enclosing function or one of its upvalues.
	Local bool
	Index int32
}

// LinePos says that the instructions from PC on belong to the source line Line.
type LinePos struct {
	PC   int32
	Line int32
}

// ConstKind is the type of a constant.
type ConstKind uint8

const (
	ConstNumber ConstKind = iota
	ConstString
	ConstBigInt
)

// Const is a constant of a function. Strings are in WTF-8, and a BigInt is its decimal digits in Str.
type Const struct {
	Kind ConstKind
	Num  float64
	Str  string
}

// Template is the site of a tagged template. A cooked string is absent where HasCooked is false.
type Template struct {
	Cooked    []string
	HasCooked []bool
	Raw       []string
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

	// Lines maps instructions to source lines, ordered by PC.
	Lines []LinePos

	// Length is the value of the length property of the function.
	Length int

	// NumParams is the number of leading registers that receive arguments.
	NumParams int

	// NumClear is the number of leading registers that must not hold stale values when the function starts.
	NumClear int
	NumRegs  int

	Strict      bool
	IsArrow     bool
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

// Line returns the source line of the instruction at pc.
func (f *Func) Line(pc int) int {
	l := 0
	for _, e := range f.Lines {
		if int(e.PC) > pc {
			break
		}
		l = int(e.Line)
	}
	return l
}
