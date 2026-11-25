// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package engine implements the JavaScript virtual machine: the interpreter and the abstract
// operations of the language. The values and objects are in the package storage, and the built-in
// objects are in the package builtin.
package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// typeofNames are the results of the typeof operator, indexed by the typeof constants.
var typeofNames = [storage.TypeofCount]wtf8.String{
	storage.TypeofUndefined: "undefined",
	storage.TypeofNumber:    "number",
	storage.TypeofBoolean:   "boolean",
	storage.TypeofString:    "string",
	storage.TypeofSymbol:    "symbol",
	storage.TypeofBigInt:    "bigint",
	storage.TypeofFunction:  "function",
	storage.TypeofObject:    "object",
}
