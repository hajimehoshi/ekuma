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

	// OperandGlobalCache is the index of an inline cache of a global variable, below
	// [Func.NumGlobalCaches].
	OperandGlobalCache
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

// Flow says where an instruction can continue within its function, besides its jump targets
// and the handlers of the exceptions that it throws. The next instruction is the one after the
// instruction and its [OpExtra].
type Flow uint8

const (
	// FlowNext continues with the next instruction.
	FlowNext Flow = iota + 1

	// FlowEnd never continues with the next instruction, as a jump, a return or a throw.
	FlowEnd

	// FlowNextOrSkip continues with the next instruction or with the one after it.
	FlowNextOrSkip
)

// Flow returns where an instruction of op can continue, or 0 when op is not an opcode.
func (op Opcode) Flow() Flow {
	if int(op) < len(opcodes) {
		return opcodes[op].flow
	}
	return 0
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

	// fwdReads holds the callee, the this value and the arguments object in c.
	fwdReads = []Regs{{At: FieldB, Len: 2}, {At: FieldC, Len: 1}}

	// applyFwdReads holds the callee, the function, the this value and the arguments object.
	applyFwdReads  = []Regs{{At: FieldB, Len: 2}, {At: FieldC, Len: 1}, {At: FieldXA, Len: 1}}
	applyFwdWrites = []Regs{{At: FieldA, Len: 1}, {At: FieldB, Offset: 2, Len: RegsToEnd}, {At: FieldXA, Len: 1}}
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
		B:      OperandKey,
		C:      OperandGlobalCache,
		Writes: regA,
	}
	setGlobalOperands = Operands{
		A:     OperandReg,
		B:     OperandKey,
		C:     OperandGlobalCache,
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
	storeIntOperands = Operands{
		A:     OperandReg,
		B:     OperandInt,
		C:     OperandReg,
		Reads: regAC,
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
		XA:     OperandReg,
		Reads:  applyFwdReads,
		Writes: applyFwdWrites,
	}
	callFwdOperands = Operands{
		A:      OperandReg,
		B:      OperandReg,
		C:      OperandReg,
		Reads:  fwdReads,
		Writes: regAC,
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

// opcodes holds the name, the operands and the flow of every opcode.
var opcodes = [...]struct {
	name     string
	operands Operands
	flow     Flow
}{
	OpLoadK: {
		name: "OpLoadK",
		operands: Operands{
			A:      OperandReg,
			B:      OperandConst,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpLoadInt: {
		name: "OpLoadInt",
		operands: Operands{
			A:      OperandReg,
			B:      OperandInt,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpLoadUndef: {
		name:     "OpLoadUndef",
		operands: loadOperands,
		flow:     FlowNext,
	},
	OpLoadNull: {
		name:     "OpLoadNull",
		operands: loadOperands,
		flow:     FlowNext,
	},
	OpLoadTrue: {
		name:     "OpLoadTrue",
		operands: loadOperands,
		flow:     FlowNext,
	},
	OpLoadFalse: {
		name:     "OpLoadFalse",
		operands: loadOperands,
		flow:     FlowNext,
	},
	OpLoadTDZ: {
		name:     "OpLoadTDZ",
		operands: loadOperands,
		flow:     FlowNext,
	},
	OpMove: {
		name:     "OpMove",
		operands: unaryOperands,
		flow:     FlowNext,
	},

	OpCheckTDZ: {
		name: "OpCheckTDZ",
		operands: Operands{
			A:     OperandReg,
			B:     OperandKey,
			Reads: regA,
		},
		flow: FlowNext,
	},
	OpRequireCoercible: {
		name:     "OpRequireCoercible",
		operands: useOperands,
		flow:     FlowNext,
	},
	OpThrowConstAssign: {
		name: "OpThrowConstAssign",
		operands: Operands{
			A: OperandKey,
		},
		flow: FlowEnd,
	},
	OpThrowError: {
		name: "OpThrowError",
		operands: Operands{
			A: OperandThrow,
			B: OperandConst,
		},
		flow: FlowEnd,
	},

	OpGetGlobal: {
		name:     "OpGetGlobal",
		operands: getGlobalOperands,
		flow:     FlowNext,
	},
	OpGetGlobalStrict: {
		name:     "OpGetGlobalStrict",
		operands: getGlobalOperands,
		flow:     FlowNext,
	},
	OpGetGlobalOrUndef: {
		name:     "OpGetGlobalOrUndef",
		operands: getGlobalOperands,
		flow:     FlowNext,
	},
	OpGetGlobalOrUndefStrict: {
		name:     "OpGetGlobalOrUndefStrict",
		operands: getGlobalOperands,
		flow:     FlowNext,
	},
	OpSetGlobal: {
		name:     "OpSetGlobal",
		operands: setGlobalOperands,
		flow:     FlowNext,
	},
	OpSetGlobalStrict: {
		name:     "OpSetGlobalStrict",
		operands: setGlobalOperands,
		flow:     FlowNext,
	},
	OpPutGlobal: {
		name:     "OpPutGlobal",
		operands: setGlobalOperands,
		flow:     FlowNext,
	},
	OpPutGlobalStrict: {
		name:     "OpPutGlobalStrict",
		operands: setGlobalOperands,
		flow:     FlowNext,
	},
	OpCopyPropToGlobal: {
		name: "OpCopyPropToGlobal",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandTarget,
			Reads:  regB,
			Writes: regPairA,
		},
		flow: FlowNext,
	},
	OpResolveGlobal: {
		name:     "OpResolveGlobal",
		operands: getGlobalOperands,
		flow:     FlowNext,
	},
	OpDeleteName: {
		name: "OpDeleteName",
		operands: Operands{
			A:      OperandReg,
			B:      OperandKey,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpDeclareGlobalVar: {
		name: "OpDeclareGlobalVar",
		operands: Operands{
			A:     OperandKey,
			B:     OperandGlobalVar,
			C:     OperandOptReg,
			Reads: regC,
		},
		flow: FlowNext,
	},
	OpDeclareGlobalLex: {
		name: "OpDeclareGlobalLex",
		operands: Operands{
			A: OperandKey,
			B: OperandFlag,
		},
		flow: FlowNext,
	},
	OpInitGlobalLex: {
		name: "OpInitGlobalLex",
		operands: Operands{
			A:     OperandReg,
			B:     OperandKey,
			Reads: regA,
		},
		flow: FlowNext,
	},
	OpCheckGlobalDecl: {
		name: "OpCheckGlobalDecl",
		operands: Operands{
			A: OperandKey,
			B: OperandDecl,
		},
		flow: FlowNext,
	},
	OpDeclareAnnexB: {
		name: "OpDeclareAnnexB",
		operands: Operands{
			A:      OperandReg,
			B:      OperandKey,
			C:      OperandGlobalVar,
			Writes: regA,
		},
		flow: FlowNext,
	},

	OpGetUpval: {
		name: "OpGetUpval",
		operands: Operands{
			A:      OperandReg,
			B:      OperandUpval,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpGetUpvalChecked: {
		name: "OpGetUpvalChecked",
		operands: Operands{
			A:      OperandReg,
			B:      OperandUpval,
			C:      OperandKey,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpSetUpval: {
		name: "OpSetUpval",
		operands: Operands{
			A:     OperandReg,
			B:     OperandUpval,
			Reads: regA,
		},
		flow: FlowNext,
	},
	OpIncUpval: {
		name: "OpIncUpval",
		operands: Operands{
			A: OperandUpval,
			B: OperandKey,
		},
		flow: FlowNext,
	},
	OpDecUpval: {
		name: "OpDecUpval",
		operands: Operands{
			A: OperandUpval,
			B: OperandKey,
		},
		flow: FlowNext,
	},
	OpCloseUpvals: {
		name: "OpCloseUpvals",
		operands: Operands{
			A:     OperandReg,
			Reads: []Regs{{At: FieldA, Len: RegsToEnd}},
		},
		flow: FlowNext,
	},
	OpClosure: {
		name: "OpClosure",
		operands: Operands{
			A:      OperandReg,
			B:      OperandProto,
			Writes: regA,
		},
		flow: FlowNext,
	},

	OpLoadThis: {
		name:     "OpLoadThis",
		operands: loadOperands,
		flow:     FlowNext,
	},
	OpLoadThisSloppy: {
		name:     "OpLoadThisSloppy",
		operands: loadOperands,
		flow:     FlowNext,
	},
	OpLoadCallee: {
		name:     "OpLoadCallee",
		operands: loadOperands,
		flow:     FlowNext,
	},
	OpLoadNewTarget: {
		name:     "OpLoadNewTarget",
		operands: loadOperands,
		flow:     FlowNext,
	},
	OpLoadHome: {
		name:     "OpLoadHome",
		operands: loadOperands,
		flow:     FlowNext,
	},
	OpArguments: {
		name: "OpArguments",
		operands: Operands{
			A:      OperandReg,
			B:      OperandFlag,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpRestArgs: {
		name: "OpRestArgs",
		operands: Operands{
			A:      OperandReg,
			B:      OperandInt,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpGetArgsLength: {
		name: "OpGetArgsLength",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandSite,
			Reads:  regB,
			Writes: regAB,
		},
		flow: FlowNext,
	},
	OpGetArgsIndex: {
		name: "OpGetArgsIndex",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandReg,
			Reads:  regBC,
			Writes: regAB,
		},
		flow: FlowNext,
	},
	OpGetArgsIndexInt: {
		name: "OpGetArgsIndexInt",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandInt,
			Reads:  regB,
			Writes: regAB,
		},
		flow: FlowNext,
	},

	OpGetProp: {
		name:     "OpGetProp",
		operands: getPropOperands,
		flow:     FlowNext,
	},
	OpGetPropChain: {
		name:     "OpGetPropChain",
		operands: getPropOperands,
		flow:     FlowNextOrSkip,
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
		flow: FlowNext,
	},
	OpGetLength: {
		name:     "OpGetLength",
		operands: getPropOperands,
		flow:     FlowNext,
	},
	OpGetIndex: {
		name:     "OpGetIndex",
		operands: binaryOperands,
		flow:     FlowNext,
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
		flow: FlowNext,
	},
	OpSetProp: {
		name:     "OpSetProp",
		operands: setPropOperands,
		flow:     FlowNext,
	},
	OpSetPropStrict: {
		name:     "OpSetPropStrict",
		operands: setPropOperands,
		flow:     FlowNext,
	},
	OpSetIndex: {
		name:     "OpSetIndex",
		operands: storeOperands,
		flow:     FlowNext,
	},
	OpSetIndexStrict: {
		name:     "OpSetIndexStrict",
		operands: storeOperands,
		flow:     FlowNext,
	},
	OpSetIndexInt: {
		name:     "OpSetIndexInt",
		operands: storeIntOperands,
		flow:     FlowNext,
	},
	OpSetIndexIntStrict: {
		name:     "OpSetIndexIntStrict",
		operands: storeIntOperands,
		flow:     FlowNext,
	},
	OpDeleteProp: {
		name:     "OpDeleteProp",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpDeletePropStrict: {
		name:     "OpDeletePropStrict",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpIn: {
		name:     "OpIn",
		operands: binaryOperands,
		flow:     FlowNext,
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
		flow: FlowNext,
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
		flow: FlowNext,
	},

	OpSuperBase: {
		name:     "OpSuperBase",
		operands: unaryOperands,
		flow:     FlowNext,
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
		flow: FlowNext,
	},
	OpSetSuper: {
		name:     "OpSetSuper",
		operands: superStoreOperands,
		flow:     FlowNext,
	},
	OpSetSuperStrict: {
		name:     "OpSetSuperStrict",
		operands: superStoreOperands,
		flow:     FlowNext,
	},

	OpHasPrivate: {
		name:     "OpHasPrivate",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpGetPrivate: {
		name:     "OpGetPrivate",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpSetPrivate: {
		name:     "OpSetPrivate",
		operands: storeOperands,
		flow:     FlowNext,
	},

	OpAdd: {
		name:     "OpAdd",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpSub: {
		name:     "OpSub",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpMul: {
		name:     "OpMul",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpMulAdd: {
		name:     "OpMulAdd",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpDiv: {
		name:     "OpDiv",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpMod: {
		name:     "OpMod",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpExp: {
		name:     "OpExp",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpBitOr: {
		name:     "OpBitOr",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpBitAnd: {
		name:     "OpBitAnd",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpBitXor: {
		name:     "OpBitXor",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpShl: {
		name:     "OpShl",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpShr: {
		name:     "OpShr",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpUShr: {
		name:     "OpUShr",
		operands: binaryOperands,
		flow:     FlowNext,
	},

	OpAddK: {
		name:     "OpAddK",
		operands: binaryKOperands,
		flow:     FlowNext,
	},
	OpSubK: {
		name:     "OpSubK",
		operands: binaryKOperands,
		flow:     FlowNext,
	},
	OpMulK: {
		name:     "OpMulK",
		operands: binaryKOperands,
		flow:     FlowNext,
	},
	OpDivK: {
		name:     "OpDivK",
		operands: binaryKOperands,
		flow:     FlowNext,
	},
	OpModK: {
		name:     "OpModK",
		operands: binaryKOperands,
		flow:     FlowNext,
	},
	OpBitOrK: {
		name:     "OpBitOrK",
		operands: binaryKOperands,
		flow:     FlowNext,
	},
	OpBitAndK: {
		name:     "OpBitAndK",
		operands: binaryKOperands,
		flow:     FlowNext,
	},
	OpShlK: {
		name:     "OpShlK",
		operands: binaryKOperands,
		flow:     FlowNext,
	},
	OpShrK: {
		name:     "OpShrK",
		operands: binaryKOperands,
		flow:     FlowNext,
	},
	OpUShrK: {
		name:     "OpUShrK",
		operands: binaryKOperands,
		flow:     FlowNext,
	},

	OpNeg: {
		name:     "OpNeg",
		operands: unaryOperands,
		flow:     FlowNext,
	},
	OpPlus: {
		name:     "OpPlus",
		operands: unaryOperands,
		flow:     FlowNext,
	},
	OpNot: {
		name:     "OpNot",
		operands: unaryOperands,
		flow:     FlowNext,
	},
	OpBitNot: {
		name:     "OpBitNot",
		operands: unaryOperands,
		flow:     FlowNext,
	},
	OpTypeof: {
		name:     "OpTypeof",
		operands: unaryOperands,
		flow:     FlowNext,
	},
	OpInc: {
		name:     "OpInc",
		operands: unaryOperands,
		flow:     FlowNext,
	},
	OpDec: {
		name:     "OpDec",
		operands: unaryOperands,
		flow:     FlowNext,
	},
	OpToNumeric: {
		name:     "OpToNumeric",
		operands: unaryOperands,
		flow:     FlowNext,
	},
	OpPostInc: {
		name: "OpPostInc",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			Reads:  regB,
			Writes: regAB,
		},
		flow: FlowNext,
	},
	OpPostDec: {
		name: "OpPostDec",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			Reads:  regB,
			Writes: regAB,
		},
		flow: FlowNext,
	},
	OpIncGetIndex: {
		name:     "OpIncGetIndex",
		operands: unaryOperands,
		flow:     FlowNextOrSkip,
	},
	OpPostIncGetIndex: {
		name: "OpPostIncGetIndex",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			Reads:  regB,
			Writes: regAB,
		},
		flow: FlowNextOrSkip,
	},
	OpToString: {
		name:     "OpToString",
		operands: unaryOperands,
		flow:     FlowNext,
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
		flow: FlowNext,
	},
	OpAppend: {
		name: "OpAppend",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			Reads:  regAB,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpSeal: {
		name: "OpSeal",
		operands: Operands{
			A:     OperandReg,
			Reads: regA,
		},
		flow: FlowNext,
	},
	OpAppendK: {
		name: "OpAppendK",
		operands: Operands{
			A:      OperandReg,
			B:      OperandConst,
			Reads:  regA,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpPrepend: {
		name: "OpPrepend",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			Reads:  regAB,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpPrependK: {
		name: "OpPrependK",
		operands: Operands{
			A:      OperandReg,
			B:      OperandConst,
			C:      OperandFlag,
			Reads:  regA,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpConcatK: {
		name:     "OpConcatK",
		operands: binaryKOperands,
		flow:     FlowNext,
	},

	OpLt: {
		name:     "OpLt",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpLe: {
		name:     "OpLe",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpGt: {
		name:     "OpGt",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpGe: {
		name:     "OpGe",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpEq: {
		name:     "OpEq",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpNe: {
		name:     "OpNe",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpStrictEq: {
		name:     "OpStrictEq",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpStrictNe: {
		name:     "OpStrictNe",
		operands: binaryOperands,
		flow:     FlowNext,
	},

	OpJump: {
		name: "OpJump",
		operands: Operands{
			A: OperandTarget,
		},
		flow: FlowEnd,
	},
	OpJumpIf: {
		name:     "OpJumpIf",
		operands: jumpIfOperands,
		flow:     FlowNext,
	},
	OpJumpIfNot: {
		name:     "OpJumpIfNot",
		operands: jumpIfOperands,
		flow:     FlowNext,
	},
	OpJumpIfNullish: {
		name:     "OpJumpIfNullish",
		operands: jumpNullishOperands,
		flow:     FlowNext,
	},
	OpJumpIfNotNullish: {
		name:     "OpJumpIfNotNullish",
		operands: jumpNullishOperands,
		flow:     FlowNext,
	},
	OpJumpIfUndef: {
		name:     "OpJumpIfUndef",
		operands: jumpIfOperands,
		flow:     FlowNext,
	},
	OpJumpIfNotUndef: {
		name:     "OpJumpIfNotUndef",
		operands: jumpIfOperands,
		flow:     FlowNext,
	},
	OpJumpIfInt: {
		name: "OpJumpIfInt",
		operands: Operands{
			A:     OperandTarget,
			B:     OperandReg,
			C:     OperandInt,
			Reads: regB,
		},
		flow: FlowNext,
	},

	OpJumpNotLt: {
		name:     "OpJumpNotLt",
		operands: jumpCmpOperands,
		flow:     FlowNext,
	},
	OpJumpNotLe: {
		name:     "OpJumpNotLe",
		operands: jumpCmpOperands,
		flow:     FlowNext,
	},
	OpJumpNotGt: {
		name:     "OpJumpNotGt",
		operands: jumpCmpOperands,
		flow:     FlowNext,
	},
	OpJumpNotGe: {
		name:     "OpJumpNotGe",
		operands: jumpCmpOperands,
		flow:     FlowNext,
	},
	OpJumpNotEq: {
		name:     "OpJumpNotEq",
		operands: jumpCmpOperands,
		flow:     FlowNext,
	},
	OpJumpNotNe: {
		name:     "OpJumpNotNe",
		operands: jumpCmpOperands,
		flow:     FlowNext,
	},
	OpJumpNotSEq: {
		name:     "OpJumpNotSEq",
		operands: jumpCmpOperands,
		flow:     FlowNext,
	},
	OpJumpNotSNe: {
		name:     "OpJumpNotSNe",
		operands: jumpCmpOperands,
		flow:     FlowNext,
	},
	OpJumpNotLtK: {
		name:     "OpJumpNotLtK",
		operands: jumpCmpKOperands,
		flow:     FlowNext,
	},
	OpJumpNotLeK: {
		name:     "OpJumpNotLeK",
		operands: jumpCmpKOperands,
		flow:     FlowNext,
	},
	OpJumpNotGtK: {
		name:     "OpJumpNotGtK",
		operands: jumpCmpKOperands,
		flow:     FlowNext,
	},
	OpJumpNotGeK: {
		name:     "OpJumpNotGeK",
		operands: jumpCmpKOperands,
		flow:     FlowNext,
	},
	OpJumpNotSEqK: {
		name:     "OpJumpNotSEqK",
		operands: jumpCmpKOperands,
		flow:     FlowNext,
	},
	OpJumpNotSNeK: {
		name:     "OpJumpNotSNeK",
		operands: jumpCmpKOperands,
		flow:     FlowNext,
	},
	OpJumpNotSEqP: {
		name:     "OpJumpNotSEqP",
		operands: jumpCmpPOperands,
		flow:     FlowNext,
	},
	OpJumpNotSNeP: {
		name:     "OpJumpNotSNeP",
		operands: jumpCmpPOperands,
		flow:     FlowNext,
	},

	OpLoopIf: {
		name:     "OpLoopIf",
		operands: jumpIfOperands,
		flow:     FlowNext,
	},
	OpLoopLt: {
		name:     "OpLoopLt",
		operands: jumpCmpOperands,
		flow:     FlowNext,
	},
	OpLoopLe: {
		name:     "OpLoopLe",
		operands: jumpCmpOperands,
		flow:     FlowNext,
	},
	OpLoopGt: {
		name:     "OpLoopGt",
		operands: jumpCmpOperands,
		flow:     FlowNext,
	},
	OpLoopGe: {
		name:     "OpLoopGe",
		operands: jumpCmpOperands,
		flow:     FlowNext,
	},
	OpLoopLtK: {
		name:     "OpLoopLtK",
		operands: jumpCmpKOperands,
		flow:     FlowNext,
	},
	OpLoopLeK: {
		name:     "OpLoopLeK",
		operands: jumpCmpKOperands,
		flow:     FlowNext,
	},
	OpLoopGtK: {
		name:     "OpLoopGtK",
		operands: jumpCmpKOperands,
		flow:     FlowNext,
	},
	OpLoopGeK: {
		name:     "OpLoopGeK",
		operands: jumpCmpKOperands,
		flow:     FlowNext,
	},
	OpLoopSEqP: {
		name:     "OpLoopSEqP",
		operands: jumpCmpPOperands,
		flow:     FlowNext,
	},
	OpLoopSNeP: {
		name:     "OpLoopSNeP",
		operands: jumpCmpPOperands,
		flow:     FlowNext,
	},
	OpLoopNullish: {
		name:     "OpLoopNullish",
		operands: jumpNullishOperands,
		flow:     FlowNext,
	},
	OpLoopNotNullish: {
		name:     "OpLoopNotNullish",
		operands: jumpNullishOperands,
		flow:     FlowNext,
	},

	OpCall: {
		name:     "OpCall",
		operands: callOperands,
		flow:     FlowNext,
	},
	OpCallUndef: {
		name:     "OpCallUndef",
		operands: callUndefOperands,
		flow:     FlowNext,
	},
	OpCallCall: {
		name:     "OpCallCall",
		operands: callOperands,
		flow:     FlowNext,
	},
	OpApplyFwd: {
		name:     "OpApplyFwd",
		operands: applyFwdOperands,
		flow:     FlowNext,
	},
	OpCallFwd: {
		name:     "OpCallFwd",
		operands: callFwdOperands,
		flow:     FlowNext,
	},
	OpCallSpread: {
		name:     "OpCallSpread",
		operands: callSpreadOperands,
		flow:     FlowNext,
	},
	OpCallEval: {
		name:     "OpCallEval",
		operands: callEvalOperands,
		flow:     FlowNext,
	},
	OpCallEvalSpread: {
		name:     "OpCallEvalSpread",
		operands: callEvalSpreadOperands,
		flow:     FlowNext,
	},
	OpSpreadArgs: {
		name:     "OpSpreadArgs",
		operands: unaryOperands,
		flow:     FlowNext,
	},

	OpTailCall: {
		name:     "OpTailCall",
		operands: callOperands,
		flow:     FlowNext,
	},
	OpTailCallUndef: {
		name:     "OpTailCallUndef",
		operands: callUndefOperands,
		flow:     FlowNext,
	},
	OpTailCallSpread: {
		name:     "OpTailCallSpread",
		operands: callSpreadOperands,
		flow:     FlowNext,
	},
	OpTailCallEval: {
		name:     "OpTailCallEval",
		operands: callEvalOperands,
		flow:     FlowNext,
	},
	OpTailCallEvalSpread: {
		name:     "OpTailCallEvalSpread",
		operands: callEvalSpreadOperands,
		flow:     FlowNext,
	},
	OpTailApplyFwd: {
		name:     "OpTailApplyFwd",
		operands: applyFwdOperands,
		flow:     FlowNext,
	},
	OpTailCallFwd: {
		name:     "OpTailCallFwd",
		operands: callFwdOperands,
		flow:     FlowNext,
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
		flow: FlowNext,
	},
	OpNewSpread: {
		name:     "OpNewSpread",
		operands: callSpreadOperands,
		flow:     FlowNext,
	},
	OpSuperCall: {
		name:     "OpSuperCall",
		operands: callOperands,
		flow:     FlowNext,
	},
	OpSuperSpread: {
		name:     "OpSuperSpread",
		operands: callSpreadOperands,
		flow:     FlowNext,
	},
	OpCheckThisUninit: {
		name:     "OpCheckThisUninit",
		operands: useOperands,
		flow:     FlowNext,
	},

	OpReturn: {
		name:     "OpReturn",
		operands: useOperands,
		flow:     FlowEnd,
	},
	OpReturnUndef: {
		name: "OpReturnUndef",
		flow: FlowEnd,
	},
	OpReturnDerived: {
		name:     "OpReturnDerived",
		operands: usePairOperands,
		flow:     FlowEnd,
	},
	OpThrow: {
		name:     "OpThrow",
		operands: useOperands,
		flow:     FlowEnd,
	},
	OpRethrow: {
		name: "OpRethrow",
		operands: Operands{
			A:     OperandReg,
			B:     OperandOptReg,
			Reads: regAB,
		},
		flow: FlowEnd,
	},

	OpInitialYield: {
		name: "OpInitialYield",
		flow: FlowNext,
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
		flow: FlowNext,
	},
	OpAwait: {
		name:     "OpAwait",
		operands: unaryOperands,
		flow:     FlowNext,
	},
	OpYieldDelegateResult: {
		name:     "OpYieldDelegateResult",
		operands: valueOrJumpOperands,
		flow:     FlowNext,
	},
	OpGenResumeMode: {
		name:     "OpGenResumeMode",
		operands: loadOperands,
		flow:     FlowNext,
	},

	OpGenFinally: {
		name: "OpGenFinally",
		operands: Operands{
			A:      OperandReg,
			B:      OperandInt,
			Reads:  []Regs{{At: FieldA, Offset: 1, Len: 1}},
			Writes: regPairA,
		},
		flow: FlowNext,
	},

	OpNewObject: {
		name: "OpNewObject",
		operands: Operands{
			A:      OperandReg,
			C:      OperandInt,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpNewArray: {
		name: "OpNewArray",
		operands: Operands{
			A:      OperandReg,
			C:      OperandInt,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpNewArrayK: {
		name: "OpNewArrayK",
		operands: Operands{
			A:      OperandReg,
			B:      OperandConstRun,
			C:      OperandInt,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpArrayPush: {
		name:     "OpArrayPush",
		operands: usePairOperands,
		flow:     FlowNext,
	},
	OpArrayHole: {
		name:     "OpArrayHole",
		operands: useOperands,
		flow:     FlowNext,
	},
	OpArraySpread: {
		name:     "OpArraySpread",
		operands: usePairOperands,
		flow:     FlowNext,
	},
	OpCopyProps: {
		name:     "OpCopyProps",
		operands: usePairOperands,
		flow:     FlowNext,
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
		flow: FlowNext,
	},
	OpSetProto: {
		name:     "OpSetProto",
		operands: usePairOperands,
		flow:     FlowNext,
	},
	OpTemplate: {
		name: "OpTemplate",
		operands: Operands{
			A:      OperandReg,
			B:      OperandTemplate,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpRegExp: {
		name: "OpRegExp",
		operands: Operands{
			A:      OperandReg,
			B:      OperandRegExp,
			Writes: regA,
		},
		flow: FlowNext,
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
		flow: FlowNext,
	},
	OpRegExpMethod: {
		name: "OpRegExpMethod",
		operands: Operands{
			A:      OperandReg,
			B:      OperandRegExp,
			C:      OperandSite,
			Writes: regPairA,
		},
		flow: FlowNext,
	},

	OpDefineField: {
		name:     "OpDefineField",
		operands: storeOperands,
		flow:     FlowNext,
	},
	OpDefineFieldK: {
		name:     "OpDefineFieldK",
		operands: setPropOperands,
		flow:     FlowNext,
	},
	OpDefineMethod: {
		name:     "OpDefineMethod",
		operands: storeOperands,
		flow:     FlowNext,
	},
	OpDefineGetter: {
		name:     "OpDefineGetter",
		operands: accessorOperands,
		flow:     FlowNext,
	},
	OpDefineSetter: {
		name:     "OpDefineSetter",
		operands: accessorOperands,
		flow:     FlowNext,
	},
	OpSetFuncName: {
		name: "OpSetFuncName",
		operands: Operands{
			A:     OperandReg,
			B:     OperandReg,
			C:     OperandNamePrefix,
			Reads: regAB,
		},
		flow: FlowNext,
	},
	OpSetHome: {
		name:     "OpSetHome",
		operands: usePairOperands,
		flow:     FlowNext,
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
		flow: FlowNext,
	},
	OpPrivateName: {
		name: "OpPrivateName",
		operands: Operands{
			A:      OperandReg,
			B:      OperandKey,
			C:      OperandFlag,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpInitFields: {
		name:     "OpInitFields",
		operands: usePairOperands,
		flow:     FlowNext,
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
		flow: FlowNext,
	},
	OpAutoAccessor: {
		name: "OpAutoAccessor",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			Reads:  regB,
			Writes: []Regs{{At: FieldA, Len: 3}},
		},
		flow: FlowNext,
	},

	OpDecorateBegin: {
		name:     "OpDecorateBegin",
		operands: unaryOperands,
		flow:     FlowNext,
	},
	OpDecorateAdd: {
		name:     "OpDecorateAdd",
		operands: storeOperands,
		flow:     FlowNext,
	},
	OpDecorateClass: {
		name:     "OpDecorateClass",
		operands: useOperands,
		flow:     FlowNext,
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
		flow: FlowNext,
	},
	OpDecorateApply: {
		name:     "OpDecorateApply",
		operands: binaryOperands,
		flow:     FlowNext,
	},
	OpDecorateFinish: {
		name:     "OpDecorateFinish",
		operands: usePairOperands,
		flow:     FlowNext,
	},

	OpForIn: {
		name:     "OpForIn",
		operands: unaryOperands,
		flow:     FlowNext,
	},
	OpForInNext: {
		name:     "OpForInNext",
		operands: valueOrJumpOperands,
		flow:     FlowNext,
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
		flow: FlowNext,
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
		flow: FlowNext,
	},
	OpIterElision: {
		name: "OpIterElision",
		operands: Operands{
			A:      OperandReg,
			Reads:  regPairA,
			Writes: regPairA,
		},
		flow: FlowNext,
	},
	OpIterRest: {
		name: "OpIterRest",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			Reads:  []Regs{{At: FieldB, Len: 2}},
			Writes: []Regs{{At: FieldA, Len: 1}, {At: FieldB, Len: 2}},
		},
		flow: FlowNext,
	},
	OpIterNextPair: {
		name: "OpIterNextPair",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			C:      OperandTarget,
			Extra:  true,
			XA:     OperandReg,
			Reads:  []Regs{{At: FieldB, Len: 2}},
			Writes: []Regs{{At: FieldA, Len: 1}, {At: FieldB, Len: 2}, {At: FieldXA, Len: 1}},
		},
		flow: FlowNext,
	},
	OpIterClose: {
		name: "OpIterClose",
		operands: Operands{
			A:      OperandReg,
			B:      OperandOptReg,
			Reads:  []Regs{{At: FieldA, Len: 2}, {At: FieldB, Len: 1}},
			Writes: regPairA,
		},
		flow: FlowNext,
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
		flow: FlowNext,
	},
	OpIterReturn: {
		name: "OpIterReturn",
		operands: Operands{
			A:      OperandReg,
			B:      OperandReg,
			Reads:  regB,
			Writes: []Regs{{At: FieldA, Len: 2}, {At: FieldB, Len: 1}},
		},
		flow: FlowNext,
	},
	OpIterCheckResult: {
		name:     "OpIterCheckResult",
		operands: useOperands,
		flow:     FlowNext,
	},
	OpIterResultValue: {
		name:     "OpIterResultValue",
		operands: valueOrJumpOperands,
		flow:     FlowNext,
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
		flow: FlowNext,
	},
	OpDisposeBegin: {
		name:     "OpDisposeBegin",
		operands: storeOperands,
		flow:     FlowNext,
	},
	OpDisposeStep: {
		name:     "OpDisposeStep",
		operands: valueOrJumpOperands,
		flow:     FlowNext,
	},
	OpDisposeError: {
		name:     "OpDisposeError",
		operands: usePairOperands,
		flow:     FlowNext,
	},
	OpDisposeEnd: {
		name: "OpDisposeEnd",
		operands: Operands{
			A:      OperandReg,
			Reads:  regA,
			Writes: regA,
		},
		flow: FlowNext,
	},

	OpWith: {
		name:     "OpWith",
		operands: useOperands,
		flow:     FlowNext,
	},
	OpPopScope: {
		name: "OpPopScope",
		flow: FlowNext,
	},
	OpResolveName: {
		name: "OpResolveName",
		operands: Operands{
			A:      OperandReg,
			B:      OperandKey,
			C:      OperandInt,
			Writes: regA,
		},
		flow: FlowNext,
	},
	OpGetScoped: {
		name:     "OpGetScoped",
		operands: getScopedOperands,
		flow:     FlowNext,
	},
	OpGetScopedStrict: {
		name:     "OpGetScopedStrict",
		operands: getScopedOperands,
		flow:     FlowNext,
	},
	OpSetScoped: {
		name:     "OpSetScoped",
		operands: setScopedOperands,
		flow:     FlowNext,
	},
	OpSetScopedStrict: {
		name:     "OpSetScopedStrict",
		operands: setScopedOperands,
		flow:     FlowNext,
	},
	OpDeleteScoped: {
		name:     "OpDeleteScoped",
		operands: getScopedOperands,
		flow:     FlowNext,
	},
	OpCaptureScopes: {
		name:     "OpCaptureScopes",
		operands: useOperands,
		flow:     FlowNext,
	},
	OpEvalScope: {
		name: "OpEvalScope",
		flow: FlowNext,
	},
	OpScopeThis: {
		name:     "OpScopeThis",
		operands: unaryOperands,
		flow:     FlowNext,
	},
	OpDeclareEvalVar: {
		name: "OpDeclareEvalVar",
		operands: Operands{
			A:     OperandKey,
			B:     OperandInt,
			C:     OperandOptReg,
			Reads: regC,
		},
		flow: FlowNext,
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
		flow: FlowNext,
	},
	OpImportMeta: {
		name:     "OpImportMeta",
		operands: loadOperands,
		flow:     FlowNext,
	},

	OpExtra: {
		name: "OpExtra",
		flow: FlowNext,
	},
}
