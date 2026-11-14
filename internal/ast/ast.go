// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package ast defines the syntax tree and the scope records of ECMAScript source text.
//
// String values (string literals and template strings) are Go strings in WTF-8:
// UTF-8 in which a lone surrogate is encoded as its three-byte sequence.
package ast

// Node is a syntax tree node.
type Node interface {
	// Start returns the byte offset of the first byte of the node.
	Start() int32
	// End returns the byte offset just past the last byte of the node.
	End() int32
}

// Expr is an expression node.
type Expr interface {
	Node
	exprNode()
}

// Stmt is a statement or declaration node.
type Stmt interface {
	Node
	stmtNode()
}

// Pattern is a binding or assignment target.
//
// The implementations are *Identifier, *MemberExpr (assignment targets only),
// *CallExpr (a sloppy-mode assignment target that throws a ReferenceError when
// assigned), *ObjectPattern, *ArrayPattern, *AssignPattern (a target with a default
// value) and *RestElement.
type Pattern interface {
	Node
	patternNode()
}

// Span is the source range of a node as byte offsets.
type Span struct {
	StartOff int32
	EndOff   int32
}

// Start returns the byte offset of the first byte of the range.
func (s Span) Start() int32 { return s.StartOff }

// End returns the byte offset just past the last byte of the range.
func (s Span) End() int32 { return s.EndOff }

// Op is an operator.
type Op uint8

const (
	// OpNone is the absence of an operator. In an AssignExpr it means plain assignment.
	OpNone Op = iota

	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpExp
	OpShl
	OpShr
	OpUShr
	OpBitAnd
	OpBitOr
	OpBitXor
	OpEq
	OpNe
	OpStrictEq
	OpStrictNe
	OpLt
	OpLe
	OpGt
	OpGe
	OpIn
	OpInstanceof

	OpAnd
	OpOr
	OpCoalesce

	OpNeg
	OpPos
	OpNot
	OpBitNot
	OpTypeof
	OpVoid
	OpDelete

	OpInc
	OpDec
)

var opNames = [...]string{
	OpNone:       "",
	OpAdd:        "+",
	OpSub:        "-",
	OpMul:        "*",
	OpDiv:        "/",
	OpMod:        "%",
	OpExp:        "**",
	OpShl:        "<<",
	OpShr:        ">>",
	OpUShr:       ">>>",
	OpBitAnd:     "&",
	OpBitOr:      "|",
	OpBitXor:     "^",
	OpEq:         "==",
	OpNe:         "!=",
	OpStrictEq:   "===",
	OpStrictNe:   "!==",
	OpLt:         "<",
	OpLe:         "<=",
	OpGt:         ">",
	OpGe:         ">=",
	OpIn:         "in",
	OpInstanceof: "instanceof",
	OpAnd:        "&&",
	OpOr:         "||",
	OpCoalesce:   "??",
	OpNeg:        "-",
	OpPos:        "+",
	OpNot:        "!",
	OpBitNot:     "~",
	OpTypeof:     "typeof",
	OpVoid:       "void",
	OpDelete:     "delete",
	OpInc:        "++",
	OpDec:        "--",
}

// String returns the source spelling of the operator.
func (o Op) String() string {
	if int(o) < len(opNames) {
		return opNames[o]
	}
	return "?"
}

// Identifier is an identifier used as a reference, a binding, a label or a property name.
type Identifier struct {
	Span
	Name string

	// Parenthesized reports whether the identifier reference was written in parentheses.
	// A parenthesized assignment target does not name an anonymous function assigned to it.
	Parenthesized bool
}

// PrivateName is a private identifier such as #x. Name excludes the '#'.
type PrivateName struct {
	Span
	Name string
}

// NullLit is the null literal.
type NullLit struct {
	Span
}

// BoolLit is a boolean literal.
type BoolLit struct {
	Span
	Value bool
}

// NumberLit is a numeric literal.
type NumberLit struct {
	Span
	Value float64
}

// BigIntLit is a BigInt literal. Value is its decimal digit string.
type BigIntLit struct {
	Span
	Value string
}

// StringLit is a string literal. Value is in WTF-8.
type StringLit struct {
	Span
	Value string
}

// RegExpLit is a regular expression literal. Pattern and Flags are source text.
type RegExpLit struct {
	Span
	Pattern string
	Flags   string
}

