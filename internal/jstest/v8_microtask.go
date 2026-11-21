// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"testing"
	"testing/fstest"

	"github.com/hajimehoshi/ekuma/internal/engine"
)

func installV8QueueMicrotask(vm *engine.VM) {
	queueMicrotask := vm.NewNative("queueMicrotask", 1, func(vm *engine.VM, _ engine.Value, args []engine.Value) engine.Value {
		var callback engine.Value
		if len(args) != 0 {
			callback = args[0]
		}
		vm.QueueMicrotask(callback)
		return engine.Undefined
	})
	vm.Define(vm.GlobalObject().Object(), vm.Intern("queueMicrotask"), engine.ObjectValue(queueMicrotask), engine.FlagsHidden)
}

// CheckQueueMicrotask checks the d8 binding and its microtask checkpoint.
func (host *V8) CheckQueueMicrotask(t *testing.T) {
	test := v8Test{
		file:  "test/mjsunit/microtask.js",
		flags: []string{"--enable-queue-microtask"},
	}
	check := func(src string, want suiteOutcome, wantPrint string) {
		t.Helper()
		checkout := fstest.MapFS{
			test.file: {
				Data: []byte(src),
			},
		}
		result, printed := test.execute(t.Context(), host, checkout, testTimeout)
		if result.outcome != want {
			t.Errorf("outcome = %v, want %v (%s)", result.outcome, want, result.reason)
		}
		if printed != wantPrint {
			t.Errorf("last print = %q, want %q", printed, wantPrint)
		}
	}
	check(`if ("queueMicrotask" in globalThis) throw Error("unexpected queueMicrotask");`, suitePassed, "")
	check(`// Flags: --enable-queue-microtask
		const child = Realm.global(Realm.createAllowCrossRealmAccess());
		for (const args of [[], [{}]]) {
			let caught;
			try { child.queueMicrotask(...args); } catch (e) { caught = e; }
			if (!(caught instanceof child.TypeError)) throw Error("callback validation");
		}
		let order = "";
		Promise.resolve().then(() => order += "A");
		const result = queueMicrotask(function () {
			"use strict";
			if (this !== undefined || arguments.length) quit(1);
			order += "B";
			queueMicrotask(() => order += "D");
			return {get then() { quit(1); }};
		}, 42);
		if (result !== undefined) throw Error("queueMicrotask return value");
		Promise.resolve().then(() => order += "C");
		setTimeout(() => print(order));
	`, suitePassed, "[ABCD]")
	check(`// Flags: --enable-queue-microtask
		queueMicrotask(async () => { throw Error("rejected"); });
		setTimeout(() => print("timer ran"));
	`, suiteFailed, "[timer ran]")
}
