// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package parser

import (
	"github.com/hajimehoshi/ekuma/internal/ast"
)

type tokKind uint8

const (
	tEOF tokKind = iota

	tNumber
	tBigInt
	tString
	tTemplate
	tRegExp
	tPrivateName

	tLBrace
	tRBrace
	tLParen
	tRParen
	tLBrack
	tRBrack
	tDot
	tEllipsis
	tSemi
	tComma
	tQuestion
	tQuestionDot
	tColon
	tArrow

	tAssign
	tAddAssign
	tSubAssign
	tMulAssign
	tDivAssign
	tModAssign
	tExpAssign
	tShlAssign
	tShrAssign
	tUShrAssign
	tBitAndAssign
	tBitOrAssign
	tBitXorAssign
	tAndAssign
	tOrAssign
	tCoalesceAssign

	tAdd
	tSub
	tMul
	tDiv
	tMod
	tExp
	tShl
	tShr
	tUShr
	tBitAnd
	tBitOr
	tBitXor
	tEq
	tNe
	tStrictEq
	tStrictNe
	tLt
	tLe
	tGt
	tGe
	tAnd
	tOr
	tCoalesce
	tNot
	tTilde
	tInc
	tDec
	// tAt is the @ that starts a decorator.
	tAt

	// tIdent and every kind after it is a word: its token text is an IdentifierName.
	tIdent
	// tEscKeyword is a reserved word written with a Unicode escape.
	tEscKeyword

	tBreak
	tCase
	tCatch
	tClass
	tConst
	tContinue
	tDebugger
	tDefault
	tDelete
	tDo
	tElse
	tEnum
	tExport
	tExtends
	tFalse
	tFinally
	tFor
	tFunction
	tIf
	tImport
	tIn
	tInstanceof
	tNew
	tNull
	tReturn
	tSuper
	tSwitch
	tThis
	tThrow
	tTrue
	tTry
	tTypeof
	tVar
	tVoid
	tWhile
	tWith
)

func (k tokKind) isWord() bool {
	return k >= tIdent
}

// token is the current token of the lexer.
type token struct {
	kind tokKind

	// nl reports whether a line terminator precedes the token.
	nl bool

	// esc reports whether a word was written with a Unicode escape.
	esc bool

	// octal reports a legacy octal or non-octal-decimal number, or a string with a legacy escape.
	octal bool

	// tail reports whether a template token ends the template.
	tail bool

	// cooked reports whether a template token has a cooked value.
	cooked bool

	start int32
	end   int32

	// str is the name of a word or private name, the value of a string, the cooked value of a template,
	// the digits of a BigInt, or the pattern of a regular expression.
	str string

	// raw is the raw value of a template or the flags of a regular expression.
	raw string

	num float64
}

func keywordKind(s string) tokKind {
	switch s {
	case "break":
		return tBreak
	case "case":
		return tCase
	case "catch":
		return tCatch
	case "class":
		return tClass
	case "const":
		return tConst
	case "continue":
		return tContinue
	case "debugger":
		return tDebugger
	case "default":
		return tDefault
	case "delete":
		return tDelete
	case "do":
		return tDo
	case "else":
		return tElse
	case "enum":
		return tEnum
	case "export":
		return tExport
	case "extends":
		return tExtends
	case "false":
		return tFalse
	case "finally":
		return tFinally
	case "for":
		return tFor
	case "function":
		return tFunction
	case "if":
		return tIf
	case "import":
		return tImport
	case "in":
		return tIn
	case "instanceof":
		return tInstanceof
	case "new":
		return tNew
	case "null":
		return tNull
	case "return":
		return tReturn
	case "super":
		return tSuper
	case "switch":
		return tSwitch
	case "this":
		return tThis
	case "throw":
		return tThrow
	case "true":
		return tTrue
	case "try":
		return tTry
	case "typeof":
		return tTypeof
	case "var":
		return tVar
	case "void":
		return tVoid
	case "while":
		return tWhile
	case "with":
		return tWith
	}
	return tIdent
}

const (
	precCoalesce = 1
	precOr       = 1
	precAnd      = 2
	precRel      = 7
)

// binaryOps maps a token kind to its binary operator and precedence. A zero precedence means no binary operator.
var binaryOps = [tWith + 1]struct {
	op   ast.Op
	prec uint8
}{
	tCoalesce: {
		op:   ast.OpCoalesce,
		prec: precCoalesce,
	},
	tOr: {
		op:   ast.OpOr,
		prec: precOr,
	},
	tAnd: {
		op:   ast.OpAnd,
		prec: precAnd,
	},
	tBitOr: {
		op:   ast.OpBitOr,
		prec: 3,
	},
	tBitXor: {
		op:   ast.OpBitXor,
		prec: 4,
	},
	tBitAnd: {
		op:   ast.OpBitAnd,
		prec: 5,
	},
	tEq: {
		op:   ast.OpEq,
		prec: 6,
	},
	tNe: {
		op:   ast.OpNe,
		prec: 6,
	},
	tStrictEq: {
		op:   ast.OpStrictEq,
		prec: 6,
	},
	tStrictNe: {
		op:   ast.OpStrictNe,
		prec: 6,
	},
	tLt: {
		op:   ast.OpLt,
		prec: precRel,
	},
	tLe: {
		op:   ast.OpLe,
		prec: precRel,
	},
	tGt: {
		op:   ast.OpGt,
		prec: precRel,
	},
	tGe: {
		op:   ast.OpGe,
		prec: precRel,
	},
	tIn: {
		op:   ast.OpIn,
		prec: precRel,
	},
	tInstanceof: {
		op:   ast.OpInstanceof,
		prec: precRel,
	},
	tShl: {
		op:   ast.OpShl,
		prec: 8,
	},
	tShr: {
		op:   ast.OpShr,
		prec: 8,
	},
	tUShr: {
		op:   ast.OpUShr,
		prec: 8,
	},
	tAdd: {
		op:   ast.OpAdd,
		prec: 9,
	},
	tSub: {
		op:   ast.OpSub,
		prec: 9,
	},
	tMul: {
		op:   ast.OpMul,
		prec: 10,
	},
	tDiv: {
		op:   ast.OpDiv,
		prec: 10,
	},
	tMod: {
		op:   ast.OpMod,
		prec: 10,
	},
}

// assignOps maps an assignment token kind, relative to tAssign, to the operator of the assignment.
var assignOps = [...]ast.Op{
	tAssign - tAssign:         ast.OpNone,
	tAddAssign - tAssign:      ast.OpAdd,
	tSubAssign - tAssign:      ast.OpSub,
	tMulAssign - tAssign:      ast.OpMul,
	tDivAssign - tAssign:      ast.OpDiv,
	tModAssign - tAssign:      ast.OpMod,
	tExpAssign - tAssign:      ast.OpExp,
	tShlAssign - tAssign:      ast.OpShl,
	tShrAssign - tAssign:      ast.OpShr,
	tUShrAssign - tAssign:     ast.OpUShr,
	tBitAndAssign - tAssign:   ast.OpBitAnd,
	tBitOrAssign - tAssign:    ast.OpBitOr,
	tBitXorAssign - tAssign:   ast.OpBitXor,
	tAndAssign - tAssign:      ast.OpAnd,
	tOrAssign - tAssign:       ast.OpOr,
	tCoalesceAssign - tAssign: ast.OpCoalesce,
}

func isAssignOp(k tokKind) bool {
	return k >= tAssign && k <= tCoalesceAssign
}