// TemplateElement is a string part of a template literal. Cooked and Raw are in WTF-8.
type TemplateElement struct {
	Span
	Cooked string
	Raw    string

	// HasCooked is false when the part contains an invalid escape, which only a tagged template permits.
	HasCooked bool
}

// TemplateLit is a template literal. len(Quasis) is len(Exprs)+1.
type TemplateLit struct {
	Span
	Quasis []TemplateElement
	Exprs  []Expr
}

// TaggedTemplate is a tagged template expression.
type TaggedTemplate struct {
	Span
	Tag   Expr
	Quasi *TemplateLit
}

// ThisExpr is the this keyword.
type ThisExpr struct {
	Span
}

// SuperExpr is the super keyword. It only appears as MemberExpr.Object or CallExpr.Callee.
type SuperExpr struct {
	Span
}

// NewTargetExpr is new.target.
type NewTargetExpr struct {
	Span
}

// ImportMetaExpr is import.meta.
type ImportMetaExpr struct {
	Span
}

// ImportCall is a dynamic import. Options is nil when absent.
type ImportCall struct {
	Span
	Source  Expr
	Options Expr
}

// ArrayLit is an array literal. A nil element is a hole. An element may be a *SpreadElement.
type ArrayLit struct {
	Span
	Elements []Expr
}

// PropertyKind is the kind of an object literal property.
type PropertyKind uint8

const (
	// PropertyInit is a key and value pair, including shorthand properties.
	PropertyInit PropertyKind = iota
	// PropertyMethod is a method definition. Value is a *Function.
	PropertyMethod
	// PropertyGet is a getter. Value is a *Function.
	PropertyGet
	// PropertySet is a setter. Value is a *Function.
	PropertySet
	// PropertySpread is a spread property. Key is nil and Value is the spread operand.
	PropertySpread
)

// Property is a property of an object literal.
//
// Key is an *Identifier, *StringLit, *NumberLit or *BigIntLit, or any expression when Computed is set.
type Property struct {
	Span
	Kind      PropertyKind
	Computed  bool
	Shorthand bool
	Key       Expr
	Value     Expr
}

// ObjectLit is an object literal.
type ObjectLit struct {
	Span
	Properties []Property
}

// Function is a function of any kind: declaration, expression, arrow function, method, accessor,
// class constructor, class field initializer or class static block.
//
// Body holds the statements of a block body. ExprBody is set instead for an arrow function
// with an expression body and for a class field initializer.
type Function struct {
	Span
	Name     *Identifier
	Params   []Pattern
	Body     []Stmt
	ExprBody Expr

	// Scope holds the parameters and the declarations of the top level of the body.
	Scope *Scope

	// Length is the number of parameters before the first one with a default value or the rest parameter.
	Length int

	Arrow     bool
	Async     bool
	Generator bool

	// Method reports a function with a home object: a method, a getter, a setter or a class constructor.
	Method bool
	Getter bool
	Setter bool

	ClassConstructor bool
	// DerivedConstructor reports the constructor of a class with an extends clause.
	DerivedConstructor bool

	// FieldInit reports the initializer of a class field. ExprBody is the initializer expression.
	FieldInit bool
	// StaticBlock reports a class static initialization block.
	StaticBlock bool

	// Strict reports whether the function code is strict mode code.
	Strict bool
	// SimpleParams reports whether every parameter is a plain identifier.
	SimpleParams bool

	// HasDirectEval reports whether the function, or an arrow function nested in it, calls eval directly.
	HasDirectEval bool

	// The Uses fields report use in the function itself or in arrow functions nested in it.
	// UsesArguments reports a reference to the identifier arguments.
	UsesArguments     bool
	UsesThis          bool
	UsesSuperProperty bool
	UsesSuperCall     bool
	UsesNewTarget     bool
}

// MemberKind is the kind of a class member.
type MemberKind uint8

const (
	MemberMethod MemberKind = iota
	MemberGetter
	MemberSetter
	// MemberField is a field. Value is the initializer function, or nil without an initializer.
	MemberField
	// MemberConstructor is the class constructor.
	MemberConstructor
	// MemberStaticBlock is a static initialization block. Key is nil.
	MemberStaticBlock
)

