// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package nointl_test tests a program that does not import package intl, in a test binary of
// its own.
package nointl_test

import (
	"context"
	"testing"
	"time"

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
	if err := vm.SetLocale("ja-JP"); err != nil {
		t.Fatal(err)
	}
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

func TestArrayToLocaleStringCallsJoinWithoutIntl(t *testing.T) {
	var vm ekuma.VM
	v, err := runScript(&vm, `
		const element = {
			toLocaleString() { return array; },
			toString() { return "element"; }
		};
		const array = [element];
		array.toLocaleString();
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "element"; got != want {
		t.Errorf("array.toLocaleString() = %q, want %q", got, want)
	}
}

func TestDateAndTemporalToLocaleStringWithoutIntl(t *testing.T) {
	var vm ekuma.VM
	vm.SetTimeZone(time.UTC)
	v, err := runScript(&vm, `new Date(0).toLocaleString("de") + " " + new Temporal.PlainDate(2024, 3, 1).toLocaleString("de")`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "1/1/1970, 12:00:00 AM 2024-03-01"; got != want {
		t.Errorf("toLocaleString: got %q, want %q", got, want)
	}
}

func TestStringLocaleMethodsWithoutIntl(t *testing.T) {
	var vm ekuma.VM
	// Without Intl, localeCompare compares code units after NFC, and the case mappings
	// ignore the locales.
	v, err := runScript(&vm, `["a".localeCompare("B"), "ö".localeCompare("ö"), "i".toLocaleUpperCase("tr"), [1234.5].toLocaleString("de")].join(" ")`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "1 0 I 1234.5"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
