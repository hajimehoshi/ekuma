// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package bytecode

import (
	"strconv"
)

// OperandKind is what an operand of an instruction holds.
type OperandKind uint8

const (
	// OperandUnused is an operand that the instruction does not use, which is 0.
	OperandUnused OperandKind = iota

	// OperandReg is a register.
	OperandReg

	// OperandOptReg is a register, or -1 for none.
	OperandOptReg

	// OperandConst is an index into [Func.Consts].
	OperandConst

	// OperandConstRun is an index into [Func.Consts] at which a run of constants starts. It is
	// the operand b of an instruction whose operand c, an [OperandInt], is the length of the run.
	OperandConstRun

	// OperandKey is an index into [Func.Keys].
	OperandKey

	// OperandSite is an index into [Func.Keys] that also names an inline cache.
	OperandSite

	// OperandTarget is the index of the instruction that a jump goes to.
	OperandTarget

	// OperandInt is an integer.
	OperandInt

	// OperandCount is the number of registers that it adds to a [Regs] with Counted set.
	OperandCount

	// OperandFlag is 0 or 1.
	OperandFlag

	// OperandUpval is an index into the upvalues of the running function.
	OperandUpval

	// OperandProto is an index into [Func.Protos].
	OperandProto

	// OperandRegExp is an index into [Func.RegExps].
	OperandRegExp

	// OperandTemplate is an index into [Func.Templates].
	OperandTemplate

	// OperandEvalScope is an index into [Func.EvalScopes].
	OperandEvalScope

	// OperandThrow is one of the Throw kinds, such as [ThrowTypeError].
	OperandThrow

	// OperandDecl is one of the Decl kinds, such as [DeclVar].
	OperandDecl

	// OperandIter is one of the Iter kinds, such as [IterSync].
	OperandIter

	// OperandElem is an Elem kind, such as [ElemField], combined as [ElemStatic] and [ElemValue] allow.
	OperandElem

	// OperandDispose is one of the Dispose hints, such as [DisposeSync].
	OperandDispose

	// OperandNamePrefix is one of the NamePrefix kinds, such as [NamePrefixNone].
	OperandNamePrefix

	// OperandPrim is one of the Prim values, such as [PrimUndefined].
	OperandPrim

	// OperandGlobalVar is a set of GlobalVar flags, such as [GlobalVarFunc].
	OperandGlobalVar
)

// Field names an operand: a, b or c of an instruction, or x.a or x.b of the [OpExtra] after it.
type Field uint8

const (
	FieldA Field = iota
	FieldB
	FieldC
	FieldXA
	FieldXB
)

// Regs is a run of consecutive registers that an instruction uses. It starts Offset registers
// after the register in the operand At.
type Regs struct {
	At     Field
	Offset int

	// Len is the number of registers, or [RegsToEnd]. With Counted, the value of the operand
	// of the kind [OperandCount] adds to it.
	Len     int
	Counted bool
}

// RegsToEnd as [Regs.Len] makes the run go on through the last register of the function.
const RegsToEnd = -1

// Operands describes the operands of an opcode and the registers that its instructions use.
type Operands struct {
	// A, B and C are what the operands a, b and c hold.
	A, B, C OperandKind

	// Extra says whether an [OpExtra] follows the instruction. XA and XB are what its operands
	// x.a and x.b hold, and its operand c is unused.
	Extra  bool
	XA, XB OperandKind

	// Reads are the registers that the instruction may read, and Writes are the registers that
	// it may write. They leave out the registers that no operand names, such as the parameters
	// behind an arguments object and the variables that closures capture. They must not be
	// modified.
	Reads, Writes []Regs
}

// Operands returns the description of the operands of op, which must be an opcode.
func (op Opcode) Operands() Operands {
	return opcodes[op].operands
}

// String returns the name of op.
func (op Opcode) String() string {
	if int(op) < len(opcodes) {
		if n := opcodes[op].name; n != "" {
			return n
		}
	}
	return "Opcode(" + strconv.Itoa(int(op)) + ")"
}

// The runs of single registers that most instructions use.
var (
	regA   = []Regs{{At: FieldA, Len: 1}}
	regB   = []Regs{{At: FieldB, Len: 1}}
	regC   = []Regs{{At: FieldC, Len: 1}}
	regAB  = []Regs{{At: FieldA, Len: 1}, {At: FieldB, Len: 1}}
	regAC  = []Regs{{At: FieldA, Len: 1}, {At: FieldC, Len: 1}}
	regBC  = []Regs{{At: FieldB, Len: 1}, {At: FieldC, Len: 1}}
	regABC = []Regs{{At: FieldA, Len: 1}, {At: FieldB, Len: 1}, {At: FieldC, Len: 1}}

	// regPairA is a and a+1.
	regPairA = []Regs{{At: FieldA, Len: 2}}

	// superStoreReads holds the this value, the key and the value of a super property store, and
	// its super base in x.a.
	superStoreReads = []Regs{{At: FieldA, Len: 1}, {At: FieldB, Len: 1}, {At: FieldC, Len: 1}, {At: FieldXA, Len: 1}}
)