// ClassMember is an element of a class body.
//
// Key is an *Identifier, *StringLit, *NumberLit, *BigIntLit or *PrivateName, or any expression when Computed is set.
type ClassMember struct {
	Span
	Kind     MemberKind
	Static   bool
	Computed bool
	Key      Expr
	Value    *Function
}

// Class is a class declaration or expression.
type Class struct {
	Span
	Name    *Identifier
	Super   Expr
	Members []ClassMember

	// Constructor is the explicit constructor, or nil.
	Constructor *Function

	// Scope holds the inner binding of the class name.
	Scope *Scope
}

// UnaryExpr is a unary operation.
type UnaryExpr struct {
	Span
	Op  Op
	Arg Expr
}

// UpdateExpr is an increment or decrement. Arg is an *Identifier or a *MemberExpr.
type UpdateExpr struct {
	Span
	Op     Op
	Prefix bool
	Arg    Expr
}

// BinaryExpr is a binary operation other than a logical one.
type BinaryExpr struct {
	Span
	Op    Op
	Left  Expr
	Right Expr
}

// LogicalExpr is a short-circuiting binary operation.
type LogicalExpr struct {
	Span
	Op    Op
	Left  Expr
	Right Expr
}

// PrivateInExpr is a private brand check such as #x in o.
type PrivateInExpr struct {
	Span
	Name  *PrivateName
	Right Expr
}

// AssignExpr is an assignment. Op is OpNone for plain assignment, and the binary or logical operator otherwise.
//
// Target is an *Identifier or a *MemberExpr, or with plain assignment an *ObjectPattern or an *ArrayPattern.
type AssignExpr struct {
	Span
	Op     Op
	Target Pattern
	Value  Expr
}

// CondExpr is a conditional expression.
type CondExpr struct {
	Span
	Test       Expr
	Consequent Expr
	Alternate  Expr
}

// CallExpr is a call. Callee may be a *SuperExpr. An argument may be a *SpreadElement.
type CallExpr struct {
	Span
	Callee Expr
	Args   []Expr

	// Optional reports whether the call is written with ?. before the arguments.
	Optional bool
}

// NewExpr is a new expression.
type NewExpr struct {
	Span
	Callee Expr
	Args   []Expr
}

// MemberExpr is a property access.
//
// Property is an *Identifier or a *PrivateName, or any expression when Computed is set.
// Object may be a *SuperExpr.
type MemberExpr struct {
	Span
	Object   Expr
	Property Expr
	Computed bool

	// Optional reports whether the access is written with ?.
	Optional bool
}

// OptionalChain wraps a whole optional chain. A short circuit inside Expr ends the evaluation of the chain.
type OptionalChain struct {
	Span
	Expr Expr
}

// SequenceExpr is a comma expression.
type SequenceExpr struct {
	Span
	Exprs []Expr
}

// SpreadElement is a spread in an array literal or an argument list.
type SpreadElement struct {
	Span
	Arg Expr
}

// YieldExpr is a yield expression. Arg may be nil.
type YieldExpr struct {
	Span
	Arg      Expr
	Delegate bool
}

// AwaitExpr is an await expression.
type AwaitExpr struct {
	Span
	Arg Expr
}

// PatternProperty is a property of an object pattern.
type PatternProperty struct {
	Span
	Computed  bool
	Shorthand bool
	Key       Expr
	Value     Pattern
}

// ObjectPattern is an object destructuring pattern. Rest is nil when absent.
type ObjectPattern struct {
	Span
	Properties []PatternProperty
	Rest       Pattern
}

// ArrayPattern is an array destructuring pattern. A nil element is a hole. The last element may be a *RestElement.
type ArrayPattern struct {
	Span
	Elements []Pattern
}

// AssignPattern is a target with a default value.
type AssignPattern struct {
	Span
	Target  Pattern
	Default Expr
}

// RestElement is a rest element of an array pattern or a rest parameter.
type RestElement struct {
	Span
	Target Pattern
}

// ExprStmt is an expression statement.
type ExprStmt struct {
	Span
	Expr Expr

	// Directive reports whether the statement belongs to a directive prologue. Expr is then a *StringLit.
	Directive bool
}

// Declarator is one binding of a variable declaration. Init may be nil.
type Declarator struct {
	Span
	Target Pattern
	Init   Expr
}

