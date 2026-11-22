// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// TestStringLiteralIndex tests that a string literal key is read as an integer index only when it
// is the canonical form of one that fits.
func TestStringLiteralIndex(t *testing.T) {
	testCases := []struct {
		name string
		key  string

		// want is the index that the read uses, or -1 for a read with a key in a register.
		want int32
	}{
		{
			name: "index",
			key:  `"5"`,
			want: 5,
		},
		{
			name: "leading zero",
			key:  `"05"`,
			want: -1,
		},
		{
			name: "above int32",
			key:  `"2147483648"`,
			want: -1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parser.ParseScript(wtf8.String(`o[`+tc.key+`];`), parser.Options{})
			if err != nil {
				t.Fatal(err)
			}
			f, err := compiler.CompileScript(prog, "test.js")
			if err != nil {
				t.Fatal(err)
			}
			got := int32(-2)
			for _, i := range f.Code {
				switch i.Op {
				case bytecode.OpGetIndexInt:
					got = i.C
				case bytecode.OpGetIndex:
					got = -1
				}
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestLiteralIndexUpdate tests that a compound assignment reads a literal index key without
// converting it, and converts any other key first.
func TestLiteralIndexUpdate(t *testing.T) {
	testCases := []struct {
		name string
		src  string

		// want is the index that the read uses, or -1 for a read after a conversion of the key.
		want int32
	}{
		{
			name: "literal index",
			src:  `o[3] += v;`,
			want: 3,
		},
		{
			name: "variable key",
			src:  `o[k] += v;`,
			want: -1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parser.ParseScript(wtf8.String(tc.src), parser.Options{})
			if err != nil {
				t.Fatal(err)
			}
			f, err := compiler.CompileScript(prog, "test.js")
			if err != nil {
				t.Fatal(err)
			}
			got := int32(-2)
			var converted bool
			for _, i := range f.Code {
				switch i.Op {
				case bytecode.OpToPropertyKey:
					converted = true
				case bytecode.OpGetIndexInt:
					got = i.C
				case bytecode.OpGetIndex:
					if converted {
						got = -1
					}
				}
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// TestLiteralIndexStore tests that a store to a literal index key holds the index in the
// instruction.
func TestLiteralIndexStore(t *testing.T) {
	testCases := []struct {
		name string
		src  string

		wantOp bytecode.Opcode

		// wantIndex is the index that the store holds, for an OpSetIndexInt or OpSetIndexIntStrict.
		wantIndex int32
	}{
		{
			name:      "assignment",
			src:       `o[3] = v;`,
			wantOp:    bytecode.OpSetIndexInt,
			wantIndex: 3,
		},
		{
			name:      "strict assignment",
			src:       `"use strict"; o[3] = v;`,
			wantOp:    bytecode.OpSetIndexIntStrict,
			wantIndex: 3,
		},
		{
			name:      "compound assignment",
			src:       `o["4"] += v;`,
			wantOp:    bytecode.OpSetIndexInt,
			wantIndex: 4,
		},
		{
			name:   "variable key",
			src:    `o[k] = v;`,
			wantOp: bytecode.OpSetIndex,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parser.ParseScript(wtf8.String(tc.src), parser.Options{})
			if err != nil {
				t.Fatal(err)
			}
			f, err := compiler.CompileScript(prog, "test.js")
			if err != nil {
				t.Fatal(err)
			}
			var store *bytecode.Instr
			for k, i := range f.Code {
				switch i.Op {
				case bytecode.OpSetIndex, bytecode.OpSetIndexStrict, bytecode.OpSetIndexInt, bytecode.OpSetIndexIntStrict:
					store = &f.Code[k]
				}
			}
			if store == nil {
				t.Fatal("no store to an index")
			}
			if store.Op != tc.wantOp {
				t.Errorf("got %s, want %s", store.Op, tc.wantOp)
			}
			if tc.wantOp == bytecode.OpSetIndexInt || tc.wantOp == bytecode.OpSetIndexIntStrict {
				if store.B != tc.wantIndex {
					t.Errorf("got the index %d, want %d", store.B, tc.wantIndex)
				}
			}
		})
	}
}
