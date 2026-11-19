// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "variables of a finally region or a using declaration that throws"
---*/

var fns = [];
function res(name) {
	return { name: name, [Symbol.dispose]() {} };
}
function usingLoop(items) {
	for (using x of items) {
		fns.push(function () { return x.name; });
		if (x.name === "b") throw "using";
	}
}
function tryFinally() {
	try {
		let v = "c";
		fns.push(function () { return v; });
		throw "finally";
	} finally {
	}
}
// A region whose body has no registers of its own.
function emptyUsingLoop(items) {
	for (using x of items) {}
}
// Overwrites the stack that the functions above used.
function clobber(p, q, r, s, t, u, v, w) {
	var a = [p, q, r, s, t, u, v, w];
	return a.join();
}

var thrown = [];
try { usingLoop([res("a"), res("b")]); } catch (e) { thrown.push(e); }
clobber(1, 2, 3, 4, 5, 6, 7, 8);
try { tryFinally(); } catch (e) { thrown.push(e); }
clobber(1, 2, 3, 4, 5, 6, 7, 8);
emptyUsingLoop([res("d")]);
assert.sameValue(thrown.join(), "using,finally");
assert.sameValue(fns.map(function (g) { return g(); }).join(), "a,b,c");
