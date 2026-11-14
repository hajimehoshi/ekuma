// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ast

// ScopeKind is the kind of construct that creates a scope.
type ScopeKind uint8

const (
	// ScopeScript is the top level of a script.
	ScopeScript ScopeKind = iota
	// ScopeModule is the top level of a module.
	ScopeModule
	// ScopeFunction covers the parameters and the top level of the body of a function.
	ScopeFunction
	// ScopeBlock is a block statement.
	ScopeBlock
	// ScopeFor is the head of a for, for-in or for-of statement.
	ScopeFor
	// ScopeCatch holds the parameter of a catch clause. The clause body is a child ScopeBlock.
	ScopeCatch
	// ScopeSwitch is the case block of a switch statement.
	ScopeSwitch
	// ScopeClass covers the heritage and the body of a class.
	ScopeClass
)

// DeclKind is the kind of a declaration.
type DeclKind uint8

const (
	DeclVar DeclKind = iota
	DeclLet
	DeclConst
	// DeclFunction is a function declaration. It is var-like at the top level of a script or a function,
	// and lexical in a block or at the top level of a module.
	DeclFunction
	DeclClass
	DeclParam
	DeclCatchParam
	DeclImport
	// DeclAnnexBVar is the var binding created in a function or script scope for a
	// function declared in a nested block of sloppy mode code.
	DeclAnnexBVar
	DeclUsing
	DeclAwaitUsing
)

// Decl is one declaration of a scope.
type Decl struct {
	Name string
	Kind DeclKind

	// Pos is the byte offset of the declared name.
	Pos int32

	// Func is the declared function for DeclFunction, and nil otherwise.
	Func *Function
}

// Scope is the record of the declarations of one scope.
//
// Decls is in source order. A name declared with var more than once is listed once as DeclVar.
// Every function declaration is listed, so the last DeclFunction of a name is the one in effect.
// A var declaration is listed in the scope of the enclosing function, script or module.
// A name can be listed under several kinds, for example as a parameter and as a var.
// DeclAnnexBVar entries come after all other entries.
// A default export without a name is listed as "*default*".
type Scope struct {
	Kind   ScopeKind
	Parent *Scope
	Decls  []Decl

	// ContainsDirectEval reports whether a direct eval call occurs in this scope or in any scope nested in it,
	// including nested functions.
	ContainsDirectEval bool
}
