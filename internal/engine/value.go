// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package engine implements the JavaScript virtual machine: the interpreter and the abstract
// operations of the language. The values and objects are in the package storage, and the built-in
// objects are in the package builtin.
package engine

import "github.com/hajimehoshi/ekuma/internal/wtf8"

// typeofNames are the results of the typeof operator, indexed by the typeof constants.
var typeofNames = [typeofCount]wtf8.String{
	typeofUndefined: "undefined",
	typeofNumber:    "number",
	typeofBoolean:   "boolean",
	typeofString:    "string",
	typeofSymbol:    "symbol",
	typeofBigInt:    "bigint",
	typeofFunction:  "function",
	typeofObject:    "object",
}