// VarDecl is a var, let, const, using or await using declaration. Kind is DeclVar, DeclLet,
// DeclConst, DeclUsing or DeclAwaitUsing.
type VarDecl struct {
	Span
	Kind  DeclKind
	Decls []Declarator
}

// FuncDecl is a function declaration.
type FuncDecl struct {
	Span
	Func *Function

	// AnnexBHoist reports that evaluating the declaration also assigns the function to the
	// var binding of the same name in the enclosing function or program.
	AnnexBHoist bool
}

// ClassDecl is a class declaration.
type ClassDecl struct {
	Span
	Class *Class
}

// BlockStmt is a block.
type BlockStmt struct {
	Span
	Body  []Stmt
	Scope *Scope
}

// EmptyStmt is an empty statement.
type EmptyStmt struct {
	Span
}

// IfStmt is an if statement. Alternate may be nil.
type IfStmt struct {
	Span
	Test       Expr
	Consequent Stmt
	Alternate  Stmt
}

// ForStmt is a for statement. Init is nil, a *VarDecl or an Expr. Test and Update may be nil.
type ForStmt struct {
	Span
	Init   Node
	Test   Expr
	Update Expr
	Body   Stmt

	// Scope holds the let and const declarations of the head.
	Scope *Scope
}

// ForInStmt is a for-in statement. Exactly one of Decl and Target is set.
type ForInStmt struct {
	Span
	Decl   *VarDecl
	Target Pattern
	Right  Expr
	Body   Stmt
	Scope  *Scope
}

// ForOfStmt is a for-of statement. Exactly one of Decl and Target is set.
type ForOfStmt struct {
	Span
	Decl   *VarDecl
	Target Pattern
	Right  Expr
	Body   Stmt
	Await  bool
	Scope  *Scope
}

// WhileStmt is a while statement.
type WhileStmt struct {
	Span
	Test Expr
	Body Stmt
}

// DoWhileStmt is a do-while statement.
type DoWhileStmt struct {
	Span
	Body Stmt
	Test Expr
}

// ReturnStmt is a return statement. Arg may be nil.
type ReturnStmt struct {
	Span
	Arg Expr
}

// BreakStmt is a break statement. Label may be nil.
type BreakStmt struct {
	Span
	Label *Identifier
}

// ContinueStmt is a continue statement. Label may be nil.
type ContinueStmt struct {
	Span
	Label *Identifier
}

// ThrowStmt is a throw statement.
type ThrowStmt struct {
	Span
	Arg Expr
}

// TryStmt is a try statement.
//
// Handler is nil without a catch clause. Param and CatchScope are nil without a catch parameter.
// Finalizer is nil without a finally clause.
type TryStmt struct {
	Span
	Block *BlockStmt

	Param      Pattern
	CatchScope *Scope
	Handler    *BlockStmt

	Finalizer *BlockStmt
}

// SwitchCase is a clause of a switch statement. Test is nil for the default clause.
type SwitchCase struct {
	Span
	Test Expr
	Body []Stmt
}

// SwitchStmt is a switch statement.
type SwitchStmt struct {
	Span
	Discriminant Expr
	Cases        []SwitchCase

	// Scope holds the declarations of the case block.
	Scope *Scope
}

// LabeledStmt is a labelled statement.
type LabeledStmt struct {
	Span
	Label *Identifier
	Body  Stmt
}

// WithStmt is a with statement.
type WithStmt struct {
	Span
	Object Expr
	Body   Stmt
}

// DebuggerStmt is a debugger statement.
type DebuggerStmt struct {
	Span
}

// ModuleName is an import or export name, written as an identifier or as a string literal.
type ModuleName struct {
	Span
	Name     string
	IsString bool
}

// ImportKind is the kind of an import specifier.
type ImportKind uint8

const (
	// ImportNamed is a named import such as {a as b}.
	ImportNamed ImportKind = iota
	// ImportDefault is a default import.
	ImportDefault
	// ImportNamespace is a namespace import such as * as ns.
	ImportNamespace
)

// ImportSpecifier is one binding of an import declaration. Imported is only meaningful for ImportNamed.
type ImportSpecifier struct {
	Span
	Kind     ImportKind
	Imported ModuleName
	Local    *Identifier
}

// ImportAttribute is one entry of a with clause.
type ImportAttribute struct {
	Span
	Key   string
	Value *StringLit
}

