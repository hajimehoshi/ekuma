// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package jstest

import (
	"testing"
	"testing/fstest"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

var nativeQueueMicrotask = engine.RegisterNative(queueMicrotask)

func installV8QueueMicrotask(cx *engine.ExecutionContext) {
	f := cx.NewNative("queueMicrotask", 1, nativeQueueMicrotask)
	cx.Define(cx.GlobalObject().Object(), cx.Intern("queueMicrotask"), storage.ObjectValue(f), storage.FlagsHidden)
}

func queueMicrotask(cx *engine.ExecutionContext, _ storage.Value, args []storage.Value) storage.Value {
	var callback storage.Value
	if len(args) != 0 {
		callback = args[0]
	}
	cx.QueueMicrotask(callback)
	return storage.Undefined
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
		const child = Realm.createAllowCrossRealmAccess();
		const callback = new (Realm.global(child).Proxy)(() => print(Realm.current() === child), {});
		queueMicrotask(callback);
	`, suitePassed, "[true]")
	check(`// Flags: --enable-queue-microtask
		queueMicrotask(async () => { throw Error("rejected"); });
		setTimeout(() => print("timer ran"));
	`, suiteFailed, "[timer ran]")
}
