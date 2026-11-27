// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A global store of a local variable that no other code can assign throws at the assignment on every iteration, and the stores of an assignment whose result is used and of the global undefined store the value"
flags: [noStrict, noRepeat]
noRepeatReason: "the proxy stays on the prototype chain of the global object"
---*/

// where returns the line and column of frame in the stack of the error that f throws.
function where(f, frame) {
	try {
		f();
	} catch (e) {
		return /:(\d+:\d+)\)?$/.exec(e.stack.split("\n")[frame])[1];
	}
}

function storeUndeclared(v) {
	"use strict";
	for (var i = 0; i < 2; i++) {
		if (i > 0)
			undeclaredGlobal = v;
	}
}

Object.defineProperty(globalThis, "throwingSetter", {
	set(v) { throw new Error(); },
	configurable: true,
});
function storeToSetter(v) {
	throwingSetter = v;
}

Object.setPrototypeOf(globalThis, new Proxy(Object.getPrototypeOf(globalThis), {
	has(target, key) {
		if (key === "trappedGlobal") {
			throw new Error();
		}
		return Reflect.has(target, key);
	},
}));
function storeTrapped(v) {
	trappedGlobal = v;
}

var resultGlobal, chainOuter, chainInner, undefinedGlobal = 0;
function result(v) {
	return resultGlobal = v;
}
function chain(v) {
	chainOuter = chainInner = v;
}
function storeUndefined() {
	undefinedGlobal = undefined;
}

for (var n = 0; n < 3; n++) {
	assert.sameValue(where(() => storeUndeclared(n), 1), "23:4");
	assert.sameValue(where(() => storeToSetter(n), 2), "32:2");
	assert.sameValue(where(() => storeTrapped(n), 2), "44:2");
	assert.sameValue(result(n), n);
	chain(n);
	assert.sameValue(chainOuter, n);
	assert.sameValue(chainInner, n);
	storeUndefined();
	assert.sameValue(undefinedGlobal, undefined);
}