// The runs of the calls. The callee is in b and the this value in b+1, and the arguments follow
// them. The frame of a callee can take the place of the registers from the arguments on.
var (
	callReads  = []Regs{{At: FieldB, Len: 2, Counted: true}}
	callWrites = []Regs{{At: FieldA, Len: 1}, {At: FieldB, Offset: 2, Len: RegsToEnd}}

	// callUndefReads skips the this value, which the instruction sets.
	callUndefReads  = []Regs{{At: FieldB, Len: 1}, {At: FieldB, Offset: 2, Counted: true}}
	callUndefWrites = []Regs{{At: FieldA, Len: 1}, {At: FieldB, Offset: 1, Len: RegsToEnd}}

	// spreadReads holds the callee, the this value and the array of the arguments.
	spreadReads = []Regs{{At: FieldB, Len: 3}}

	// fwdReads holds the callee and the this value.
	fwdReads = []Regs{{At: FieldB, Len: 2}}

	// applyFwdReads holds the callee, the function and the this value in c.
	applyFwdReads = []Regs{{At: FieldB, Len: 2}, {At: FieldC, Len: 1}}
)

// The operands of the families of opcodes.
var (
	loadOperands = Operands{
		A:      OperandReg,
		Writes: regA,
	}
	unaryOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		Reads:  regB,
		Writes: regA,
	}
	binaryOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		C:      OperandReg,
		Reads:  regBC,
		Writes: regA,
	}
	binaryKOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		C:      OperandConst,
		Reads:  regB,
		Writes: regA,
	}
	getGlobalOperands = Operands{
		A:      OperandReg,
		B:      OperandSite,
		Writes: regA,
	}
	setGlobalOperands = Operands{
		A:     OperandReg,
		B:     OperandSite,
		Reads: regA,
	}
	getPropOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		C:      OperandSite,
		Reads:  regB,
		Writes: regA,
	}
	setPropOperands = Operands{
		A:     OperandReg,
		B:     OperandSite,
		C:     OperandReg,
		Reads: regAC,
	}
	storeOperands = Operands{
		A:     OperandReg,
		B:     OperandReg,
		C:     OperandReg,
		Reads: regABC,
	}
	superStoreOperands = Operands{
		A:     OperandReg,
		B:     OperandReg,
		C:     OperandReg,
		Extra: true,
		XA:    OperandReg,
		Reads: superStoreReads,
	}
	accessorOperands = Operands{
		A:     OperandReg,
		B:     OperandReg,
		C:     OperandReg,
		Extra: true,
		XA:    OperandFlag,
		Reads: regABC,
	}
	jumpIfOperands = Operands{
		A:     OperandTarget,
		B:     OperandReg,
		Reads: regB,
	}
	jumpNullishOperands = Operands{
		A:     OperandTarget,
		B:     OperandReg,
		C:     OperandFlag,
		Reads: regB,
	}
	jumpCmpOperands = Operands{
		A:     OperandTarget,
		B:     OperandReg,
		C:     OperandReg,
		Reads: regBC,
	}
	jumpCmpKOperands = Operands{
		A:     OperandTarget,
		B:     OperandReg,
		C:     OperandConst,
		Reads: regB,
	}
	jumpCmpPOperands = Operands{
		A:     OperandTarget,
		B:     OperandReg,
		C:     OperandPrim,
		Reads: regB,
	}
	// valueOrJumpOperands is for the instructions that produce a value or jump.
	valueOrJumpOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		C:      OperandTarget,
		Reads:  regB,
		Writes: regA,
	}
	callOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		C:      OperandCount,
		Reads:  callReads,
		Writes: callWrites,
	}
	callUndefOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		C:      OperandCount,
		Reads:  callUndefReads,
		Writes: callUndefWrites,
	}
	applyFwdOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		C:      OperandReg,
		Extra:  true,
		XA:     OperandFlag,
		Reads:  applyFwdReads,
		Writes: callWrites,
	}
	callFwdOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		Reads:  fwdReads,
		Writes: regA,
	}
	callSpreadOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		Reads:  spreadReads,
		Writes: regA,
	}
	callEvalOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		C:      OperandCount,
		Extra:  true,
		XA:     OperandEvalScope,
		Reads:  callReads,
		Writes: callWrites,
	}
	callEvalSpreadOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		Extra:  true,
		XA:     OperandEvalScope,
		Reads:  spreadReads,
		Writes: regA,
	}
	getScopedOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		C:      OperandKey,
		Reads:  regB,
		Writes: regA,
	}
	setScopedOperands = Operands{
		A:     OperandReg,
		B:     OperandReg,
		C:     OperandKey,
		Reads: regAB,
	}
	useOperands = Operands{
		A:     OperandReg,
		Reads: regA,
	}
	usePairOperands = Operands{
		A:     OperandReg,
		B:     OperandReg,
		Reads: regAB,
	}
)