// ImportDecl is an import declaration.
type ImportDecl struct {
	Span
	Specifiers []ImportSpecifier
	Source     *StringLit
	Attributes []ImportAttribute
}

// ExportSpecifier is one entry of an export clause.
type ExportSpecifier struct {
	Span
	Local    ModuleName
	Exported ModuleName
}

// ExportNamedDecl is an export of a declaration (Decl is set) or of an export clause.
// Source is nil unless the clause re-exports from another module.
type ExportNamedDecl struct {
	Span
	Decl       Stmt
	Specifiers []ExportSpecifier
	Source     *StringLit
	Attributes []ImportAttribute
}

// ExportDefaultDecl is a default export. Exactly one of Decl and Expr is set.
// Decl is a *FuncDecl or a *ClassDecl whose name may be nil.
type ExportDefaultDecl struct {
	Span
	Decl Stmt
	Expr Expr
}

// ExportAllDecl is an export * declaration. Exported is nil without an as clause.
type ExportAllDecl struct {
	Span
	Exported   *ModuleName
	Source     *StringLit
	Attributes []ImportAttribute
}

func (*Identifier) exprNode()     {}
func (*PrivateName) exprNode()    {}
func (*NullLit) exprNode()        {}
func (*BoolLit) exprNode()        {}
func (*NumberLit) exprNode()      {}
func (*BigIntLit) exprNode()      {}
func (*StringLit) exprNode()      {}
func (*RegExpLit) exprNode()      {}
func (*TemplateLit) exprNode()    {}
func (*TaggedTemplate) exprNode() {}
func (*ThisExpr) exprNode()       {}
func (*SuperExpr) exprNode()      {}
func (*NewTargetExpr) exprNode()  {}
func (*ImportMetaExpr) exprNode() {}
func (*ImportCall) exprNode()     {}
func (*ArrayLit) exprNode()       {}
func (*ObjectLit) exprNode()      {}
func (*Function) exprNode()       {}
func (*Class) exprNode()          {}
func (*UnaryExpr) exprNode()      {}
func (*UpdateExpr) exprNode()     {}
func (*BinaryExpr) exprNode()     {}
func (*LogicalExpr) exprNode()    {}
func (*PrivateInExpr) exprNode()  {}
func (*AssignExpr) exprNode()     {}
func (*CondExpr) exprNode()       {}
func (*CallExpr) exprNode()       {}
func (*NewExpr) exprNode()        {}
func (*MemberExpr) exprNode()     {}
func (*OptionalChain) exprNode()  {}
func (*SequenceExpr) exprNode()   {}
func (*SpreadElement) exprNode()  {}
func (*YieldExpr) exprNode()      {}
func (*AwaitExpr) exprNode()      {}

func (*Identifier) patternNode()    {}
func (*MemberExpr) patternNode()    {}
func (*CallExpr) patternNode()      {}
func (*ObjectPattern) patternNode() {}
func (*ArrayPattern) patternNode()  {}
func (*AssignPattern) patternNode() {}
func (*RestElement) patternNode()   {}

func (*ExprStmt) stmtNode()          {}
func (*VarDecl) stmtNode()           {}
func (*FuncDecl) stmtNode()          {}
func (*ClassDecl) stmtNode()         {}
func (*BlockStmt) stmtNode()         {}
func (*EmptyStmt) stmtNode()         {}
func (*IfStmt) stmtNode()            {}
func (*ForStmt) stmtNode()           {}
func (*ForInStmt) stmtNode()         {}
func (*ForOfStmt) stmtNode()         {}
func (*WhileStmt) stmtNode()         {}
func (*DoWhileStmt) stmtNode()       {}
func (*ReturnStmt) stmtNode()        {}
func (*BreakStmt) stmtNode()         {}
func (*ContinueStmt) stmtNode()      {}
func (*ThrowStmt) stmtNode()         {}
func (*TryStmt) stmtNode()           {}
func (*SwitchStmt) stmtNode()        {}
func (*LabeledStmt) stmtNode()       {}
func (*WithStmt) stmtNode()          {}
func (*DebuggerStmt) stmtNode()      {}
func (*ImportDecl) stmtNode()        {}
func (*ExportNamedDecl) stmtNode()   {}
func (*ExportDefaultDecl) stmtNode() {}
func (*ExportAllDecl) stmtNode()     {}
