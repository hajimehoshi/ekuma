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

func TestNoIntl(t *testing.T) {
	var vm ekuma.VM
	v, err := vm.RunScript(context.Background(), "test.js", `"Intl" in globalThis`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Bool() {
		t.Error("Intl is defined without package intl")
	}
}