// opcodes holds the name and the operands of every opcode.
var opcodes = [...]struct {
	name     string
	operands Operands
}{
	OpLoadK: {
		name: "OpLoadK",
		operands: Operands{
			A:      OperandReg,
			B:      OperandConst,
			Writes: regA,
		},
	},
	OpLoadInt: {
		name: "OpLoadInt",
		operands: Operands{
			A:      OperandReg,
			B:      OperandInt,
			Writes: regA,
		},
	},
	OpLoadUndef: {
		name:     "OpLoadUndef",
		operands: loadOperands,
	},
	OpLoadNull: {
		name:     "OpLoadNull",
		operands: loadOperands,
	},
	OpLoadTrue: {
		name:     "OpLoadTrue",
		operands: loadOperands,
	},
	OpLoadFalse: {
		name:     "OpLoadFalse",
		operands: loadOperands,
	},
	OpLoadTDZ: {
		name:     "OpLoadTDZ",
		operands: loadOperands,
	},
	OpMove: {
		name:     "OpMove",
		operands: unaryOperands,
	},

	OpCheckTDZ: {
		name: "OpCheckTDZ",
		operands: Operands{
			A:     OperandReg,
			B:     OperandKey,
			Reads: regA,
		},
	},
	OpRequireCoercible: {
		name:     "OpRequireCoercible",
		operands: useOperands,
	},
	OpThrowConstAssign: {
		name: "OpThrowConstAssign",
		operands: Operands{
			A: OperandKey,
		},
	},
	OpThrowError: {
		name: "OpThrowError",
		operands: Operands{
			A: OperandThrow,
			B: OperandConst,
		},
	},

	OpGetGlobal: {
		name:     "OpGetGlobal",
		operands: getGlobalOperands,
	},
	OpGetGlobalStrict: {
		name:     "OpGetGlobalStrict",
		operands: getGlobalOperands,
	},
	OpGetGlobalOrUndef: {
		name:     "OpGetGlobalOrUndef",
		operands: getGlobalOperands,
	},
	OpGetGlobalOrUndefStrict: {
		name:     "OpGetGlobalOrUndefStrict",
		operands: getGlobalOperands,
	},
	OpSetGlobal: {
		name:     "OpSetGlobal",
		operands: setGlobalOperands,
	},
	OpSetGlobalStrict: {
		name:     "OpSetGlobalStrict",
		operands: setGlobalOperands,
	},
	OpResolveGlobal: {
		name:     "OpResolveGlobal",
		operands: getGlobalOperands,
	},
	OpDeleteName: {
		name: "OpDeleteName",
		operands: Operands{
			A:      OperandReg,
			B:      OperandKey,
			Writes: regA,
		},
	},
	OpDeclareGlobalVar: {
		name: "OpDeclareGlobalVar",
		operands: Operands{
			A:     OperandKey,
			B:     OperandGlobalVar,
			C:     OperandOptReg,
			Reads: regC,
		},
	},
	OpDeclareGlobalLex: {
		name: "OpDeclareGlobalLex",
		operands: Operands{
			A: OperandKey,
			B: OperandFlag,
		},
	},
	OpInitGlobalLex: {
		name: "OpInitGlobalLex",
		operands: Operands{
			A:     OperandReg,
			B:     OperandKey,
			Reads: regA,
		},
	},
	OpCheckGlobalDecl: {
		name: "OpCheckGlobalDecl",
		operands: Operands{
			A: OperandKey,
			B: OperandDecl,
		},
	},
	OpDeclareAnnexB: {
		name: "OpDeclareAnnexB",
		operands: Operands{
			A:      OperandReg,
			B:      OperandKey,
			C:      OperandGlobalVar,
			Writes: regA,
		},
	},

	OpGetUpval: {
		name: "OpGetUpval",
		operands: Operands{
			A:      OperandReg,
			B:      OperandUpval,
			Writes: regA,
		},
	},
	OpSetUpval: {
		name: "OpSetUpval",
		operands: Operands{
			A:     OperandReg,
			B:     OperandUpval,
			Reads: regA,
		},
	},
	OpCloseUpvals: {
		name: "OpCloseUpvals",
		operands: Operands{
			A:     OperandReg,
			Reads: []Regs{{At: FieldA, Len: RegsToEnd}},
		},
	},
	OpClosure: {
		name: "OpClosure",
		operands: Operands{
			A:      OperandReg,
			B:      OperandProto,
			Writes: regA,
		},
	},

	OpLoadThis: {
		name:     "OpLoadThis",
		operands: loadOperands,
	},
	OpLoadThisSloppy: {
		name:     "OpLoadThisSloppy",
		operands: loadOperands,
	},
	OpLoadCallee: {
		name:     "OpLoadCallee",
		operands: loadOperands,
	},
	OpLoadNewTarget: {
		name:     "OpLoadNewTarget",
		operands: loadOperands,
	},
	OpLoadHome: {
		name:     "OpLoadHome",
		operands: loadOperands,
	},
	OpArguments: {
		name: "OpArguments",
		operands: Operands{
			A:      OperandReg,
			B:      OperandFlag,
			Writes: regA,
		},
	},
	OpRestArgs: {
		name: "OpRestArgs",
		operands: Operands{
			A:      OperandReg,
			B:      OperandInt,
			Writes: regA,
		},
	},

	OpGetProp: {
		name:     "OpGetProp",
		operands: getPropOperands,
	},
	OpGetPropChain: {
		name:     "OpGetPropChain",
		operands: getPropOperands,
	},
	OpGetMethod: {
		name: "OpGetMethod",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandSite,
			Reads:  regB,
			Writes: regPairA,
		},
	},
	OpGetLength: {
		name:     "OpGetLength",
		operands: getPropOperands,
	},
	OpGetIndex: {
		name:     "OpGetIndex",
		operands: binaryOperands,
	},
	OpGetIndexInt: {
		name: "OpGetIndexInt",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandInt,
			Reads:  regB,
			Writes: regA,
		},
	},
	OpSetProp: {
		name:     "OpSetProp",
		operands: setPropOperands,
	},
	OpSetPropStrict: {
		name:     "OpSetPropStrict",
		operands: setPropOperands,
	},
	OpSetIndex: {
		name:     "OpSetIndex",
		operands: storeOperands,
	},
	OpSetIndexStrict: {
		name:     "OpSetIndexStrict",
		operands: storeOperands,
	},
	OpDeleteProp: {
		name:     "OpDeleteProp",
		operands: binaryOperands,
	},
	OpDeletePropStrict: {
		name:     "OpDeletePropStrict",
		operands: binaryOperands,
	},
	OpIn: {
		name:     "OpIn",
		operands: binaryOperands,
	},
	OpInstanceof: {
		name: "OpInstanceof",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandReg,
			Extra:  true,
			XA:     OperandSite,
			Reads:  regBC,
			Writes: regA,
		},
	},
	OpToPropertyKey: {
		name: "OpToPropertyKey",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandOptReg,
			Reads:  regBC,
			Writes: regA,
		},
	},

	OpSuperBase: {
		name:     "OpSuperBase",
		operands: unaryOperands,
	},
	OpGetSuper: {
		name: "OpGetSuper",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandReg,
			Extra:  true,
			XA:     OperandReg,
			XB:     OperandFlag,
			Reads:  []Regs{{At: FieldB, Len: 1}, {At: FieldC, Len: 1}, {At: FieldXA, Len: 1}},
			Writes: regAC,
		},
	},
	OpSetSuper: {
		name:     "OpSetSuper",
		operands: superStoreOperands,
	},
	OpSetSuperStrict: {
		name:     "OpSetSuperStrict",
		operands: superStoreOperands,
	},

	OpHasPrivate: {
		name:     "OpHasPrivate",
		operands: binaryOperands,
	},
	OpGetPrivate: {
		name:     "OpGetPrivate",
		operands: binaryOperands,
	},
	OpSetPrivate: {
		name:     "OpSetPrivate",
		operands: storeOperands,
	},

	OpAdd: {
		name:     "OpAdd",
		operands: binaryOperands,
	},
	OpSub: {
		name:     "OpSub",
		operands: binaryOperands,
	},
	OpMul: {
		name:     "OpMul",
		operands: binaryOperands,
	},
	OpDiv: {
		name:     "OpDiv",
		operands: binaryOperands,
	},
	OpMod: {
		name:     "OpMod",
		operands: binaryOperands,
	},
	OpExp: {
		name:     "OpExp",
		operands: binaryOperands,
	},
	OpBitOr: {
		name:     "OpBitOr",
		operands: binaryOperands,
	},
	OpBitAnd: {
		name:     "OpBitAnd",
		operands: binaryOperands,
	},
	OpBitXor: {
		name:     "OpBitXor",
		operands: binaryOperands,
	},
	OpShl: {
		name:     "OpShl",
		operands: binaryOperands,
	},
	OpShr: {
		name:     "OpShr",
		operands: binaryOperands,
	},
	OpUShr: {
		name:     "OpUShr",
		operands: binaryOperands,
	},

	OpAddK: {
		name:     "OpAddK",
		operands: binaryKOperands,
	},
	OpSubK: {
		name:     "OpSubK",
		operands: binaryKOperands,
	},
	OpMulK: {
		name:     "OpMulK",
		operands: binaryKOperands,
	},
	OpDivK: {
		name:     "OpDivK",
		operands: binaryKOperands,
	},
	OpModK: {
		name:     "OpModK",
		operands: binaryKOperands,
	},
	OpBitOrK: {
		name:     "OpBitOrK",
		operands: binaryKOperands,
	},
	OpBitAndK: {
		name:     "OpBitAndK",
		operands: binaryKOperands,
	},
	OpShlK: {
		name:     "OpShlK",
		operands: binaryKOperands,
	},
	OpShrK: {
		name:     "OpShrK",
		operands: binaryKOperands,
	},
	OpUShrK: {
		name:     "OpUShrK",
		operands: binaryKOperands,
	},

	OpNeg: {
		name:     "OpNeg",
		operands: unaryOperands,
	},
	OpPlus: {
		name:     "OpPlus",
		operands: unaryOperands,
	},
	OpNot: {
		name:     "OpNot",
		operands: unaryOperands,
	},
	OpBitNot: {
		name:     "OpBitNot",
		operands: unaryOperands,
	},
	OpTypeof: {
		name:     "OpTypeof",
		operands: unaryOperands,
	},
	OpInc: {
		name:     "OpInc",
		operands: unaryOperands,
	},
	OpDec: {
		name:     "OpDec",
		operands: unaryOperands,
	},
	OpToNumeric: {
		name:     "OpToNumeric",
		operands: unaryOperands,
	},
	OpPostInc: {
		name: "OpPostInc",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			Reads:  regB,
			Writes: regAB,
		},
	},
	OpPostDec: {
		name: "OpPostDec",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			Reads:  regB,
			Writes: regAB,
		},
	},
	OpToString: {
		name:     "OpToString",
		operands: unaryOperands,
	},
	OpConcat: {
		name: "OpConcat",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandCount,
			Reads:  []Regs{{At: FieldB, Counted: true}},
			Writes: regA,
		},
	},

	OpLt: {
		name:     "OpLt",
		operands: binaryOperands,
	},
	OpLe: {
		name:     "OpLe",
		operands: binaryOperands,
	},
	OpGt: {
		name:     "OpGt",
		operands: binaryOperands,
	},
	OpGe: {
		name:     "OpGe",
		operands: binaryOperands,
	},
	OpEq: {
		name:     "OpEq",
		operands: binaryOperands,
	},
	OpNe: {
		name:     "OpNe",
		operands: binaryOperands,
	},
	OpStrictEq: {
		name:     "OpStrictEq",
		operands: binaryOperands,
	},
	OpStrictNe: {
		name:     "OpStrictNe",
		operands: binaryOperands,
	},

	OpJump: {
		name: "OpJump",
		operands: Operands{
			A: OperandTarget,
		},
	},
	OpJumpIf: {
		name:     "OpJumpIf",
		operands: jumpIfOperands,
	},
	OpJumpIfNot: {
		name:     "OpJumpIfNot",
		operands: jumpIfOperands,
	},
	OpJumpIfNullish: {
		name:     "OpJumpIfNullish",
		operands: jumpNullishOperands,
	},
	OpJumpIfNotNullish: {
		name:     "OpJumpIfNotNullish",
		operands: jumpNullishOperands,
	},
	OpJumpIfUndef: {
		name:     "OpJumpIfUndef",
		operands: jumpIfOperands,
	},
	OpJumpIfNotUndef: {
		name:     "OpJumpIfNotUndef",
		operands: jumpIfOperands,
	},
	OpJumpIfInt: {
		name: "OpJumpIfInt",
		operands: Operands{
			A:     OperandTarget,
			B:     OperandReg,
			C:     OperandInt,
			Reads: regB,
		},
	},

	OpJumpNotLt: {
		name:     "OpJumpNotLt",
		operands: jumpCmpOperands,
	},
	OpJumpNotLe: {
		name:     "OpJumpNotLe",
		operands: jumpCmpOperands,
	},
	OpJumpNotGt: {
		name:     "OpJumpNotGt",
		operands: jumpCmpOperands,
	},
	OpJumpNotGe: {
		name:     "OpJumpNotGe",
		operands: jumpCmpOperands,
	},
	OpJumpNotEq: {
		name:     "OpJumpNotEq",
		operands: jumpCmpOperands,
	},
	OpJumpNotNe: {
		name:     "OpJumpNotNe",
		operands: jumpCmpOperands,
	},
	OpJumpNotSEq: {
		name:     "OpJumpNotSEq",
		operands: jumpCmpOperands,
	},
	OpJumpNotSNe: {
		name:     "OpJumpNotSNe",
		operands: jumpCmpOperands,
	},
	OpJumpNotLtK: {
		name:     "OpJumpNotLtK",
		operands: jumpCmpKOperands,
	},
	OpJumpNotLeK: {
		name:     "OpJumpNotLeK",
		operands: jumpCmpKOperands,
	},
	OpJumpNotGtK: {
		name:     "OpJumpNotGtK",
		operands: jumpCmpKOperands,
	},
	OpJumpNotGeK: {
		name:     "OpJumpNotGeK",
		operands: jumpCmpKOperands,
	},
	OpJumpNotSEqK: {
		name:     "OpJumpNotSEqK",
		operands: jumpCmpKOperands,
	},
	OpJumpNotSNeK: {
		name:     "OpJumpNotSNeK",
		operands: jumpCmpKOperands,
	},
	OpJumpNotSEqP: {
		name:     "OpJumpNotSEqP",
		operands: jumpCmpPOperands,
	},
	OpJumpNotSNeP: {
		name:     "OpJumpNotSNeP",
		operands: jumpCmpPOperands,
	},

	OpCall: {
		name:     "OpCall",
		operands: callOperands,
	},
	OpCallUndef: {
		name:     "OpCallUndef",
		operands: callUndefOperands,
	},
	OpCallCall: {
		name:     "OpCallCall",
		operands: callOperands,
	},
	OpApplyFwd: {
		name:     "OpApplyFwd",
		operands: applyFwdOperands,
	},
	OpCallFwd: {
		name:     "OpCallFwd",
		operands: callFwdOperands,
	},
	OpCallSpread: {
		name:     "OpCallSpread",
		operands: callSpreadOperands,
	},
	OpCallEval: {
		name:     "OpCallEval",
		operands: callEvalOperands,
	},
	OpCallEvalSpread: {
		name:     "OpCallEvalSpread",
		operands: callEvalSpreadOperands,
	},
	OpSpreadArgs: {
		name:     "OpSpreadArgs",
		operands: unaryOperands,
	},

	OpTailCall: {
		name:     "OpTailCall",
		operands: callOperands,
	},
	OpTailCallUndef: {
		name:     "OpTailCallUndef",
		operands: callUndefOperands,
	},
	OpTailCallSpread: {
		name:     "OpTailCallSpread",
		operands: callSpreadOperands,
	},
	OpTailCallEval: {
		name:     "OpTailCallEval",
		operands: callEvalOperands,
	},
	OpTailCallEvalSpread: {
		name:     "OpTailCallEvalSpread",
		operands: callEvalSpreadOperands,
	},
	OpTailApplyFwd: {
		name:     "OpTailApplyFwd",
		operands: applyFwdOperands,
	},
	OpTailCallFwd: {
		name:     "OpTailCallFwd",
		operands: callFwdOperands,
	},

	OpNew: {
		name: "OpNew",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandCount,
			Extra:  true,
			XA:     OperandSite,
			Reads:  callUndefReads,
			Writes: callWrites,
		},
	},
	OpNewSpread: {
		name:     "OpNewSpread",
		operands: callSpreadOperands,
	},
	OpSuperCall: {
		name:     "OpSuperCall",
		operands: callOperands,
	},
	OpSuperSpread: {
		name:     "OpSuperSpread",
		operands: callSpreadOperands,
	},
	OpCheckThisUninit: {
		name:     "OpCheckThisUninit",
		operands: useOperands,
	},

	OpReturn: {
		name:     "OpReturn",
		operands: useOperands,
	},
	OpReturnUndef: {
		name: "OpReturnUndef",
	},
	OpReturnDerived: {
		name:     "OpReturnDerived",
		operands: usePairOperands,
	},
	OpThrow: {
		name:     "OpThrow",
		operands: useOperands,
	},
	OpRethrow: {
		name: "OpRethrow",
		operands: Operands{
			A:     OperandReg,
			B:     OperandOptReg,
			Reads: regAB,
		},
	},

	OpInitialYield: {
		name: "OpInitialYield",
	},
	OpYield: {
		name: "OpYield",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandFlag,
			Reads:  regB,
			Writes: regA,
		},
	},
	OpAwait: {
		name:     "OpAwait",
		operands: unaryOperands,
	},
	OpYieldDelegateResult: {
		name:     "OpYieldDelegateResult",
		operands: valueOrJumpOperands,
	},
	OpGenResumeMode: {
		name:     "OpGenResumeMode",
		operands: loadOperands,
	},

	OpNewObject: {
		name: "OpNewObject",
		operands: Operands{
			A:      OperandReg,
			C:      OperandInt,
			Writes: regA,
		},
	},
	OpNewArray: {
		name: "OpNewArray",
		operands: Operands{
			A:      OperandReg,
			C:      OperandInt,
			Writes: regA,
		},
	},
	OpNewArrayK: {
		name: "OpNewArrayK",
		operands: Operands{
			A:      OperandReg,
			B:      OperandConstRun,
			C:      OperandInt,
			Writes: regA,
		},
	},
	OpArrayPush: {
		name:     "OpArrayPush",
		operands: usePairOperands,
	},
	OpArrayHole: {
		name:     "OpArrayHole",
		operands: useOperands,
	},
	OpArraySpread: {
		name:     "OpArraySpread",
		operands: usePairOperands,
	},
	OpCopyProps: {
		name:     "OpCopyProps",
		operands: usePairOperands,
	},
	OpCopyPropsExcept: {
		name: "OpCopyPropsExcept",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandReg,
			Extra:  true,
			XA:     OperandCount,
			Reads:  []Regs{{At: FieldB, Len: 1}, {At: FieldC, Counted: true}},
			Writes: regA,
		},
	},
	OpSetProto: {
		name:     "OpSetProto",
		operands: usePairOperands,
	},
	OpTemplate: {
		name: "OpTemplate",
		operands: Operands{
			A:      OperandReg,
			B:      OperandTemplate,
			Writes: regA,
		},
	},
	OpRegExp: {
		name: "OpRegExp",
		operands: Operands{
			A:      OperandReg,
			B:      OperandRegExp,
			Writes: regA,
		},
	},
	OpRegExpArg: {
		name: "OpRegExpArg",
		operands: Operands{
			A:      OperandReg,
			B:      OperandRegExp,
			C:      OperandReg,
			Reads:  regAC,
			Writes: regA,
		},
	},
	OpRegExpMethod: {
		name: "OpRegExpMethod",
		operands: Operands{
			A:      OperandReg,
			B:      OperandRegExp,
			C:      OperandSite,
			Writes: regPairA,
		},
	},

	OpDefineField: {
		name:     "OpDefineField",
		operands: storeOperands,
	},
	OpDefineFieldK: {
		name:     "OpDefineFieldK",
		operands: setPropOperands,
	},
	OpDefineMethod: {
		name:     "OpDefineMethod",
		operands: storeOperands,
	},
	OpDefineGetter: {
		name:     "OpDefineGetter",
		operands: accessorOperands,
	},
	OpDefineSetter: {
		name:     "OpDefineSetter",
		operands: accessorOperands,
	},
	OpSetFuncName: {
		name: "OpSetFuncName",
		operands: Operands{
			A:     OperandReg,
			B:     OperandReg,
			C:     OperandNamePrefix,
			Reads: regAB,
		},
	},
	OpSetHome: {
		name:     "OpSetHome",
		operands: usePairOperands,
	},
	OpClass: {
		name: "OpClass",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandOptReg,
			Reads:  regBC,
			Writes: regA,
		},
	},
	OpPrivateName: {
		name: "OpPrivateName",
		operands: Operands{
			A:      OperandReg,
			B:      OperandKey,
			C:      OperandFlag,
			Writes: regA,
		},
	},
	OpInitFields: {
		name:     "OpInitFields",
		operands: usePairOperands,
	},
	OpAddField: {
		name: "OpAddField",
		operands: Operands{
			A:     OperandReg,
			B:     OperandReg,
			C:     OperandReg,
			Extra: true,
			XA:    OperandElem,
			Reads: regABC,
		},
	},
	OpAutoAccessor: {
		name: "OpAutoAccessor",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			Reads:  regB,
			Writes: []Regs{{At: FieldA, Len: 3}},
		},
	},

	OpDecorateBegin: {
		name:     "OpDecorateBegin",
		operands: unaryOperands,
	},
	OpDecorateAdd: {
		name:     "OpDecorateAdd",
		operands: storeOperands,
	},
	OpDecorateClass: {
		name:     "OpDecorateClass",
		operands: useOperands,
	},
	OpDecorateElement: {
		name: "OpDecorateElement",
		operands: Operands{
			A:     OperandReg,
			B:     OperandOptReg,
			C:     OperandOptReg,
			Extra: true,
			XA:    OperandElem,
			Reads: regABC,
		},
	},
	OpDecorateApply: {
		name:     "OpDecorateApply",
		operands: binaryOperands,
	},
	OpDecorateFinish: {
		name:     "OpDecorateFinish",
		operands: usePairOperands,
	},

	OpForIn: {
		name:     "OpForIn",
		operands: unaryOperands,
	},
	OpForInNext: {
		name:     "OpForInNext",
		operands: valueOrJumpOperands,
	},
	OpGetIterator: {
		name: "OpGetIterator",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandIter,
			Reads:  regB,
			Writes: regPairA,
		},
	},
	OpIterNext: {
		name: "OpIterNext",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandTarget,
			Reads:  []Regs{{At: FieldB, Len: 2}},
			Writes: []Regs{{At: FieldA, Len: 1}, {At: FieldB, Len: 2}},
		},
	},
	OpIterClose: {
		name: "OpIterClose",
		operands: Operands{
			A:      OperandReg,
			B:      OperandOptReg,
			Reads:  []Regs{{At: FieldA, Len: 2}, {At: FieldB, Len: 1}},
			Writes: regA,
		},
	},
	OpIterCall: {
		name: "OpIterCall",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandKey,
			Reads:  regAB,
			Writes: regPairA,
		},
	},
	OpIterReturn: {
		name: "OpIterReturn",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			Reads:  regB,
			Writes: []Regs{{At: FieldA, Len: 2}, {At: FieldB, Len: 1}},
		},
	},
	OpIterCheckResult: {
		name:     "OpIterCheckResult",
		operands: useOperands,
	},
	OpIterResultValue: {
		name:     "OpIterResultValue",
		operands: valueOrJumpOperands,
	},

	OpUsing: {
		name: "OpUsing",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandDispose,
			Reads:  regAB,
			Writes: regA,
		},
	},
	OpDisposeBegin: {
		name:     "OpDisposeBegin",
		operands: storeOperands,
	},
	OpDisposeStep: {
		name:     "OpDisposeStep",
		operands: valueOrJumpOperands,
	},
	OpDisposeError: {
		name:     "OpDisposeError",
		operands: usePairOperands,
	},
	OpDisposeEnd: {
		name: "OpDisposeEnd",
		operands: Operands{
			A:      OperandReg,
			Reads:  regA,
			Writes: regA,
		},
	},

	OpWith: {
		name:     "OpWith",
		operands: useOperands,
	},
	OpPopScope: {
		name: "OpPopScope",
	},
	OpResolveName: {
		name: "OpResolveName",
		operands: Operands{
			A:      OperandReg,
			B:      OperandKey,
			C:      OperandInt,
			Writes: regA,
		},
	},
	OpGetScoped: {
		name:     "OpGetScoped",
		operands: getScopedOperands,
	},
	OpGetScopedStrict: {
		name:     "OpGetScopedStrict",
		operands: getScopedOperands,
	},
	OpSetScoped: {
		name:     "OpSetScoped",
		operands: setScopedOperands,
	},
	OpSetScopedStrict: {
		name:     "OpSetScopedStrict",
		operands: setScopedOperands,
	},
	OpDeleteScoped: {
		name:     "OpDeleteScoped",
		operands: getScopedOperands,
	},
	OpCaptureScopes: {
		name:     "OpCaptureScopes",
		operands: useOperands,
	},
	OpEvalScope: {
		name: "OpEvalScope",
	},
	OpScopeThis: {
		name:     "OpScopeThis",
		operands: unaryOperands,
	},
	OpDeclareEvalVar: {
		name: "OpDeclareEvalVar",
		operands: Operands{
			A:     OperandKey,
			B:     OperandInt,
			C:     OperandOptReg,
			Reads: regC,
		},
	},

	OpImport: {
		name: "OpImport",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandReg,
			Reads:  []Regs{{At: FieldB, Len: 1}, {At: FieldC, Len: 2}},
			Writes: regA,
		},
	},
	OpImportMeta: {
		name:     "OpImportMeta",
		operands: loadOperands,
	},

	OpExtra: {
		name: "OpExtra",
	},
}
