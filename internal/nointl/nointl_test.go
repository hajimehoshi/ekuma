// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package nointl_test tests a program that does not import package intl, in a test binary of
// its own.
package nointl_test

import (
	"context"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

// runScript parses src as a script of vm and evaluates it.
func runScript(vm *ekuma.VM, src string) (ekuma.Value, error) {
	s, err := vm.ParseScript("test.js", src)
	if err != nil {
		return ekuma.Undefined, err
	}
	return s.Evaluate(context.Background())
}

func TestNoIntl(t *testing.T) {
	var vm ekuma.VM
	v, err := runScript(&vm, `"Intl" in globalThis`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Bool() {
		t.Error("Intl is defined without package intl")
	}
}

func TestToLocaleStringWithoutIntl(t *testing.T) {
	var vm ekuma.VM
	v, err := runScript(&vm, `(1234.5).toLocaleString("de") + " " + (12345n).toLocaleString("de")`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "1234.5 12345"; got != want {
		t.Errorf("toLocaleString: got %q, want %q", got, want)
	}
}
