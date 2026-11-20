// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a call that throws leaves the variable it assigns unchanged"
---*/

function thrower() {
	throw new Error("thrown");
}

function Thrower() {
	throw new Error("thrown");
}

assert.sameValue((function () {
	var x = 1;
	try {
		x = thrower();
	} catch (e) {
		return x;
	}
})(), 1, "a function written in JavaScript");

assert.sameValue((function () {
	var x = 1;
	try {
		x = JSON.parse("{");
	} catch (e) {
		return x;
	}
})(), 1, "a native function");

assert.sameValue((function () {
	var x = 1;
	try {
		x = new Thrower();
	} catch (e) {
		return x;
	}
})(), 1, "a constructor written in JavaScript");

assert.sameValue((function () {
	var x = 1;
	try {
		x = new Array(-1);
	} catch (e) {
		return x;
	}
})(), 1, "a native constructor");

assert.sameValue((function () {
	var x = 1;
	try {
		x = eval("throw 0;");
	} catch (e) {
		return x;
	}
})(), 1, "a direct eval");

assert.sameValue((function () {
	var x = 1;
	try {
		x = x > 0 ? thrower() : 2;
	} catch (e) {
		return x;
	}
})(), 1, "a conditional");

assert.sameValue((function () {
	var x = 1;
	try {
		x = (x++, thrower());
	} catch (e) {
		return x;
	}
})(), 2, "a sequence");

assert.sameValue((function () {
	var x = 1, y = 1;
	try {
		x = y = thrower();
	} catch (e) {
		return [x, y].join();
	}
})(), "1,1", "an assignment to another variable");

assert.sameValue((function () {
	var x = 1;
	x = eval("x + 1");
	return x;
})(), 2, "a direct eval that reads the variable");

assert.sameValue((function () {
	var read;
	try {
		read = function () {
			return c;
		};
		let c = thrower();
	} catch (e) {
	}
	try {
		read();
	} catch (e) {
		return e instanceof ReferenceError;
	}
	return false;
})(), true, "a lexical declaration");
